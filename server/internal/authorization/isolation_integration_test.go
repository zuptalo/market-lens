package authorization_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/authtest"
	"market-lens/server/internal/db"
	"market-lens/server/internal/events"
	"market-lens/server/internal/identity"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	ownerID   = "10000000-0000-4000-8000-0000000000b0"
	memberAID = "10000000-0000-4000-8000-0000000000b1"
	memberBID = "10000000-0000-4000-8000-0000000000b2"
	// guessedID is a well-formed UUID that names nothing. A caller who guesses an identifier
	// must learn no more from the response than a caller who guesses a wrong one.
	guessedID = "10000000-0000-4000-8000-00000000dead"
)

// isolationFixture is two members and one owner, each with private sessions and events, plus the
// shared market data both may read. It is the smallest world in which cross-user disclosure is
// observable at all.
type isolationFixture struct {
	pool     *pgxpool.Pool
	sessions *auth.Repository
	identity *identity.Repository
	events   *events.Repository
	start    time.Time
}

func newIsolationFixture(t *testing.T) isolationFixture {
	t.Helper()
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, time.August, 30, 9, 0, 0, 0, time.UTC)
	seedUser(t, pool, ownerID, "owner@example.com", "Owner", "owner", start)
	seedUser(t, pool, memberAID, "member-a@example.com", "Member A", "member", start)
	seedUser(t, pool, memberBID, "member-b@example.com", "Member B", "member", start)
	return isolationFixture{
		pool: pool, sessions: auth.NewRepository(pool), identity: identity.NewRepository(pool),
		events: events.NewRepository(pool), start: start,
	}
}

func seedUser(t *testing.T, pool *pgxpool.Pool, id, email, displayName, role string, at time.Time) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `INSERT INTO users
		(id,email,normalized_email,display_name,role,status,email_verified_at,created_at,updated_at)
		VALUES ($1,$2,lower($2),$3,$4,'active',$5,$5,$5)`, id, email, displayName, role, at); err != nil {
		t.Fatal(err)
	}
}

// seedSession writes one active session directly, because this test is about who may read a
// session, not about how sign-in mints one.
func seedSession(t *testing.T, fixture isolationFixture, sessionID, userID, label string) {
	t.Helper()
	secrets, err := auth.NewSecrets(bytes.Repeat([]byte{0xb4}, 32), authtest.NewRandomReader(0x11))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(context.Background(), `INSERT INTO sessions
		(id,user_id,token_digest,csrf_digest,created_at,last_seen_at,idle_expires_at,absolute_expires_at,
		 device_label,origin_digest)
		VALUES ($1,$2,$3,$4,$5,$5,$6,$7,$8,$9)`,
		sessionID, userID, secrets.Digest(auth.PurposeSession, sessionID),
		secrets.Digest(auth.PurposeCSRF, sessionID), fixture.start,
		fixture.start.Add(2*time.Hour), fixture.start.Add(30*24*time.Hour), label,
		secrets.Digest(auth.PurposeOrigin, "198.51.100.0/24")); err != nil {
		t.Fatal(err)
	}
}

// seedEvent writes one durable client event in the given scope.
func seedEvent(t *testing.T, fixture isolationFixture, eventType, scope, subject, entityID string, offset time.Duration) {
	t.Helper()
	var subjectValue any
	if subject != "" {
		subjectValue = subject
	}
	if _, err := fixture.pool.Exec(context.Background(), `INSERT INTO client_events
		(event_type,version,scope,subject_user_id,entity_type,entity_id,payload,occurred_at)
		VALUES ($1,1,$2,$3,'fixture',$4,'{}'::jsonb,$5)`,
		eventType, scope, subjectValue, entityID, fixture.start.Add(offset)); err != nil {
		t.Fatal(err)
	}
}

