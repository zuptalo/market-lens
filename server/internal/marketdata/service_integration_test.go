package marketdata_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"market-lens/server/internal/db"
	"market-lens/server/internal/instruments"
	"market-lens/server/internal/marketdata"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	stockholmInstrument = "22000000-0000-4000-8000-000000000001"
	stockholmSecond     = "22000000-0000-4000-8000-000000000002"
	stockholmThird      = "22000000-0000-4000-8000-000000000003"
)

func TestImportServiceIsIdempotentAndPreservesCorrectionsAndActions(t *testing.T) {
	pool := migratedPool(t)
	provider := newScriptedProvider()
	provider.set("NORD.ST", "", marketdata.DailyPage{
		Bars: []marketdata.ProviderBar{
			bar(t, "2024-03-28", "100.5", "50.25", 125000, "bar-1"),
			bar(t, "2024-04-02", "51.75", "51.75", 180000, "bar-2"),
		},
		NextCursor: "page-2",
	})
	ratio := decimal(t, "2")
	provider.set("NORD.ST", "page-2", marketdata.DailyPage{
		Bars: []marketdata.ProviderBar{
			bar(t, "2024-04-02", "51.75", "51.75", 180000, "bar-2"),
			bar(t, "2024-04-03", "53", "53", 181000, "bar-3"),
		},
		Actions: []marketdata.ProviderAction{{
			ProviderActionID: "split-1", Type: marketdata.ActionSplit,
			ExDate: session(t, "2024-04-02"), Ratio: &ratio, SourceHash: "action-1",
		}},
	})

	repository := marketdata.NewRepository(pool)
	service := marketdata.NewImportService(repository, provider)
	request := importRequest(t, target(t, stockholmInstrument, "NORD.ST"))

	first, err := service.Import(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != marketdata.ImportSucceeded || first.Counts.Processed != 4 || first.Counts.Accepted != 4 {
		t.Fatalf("first run = %#v", first)
	}
	assertCounts(t, pool, 3, 0, 1)
	assertCurrentBar(t, pool, stockholmInstrument, "2024-04-02", "51.75000000", first.ID.String())

	replay, err := service.Import(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Status != marketdata.ImportSucceeded || replay.Counts != first.Counts {
		t.Fatalf("replay run = %#v, first = %#v", replay, first)
	}
	assertCounts(t, pool, 3, 0, 1)
	assertCurrentBar(t, pool, stockholmInstrument, "2024-04-02", "51.75000000", first.ID.String())

	corrected := bar(t, "2024-04-02", "52", "52", 181250, "bar-2-corrected")
	provider.set("NORD.ST", "", marketdata.DailyPage{Bars: []marketdata.ProviderBar{corrected}})
	correctedRun, err := service.Import(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	assertCounts(t, pool, 3, 1, 1)
	assertCurrentBar(t, pool, stockholmInstrument, "2024-04-02", "52.00000000", correctedRun.ID.String())

	var priorClose, priorRun, supersedingRun string
	var revision int
	if err := pool.QueryRow(context.Background(), `SELECT close::text, import_run_id::text,
		superseding_run_id::text, revision FROM price_bar_revisions
		WHERE instrument_id=$1 AND session_date='2024-04-02'`, stockholmInstrument).
		Scan(&priorClose, &priorRun, &supersedingRun, &revision); err != nil {
		t.Fatal(err)
	}
	if priorClose != "51.75000000" || priorRun != first.ID.String() || supersedingRun != correctedRun.ID.String() || revision != 1 {
		t.Fatalf("revision = close %s prior %s superseding %s number %d", priorClose, priorRun, supersedingRun, revision)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE price_bar_revisions SET close=1
		WHERE instrument_id=$1 AND session_date='2024-04-02'`, stockholmInstrument); err == nil {
		t.Fatal("immutable correction revision accepted an update")
	}

	var actionRun, actionHash string
	if err := pool.QueryRow(context.Background(), `SELECT import_run_id::text, source_hash FROM corporate_actions
		WHERE provider='fixture' AND provider_action_id='split-1'`).Scan(&actionRun, &actionHash); err != nil {
		t.Fatal(err)
	}
	if actionRun != first.ID.String() || actionHash != "action-1" {
		t.Fatalf("action provenance = run %s hash %s", actionRun, actionHash)
	}
}

func TestImportServiceCommitsEachInstrumentIndependently(t *testing.T) {
	pool := migratedPool(t)
	provider := newScriptedProvider()
	provider.set("GOOD.ST", "", marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-04-02", "51.75", "51.75", 100, "good"),
	}})
	provider.fail("BAD.ST", errors.New("upstream token=must-not-survive"))

	service := marketdata.NewImportService(marketdata.NewRepository(pool), provider)
	request := importRequest(t,
		target(t, stockholmInstrument, "GOOD.ST"),
		target(t, stockholmSecond, "BAD.ST"),
	)
	run, err := service.Import(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != marketdata.ImportPartial || run.Counts.Processed != 1 || run.Counts.Accepted != 1 {
		t.Fatalf("partial run = %#v", run)
	}

	var bars int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM daily_price_bars`).Scan(&bars); err != nil {
		t.Fatal(err)
	}
	if bars != 1 {
		t.Fatalf("committed bars = %d, want 1", bars)
	}
	var status, code, summary string
	if err := pool.QueryRow(context.Background(), `SELECT status, error_code, error_summary FROM import_items
		WHERE run_id=$1 AND instrument_id=$2`, run.ID.String(), stockholmSecond).Scan(&status, &code, &summary); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || code != "provider_authentication" || summary != "Market-data provider authentication failed." {
		t.Fatalf("failed item = %s %s %q", status, code, summary)
	}
}

func TestImportServiceRejectsAdvisoryLockConflict(t *testing.T) {
	pool := migratedPool(t)
	repository := marketdata.NewRepository(pool)
	instrumentID := uuid(t, stockholmInstrument)
	scope, err := repository.BeginImportScope(context.Background(), "fixture", instrumentID, "daily")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = scope.Rollback(context.Background()) })

	provider := newScriptedProvider()
	provider.set("NORD.ST", "", marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-04-02", "51.75", "51.75", 100, "locked"),
	}})
	service := marketdata.NewImportService(repository, provider)
	_, err = service.Import(context.Background(), importRequest(t, target(t, stockholmInstrument, "NORD.ST")))
	if !errors.Is(err, marketdata.ErrImportConflict) {
		t.Fatalf("error = %v, want advisory-lock conflict", err)
	}

	var bars int
	if queryErr := pool.QueryRow(context.Background(), `SELECT count(*) FROM daily_price_bars`).Scan(&bars); queryErr != nil {
		t.Fatal(queryErr)
	}
	if bars != 0 {
		t.Fatalf("bars committed despite lock conflict: %d", bars)
	}
}

