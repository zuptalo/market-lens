package marketdata_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"market-lens/server/internal/marketdata"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// countingFallback records which symbols it was asked about, and over which window.
type countingFallback struct {
	fallbackSource
	mu    sync.Mutex
	asked map[string]marketdata.DailyRequest
}

func newCountingFallback() *countingFallback {
	return &countingFallback{fallbackSource: newFallbackSource(), asked: map[string]marketdata.DailyRequest{}}
}

func (c *countingFallback) Daily(ctx context.Context, request marketdata.DailyRequest) (marketdata.DailyPage, error) {
	c.mu.Lock()
	c.asked[request.ProviderSymbol] = request
	c.mu.Unlock()
	return c.fallbackSource.Daily(ctx, request)
}

// tellings records what the fallback said to the notification side, in the observing transaction.
type tellings struct{ changes []marketdata.FallbackChange }

func (r *tellings) tell(_ context.Context, _ pgx.Tx, change marketdata.FallbackChange) error {
	r.changes = append(r.changes, change)
	return nil
}

func newFallback(t *testing.T, pool *pgxpool.Pool, source marketdata.Provider) (*marketdata.Fallback, *tellings) {
	t.Helper()
	repository := marketdata.NewRepository(pool)
	told := &tellings{}
	fallback := marketdata.NewFallback(repository, marketdata.NewImportService(repository, source))
	fallback.Tell = told.tell
	return fallback, told
}

