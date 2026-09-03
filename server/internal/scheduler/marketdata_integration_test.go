package scheduler

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"market-lens/server/internal/db"
	"market-lens/server/internal/instruments"
	"market-lens/server/internal/marketdata"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The scheduled pass, wired to a real store and a source that answers only for the range it was
// asked about. That last part is the whole point: a provider stub returning a fixed page
// regardless of the request would let this test pass against a pass that asks for one session.
const (
	reobserveExchangeMIC = "XSTO"
	reobserveUniverse    = "nordic-liquid-v1"
	reobserveSymbol      = "REOB.ST"
	// The calendar runs to 2026-06-30 in the seeded exchange sessions.
	reobserveAsOf = "2026-06-30"
)

var reobserveInstrument = instruments.UUID("aaaa0016-0000-4000-8000-000000000001")

type sourceFixture struct {
	t        *testing.T
	pool     *pgxpool.Pool
	ctx      context.Context
	exchange instruments.UUID
	// closes is the source's current answer per session, in cents. Restating a close is a
	// change to this map — the same thing a provider re-running its own pipeline does.
	closes map[string]int64
	// requests records every range the source was asked for, so a test can assert what the
	// pass asked rather than only what it stored.
	requests []marketdata.DailyRequest
	sessions []string
}

func newSourceFixture(t *testing.T, sessionCount int) *sourceFixture {
	t.Helper()
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	f := &sourceFixture{t: t, pool: pool, ctx: ctx, closes: map[string]int64{}}
	if err := pool.QueryRow(ctx, `SELECT id::text FROM exchanges WHERE mic = $1`, reobserveExchangeMIC).
		Scan((*string)(&f.exchange)); err != nil {
		t.Fatalf("find the exchange: %v", err)
	}
	f.exec(`INSERT INTO instruments
		(id, exchange_id, isin, ticker, name, currency, country, instrument_type, active, purchasability_status)
		VALUES ($1, $2, 'SE0000001601', 'REOB', 'Re-observed AB', 'SEK', 'SE', 'common_stock', true, 'unverified')`,
		reobserveInstrument.String(), f.exchange.String())
	f.exec(`INSERT INTO provider_instruments (instrument_id, provider, provider_symbol, active)
		VALUES ($1, 'fixture', $2, true)`, reobserveInstrument.String(), reobserveSymbol)
	f.exec(`INSERT INTO universe_memberships (universe_id, instrument_id, included_from, curation_source)
		SELECT id, $1, '2016-01-01', 'fixture' FROM research_universes WHERE code = $2`,
		reobserveInstrument.String(), reobserveUniverse)

	f.sessions = f.openSessions(sessionCount)
	for index, session := range f.sessions {
		f.closes[session] = 10000 + int64(index)*25
	}
	return f
}

func (f *sourceFixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(f.ctx, sql, args...); err != nil {
		f.t.Fatalf("fixture: %v\n%s", err, sql)
	}
}

// openSessions returns the most recent `count` open sessions, ascending.
func (f *sourceFixture) openSessions(count int) []string {
	f.t.Helper()
	rows, err := f.pool.Query(f.ctx, `SELECT session_date::text FROM (
			SELECT session_date FROM exchange_sessions
			WHERE exchange_id = $1 AND status IN ('open', 'half_day') AND session_date <= $2
			ORDER BY session_date DESC LIMIT $3
		) recent ORDER BY session_date`, f.exchange.String(), reobserveAsOf, count)
	if err != nil {
		f.t.Fatalf("list sessions: %v", err)
	}
	defer rows.Close()
	var sessions []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			f.t.Fatal(err)
		}
		sessions = append(sessions, value)
	}
	if len(sessions) < count {
		f.t.Fatalf("the calendar holds %d sessions, fewer than %d", len(sessions), count)
	}
	return sessions
}

// Name, Resolve and Daily make the fixture a Provider that answers only for the range asked.
func (f *sourceFixture) Name() string { return "fixture" }

func (f *sourceFixture) Resolve(context.Context, marketdata.ResolveRequest) (marketdata.ResolvedInstrument, error) {
	return marketdata.ResolvedInstrument{}, errors.New("resolve is not used here")
}

func (f *sourceFixture) Daily(_ context.Context, request marketdata.DailyRequest) (marketdata.DailyPage, error) {
	f.requests = append(f.requests, request)
	var page marketdata.DailyPage
	for _, session := range f.sessions {
		if session < request.From.String() || session > request.To.String() {
			continue
		}
		page.Bars = append(page.Bars, f.bar(session))
	}
	return page, nil
}

