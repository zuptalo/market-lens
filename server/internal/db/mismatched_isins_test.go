package db_test

import (
	"context"
	"testing"

	"market-lens/server/internal/db"
	"market-lens/server/internal/testdb"
)

// Two seeded ISINs did not match the listings they are imported from.
//
// A mismatch is quieter than a stale ticker and worse: the symbol resolves, prices import, and
// everything looks correct while the record carries an identifier belonging to a different
// listing of the company. Anything that later joins on ISIN — a benchmark, a second provider,
// a corporate action feed — would attach data to the wrong row.
//
// The replacements are what `marketdata resolve` reported for each stored symbol after the
// audit was taught to print the provider's identifier alongside our own. They are not inferred.
func TestMismatchedISINsMatchTheListingTheyAreImportedFrom(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	for _, expected := range []struct {
		ticker string
		isin   string
	}{
		// BW LPG is Bermuda-incorporated and listed in Oslo; the stored value was a Singapore
		// identifier from a different listing of the same company.
		{"BWLPG", "BMG173841013"},
		// ROCKWOOL's Copenhagen B share carries a newer identifier than the one seeded.
		{"ROCK-B", "DK0063855168"},
	} {
		var isin string
		if err := pool.QueryRow(ctx,
			`SELECT isin FROM instruments WHERE ticker = $1`, expected.ticker).Scan(&isin); err != nil {
			t.Fatalf("%s: %v", expected.ticker, err)
		}
		if isin != expected.isin {
			t.Errorf("%s carries ISIN %q, expected %q", expected.ticker, isin, expected.isin)
		}
	}

	// The superseded identifiers must be gone, not merely joined by the new ones.
	var stale int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM instruments
		WHERE isin IN ('SGXZ69436764', 'DK0010219153')`).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if stale != 0 {
		t.Errorf("%d superseded ISINs survived the migration", stale)
	}
}

// Correcting an identifier must not disturb the universe around it.
func TestCorrectingAnISINLeavesTheUniverseIntact(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	var instruments, memberships, collisions int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM instruments),
		(SELECT count(*) FROM universe_memberships WHERE included_to IS NULL),
		-- One security may be listed on several exchanges under one ISIN — Nordea is listed on
		-- all three — so the identity that must stay unique is the exchange-qualified one,
		-- which is what the schema's own unique index covers.
		(SELECT count(*) FROM (
			SELECT exchange_id, isin FROM instruments WHERE active
			GROUP BY exchange_id, isin HAVING count(*) > 1) duplicates)`).
		Scan(&instruments, &memberships, &collisions); err != nil {
		t.Fatal(err)
	}
	if instruments != 100 || memberships != 100 {
		t.Fatalf("instruments=%d active memberships=%d, want 100 and 100", instruments, memberships)
	}
	if collisions != 0 {
		t.Errorf("%d exchange-qualified identities are shared by more than one instrument", collisions)
	}
}