func TestOneMemberNeverReachesAnotherMembersPrivateRecords(t *testing.T) {
	fixture := newIsolationFixture(t)
	ctx := context.Background()
	seedSession(t, fixture, "20000000-0000-4000-8000-0000000000b1", memberAID, "A phone")
	seedSession(t, fixture, "20000000-0000-4000-8000-0000000000b2", memberBID, "B laptop")

	// Lists: each member's own listing is complete and contains nothing of the other's.
	listA, err := fixture.sessions.ListSessions(ctx, memberAID)
	if err != nil {
		t.Fatal(err)
	}
	listB, err := fixture.sessions.ListSessions(ctx, memberBID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listA) != 1 || len(listB) != 1 {
		t.Fatalf("session counts = %d and %d, want one each", len(listA), len(listB))
	}
	// Aggregates leak existence just as readily as records do, so the count is asserted too.
	if listA[0].UserID != memberAID || listB[0].UserID != memberBID {
		t.Fatalf("session listings crossed users: %s and %s", listA[0].UserID, listB[0].UserID)
	}
	for _, session := range listA {
		if strings.Contains(session.DeviceLabel, "B ") {
			t.Fatalf("member A read member B's device label: %q", session.DeviceLabel)
		}
	}

	// A correctly guessed identifier belonging to another member must behave exactly like an
	// identifier that names nothing at all.
	guessedError := fixture.sessions.RevokeSession(ctx, memberAID, "20000000-0000-4000-8000-0000000000b2", fixture.start.Add(time.Minute))
	unknownError := fixture.sessions.RevokeSession(ctx, memberAID, guessedID, fixture.start.Add(time.Minute))
	if !errors.Is(guessedError, auth.ErrAuthenticationRequired) || !errors.Is(unknownError, auth.ErrAuthenticationRequired) {
		t.Fatalf("guessed=%v unknown=%v, want the same scoped refusal", guessedError, unknownError)
	}
	survivors, err := fixture.sessions.ListSessions(ctx, memberBID)
	if err != nil {
		t.Fatal(err)
	}
	if len(survivors) != 1 || survivors[0].RevokedAt != nil {
		t.Fatalf("member A revoked member B's session: %#v", survivors)
	}

	// Account lookups are scoped the same way: another user's identifier discloses nothing.
	if _, err := fixture.sessions.Account(ctx, guessedID); !errors.Is(err, auth.ErrAuthenticationRequired) {
		t.Fatalf("unknown account lookup = %v, want a scoped refusal", err)
	}
}

func TestPrivateEventReplayIsScopedToItsSubjectAndTheOwnerNeverSeesIt(t *testing.T) {
	fixture := newIsolationFixture(t)
	ctx := context.Background()
	seedEvent(t, fixture, "daily_bar.changed.v1", "shared", "", "bar-1", 0)
	seedEvent(t, fixture, "session.created.v1", "user", memberAID, "session-a", time.Second)
	seedEvent(t, fixture, "session.created.v1", "user", memberBID, "session-b", 2*time.Second)
	seedEvent(t, fixture, "member.changed.v1", "owner", "", "member-a", 3*time.Second)

	feeds := map[string][]events.Event{}
	for name, audience := range map[string]events.Audience{
		"memberA": {UserID: memberAID, Role: "member"},
		"memberB": {UserID: memberBID, Role: "member"},
		"owner":   {UserID: ownerID, Role: "owner"},
	} {
		feed, err := fixture.events.ListAuthorized(ctx, audience, 0, 100)
		if err != nil {
			t.Fatalf("%s replay: %v", name, err)
		}
		feeds[name] = feed
	}

	// Shared market data reaches everyone; private activity reaches only its subject.
	for name, feed := range feeds {
		if !hasEntity(feed, "bar-1") {
			t.Fatalf("%s did not receive the shared market event: %#v", name, feed)
		}
	}
	if !hasEntity(feeds["memberA"], "session-a") || hasEntity(feeds["memberA"], "session-b") {
		t.Fatalf("member A's feed is misscoped: %#v", feeds["memberA"])
	}
	if !hasEntity(feeds["memberB"], "session-b") || hasEntity(feeds["memberB"], "session-a") {
		t.Fatalf("member B's feed is misscoped: %#v", feeds["memberB"])
	}
	// Owning the instance is administrative authority, not a window into private activity.
	if hasEntity(feeds["owner"], "session-a") || hasEntity(feeds["owner"], "session-b") {
		t.Fatalf("the owner replayed a member's private events: %#v", feeds["owner"])
	}
	if !hasEntity(feeds["owner"], "member-a") {
		t.Fatalf("the owner is missing owner-scoped administration events: %#v", feeds["owner"])
	}
	if hasEntity(feeds["memberA"], "member-a") || hasEntity(feeds["memberB"], "member-a") {
		t.Fatal("a member replayed owner-scoped administration metadata")
	}
}