func (f *sourceFixture) bar(session string) marketdata.ProviderBar {
	cents := f.closes[session]
	decimal := func(value int64) marketdata.Decimal {
		parsed, err := marketdata.ParseDecimal(fmt.Sprintf("%d.%02d", value/100, value%100))
		if err != nil {
			f.t.Fatalf("decimal %d: %v", value, err)
		}
		return parsed
	}
	return marketdata.ProviderBar{
		SessionDate: marketdata.SessionDate(session),
		Open:        decimal(cents - 50),
		High:        decimal(cents + 100),
		Low:         decimal(cents - 100),
		Close:       decimal(cents),
		Volume:      1000,
		// The hash is what the store compares, so it must move with the values the way a real
		// provider's does. A constant hash would hide every restatement.
		SourceHash: fmt.Sprintf("%s-%d", session, cents),
	}
}

// run executes one scheduled pass at the given session's close.
func (f *sourceFixture) run(sessionIndex int, reobserve int) marketdata.ImportRun {
	f.t.Helper()
	repository := marketdata.NewRepository(f.pool)
	service := marketdata.NewImportService(repository, f)
	location, err := time.LoadLocation("Europe/Stockholm")
	if err != nil {
		f.t.Fatal(err)
	}
	config := MarketDataConfig{
		Enabled: true, Hour: 20, Minute: 0, Location: location,
		Provider: "fixture", Universe: reobserveUniverse, AppVersion: "test",
		MaxRetries: 1, Workers: 1, ReobserveSessions: reobserve,
	}
	scheduler, err := NewMarketData(config, repository, service)
	if err != nil {
		f.t.Fatal(err)
	}
	session, err := time.ParseInLocation("2006-01-02", f.sessions[sessionIndex], location)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := scheduler.RunDue(f.ctx, session.Add(20*time.Hour)); err != nil {
		f.t.Fatalf("scheduled pass: %v", err)
	}
	var run marketdata.ImportRun
	if err := f.pool.QueryRow(f.ctx, `SELECT id::text FROM import_runs ORDER BY started_at DESC LIMIT 1`).
		Scan((*string)(&run.ID)); err != nil {
		f.t.Fatalf("read the run: %v", err)
	}
	return run
}

func (f *sourceFixture) storedClose(session string) string {
	f.t.Helper()
	var value string
	if err := f.pool.QueryRow(f.ctx, `SELECT close::text FROM daily_price_bars
		WHERE instrument_id = $1 AND session_date = $2::date`,
		reobserveInstrument.String(), session).Scan(&value); err != nil {
		f.t.Fatalf("read the stored close for %s: %v", session, err)
	}
	return value
}

func (f *sourceFixture) count(sql string, args ...any) int64 {
	f.t.Helper()
	var total int64
	if err := f.pool.QueryRow(f.ctx, sql, args...).Scan(&total); err != nil {
		f.t.Fatalf("count: %v\n%s", err, sql)
	}
	return total
}

// TestARestatedCloseIsCorrectedByTheNextRoutinePass is the designated first red.
//
// The correction path has existed since feature 002 and is tested: compare the source hash,
// archive the previous values, replace the bar, recompute what read it. None of it has ever run
// in normal operation, because the scheduled pass asks the source about exactly one session and
// so never gives it a chance to change its mind.
func TestARestatedCloseIsCorrectedByTheNextRoutinePass(t *testing.T) {
	fixture := newSourceFixture(t, 12)

	// Store the history one session at a time, the way the schedule would have.
	for index := range fixture.sessions {
		fixture.run(index, 1)
	}
	restated := fixture.sessions[len(fixture.sessions)-4]
	before := fixture.storedClose(restated)

	// The source restates a close three sessions back, as a provider re-running its pipeline does.
	fixture.closes[restated] += 777

	fixture.run(len(fixture.sessions)-1, 5)

	after := fixture.storedClose(restated)
	if after == before {
		t.Fatalf("the stored close for %s is still %s; the pass never asked the source about it again",
			restated, before)
	}
	if revisions := fixture.count(`SELECT count(*) FROM price_bar_revisions
		WHERE instrument_id = $1 AND session_date = $2::date`,
		reobserveInstrument.String(), restated); revisions != 1 {
		t.Fatalf("%d revisions archived for the corrected session, wanted 1", revisions)
	}
	if bars := fixture.count(`SELECT count(*) FROM daily_price_bars
		WHERE instrument_id = $1 AND session_date = $2::date`,
		reobserveInstrument.String(), restated); bars != 1 {
		t.Fatalf("%d current bars for the corrected session; a correction must replace, not duplicate", bars)
	}
}

