package db_test

import (
	"context"
	"fmt"
	"testing"

	"market-lens/server/internal/db"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPersonalPortfolioMigration covers the constraints that carry this feature's safety.
//
// This is the product's first user-owned domain record, and the thing most worth putting in the
// schema is the one mistake that is a breach rather than a bug: a trade whose owner does not match
// the portfolio it belongs to. Every ownership predicate in the service reads the trade's own
// user_id, so if that column could ever disagree with the portfolio's, a correctly written query
// would return somebody else's data.
func TestPersonalPortfolioMigration(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, table := range []string{"portfolios", "portfolio_trades"} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("table %s does not exist", table)
		}
		var nullable string
		if err := pool.QueryRow(ctx, `SELECT is_nullable FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = $1 AND column_name = 'user_id'`,
			table).Scan(&nullable); err != nil {
			t.Fatalf("%s has no user_id: %v", table, err)
		}
		if nullable != "NO" {
			t.Errorf("%s.user_id is nullable; a record with no owner is a record anybody can read", table)
		}
	}

	alice, bob := seedPortfolioUsers(t, ctx, pool)
	var instrumentID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM instruments LIMIT 1`).Scan(&instrumentID); err != nil {
		t.Fatalf("find a seeded instrument: %v", err)
	}

	alicePortfolio := "77000000-0022-4000-8000-000000000001"
	mustExec(t, ctx, pool, `INSERT INTO portfolios (id, user_id, accounting_currency)
		VALUES ($1, $2, 'SEK')`, alicePortfolio, alice)

	// One portfolio per person, for now. A second would make every figure ambiguous about which
	// one it belongs to, and nothing in this milestone needs it.
	if _, err := pool.Exec(ctx, `INSERT INTO portfolios (id, user_id, accounting_currency)
		VALUES ('77000000-0022-4000-8000-0000000000ff', $1, 'EUR')`, alice); err == nil {
		t.Errorf("a second portfolio was accepted for the same person")
	}

	// The constraint this table exists to enforce: a trade cannot claim an owner its portfolio
	// does not have.
	if _, err := pool.Exec(ctx, `INSERT INTO portfolio_trades
		(id, portfolio_id, user_id, instrument_id, direction, quantity, price, costs, trade_date, sequence)
		VALUES ('77000000-0022-4000-8000-0000000000b1', $1, $2, $3, 'buy', 100, 245.80, 39, '2026-03-16', 1)`,
		alicePortfolio, bob, instrumentID); err == nil {
		t.Errorf("a trade was accepted whose owner is not the owner of its portfolio")
	}

	mustExec(t, ctx, pool, `INSERT INTO portfolio_trades
		(id, portfolio_id, user_id, instrument_id, direction, quantity, price, costs, trade_date, sequence)
		VALUES ('77000000-0022-4000-8000-0000000000b2', $1, $2, $3, 'buy', 100, 245.80, 39, '2026-03-16', 1)`,
		alicePortfolio, alice, instrumentID)

	// A holding nobody has yet is not a holding.
	if _, err := pool.Exec(ctx, `INSERT INTO portfolio_trades
		(id, portfolio_id, user_id, instrument_id, direction, quantity, price, costs, trade_date, sequence)
		VALUES ('77000000-0022-4000-8000-0000000000b3', $1, $2, $3, 'buy', 10, 100, 0, current_date + 1, 2)`,
		alicePortfolio, alice, instrumentID); err == nil {
		t.Errorf("a trade dated in the future was accepted")
	}

	// The entry order is what first-in-first-out consumes by, so it has to be unambiguous.
	if _, err := pool.Exec(ctx, `INSERT INTO portfolio_trades
		(id, portfolio_id, user_id, instrument_id, direction, quantity, price, costs, trade_date, sequence)
		VALUES ('77000000-0022-4000-8000-0000000000b4', $1, $2, $3, 'buy', 10, 100, 0, '2026-03-16', 1)`,
		alicePortfolio, alice, instrumentID); err == nil {
		t.Errorf("two trades were accepted with the same sequence in one portfolio")
	}

	// Quantities and prices are positive. There is no shorting here, so a negative quantity is a
	// data-entry error rather than a position.
	for _, bad := range []string{"quantity = -1", "price = 0"} {
		if _, err := pool.Exec(ctx, `UPDATE portfolio_trades SET `+bad+
			` WHERE id = '77000000-0022-4000-8000-0000000000b2'`); err == nil {
			t.Errorf("a trade with %s was accepted", bad)
		}
	}

	// Superseded or withdrawn, never both: a reader could not say which fact the row was.
	if _, err := pool.Exec(ctx, `UPDATE portfolio_trades
		SET superseded_by = '77000000-0022-4000-8000-0000000000b9', withdrawn_at = now()
		WHERE id = '77000000-0022-4000-8000-0000000000b2'`); err == nil {
		t.Errorf("a trade was accepted as both superseded and withdrawn")
	}
}

// seedPortfolioUsers returns two people, so every isolation constraint has somebody to be
// isolated from.
func seedPortfolioUsers(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (string, string) {
	t.Helper()
	ids := []string{
		"70000000-0022-4000-8000-000000000001",
		"70000000-0022-4000-8000-000000000002",
	}
	for index, id := range ids {
		email := fmt.Sprintf("portfolio%d@example.com", index+1)
		mustExec(t, ctx, pool, `INSERT INTO users
			(id, email, normalized_email, display_name, role, status, email_verified_at, created_at, updated_at)
			VALUES ($1, $2, $2, $2, $3, 'active', now(), now(), now())`,
			id, email, map[int]string{0: "owner", 1: "member"}[index])
	}
	return ids[0], ids[1]
}
