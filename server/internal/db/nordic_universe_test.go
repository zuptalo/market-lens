package db_test

import (
	"context"
	"testing"

	"market-lens/server/internal/db"
	"market-lens/server/internal/testdb"
)

func TestNordicUniverseContainsExactlyTwentyFiveValidListingsPerExchange(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	var total int
	if err := pool.QueryRow(ctx, `SELECT count(*)
		FROM universe_memberships m
		JOIN research_universes u ON u.id = m.universe_id
		WHERE u.code = 'nordic-liquid-v1' AND m.included_to IS NULL`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 100 {
		t.Fatalf("active nordic-liquid-v1 memberships = %d, want 100", total)
	}

	rows, err := pool.Query(ctx, `SELECT e.mic, count(*)
		FROM universe_memberships m
		JOIN research_universes u ON u.id = m.universe_id
		JOIN instruments i ON i.id = m.instrument_id
		JOIN exchanges e ON e.id = i.exchange_id
		WHERE u.code = 'nordic-liquid-v1' AND m.included_to IS NULL
		GROUP BY e.mic ORDER BY e.mic`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var mic string
		var count int
		if err := rows.Scan(&mic, &count); err != nil {
			t.Fatal(err)
		}
		counts[mic] = count
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, mic := range []string{"XCSE", "XHEL", "XOSL", "XSTO"} {
		if counts[mic] != 25 {
			t.Errorf("%s memberships = %d, want 25", mic, counts[mic])
		}
	}
	if len(counts) != 4 {
		t.Fatalf("seeded exchanges = %#v, want exactly four MICs", counts)
	}
}

func TestNordicUniverseSeedHasCurationAndIdentityEvidence(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	var invalid int
	if err := pool.QueryRow(ctx, `SELECT count(*)
		FROM universe_memberships m
		JOIN research_universes u ON u.id = m.universe_id
		JOIN instruments i ON i.id = m.instrument_id
		JOIN exchanges e ON e.id = i.exchange_id
		LEFT JOIN provider_instruments p ON p.instrument_id = i.id AND p.provider = 'eodhd' AND p.active
		WHERE u.code = 'nordic-liquid-v1' AND (
			m.included_to IS NOT NULL OR btrim(m.curation_source) = '' OR btrim(m.curation_note) = '' OR
			i.instrument_type <> 'common_stock' OR NOT i.active OR i.isin !~ '^[A-Z]{2}[A-Z0-9]{9}[0-9]$' OR
			i.currency <> e.currency OR i.country <> e.country OR p.provider_symbol IS NULL
		)`).Scan(&invalid); err != nil {
		t.Fatal(err)
	}
	if invalid != 0 {
		t.Fatalf("seed contains %d memberships without complete identity/curation evidence", invalid)
	}
}
