package marketdata_test

import (
	"context"
	"errors"
	"testing"

	"market-lens/server/internal/instruments"
	"market-lens/server/internal/marketdata"

	"github.com/jackc/pgx/v5/pgxpool"
)

// fallbackSource is a scripted provider answering under the fallback's name.
type fallbackSource struct{ *scriptedProvider }

func (fallbackSource) Name() string { return marketdata.FallbackProvider }

func newFallbackSource() fallbackSource { return fallbackSource{newScriptedProvider()} }

// primaryRun stores a primary run for a fallback run to hang from, and returns it.
func primaryRun(t *testing.T, pool *pgxpool.Pool, page marketdata.DailyPage) marketdata.ImportRun {
	t.Helper()
	return primaryRunThrough(t, pool, "2024-04-03", page)
}

// primaryRunThrough is primaryRun over a window ending at `to`.
func primaryRunThrough(t *testing.T, pool *pgxpool.Pool, to string, page marketdata.DailyPage) marketdata.ImportRun {
	t.Helper()
	primary := newScriptedProvider()
	primary.set("NORD.ST", "", page)
	window := target(t, stockholmInstrument, "NORD.ST")
	window.To = session(t, to)
	run, err := marketdata.NewImportService(marketdata.NewRepository(pool), primary).
		Import(context.Background(), importRequest(t, window))
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func fallbackRequest(t *testing.T, parent instruments.UUID) marketdata.ImportRequest {
	t.Helper()
	return marketdata.ImportRequest{
		Kind: marketdata.ImportFallback, Provider: marketdata.FallbackProvider, AppVersion: "test",
		ParentRunID: &parent, Targets: []marketdata.ImportTarget{target(t, stockholmInstrument, "NORD.ST")},
		Workers: 1,
	}
}

func storedProvider(t *testing.T, pool *pgxpool.Pool, date string) (provider, close string) {
	t.Helper()
	if err := pool.QueryRow(context.Background(), `SELECT provider, close::text FROM daily_price_bars
		WHERE instrument_id=$1 AND session_date=$2`, stockholmInstrument, date).Scan(&provider, &close); err != nil {
		t.Fatal(err)
	}
	return provider, close
}

// FR-006. The two sources may adjust differently, so a fallback bar written over a primary one is
// a silent splice — the failure that makes a fallback worse than a gap.
func TestAFallbackImportNeverReplacesAPrimaryBar(t *testing.T) {
	pool := migratedPool(t)
	primary := primaryRun(t, pool, marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-04-02", "51.75", "51.75", 100, "primary-0402"),
	}})

	fallback := newFallbackSource()
	fallback.set("NORD.ST", "", marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-04-02", "99", "99", 100, "fallback-0402"),
		bar(t, "2024-04-03", "53", "53", 100, "fallback-0403"),
	}})
	if _, err := marketdata.NewImportService(marketdata.NewRepository(pool), fallback).
		Import(context.Background(), fallbackRequest(t, primary.ID)); err != nil {
		t.Fatal(err)
	}

	if provider, close := storedProvider(t, pool, "2024-04-02"); provider != "fixture" || close != "51.75000000" {
		t.Errorf("the primary bar became %s %s", provider, close)
	}
	if provider, _ := storedProvider(t, pool, "2024-04-03"); provider != marketdata.FallbackProvider {
		t.Errorf("the missing session was stored from %q", provider)
	}
	assertCounts(t, pool, 2, 0, 0)
}

// FR-007. The primary is the source of record: its bar replaces a fallback one, and the fallback
// bar stays recoverable as a revision.
func TestAPrimaryImportReplacesAFallbackBarAndKeepsIt(t *testing.T) {
	pool := migratedPool(t)
	first := primaryRun(t, pool, marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-04-02", "51.75", "51.75", 100, "primary-0402"),
	}})
	fallback := newFallbackSource()
	fallback.set("NORD.ST", "", marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-04-03", "53.10", "53.10", 100, "fallback-0403"),
	}})
	if _, err := marketdata.NewImportService(marketdata.NewRepository(pool), fallback).
		Import(context.Background(), fallbackRequest(t, first.ID)); err != nil {
		t.Fatal(err)
	}

	primaryRun(t, pool, marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-04-02", "51.75", "51.75", 100, "primary-0402"),
		bar(t, "2024-04-03", "53", "53", 100, "primary-0403"),
	}})

	if provider, close := storedProvider(t, pool, "2024-04-03"); provider != "fixture" || close != "53.00000000" {
		t.Errorf("after the primary returned, the session is %s %s", provider, close)
	}
	var revisedFrom, revisedClose string
	if err := pool.QueryRow(context.Background(), `SELECT provider, close::text FROM price_bar_revisions
		WHERE instrument_id=$1 AND session_date='2024-04-03'`, stockholmInstrument).Scan(&revisedFrom, &revisedClose); err != nil {
		t.Fatalf("the fallback bar was not kept: %v", err)
	}
	if revisedFrom != marketdata.FallbackProvider || revisedClose != "53.10000000" {
		t.Errorf("the kept revision is %s %s", revisedFrom, revisedClose)
	}
}

