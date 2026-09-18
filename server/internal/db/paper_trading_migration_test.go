package db_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"market-lens/server/internal/db"
	"market-lens/server/internal/testdb"
)

// The columns paper trading must never grow.
//
// The first nine are feature 025's list, forbidden for feature 025's reason: a field that exists
// will eventually be filled in, and then sent. The last three are this feature's own — FR-025
// excludes short selling, leverage and margin, and an exclusion nobody can test is a comment.
var forbiddenPaperColumns = []string{
	"venue", "order_type", "time_in_force", "destination", "expires_at",
	"broker_reference", "external_id", "limit_price", "stop_price",
	"leverage", "margin", "short",
}

func TestPaperTradingArrivesOnACleanInstall(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)

	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, table := range []string{"paper_accounts", "paper_orders", "paper_fills"} {
		var present bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&present); err != nil {
			t.Fatal(err)
		}
		if !present {
			t.Fatalf("%s does not exist", table)
		}

		// Ownership is read from the row itself on every table, so a query scoped only by the
		// parent cannot widen access and a forgotten join cannot leak.
		var nullable string
		if err := pool.QueryRow(ctx, `SELECT is_nullable FROM information_schema.columns
			WHERE table_name = $1 AND column_name = 'user_id'`, table).Scan(&nullable); err != nil {
			t.Fatalf("%s has no user_id: %v", table, err)
		}
		if nullable != "NO" {
			t.Errorf("%s.user_id is nullable", table)
		}

		// Nothing is seeded. An account exists because somebody opened one.
		var rows int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if rows != 0 {
			t.Errorf("%s arrived with %d rows", table, rows)
		}
	}

	var forbidden int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns
		WHERE table_name IN ('paper_orders', 'paper_fills') AND column_name = ANY($1)`,
		forbiddenPaperColumns).Scan(&forbidden); err != nil {
		t.Fatal(err)
	}
	if forbidden != 0 {
		t.Errorf("%d columns exist that this feature exists to not have", forbidden)
	}
}

// TestAPaperAccountIsOnePerPersonAndCannotBeRetuned. The value of the record is that it cannot be
// changed once the result is known; both halves of that are enforced by the database.
func TestAPaperAccountIsOnePerPersonAndCannotBeRetuned(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	user := seedUserForPaper(t, pool)

	open := func(id string) error {
		_, err := pool.Exec(ctx, `INSERT INTO paper_accounts
			(id, user_id, starting_cash, accounting_currency,
			 brokerage_bps, brokerage_minimum, slippage_bps, currency_spread_bps)
			VALUES ($1, $2, 1000000, 'EUR', 10, 5, 5, 10)`, id, user)
		return err
	}
	if err := open("aaaaaaaa-0026-4000-8000-000000000001"); err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := open("aaaaaaaa-0026-4000-8000-000000000002"); err == nil {
		t.Errorf("a second account was opened for the same person")
	}

	for _, change := range []struct {
		what string
		sql  string
	}{
		{"the starting balance", `UPDATE paper_accounts SET starting_cash = 9000000`},
		{"the accounting currency", `UPDATE paper_accounts SET accounting_currency = 'SEK'`},
		{"the brokerage rate", `UPDATE paper_accounts SET brokerage_bps = 0`},
		{"the slippage rate", `UPDATE paper_accounts SET slippage_bps = 0`},
	} {
		if _, err := pool.Exec(ctx, change.sql); err == nil {
			t.Errorf("%s was changed after the account was opened", change.what)
		}
	}

	var cash string
	if err := pool.QueryRow(ctx, `SELECT starting_cash::text FROM paper_accounts`).Scan(&cash); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cash, "1000000") {
		t.Errorf("the starting balance reads %s", cash)
	}
}

// TestAFillCannotPredateTheOrderThatCausedIt is the constraint the whole feature rests on. An order
// placed in a session must fill at a *later* session, because that price did not exist when the
// order was placed. In the database rather than in a service: a lookahead bug produces a flattering
// result that looks entirely plausible.
func TestAFillCannotPredateTheOrderThatCausedIt(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	user := seedUserForPaper(t, pool)
	instrument := seedInstrumentForPaper(t, pool)

	const account = "aaaaaaaa-0026-4000-8000-000000000001"
	mustExec(t, ctx, pool, `INSERT INTO paper_accounts
		(id, user_id, starting_cash, accounting_currency,
		 brokerage_bps, brokerage_minimum, slippage_bps, currency_spread_bps)
		VALUES ($1, $2, 1000000, 'EUR', 10, 5, 5, 10)`, account, user)

	// An order exists only because an intent was promoted, so the foreign key needs a real one.
	const intent = "cccccccc-0026-4000-8000-000000000001"
	mustExec(t, ctx, pool, `INSERT INTO order_intents
		(id, user_id, instrument_id, direction, quantity, price, status, settled_at)
		VALUES ($1, $2, $3, 'buy', 100, 250, 'acted_on', now())`, intent, user, instrument)

	const order = "bbbbbbbb-0026-4000-8000-000000000001"
	mustExec(t, ctx, pool, `INSERT INTO paper_orders
		(id, account_id, user_id, intent_id, instrument_id, direction, quantity,
		 expected_price, placed_session, state)
		VALUES ($1, $2, $3, $4, $5, 'buy', 100, 250, DATE '2026-06-10', 'pending')`,
		order, account, user, intent, instrument)

	fill := func(session string) error {
		_, err := pool.Exec(ctx, `INSERT INTO paper_fills
			(id, order_id, user_id, fill_session, open_price, quantity, costs, cash_effect,
			 conversion_rate, bar_observed_at)
			VALUES (gen_random_uuid(), $1, $2, $3::date, 250, 100, 25, -25025, 1, now())`,
			order, user, session)
		return err
	}
	for _, session := range []string{"2026-06-09", "2026-06-10"} {
		if err := fill(session); err == nil {
			t.Errorf("an order placed on 2026-06-10 filled at %s", session)
		}
	}
	if err := fill("2026-06-11"); err != nil {
		t.Errorf("a fill at a later session was refused: %v", err)
	}

	// And an order fills once. There are no partial fills to reconcile.
	if err := fill("2026-06-12"); err == nil {
		t.Errorf("one order filled twice")
	}
}

// seedUserForPaper is one person, because every paper table is owned by one.
func seedUserForPaper(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	const id = "70000000-0026-4000-8000-000000000001"
	mustExec(t, context.Background(), pool, `INSERT INTO users
		(id, email, normalized_email, display_name, role, status, email_verified_at, created_at, updated_at)
		VALUES ($1, 'paper@example.com', 'paper@example.com', 'Paper', 'owner', 'active', now(), now(), now())`,
		id)
	return id
}

// seedInstrumentForPaper is one thing to place an order in, so the foreign keys have somewhere to
// point and the constraints under test are the only thing that can refuse a row.
func seedInstrumentForPaper(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()
	const exchange = "60000000-0026-4000-8000-000000000001"
	const instrument = "22000000-0026-4000-8000-000000000001"
	mustExec(t, ctx, pool, `INSERT INTO exchanges (id, mic, name, country, currency, timezone)
		VALUES ($1, 'XPAP', 'Paper Exchange', 'SE', 'SEK', 'Europe/Stockholm')`, exchange)
	mustExec(t, ctx, pool, `INSERT INTO instruments
		(id, exchange_id, isin, ticker, name, currency, country, instrument_type, active, purchasability_status)
		VALUES ($1, $2, 'SE0000000026', 'PAPR', 'Paper AB', 'SEK', 'SE', 'common_stock', true, 'unverified')`,
		instrument, exchange)
	return instrument
}
