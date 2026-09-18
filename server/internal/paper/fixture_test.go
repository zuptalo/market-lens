package paper_test

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"market-lens/server/internal/db"
	"market-lens/server/internal/intents"
	"market-lens/server/internal/paper"
	"market-lens/server/internal/portfolio"
	"market-lens/server/internal/risk"
	"market-lens/server/internal/testdb"
)

// Two people and one market, with bars whose open differs from their close.
//
// The open matters: this feature fills at one, and a fixture where open equals close cannot tell a
// correct implementation from one that read the close. So the open is the close minus a known
// amount, and every fill assertion names the open it expects.

const (
	fixtureSessions = 30
	aTicker         = "PAPRA"
	bTicker         = "PAPRB"
	// euroTicker lists in the accounting currency, so a conversion that does not happen is
	// exercised alongside one that does.
	euroTicker = "PAPRE"
)

var (
	aliceID  = paper.UUID("70000000-0026-4000-8000-000000000001")
	bobID    = paper.UUID("70000000-0026-4000-8000-000000000002")
	importID = paper.UUID("70000000-0026-4000-8000-0000000000aa")
)

type fixture struct {
	t           *testing.T
	pool        *pgxpool.Pool
	ctx         context.Context
	sessions    []string
	instruments map[string]paper.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	f := &fixture{t: t, pool: pool, ctx: ctx, instruments: map[string]paper.UUID{}}

	for index, id := range []paper.UUID{aliceID, bobID} {
		email := fmt.Sprintf("paper%d@example.com", index+1)
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

	f.addInstrument("XSTO", "SEK", "SE", aTicker, "SE0000002601", 1)
	f.addInstrument("XSTO", "SEK", "SE", bTicker, "SE0000002602", 2)
	f.addInstrument("XHEL", "EUR", "FI", euroTicker, "FI0000002603", 3)
	f.addRates()
	f.addBenchmarkPoints()
	return f
}

func (f *fixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(f.ctx, sql, args...); err != nil {
		f.t.Fatalf("fixture: %v\n%s", err, sql)
	}
}

func (f *fixture) openSessions(mic string) []string {
	f.t.Helper()
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
	if mic == "XSTO" {
		f.sessions = sessions
	}
	return sessions
}

// addInstrument creates a listing and a deterministic series. The open is two whole units below the
// close, so a test can prove which of the two a fill read.
func (f *fixture) addInstrument(mic, currency, country, ticker, isin string, seed int) {
	f.t.Helper()
	id := paper.UUID(fmt.Sprintf("70000000-0026-4000-8000-0000000001%02d", seed))
	var exchangeID string
	if err := f.pool.QueryRow(f.ctx, `SELECT id::text FROM exchanges WHERE mic = $1`, mic).
		Scan(&exchangeID); err != nil {
		f.t.Fatalf("find exchange %s: %v", mic, err)
	}
	f.exec(`INSERT INTO instruments
		(id, exchange_id, isin, ticker, name, currency, country, instrument_type, active,
		 purchasability_status, sector)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'common_stock', true, 'unverified', 'unclassified')`,
		id.String(), exchangeID, isin, ticker, "Paper Fixture "+ticker, currency, country)
	f.instruments[ticker] = id

	sessions := f.openSessions(mic)
	var closes []int64
	for position := range sessions {
		closes = append(closes, int64(10000+seed*500+position*100))
	}
	f.exec(`INSERT INTO daily_price_bars
		(instrument_id, session_date, open, high, low, close, adjusted_close, volume,
		 currency, provider, source_hash, import_run_id, first_observed_at, last_observed_at)
		SELECT $1::uuid, d::date,
		       round((c - 200)::numeric / 100, 8), round((c + 50)::numeric / 100, 8),
		       round((c - 250)::numeric / 100, 8), round(c::numeric / 100, 8),
		       NULL, 1000, $4, 'fixture', 'hash-' || d, $5::uuid,
		       d::date::timestamptz, d::date::timestamptz
		FROM unnest($2::text[], $3::bigint[]) AS bars(d, c)`,
		id.String(), sessions, closes, currency, importID.String())
}

func (f *fixture) addRates() {
	f.t.Helper()
	f.exec(`INSERT INTO fx_rates (base, quote, session_date, rate)
		SELECT 'EUR', 'SEK', d::date, 11.000000000000
		FROM (SELECT DISTINCT session_date AS d FROM daily_price_bars) s`)
}

func (f *fixture) addBenchmarkPoints() {
	f.t.Helper()
	for _, mic := range []string{"XSTO", "XHEL"} {
		sessions := f.openSessions(mic)
		var closes []int64
		for position := range sessions {
			closes = append(closes, int64(100000+position*50))
		}
		f.exec(`INSERT INTO benchmark_points (series_id, session_date, close)
			SELECT b.id, d::date, round(c::numeric / 100, 8)
			FROM benchmark_series b, unnest($2::text[], $3::bigint[]) AS p(d, c)
			WHERE b.mic = $1`, mic, sessions, closes)
	}
}

// session is the nth session of the Stockholm calendar, so a test can say "the day I promoted it".
func (f *fixture) session(n int) string {
	f.t.Helper()
	return f.openSessions("XSTO")[n]
}

// openOf is the open this fixture stored for a session, at the precision a fill records it in, so
// an assertion names the price it expects rather than recomputing the generator.
//
// The cast matters: a bar is numeric(20,8) and a fill is numeric(24,12), so the same price reads
// as 114.00000000 from one and 114.000000000000 from the other.
func (f *fixture) openOf(ticker, session string) string {
	f.t.Helper()
	var open string
	if err := f.pool.QueryRow(f.ctx, `SELECT open::numeric(24,12)::text FROM daily_price_bars
		WHERE instrument_id = $1 AND session_date = $2::date`,
		f.instruments[ticker].String(), session).Scan(&open); err != nil {
		f.t.Fatalf("open of %s on %s: %v", ticker, session, err)
	}
	return open
}

func (f *fixture) service() *paper.Service {
	return paper.NewService(paper.NewRepository(f.pool),
		f.intentsService(), portfolio.NewService(portfolio.NewRepository(f.pool), slog.Default()),
		slog.Default())
}

func (f *fixture) intentsService() *intents.Service {
	holdings := portfolio.NewService(portfolio.NewRepository(f.pool), slog.Default())
	return intents.NewService(intents.NewRepository(f.pool), holdings,
		risk.NewService(risk.NewRepository(f.pool), holdings, slog.Default()), slog.Default())
}

// open is the ordinary path to an account, so a test states what it has rather than how it got it.
func (f *fixture) open(user paper.UUID, cash, currency string) paper.Account {
	f.t.Helper()
	account, err := f.service().Open(f.ctx, user.String(), paper.OpenRequest{
		StartingCash: cash, AccountingCurrency: currency,
	})
	if err != nil {
		f.t.Fatalf("open an account for %s: %v", user, err)
	}
	return account
}

// consider writes down an intent, which is the only thing an order can be promoted from.
func (f *fixture) consider(user paper.UUID, ticker string, direction intents.Direction,
	quantity, price string) intents.Intent {
	f.t.Helper()
	intent, err := f.intentsService().Record(f.ctx, user.String(), intents.RecordRequest{
		InstrumentID: f.instruments[ticker], Direction: direction,
		Quantity: quantity, Price: price, Costs: "0",
	})
	if err != nil {
		f.t.Fatalf("consider %s: %v", ticker, err)
	}
	return intent
}

// promote is the only way an order comes into existence.
func (f *fixture) promote(user paper.UUID, intent intents.Intent, session string) paper.Order {
	f.t.Helper()
	order, err := f.service().Promote(f.ctx, user.String(), paper.PromoteRequest{
		IntentID: paper.UUID(intent.ID), PlacedSession: paper.SessionDate(session),
	})
	if err != nil {
		f.t.Fatalf("promote: %v", err)
	}
	return order
}

// fillPass is the pass that runs after an import lands.
func (f *fixture) fillPass() int {
	f.t.Helper()
	filled, err := f.service().FillPending(f.ctx)
	if err != nil {
		f.t.Fatalf("fill pass: %v", err)
	}
	return filled
}

func (f *fixture) view(user paper.UUID) paper.View {
	f.t.Helper()
	view, err := f.service().View(f.ctx, user.String())
	if err != nil {
		f.t.Fatalf("read the account: %v", err)
	}
	return view
}

func (f *fixture) only(view paper.View) paper.Order {
	f.t.Helper()
	if len(view.Orders) != 1 {
		f.t.Fatalf("%d orders, want one", len(view.Orders))
	}
	return view.Orders[0]
}

func (f *fixture) count(sql string, args ...any) int64 {
	f.t.Helper()
	var total int64
	if err := f.pool.QueryRow(f.ctx, sql, args...).Scan(&total); err != nil {
		f.t.Fatalf("count: %v\n%s", err, sql)
	}
	return total
}
