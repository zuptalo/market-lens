package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/db"
	"market-lens/server/internal/testdb"
)

func TestSignInStartReturnsOneGenericProgressionWithoutOwnerEnumeration(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	clock, secrets, _ := provisionOwner(t, pool)
	service, _ := newOwnerAuthService(t, pool, clock, secrets, 1, 8*time.Hour, 30*24*time.Hour)

	for _, email := range []string{
		"owner@example.com",
		"unknown@example.com",
		"not-an-email",
		" OWNER@EXAMPLE.COM ",
	} {
		result, err := service.StartSignIn(context.Background(), auth.SignInStartRequest{
			Email: email, Origin: "198.51.100.0/24",
		})
		if err != nil {
			t.Fatalf("email %q produced distinguishable error: %v", email, err)
		}
		if result.Message != auth.GenericSignInMessage {
			t.Fatalf("email %q message = %q, want generic message", email, result.Message)
		}
	}

	var ownerCodeChallenges, ownerCodeDeliveries int
	if err := pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM member_login_challenges c JOIN users u ON u.id=c.user_id WHERE u.role='owner'),
		(SELECT count(*) FROM account_email_deliveries d JOIN users u ON u.id=d.subject_user_id WHERE u.role='owner' AND d.kind='member_login_code')
	`).Scan(&ownerCodeChallenges, &ownerCodeDeliveries); err != nil {
		t.Fatal(err)
	}
	if ownerCodeChallenges != 0 || ownerCodeDeliveries != 0 {
		t.Fatalf("generic sign-in sent owner OTP challenges=%d deliveries=%d", ownerCodeChallenges, ownerCodeDeliveries)
	}
}

func TestOwnerPasswordResetAtomicallyChangesCredentialAndRevokesEverySession(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	clock, secrets, _ := provisionOwner(t, pool)
	service, hasher := newOwnerAuthService(t, pool, clock, secrets, 1, 8*time.Hour, 30*24*time.Hour)
	if _, err := service.LoginOwner(context.Background(), auth.OwnerLoginRequest{
		Email: "owner@example.com", Password: "correct horse battery staple",
		DeviceLabel: "Second device", Origin: "198.51.100.0/24",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.ResetOwnerPassword(context.Background(), "replacement owner password"); err != nil {
		t.Fatal(err)
	}
	var activeSessions, audits, resetEvents, sessionEvents int
	var passwordHash string
	if err := pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM sessions WHERE revoked_at IS NULL),
		(SELECT count(*) FROM security_audit_events WHERE event_type='owner.password_reset.v1' AND outcome='succeeded'),
		(SELECT count(*) FROM client_events WHERE event_type='owner.password_reset.v1' AND scope='owner'),
		(SELECT count(*) FROM client_events WHERE event_type='sessions.revoked.v1' AND scope='user'),
		(SELECT password_hash FROM owner_credentials LIMIT 1)
	`).Scan(&activeSessions, &audits, &resetEvents, &sessionEvents, &passwordHash); err != nil {
		t.Fatal(err)
	}
	valid, _, err := hasher.Verify(passwordHash, "replacement owner password")
	if err != nil {
		t.Fatal(err)
	}
	if activeSessions != 0 || audits != 1 || resetEvents != 1 || sessionEvents != 1 || !valid {
		t.Fatalf("reset state sessions=%d audits=%d reset_events=%d session_events=%d password_valid=%v",
			activeSessions, audits, resetEvents, sessionEvents, valid)
	}
	if _, err := service.LoginOwner(context.Background(), auth.OwnerLoginRequest{
		Email: "owner@example.com", Password: "correct horse battery staple",
		DeviceLabel: "Old password", Origin: "198.51.100.0/24",
	}); !errors.Is(err, auth.ErrAuthenticationFailed) {
		t.Fatalf("old owner password login error = %v", err)
	}
}