func TestReplayIsKeyedByAudienceSoIdenticalCursorsCannotShareResults(t *testing.T) {
	fixture := newIsolationFixture(t)
	ctx := context.Background()
	seedEvent(t, fixture, "session.created.v1", "user", memberAID, "session-a", time.Second)
	seedEvent(t, fixture, "session.created.v1", "user", memberBID, "session-b", 2*time.Second)

	// The same cursor and limit for two audiences must not be able to serve one another's rows,
	// which is what a cache keyed on the cursor alone would do.
	first, err := fixture.events.ListAuthorized(ctx, events.Audience{UserID: memberAID, Role: "member"}, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixture.events.ListAuthorized(ctx, events.Audience{UserID: memberBID, Role: "member"}, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range first {
		for _, b := range second {
			if a.ID == b.ID && a.Scope != "shared" {
				t.Fatalf("private event %d was served to both audiences", a.ID)
			}
		}
	}

	// A deactivated member keeps no replay access, even with a previously valid cursor.
	deactivated, err := fixture.events.ListAuthorized(ctx,
		events.Audience{UserID: memberAID, Role: "member", Deactivated: true}, 0, 100)
	if err == nil && len(deactivated) > 0 {
		t.Fatalf("a deactivated member replayed %d events", len(deactivated))
	}
}

func TestAdministrationQueriesRefuseANonOwnerScopeAtThePersistenceBoundary(t *testing.T) {
	fixture := newIsolationFixture(t)
	ctx := context.Background()
	member := identity.Actor{UserID: memberAID, Role: identity.RoleMember}
	owner := identity.Actor{UserID: ownerID, Role: identity.RoleOwner}

	// Authorization belongs in the query layer too. A handler or service that forgets to check
	// must not be the only thing standing between a member and the administration tables.
	if page, err := fixture.identity.ListMembers(ctx, member, "", 50, fixture.start); err == nil && len(page.Members) > 0 {
		t.Fatalf("a member listed %d administration records", len(page.Members))
	}
	if _, err := fixture.identity.Member(ctx, member, memberBID, fixture.start); err == nil {
		t.Fatal("a member read another member's administration metadata")
	}
	if page, err := fixture.identity.ListInvitations(ctx, member, "", 50, fixture.start); err == nil && len(page.Items) > 0 {
		t.Fatalf("a member listed %d invitations", len(page.Items))
	}

	// The owner still reaches exactly what administration requires.
	page, err := fixture.identity.ListMembers(ctx, owner, "", 50, fixture.start)
	if err != nil {
		t.Fatalf("owner member listing: %v", err)
	}
	if len(page.Members) == 0 {
		t.Fatal("owner member listing is empty")
	}
	for _, record := range page.Members {
		if record.ID == ownerID {
			continue
		}
		// Administration metadata is account and security state only.
		if record.Email == "" || record.Status == "" {
			t.Fatalf("owner administration record is incomplete: %#v", record)
		}
	}
}

func hasEntity(feed []events.Event, entityID string) bool {
	for _, event := range feed {
		if event.EntityID == entityID {
			return true
		}
	}
	return false
}