func TestImportServiceBoundsWorkersAndCancelsOutstandingScopes(t *testing.T) {
	t.Run("bounded workers", func(t *testing.T) {
		pool := migratedPool(t)
		provider := &concurrencyProvider{delay: 75 * time.Millisecond}
		service := marketdata.NewImportService(marketdata.NewRepository(pool), provider)
		request := importRequest(t,
			target(t, stockholmInstrument, "ONE.ST"),
			target(t, stockholmSecond, "TWO.ST"),
			target(t, stockholmThird, "THREE.ST"),
		)
		request.Workers = 2
		run, err := service.Import(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status != marketdata.ImportSucceeded {
			t.Fatalf("run status = %s", run.Status)
		}
		if got := provider.maximum.Load(); got != 2 {
			t.Fatalf("maximum concurrent provider calls = %d, want 2", got)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		pool := migratedPool(t)
		provider := &concurrencyProvider{waitForCancellation: true}
		service := marketdata.NewImportService(marketdata.NewRepository(pool), provider)
		request := importRequest(t,
			target(t, stockholmInstrument, "ONE.ST"),
			target(t, stockholmSecond, "TWO.ST"),
		)
		request.Workers = 1
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(50*time.Millisecond, cancel)
		run, err := service.Import(ctx, request)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context cancellation", err)
		}
		if run.Status != marketdata.ImportCancelled {
			t.Fatalf("run status = %s, want cancelled", run.Status)
		}
		var cancelled int
		if queryErr := pool.QueryRow(context.Background(), `SELECT count(*) FROM import_items
			WHERE run_id=$1 AND status='cancelled'`, run.ID.String()).Scan(&cancelled); queryErr != nil {
			t.Fatal(queryErr)
		}
		if cancelled != 2 {
			t.Fatalf("cancelled items = %d, want 2", cancelled)
		}
	})
}

func TestRepositoryLoadsActiveUniverseImportTargets(t *testing.T) {
	pool := migratedPool(t)
	repository := marketdata.NewRepository(pool)
	targets, err := repository.TargetsForUniverse(context.Background(), "eodhd", "nordic-liquid-v1")
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 100 {
		t.Fatalf("targets = %d, want 100", len(targets))
	}
	seen := make(map[instruments.UUID]struct{}, len(targets))
	for _, target := range targets {
		if !target.InstrumentID.Valid() || target.ProviderSymbol == "" || len(target.Currency) != 3 ||
			target.From != "" || target.To != "" {
			t.Fatalf("invalid import target: %#v", target)
		}
		if _, duplicate := seen[target.InstrumentID]; duplicate {
			t.Fatalf("duplicate target: %s", target.InstrumentID)
		}
		seen[target.InstrumentID] = struct{}{}
	}

	if _, err := pool.Exec(context.Background(), `UPDATE provider_instruments SET active=false
		WHERE provider='eodhd' AND instrument_id=$1`, stockholmInstrument); err != nil {
		t.Fatal(err)
	}
	targets, err = repository.TargetsForUniverse(context.Background(), "eodhd", "nordic-liquid-v1")
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 99 {
		t.Fatalf("targets after inactive mapping = %d, want 99", len(targets))
	}
}

type concurrencyProvider struct {
	delay               time.Duration
	waitForCancellation bool
	active              atomic.Int32
	maximum             atomic.Int32
}

func (p *concurrencyProvider) Name() string { return "fixture" }

func (p *concurrencyProvider) Resolve(context.Context, marketdata.ResolveRequest) (marketdata.ResolvedInstrument, error) {
	return marketdata.ResolvedInstrument{}, errors.New("resolve is not used by import tests")
}

func (p *concurrencyProvider) Daily(ctx context.Context, request marketdata.DailyRequest) (marketdata.DailyPage, error) {
	active := p.active.Add(1)
	defer p.active.Add(-1)
	for {
		maximum := p.maximum.Load()
		if active <= maximum || p.maximum.CompareAndSwap(maximum, active) {
			break
		}
	}
	if p.waitForCancellation {
		<-ctx.Done()
		return marketdata.DailyPage{}, ctx.Err()
	}
	timer := time.NewTimer(p.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return marketdata.DailyPage{}, ctx.Err()
	case <-timer.C:
	}
	return marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		barWithoutTest("2024-04-02", request.ProviderSymbol),
	}}, nil
}

