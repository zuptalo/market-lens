package db_test

import (
	"context"
	"testing"

	"market-lens/server/internal/db"
	"market-lens/server/internal/testdb"
)

// Two seeded provider symbols went stale: the companies were renamed and their tickers
// changed with them. The instruments imported nothing at all, and the provider reported only
// that it had no data — which reads as a provider problem rather than as an identifier of
// ours that went out of date.
//
// The replacements are not inferred. They are what `marketdata resolve` reported after
// matching each stored ISIN against the provider's own catalog, which matters because the
// obvious guess for Sydbank was SYDB.CO and the provider actually lists ALSYDB.CO.
func TestStaleProviderSymbolsAreCorrectedOnACleanInstall(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	for _, expected := range []struct {
		isin   string
		symbol string
	}{
		{"DK0010311471", "ALSYDB.CO"}, // Sydbank A/S
		{"FI0009014575", "METSO.HE"},  // Metso Oyj, formerly Metso Outotec
	} {
		var symbol string
		if err := pool.QueryRow(ctx, `SELECT p.provider_symbol
			FROM provider_instruments p
			JOIN instruments i ON i.id = p.instrument_id
			WHERE i.isin = $1 AND p.provider = 'eodhd'`, expected.isin).Scan(&symbol); err != nil {
			t.Fatalf("ISIN %s has no eodhd mapping: %v", expected.isin, err)
		}
		if symbol != expected.symbol {
			t.Errorf("ISIN %s maps to %q, expected %q", expected.isin, symbol, expected.symbol)
		}
	}

	// The stale symbols must be gone entirely, not merely joined by the new ones.
	var stale int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM provider_instruments
		WHERE provider_symbol IN ('AL.CO', 'MOCORP.HE')`).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if stale != 0 {
		t.Errorf("%d stale provider symbols survived the migration", stale)
	}
}

// An installation that already imported under the old symbols keeps every bar it has. The
// correction changes which identifier future imports use; it does not discard history.
func TestCorrectingAStaleSymbolKeepsStoredHistory(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	// Every instrument still has exactly one active eodhd mapping, and the universe is intact.
	var instruments, mappings int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM instruments),
		(SELECT count(*) FROM provider_instruments WHERE provider = 'eodhd' AND active)`).
		Scan(&instruments, &mappings); err != nil {
		t.Fatal(err)
	}
	if instruments != 100 || mappings != 100 {
		t.Fatalf("instruments=%d active eodhd mappings=%d, want 100 and 100", instruments, mappings)
	}

	// And no two instruments share a provider symbol, which a careless UPDATE could cause.
	var duplicates int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM (
		SELECT provider_symbol FROM provider_instruments WHERE provider='eodhd' AND active
		GROUP BY provider_symbol HAVING count(*) > 1) collisions`).Scan(&duplicates); err != nil {
		t.Fatal(err)
	}
	if duplicates != 0 {
		t.Errorf("%d provider symbols are shared by more than one instrument", duplicates)
	}
}
