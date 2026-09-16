package backtest_test

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"market-lens/server/internal/backtest"
	"market-lens/server/internal/db"
	"market-lens/server/internal/features"
	"market-lens/server/internal/strategies"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The backtest fixture builds the whole chain the simulation reads — bars, then engine values,
// then signals — rather than placing signals by hand. A backtest tested against hand-written
// signals could pass while disagreeing with the strategy it claims to replay, which is the one
// thing this feature must not be able to do.
//
// It spans three markets and three currencies on purpose. A single-market fixture would let a
// conversion bug through, and the accounting currency question is the one this product has never
// had to answer before.
const (
	backtestUniverse = "backtest-fixture-v1"
	backtestAsOf     = features.SessionDate("2026-06-30")
	backtestHistory  = 320

	// Ten Swedish, four Finnish, two Danish. The Finnish listings are in the accounting
	// currency, so a single run exercises both the converted and the unconverted path.
	backtestSwedish = 10
	backtestFinnish = 4
	backtestDanish  = 2

	backtestConfiguration = "fixture_equal_weight"
)

var (
	backtestUnivID   = backtest.UUID("eeeeeeee-0021-4000-8000-0000000000ff")
	backtestImportID = backtest.UUID("eeeeeeee-0021-4000-8000-0000000000aa")
)

type listing struct {
	id       backtest.UUID
	mic      string
	currency string
	seed     int
}

type backtestFixture struct {
	t         *testing.T
	pool      *pgxpool.Pool
	ctx       context.Context
	exchanges map[string]backtest.UUID
	listings  []listing
	sessions  map[string][]string
}

func newBacktestFixture(t *testing.T) *backtestFixture {
	t.Helper()
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	f := &backtestFixture{t: t, pool: pool, ctx: ctx,
		exchanges: map[string]backtest.UUID{}, sessions: map[string][]string{}}
	for _, mic := range []string{"XSTO", "XHEL", "XCSE"} {
		var id string
		if err := pool.QueryRow(ctx, `SELECT id::text FROM exchanges WHERE mic = $1`, mic).Scan(&id); err != nil {
			t.Fatalf("find exchange %s: %v", mic, err)
		}
		f.exchanges[mic] = backtest.UUID(id)
		f.sessions[mic] = f.openSessions(mic, backtestHistory)
	}
	f.exec(`INSERT INTO import_runs (id, kind, provider, status, started_at, finished_at, app_version)
		VALUES ($1, 'backfill', 'fixture', 'succeeded', now(), now(), 'test')`, backtestImportID.String())

	seed := 1
	add := func(mic, currency, country, isinPrefix, tickerPrefix string, count int) {
		for index := range count {
			id := backtest.UUID(fmt.Sprintf("eeeeeeee-0021-4000-8000-%012d", seed))
			f.addInstrument(id, mic, currency, country,
				fmt.Sprintf("%s0000021%03d", isinPrefix, seed),
				fmt.Sprintf("%s%d", tickerPrefix, index+1),
				fmt.Sprintf("Backtest Fixture %s %d", tickerPrefix, index+1))
			f.addBars(id, mic, seed)
			f.listings = append(f.listings, listing{id: id, mic: mic, currency: currency, seed: seed})
			seed++
		}
	}
	add("XSTO", "SEK", "SE", "SE", "BSE", backtestSwedish)
	add("XHEL", "EUR", "FI", "FI", "BFI", backtestFinnish)
	add("XCSE", "DKK", "DK", "DK", "BDK", backtestDanish)

	f.exec(`INSERT INTO research_universes (id, code, name, description)
		VALUES ($1, $2, 'Backtest fixture', 'the backtest test universe')`,
		backtestUnivID.String(), backtestUniverse)
	for _, l := range f.listings {
		f.exec(`INSERT INTO universe_memberships
			(universe_id, instrument_id, included_from, curation_source)
			VALUES ($1, $2, '2016-01-01', 'fixture')`, backtestUnivID.String(), l.id.String())
	}

	f.computeFeatures()
	f.computeSignals()
	f.addRates()
	f.addBenchmarkPoints()
	f.publishConfiguration(nil)
	return f
}

func (f *backtestFixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(f.ctx, sql, args...); err != nil {
		f.t.Fatalf("fixture: %v\n%s", err, sql)
	}
}

func (f *backtestFixture) openSessions(mic string, count int) []string {
	f.t.Helper()
	rows, err := f.pool.Query(f.ctx, `SELECT session_date::text FROM (
			SELECT s.session_date FROM exchange_sessions s
			JOIN exchanges e ON e.id = s.exchange_id
			WHERE e.mic = $1 AND s.status IN ('open', 'half_day') AND s.session_date <= $2
			ORDER BY s.session_date DESC LIMIT $3
		) recent ORDER BY session_date`, mic, backtestAsOf.String(), count)
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
		f.t.Fatalf("%s holds %d sessions, fewer than the %d requested", mic, len(sessions), count)
	}
	return sessions
}