func barWithoutTest(date, hash string) marketdata.ProviderBar {
	session, _ := marketdata.ParseSessionDate(date)
	open, _ := marketdata.ParseDecimal("50.5")
	high, _ := marketdata.ParseDecimal("52")
	low, _ := marketdata.ParseDecimal("50")
	closeValue, _ := marketdata.ParseDecimal("51.75")
	return marketdata.ProviderBar{SessionDate: session, Open: open, High: high, Low: low,
		Close: closeValue, Volume: 100, SourceHash: hash}
}

type scriptedProvider struct {
	mu     sync.Mutex
	pages  map[string]marketdata.DailyPage
	errors map[string]error
}

func newScriptedProvider() *scriptedProvider {
	return &scriptedProvider{pages: make(map[string]marketdata.DailyPage), errors: make(map[string]error)}
}

func (p *scriptedProvider) Name() string { return "fixture" }

func (p *scriptedProvider) Resolve(context.Context, marketdata.ResolveRequest) (marketdata.ResolvedInstrument, error) {
	return marketdata.ResolvedInstrument{}, errors.New("resolve is not used by import tests")
}

func (p *scriptedProvider) Daily(_ context.Context, request marketdata.DailyRequest) (marketdata.DailyPage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.errors[request.ProviderSymbol]; err != nil {
		return marketdata.DailyPage{}, err
	}
	return p.pages[request.ProviderSymbol+"|"+request.Cursor], nil
}

