package instruments_test

import (
	"context"
	"fmt"
	"testing"

	"market-lens/server/internal/db"
	"market-lens/server/internal/instruments"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The exploration fixture is the shared universe every instrument-exploration test asserts
// against. It is built from the *real* Nordic calendar that migration 0005 populates rather
// than from invented dates, because the whole claim of this feature is that a closed
// exchange day and a missing observation are different things. Inventing a calendar would
// let a test agree with a bug.
//
// Freshness and every session-counted window are measured against a fixed as-of date rather
// than "now". Anchoring to the clock would make these tests report a different answer every
// day, which Principle V forbids.
const fixtureAsOf = instruments.SessionDate("2026-06-30")

type explorationFixture struct {
	pool *pgxpool.Pool
	ctx  context.Context

	stockholm  instruments.UUID
	copenhagen instruments.UUID
	oslo       instruments.UUID

	// long has the deepest history in the fixture and no gaps: the instrument every
	// "renders and stays interactive" and "statistics are computable" assertion uses.
	long instruments.UUID
	// gappy is missing three sessions the exchange was open for, and carries the split,
	// the dividend, and the quality findings.
	gappy instruments.UUID
	// short has exactly 20 stored sessions — one short of the 21 a 20-session return and
	// the volatility measure need, so every derived statistic on it must be null.
	short instruments.UUID
	// empty has no stored bars at all, which is a different state from stale.
	empty instruments.UUID
	// stale stops ten open sessions before the as-of date.
	stale instruments.UUID

	runID instruments.UUID
	// featureRunID is created on demand by the tests that seed the engine's values.
	featureRunID instruments.UUID
}

const (
	fixtureLongSessions  = 300
	fixtureGappySessions = 120
	fixtureShortSessions = 20
	fixtureStaleSessions = 50
	// fixtureStaleBehind is how many open sessions separate the stale instrument's last
	// stored bar from the as-of date.
	fixtureStaleBehind = 10
)

// fixtureGapOffsets are positions, counted back from the gappy instrument's most recent
// session, where the exchange was open and no bar is stored.
var fixtureGapOffsets = []int{17, 18, 40}

func newExplorationFixture(t *testing.T) *explorationFixture {
	t.Helper()
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	fixture := &explorationFixture{pool: pool, ctx: ctx}
	fixture.stockholm = exchangeID(t, ctx, pool, "XSTO")
	fixture.copenhagen = exchangeID(t, ctx, pool, "XCSE")
	fixture.oslo = exchangeID(t, ctx, pool, "XOSL")
	fixture.runID = mustUUID(t)

	if _, err := pool.Exec(ctx, `INSERT INTO import_runs
		(id, kind, provider, status, started_at, finished_at, app_version)
		VALUES ($1, 'backfill', 'fixture', 'succeeded', now(), now(), 'test')`,
		fixture.runID.String()); err != nil {
		t.Fatalf("insert import run: %v", err)
	}

	fixture.long = fixture.addInstrument(t, fixture.stockholm, "SE0000000100", "LONG",
		"Long History AB", "SEK", "SE", "industrials", "Machinery")
	fixture.gappy = fixture.addInstrument(t, fixture.stockholm, "SE0000000200", "GAPPY",
		"Interrupted History AB", "SEK", "SE", "information_technology", "Software")
	fixture.short = fixture.addInstrument(t, fixture.copenhagen, "DK0000000300", "SHORT",
		"Barely Listed A/S", "DKK", "DK", "health_care", "Biotechnology")
	fixture.empty = fixture.addInstrument(t, fixture.copenhagen, "DK0000000400", "EMPTY",
		"No History A/S", "DKK", "DK", "financials", "Banks")
	fixture.stale = fixture.addInstrument(t, fixture.oslo, "NO0000000500", "STALE",
		"Behind The Rest ASA", "NOK", "NO", "energy", "Oil and Gas")

	fixture.addBars(t, fixture.long, fixture.stockholm, "SEK", fixtureLongSessions, 0, nil)
	fixture.addBars(t, fixture.gappy, fixture.stockholm, "SEK", fixtureGappySessions, 0, fixtureGapOffsets)
	fixture.addBars(t, fixture.short, fixture.copenhagen, "DKK", fixtureShortSessions, 0, nil)
	fixture.addBars(t, fixture.stale, fixture.oslo, "NOK", fixtureStaleSessions, fixtureStaleBehind, nil)

	fixture.addAnnotations(t)
	return fixture
}

func (f *explorationFixture) addInstrument(t *testing.T, exchange instruments.UUID,
	isin, ticker, name, currency, country, sector, industry string) instruments.UUID {
	t.Helper()
	id := mustUUID(t)
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO instruments
		(id, exchange_id, isin, ticker, name, currency, country, instrument_type, sector, industry,
		 active, purchasability_status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'common_stock', $8, $9, true, 'unverified')`,
		id.String(), exchange.String(), isin, ticker, name, currency, country, sector, industry); err != nil {
		t.Fatalf("insert instrument %s: %v", ticker, err)
	}
	return id
}

// addBars stores one bar for each of the `count` open sessions ending `endOffset` open
// sessions before the as-of date, skipping any session listed in `skip` (counted back from
// the most recent stored session, so a skipped position is a session the exchange was open
// for and no bar exists — the only thing this feature calls a gap).
//
// Prices are derived deterministically from the session's position so that a repeated run
// produces identical data and a test can assert on an exact computed return.
func (f *explorationFixture) addBars(t *testing.T, instrument, exchange instruments.UUID,
	currency string, count, endOffset int, skip []int) {
	t.Helper()
	skipped := make([]int32, 0, len(skip))
	for _, offset := range skip {
		skipped = append(skipped, int32(offset))
	}
	if _, err := f.pool.Exec(f.ctx, `
		WITH open_sessions AS (
			SELECT session_date,
			       row_number() OVER (ORDER BY session_date DESC) - 1 AS offset_back
			FROM exchange_sessions
			WHERE exchange_id = $2 AND status IN ('open','half_day') AND session_date <= $3
		), window_sessions AS (
			SELECT session_date, offset_back - $5 AS position
			FROM open_sessions
			WHERE offset_back >= $5 AND offset_back < $5 + $4
		), kept AS (
			-- position counts back from the most recent session, so the price index is
			-- inverted to make closes rise with time the way a real series does. Without this
			-- the fixture's oldest bar would be its most expensive and every computed return
			-- would come out negative.
			SELECT session_date, ($4 - 1 - position) AS position
			FROM window_sessions
			WHERE NOT (position = ANY($6::int[]))
		)
		INSERT INTO daily_price_bars
			(instrument_id, session_date, open, high, low, close, adjusted_close, volume,
			 currency, provider, source_hash, import_run_id, first_observed_at, last_observed_at)
		SELECT $1, session_date,
		       round((100 + position * 0.25)::numeric, 8),
		       round((100 + position * 0.25 + 1.5)::numeric, 8),
		       round((100 + position * 0.25 - 1.5)::numeric, 8),
		       round((100 + position * 0.25 + 0.5)::numeric, 8),
		       round((100 + position * 0.25 + 0.5)::numeric, 8),
		       1000 + position * 7,
		       $7, 'fixture', 'hash-' || $9 || '-' || session_date, $8,
		       session_date::timestamptz, session_date::timestamptz
		FROM kept`,
		instrument.String(), exchange.String(), fixtureAsOf.String(), count, endOffset,
		skipped, currency, f.runID.String(), instrument.String()); err != nil {
		t.Fatalf("insert bars: %v", err)
	}
}

// addAnnotations gives the gappy instrument the recorded split, the dividend, and the
// findings that User Story 3 asserts against. The split is placed inside the charted range
// so that a chart drawn over it must mark it.
func (f *explorationFixture) addAnnotations(t *testing.T) {
	t.Helper()
	splitDate := f.sessionAtOffset(t, f.gappy, 30)
	dividendDate := f.sessionAtOffset(t, f.gappy, 60)
	findingDate := f.sessionAtOffset(t, f.gappy, 5)
	resolvedDate := f.sessionAtOffset(t, f.gappy, 95)

	if _, err := f.pool.Exec(f.ctx, `INSERT INTO corporate_actions
		(id, instrument_id, provider, provider_action_id, action_type, ex_date, ratio,
		 source_hash, import_run_id, first_observed_at, last_observed_at)
		VALUES ($1, $2, 'fixture', 'split-1', 'split', $3, 2, 'split-hash', $4, now(), now())`,
		mustUUID(t).String(), f.gappy.String(), splitDate.String(), f.runID.String()); err != nil {
		t.Fatalf("insert split: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO corporate_actions
		(id, instrument_id, provider, provider_action_id, action_type, ex_date, amount, currency,
		 source_hash, import_run_id, first_observed_at, last_observed_at)
		VALUES ($1, $2, 'fixture', 'dividend-1', 'dividend', $3, 4.25, 'SEK', 'dividend-hash', $4, now(), now())`,
		mustUUID(t).String(), f.gappy.String(), dividendDate.String(), f.runID.String()); err != nil {
		t.Fatalf("insert dividend: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO data_quality_findings
		(id, instrument_id, session_date, run_id, rule, severity, disposition, detail, status, created_at)
		VALUES ($1, $2, $3, $4, 'suspicious_jump', 'warning', 'flagged',
		        'close moved more than the configured threshold', 'open', now())`,
		mustUUID(t).String(), f.gappy.String(), findingDate.String(), f.runID.String()); err != nil {
		t.Fatalf("insert open finding: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO data_quality_findings
		(id, instrument_id, session_date, run_id, rule, severity, disposition, detail, status,
		 created_at, resolved_at, resolving_run_id)
		VALUES ($1, $2, $3, $4, 'provider_gap', 'warning', 'flagged',
		        'provider returned no observation', 'resolved', now(), now(), $4)`,
		mustUUID(t).String(), f.gappy.String(), resolvedDate.String(), f.runID.String()); err != nil {
		t.Fatalf("insert resolved finding: %v", err)
	}
}

// sessionAtOffset returns the session `offset` stored bars back from an instrument's most
// recent stored bar, so annotations land on sessions that actually exist.
func (f *explorationFixture) sessionAtOffset(t *testing.T, instrument instruments.UUID, offset int) instruments.SessionDate {
	t.Helper()
	var value string
	if err := f.pool.QueryRow(f.ctx, `SELECT session_date::text FROM daily_price_bars
		WHERE instrument_id = $1 ORDER BY session_date DESC OFFSET $2 LIMIT 1`,
		instrument.String(), offset).Scan(&value); err != nil {
		t.Fatalf("find session at offset %d: %v", offset, err)
	}
	return instruments.SessionDate(value)
}

// closedSessionInRange returns a date the exchange was closed inside the gappy instrument's
// stored range. A test that cannot name one cannot prove a closed day is not a gap.
func (f *explorationFixture) closedSessionInRange(t *testing.T) instruments.SessionDate {
	t.Helper()
	var value string
	if err := f.pool.QueryRow(f.ctx, `SELECT s.session_date::text FROM exchange_sessions s
		WHERE s.exchange_id = $1 AND s.status = 'closed'
		  AND s.session_date BETWEEN (SELECT min(session_date) FROM daily_price_bars WHERE instrument_id = $2)
		                         AND (SELECT max(session_date) FROM daily_price_bars WHERE instrument_id = $2)
		ORDER BY s.session_date LIMIT 1`,
		f.stockholm.String(), f.gappy.String()).Scan(&value); err != nil {
		t.Fatalf("the fixture range contains no closed exchange day, so it cannot prove one is not a gap: %v", err)
	}
	return instruments.SessionDate(value)
}

// missingSessions returns the dates the gappy instrument is missing, derived the same way a
// reader would derive them by hand.
func (f *explorationFixture) missingSessions(t *testing.T) []instruments.SessionDate {
	t.Helper()
	rows, err := f.pool.Query(f.ctx, `SELECT s.session_date::text
		FROM exchange_sessions s
		LEFT JOIN daily_price_bars b ON b.instrument_id = $2 AND b.session_date = s.session_date
		WHERE s.exchange_id = $1 AND s.status IN ('open','half_day')
		  AND s.session_date BETWEEN (SELECT min(session_date) FROM daily_price_bars WHERE instrument_id = $2)
		                         AND (SELECT max(session_date) FROM daily_price_bars WHERE instrument_id = $2)
		  AND b.instrument_id IS NULL
		ORDER BY s.session_date`, f.stockholm.String(), f.gappy.String())
	if err != nil {
		t.Fatalf("compute missing sessions: %v", err)
	}
	defer rows.Close()
	var dates []instruments.SessionDate
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		dates = append(dates, instruments.SessionDate(value))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return dates
}

// TestTheExplorationFixtureIsWorthAssertingAgainst guards the fixture itself. Every later
// test's credibility rests on these properties, and a fixture that quietly stopped
// containing a gap would turn a real regression into a passing suite.
func TestTheExplorationFixtureIsWorthAssertingAgainst(t *testing.T) {
	fixture := newExplorationFixture(t)

	counts := map[instruments.UUID]int{
		fixture.long:  fixtureLongSessions,
		fixture.gappy: fixtureGappySessions - len(fixtureGapOffsets),
		fixture.short: fixtureShortSessions,
		fixture.empty: 0,
		fixture.stale: fixtureStaleSessions,
	}
	for instrument, want := range counts {
		var got int
		if err := fixture.pool.QueryRow(fixture.ctx,
			`SELECT count(*) FROM daily_price_bars WHERE instrument_id = $1`,
			instrument.String()).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("instrument %s stored %d bars, expected %d", instrument, got, want)
		}
	}

	if missing := fixture.missingSessions(t); len(missing) != len(fixtureGapOffsets) {
		t.Errorf("expected %d missing sessions inside the stored range, found %d: %v",
			len(fixtureGapOffsets), len(missing), missing)
	}

	// The fixture must contain a day the exchange was closed inside the same range, or no
	// test can distinguish a holiday from a gap.
	closed := fixture.closedSessionInRange(t)
	for _, date := range fixture.missingSessions(t) {
		if date == closed {
			t.Fatalf("closed exchange day %s was counted as a missing session", closed)
		}
	}
}