func (f *backtestFixture) addInstrument(id backtest.UUID, mic, currency, country, isin, ticker, name string) {
	f.t.Helper()
	f.exec(`INSERT INTO instruments
		(id, exchange_id, isin, ticker, name, currency, country, instrument_type, active, purchasability_status, sector)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'common_stock', true, 'unverified', 'unclassified')`,
		id.String(), f.exchanges[mic].String(), isin, ticker, name, currency, country)
}

// backtestBar generates a price series from integer arithmetic on the bar's position, so a
// reviewer can reproduce it outside the repository. The seed decorrelates the instruments:
// without it every one would move identically, every cross-sectional rank would be a tie, and a
// ranking bug would agree with the test.
func backtestBar(seed, position int) (open, high, low, close, volume int64) {
	i, s := int64(position), int64(seed)
	close = 10000 + 400*s + 20*i + ((i*i*7919+i*104729+s*31)%601 - 300)
	high = close + (i*29+s*13)%250 + 40
	low = close - (i*19+s*11)%250 - 40
	open = (high + low) / 2
	volume = 1000 + (i*7919+s*101)%6000
	return
}

func (f *backtestFixture) addBars(instrument backtest.UUID, mic string, seed int) {
	f.t.Helper()
	sessions := f.sessions[mic]
	var opens, highs, lows, closes, volumes []int64
	for position := range sessions {
		open, high, low, close, volume := backtestBar(seed, position)
		opens, highs, lows = append(opens, open), append(highs, high), append(lows, low)
		closes, volumes = append(closes, close), append(volumes, volume)
	}
	var currency string
	if err := f.pool.QueryRow(f.ctx, `SELECT currency FROM exchanges WHERE mic = $1`, mic).Scan(&currency); err != nil {
		f.t.Fatal(err)
	}
	f.exec(`INSERT INTO daily_price_bars
		(instrument_id, session_date, open, high, low, close, adjusted_close, volume,
		 currency, provider, source_hash, import_run_id, first_observed_at, last_observed_at)
		SELECT $1::uuid, d::date,
		       round(o::numeric / 100, 8), round(h::numeric / 100, 8),
		       round(l::numeric / 100, 8), round(c::numeric / 100, 8),
		       NULL, v, $9, 'fixture', 'hash-' || $8::text || '-' || d, $8::uuid,
		       d::date::timestamptz, d::date::timestamptz
		FROM unnest($2::text[], $3::bigint[], $4::bigint[], $5::bigint[], $6::bigint[], $7::bigint[])
		     AS bars(d, o, h, l, c, v)`,
		instrument.String(), sessions, opens, highs, lows, closes, volumes,
		backtestImportID.String(), currency)
}

func (f *backtestFixture) computeFeatures() {
	f.t.Helper()
	service := features.NewService(features.NewRepository(f.pool), slog.Default())
	run, err := service.Compute(f.ctx, features.ComputeRequest{
		Kind: features.RunKindFull, Universe: backtestUniverse, Workers: 4, AppVersion: "test"})
	if err != nil {
		f.t.Fatalf("compute features: %v", err)
	}
	if run.Status != features.RunStatusSucceeded {
		f.t.Fatalf("the feature run ended %s", run.Status)
	}
}

func (f *backtestFixture) computeSignals() {
	f.t.Helper()
	service := strategies.NewService(strategies.NewRepository(f.pool),
		features.NewRepository(f.pool), slog.Default())
	run, err := service.Compute(f.ctx, strategies.ComputeRequest{
		Kind: strategies.RunKindFull, Universe: backtestUniverse, Workers: 4, AppVersion: "test"})
	if err != nil {
		f.t.Fatalf("compute signals: %v", err)
	}
	if run.Status != strategies.RunStatusSucceeded {
		f.t.Fatalf("the signal run ended %s", run.Status)
	}
}

// addRates stores EURSEK and EURDKK for every session any market in the fixture traded on. The
// rates drift deterministically so a conversion bug cannot hide behind a constant.
func (f *backtestFixture) addRates() {
	f.t.Helper()
	for _, pair := range []struct {
		quote string
		base  int64
	}{{"SEK", 9400000000000}, {"DKK", 7440000000000}} {
		f.exec(`INSERT INTO fx_rates (base, quote, session_date, rate)
			SELECT 'EUR', $1, d::date,
			       ($2::numeric + (row_number() OVER (ORDER BY d) % 40) * 1000000000) / 1000000000000
			FROM (SELECT DISTINCT session_date AS d FROM daily_price_bars) s
			ORDER BY d`, pair.quote, pair.base)
	}
}