func (p *scriptedProvider) set(symbol, cursor string, page marketdata.DailyPage) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pages[symbol+"|"+cursor] = page
}

func (p *scriptedProvider) fail(symbol string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.errors[symbol] = err
}

func (p *scriptedProvider) recover(symbol string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.errors, symbol)
}

func migratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

func importRequest(t *testing.T, targets ...marketdata.ImportTarget) marketdata.ImportRequest {
	t.Helper()
	return marketdata.ImportRequest{
		Kind: marketdata.ImportBackfill, Provider: "fixture", AppVersion: "test",
		Targets: targets, MaxRetries: 0, Workers: 1,
	}
}

func target(t *testing.T, id, symbol string) marketdata.ImportTarget {
	t.Helper()
	return marketdata.ImportTarget{
		InstrumentID: uuid(t, id), ProviderSymbol: symbol, Currency: "SEK",
		From: session(t, "2024-03-28"), To: session(t, "2024-04-03"),
	}
}

func bar(t *testing.T, date, close, adjusted string, volume int64, hash string) marketdata.ProviderBar {
	t.Helper()
	adjustedValue := decimal(t, adjusted)
	return marketdata.ProviderBar{
		SessionDate: session(t, date), Open: decimal(t, "50.5"), High: decimal(t, "110"),
		Low: decimal(t, "50"), Close: decimal(t, close), AdjustedClose: &adjustedValue,
		Volume: volume, SourceHash: hash,
	}
}

