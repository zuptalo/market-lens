package instruments_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"market-lens/server/internal/db"
	"market-lens/server/internal/instruments"
	"market-lens/server/internal/testdb"
)

func TestSynchronizeUniverseIsIdempotentAndRetainsRemovedIdentity(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	service := instruments.NewService(instruments.NewRepository(pool))
	selectionDate := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)

	request := instruments.SyncRequest{
		Provider: "fixture", UniverseCode: "nordic-test", UniverseName: "Nordic Test",
		Description: "Reviewed fixture universe", SelectionDate: selectionDate, AppVersion: "test",
		Listings: []instruments.SyncListing{
			listing("XSTO", "SE", "SEK", "Europe/Stockholm", "SE0000000001", "SAME", "Swedish Listing", "SAME.ST"),
			listing("XCSE", "DK", "DKK", "Europe/Copenhagen", "DK0000000001", "SAME", "Danish Listing", "SAME.CO"),
		},
	}
	first, err := service.SyncUniverse(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != "succeeded" || first.Counts.Processed != 2 || first.Counts.Accepted != 2 || len(first.InstrumentIDs) != 2 {
		t.Fatalf("unexpected first result: %#v", first)
	}
	stockholmID := first.InstrumentIDs["SAME.ST"]
	copenhagenID := first.InstrumentIDs["SAME.CO"]
	if !stockholmID.Valid() || !copenhagenID.Valid() || stockholmID == copenhagenID {
		t.Fatalf("unstable identities: %q %q", stockholmID, copenhagenID)
	}

	request.Listings = request.Listings[:1]
	request.Listings[0].Name = "Swedish Listing Renamed"
	second, err := service.SyncUniverse(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if second.InstrumentIDs["SAME.ST"] != stockholmID {
		t.Fatalf("instrument UUID changed from %s to %s", stockholmID, second.InstrumentIDs["SAME.ST"])
	}

	var name string
	var stockholmActive, copenhagenActive, copenhagenMappingActive bool
	if err := pool.QueryRow(ctx, `SELECT name, active FROM instruments WHERE id = $1`, stockholmID.String()).Scan(&name, &stockholmActive); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT i.active, p.active FROM instruments i JOIN provider_instruments p ON p.instrument_id = i.id WHERE i.id = $1`, copenhagenID.String()).Scan(&copenhagenActive, &copenhagenMappingActive); err != nil {
		t.Fatal(err)
	}
	if name != "Swedish Listing Renamed" || !stockholmActive || copenhagenActive || copenhagenMappingActive {
		t.Fatalf("metadata/inactivation mismatch: name=%q stockholm=%t copenhagen=%t mapping=%t", name, stockholmActive, copenhagenActive, copenhagenMappingActive)
	}

	var memberships, runs int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM universe_memberships m
		JOIN research_universes u ON u.id=m.universe_id WHERE u.code='nordic-test'`).Scan(&memberships); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM import_runs WHERE kind = 'universe_sync' AND status = 'succeeded' AND error_summary IS NULL`).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if memberships != 2 || runs != 2 {
		t.Fatalf("retained memberships=%d immutable runs=%d", memberships, runs)
	}
	if _, err := pool.Exec(ctx, `UPDATE import_runs SET accepted_count = 99 WHERE id = $1`, first.RunID.String()); err == nil {
		t.Fatal("terminal universe_sync run was mutable")
	}
}

func TestSynchronizeUniverseRejectsConflictingProviderIdentitySafely(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	service := instruments.NewService(instruments.NewRepository(pool))
	request := instruments.SyncRequest{
		Provider: "fixture", UniverseCode: "nordic-test", UniverseName: "Nordic Test",
		SelectionDate: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC), AppVersion: "test",
		Listings: []instruments.SyncListing{listing("XSTO", "SE", "SEK", "Europe/Stockholm", "SE0000000001", "AAA", "First", "AAA.ST")},
	}
	if _, err := service.SyncUniverse(ctx, request); err != nil {
		t.Fatal(err)
	}
	request.Listings[0].ISIN = "SE0000000002"
	request.Listings[0].Ticker = "BBB"
	request.Listings[0].Name = "Conflicting"
	_, err := service.SyncUniverse(ctx, request)
	if !errors.Is(err, instruments.ErrIdentityConflict) {
		t.Fatalf("conflicting sync error = %v", err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "fixture") || strings.Contains(err.Error(), request.Listings[0].ProviderSymbol) {
		t.Fatalf("conflict error exposed provider details: %v", err)
	}

	var runStatus, errorCode, errorSummary string
	if err := pool.QueryRow(ctx, `SELECT status, error_code, error_summary FROM import_runs ORDER BY started_at DESC LIMIT 1`).Scan(&runStatus, &errorCode, &errorSummary); err != nil {
		t.Fatal(err)
	}
	if runStatus != "failed" || errorCode != "identity_conflict" || errorSummary == "" || strings.Contains(errorSummary, request.Listings[0].ProviderSymbol) {
		t.Fatalf("unsafe failed run: status=%q code=%q summary=%q", runStatus, errorCode, errorSummary)
	}
}

func listing(mic, country, currency, timezone, isin, ticker, name, providerSymbol string) instruments.SyncListing {
	return instruments.SyncListing{
		MIC: mic, ExchangeName: mic + " Exchange", ExchangeCountry: country, ExchangeCurrency: currency,
		ExchangeTimezone: timezone, ISIN: isin, Ticker: ticker, Name: name, Currency: currency, Country: country,
		Type: instruments.InstrumentTypeCommonStock, PurchasabilityStatus: instruments.PurchasabilityUnverified,
		ProviderSymbol: providerSymbol, CurationSource: "fixture review", CurationNote: "approved for deterministic tests",
	}
}
