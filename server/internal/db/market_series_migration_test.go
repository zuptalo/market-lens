package db_test

import (
	"context"
	"testing"

	"market-lens/server/internal/db"
	"market-lens/server/internal/testdb"
)

// TestMarketSeriesMigration covers what a benchmark and a rate are allowed to be.
//
// The decision this schema encodes is that neither is an instrument. An index stored as an
// instrument would appear on Markets, be computed over by the feature engine, and be scored by
// the very strategy it exists to judge — so it lives in its own table with no exchange
// membership, no sector and no purchasability, and nothing in the product can put it in a
// universe by accident.
func TestMarketSeriesMigration(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, table := range []string{"benchmark_series", "benchmark_points", "fx_rates"} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("table %s does not exist", table)
		}
	}

	// The four series the universe's markets are compared against arrive with the migration,
	// each naming the market it is the benchmark *for*, so a result never has to guess.
	rows, err := pool.Query(ctx, `SELECT code, mic, currency FROM benchmark_series ORDER BY code`)
	if err != nil {
		t.Fatalf("read the published series: %v", err)
	}
	defer rows.Close()
	published := map[string][2]string{}
	for rows.Next() {
		var code, mic, currency string
		if err := rows.Scan(&code, &mic, &currency); err != nil {
			t.Fatal(err)
		}
		published[code] = [2]string{mic, currency}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for code, want := range map[string][2]string{
		"OMXS30.INDX": {"XSTO", "SEK"},
		"OMXH25.INDX": {"XHEL", "EUR"},
		"OBX.INDX":    {"XOSL", "NOK"},
		"OMXC25.INDX": {"XCSE", "DKK"},
	} {
		got, ok := published[code]
		if !ok {
			t.Errorf("benchmark %s is not published", code)
			continue
		}
		if got != want {
			t.Errorf("benchmark %s is for %v, want %v", code, got, want)
		}
	}

	// One market, one benchmark: two series claiming the same market would leave a result free
	// to pick whichever flattered it.
	if _, err := pool.Exec(ctx, `INSERT INTO benchmark_series (id, code, name, currency, mic)
		VALUES ('00000000-0021-4000-8000-0000000000ff', 'OMXC20.INDX', 'OMX Copenhagen 20', 'DKK', 'XCSE')`); err == nil {
		t.Errorf("a second benchmark was accepted for XCSE")
	}

	var seriesID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM benchmark_series WHERE code = 'OMXS30.INDX'`).
		Scan(&seriesID); err != nil {
		t.Fatalf("read the Swedish series: %v", err)
	}

	mustExec(t, ctx, pool, `INSERT INTO benchmark_points (series_id, session_date, close)
		VALUES ($1, '2016-08-31', 1432.25)`, seriesID)
	if _, err := pool.Exec(ctx, `INSERT INTO benchmark_points (series_id, session_date, close)
		VALUES ($1, '2016-08-31', 1500)`, seriesID); err == nil {
		t.Errorf("a second close was accepted for the same series and session")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO benchmark_points (series_id, session_date, close)
		VALUES ($1, '2016-09-01', 0)`, seriesID); err == nil {
		t.Errorf("a close of zero was accepted")
	}

	// A rate is stored as the provider quotes it, in one direction only. Storing the inverse as
	// a second row would let the two disagree, and nothing would say which was right.
	mustExec(t, ctx, pool, `INSERT INTO fx_rates (base, quote, session_date, rate)
		VALUES ('EUR', 'SEK', '2016-08-31', 9.456700000000)`)
	if _, err := pool.Exec(ctx, `INSERT INTO fx_rates (base, quote, session_date, rate)
		VALUES ('EUR', 'SEK', '2016-08-31', 9.5)`); err == nil {
		t.Errorf("a second rate was accepted for the same pair and session")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO fx_rates (base, quote, session_date, rate)
		VALUES ('eur', 'SEK', '2016-09-01', 9.5)`); err == nil {
		t.Errorf("a lower-case currency code was accepted")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO fx_rates (base, quote, session_date, rate)
		VALUES ('EUR', 'EUR', '2016-09-01', 1)`); err == nil {
		t.Errorf("a pair of a currency with itself was accepted")
	}

	// The precision the feature and strategy layers already use. A rate rounded to fewer places
	// than the money it converts would make a conversion irreproducible at the last digit.
	var precision, scale int
	if err := pool.QueryRow(ctx, `SELECT numeric_precision, numeric_scale
		FROM information_schema.columns WHERE table_schema = current_schema()
		  AND table_name = 'fx_rates' AND column_name = 'rate'`).Scan(&precision, &scale); err != nil {
		t.Fatalf("read the rate precision: %v", err)
	}
	if precision != 24 || scale != 12 {
		t.Errorf("fx_rates.rate is numeric(%d,%d), want numeric(24,12)", precision, scale)
	}
}
