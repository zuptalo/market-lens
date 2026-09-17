package portfolio_test

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"market-lens/server/internal/db"
	"market-lens/server/internal/portfolio"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The portfolio fixture builds two people and the shared reference data a holding needs — real
// instruments across three currencies, real stored bars, and the benchmark and rate series feature
// 021 imports. Two people rather than one, always: this is the product's first user-owned domain
// record, and a fixture with a single user cannot fail an isolation test.

const (
	fixtureSessions = 40
	alfaTicker      = "PALFA"
	betaTicker      = "PBETA"
	danaTicker      = "PDANA"
)

var (
	aliceID  = portfolio.UUID("70000000-0022-4000-8000-000000000001")
	bobID    = portfolio.UUID("70000000-0022-4000-8000-000000000002")
	importID = portfolio.UUID("70000000-0022-4000-8000-0000000000aa")
)

type portfolioFixture struct {
	t        *testing.T
	pool     *pgxpool.Pool
	ctx      context.Context
	sessions map[string][]string
	// instruments maps a ticker to its identifier, so a test names what it means rather than a UUID.
	instruments map[string]portfolio.UUID
}

func newPortfolioFixture(t *testing.T) *portfolioFixture {
	t.Helper()
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	f := &portfolioFixture{t: t, pool: pool, ctx: ctx,
		sessions: map[string][]string{}, instruments: map[string]portfolio.UUID{}}

	for index, id := range []portfolio.UUID{aliceID, bobID} {
		email := fmt.Sprintf("portfolio%d@example.com", index+1)
		role := "owner"
		if index == 1 {
			role = "member"
		}
		f.exec(`INSERT INTO users
			(id, email, normalized_email, display_name, role, status, email_verified_at, created_at, updated_at)
			VALUES ($1, $2, $2, $2, $3, 'active', now(), now(), now())`, id.String(), email, role)
	}

	f.exec(`INSERT INTO import_runs (id, kind, provider, status, started_at, finished_at, app_version)
		VALUES ($1, 'backfill', 'fixture', 'succeeded', now(), now(), 'test')`, importID.String())

	// Three markets and three currencies, so a conversion that crosses through the euro is
	// exercised by the ordinary fixture rather than by a special case.
	f.addInstrument("XSTO", "SEK", "SE", alfaTicker, "SE0000002201", 1)
	f.addInstrument("XHEL", "EUR", "FI", betaTicker, "FI0000002202", 2)
	f.addInstrument("XCSE", "DKK", "DK", danaTicker, "DK0000002203", 3)
	f.addRates()
	f.addBenchmarkPoints()
	return f
}

func (f *portfolioFixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(f.ctx, sql, args...); err != nil {
		f.t.Fatalf("fixture: %v\n%s", err, sql)
	}
}

func (f *portfolioFixture) openSessions(mic string) []string {
	f.t.Helper()
	if cached, ok := f.sessions[mic]; ok {
		return cached
	}
	rows, err := f.pool.Query(f.ctx, `SELECT session_date::text FROM (
			SELECT s.session_date FROM exchange_sessions s
			JOIN exchanges e ON e.id = s.exchange_id
			WHERE e.mic = $1 AND s.status IN ('open', 'half_day') AND s.session_date <= current_date
			ORDER BY s.session_date DESC LIMIT $2
		) recent ORDER BY session_date`, mic, fixtureSessions)
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
	if len(sessions) < fixtureSessions {
		f.t.Fatalf("%s holds %d sessions, fewer than %d", mic, len(sessions), fixtureSessions)
	}
	f.sessions[mic] = sessions
	return sessions
}

