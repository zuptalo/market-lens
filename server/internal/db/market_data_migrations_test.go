package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"market-lens/server/internal/db"
	"market-lens/server/internal/instruments"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestMarketDataMigrationConstraintsAndAppendOnlyHistory(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	var instrumentID, exchangeID string
	if err := pool.QueryRow(ctx, `SELECT i.id::text, i.exchange_id::text FROM instruments i ORDER BY i.id LIMIT 1`).Scan(&instrumentID, &exchangeID); err != nil {
		t.Fatal(err)
	}
	runID, err := instruments.NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO import_runs (id,kind,provider,requested_from,requested_to,status,started_at,app_version)
		VALUES ($1,'backfill','fixture','2026-08-24','2026-08-24','running',now(),'test')`, runID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO exchange_sessions
		(exchange_id,session_date,status,opens_at,closes_at,source_reference)
		VALUES ($1,'2026-08-24','open','2026-08-24 07:00:00Z','2026-08-24 15:30:00Z','official fixture')
		ON CONFLICT (exchange_id,session_date) DO NOTHING`, exchangeID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO import_items
		(run_id,instrument_id,requested_from,requested_to,status,attempts)
		VALUES ($1,$2,'2026-08-24','2026-08-24','running',1)`, runID.String(), instrumentID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO daily_price_bars
		(instrument_id,session_date,open,high,low,close,adjusted_close,volume,currency,provider,source_hash,import_run_id,first_observed_at,last_observed_at)
		VALUES ($1,'2026-08-24',100.1,105,99.5,104.2,103.9,1200,'SEK','fixture','hash-one',$2,now(),now())`, instrumentID, runID.String()); err != nil {
		t.Fatal(err)
	}

	constraintTests := []struct {
		name, statement, code string
		arguments             []any
	}{
		{name: "duplicate bar", code: "23505", statement: `INSERT INTO daily_price_bars
			(instrument_id,session_date,open,high,low,close,volume,currency,provider,source_hash,import_run_id,first_observed_at,last_observed_at)
			SELECT instrument_id,session_date,open,high,low,close,volume,currency,provider,'other',import_run_id,now(),now() FROM daily_price_bars LIMIT 1`},
		{name: "impossible OHLC", code: "23514", statement: `UPDATE daily_price_bars SET low=106 WHERE instrument_id=$1`, arguments: []any{instrumentID}},
		{name: "negative volume", code: "23514", statement: `UPDATE daily_price_bars SET volume=-1 WHERE instrument_id=$1`, arguments: []any{instrumentID}},
		{name: "orphan action", code: "23503", statement: `INSERT INTO corporate_actions
			(id,instrument_id,provider,provider_action_id,action_type,ex_date,source_hash,import_run_id,first_observed_at,last_observed_at)
			VALUES ('23000000-0000-4000-8000-000000000001','23000000-0000-4000-8000-000000000002','fixture','orphan','split','2026-08-24','hash',$1,now(),now())`, arguments: []any{runID.String()}},
	}
	for _, constraintTest := range constraintTests {
		t.Run(constraintTest.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, constraintTest.statement, constraintTest.arguments...)
			var postgresError *pgconn.PgError
			if !errors.As(err, &postgresError) || postgresError.Code != constraintTest.code {
				t.Fatalf("constraint error = %v, want SQLSTATE %s", err, constraintTest.code)
			}
		})
	}

	revisionID, err := instruments.NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO price_bar_revisions
		(id,instrument_id,session_date,revision,open,high,low,close,adjusted_close,volume,currency,provider,source_hash,
		import_run_id,first_observed_at,last_observed_at,superseding_run_id,superseded_at)
		SELECT $1,instrument_id,session_date,1,open,high,low,close,adjusted_close,volume,currency,provider,source_hash,
		import_run_id,first_observed_at,last_observed_at,$2,now() FROM daily_price_bars WHERE instrument_id=$3`,
		revisionID.String(), runID.String(), instrumentID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE price_bar_revisions SET close=1 WHERE id=$1`, revisionID.String()); err == nil {
		t.Fatal("price-bar revision was mutable")
	}

	findingID, err := instruments.NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO data_quality_findings
		(id,instrument_id,session_date,run_id,rule,severity,disposition,detail,status,created_at)
		VALUES ($1,$2,'2026-08-24',$3,'zero_volume','warning','flagged','Zero volume observed.','open',$4)`,
		findingID.String(), instrumentID, runID.String(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
}

func TestMarketDataMigrationsUpgradeAnExplicitBaseline(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (
		version int PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now()
	); INSERT INTO schema_migrations(version) VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var versions int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations WHERE version BETWEEN 1 AND 5`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 5 {
		t.Fatalf("applied migration versions = %d, want 5", versions)
	}
}
