package db

import (
	"context"
	"testing"

	"market-lens/server/internal/testdb"
)

// Feature 014 US3. Sector was null for all 100 curated instruments: the seed never populated
// it, the code that would has no caller, and the deployment's market-data plan excludes the
// fundamentals that carry it. The interface offered a filter whose every choice returned
// nothing. Classification is therefore curated reference data, arriving by migration.
func TestSectorClassificationMigrationClassifiesTheCuratedUniverse(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	var vocabulary int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sectors`).Scan(&vocabulary); err != nil {
		t.Fatal(err)
	}
	if vocabulary < 12 {
		t.Fatalf("the vocabulary holds %d values, expected the eleven sectors plus unclassified", vocabulary)
	}

	var unclassifiedPresent bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM sectors WHERE code = 'unclassified')`).Scan(&unclassifiedPresent); err != nil {
		t.Fatal(err)
	}
	if !unclassifiedPresent {
		t.Error("the vocabulary has no unclassified member, so an unknown classification has nowhere to go")
	}

	// Every curated instrument carries a classification and its provenance.
	var total, classified, withProvenance int
	if err := pool.QueryRow(ctx, `SELECT count(*),
		count(*) FILTER (WHERE sector <> 'unclassified'),
		count(*) FILTER (WHERE btrim(sector_source) <> '' AND sector_reviewed_on IS NOT NULL)
		FROM instruments`).Scan(&total, &classified, &withProvenance); err != nil {
		t.Fatal(err)
	}
	if total == 0 {
		t.Fatal("the seeded universe is empty")
	}
	if classified != total {
		t.Errorf("%d of %d instruments are unclassified; the curated universe is classified by hand and must be complete",
			total-classified, total)
	}
	if withProvenance != total {
		t.Errorf("%d instruments carry no source or review date; curated data without provenance cannot be seen to go stale",
			total-withProvenance)
	}

	// Spot-check the judgement itself, so a mangled assignment is visible rather than merely
	// well-formed. These three are not borderline.
	for isin, want := range map[string]string{
		"DK0062498333": "health_care",            // Novo Nordisk
		"SE0000108656": "information_technology", // Ericsson
		"DK0063855168": "industrials",            // Rockwool, whose ISIN migration 0014 corrected
		"NO0010096985": "energy",                 // Equinor
	} {
		var got string
		if err := pool.QueryRow(ctx, `SELECT sector FROM instruments WHERE isin = $1`, isin).Scan(&got); err != nil {
			t.Errorf("read %s: %v", isin, err)
			continue
		}
		if got != want {
			t.Errorf("%s is classified %q, expected %q", isin, got, want)
		}
	}
}

// The constraint, not the convention, is what stops this recurring: the column is NOT NULL
// against a vocabulary that contains "unclassified", so an instrument with no classification
// state at all cannot be stored.
func TestAnInstrumentCannotExistWithoutAClassificationState(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var exchangeID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM exchanges ORDER BY mic LIMIT 1`).Scan(&exchangeID); err != nil {
		t.Fatal(err)
	}

	// Omitting the column takes the default rather than storing null.
	if _, err := pool.Exec(ctx, `INSERT INTO instruments
		(id, exchange_id, isin, ticker, name, currency, country, instrument_type, active, purchasability_status)
		VALUES ($1,$2,'SE9999999991','NOSEC','No Sector AB','SEK','SE','common_stock',true,'unverified')`,
		mustNewUUID(t), exchangeID); err != nil {
		t.Fatalf("insert without a sector: %v", err)
	}
	var stored string
	if err := pool.QueryRow(ctx, `SELECT sector FROM instruments WHERE isin = 'SE9999999991'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "unclassified" {
		t.Errorf("an instrument stored with no sector reads %q, expected unclassified", stored)
	}

	// A value outside the vocabulary is refused, so a typo cannot invent a sector.
	if _, err := pool.Exec(ctx, `INSERT INTO instruments
		(id, exchange_id, isin, ticker, name, currency, country, instrument_type, sector, active, purchasability_status)
		VALUES ($1,$2,'SE9999999992','BADSEC','Bad Sector AB','SEK','SE','common_stock','Tehcnology',true,'unverified')`,
		mustNewUUID(t), exchangeID); err == nil {
		t.Error("an instrument was stored with a sector that is not in the vocabulary")
	}

	// And null is refused outright.
	if _, err := pool.Exec(ctx, `INSERT INTO instruments
		(id, exchange_id, isin, ticker, name, currency, country, instrument_type, sector, active, purchasability_status)
		VALUES ($1,$2,'SE9999999993','NULLSEC','Null Sector AB','SEK','SE','common_stock',NULL,true,'unverified')`,
		mustNewUUID(t), exchangeID); err == nil {
		t.Error("an instrument was stored with a null sector")
	}
}

// The upgrade path: a database already carrying the current schema, and whatever free text an
// earlier sync might have stored, ends classified with no manual step.
func TestUpgradeFromTheCurrentSchemaClassifiesEveryInstrument(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	applyMigrationsThrough(t, ctx, pool, 19)

	var exchangeID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM exchanges ORDER BY mic LIMIT 1`).Scan(&exchangeID); err != nil {
		t.Fatal(err)
	}
	// Free text of the kind the unused sync path would have written.
	if _, err := pool.Exec(ctx, `INSERT INTO instruments
		(id, exchange_id, isin, ticker, name, currency, country, instrument_type, sector, active, purchasability_status)
		VALUES ($1,$2,'SE9999999994','FREE','Free Text AB','SEK','SE','common_stock','Technology',true,'unverified')`,
		mustNewUUID(t), exchangeID); err != nil {
		t.Fatal(err)
	}

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("the upgrade needs a manual step: %v", err)
	}

	var nulls, orphans int
	if err := pool.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE sector IS NULL),
		count(*) FILTER (WHERE sector NOT IN (SELECT code FROM sectors))
		FROM instruments`).Scan(&nulls, &orphans); err != nil {
		t.Fatal(err)
	}
	if nulls != 0 || orphans != 0 {
		t.Errorf("after the upgrade %d instruments have no sector and %d have one outside the vocabulary", nulls, orphans)
	}
	// The free text becomes a stated "unclassified" rather than silently surviving as a value
	// the filter would never offer.
	var free string
	if err := pool.QueryRow(ctx, `SELECT sector FROM instruments WHERE isin = 'SE9999999994'`).Scan(&free); err != nil {
		t.Fatal(err)
	}
	if free != "unclassified" && free != "information_technology" {
		t.Errorf("free text 'Technology' became %q", free)
	}
}