// FR-008a. The primary stays the only source of corporate actions, even if a fallback page
// carries one.
func TestAFallbackImportStoresNoCorporateActions(t *testing.T) {
	pool := migratedPool(t)
	primary := primaryRun(t, pool, marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-04-02", "51.75", "51.75", 100, "primary-0402"),
	}})
	amount := decimal(t, "2")
	fallback := newFallbackSource()
	fallback.set("NORD.ST", "", marketdata.DailyPage{
		Bars: []marketdata.ProviderBar{bar(t, "2024-04-03", "53", "53", 100, "fallback-0403")},
		Actions: []marketdata.ProviderAction{{ProviderActionID: "div", Type: marketdata.ActionDividend,
			ExDate: session(t, "2024-04-03"), Amount: &amount, Currency: "SEK", SourceHash: "div"}},
	})
	if _, err := marketdata.NewImportService(marketdata.NewRepository(pool), fallback).
		Import(context.Background(), fallbackRequest(t, primary.ID)); err != nil {
		t.Fatal(err)
	}
	assertCounts(t, pool, 2, 0, 0)
}

// FR-010a. The import lock belongs to the instrument, not to the provider asking about it, so a
// fallback can never write an instrument a primary import is writing.
func TestTheImportLockIsSharedAcrossProviders(t *testing.T) {
	pool := migratedPool(t)
	primary := primaryRun(t, pool, marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-04-02", "51.75", "51.75", 100, "primary-0402"),
	}})
	repository := marketdata.NewRepository(pool)
	scope, err := repository.BeginImportScope(context.Background(), "fixture", uuid(t, stockholmInstrument), "daily")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = scope.Rollback(context.Background()) })

	fallback := newFallbackSource()
	fallback.set("NORD.ST", "", marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-04-03", "53", "53", 100, "fallback-0403"),
	}})
	_, err = marketdata.NewImportService(repository, fallback).Import(context.Background(), fallbackRequest(t, primary.ID))
	if !errors.Is(err, marketdata.ErrImportConflict) {
		t.Fatalf("a fallback import ran beside a primary one: %v", err)
	}
}

// A fallback run always covers a primary run, and the fallback provider only ever runs as one —
// so it can never become the source of an ordinary import by misconfiguration.
func TestTheFallbackKindIsOnlyForTheFallbackProvider(t *testing.T) {
	pool := migratedPool(t)
	primary := primaryRun(t, pool, marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-04-02", "51.75", "51.75", 100, "primary-0402"),
	}})
	fallbackService := marketdata.NewImportService(marketdata.NewRepository(pool), newFallbackSource())

	orphan := fallbackRequest(t, primary.ID)
	orphan.ParentRunID = nil
	if _, err := fallbackService.Import(context.Background(), orphan); !errors.Is(err, marketdata.ErrInvalidImport) {
		t.Errorf("a fallback run without a parent: %v", err)
	}

	ordinary := fallbackRequest(t, primary.ID)
	ordinary.Kind, ordinary.ParentRunID = marketdata.ImportDailyUpdate, nil
	if _, err := fallbackService.Import(context.Background(), ordinary); !errors.Is(err, marketdata.ErrInvalidImport) {
		t.Errorf("the fallback provider ran an ordinary import: %v", err)
	}

	primaryService := marketdata.NewImportService(marketdata.NewRepository(pool), newScriptedProvider())
	disguised := fallbackRequest(t, primary.ID)
	disguised.Provider = "fixture"
	if _, err := primaryService.Import(context.Background(), disguised); !errors.Is(err, marketdata.ErrInvalidImport) {
		t.Errorf("the primary ran as a fallback: %v", err)
	}
}

// Feature 017's findings are the primary's to raise and the owner's to settle. A clean fallback bar
// is not evidence that a condition the primary reported has passed, and a fallback's own rejection
// is not a condition anybody should be asked to decide about — so a fallback import neither settles
// nor raises one.
func TestAFallbackImportNeitherSettlesNorRaisesFindings(t *testing.T) {
	pool := migratedPool(t)
	// The primary's 04-03 closes above its high, so it is rejected and a finding is raised.
	primary := primaryRun(t, pool, marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-04-02", "51.75", "51.75", 100, "primary-0402"),
		bar(t, "2024-04-03", "120", "120", 100, "primary-0403-invalid"),
	}})
	openFindings := func() int {
		t.Helper()
		var count int
		if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM data_quality_findings
			WHERE instrument_id=$1 AND status='open'`, stockholmInstrument).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	before := openFindings()
	if before == 0 {
		t.Fatal("the fixture raised no finding, so this proves nothing")
	}

	// The fallback answers 04-03 cleanly, and 04-04 invalidly.
	fallback := newFallbackSource()
	fallback.set("NORD.ST", "", marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-04-03", "53", "53", 100, "fallback-0403"),
		bar(t, "2024-04-04", "130", "130", 100, "fallback-0404-invalid"),
	}})
	request := fallbackRequest(t, primary.ID)
	request.Targets[0].To = session(t, "2024-04-04")
	if _, err := marketdata.NewImportService(marketdata.NewRepository(pool), fallback).
		Import(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if after := openFindings(); after != before {
		t.Errorf("a fallback import changed the open findings from %d to %d", before, after)
	}
	var resolved int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM data_quality_findings
		WHERE instrument_id=$1 AND status='resolved'`, stockholmInstrument).Scan(&resolved); err != nil {
		t.Fatal(err)
	}
	if resolved != 0 {
		t.Errorf("a fallback import settled %d of the primary's findings", resolved)
	}
}
