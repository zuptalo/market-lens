package db

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"market-lens/server/internal/testdb"
)

// The upgrade must not tell somebody again about the session it already told them about.
//
// Feature 029 made a telling about a session and claims it before raising. An installation
// upgrading into it has a session that already produced one telling per instrument and no claim to
// show for it, so the first pass after the upgrade would claim that session and say the whole thing
// once more — a twelfth message about a day somebody heard about eleven times.
func TestASessionAlreadyToldAboutIsClaimedByTheUpgrade(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	// The installation as it stood before this upgrade: the collapsing table exists but nothing
	// has claimed anything, and a session has already produced one telling per instrument.
	applyMigrationsThrough(t, ctx, pool, 34)
	seedToldSession(t, ctx, pool, "2026-09-18", []string{"AAA", "BBB", "CCC"})

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var claimed string
	var changes int
	if err := pool.QueryRow(ctx, `SELECT session_date::text, changes FROM signal_change_sessions`).
		Scan(&claimed, &changes); err != nil {
		t.Fatalf("the session already told about was not claimed: %v", err)
	}
	if claimed != "2026-09-18" {
		t.Errorf("claimed %s, want the session the tellings were about", claimed)
	}
	if changes != 3 {
		t.Errorf("recorded %d changes, want the three instruments that were told about", changes)
	}

	// Migrating again changes nothing, which is what the primary key is for.
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM signal_change_sessions`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("%d claims after running twice, want one", rows)
	}
}

// A clean installation has told nobody anything, and must not claim a session on their behalf: the
// first real pass would then say nothing at all about a day that did change.
func TestACleanInstallationClaimsNothing(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM signal_change_sessions`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Errorf("%d sessions were claimed on an installation that has told nobody anything", rows)
	}
}

func seedToldSession(t *testing.T, ctx context.Context, pool *pgxpool.Pool, session string, tickers []string) {
	t.Helper()
	var userID string
	now := time.Now().UTC()
	if err := pool.QueryRow(ctx, `INSERT INTO users
		(id,email,normalized_email,display_name,role,status,created_at,updated_at,email_verified_at)
		VALUES (gen_random_uuid(),'told@example.com','told@example.com','Told','owner','active',$1,$1,$1)
		RETURNING id::text`, now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	execOrFail(t, ctx, pool, `INSERT INTO exchanges (id, mic, name, country, currency, timezone)
		VALUES (gen_random_uuid(), 'XTLD', 'Told Exchange', 'SE', 'SEK', 'Europe/Stockholm')`)
	execOrFail(t, ctx, pool, `INSERT INTO research_universes (id, code, name, description)
		VALUES ('70000000-0029-4000-8000-0000000000d1', 'told-fixture', 'Told fixture', 'fixture')`)
	execOrFail(t, ctx, pool, `INSERT INTO strategy_runs
		(id, strategy_id, kind, status, universe_id, started_at, finished_at, app_version)
		SELECT '70000000-0029-4000-8000-0000000000d2', s.id, 'strategy', 'succeeded',
		       '70000000-0029-4000-8000-0000000000d1', now(), now(), 'test'
		FROM strategies s WHERE s.name = 'momentum_trend' ORDER BY s.version DESC LIMIT 1`)
	for index, ticker := range tickers {
		execOrFail(t, ctx, pool, `INSERT INTO instruments
			(id, exchange_id, isin, ticker, name, currency, country, instrument_type, active,
			 purchasability_status)
			SELECT gen_random_uuid(), e.id, 'SE000000004' || $2, $1, 'Told Fixture', 'SEK', 'SE',
			       'common_stock', true, 'unverified'
			FROM exchanges e WHERE e.mic = 'XTLD'`, ticker, strconv.Itoa(index))
		execOrFail(t, ctx, pool, `INSERT INTO signals
			(instrument_id, session_date, strategy_id, score, action, confidence,
			 contributions, divisor, computed_at, run_id)
			SELECT i.id, $2::date, r.strategy_id, 0, 'BUY', 0.5, '[]'::jsonb, 1, now(), r.id
			FROM instruments i, strategy_runs r
			WHERE i.ticker = $1 AND r.id = '70000000-0029-4000-8000-0000000000d2'`, ticker, session)
		// One telling per instrument, keyed by ticker: the shape this upgrade replaces.
		execOrFail(t, ctx, pool, `INSERT INTO notifications
			(id, user_id, kind, channel, subject_key, count, detail, state, sent_at)
			VALUES (gen_random_uuid(), $1, 'signal_change', 'email', $2, 1, '{}'::jsonb, 'sent', now())`,
			userID, ticker)
	}
}

// execOrFail is the local spelling of a helper the external test package already has; this file
// lives in package db because it needs applyMigrationsThrough, which does not cross that line.
func execOrFail(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}
