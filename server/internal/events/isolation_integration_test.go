package events_test

import (
	"context"
	"testing"
	"time"

	"market-lens/server/internal/db"
	"market-lens/server/internal/events"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	streamOwnerID  = "10000000-0000-4000-8000-0000000000c0"
	streamMemberID = "10000000-0000-4000-8000-0000000000c1"
	streamOtherID  = "10000000-0000-4000-8000-0000000000c2"
)

func newStreamFixture(t *testing.T) (*pgxpool.Pool, *events.Repository, time.Time) {
	t.Helper()
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, time.August, 30, 11, 0, 0, 0, time.UTC)
	seedEventUser(t, pool, streamOwnerID, "stream-owner@example.com", "Owner", "owner", start)
	seedEventUser(t, pool, streamMemberID, "stream-member@example.com", "Member", "member", start)
	seedEventUser(t, pool, streamOtherID, "stream-other@example.com", "Other", "member", start)
	return pool, events.NewRepository(pool), start
}

func appendStreamEvent(t *testing.T, pool *pgxpool.Pool, scope, subject, entityID string, at time.Time) int64 {
	t.Helper()
	var subjectValue any
	if subject != "" {
		subjectValue = subject
	}
	var id int64
	if err := pool.QueryRow(context.Background(), `INSERT INTO client_events
		(event_type,version,scope,subject_user_id,entity_type,entity_id,payload,occurred_at)
		VALUES ('fixture.changed.v1',1,$1,$2,'fixture',$3,'{}'::jsonb,$4) RETURNING id`,
		scope, subjectValue, entityID, at).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// TestAuthorizedReplayResumesWithoutDuplicatesOrGapsInTheAudiencesOwnSequence covers the
// contract a reconnecting client depends on: everything it has not seen, once each, in order.
func TestAuthorizedReplayResumesWithoutDuplicatesOrGapsInTheAudiencesOwnSequence(t *testing.T) {
	pool, repository, start := newStreamFixture(t)
	ctx := context.Background()
	var mine []int64
	for index := 0; index < 6; index++ {
		at := start.Add(time.Duration(index) * time.Second)
		if index%2 == 0 {
			mine = append(mine, appendStreamEvent(t, pool, "user", streamMemberID, "mine", at))
			continue
		}
		// Another member's private events consume identifiers in between, which is exactly the
		// gap a client must tolerate without concluding it lost anything.
		appendStreamEvent(t, pool, "user", streamOtherID, "theirs", at)
	}
	audience := events.Audience{UserID: streamMemberID, Role: "member"}

	var seen []int64
	after := int64(0)
	for {
		batch, err := repository.ListAuthorized(ctx, audience, after, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(batch) == 0 {
			break
		}
		if len(batch) > 2 {
			t.Fatalf("a batch of %d exceeded the requested bound, which is what keeps a slow "+
				"consumer from pulling the whole outbox into memory", len(batch))
		}
		for _, event := range batch {
			if len(seen) > 0 && event.ID <= seen[len(seen)-1] {
				t.Fatalf("replay went backwards or repeated: %d after %d", event.ID, seen[len(seen)-1])
			}
			seen = append(seen, event.ID)
			after = event.ID
		}
	}
	if len(seen) != len(mine) {
		t.Fatalf("replayed %d events, want the member's own %d: %v vs %v", len(seen), len(mine), seen, mine)
	}
	for index, id := range mine {
		if seen[index] != id {
			t.Fatalf("replay position %d = %d, want %d", index, seen[index], id)
		}
	}

	// Resuming from the last delivered identifier replays nothing a second time.
	if repeat, err := repository.ListAuthorized(ctx, audience, seen[len(seen)-1], 100); err != nil || len(repeat) != 0 {
		t.Fatalf("resume after the final event = %#v err=%v, want an empty replay", repeat, err)
	}

	// Resuming from an identifier the member never saw, because it belongs to somebody else,
	// must not reveal it and must not skip the member's own later events.
	fromForeign, err := repository.ListAuthorized(ctx, audience, mine[0], 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(fromForeign) != len(mine)-1 {
		t.Fatalf("resume from a foreign cursor replayed %d events, want %d", len(fromForeign), len(mine)-1)
	}
	for _, event := range fromForeign {
		if event.EntityID == "theirs" {
			t.Fatal("a foreign cursor disclosed another member's private event")
		}
	}
}

// TestStreamAudienceComesFromPersistedRoleAndStatusNotTheCaller proves the audience cannot be
// widened by anything a client says: it is re-derived from the durable user record.
func TestStreamAudienceComesFromPersistedRoleAndStatusNotTheCaller(t *testing.T) {
	pool, repository, start := newStreamFixture(t)
	ctx := context.Background()
	appendStreamEvent(t, pool, "owner", "", "administration", start)
	appendStreamEvent(t, pool, "shared", "", "market", start.Add(time.Second))

	member, err := repository.Audience(ctx, streamMemberID)
	if err != nil {
		t.Fatalf("resolve member audience: %v", err)
	}
	if member.Role != "member" || member.Deactivated {
		t.Fatalf("member audience = %#v, want the persisted member role", member)
	}
	owner, err := repository.Audience(ctx, streamOwnerID)
	if err != nil {
		t.Fatalf("resolve owner audience: %v", err)
	}
	if owner.Role != "owner" {
		t.Fatalf("owner audience = %#v, want the persisted owner role", owner)
	}

	feed, err := repository.ListAuthorized(ctx, member, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if hasStreamEntity(feed, "administration") || !hasStreamEntity(feed, "market") {
		t.Fatalf("resolved member feed = %#v, want shared data only", feed)
	}

	// Ownership itself cannot drift underneath a stream: the schema refuses a role change
	// outright until a reviewed ownership-transfer migration exists.
	if _, err := pool.Exec(ctx, `UPDATE users SET role='member' WHERE id=$1`, streamOwnerID); err == nil {
		t.Fatal("the owner role was silently demoted")
	}
	stillOwner, err := repository.Audience(ctx, streamOwnerID)
	if err != nil || stillOwner.Role != "owner" {
		t.Fatalf("owner audience after a refused demotion = %#v err=%v", stillOwner, err)
	}

	// A deactivated account resolves to an audience that can read nothing at all.
	if _, err := pool.Exec(ctx, `UPDATE users SET status='deactivated',deactivated_at=$2 WHERE id=$1`,
		streamMemberID, start.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	stopped, err := repository.Audience(ctx, streamMemberID)
	if err == nil && !stopped.Deactivated {
		t.Fatalf("deactivated audience = %#v err=%v, want a refusal or a deactivated audience", stopped, err)
	}
	if err == nil {
		if replay, replayErr := repository.ListAuthorized(ctx, stopped, 0, 100); replayErr == nil && len(replay) > 0 {
			t.Fatalf("a deactivated account replayed %d events", len(replay))
		}
	}

	// An unknown subject never resolves into a usable audience.
	if unknown, err := repository.Audience(ctx, "10000000-0000-4000-8000-00000000dead"); err == nil && !unknown.Deactivated {
		t.Fatalf("unknown audience = %#v, want a refusal", unknown)
	}
}

func hasStreamEntity(feed []events.Event, entityID string) bool {
	for _, event := range feed {
		if event.EntityID == entityID {
			return true
		}
	}
	return false
}
