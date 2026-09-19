package db_test

import (
	"context"
	"testing"
	"time"

	"market-lens/server/internal/db"
	"market-lens/server/internal/testdb"
)

// One row per session already told about, so a repeated pass says nothing twice (feature 029).
//
// The primary key is the whole mechanism: the pass claims the session with ON CONFLICT DO NOTHING
// inside its own transaction, and whichever pass inserts the row is the one that raises.
func TestASignalSessionCanOnlyBeToldAboutOnce(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM signal_change_sessions`).Scan(&rows); err != nil {
		t.Fatalf("the table does not exist: %v", err)
	}
	if rows != 0 {
		t.Fatalf("%d sessions arrived already told about", rows)
	}

	today := time.Now().UTC().Format("2006-01-02")
	claim := func() int64 {
		t.Helper()
		result, err := pool.Exec(ctx, `INSERT INTO signal_change_sessions (session_date, changes)
			VALUES ($1::date, 11) ON CONFLICT (session_date) DO NOTHING`, today)
		if err != nil {
			t.Fatal(err)
		}
		return result.RowsAffected()
	}
	if first := claim(); first != 1 {
		t.Fatalf("the first pass claimed %d rows, want 1", first)
	}
	if second := claim(); second != 0 {
		t.Fatalf("the second pass claimed %d rows, want 0 — it would tell somebody twice", second)
	}

	// A session with nothing to say is not recorded, so the count may never be zero.
	if _, err := pool.Exec(ctx, `INSERT INTO signal_change_sessions (session_date, changes)
		VALUES ($1::date - 1, 0)`, today); err == nil {
		t.Error("a session with no changes was recorded as told about")
	}
}