func session(t *testing.T, value string) marketdata.SessionDate {
	t.Helper()
	result, err := marketdata.ParseSessionDate(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func decimal(t *testing.T, value string) marketdata.Decimal {
	t.Helper()
	result, err := marketdata.ParseDecimal(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func uuid(t *testing.T, value string) instruments.UUID {
	t.Helper()
	result, err := instruments.ParseUUID(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func assertCounts(t *testing.T, pool *pgxpool.Pool, bars, revisions, actions int) {
	t.Helper()
	var gotBars, gotRevisions, gotActions int
	if err := pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM daily_price_bars),
		(SELECT count(*) FROM price_bar_revisions),
		(SELECT count(*) FROM corporate_actions)`).Scan(&gotBars, &gotRevisions, &gotActions); err != nil {
		t.Fatal(err)
	}
	if gotBars != bars || gotRevisions != revisions || gotActions != actions {
		t.Fatalf("stored counts = bars %d revisions %d actions %d", gotBars, gotRevisions, gotActions)
	}
}

func assertCurrentBar(t *testing.T, pool *pgxpool.Pool, instrumentID, date, close, runID string) {
	t.Helper()
	var gotClose, gotRun string
	if err := pool.QueryRow(context.Background(), `SELECT close::text, import_run_id::text
		FROM daily_price_bars WHERE instrument_id=$1 AND session_date=$2`, instrumentID, date).Scan(&gotClose, &gotRun); err != nil {
		t.Fatal(err)
	}
	if gotClose != close || gotRun != runID {
		t.Fatalf("current bar = close %s run %s, want close %s run %s", gotClose, gotRun, close, runID)
	}
}

// Corporate actions were recorded silently while every neighbouring write published an
// event. That made them invisible to a connected client, which is precisely what the
// constitution's live-update principle forbids for a client-visible committed change.
func TestImportPublishesAnEventWhenItRecordsACorporateAction(t *testing.T) {
	pool := migratedPool(t)
	provider := newScriptedProvider()
	ratio := decimal(t, "2")
	provider.set("NORD.ST", "", marketdata.DailyPage{
		Bars: []marketdata.ProviderBar{
			bar(t, "2024-03-28", "100.5", "50.25", 125000, "bar-1"),
			bar(t, "2024-04-02", "51.75", "51.75", 180000, "bar-2"),
		},
		Actions: []marketdata.ProviderAction{{
			ProviderActionID: "split-1", Type: marketdata.ActionSplit,
			ExDate: session(t, "2024-04-02"), Ratio: &ratio, SourceHash: "action-1",
		}},
	})

	service := marketdata.NewImportService(marketdata.NewRepository(pool), provider)
	if _, err := service.Import(context.Background(), importRequest(t, target(t, stockholmInstrument, "NORD.ST"))); err != nil {
		t.Fatal(err)
	}

	var count int
	var scope, payload string
	if err := pool.QueryRow(context.Background(), `SELECT count(*),
		coalesce(max(scope), ''), coalesce(max(payload::text), '')
		FROM client_events WHERE event_type = 'corporate_action.changed.v1'`).
		Scan(&count, &scope, &payload); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("recording a corporate action published %d events, expected exactly one", count)
	}
	if scope != "shared" {
		t.Errorf("the event was published with scope %q; market data is shared reference data", scope)
	}
	// The payload has to name what changed, or a client cannot tell whether the change
	// concerns the instrument it is displaying and must refetch everything.
	for _, want := range []string{"instrument_id", "ex_date", "2024-04-02"} {
		if !strings.Contains(payload, want) {
			t.Errorf("the event payload does not name %s: %s", want, payload)
		}
	}

	// A replay records nothing new, so it must publish nothing new either.
	if _, err := service.Import(context.Background(), importRequest(t, target(t, stockholmInstrument, "NORD.ST"))); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM client_events
		WHERE event_type = 'corporate_action.changed.v1'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("replaying an import published the action event again (%d total)", count)
	}
}

// The event and the row it reports must be written in the same transaction: a client that
// reconnects after a failed import must not find an event for an action that was rolled back.
func TestACorporateActionEventIsWrittenInTheSameTransactionAsTheAction(t *testing.T) {
	pool := migratedPool(t)
	provider := newScriptedProvider()
	ratio := decimal(t, "2")
	provider.set("NORD.ST", "", marketdata.DailyPage{
		Bars: []marketdata.ProviderBar{bar(t, "2024-03-28", "100.5", "50.25", 125000, "bar-1")},
		Actions: []marketdata.ProviderAction{{
			ProviderActionID: "split-1", Type: marketdata.ActionSplit,
			ExDate: session(t, "2024-04-02"), Ratio: &ratio, SourceHash: "action-1",
		}},
	})
	service := marketdata.NewImportService(marketdata.NewRepository(pool), provider)
	if _, err := service.Import(context.Background(), importRequest(t, target(t, stockholmInstrument, "NORD.ST"))); err != nil {
		t.Fatal(err)
	}

	var actions, events int
	if err := pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM corporate_actions),
		(SELECT count(*) FROM client_events WHERE event_type = 'corporate_action.changed.v1')`).
		Scan(&actions, &events); err != nil {
		t.Fatal(err)
	}
	if actions != events {
		t.Fatalf("%d corporate actions are stored but %d events were published; the two are "+
			"not transactionally coupled", actions, events)
	}
}

// A finding describes a condition, and conditions end.
//
// Nothing ever wrote resolved_at or resolving_run_id, so every finding ever recorded stayed
// open forever — 8,408 of them in production, most describing gaps that later imports had
// already filled. The chart then marked sessions that were fine, and the count of open
// findings measured how much had ever been wrong rather than how much still was.
//
// Feature 002 specified this: the finding entity carries a resolution state and a resolving
// run. The columns existed and were only ever read.
func TestAnImportResolvesFindingsWhoseConditionHasPassed(t *testing.T) {
	pool := migratedPool(t)
	repository := marketdata.NewRepository(pool)
	provider := newScriptedProvider()

	// The first import is missing 2024-04-02, a session the exchange was open for that sits
	// *inside* the history returned — a hole, rather than the point where the history starts.
	provider.set("NORD.ST", "", marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-03-28", "50", "50", 179000, "bar-1"),
		bar(t, "2024-04-03", "53", "53", 181000, "bar-3"),
	}})
	service := marketdata.NewImportService(repository, provider)
	request := importRequest(t, target(t, stockholmInstrument, "NORD.ST"))
	if _, err := service.Import(context.Background(), request); err != nil {
		t.Fatal(err)
	}

	var openBefore int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM data_quality_findings
		WHERE rule='missing_session' AND status='open'`).Scan(&openBefore); err != nil {
		t.Fatal(err)
	}
	if openBefore == 0 {
		t.Fatal("the first import recorded no missing-session finding, so there is nothing to resolve")
	}

	// The second import supplies the session that was missing.
	provider.set("NORD.ST", "", marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-03-28", "50", "50", 179000, "bar-1"),
		bar(t, "2024-04-02", "51.75", "51.75", 180000, "bar-2"),
		bar(t, "2024-04-03", "53", "53", 181000, "bar-3"),
	}})
	secondRun, err := service.Import(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}

	var stillOpen, resolved int
	var resolvingRun *string
	if err := pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM data_quality_findings WHERE rule='missing_session' AND status='open'),
		(SELECT count(*) FROM data_quality_findings WHERE rule='missing_session' AND status='resolved'),
		(SELECT max(resolving_run_id::text) FROM data_quality_findings WHERE status='resolved')`).
		Scan(&stillOpen, &resolved, &resolvingRun); err != nil {
		t.Fatal(err)
	}
	if resolved == 0 {
		t.Fatalf("filling the gap resolved nothing: %d still open", stillOpen)
	}
	if resolvingRun == nil || *resolvingRun != secondRun.ID.String() {
		t.Errorf("resolving run = %v, expected the import that filled the gap (%s)",
			resolvingRun, secondRun.ID)
	}

	// Resolution is a client-visible committed change and must publish its event.
	var events int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM client_events
		WHERE event_type='quality_finding.changed.v1'`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events < 2 {
		t.Errorf("only %d quality-finding events were published; resolving one publishes none", events)
	}
}

// A gap that is still a gap stays open. Resolving on any import at all would turn the finding
// into a record of "an import happened" rather than of a condition.
func TestAnImportLeavesAFindingOpenWhileItsConditionHolds(t *testing.T) {
	pool := migratedPool(t)
	provider := newScriptedProvider()
	page := marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-03-28", "50", "50", 179000, "bar-1"),
		bar(t, "2024-04-03", "53", "53", 181000, "bar-3"),
	}}
	provider.set("NORD.ST", "", page)

	service := marketdata.NewImportService(marketdata.NewRepository(pool), provider)
	request := importRequest(t, target(t, stockholmInstrument, "NORD.ST"))
	if _, err := service.Import(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	// The same incomplete page again: the session is still missing.
	provider.set("NORD.ST", "", page)
	if _, err := service.Import(context.Background(), request); err != nil {
		t.Fatal(err)
	}

	var open, resolved int
	if err := pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM data_quality_findings WHERE rule='missing_session' AND status='open'),
		(SELECT count(*) FROM data_quality_findings WHERE rule='missing_session' AND status='resolved')`).
		Scan(&open, &resolved); err != nil {
		t.Fatal(err)
	}
	if open == 0 || resolved != 0 {
		t.Fatalf("a persisting gap reported open=%d resolved=%d", open, resolved)
	}
}