// TestTheWindowCountsTradingSessionsPerExchange is FR-002.
//
// Five calendar days and five trading sessions are the same thing only in a week with no
// holidays. The four Nordic exchanges keep different calendars, so a window counted in days
// reaches a different distance into each — and does so silently, because both answers look like
// dates.
func TestTheWindowCountsTradingSessionsPerExchange(t *testing.T) {
	fixture := newSourceFixture(t, 12)
	repository := marketdata.NewRepository(fixture.pool)
	asOf := marketdata.SessionDate(fixture.sessions[len(fixture.sessions)-1])

	starts, err := repository.ReobservationStarts(fixture.ctx, "fixture", reobserveUniverse, 5, asOf)
	if err != nil {
		t.Fatalf("read the window: %v", err)
	}
	start, known := starts[reobserveInstrument]
	if !known {
		t.Fatalf("the universe member has no window start")
	}
	// The fifth most recent open session, counted back through the stored calendar.
	wanted := fixture.sessions[len(fixture.sessions)-5]
	if start.String() != wanted {
		t.Fatalf("window starts at %s, wanted the fifth most recent session %s", start, wanted)
	}

	// Counted in sessions, the window spans exactly five of them however many calendar days
	// that turns out to be.
	spanned := fixture.count(`SELECT count(*) FROM exchange_sessions
		WHERE exchange_id = $1 AND status IN ('open', 'half_day')
		  AND session_date BETWEEN $2::date AND $3::date`,
		fixture.exchange.String(), start.String(), asOf.String())
	if spanned != 5 {
		t.Fatalf("the window spans %d trading sessions, wanted 5", spanned)
	}

	// A one-session window is the behaviour before this feature and must still be expressible.
	narrow, err := repository.ReobservationStarts(fixture.ctx, "fixture", reobserveUniverse, 1, asOf)
	if err != nil {
		t.Fatalf("read the narrow window: %v", err)
	}
	if narrow[reobserveInstrument].String() != asOf.String() {
		t.Fatalf("a one-session window starts at %s, wanted %s", narrow[reobserveInstrument], asOf)
	}
}

// TestTheWindowNeverPredatesAnInstrumentsFirstSession is FR-004: an instrument listed last week
// is not asked about sessions that predate its listing.
func TestTheWindowNeverPredatesAnInstrumentsFirstSession(t *testing.T) {
	fixture := newSourceFixture(t, 12)
	repository := marketdata.NewRepository(fixture.pool)
	asOf := marketdata.SessionDate(fixture.sessions[len(fixture.sessions)-1])

	// Store only the three most recent sessions, as a recently listed instrument would have.
	for index := len(fixture.sessions) - 3; index < len(fixture.sessions); index++ {
		fixture.run(index, 1)
	}
	stored := fixture.count(`SELECT count(*) FROM daily_price_bars WHERE instrument_id = $1`,
		reobserveInstrument.String())
	if stored != 3 {
		t.Fatalf("the fixture stored %d sessions, wanted 3", stored)
	}

	starts, err := repository.ReobservationStarts(fixture.ctx, "fixture", reobserveUniverse, 10, asOf)
	if err != nil {
		t.Fatalf("read the window: %v", err)
	}
	first := fixture.sessions[len(fixture.sessions)-3]
	if got := starts[reobserveInstrument].String(); got != first {
		t.Fatalf("a ten-session window on a three-session history starts at %s, wanted its first stored session %s",
			got, first)
	}
}