// addInstrument creates one listing and a deterministic price series for it. The closes rise
// steadily so a test can state what a profit should be without reproducing a generator.
func (f *portfolioFixture) addInstrument(mic, currency, country, ticker, isin string, seed int) {
	f.t.Helper()
	id := portfolio.UUID(fmt.Sprintf("70000000-0022-4000-8000-0000000001%02d", seed))
	var exchangeID string
	if err := f.pool.QueryRow(f.ctx, `SELECT id::text FROM exchanges WHERE mic = $1`, mic).
		Scan(&exchangeID); err != nil {
		f.t.Fatalf("find exchange %s: %v", mic, err)
	}
	f.exec(`INSERT INTO instruments
		(id, exchange_id, isin, ticker, name, currency, country, instrument_type, active,
		 purchasability_status, sector)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'common_stock', true, 'unverified', 'unclassified')`,
		id.String(), exchangeID, isin, ticker, "Portfolio Fixture "+ticker, currency, country)
	f.instruments[ticker] = id

	sessions := f.openSessions(mic)
	// close in minor units: 100.00 rising by 1.00 a session, offset by the seed.
	var closes []int64
	for position := range sessions {
		closes = append(closes, int64(10000+seed*500+position*100))
	}
	f.exec(`INSERT INTO daily_price_bars
		(instrument_id, session_date, open, high, low, close, adjusted_close, volume,
		 currency, provider, source_hash, import_run_id, first_observed_at, last_observed_at)
		SELECT $1::uuid, d::date,
		       round(c::numeric / 100, 8), round((c + 50)::numeric / 100, 8),
		       round((c - 50)::numeric / 100, 8), round(c::numeric / 100, 8),
		       NULL, 1000, $4, 'fixture', 'hash-' || d, $5::uuid,
		       d::date::timestamptz, d::date::timestamptz
		FROM unnest($2::text[], $3::bigint[]) AS bars(d, c)`,
		id.String(), sessions, closes, currency, importID.String())
}

// addRates stores EURSEK and EURDKK for every session any fixture market traded. Fixed rather than
// drifting, so a conversion a test asserts by hand is reproducible.
func (f *portfolioFixture) addRates() {
	f.t.Helper()
	for quote, rate := range map[string]string{"SEK": "11.000000000000", "DKK": "7.500000000000"} {
		f.exec(`INSERT INTO fx_rates (base, quote, session_date, rate)
			SELECT 'EUR', $1, d::date, $2::numeric
			FROM (SELECT DISTINCT session_date AS d FROM daily_price_bars) s`, quote, rate)
	}
}

func (f *portfolioFixture) addBenchmarkPoints() {
	f.t.Helper()
	for _, mic := range []string{"XSTO", "XHEL", "XCSE"} {
		sessions := f.openSessions(mic)
		var closes []int64
		for position := range sessions {
			// The benchmark rises more slowly than the instruments, so a holding that beat its
			// market and one that did not are both reachable.
			closes = append(closes, int64(100000+position*50))
		}
		f.exec(`INSERT INTO benchmark_points (series_id, session_date, close)
			SELECT b.id, d::date, round(c::numeric / 100, 8)
			FROM benchmark_series b, unnest($2::text[], $3::bigint[]) AS p(d, c)
			WHERE b.mic = $1`, mic, sessions, closes)
	}
}

// session returns the nth session of a market's calendar, so a test can say "the day I bought".
func (f *portfolioFixture) session(mic string, n int) portfolio.SessionDate {
	f.t.Helper()
	return portfolio.SessionDate(f.openSessions(mic)[n])
}

func (f *portfolioFixture) service() *portfolio.Service {
	return portfolio.NewService(portfolio.NewRepository(f.pool), slog.Default())
}

func (f *portfolioFixture) count(sql string, args ...any) int64 {
	f.t.Helper()
	var total int64
	if err := f.pool.QueryRow(f.ctx, sql, args...).Scan(&total); err != nil {
		f.t.Fatalf("count: %v\n%s", err, sql)
	}
	return total
}

// record is the ordinary path a test takes to put a trade in, so a test states what happened
// rather than how it was stored.
func (f *portfolioFixture) record(user portfolio.UUID, ticker string, direction portfolio.Direction,
	quantity, price, costs string, date portfolio.SessionDate) portfolio.Trade {
	f.t.Helper()
	trade, err := f.service().Record(f.ctx, user.String(), portfolio.RecordRequest{
		InstrumentID: f.instruments[ticker], Direction: direction,
		Quantity: quantity, Price: price, Costs: costs, TradeDate: date,
	})
	if err != nil {
		f.t.Fatalf("record %s %s: %v", direction, ticker, err)
	}
	return trade
}

func (f *portfolioFixture) view(user portfolio.UUID) portfolio.View {
	f.t.Helper()
	view, err := f.service().View(f.ctx, user.String())
	if err != nil {
		f.t.Fatalf("read the portfolio: %v", err)
	}
	return view
}
