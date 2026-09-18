package db_test

import (
	"context"
	"testing"

	"market-lens/server/internal/db"
	"market-lens/server/internal/testdb"
)

// TestOrderIntentsMigration covers what an intent may be — and, unusually, what it may not have.
//
// The absence is the safeguard. An intent that carried an order type or a venue would be one field
// away from being transmissible, and the next feature to touch it would have to decide not to send
// it. Naming those columns in a test means a later addition has to argue with a failing test rather
// than with a comment somebody may not read.
func TestOrderIntentsMigration(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('order_intents') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatalf("table order_intents does not exist")
	}

	var nullable string
	if err := pool.QueryRow(ctx, `SELECT is_nullable FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'order_intents'
		  AND column_name = 'user_id'`).Scan(&nullable); err != nil {
		t.Fatalf("order_intents has no user_id: %v", err)
	}
	if nullable != "NO" {
		t.Errorf("order_intents.user_id is nullable; an intent with no owner is one anybody could read")
	}

	// Nothing a broker could act on.
	for _, forbidden := range []string{
		"venue", "order_type", "time_in_force", "destination", "expires_at", "broker_reference",
		"external_id", "limit_price", "stop_price",
	} {
		var present bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = 'order_intents'
			  AND column_name = $1)`, forbidden).Scan(&present); err != nil {
			t.Fatal(err)
		}
		if present {
			t.Errorf("order_intents carries %q; an intent is not an order, and this column is how it stops being true", forbidden)
		}
	}

	// The product seeds nothing: every intent is written by a person.
	var seeded int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM order_intents`).Scan(&seeded); err != nil {
		t.Fatal(err)
	}
	if seeded != 0 {
		t.Errorf("the migration seeded %d intents", seeded)
	}

	alice, _ := seedPortfolioUsers(t, ctx, pool)
	var instrumentID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM instruments LIMIT 1`).Scan(&instrumentID); err != nil {
		t.Fatalf("find a seeded instrument: %v", err)
	}

	mustExec(t, ctx, pool, `INSERT INTO order_intents
		(id, user_id, instrument_id, direction, quantity, price, costs, status)
		VALUES ('55000000-0025-4000-8000-000000000001', $1, $2, 'buy', 40, 245.80, 39, 'considering')`,
		alice, instrumentID)

	// A settled intent carries when it settled; an unsettled one does not.
	if _, err := pool.Exec(ctx, `UPDATE order_intents SET status = 'withdrawn'
		WHERE id = '55000000-0025-4000-8000-000000000001'`); err == nil {
		t.Errorf("an intent was withdrawn without recording when")
	}
	if _, err := pool.Exec(ctx, `UPDATE order_intents SET settled_at = now()
		WHERE id = '55000000-0025-4000-8000-000000000001'`); err == nil {
		t.Errorf("an intent still under consideration was given a settled time")
	}
	mustExec(t, ctx, pool, `UPDATE order_intents SET status = 'acted_on', settled_at = now()
		WHERE id = '55000000-0025-4000-8000-000000000001'`)

	// A status outside the vocabulary is not a state anybody can read.
	if _, err := pool.Exec(ctx, `INSERT INTO order_intents
		(id, user_id, instrument_id, direction, quantity, price, status)
		VALUES (gen_random_uuid(), $1, $2, 'buy', 1, 1, 'submitted')`, alice, instrumentID); err == nil {
		t.Errorf("an intent was accepted with a status that implies transmission")
	}

	// Quantity and price are positive. A sale larger than the position is a consequence to report,
	// not a constraint to enforce — that distinction belongs in the service, not here.
	for _, bad := range []string{"quantity = 0", "price = -1"} {
		if _, err := pool.Exec(ctx, `UPDATE order_intents SET `+bad+
			` WHERE id = '55000000-0025-4000-8000-000000000001'`); err == nil {
			t.Errorf("an intent with %s was accepted", bad)
		}
	}
}
