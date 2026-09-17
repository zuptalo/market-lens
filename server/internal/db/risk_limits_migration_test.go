package db_test

import (
	"context"
	"testing"

	"market-lens/server/internal/db"
	"market-lens/server/internal/testdb"
)

// TestRiskLimitsMigration covers what a limit is allowed to be, and the one thing this table must
// never contain.
//
// It must never contain a limit nobody stated. A default threshold is the product telling somebody
// what is prudent, which is the line this whole feature is built on staying the right side of — so
// the migration seeds nothing, and that absence is asserted rather than assumed.
func TestRiskLimitsMigration(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('risk_limits') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatalf("table risk_limits does not exist")
	}

	var nullable string
	if err := pool.QueryRow(ctx, `SELECT is_nullable FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'risk_limits'
		  AND column_name = 'user_id'`).Scan(&nullable); err != nil {
		t.Fatalf("risk_limits has no user_id: %v", err)
	}
	if nullable != "NO" {
		t.Errorf("risk_limits.user_id is nullable; a rule with no owner is a rule anybody could read")
	}

	// The product publishes no defaults. A person with no limits has none.
	var seeded int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM risk_limits`).Scan(&seeded); err != nil {
		t.Fatal(err)
	}
	if seeded != 0 {
		t.Errorf("the migration seeded %d limits; a default threshold is advice", seeded)
	}

	alice, _ := seedPortfolioUsers(t, ctx, pool)

	mustExec(t, ctx, pool, `INSERT INTO risk_limits (id, user_id, kind, threshold)
		VALUES ('99000000-0023-4000-8000-000000000001', $1, 'instrument_share', 0.25)`, alice)

	// One limit of each kind. Two thresholds for the same thing is a contradiction, not a
	// refinement, and "which one applies" has no good answer.
	if _, err := pool.Exec(ctx, `INSERT INTO risk_limits (id, user_id, kind, threshold)
		VALUES ('99000000-0023-4000-8000-0000000000ff', $1, 'instrument_share', 0.40)`, alice); err == nil {
		t.Errorf("a second limit of the same kind was accepted")
	}

	// A share above 100% is not a rule, it is a typo — and so is a share of nothing.
	for _, bad := range []struct{ kind, threshold string }{
		{"sector_share", "1.5"},
		{"sector_share", "0"},
		{"market_share", "-0.1"},
		// A holding count is a whole number of things somebody owns.
		{"holding_count", "7.5"},
		{"holding_count", "0"},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO risk_limits (id, user_id, kind, threshold)
			VALUES (gen_random_uuid(), $1, $2, $3::numeric)`, alice, bad.kind, bad.threshold); err == nil {
			t.Errorf("a %s threshold of %s was accepted", bad.kind, bad.threshold)
		}
	}

	// And a kind the product cannot evaluate is not a limit it may store.
	if _, err := pool.Exec(ctx, `INSERT INTO risk_limits (id, user_id, kind, threshold)
		VALUES (gen_random_uuid(), $1, 'portfolio_drawdown', 0.2)`, alice); err == nil {
		t.Errorf("a limit kind the product cannot evaluate was accepted")
	}

	// A count limit of a sensible size is fine.
	mustExec(t, ctx, pool, `INSERT INTO risk_limits (id, user_id, kind, threshold)
		VALUES (gen_random_uuid(), $1, 'holding_count', 15)`, alice)
}
