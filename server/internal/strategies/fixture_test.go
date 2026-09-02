package strategies_test

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"market-lens/server/internal/db"
	"market-lens/server/internal/features"
	"market-lens/server/internal/strategies"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The strategy fixture computes the feature engine's output first, then scores it. Nothing here
// writes a feature value directly: a strategy that were tested against hand-placed features
// could pass while disagreeing with the engine it is supposed to read.
//
// The universe is deliberately wider than the engine's own fixtures need to be, because a
// cross-sectional factor is a comparison: with three instruments a percentile rank can only be
// -1, 0 or +1, and a test could agree with a ranking bug by never exercising a middle position.
const (
	strategyExchangeMIC = "XSTO"
	strategyUniverse    = "strategy-fixture-v1"
	strategyAsOf        = features.SessionDate("2026-06-30")

	// strategyRankedCount instruments carry a full history, so every one of them can be scored
	// and ranked against the others.
	strategyRankedCount = 10
	strategyDeepHistory = 320
	// strategyShortHistory is below the strategy's minimum, so one instrument must record an
	// absence rather than a middling score.
	strategyShortHistory = 40
)

var (
	strategyUnivID = features.UUID("ffffffff-0015-4000-8000-0000000000ff")
	strategyRunID  = features.UUID("ffffffff-0015-4000-8000-0000000000aa")
	// strategyShort is the instrument with too little history; strategyBare has none at all.
	strategyShort = features.UUID("ffffffff-0015-4000-8000-000000000091")
	strategyBare  = features.UUID("ffffffff-0015-4000-8000-000000000092")
)

func strategyRanked(index int) features.UUID {
	return features.UUID(fmt.Sprintf("ffffffff-0015-4000-8000-0000000000%02d", index+1))
}

// strategyBar generates a price series from integer arithmetic on the bar's position, so a
// reviewer can reproduce it outside the repository. The seed decorrelates the instruments:
// without it every instrument would move identically and every cross-sectional rank would be a
// tie, which is exactly the case a ranking test must not be limited to.
func strategyBar(seed, position int) (open, high, low, close, volume int64) {
	i, s := int64(position), int64(seed)
	close = 10000 + 400*s + 20*i + ((i*i*7919+i*104729+s*31)%601 - 300)
	high = close + (i*29+s*13)%250 + 40
	low = close - (i*19+s*11)%250 - 40
	open = (high + low) / 2
	volume = 1000 + (i*7919+s*101)%6000
	return
}

type strategyFixture struct {
	t        *testing.T
	pool     *pgxpool.Pool
	ctx      context.Context
	exchange features.UUID
	// endOffset is how many open sessions short of the as-of date the fixture's history stops,
	// leaving room for extendHistory to add sessions the first computation could not have seen.
	endOffset int
	// positions remembers how far each instrument's generated series has run, so an extension
	// continues it rather than restarting and quietly rewriting the past.
	positions map[features.UUID]int
	seeds     map[features.UUID]int
}

func newStrategyFixture(t *testing.T) *strategyFixture {
	return newStrategyFixtureEndingBefore(t, 0)
}

// newStrategyFixtureEndingBefore stops the history short of the as-of date so a later extension
// can add sessions that did not exist when the first signals were computed.
func newStrategyFixtureEndingBefore(t *testing.T, sessionsBack int) *strategyFixture {
	t.Helper()
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	f := &strategyFixture{t: t, pool: pool, ctx: ctx, endOffset: sessionsBack,
		positions: map[features.UUID]int{}, seeds: map[features.UUID]int{}}
	if err := pool.QueryRow(ctx, `SELECT id::text FROM exchanges WHERE mic = $1`, strategyExchangeMIC).
		Scan((*string)(&f.exchange)); err != nil {
		t.Fatalf("find the fixture exchange: %v", err)
	}
	f.exec(`INSERT INTO import_runs (id, kind, provider, status, started_at, finished_at, app_version)
		VALUES ($1, 'backfill', 'fixture', 'succeeded', now(), now(), 'test')`, strategyRunID.String())

	members := make([]features.UUID, 0, strategyRankedCount+2)
	for index := range strategyRankedCount {
		id := strategyRanked(index)
		f.addInstrument(id, fmt.Sprintf("SE00000015%02d", index), fmt.Sprintf("STR%d", index+1),
			fmt.Sprintf("Strategy Fixture %d AB", index+1))
		f.addBars(id, index+1, strategyDeepHistory)
		members = append(members, id)
	}
	f.addInstrument(strategyShort, "SE0000001591", "STRS", "Strategy Fixture Short AB")
	f.addBars(strategyShort, 91, strategyShortHistory)
	f.addInstrument(strategyBare, "SE0000001592", "STRB", "Strategy Fixture Bare AB")
	members = append(members, strategyShort, strategyBare)

	f.exec(`INSERT INTO research_universes (id, code, name, description)
		VALUES ($1, $2, 'Strategy fixture', 'the strategy test universe')`,
		strategyUnivID.String(), strategyUniverse)
	for _, member := range members {
		f.exec(`INSERT INTO universe_memberships
			(universe_id, instrument_id, included_from, curation_source)
			VALUES ($1, $2, '2016-01-01', 'fixture')`, strategyUnivID.String(), member.String())
	}

	f.computeFeatures()
	return f
}