// TestAnUnchangedReobservationWritesNothing pins R-002, the property the whole design rests on.
//
// The incremental feature scope is derived from bars carrying the current run's import_run_id.
// If an unchanged re-observation ever started touching that column — or last_observed_at, on the
// way to it — every night would recompute five sessions for every instrument and rescore the
// whole universe, for nothing. The only symptom would be a slower pass, which is exactly the kind
// of regression nobody attributes to the right cause.
func TestAnUnchangedReobservationWritesNothing(t *testing.T) {
	fixture := newSourceFixture(t, 12)
	for index := range fixture.sessions {
		fixture.run(index, 1)
	}

	type barState struct{ runID, observed, hash string }
	before := map[string]barState{}
	read := func(into map[string]barState) {
		rows, err := fixture.pool.Query(fixture.ctx, `SELECT session_date::text, import_run_id::text,
			last_observed_at::text, source_hash FROM daily_price_bars WHERE instrument_id = $1`,
			reobserveInstrument.String())
		if err != nil {
			t.Fatalf("read bars: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
			var session string
			var state barState
			if err := rows.Scan(&session, &state.runID, &state.observed, &state.hash); err != nil {
				t.Fatal(err)
			}
			into[session] = state
		}
	}
	read(before)
	revisionsBefore := fixture.count(`SELECT count(*) FROM price_bar_revisions WHERE instrument_id = $1`,
		reobserveInstrument.String())

	// A second pass over the same, unchanged source, this time asking about five sessions.
	run := fixture.run(len(fixture.sessions)-1, 5)

	after := map[string]barState{}
	read(after)
	for session, state := range before {
		if after[session] != state {
			t.Fatalf("session %s was rewritten by a re-observation that found nothing changed:\n before %+v\n after  %+v",
				session, state, after[session])
		}
	}
	if revisions := fixture.count(`SELECT count(*) FROM price_bar_revisions WHERE instrument_id = $1`,
		reobserveInstrument.String()); revisions != revisionsBefore {
		t.Fatalf("%d revisions archived by a pass that corrected nothing", revisions-revisionsBefore)
	}
	// The decisive assertion: nothing carries this run's identifier, so the incremental feature
	// scope derived from it is empty and no recomputation follows.
	if touched := fixture.count(`SELECT count(*) FROM daily_price_bars WHERE import_run_id = $1`,
		run.ID.String()); touched != 0 {
		t.Fatalf("%d bars carry the quiet run's identifier; the feature pass would recompute them", touched)
	}
}

// TestARunCountsCorrectionsSeparatelyFromInsertions is FR-009.
//
// A run that stores a session for the first time and a run that replaces one it stored earlier
// have looked identical since feature 002. Only the second means every value derived from that
// session changed underneath, which is the case worth being able to see.
func TestARunCountsCorrectionsSeparatelyFromInsertions(t *testing.T) {
	fixture := newSourceFixture(t, 12)
	for index := range fixture.sessions {
		fixture.run(index, 1)
	}

	// Two sessions inside the window are restated; the session that just closed is new.
	fixture.closes[fixture.sessions[len(fixture.sessions)-3]] += 101
	fixture.closes[fixture.sessions[len(fixture.sessions)-4]] += 202

	// Roll forward a session so the pass has one new session to store as well.
	fixture.sessions = append(fixture.sessions, fixture.nextSession())
	fixture.closes[fixture.sessions[len(fixture.sessions)-1]] = 10500

	run := fixture.run(len(fixture.sessions)-1, 5)

	var processed, accepted, revised int64
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT processed_count, accepted_count, revised_count
		FROM import_runs WHERE id = $1`, run.ID.String()).Scan(&processed, &accepted, &revised); err != nil {
		t.Fatalf("read the run counts: %v", err)
	}
	if revised != 2 {
		t.Fatalf("the run reports %d corrected sessions, wanted 2 (processed %d, accepted %d)",
			revised, processed, accepted)
	}
	if revised >= accepted {
		t.Fatalf("corrections (%d) are not distinguishable from what was stored (%d)", revised, accepted)
	}
	if revised > processed {
		t.Fatalf("the run corrected %d sessions of %d processed", revised, processed)
	}

	// The per-instrument item attributes them, which is what makes the count actionable.
	var itemRevised int64
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT revised_count FROM import_items
		WHERE run_id = $1 AND instrument_id = $2`, run.ID.String(), reobserveInstrument.String()).
		Scan(&itemRevised); err != nil {
		t.Fatalf("read the item count: %v", err)
	}
	if itemRevised != 2 {
		t.Fatalf("the item reports %d corrections, wanted 2", itemRevised)
	}

	// And a pass that corrects nothing says so.
	quiet := fixture.run(len(fixture.sessions)-1, 5)
	if quiet.ID == run.ID {
		t.Fatalf("the second pass did not run")
	}
}

// nextSession returns the open session after the fixture's current last one.
func (f *sourceFixture) nextSession() string {
	f.t.Helper()
	var next string
	if err := f.pool.QueryRow(f.ctx, `SELECT session_date::text FROM exchange_sessions
		WHERE exchange_id = $1 AND status IN ('open', 'half_day') AND session_date > $2::date
		ORDER BY session_date LIMIT 1`,
		f.exchange.String(), f.sessions[len(f.sessions)-1]).Scan(&next); err != nil {
		f.t.Fatalf("find the next session: %v", err)
	}
	return next
}

