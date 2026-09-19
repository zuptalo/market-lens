package db_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"market-lens/server/internal/db"
	"market-lens/server/internal/testdb"
)

// What notification tables must never grow.
//
// The first five are engagement tracking, excluded by FR-027: a product that records whether
// somebody opened a message has started measuring them. The last two are device fingerprinting — a
// push needs neither to be delivered, and a subscription list would otherwise quietly accumulate a
// record of where somebody reads their mail.
var forbiddenNotificationColumns = []string{
	"opened_at", "clicked_at", "read_at", "tracking_id", "campaign",
	"user_agent", "ip_address",
}

var notificationTables = []string{
	"instance_push_key", "notification_preferences", "notification_quiet_hours",
	"push_subscriptions", "notifications",
}

func TestNotificationsArriveOnACleanInstall(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, table := range notificationTables {
		var present bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&present); err != nil {
			t.Fatal(err)
		}
		if !present {
			t.Fatalf("%s does not exist", table)
		}
		var rows int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		// Nothing is seeded, including preferences: a seeded row would look like a decision had
		// been made on somebody's behalf, and the absence of a row means the same as `false`.
		if rows != 0 {
			t.Errorf("%s arrived with %d rows", table, rows)
		}
	}

	// Ownership is read from the row itself on every person-owned table.
	for _, table := range []string{"notification_preferences", "notification_quiet_hours",
		"push_subscriptions", "notifications"} {
		var nullable string
		if err := pool.QueryRow(ctx, `SELECT is_nullable FROM information_schema.columns
			WHERE table_name = $1 AND column_name = 'user_id'`, table).Scan(&nullable); err != nil {
			t.Fatalf("%s has no user_id: %v", table, err)
		}
		if nullable != "NO" {
			t.Errorf("%s.user_id is nullable", table)
		}
	}

	var forbidden int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns
		WHERE table_name = ANY($1) AND column_name = ANY($2)`,
		notificationTables, forbiddenNotificationColumns).Scan(&forbidden); err != nil {
		t.Fatal(err)
	}
	if forbidden != 0 {
		t.Errorf("%d columns exist that this feature exists to not have", forbidden)
	}
}

// TestThereIsExactlyOnePushKeyForever. The same shape as the instance signing key, and for a worse
// reason: rotating a VAPID key invalidates every subscription in existence, and the failure is
// silent — pushes simply stop arriving.
func TestThereIsExactlyOnePushKeyForever(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	insert := func() error {
		_, err := pool.Exec(ctx, `INSERT INTO instance_push_key
			(id, private_key, public_key, subject)
			VALUES (gen_random_uuid(), repeat('a', 32)::bytea, repeat('b', 65)::bytea,
			        'mailto:owner@example.com')`)
		return err
	}
	if err := insert(); err != nil {
		t.Fatalf("provision: %v", err)
	}
	if err := insert(); err == nil {
		t.Errorf("a second push key was stored; every subscription is signed against one")
	}

	// And the shapes are checked, because a key of the wrong length fails at send time with an
	// error nobody can read.
	if _, err := pool.Exec(ctx, `UPDATE instance_push_key SET private_key = 'short'::bytea`); err == nil {
		t.Errorf("a private key of the wrong length was accepted")
	}
}

// TestAPreferenceExistsOncePerKindPerChannel, and only for kinds this product actually has.
func TestAPreferenceExistsOncePerKindPerChannel(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	user := seedUserForNotifications(t, pool)

	set := func(kind, channel string) error {
		_, err := pool.Exec(ctx, `INSERT INTO notification_preferences (user_id, kind, channel, enabled)
			VALUES ($1, $2, $3, true)`, user, kind, channel)
		return err
	}
	if err := set("decision_waiting", "email"); err != nil {
		t.Fatalf("set a preference: %v", err)
	}
	if err := set("decision_waiting", "email"); err == nil {
		t.Errorf("one kind and channel was stored twice for one person")
	}
	for _, unknown := range []struct{ kind, channel string }{
		{"everything", "email"},
		{"decision_waiting", "sms"},
		{"decision_waiting", "webhook"},
	} {
		if err := set(unknown.kind, unknown.channel); err == nil {
			t.Errorf("%s on %s was accepted", unknown.kind, unknown.channel)
		}
	}
}

// TestASentNotificationCarriesWhenItWasSent. Half a state is worse than either: a row that says
// sent with no moment cannot be reconciled against anything.
func TestASentNotificationCarriesWhenItWasSent(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	user := seedUserForNotifications(t, pool)

	raise := func(state string, sentAt any) error {
		_, err := pool.Exec(ctx, `INSERT INTO notifications
			(id, user_id, kind, channel, subject_key, state, sent_at)
			VALUES (gen_random_uuid(), $1, 'paper_fill', 'email', 'order', $2, $3)`,
			user, state, sentAt)
		return err
	}
	if err := raise("sent", nil); err == nil {
		t.Errorf("a notification says it was sent and does not say when")
	}
	if err := raise("pending", "2026-09-18T09:00:00Z"); err == nil {
		t.Errorf("a pending notification carries a moment it was sent")
	}
	if err := raise("sent", "2026-09-18T09:00:00Z"); err != nil {
		t.Errorf("an ordinary sent notification was refused: %v", err)
	}
	if err := raise("pending", nil); err != nil {
		t.Errorf("an ordinary pending notification was refused: %v", err)
	}
	if err := raise("delivered", nil); err == nil {
		t.Errorf("an unknown state was accepted")
	}
}

func seedUserForNotifications(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	const id = "70000000-0027-4000-8000-000000000001"
	mustExec(t, context.Background(), pool, `INSERT INTO users
		(id, email, normalized_email, display_name, role, status, email_verified_at, created_at, updated_at)
		VALUES ($1, 'notify@example.com', 'notify@example.com', 'Notify', 'owner', 'active', now(), now(), now())`,
		id)
	return id
}

// TestTheDueIndexMatchesTheQueryThatReadsIt.
//
// The pass asks for pending and failed together, because a failed notification is one waiting for
// its next attempt rather than one that has stopped. An index covering only 'pending' left every
// pass scanning the table — invisible while delivery ran nightly, and not invisible now that it
// runs every minute.
func TestTheDueIndexMatchesTheQueryThatReadsIt(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	user := seedUserForNotifications(t, pool)

	// Enough rows that the planner has a reason to prefer an index.
	mustExec(t, ctx, pool, `INSERT INTO notifications
		(id, user_id, kind, channel, subject_key, state, available_at)
		SELECT gen_random_uuid(), $1, 'paper_fill', 'email', 'order-' || n,
		       CASE WHEN n % 3 = 0 THEN 'failed' WHEN n % 3 = 1 THEN 'pending' ELSE 'sent' END,
		       now() - (n || ' minutes')::interval
		FROM generate_series(1, 4000) n
		WHERE n % 3 <> 2`, user)
	mustExec(t, ctx, pool, `ANALYZE notifications`)

	// EXPLAIN answers a row per line, and the node that matters is rarely the first.
	rows, err := pool.Query(ctx, `EXPLAIN (FORMAT TEXT)
		SELECT id FROM notifications
		WHERE state IN ('pending', 'failed') AND available_at <= now()
		ORDER BY available_at LIMIT 500`)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		plan.WriteString(line + "\n")
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), "notifications_due_idx") {
		t.Errorf("the delivery query does not use its own index:\n%s", plan.String())
	}
}

// TestAVersionIsRecordedOnceHoweverManyPodsSeeIt.
//
// A deployment notification has to fire once per version, not once per process start. Keel rolls
// pods and a crash loop restarts them, so "tell somebody when the version changed" has to be a
// claim about the version rather than about this instance having booted.
//
// The primary key is what makes that true without coordination: whichever pod inserts first is the
// one that announces, and the others find the row already there.
func TestAVersionIsRecordedOnceHoweverManyPodsSeeIt(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	claim := func(version, summary string) int64 {
		tag, err := pool.Exec(ctx, `INSERT INTO deployed_versions (version, summary)
			VALUES ($1, $2) ON CONFLICT DO NOTHING`, version, summary)
		if err != nil {
			t.Fatalf("record %s: %v", version, err)
		}
		return tag.RowsAffected()
	}

	if claim("0.23.4", "fix(notify): something") != 1 {
		t.Errorf("the first pod to see a version did not claim it")
	}
	if claim("0.23.4", "fix(notify): something") != 0 {
		t.Errorf("a second pod claimed a version that was already recorded")
	}
	// A different version is a different claim — including a rollback, which is worth being told
	// about for exactly the same reason an upgrade is.
	if claim("0.23.3", "the previous one") != 1 {
		t.Errorf("a rollback was not treated as a change")
	}

	var recorded int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM deployed_versions`).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded != 2 {
		t.Errorf("%d versions recorded, want 2", recorded)
	}

	// A version with nothing to say about it is still a version.
	if _, err := pool.Exec(ctx, `INSERT INTO deployed_versions (version) VALUES ('0.23.5')`); err != nil {
		t.Errorf("a version with no summary was refused: %v", err)
	}
	// An empty version is not.
	if _, err := pool.Exec(ctx, `INSERT INTO deployed_versions (version) VALUES ('')`); err == nil {
		t.Errorf("an empty version was recorded")
	}
}