// engineRun records one feature run the seeded values can point at, because a value without
// the run that produced it is not something the engine can ever write.
func (f *explorationFixture) engineRun(t *testing.T) instruments.UUID {
	t.Helper()
	if f.featureRunID != "" {
		return f.featureRunID
	}
	id := mustUUID(t)
	var universe string
	if err := f.pool.QueryRow(f.ctx, `SELECT id::text FROM research_universes ORDER BY code LIMIT 1`).Scan(&universe); err != nil {
		t.Fatalf("read a research universe: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO feature_runs
		(id, kind, status, universe_id, started_at, finished_at, instrument_count, value_count, app_version)
		VALUES ($1, 'full', 'succeeded', $2, now(), now(), 1, 0, 'test')`,
		id.String(), universe); err != nil {
		t.Fatalf("insert feature run: %v", err)
	}
	f.featureRunID = id
	return id
}

// seedEngineValue stores what the engine would have written for one definition of one
// instrument at one session: a decimal string, or an absence when the value is empty.
func (f *explorationFixture) seedEngineValue(t *testing.T, instrument instruments.UUID, session, definition, value string) {
	t.Helper()
	run := f.engineRun(t)
	var definitionID string
	if err := f.pool.QueryRow(f.ctx, `SELECT id::text FROM feature_definitions
		WHERE name = $1 AND superseded_at IS NULL`, definition).Scan(&definitionID); err != nil {
		t.Fatalf("read definition %s: %v", definition, err)
	}
	var stored, absence *string
	if value == "" {
		reason := "insufficient_history"
		absence = &reason
	} else {
		stored = &value
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO feature_values
		(instrument_id, session_date, definition_id, value, absence_reason, computed_at, run_id)
		VALUES ($1, $2::date, $3, $4::numeric, $5, now(), $6)
		ON CONFLICT (instrument_id, session_date, definition_id) DO UPDATE
		SET value = excluded.value, absence_reason = excluded.absence_reason, run_id = excluded.run_id`,
		instrument.String(), session, definitionID, stored, absence, run.String()); err != nil {
		t.Fatalf("seed %s for %s: %v", definition, instrument, err)
	}
}

// latestSession is the most recent session an instrument has a stored bar for.
func (f *explorationFixture) latestSession(t *testing.T, instrument instruments.UUID) string {
	t.Helper()
	var session string
	if err := f.pool.QueryRow(f.ctx, `SELECT max(session_date)::text FROM daily_price_bars
		WHERE instrument_id = $1`, instrument.String()).Scan(&session); err != nil {
		t.Fatalf("latest session of %s: %v", instrument, err)
	}
	return session
}

// addSector adds `count` instruments in one sector on one exchange, so a filter can match a
// known number of rows rather than however many the seeded Nordic universe happens to hold.
// The tickers are numbered so an ordering assertion has something stable to check.
func (f *explorationFixture) addSector(t *testing.T, prefix string, count int) []instruments.UUID {
	t.Helper()
	ids := make([]instruments.UUID, 0, count)
	for index := 0; index < count; index++ {
		ids = append(ids, f.addInstrument(t, f.stockholm,
			fmt.Sprintf("SE90%08d", index), fmt.Sprintf("%s%03d", prefix, index),
			fmt.Sprintf("%s Holdings %03d AB", prefix, index), "SEK", "SE", "industrials", "Machinery"))
	}
	return ids
}
