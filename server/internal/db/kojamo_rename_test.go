package db_test

import (
	"context"
	"testing"

	"market-lens/server/internal/db"
	"market-lens/server/internal/testdb"
)

// Kojamo Oyj became Lumo Kodit Oyj and its trading code changed from KOJAMO to LUMO on
// 16 March 2026. The seeded universe still asked the provider for KOJAMO.HE, which kept
// serving history until mid-May and then stopped — so the instrument looked healthy by every
// measure except the one that mattered, its newest session.
//
// The identity is the whole row, not just the symbol. Leaving the ticker and name behind would
// mean the explorer lists a company under a name it no longer has, and the next person to
// audit the universe finds a name that matches nothing in the provider's catalog.
//
// The ISIN does not change: the exchange announcement is explicit that FI4000312251 carried
// over, and it is what ties the corrected row to the history already stored against it.
func TestKojamoIsRenamedToLumoOnACleanInstall(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	var ticker, name, symbol, mic string
	if err := pool.QueryRow(ctx, `SELECT i.ticker,i.name,p.provider_symbol,e.mic
		FROM instruments i
		JOIN provider_instruments p ON p.instrument_id = i.id AND p.provider = 'eodhd'
		JOIN exchanges e ON e.id = i.exchange_id
		WHERE i.isin = 'FI4000312251'`).Scan(&ticker, &name, &symbol, &mic); err != nil {
		t.Fatalf("ISIN FI4000312251 has no eodhd mapping: %v", err)
	}
	if ticker != "LUMO" || name != "Lumo Kodit Oyj" || symbol != "LUMO.HE" || mic != "XHEL" {
		t.Errorf("row = %s / %q / %s on %s; want LUMO / \"Lumo Kodit Oyj\" / LUMO.HE on XHEL",
			ticker, name, symbol, mic)
	}

	// The old identity must be gone rather than duplicated: a second row for the same company
	// would import the same history twice under two names.
	var stale int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM instruments i
		LEFT JOIN provider_instruments p ON p.instrument_id = i.id
		WHERE i.ticker = 'KOJAMO' OR p.provider_symbol = 'KOJAMO.HE'`).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if stale != 0 {
		t.Errorf("%d rows still carry the old KOJAMO identity", stale)
	}
}