func (f *strategyFixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(f.ctx, sql, args...); err != nil {
		f.t.Fatalf("fixture: %v\n%s", err, sql)
	}
}

func (f *strategyFixture) addInstrument(id features.UUID, isin, ticker, name string) {
	f.t.Helper()
	f.exec(`INSERT INTO instruments
		(id, exchange_id, isin, ticker, name, currency, country, instrument_type, active, purchasability_status)
		VALUES ($1, $2, $3, $4, $5, 'SEK', 'SE', 'common_stock', true, 'unverified')`,
		id.String(), f.exchange.String(), isin, ticker, name)
}

// openSessions returns the `count` open sessions ending `skip` sessions before the as-of date.
func (f *strategyFixture) openSessions(count, skip int) []string {
	f.t.Helper()
	rows, err := f.pool.Query(f.ctx, `SELECT session_date::text FROM (
			SELECT session_date FROM exchange_sessions
			WHERE exchange_id = $1 AND status IN ('open', 'half_day') AND session_date <= $2
			ORDER BY session_date DESC OFFSET $4 LIMIT $3
		) recent ORDER BY session_date`, f.exchange.String(), strategyAsOf.String(), count, skip)
	if err != nil {
		f.t.Fatalf("list open sessions: %v", err)
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
		f.t.Fatalf("the calendar holds %d sessions, fewer than the %d requested", len(sessions), count)
	}
	return sessions
}

func (f *strategyFixture) addBars(instrument features.UUID, seed, count int) {
	f.t.Helper()
	f.seeds[instrument] = seed
	sessions := f.openSessions(count, f.endOffset)
	var opens, highs, lows, closes, volumes []int64
	for position := range sessions {
		open, high, low, close, volume := strategyBar(seed, position)
		opens, highs, lows = append(opens, open), append(highs, high), append(lows, low)
		closes, volumes = append(closes, close), append(volumes, volume)
	}
	f.insertBars(instrument, strategyRunID, sessions, opens, highs, lows, closes, volumes)
	f.positions[instrument] += len(sessions)
}

// extendHistory appends `count` newer sessions to every instrument that already has bars,
// continuing each generated series. It exists so a test can ask the question that matters for
// leakage: does knowing what happened next change what the strategy said before it happened?
func (f *strategyFixture) extendHistory(count int) features.UUID {
	f.t.Helper()
	if count > f.endOffset {
		f.t.Fatalf("the fixture reserved %d sessions, not %d", f.endOffset, count)
	}
	runID := f.newImportRun()
	sessions := f.openSessions(count, f.endOffset-count)
	for instrument, position := range f.positions {
		if position == 0 {
			continue
		}
		seed := f.seeds[instrument]
		var opens, highs, lows, closes, volumes []int64
		for offset := range sessions {
			open, high, low, close, volume := strategyBar(seed, position+offset)
			opens, highs, lows = append(opens, open), append(highs, high), append(lows, low)
			closes, volumes = append(closes, close), append(volumes, volume)
		}
		f.insertBars(instrument, runID, sessions, opens, highs, lows, closes, volumes)
		f.positions[instrument] += len(sessions)
	}
	f.endOffset -= count
	return runID
}

// reviseBar restates one instrument's close deep in its history, as a provider correction would.
func (f *strategyFixture) reviseBar(instrument features.UUID, sessionsBack int, closeCents int64) (features.UUID, features.SessionDate) {
	f.t.Helper()
	sessions := f.openSessions(1, f.endOffset+sessionsBack)
	runID := f.newImportRun()
	f.insertBars(instrument, runID, sessions,
		[]int64{closeCents}, []int64{closeCents + 100}, []int64{closeCents - 100},
		[]int64{closeCents}, []int64{4000})
	return runID, features.SessionDate(sessions[0])
}