// TestTheWidenedWindowMakesNoExtraSourceRequests pins R-001, the measurement that makes the
// window free.
//
// A source range is asked for once per instrument whatever its width, so five sessions cost what
// one costs. If that ever stopped being true — a provider client that paged per day, say — the
// window would quietly become a quota multiplier, and the first symptom would be a bill.
func TestTheWidenedWindowMakesNoExtraSourceRequests(t *testing.T) {
	fixture := newSourceFixture(t, 12)
	for index := range fixture.sessions {
		fixture.run(index, 1)
	}

	fixture.requests = nil
	fixture.run(len(fixture.sessions)-1, 1)
	narrow := len(fixture.requests)

	fixture.requests = nil
	fixture.run(len(fixture.sessions)-1, 5)
	wide := len(fixture.requests)

	if narrow == 0 {
		t.Fatalf("the pass asked the source nothing")
	}
	if wide != narrow {
		t.Fatalf("a five-session window made %d source requests, a one-session window %d", wide, narrow)
	}
	// And it really did ask about five sessions, so the test is not passing because the window
	// was ignored.
	for _, request := range fixture.requests {
		spanned := fixture.count(`SELECT count(*) FROM exchange_sessions
			WHERE exchange_id = $1 AND status IN ('open', 'half_day')
			  AND session_date BETWEEN $2::date AND $3::date`,
			fixture.exchange.String(), request.From.String(), request.To.String())
		if spanned != 5 {
			t.Fatalf("the widened request spanned %d sessions (%s..%s), wanted 5",
				spanned, request.From, request.To)
		}
	}
}

// TestAQuietRunTriggersNoRecomputation is SC-003 stated the way the feature engine would see it.
func TestAQuietRunTriggersNoRecomputation(t *testing.T) {
	fixture := newSourceFixture(t, 12)
	for index := range fixture.sessions {
		fixture.run(index, 1)
	}

	// A second pass over an unchanged source, asking about five sessions.
	run := fixture.run(len(fixture.sessions)-1, 5)

	// The incremental feature scope is exactly these two sets. Both empty means the engine is
	// asked to recompute nothing at all.
	touched := fixture.count(`SELECT count(*) FROM (
		SELECT instrument_id, session_date FROM daily_price_bars WHERE import_run_id = $1
		UNION
		SELECT instrument_id, session_date FROM price_bar_revisions WHERE superseding_run_id = $1
	) scope`, run.ID.String())
	if touched != 0 {
		t.Fatalf("a pass that corrected nothing left %d sessions in the recomputation scope", touched)
	}
	var revised int64
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT revised_count FROM import_runs WHERE id = $1`,
		run.ID.String()).Scan(&revised); err != nil {
		t.Fatalf("read the run: %v", err)
	}
	if revised != 0 {
		t.Fatalf("a pass over an unchanged source reported %d corrections", revised)
	}
}

// TestTheScheduledPassReExaminesAnOpenFinding closes the gap research R-009 recorded.
//
// WidenToUnsettled exists so an import reaches back far enough to re-examine a session with an
// open data-quality finding, and its own comment records why: without it such findings "can
// never be re-examined and stay open for good. Production held eight of them, all raised by a
// validation rule that had since been corrected." Only the backfill command applied it. The
// scheduled pass — the one that runs every night without anybody deciding to — did not, so a
// finding older than the re-observation window stayed open no matter how many nights passed.
func TestTheScheduledPassReExaminesAnOpenFinding(t *testing.T) {
	fixture := newSourceFixture(t, 12)
	for index := range fixture.sessions {
		fixture.run(index, 1)
	}

	// A finding on a session well outside the five-session window.
	stale := fixture.sessions[2]
	fixture.exec(`INSERT INTO data_quality_findings
		(id, instrument_id, session_date, run_id, rule, severity, disposition, detail, status, created_at)
		SELECT gen_random_uuid(), $1, $2::date, id, 'suspicious_jump', 'warning', 'flagged',
		       'raised by a rule that has since been corrected', 'open', now()
		FROM import_runs ORDER BY started_at LIMIT 1`,
		reobserveInstrument.String(), stale)

	fixture.requests = nil
	fixture.run(len(fixture.sessions)-1, 5)

	if len(fixture.requests) == 0 {
		t.Fatalf("the pass asked the source nothing")
	}
	for _, request := range fixture.requests {
		if request.From.String() > stale {
			t.Fatalf("the pass asked from %s, which never reaches the open finding at %s — so the "+
				"finding can never be re-examined", request.From, stale)
		}
	}
}
