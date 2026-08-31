package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/db"
	appmail "market-lens/server/internal/mail"
	"market-lens/server/internal/mail/mailtest"
	"market-lens/server/internal/testdb"
)

// TestRevalidateSessionEndsAStreamOnRevocationDeactivationAndExpiry covers the check an already
// open event stream repeats. Authentication happened once, at connect; everything that can take
// access away afterwards has to be visible to this call.
func TestRevalidateSessionEndsAStreamOnRevocationDeactivationAndExpiry(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	clock, secrets, _ := provisionOwner(t, pool)
	start := clock.Now().Add(time.Hour)
	clock.Set(start)
	member := provisionMember(t, pool, "10000000-0000-4000-8000-000000000801", "stream@example.com", "Stream Member", start)
	service, _ := newMemberAuthService(t, pool, clock, secrets, mailtest.NewCapture[appmail.Message]())
	repository := auth.NewRepository(pool)

	sessionID := "20000000-0000-4000-8000-000000000801"
	session := memberSession(t, secrets, sessionID, member, start)
	if err := repository.IssueMemberChallenge(ctx, auth.IssueMemberChallengeParams{
		ChallengeID: "70000000-0000-4000-8000-000000000801", DeliveryID: "70000000-0000-4000-8000-000000000802",
		UserID: member, Email: "stream@example.com",
		CodeDigest: secrets.Digest(auth.PurposeMemberCode, "654321"),
		CreatedAt:  start, ExpiresAt: start.Add(auth.MemberCodeTTL),
		OriginDigest: secrets.Digest(auth.PurposeOrigin, "203.0.113.0/24"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.VerifyMemberChallenge(ctx, auth.VerifyMemberChallengeParams{
		UserID: member, CodeDigest: secrets.Digest(auth.PurposeMemberCode, "654321"),
		Session: session, Now: start.Add(time.Minute),
		OriginDigest: secrets.Digest(auth.PurposeOrigin, "203.0.113.0/24"),
	}); err != nil {
		t.Fatal(err)
	}

	clock.Set(start.Add(2 * time.Minute))
	if err := service.RevalidateSession(ctx, sessionID); err != nil {
		t.Fatalf("an active session was refused: %v", err)
	}
	// An unknown session identifier is refused without saying anything more.
	if err := service.RevalidateSession(ctx, "20000000-0000-4000-8000-0000000008ff"); !errors.Is(err, auth.ErrAuthenticationRequired) {
		t.Fatalf("unknown session = %v, want a refusal", err)
	}

	// Deactivating the account behind the stream ends it, even though the session row is intact.
	if _, err := pool.Exec(ctx, `UPDATE users SET status='deactivated',deactivated_at=$2 WHERE id=$1`,
		member, start.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := service.RevalidateSession(ctx, sessionID); !errors.Is(err, auth.ErrAuthenticationRequired) {
		t.Fatalf("deactivated account = %v, want a refusal", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET status='active',deactivated_at=NULL WHERE id=$1`, member); err != nil {
		t.Fatal(err)
	}

	// Revoking the session ends it too.
	if err := repository.RevokeSession(ctx, member, sessionID, start.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := service.RevalidateSession(ctx, sessionID); !errors.Is(err, auth.ErrAuthenticationRequired) {
		t.Fatalf("revoked session = %v, want a refusal", err)
	}

	// Revalidation is a read: it must not extend the idle window and keep a stream alive
	// forever on a connection nobody is using.
	var idleBefore time.Time
	if err := pool.QueryRow(ctx, `SELECT idle_expires_at FROM sessions WHERE id=$1`, sessionID).Scan(&idleBefore); err != nil {
		t.Fatal(err)
	}
	clock.Set(start.Add(10 * time.Minute))
	_ = service.RevalidateSession(ctx, sessionID)
	var idleAfter time.Time
	if err := pool.QueryRow(ctx, `SELECT idle_expires_at FROM sessions WHERE id=$1`, sessionID).Scan(&idleAfter); err != nil {
		t.Fatal(err)
	}
	if !idleAfter.Equal(idleBefore) {
		t.Fatalf("revalidation extended the idle window from %s to %s", idleBefore, idleAfter)
	}
}
