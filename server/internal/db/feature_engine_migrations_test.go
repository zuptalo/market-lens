package db

import (
	"context"
	"errors"
	"testing"

	"market-lens/server/internal/instruments"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The twenty-five version-1 definitions feature 013 seeds. The engine cannot compute a
// feature it has no published definition for (SC-003), so the seed and the registry must
// agree, and this list is the one both are checked against.
var featureEngineVersionOneDefinitions = []string{
	"return_1", "return_5", "return_20", "return_60", "return_90", "return_250", "log_return_1",
	"sma_20", "sma_50", "sma_200", "trend_50_200", "momentum_20",
	"relative_strength_20", "relative_strength_90",
	"volatility_20", "atr_14", "rsi_14", "macd_12_26", "macd_signal_9", "macd_histogram",
	"drawdown_250", "volume_sma_20", "volume_ratio_20", "regime", "composite_return_1",
}

func TestFeatureEngineMigrationsCleanInstall(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{"feature_definitions", "feature_values", "universe_composites", "feature_runs", "feature_run_items"} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("table %s does not exist", table)
		}
	}
	if t.Failed() {
		t.FailNow()
	}

	assertPrimaryKey(t, ctx, pool, "feature_values", "instrument_id, session_date, definition_id")
	assertPrimaryKey(t, ctx, pool, "universe_composites", "universe_id, session_date, definition_id")
	assertPrimaryKey(t, ctx, pool, "feature_run_items", "run_id, instrument_id")

	// Read from the catalog directly, and never through pg_indexes: that view calls
	// pg_get_indexdef() on every index in the database, which races the teardown of the tests
	// running beside this one ("could not open relation with OID"). Comparing the indexed
	// column names touches nothing outside this schema.
	var indexed bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM pg_index x
		JOIN pg_class t ON t.oid = x.indrelid
		WHERE t.relname = 'feature_values'
		  AND t.relnamespace = current_schema()::regnamespace
		  AND (SELECT string_agg(a.attname, ', ' ORDER BY k.ordinality)
		       FROM unnest(x.indkey) WITH ORDINALITY AS k(attnum, ordinality)
		       JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum)
		      = 'definition_id, session_date')`).Scan(&indexed); err != nil {
		t.Fatal(err)
	}
	if !indexed {
		t.Error("feature_values has no (definition_id, session_date) index for the universe-wide reads")
	}

	seeded := map[string]bool{}
	rows, err := pool.Query(ctx, `SELECT name, version, price_basis, superseded_at IS NULL, window_sessions
		FROM feature_definitions ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name, basis string
		var version int
		var current bool
		var window *int
		if err := rows.Scan(&name, &version, &basis, &current, &window); err != nil {
			t.Fatal(err)
		}
		if version != 1 || !current {
			t.Errorf("%s is seeded as version %d, current=%v; want version 1 and current", name, version, current)
		}
		if basis != "raw" && basis != "adjusted" {
			t.Errorf("%s has price basis %q", name, basis)
		}
		if window == nil || *window <= 0 {
			t.Errorf("%s has no positive window in sessions", name)
		}
		seeded[name] = true
	}
	rows.Close()
	for _, name := range featureEngineVersionOneDefinitions {
		if !seeded[name] {
			t.Errorf("definition %s is not seeded", name)
		}
	}
	if len(seeded) != len(featureEngineVersionOneDefinitions) {
		t.Errorf("%d definitions seeded, want %d", len(seeded), len(featureEngineVersionOneDefinitions))
	}

	// The three statistics adopted from feature 005 read raw closes, exactly as its listing
	// query does, so that no displayed number moves when the source of it changes.
	for _, adopted := range []string{"return_20", "return_90", "volatility_20"} {
		var basis string
		if err := pool.QueryRow(ctx, `SELECT price_basis FROM feature_definitions WHERE name = $1`, adopted).Scan(&basis); err != nil {
			t.Fatal(err)
		}
		if basis != "raw" {
			t.Errorf("%s is adopted from feature 005 and must read raw closes; it reads %s", adopted, basis)
		}
	}

	var instrumentID, universeID string
	if err := pool.QueryRow(ctx, `SELECT i.id::text, u.id::text FROM instruments i, research_universes u
		WHERE u.code = 'nordic-liquid-v1' ORDER BY i.id LIMIT 1`).Scan(&instrumentID, &universeID); err != nil {
		t.Fatal(err)
	}
	runID := mustNewUUID(t)
	if _, err := pool.Exec(ctx, `INSERT INTO feature_runs (id,kind,status,universe_id,started_at,app_version)
		VALUES ($1,'full','running',$2,now(),'test')`, runID, universeID); err != nil {
		t.Fatalf("a running full run could not be recorded: %v", err)
	}
	var definitionID, compositeID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM feature_definitions WHERE name = 'return_1'`).Scan(&definitionID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT id::text FROM feature_definitions WHERE name = 'composite_return_1'`).Scan(&compositeID); err != nil {
		t.Fatal(err)
	}

	valueInsert := `INSERT INTO feature_values (instrument_id,session_date,definition_id,value,label,absence_reason,computed_at,run_id)
		VALUES ($1,$2,$3,$4,$5,$6,now(),$7)`
	rejections := []struct {
		name      string
		statement string
		arguments []any
	}{
		{"a value and a reason", valueInsert, []any{instrumentID, "2026-01-02", definitionID, "0.1", nil, "window_gap", runID}},
		{"neither value nor reason", valueInsert, []any{instrumentID, "2026-01-03", definitionID, nil, nil, nil, runID}},
		{"a value and a label", valueInsert, []any{instrumentID, "2026-01-04", definitionID, "0.1", "trending_up", nil, runID}},
		{"an unknown reason", valueInsert, []any{instrumentID, "2026-01-05", definitionID, nil, nil, "imputed", runID}},
		{"a composite with a mean and a reason", `INSERT INTO universe_composites
			(universe_id,session_date,definition_id,mean_return,contributor_count,absence_reason,computed_at,run_id)
			VALUES ($1,'2026-01-02',$2,0.01,3,'insufficient_contributors',now(),$3)`, []any{universeID, compositeID, runID}},
		{"a composite with a negative contributor count", `INSERT INTO universe_composites
			(universe_id,session_date,definition_id,mean_return,contributor_count,computed_at,run_id)
			VALUES ($1,'2026-01-03',$2,0.01,-1,now(),$3)`, []any{universeID, compositeID, runID}},
		{"a second version-1 return_1", `INSERT INTO feature_definitions
			(id,name,version,window_sessions,price_basis,parameters,undefined_conditions,session_length_sensitive,published_at)
			VALUES ($1,'return_1',1,2,'adjusted','{}','none',false,now())`, []any{mustNewUUID(t)}},
		{"an unknown price basis", `INSERT INTO feature_definitions
			(id,name,version,window_sessions,price_basis,parameters,undefined_conditions,session_length_sensitive,published_at)
			VALUES ($1,'return_1',2,2,'provider_adjusted','{}','none',false,now())`, []any{mustNewUUID(t)}},
	}
	for _, rejection := range rejections {
		_, err := pool.Exec(ctx, rejection.statement, rejection.arguments...)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || (pgErr.Code != "23514" && pgErr.Code != "23505") {
			t.Errorf("%s was accepted (err=%v); the schema must refuse it", rejection.name, err)
		}
	}

	accepted := []struct {
		name      string
		arguments []any
	}{
		{"a value", []any{instrumentID, "2026-02-02", definitionID, "0.012345678901", nil, nil, runID}},
		{"a label", []any{instrumentID, "2026-02-03", definitionID, nil, "range_bound", nil, runID}},
		{"an absence", []any{instrumentID, "2026-02-04", definitionID, nil, nil, "zero_denominator", runID}},
	}
	for _, row := range accepted {
		if _, err := pool.Exec(ctx, valueInsert, row.arguments...); err != nil {
			t.Errorf("%s was refused: %v", row.name, err)
		}
	}
	var stored string
	if err := pool.QueryRow(ctx, `SELECT value::text FROM feature_values WHERE session_date = '2026-02-02'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "0.012345678901" {
		t.Errorf("a twelve-place value read back as %s; numeric(24,12) must not re-round", stored)
	}
}

func TestFeatureEngineMigrationsUpgradeVersionSixteen(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	applyMigrationsThrough(t, ctx, pool, 16)

	var instrumentID, exchangeID string
	if err := pool.QueryRow(ctx, `SELECT i.id::text, i.exchange_id::text FROM instruments i ORDER BY i.id LIMIT 1`).Scan(&instrumentID, &exchangeID); err != nil {
		t.Fatal(err)
	}
	runID := mustNewUUID(t)
	if _, err := pool.Exec(ctx, `INSERT INTO import_runs (id,kind,provider,requested_from,requested_to,status,started_at,app_version)
		VALUES ($1,'backfill','fixture','2026-08-24','2026-08-24','running',now(),'test')`, runID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO exchange_sessions
		(exchange_id,session_date,status,opens_at,closes_at,source_reference)
		VALUES ($1,'2026-08-24','open','2026-08-24 07:00:00Z','2026-08-24 15:30:00Z','official fixture')
		ON CONFLICT (exchange_id,session_date) DO NOTHING`, exchangeID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO daily_price_bars
		(instrument_id,session_date,open,high,low,close,volume,currency,provider,source_hash,import_run_id,first_observed_at,last_observed_at)
		VALUES ($1,'2026-08-24',100.1,105,99.5,104.2,1200,'SEK','fixture','hash-one',$2,now(),now())`, instrumentID, runID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO data_quality_findings
		(id,instrument_id,session_date,run_id,rule,severity,disposition,detail,status,created_at)
		VALUES ($1,$2,'2026-08-21',$3,'missing_session','warning','flagged','fixture','open',now())`, mustNewUUID(t), instrumentID, runID); err != nil {
		t.Fatal(err)
	}

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade from version 16 needs a manual step: %v", err)
	}

	var bars, findings, definitions, latest int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM daily_price_bars),
		(SELECT count(*) FROM data_quality_findings WHERE status = 'open'),
		(SELECT count(*) FROM feature_definitions WHERE superseded_at IS NULL),
		(SELECT max(version) FROM schema_migrations)`).Scan(&bars, &findings, &definitions, &latest); err != nil {
		t.Fatal(err)
	}
	if bars != 1 || findings != 1 {
		t.Errorf("the upgrade disturbed existing data: bars=%d open findings=%d", bars, findings)
	}
	if definitions != len(featureEngineVersionOneDefinitions) {
		t.Errorf("%d current definitions after upgrade, want %d", definitions, len(featureEngineVersionOneDefinitions))
	}
	// The upgrade runs every migration, so this tracks the head of the ordered set. It exists
	// to catch a migration that failed to apply, not to pin a number.
	if latest != 21 {
		t.Errorf("schema is at version %d after upgrade, want 21", latest)
	}
}

func assertPrimaryKey(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table, columns string) {
	t.Helper()
	var actual string
	if err := pool.QueryRow(ctx, `SELECT string_agg(a.attname, ', ' ORDER BY k.ordinality)
		FROM pg_constraint c
		JOIN LATERAL unnest(c.conkey) WITH ORDINALITY AS k(attnum, ordinality) ON true
		JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = k.attnum
		WHERE c.conrelid = to_regclass($1) AND c.contype = 'p'`, table).Scan(&actual); err != nil {
		t.Fatalf("%s primary key: %v", table, err)
	}
	if actual != columns {
		t.Errorf("%s primary key is (%s), want (%s)", table, actual, columns)
	}
}

func mustNewUUID(t *testing.T) string {
	t.Helper()
	id, err := instruments.NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	return id.String()
}

// US5's upgrade case: the Markets listing adopts the engine's statistics in a release that
// ships *after* the engine has computed, but an installation that migrates and lists before
// the first pass must still work — showing the three statistics absent rather than failing or
// inventing them. Sorting by one of them must still put the absences last.
func TestUpgradeLeavesTheMarketsStatisticsReadableUntilEngineValuesExist(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var values int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM feature_values`).Scan(&values); err != nil {
		t.Fatal(err)
	}
	if values != 0 {
		t.Fatalf("a freshly migrated database holds %d feature values, expected none", values)
	}

	page, err := instruments.NewRepository(pool).Listing(ctx, instruments.ListingFilter{
		Sort: instruments.SortReturn20, Limit: 50, AsOf: instruments.SessionDate("2026-08-31"),
	})
	if err != nil {
		t.Fatalf("the listing failed with no engine values stored: %v", err)
	}
	if len(page.Items) == 0 {
		t.Fatal("the seeded universe listed no instruments")
	}
	for _, row := range page.Items {
		if row.Return20 != nil || row.Return90 != nil || row.Volatility != nil {
			t.Errorf("%s listed a statistic before the engine ran: r20=%v r90=%v vol=%v",
				row.Ticker, row.Return20, row.Return90, row.Volatility)
		}
	}
}