// A finding for a session the import never covered is none of that import's business.
func TestAnImportDoesNotResolveFindingsOutsideItsRange(t *testing.T) {
	pool := migratedPool(t)
	ctx := context.Background()

	// A finding from an earlier era, well outside the range the import below requests.
	var runID string
	if err := pool.QueryRow(ctx, `INSERT INTO import_runs
		(id,kind,provider,status,started_at,finished_at,app_version)
		VALUES (gen_random_uuid(),'backfill','fixture','succeeded',now(),now(),'test')
		RETURNING id::text`).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO data_quality_findings
		(id,instrument_id,session_date,run_id,rule,severity,disposition,detail,status,created_at)
		VALUES (gen_random_uuid(),$1,'2019-05-06',$2,'missing_session','warning','flagged',
		        'older than the imported range','open',now())`,
		stockholmInstrument, runID); err != nil {
		t.Fatal(err)
	}

	provider := newScriptedProvider()
	provider.set("NORD.ST", "", marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-04-02", "51.75", "51.75", 180000, "bar-2"),
		bar(t, "2024-04-03", "53", "53", 181000, "bar-3"),
	}})
	service := marketdata.NewImportService(marketdata.NewRepository(pool), provider)
	if _, err := service.Import(ctx, importRequest(t, target(t, stockholmInstrument, "NORD.ST"))); err != nil {
		t.Fatal(err)
	}

	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM data_quality_findings
		WHERE session_date='2019-05-06'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "open" {
		t.Errorf("a finding outside the imported range was marked %q", status)
	}
}

// A finding is a record of a condition, not of an observation of it.
//
// Every import re-inserted a row for a gap that was already recorded, so repeated backfills
// multiplied the same facts: production held 25,853 missing-session findings describing 8,662
// distinct conditions. The count then measured how often imports had run rather than how much
// was wrong, and no amount of resolving could keep up with it.
func TestAPersistingConditionRecordsOneFindingNotOnePerImport(t *testing.T) {
	pool := migratedPool(t)
	provider := newScriptedProvider()
	page := marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-03-28", "50", "50", 179000, "bar-1"),
		bar(t, "2024-04-03", "53", "53", 181000, "bar-3"),
	}}

	service := marketdata.NewImportService(marketdata.NewRepository(pool), provider)
	request := importRequest(t, target(t, stockholmInstrument, "NORD.ST"))

	provider.set("NORD.ST", "", page)
	if _, err := service.Import(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	var afterFirst, eventsAfterFirst int
	if err := pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM data_quality_findings WHERE rule='missing_session'),
		(SELECT count(*) FROM client_events WHERE event_type='quality_finding.changed.v1')`).
		Scan(&afterFirst, &eventsAfterFirst); err != nil {
		t.Fatal(err)
	}
	if afterFirst == 0 {
		t.Fatal("the first import recorded no missing-session finding")
	}

	// The very same gap, observed again.
	provider.set("NORD.ST", "", page)
	if _, err := service.Import(context.Background(), request); err != nil {
		t.Fatal(err)
	}

	var afterSecond, eventsAfterSecond, distinct int
	if err := pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM data_quality_findings WHERE rule='missing_session'),
		(SELECT count(*) FROM client_events WHERE event_type='quality_finding.changed.v1'),
		(SELECT count(DISTINCT (instrument_id, session_date, rule)) FROM data_quality_findings)`).
		Scan(&afterSecond, &eventsAfterSecond, &distinct); err != nil {
		t.Fatal(err)
	}
	if afterSecond != afterFirst {
		t.Errorf("re-observing the same gap added %d findings; rows=%d distinct conditions=%d",
			afterSecond-afterFirst, afterSecond, distinct)
	}
	// Nothing changed, so nothing is published. An event per import for an unchanged condition
	// is the storm the client had to be taught to coalesce.
	if eventsAfterSecond != eventsAfterFirst {
		t.Errorf("re-observing the same gap published %d further events",
			eventsAfterSecond-eventsAfterFirst)
	}
}

// Resolving a finding must not prevent the same condition being recorded again if it returns.
func TestAConditionThatReturnsIsRecordedAgain(t *testing.T) {
	pool := migratedPool(t)
	provider := newScriptedProvider()
	service := marketdata.NewImportService(marketdata.NewRepository(pool), provider)
	request := importRequest(t, target(t, stockholmInstrument, "NORD.ST"))

	gap := marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-03-28", "50", "50", 179000, "bar-1"),
		bar(t, "2024-04-03", "53", "53", 181000, "bar-3"),
	}}
	complete := marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-03-28", "50", "50", 179000, "bar-1"),
		bar(t, "2024-04-02", "51.75", "51.75", 180000, "bar-2"),
		bar(t, "2024-04-03", "53", "53", 181000, "bar-3"),
	}}

	provider.set("NORD.ST", "", gap)
	if _, err := service.Import(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	provider.set("NORD.ST", "", complete)
	if _, err := service.Import(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	provider.set("NORD.ST", "", gap)
	if _, err := service.Import(context.Background(), request); err != nil {
		t.Fatal(err)
	}

	var open, resolved int
	if err := pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM data_quality_findings WHERE rule='missing_session' AND status='open'),
		(SELECT count(*) FROM data_quality_findings WHERE rule='missing_session' AND status='resolved')`).
		Scan(&open, &resolved); err != nil {
		t.Fatal(err)
	}
	if open == 0 {
		t.Error("a gap that returned was not recorded again")
	}
	if resolved == 0 {
		t.Error("the import that filled the gap resolved nothing")
	}
}