// addBenchmarkPoints stores a close for every session each market traded on. The Danish series
// deliberately starts a third of the way in, which is the real coverage gap OMXC25.INDX has
// against this product's stored history.
func (f *backtestFixture) addBenchmarkPoints() {
	f.t.Helper()
	for _, mic := range []string{"XSTO", "XHEL", "XCSE"} {
		sessions := f.sessions[mic]
		if mic == "XCSE" {
			sessions = sessions[len(sessions)/3:]
		}
		var closes []int64
		for position := range sessions {
			_, _, _, close, _ := backtestBar(len(mic), position)
			closes = append(closes, close)
		}
		f.exec(`INSERT INTO benchmark_points (series_id, session_date, close)
			SELECT b.id, d::date, round(c::numeric / 100, 8)
			FROM benchmark_series b, unnest($2::text[], $3::bigint[]) AS p(d, c)
			WHERE b.mic = $1`, mic, sessions, closes)
	}
}

// publishConfiguration writes the fixture's own configuration. Overrides replace individual
// parameters so a test can state the one rule it is about — a schedule, a cost, a currency —
// without restating the other twelve.
func (f *backtestFixture) publishConfiguration(overrides map[string]any) {
	f.t.Helper()
	parameters := map[string]any{
		"strategy_name":       "momentum_trend",
		"strategy_version":    1,
		"universe_code":       backtestUniverse,
		"starting_capital":    "1000000",
		"accounting_currency": "EUR",
		"sizing_rule":         "equal_weight_top_n",
		"sizing_n":            5,
		"rebalance":           "monthly",
		"brokerage_bps":       "10",
		"brokerage_minimum":   "5",
		"slippage_bps":        "5",
		"spread_bps":          "10",
		"give_up_sessions":    5,
	}
	for key, value := range overrides {
		parameters[key] = value
	}
	// Supersede rather than replace, exactly as production does. A configuration a result was
	// recorded under is never edited or removed, so a test that changes one rule leaves the
	// earlier result still readable from the rules that produced it.
	f.exec(`UPDATE backtest_configurations SET superseded_at = now()
		WHERE name = $1 AND superseded_at IS NULL`, backtestConfiguration)
	f.exec(`INSERT INTO backtest_configurations
		(id, name, version, title, intent, caveat, parameters, published_at)
		SELECT gen_random_uuid(), $1, coalesce(max(version), 0) + 1, 'Backtest fixture',
		       'the backtest test configuration',
		       'a simulation over past data, not a prediction', $2, now()
		FROM backtest_configurations WHERE name = $1`, backtestConfiguration, parameters)
}

// stopTrading removes an instrument's bars after a session, as delisting looks from inside this
// product: the signals stay, because the strategy did say something at the time, and the prices
// simply stop. It is how the two hardest edge cases are reached — a trade that cannot execute and
// a holding that can no longer be valued.
func (f *backtestFixture) stopTrading(instrument backtest.UUID, after string) {
	f.t.Helper()
	f.exec(`DELETE FROM daily_price_bars WHERE instrument_id = $1 AND session_date > $2::date`,
		instrument.String(), after)
}

// session returns the nth session of the union calendar the simulation will walk.
func (f *backtestFixture) session(n int) string {
	f.t.Helper()
	var value string
	if err := f.pool.QueryRow(f.ctx, `SELECT session_date::text FROM (
			SELECT DISTINCT session_date FROM daily_price_bars ORDER BY session_date OFFSET $1 LIMIT 1
		) s`, n).Scan(&value); err != nil {
		f.t.Fatalf("read session %d: %v", n, err)
	}
	return value
}

// confineToCurrency narrows the universe to the listings in one currency, so a run can be asked
// the single-currency question without a second fixture.
func (f *backtestFixture) confineToCurrency(currency string) {
	f.t.Helper()
	f.exec(`DELETE FROM universe_memberships m USING instruments i
		WHERE i.id = m.instrument_id AND m.universe_id = $1 AND i.currency <> $2`,
		backtestUnivID.String(), currency)
}

func (f *backtestFixture) runExpectingError() error {
	f.t.Helper()
	_, err := f.service().Run(f.ctx, backtest.RunRequest{
		Configuration: backtestConfiguration, AppVersion: "test"})
	return err
}

func (f *backtestFixture) run() backtest.Run {
	f.t.Helper()
	run, err := f.service().Run(f.ctx, backtest.RunRequest{
		Configuration: backtestConfiguration, AppVersion: "test"})
	if err != nil {
		f.t.Fatalf("run the backtest: %v", err)
	}
	return run
}

func (f *backtestFixture) service() *backtest.Service {
	return backtest.NewService(backtest.NewRepository(f.pool), slog.Default())
}

func (f *backtestFixture) count(sql string, args ...any) int64 {
	f.t.Helper()
	var total int64
	if err := f.pool.QueryRow(f.ctx, sql, args...).Scan(&total); err != nil {
		f.t.Fatalf("count: %v\n%s", err, sql)
	}
	return total
}

// snapshot copies a result's tables so a recomputation can be compared with it field by field.
func (f *backtestFixture) snapshot(name, table string) {
	f.t.Helper()
	f.exec(`DROP TABLE IF EXISTS ` + name)
	f.exec(`CREATE TABLE ` + name + ` AS SELECT * FROM ` + table)
}
