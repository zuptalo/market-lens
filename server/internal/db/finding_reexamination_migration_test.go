package db_test

import (
	"context"
	"testing"

	"market-lens/server/internal/db"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestFindingReexaminationMigration covers the one thing this migration must get right on an
// existing installation: what it says about findings nobody has re-examined yet.
//
// Nothing has asked those a second time, so recording that it had would be the product asserting
// something it never did. They stay exactly as they are and earn the state on the first pass that
// re-examines them — which is also what makes the first morning's screen an honest picture of the
// backlog rather than a migration's guess at one.
func TestFindingReexaminationMigration(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, column := range []string{"reexamined_at", "reexamining_run_id", "accepted_at", "accepted_by"} {
		var nullable string
		if err := pool.QueryRow(ctx, `SELECT is_nullable FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = 'data_quality_findings'
			  AND column_name = $1`, column).Scan(&nullable); err != nil {
			t.Fatalf("data_quality_findings has no %s: %v", column, err)
		}
		if nullable != "YES" {
			t.Errorf("%s is NOT NULL; a finding nobody has examined twice has no value for it", column)
		}
	}

	// A finding that predates the feature reads as open and unexamined, not as anything else.
	runID, instrumentID := seedFindingContext(t, ctx, pool)
	findingID := "dddddddd-0017-4000-8000-000000000001"
	mustExec(t, ctx, pool, `INSERT INTO data_quality_findings
		(id, instrument_id, session_date, run_id, rule, severity, disposition, detail, status, created_at)
		VALUES ($1, $2, '2017-04-13', $3, 'provider_gap', 'warning', 'rejected',
		        'the source has no bar for this session', 'open', now())`,
		findingID, instrumentID, runID)
	var status string
	var reexamined, accepted *string
	if err := pool.QueryRow(ctx, `SELECT status, reexamined_at::text, accepted_at::text
		FROM data_quality_findings WHERE id = $1`, findingID).Scan(&status, &reexamined, &accepted); err != nil {
		t.Fatalf("read the finding: %v", err)
	}
	if status != "open" || reexamined != nil || accepted != nil {
		t.Fatalf("an existing finding reads status=%s reexamined=%v accepted=%v", status, reexamined, accepted)
	}

	// The pairs are set together or not at all: a time with no name, or a name with no time, is
	// an attribution nobody can read.
	if _, err := pool.Exec(ctx, `UPDATE data_quality_findings SET reexamined_at = now() WHERE id = $1`,
		findingID); err == nil {
		t.Errorf("a re-examination with no run behind it was accepted")
	}
	if _, err := pool.Exec(ctx, `UPDATE data_quality_findings SET accepted_at = now() WHERE id = $1`,
		findingID); err == nil {
		t.Errorf("an acceptance with nobody behind it was accepted")
	}

	// Nothing may be accepted before it has been examined twice — there is nothing to decide yet.
	var ownerID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM users LIMIT 1`).Scan(&ownerID); err != nil {
		t.Skip("no user seeded; acceptance attribution is covered by the service tests")
	}
	if _, err := pool.Exec(ctx, `UPDATE data_quality_findings
		SET status='accepted_limitation', accepted_at=now(), accepted_by=$2 WHERE id = $1`,
		findingID, ownerID); err == nil {
		t.Errorf("a finding nobody had re-examined was accepted")
	}

	// Once re-examined, it may be accepted.
	mustExec(t, ctx, pool, `UPDATE data_quality_findings
		SET reexamined_at = now(), reexamining_run_id = $2 WHERE id = $1`, findingID, runID)
	mustExec(t, ctx, pool, `UPDATE data_quality_findings
		SET status='accepted_limitation', accepted_at=now(), accepted_by=$2 WHERE id = $1`, findingID, ownerID)
}

// seedFindingContext returns an import run and an instrument a finding can point at.
func seedFindingContext(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (string, string) {
	t.Helper()
	var instrumentID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM instruments LIMIT 1`).Scan(&instrumentID); err != nil {
		t.Fatalf("find a seeded instrument: %v", err)
	}
	runID := "dddddddd-0017-4000-8000-0000000000aa"
	mustExec(t, ctx, pool, `INSERT INTO import_runs
		(id, kind, provider, status, started_at, finished_at, app_version)
		VALUES ($1, 'daily_update', 'fixture', 'succeeded', now(), now(), 'test')`, runID)
	return runID, instrumentID
}