// A primary run over two instruments whose bars stop at 2024-03-28: the first instrument then
// refuses authentication, the second is rate limited.
func lapsedPrimaryRun(t *testing.T, pool *pgxpool.Pool) marketdata.ImportRun {
	t.Helper()
	primary := newScriptedProvider()
	primary.set("INVE-B.ST", "", marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-03-28", "100.5", "100.5", 100, "inve-0328")}})
	primary.set("VOLV-B.ST", "", marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-03-28", "80", "80", 100, "volv-0328")}})
	service := marketdata.NewImportService(marketdata.NewRepository(pool), primary)
	history := marketdata.ImportRequest{Kind: marketdata.ImportBackfill, Provider: "fixture", AppVersion: "test",
		Workers: 1, Targets: []marketdata.ImportTarget{
			target(t, stockholmInstrument, "INVE-B.ST"), target(t, stockholmSecond, "VOLV-B.ST")}}
	for index := range history.Targets {
		history.Targets[index].To = session(t, "2024-03-28")
	}
	if _, err := service.Import(context.Background(), history); err != nil {
		t.Fatal(err)
	}

	primary.fail("INVE-B.ST", &marketdata.ProviderError{Code: "provider_authentication", Summary: "x"})
	primary.fail("VOLV-B.ST", &marketdata.ProviderError{Code: "provider_rate_limited", Summary: "x"})
	nightly := history
	nightly.Kind = marketdata.ImportDailyUpdate
	for index := range nightly.Targets {
		nightly.Targets[index].From = session(t, "2024-04-01")
		nightly.Targets[index].To = session(t, "2024-04-03")
	}
	run, err := service.Import(context.Background(), nightly)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != marketdata.ImportFailed {
		t.Fatalf("the lapsed run ended %s", run.Status)
	}
	return run
}

func coverOptions() marketdata.CoverOptions {
	return marketdata.CoverOptions{AppVersion: "test", Workers: 2, MaxRetries: 0}
}

func TestASwitchedOffFallbackAsksNobody(t *testing.T) {
	pool := migratedPool(t)
	lapsed := lapsedPrimaryRun(t, pool)
	source := newCountingFallback()
	fallback, _ := newFallback(t, pool, source)

	_, covered, err := fallback.Cover(context.Background(), lapsed, coverOptions())
	if err != nil {
		t.Fatal(err)
	}
	if covered || len(source.asked) != 0 {
		t.Errorf("a switched-off fallback covered=%v and asked about %v", covered, source.asked)
	}
}

// FR-001, FR-002, FR-008. Only the instrument that refused authentication is asked about, and
// only for the sessions after its newest primary price, in a run recorded as the primary run's
// child.
func TestTheFallbackCoversOnlyWhatAuthenticationRefused(t *testing.T) {
	pool := migratedPool(t)
	lapsed := lapsedPrimaryRun(t, pool)
	source := newCountingFallback()
	source.set("INVE-B.ST", "", marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-04-02", "101", "101", 100, "fb-inve-0402"),
		bar(t, "2024-04-03", "102", "102", 100, "fb-inve-0403"),
	}})
	fallback, _ := newFallback(t, pool, source)
	if err := fallback.SetEnabled(context.Background(), true); err != nil {
		t.Fatal(err)
	}

	run, covered, err := fallback.Cover(context.Background(), lapsed, coverOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !covered {
		t.Fatal("an authentication failure was not covered")
	}
	if len(source.asked) != 1 {
		t.Fatalf("the fallback was asked about %v", source.asked)
	}
	window := source.asked["INVE-B.ST"]
	if window.From.String() != "2024-03-29" || window.To.String() != "2024-04-03" {
		t.Errorf("the fallback was asked for %s..%s, want the sessions after the newest primary price", window.From, window.To)
	}
	var kind, provider, parent string
	if err := pool.QueryRow(context.Background(), `SELECT kind, provider, parent_run_id::text FROM import_runs
		WHERE id=$1`, run.ID.String()).Scan(&kind, &provider, &parent); err != nil {
		t.Fatal(err)
	}
	if kind != "fallback" || provider != marketdata.FallbackProvider || parent != lapsed.ID.String() {
		t.Errorf("the fallback run is %s/%s under %s", kind, provider, parent)
	}
	if got, _ := storedProvider(t, pool, "2024-04-03"); got != marketdata.FallbackProvider {
		t.Errorf("the missing session came from %q", got)
	}
}

// A run with nothing refused has nothing to cover.
func TestNothingRefusedMeansNothingCovered(t *testing.T) {
	pool := migratedPool(t)
	primary := primaryRun(t, pool, marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-04-02", "51.75", "51.75", 100, "primary-0402")}})
	source := newCountingFallback()
	fallback, _ := newFallback(t, pool, source)
	if err := fallback.SetEnabled(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if _, covered, err := fallback.Cover(context.Background(), primary, coverOptions()); err != nil || covered {
		t.Errorf("covered=%v err=%v", covered, err)
	}
	if len(source.asked) != 0 {
		t.Errorf("asked about %v", source.asked)
	}
}

// FR-029. When the fallback fails too, nothing is written and the failure is the fallback run's.
func TestWhenBothFailNothingIsWritten(t *testing.T) {
	pool := migratedPool(t)
	lapsed := lapsedPrimaryRun(t, pool)
	source := newCountingFallback()
	source.fail("INVE-B.ST", &marketdata.ProviderError{Code: "provider_unavailable", Summary: "x", Transient: true})
	fallback, _ := newFallback(t, pool, source)
	if err := fallback.SetEnabled(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	run, _, err := fallback.Cover(context.Background(), lapsed, coverOptions())
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != marketdata.ImportFailed || run.Error == nil || run.Error.Code != "provider_unavailable" {
		t.Errorf("the fallback run = %s %#v", run.Status, run.Error)
	}
	assertCounts(t, pool, 2, 0, 0)
}

func storeFallbackBar(t *testing.T, pool *pgxpool.Pool, parent marketdata.ImportRun, date string) {
	t.Helper()
	source := newFallbackSource()
	source.set("NORD.ST", "", marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, date, "53.10", "53.10", 100, "fallback-"+date)}})
	if _, err := marketdata.NewImportService(marketdata.NewRepository(pool), source).
		Import(context.Background(), fallbackRequest(t, parent.ID)); err != nil {
		t.Fatal(err)
	}
}

func fallbackEvents(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM client_events
		WHERE event_type = 'market_data_fallback.changed.v1' AND scope = 'shared'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// FR-015 – FR-019. The state is derived from the bars, recorded once per change, published as an
// event and told once — and a quiet repetition says nothing.
func TestAFallbackStateChangeIsToldOnce(t *testing.T) {
	pool := migratedPool(t)
	ctx := context.Background()
	fallback, told := newFallback(t, pool, newFallbackSource())

	state, changed, err := fallback.Observe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if changed || state.Active || fallbackEvents(t, pool) != 0 {
		t.Fatalf("a quiet install changed=%v state=%#v", changed, state)
	}

	primary := primaryRun(t, pool, marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-04-02", "51.75", "51.75", 100, "primary-0402")}})
	storeFallbackBar(t, pool, primary, "2024-04-03")

	state, changed, err = fallback.Observe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || !state.Active || state.Instruments != 1 || state.Since == nil || state.Since.String() != "2024-04-03" ||
		state.PendingReconciliation {
		t.Fatalf("after a fallback bar: changed=%v state=%#v", changed, state)
	}
	if fallbackEvents(t, pool) != 1 || len(told.changes) != 1 || !told.changes[0].Entered {
		t.Fatalf("events=%d tellings=%#v", fallbackEvents(t, pool), told.changes)
	}

	if _, changed, err = fallback.Observe(ctx); err != nil || changed {
		t.Fatalf("a repeated observation changed=%v err=%v", changed, err)
	}
	if fallbackEvents(t, pool) != 1 || len(told.changes) != 1 {
		t.Fatalf("a repeated observation said something: events=%d tellings=%d", fallbackEvents(t, pool), len(told.changes))
	}

	// The primary returns with a newer session but has not replaced the fallback one yet.
	primaryRunThrough(t, pool, "2024-04-04", marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-04-02", "51.75", "51.75", 100, "primary-0402"),
		bar(t, "2024-04-04", "54", "54", 100, "primary-0404")}})
	state, changed, err = fallback.Observe(ctx)
	if err != nil || !changed || !state.Active || !state.PendingReconciliation {
		t.Fatalf("after the primary returned: changed=%v state=%#v err=%v", changed, state, err)
	}
	if len(told.changes) != 1 {
		t.Errorf("reconciliation pending was told as a notice: %#v", told.changes)
	}

	// Reconciled: the primary replaces the fallback bar.
	primaryRun(t, pool, marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-04-03", "53", "53", 100, "primary-0403")}})
	state, changed, err = fallback.Observe(ctx)
	if err != nil || !changed || state.Active || state.Instruments != 0 || state.Since != nil {
		t.Fatalf("after reconciliation: changed=%v state=%#v err=%v", changed, state, err)
	}
	if len(told.changes) != 2 || !told.changes[1].Ended {
		t.Fatalf("the end was not told: %#v", told.changes)
	}
	if fallbackEvents(t, pool) != 3 {
		t.Errorf("%d events for three changes", fallbackEvents(t, pool))
	}
}

// A telling that cannot be written rolls the change back, so it is tried again next time rather
// than recorded as told.
func TestAFailedTellingLeavesTheChangeUnrecorded(t *testing.T) {
	pool := migratedPool(t)
	ctx := context.Background()
	fallback, _ := newFallback(t, pool, newFallbackSource())
	fallback.Tell = func(context.Context, pgx.Tx, marketdata.FallbackChange) error { return errors.New("no") }
	primary := primaryRun(t, pool, marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-04-02", "51.75", "51.75", 100, "primary-0402")}})
	storeFallbackBar(t, pool, primary, "2024-04-03")

	if _, _, err := fallback.Observe(ctx); err == nil {
		t.Fatal("a failed telling was reported as observed")
	}
	state, err := fallback.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Recorded.Active || fallbackEvents(t, pool) != 0 {
		t.Errorf("the change was recorded without being told: %#v", state)
	}
}

// FR-020. Reconciliation asks the primary, under its own symbols, for exactly the span each
// instrument's fallback bars cover.
func TestReconciliationAsksThePrimaryForTheFallbackSpan(t *testing.T) {
	pool := migratedPool(t)
	primary := primaryRun(t, pool, marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-04-02", "51.75", "51.75", 100, "primary-0402")}})
	storeFallbackBar(t, pool, primary, "2024-04-03")
	fallback, _ := newFallback(t, pool, newFallbackSource())

	targets, err := fallback.ReconcileTargets(context.Background(), "eodhd")
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("targets = %#v", targets)
	}
	got := targets[0]
	if got.InstrumentID.String() != stockholmInstrument || got.ProviderSymbol != "INVE-B.ST" || got.Currency != "SEK" ||
		got.From.String() != "2024-04-03" || got.To.String() != "2024-04-03" {
		t.Errorf("target = %#v", got)
	}
}

// FR-011. The audit's stored evidence is each current member's fallback symbol and its newest
// primary close — a fallback bar is never evidence for itself.
func TestTheAuditReadsTheNewestPrimaryClose(t *testing.T) {
	pool := migratedPool(t)
	primary := primaryRun(t, pool, marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-04-02", "51.75", "51.75", 100, "primary-0402")}})
	storeFallbackBar(t, pool, primary, "2024-04-03")

	entries, err := marketdata.NewRepository(pool).FallbackAuditEntries(context.Background(), "nordic-liquid-v1")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 100 {
		t.Fatalf("%d entries for the seeded universe", len(entries))
	}
	var found bool
	for _, entry := range entries {
		if entry.Ticker != "INVE-B" {
			if entry.StoredClose != nil {
				t.Errorf("%s has a stored close with no bars", entry.Ticker)
			}
			continue
		}
		found = true
		if entry.Symbol != "INVE-B.ST" || entry.MIC != "XSTO" || entry.Currency != "SEK" ||
			entry.Session.String() != "2024-04-02" || entry.StoredClose == nil || entry.StoredClose.String() != "51.75" {
			t.Errorf("INVE-B evidence = %#v", entry)
		}
	}
	if !found {
		t.Error("INVE-B is missing from the audit")
	}
}
