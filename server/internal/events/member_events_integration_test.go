package events_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/authtest"
	"market-lens/server/internal/db"
	"market-lens/server/internal/events"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMemberAuthenticationEventsAreScopedAndCarryNoSecrets(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	secrets, err := auth.NewSecrets(bytes.Repeat([]byte{0xa1}, 32), authtest.NewRandomReader(0x07))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, time.August, 30, 10, 0, 0, 0, time.UTC)
	owner := seedEventUser(t, pool, "10000000-0000-4000-8000-0000000000a1", "owner@example.com", "Owner", "owner", start)
	member := seedEventUser(t, pool, "10000000-0000-4000-8000-0000000000a2", "member@example.com", "Member", "member", start)
	repository := auth.NewRepository(pool)
	origin := secrets.Digest(auth.PurposeOrigin, "203.0.113.0/24")

	// A successful member sign-in publishes a user-scoped session event.
	if err := repository.IssueMemberChallenge(ctx, auth.IssueMemberChallengeParams{
		ChallengeID: "70000000-0000-4000-8000-000000000001", DeliveryID: "70000000-0000-4000-8000-000000000002",
		UserID: member, Email: "member@example.com", CodeDigest: secrets.Digest(auth.PurposeMemberCode, "424242"),
		CreatedAt: start, ExpiresAt: start.Add(auth.MemberCodeTTL), OriginDigest: origin,
	}); err != nil {
		t.Fatal(err)
	}
	session := auth.Session{
		ID: "20000000-0000-4000-8000-0000000000a2", UserID: member,
		TokenDigest: secrets.Digest(auth.PurposeSession, "member-session"),
		CSRFDigest:  secrets.Digest(auth.PurposeCSRF, "member-csrf"),
		CreatedAt:   start, LastSeenAt: start, IdleExpiresAt: start.Add(2 * time.Hour),
		AbsoluteExpiresAt: start.Add(30 * 24 * time.Hour), DeviceLabel: "Member device", OriginDigest: origin,
	}
	if _, err := repository.VerifyMemberChallenge(ctx, auth.VerifyMemberChallengeParams{
		UserID: member, CodeDigest: secrets.Digest(auth.PurposeMemberCode, "424242"),
		Session: session, Now: start.Add(time.Minute), OriginDigest: origin,
	}); err != nil {
		t.Fatal(err)
	}

	// The owner unlock publishes an owner-scoped administration event.
	if err := repository.UnlockMember(ctx, owner, member, start.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}

	reader := events.NewService(events.NewRepository(pool))
	memberFeed, err := reader.ListAuthorized(ctx, events.Audience{UserID: member, Role: "member"}, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	ownerFeed, err := reader.ListAuthorized(ctx, events.Audience{UserID: owner, Role: "owner"}, 0, 100)
	if err != nil {
		t.Fatal(err)
	}

	if !containsEvent(memberFeed, "session.created.v1") {
		t.Fatalf("member feed missing its own session event: %#v", memberFeed)
	}
	if containsEvent(memberFeed, "member.changed.v1") {
		t.Fatal("a member replayed an owner-scoped administration event")
	}
	if !containsEvent(ownerFeed, "member.changed.v1") {
		t.Fatalf("owner feed missing the member administration event: %#v", ownerFeed)
	}

	// No published payload may carry the code, the session token, or any digest.
	for _, event := range append(append([]events.Event{}, memberFeed...), ownerFeed...) {
		payload := string(event.Payload)
		for _, secret := range []string{"424242", "member-session", "member-csrf"} {
			if strings.Contains(payload, secret) {
				t.Fatalf("event %s payload disclosed %q: %s", event.Type, secret, payload)
			}
		}
		if payload != "{}" && strings.Contains(payload, "digest") {
			t.Fatalf("event %s payload carried digest material: %s", event.Type, payload)
		}
	}
}

func containsEvent(feed []events.Event, eventType string) bool {
	for _, event := range feed {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

func seedEventUser(t *testing.T, pool *pgxpool.Pool, id, email, displayName, role string, createdAt time.Time) string {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `INSERT INTO users
		(id,email,normalized_email,display_name,role,status,email_verified_at,created_at,updated_at)
		VALUES ($1,$2,lower($2),$3,$4,'active',$5,$5,$5)`, id, email, displayName, role, createdAt); err != nil {
		t.Fatal(err)
	}
	return id
}
