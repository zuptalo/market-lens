package auth_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/authtest"
	"market-lens/server/internal/credentials"
	"market-lens/server/internal/db"
	"market-lens/server/internal/identity"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOwnerLoginUsesGenericFailuresRehashesAndRotatesIntoNewSession(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	clock, secrets, bootstrap := provisionOwner(t, pool)
	service, strongerHasher := newOwnerAuthService(t, pool, clock, secrets, 2, 8*time.Hour, 30*24*time.Hour)

	for _, request := range []auth.OwnerLoginRequest{
		{Email: "unknown@example.com", Password: "wrong password value", DeviceLabel: "Unknown", Origin: "198.51.100.0/24"},
		{Email: "owner@example.com", Password: "wrong password value", DeviceLabel: "Wrong", Origin: "198.51.100.0/24"},
	} {
		_, err := service.LoginOwner(context.Background(), request)
		if !errors.Is(err, auth.ErrAuthenticationFailed) {
			t.Fatalf("login failure = %v, want generic authentication failure", err)
		}
		if strings.Contains(err.Error(), request.Email) || strings.Contains(err.Error(), request.Password) {
			t.Fatalf("login failure disclosed submitted credentials: %v", err)
		}
	}

	result, err := service.LoginOwner(context.Background(), auth.OwnerLoginRequest{
		Email: "OWNER@EXAMPLE.COM", Password: "correct horse battery staple",
		DeviceLabel: "Firefox on Linux", Origin: "198.51.100.0/24",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Account.Role != "owner" || result.SessionToken == "" || result.CSRFToken == "" ||
		result.SessionToken == bootstrap.SessionToken || result.CSRFToken == bootstrap.CSRFToken {
		t.Fatalf("owner login did not rotate one-time session material: %#v", result)
	}
	var passwordHash, persistedSessions string
	var sessionCount, loginEventCount int
	if err := pool.QueryRow(context.Background(), `SELECT
		(SELECT password_hash FROM owner_credentials LIMIT 1),
		(SELECT count(*) FROM sessions),
		(SELECT count(*) FROM client_events WHERE event_type='session.created.v1' AND scope='user' AND subject_user_id=$1),
		(SELECT jsonb_agg(to_jsonb(s))::text FROM sessions s)`, result.Account.ID).
		Scan(&passwordHash, &sessionCount, &loginEventCount, &persistedSessions); err != nil {
		t.Fatal(err)
	}
	valid, needsRehash, err := strongerHasher.Verify(passwordHash, "correct horse battery staple")
	if err != nil || !valid || needsRehash {
		t.Fatalf("owner hash was not upgraded: valid=%v needsRehash=%v err=%v", valid, needsRehash, err)
	}
	if sessionCount != 2 {
		t.Fatalf("session count = %d, want bootstrap plus login", sessionCount)
	}
	if loginEventCount != 1 {
		t.Fatalf("transactional login events = %d, want 1", loginEventCount)
	}
	for _, secret := range []string{result.SessionToken, result.CSRFToken, "correct horse battery staple"} {
		if strings.Contains(persistedSessions, secret) {
			t.Fatal("owner login persisted plaintext authentication material")
		}
	}
}

func TestOwnerSessionAuthenticationExpiryAndRevocation(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	clock, secrets, _ := provisionOwner(t, pool)
	service, _ := newOwnerAuthService(t, pool, clock, secrets, 1, 8*time.Hour, 30*24*time.Hour)
	login, err := service.LoginOwner(context.Background(), auth.OwnerLoginRequest{
		Email: "owner@example.com", Password: "correct horse battery staple",
		DeviceLabel: "Safari on iPhone", Origin: "203.0.113.0/24",
	})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := service.AuthenticateSession(context.Background(), login.SessionToken)
	if err != nil || principal.UserID != login.Account.ID || principal.SessionID != login.Session.ID ||
		!principal.VerifyCSRF(login.CSRFToken) || principal.VerifyCSRF("wrong") {
		t.Fatalf("authenticated principal = %#v err=%v", principal, err)
	}
	account, err := service.Account(context.Background(), principal.UserID)
	if err != nil || account.ID != principal.UserID || account.Email != "owner@example.com" ||
		account.DisplayName != "Market Owner" || account.Role != "owner" || account.Status != "active" {
		t.Fatalf("safe account = %#v err=%v", account, err)
	}
	sessions, err := service.ListSessions(context.Background(), principal.UserID, principal.SessionID)
	if err != nil || len(sessions) != 2 {
		t.Fatalf("safe sessions = %#v err=%v", sessions, err)
	}
	if err := service.RevokeSession(context.Background(), principal.UserID, principal.SessionID); err != nil {
		t.Fatal(err)
	}
	assertScopedEventCount(t, pool, "session.revoked.v1", principal.UserID, principal.SessionID, 1)
	if _, err := service.AuthenticateSession(context.Background(), login.SessionToken); !errors.Is(err, auth.ErrAuthenticationRequired) {
		t.Fatalf("revoked session authentication error = %v", err)
	}

	second, err := service.LoginOwner(context.Background(), auth.OwnerLoginRequest{
		Email: "owner@example.com", Password: "correct horse battery staple",
		DeviceLabel: "Chrome on Android", Origin: "203.0.113.0/24",
	})
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(8 * time.Hour)
	if _, err := service.AuthenticateSession(context.Background(), second.SessionToken); !errors.Is(err, auth.ErrAuthenticationRequired) {
		t.Fatalf("idle-expired session authentication error = %v", err)
	}

	absoluteService, _ := newOwnerAuthService(t, pool, clock, secrets, 1, 20*24*time.Hour, 30*24*time.Hour)
	absoluteSession, err := absoluteService.LoginOwner(context.Background(), auth.OwnerLoginRequest{
		Email: "owner@example.com", Password: "correct horse battery staple",
		DeviceLabel: "Long-lived desktop", Origin: "203.0.113.0/24",
	})
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(15 * 24 * time.Hour)
	if _, err := absoluteService.AuthenticateSession(context.Background(), absoluteSession.SessionToken); err != nil {
		t.Fatalf("session did not renew before absolute expiry: %v", err)
	}
	clock.Advance(15 * 24 * time.Hour)
	if _, err := absoluteService.AuthenticateSession(context.Background(), absoluteSession.SessionToken); !errors.Is(err, auth.ErrAuthenticationRequired) {
		t.Fatalf("absolute-expired session authentication error = %v", err)
	}

	activeAgain, err := absoluteService.LoginOwner(context.Background(), auth.OwnerLoginRequest{
		Email: "owner@example.com", Password: "correct horse battery staple",
		DeviceLabel: "Replacement desktop", Origin: "203.0.113.0/24",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := absoluteService.RevokeAllSessions(context.Background(), activeAgain.Account.ID); err != nil {
		t.Fatal(err)
	}
	assertScopedEventCount(t, pool, "sessions.revoked.v1", activeAgain.Account.ID, activeAgain.Account.ID, 1)
	if _, err := absoluteService.AuthenticateSession(context.Background(), activeAgain.SessionToken); !errors.Is(err, auth.ErrAuthenticationRequired) {
		t.Fatalf("all-device-revoked session authentication error = %v", err)
	}
}

func assertScopedEventCount(t *testing.T, pool *pgxpool.Pool, eventType, userID, entityID string, want int) {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM client_events
		WHERE event_type=$1 AND scope='user' AND subject_user_id=$2 AND entity_id=$3`, eventType, userID, entityID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("%s event count = %d, want %d", eventType, count, want)
	}
}

func provisionOwner(t *testing.T, pool *pgxpool.Pool) (*authtest.Clock, *auth.Secrets, identity.BootstrapResult) {
	t.Helper()
	clock := authtest.NewClock(time.Date(2026, time.August, 30, 9, 0, 0, 0, time.UTC))
	weakHasher, err := auth.NewPasswordHasher(authtest.NewRandomReader(0x31), auth.Argon2Params{
		Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	pattern := make([]byte, 67)
	for index := range pattern {
		pattern[index] = byte(index + 1)
	}
	secrets, err := auth.NewSecrets(bytes.Repeat([]byte{0x32}, 32), authtest.NewRandomReader(pattern...))
	if err != nil {
		t.Fatal(err)
	}
	identityService, err := identity.NewService(identity.ServiceDependencies{
		Repository: identity.NewRepository(pool), Passwords: weakHasher, Secrets: secrets, Now: clock.Now,
		SetupTTL: 15 * time.Minute, OwnerIdleTimeout: 8 * time.Hour, SessionAbsoluteTimeout: 30 * 24 * time.Hour,
		EODHDValidator:   eodhdValidatorFunc(func(context.Context, string) error { return nil }),
		CredentialCipher: ownerTestCredentialCipher(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	setup, err := identityService.IssueSetupCapability(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result, err := identityService.BootstrapOwner(context.Background(), identity.BootstrapRequest{
		Capability: setup.Token, Email: "owner@example.com", Password: "correct horse battery staple",
		DisplayName: "Market Owner", DeviceLabel: "Bootstrap browser", Origin: "192.0.2.0/24",
		EODHDAPIKey: "owner-test-eodhd-key",
		SMTP:        identity.SMTPSetupConfiguration{Host: "smtp.example.test", Port: 587, From: "access@example.test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return clock, secrets, result
}

type eodhdValidatorFunc func(context.Context, string) error

func (function eodhdValidatorFunc) ValidateCredential(ctx context.Context, key string) error {
	return function(ctx, key)
}

func ownerTestCredentialCipher(t *testing.T) *credentials.Cipher {
	t.Helper()
	cipher, err := credentials.NewCipher(bytes.Repeat([]byte{0x33}, 32), 1)
	if err != nil {
		t.Fatal(err)
	}
	return cipher
}

func newOwnerAuthService(t *testing.T, pool *pgxpool.Pool, clock *authtest.Clock, secrets *auth.Secrets, iterations uint32, idle, absolute time.Duration) (*auth.Service, *auth.PasswordHasher) {
	t.Helper()
	hasher, err := auth.NewPasswordHasher(authtest.NewRandomReader(0x41), auth.Argon2Params{
		Memory: 8 * 1024, Iterations: iterations, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := auth.NewService(auth.ServiceDependencies{
		Repository: auth.NewRepository(pool), Passwords: hasher, Secrets: secrets, Now: clock.Now,
		OwnerIdleTimeout: idle, SessionAbsoluteTimeout: absolute,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, hasher
}
