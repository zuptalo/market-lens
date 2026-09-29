package db_test

import (
	"context"
	"testing"

	"market-lens/server/internal/db"
	"market-lens/server/internal/testdb"
)

// Feature 030 arrives switched off, with nothing to report, and with a verified fallback symbol for
// every instrument in the universe.
func TestTheFallbackArrivesSwitchedOffAndMapped(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var enabled bool
	if err := pool.QueryRow(ctx, `SELECT enabled FROM market_data_fallback_settings`).Scan(&enabled); err != nil {
		t.Fatalf("the switch does not exist: %v", err)
	}
	if enabled {
		t.Error("the fallback arrived switched on; an install that never asked must receive no unofficial data")
	}
	// A singleton: a second switch would make "is it on?" ambiguous.
	if _, err := pool.Exec(ctx, `INSERT INTO market_data_fallback_settings (enabled) VALUES (true)`); err == nil {
		t.Error("a second fallback switch was accepted")
	}

	var active bool
	var instrumentsCovered int
	if err := pool.QueryRow(ctx, `SELECT active, instruments FROM market_data_fallback_state`).
		Scan(&active, &instrumentsCovered); err != nil {
		t.Fatalf("the state does not exist: %v", err)
	}
	if active || instrumentsCovered != 0 {
		t.Errorf("a fresh install reports fallback active=%v over %d instruments", active, instrumentsCovered)
	}

	// The fallback run is a kind of its own, so every run's provider column stays true.
	if _, err := pool.Exec(ctx, `INSERT INTO import_runs (id, kind, provider, status, parent_run_id, started_at, finished_at, app_version)
		VALUES ('30000000-0000-4000-8000-000000000001', 'daily_update', 'eodhd', 'failed', NULL, now(), now(), 'test'),
		       ('30000000-0000-4000-8000-000000000002', 'fallback', 'yahoo', 'succeeded',
		        '30000000-0000-4000-8000-000000000001', now(), now(), 'test')`); err != nil {
		t.Errorf("a fallback run cannot be recorded: %v", err)
	}
	// And never as an orphan: a fallback run covers a primary run.
	if _, err := pool.Exec(ctx, `INSERT INTO import_runs (id, kind, provider, status, started_at, finished_at, app_version)
		VALUES ('30000000-0000-4000-8000-000000000003', 'fallback', 'yahoo', 'succeeded', now(), now(), 'test')`); err == nil {
		t.Error("a fallback run without the primary run it covered was accepted")
	}

	for _, table := range []string{"notification_preferences", "notifications"} {
		var accepts bool
		if err := pool.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) LIKE '%market_data_fallback%'
			FROM pg_constraint WHERE conname = $1 AND conrelid = $2::regclass`, table+"_kind_check", table).Scan(&accepts); err != nil {
			t.Fatal(err)
		}
		if !accepts {
			t.Errorf("%s cannot hold the market_data_fallback kind", table)
		}
	}

	// Every active instrument has exactly one active fallback symbol.
	var unmapped int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM instruments i
		WHERE i.active AND NOT EXISTS (
			SELECT 1 FROM provider_instruments p
			WHERE p.instrument_id = i.id AND p.provider = 'yahoo' AND p.active)`).Scan(&unmapped); err != nil {
		t.Fatal(err)
	}
	if unmapped != 0 {
		t.Errorf("%d active instruments have no fallback symbol", unmapped)
	}

	// Keyed on ISIN and exchange: Nordea's one ISIN is listed on three exchanges, and each listing
	// must map to its own symbol. Sydbank is the instrument whose obvious guess was wrong once.
	for _, want := range []struct{ mic, isin, symbol string }{
		{"XHEL", "FI4000297767", "NDA-FI.HE"},
		{"XCSE", "FI4000297767", "NDA-DK.CO"},
		{"XCSE", "DK0010311471", "ALSYDB.CO"},
		{"XOSL", "NO0010096985", "EQNR.OL"},
	} {
		var symbol string
		if err := pool.QueryRow(ctx, `SELECT p.provider_symbol FROM provider_instruments p
			JOIN instruments i ON i.id = p.instrument_id JOIN exchanges e ON e.id = i.exchange_id
			WHERE p.provider = 'yahoo' AND p.active AND e.mic = $1 AND i.isin = $2`, want.mic, want.isin).
			Scan(&symbol); err != nil {
			t.Errorf("%s on %s: %v", want.isin, want.mic, err)
			continue
		}
		if symbol != want.symbol {
			t.Errorf("%s on %s maps to %q, want %q", want.isin, want.mic, symbol, want.symbol)
		}
	}
}