func (f *strategyFixture) newImportRun() features.UUID {
	f.t.Helper()
	var id string
	if err := f.pool.QueryRow(f.ctx, `INSERT INTO import_runs
		(id, kind, provider, status, started_at, finished_at, app_version)
		VALUES (gen_random_uuid(), 'daily_update', 'fixture', 'succeeded', now(), now(), 'test')
		RETURNING id::text`).Scan(&id); err != nil {
		f.t.Fatalf("create an import run: %v", err)
	}
	return features.UUID(id)
}

func (f *strategyFixture) insertBars(instrument, runID features.UUID, sessions []string,
	opens, highs, lows, closes, volumes []int64) {
	f.t.Helper()
	f.exec(`INSERT INTO daily_price_bars
		(instrument_id, session_date, open, high, low, close, adjusted_close, volume,
		 currency, provider, source_hash, import_run_id, first_observed_at, last_observed_at)
		SELECT $1::uuid, d::date,
		       round(o::numeric / 100, 8), round(h::numeric / 100, 8),
		       round(l::numeric / 100, 8), round(c::numeric / 100, 8),
		       NULL, v, 'SEK', 'fixture', 'hash-' || $8::text || '-' || d, $8::uuid,
		       d::date::timestamptz, d::date::timestamptz
		FROM unnest($2::text[], $3::bigint[], $4::bigint[], $5::bigint[], $6::bigint[], $7::bigint[])
		     AS bars(d, o, h, l, c, v)
		ON CONFLICT (instrument_id, session_date) DO UPDATE SET
		    open = excluded.open, high = excluded.high, low = excluded.low, close = excluded.close,
		    volume = excluded.volume, source_hash = excluded.source_hash,
		    import_run_id = excluded.import_run_id, last_observed_at = excluded.last_observed_at`,
		instrument.String(), sessions, opens, highs, lows, closes, volumes, runID.String())
}

func (f *strategyFixture) computeFeatures() features.Run {
	return f.computeFeatureRun(features.ComputeRequest{Kind: features.RunKindFull})
}

func (f *strategyFixture) computeFeatureRun(request features.ComputeRequest) features.Run {
	f.t.Helper()
	request.Universe, request.Workers, request.AppVersion = strategyUniverse, 4, "test"
	service := features.NewService(features.NewRepository(f.pool), slog.Default())
	run, err := service.Compute(f.ctx, request)
	if err != nil {
		f.t.Fatalf("compute features: %v", err)
	}
	if run.Status != features.RunStatusSucceeded {
		f.t.Fatalf("the feature run ended %s", run.Status)
	}
	return run
}

// compute scores the fixture universe with the current published strategy.
func (f *strategyFixture) compute(request strategies.ComputeRequest) strategies.Run {
	f.t.Helper()
	if request.Kind == "" {
		request.Kind = strategies.RunKindFull
	}
	if request.Universe == "" {
		request.Universe = strategyUniverse
	}
	if request.AppVersion == "" {
		request.AppVersion = "test"
	}
	if request.Workers == 0 {
		request.Workers = 4
	}
	service := strategies.NewService(strategies.NewRepository(f.pool),
		features.NewRepository(f.pool), slog.Default())
	run, err := service.Compute(f.ctx, request)
	if err != nil {
		f.t.Fatalf("compute signals: %v", err)
	}
	return run
}

func (f *strategyFixture) count(sql string, args ...any) int64 {
	f.t.Helper()
	var total int64
	if err := f.pool.QueryRow(f.ctx, sql, args...).Scan(&total); err != nil {
		f.t.Fatalf("count: %v\n%s", err, sql)
	}
	return total
}

// snapshot copies the signal table so a recomputation can be compared with it field by field.
func (f *strategyFixture) snapshot(name string) {
	f.t.Helper()
	f.exec(`DROP TABLE IF EXISTS ` + name)
	f.exec(`CREATE TABLE ` + name + ` AS SELECT * FROM signals`)
}

// changedSignals counts signals that differ from a snapshot in any field a reader can see.
func (f *strategyFixture) changedSignals(name string) int64 {
	f.t.Helper()
	return f.count(`SELECT count(*) FROM signals s JOIN ` + name + ` p
		USING (instrument_id, session_date, strategy_id)
		WHERE s.score IS DISTINCT FROM p.score
		   OR s.action IS DISTINCT FROM p.action
		   OR s.confidence IS DISTINCT FROM p.confidence
		   OR s.absence_reason IS DISTINCT FROM p.absence_reason
		   OR s.divisor IS DISTINCT FROM p.divisor
		   OR s.contributions::text IS DISTINCT FROM p.contributions::text`)
}
