package db_test

import (
	"context"
	"testing"

	"market-lens/server/internal/db"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestRevisedCountMigrationUpgradesAndConstrains covers the one thing a count of corrections has
// to get right on an existing installation: what it says about runs that predate it.
//
// Zero is the truthful answer rather than an unknown dressed as one, because those runs really
// did correct nothing — not because nobody counted, but because nothing ever asked the source to
// reconsider a session it had already answered for.
func TestRevisedCountMigrationUpgradesAndConstrains(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, table := range []string{"import_runs", "import_items"} {
		var dataType, nullable, defaultValue string
		if err := pool.QueryRow(ctx, `SELECT data_type, is_nullable, coalesce(column_default, '')
			FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = $1 AND column_name = 'revised_count'`,
			table).Scan(&dataType, &nullable, &defaultValue); err != nil {
			t.Fatalf("%s has no revised_count: %v", table, err)
		}
		if dataType != "bigint" || nullable != "NO" || defaultValue == "" {
			t.Errorf("%s.revised_count is %s nullable=%s default=%q", table, dataType, nullable, defaultValue)
		}
	}

	// A run that predates the column reads zero, not null.
	runID := "cccccccc-0016-4000-8000-000000000001"
	mustExec(t, ctx, pool, `INSERT INTO import_runs
		(id, kind, provider, status, started_at, finished_at, app_version, processed_count, accepted_count)
		VALUES ($1, 'daily_update', 'fixture', 'succeeded', now(), now(), 'test', 10, 10)`, runID)
	var revised *int64
	if err := pool.QueryRow(ctx, `SELECT revised_count FROM import_runs WHERE id = $1`, runID).Scan(&revised); err != nil {
		t.Fatalf("read revised_count: %v", err)
	}
	if revised == nil || *revised != 0 {
		t.Fatalf("a run that corrected nothing reads %v, wanted 0", revised)
	}

	// FR-010, tested on insert rather than update. A terminal run is immutable by trigger, so an
	// UPDATE is refused whatever the counts say — a test written that way would pass against a
	// schema with no constraint at all, which is the least useful kind of green.
	insertRun := func(id string, processed, revisedCount int64) error {
		_, err := pool.Exec(ctx, `INSERT INTO import_runs
			(id, kind, provider, status, started_at, finished_at, app_version,
			 processed_count, accepted_count, revised_count)
			VALUES ($1, 'daily_update', 'fixture', 'succeeded', now(), now(), 'test', $2, $2, $3)`,
			id, processed, revisedCount)
		return err
	}
	if err := insertRun("cccccccc-0016-4000-8000-000000000002", 10, 10); err != nil {
		t.Fatalf("a run that corrected every session it looked at was refused: %v", err)
	}
	if err := insertRun("cccccccc-0016-4000-8000-000000000003", 10, 11); err == nil {
		t.Fatalf("a run correcting more sessions than it processed was accepted")
	}
	if err := insertRun("cccccccc-0016-4000-8000-000000000004", 10, -1); err == nil {
		t.Fatalf("a negative corrected count was accepted")
	}

	// The same rules on the per-instrument item.
	insertItem := func(runID string, processed, revisedCount int64) error {
		var instrumentID string
		if err := pool.QueryRow(ctx, `SELECT id::text FROM instruments LIMIT 1`).Scan(&instrumentID); err != nil {
			t.Fatalf("find an instrument: %v", err)
		}
		_, err := pool.Exec(ctx, `INSERT INTO import_items
			(run_id, instrument_id, requested_from, requested_to, status,
			 processed_count, accepted_count, revised_count, started_at, finished_at)
			VALUES ($1, $2, '2026-08-25', '2026-08-31', 'succeeded', $3, $3, $4, now(), now())`,
			runID, instrumentID, processed, revisedCount)
		return err
	}
	if err := insertItem("cccccccc-0016-4000-8000-000000000002", 5, 2); err != nil {
		t.Fatalf("an item recording two corrections was refused: %v", err)
	}
	if err := insertItem("cccccccc-0016-4000-8000-000000000001", 5, 6); err == nil {
		t.Fatalf("an item correcting more sessions than it processed was accepted")
	}
}

func mustExec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("exec: %v\n%s", err, sql)
	}
}
