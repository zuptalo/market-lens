package db_test

import (
	"context"
	"testing"
	"time"

	"market-lens/server/internal/db"
	"market-lens/server/internal/testdb"
)

// The two columns feature 028 adds, and the shape they must have.
//
// Nullable is the whole design: every session that exists when this ships was created by a request
// that is long over, so there is no address to write and none may be invented. A null means "not
// recorded", the screen says so in words, and the next authenticated request fills it in.
func TestSessionOriginColumnsArriveNullableAndTyped(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, column := range []string{"created_ip", "last_seen_ip"} {
		var dataType, nullable string
		if err := pool.QueryRow(ctx, `SELECT data_type, is_nullable FROM information_schema.columns
			WHERE table_name = 'sessions' AND column_name = $1`, column).Scan(&dataType, &nullable); err != nil {
			t.Fatalf("sessions.%s is missing: %v", column, err)
		}
		// inet rather than text: the database canonicalises, so one device cannot appear as two,
		// and a value that is not an address cannot be stored at all.
		if dataType != "inet" {
			t.Errorf("sessions.%s is %s, expected inet", column, dataType)
		}
		if nullable != "YES" {
			t.Errorf("sessions.%s is NOT NULL, which would make every pre-existing session invalid", column)
		}
	}

	// The keyed digest feature 004 specified stays exactly as it was. This feature narrows that
	// decision to the session row; it does not remove the digest or relax its constraint.
	var digestNullable string
	if err := pool.QueryRow(ctx, `SELECT is_nullable FROM information_schema.columns
		WHERE table_name = 'sessions' AND column_name = 'origin_digest'`).Scan(&digestNullable); err != nil {
		t.Fatalf("origin_digest is missing: %v", err)
	}
	if digestNullable != "NO" {
		t.Errorf("origin_digest became nullable; feature 004's digest must survive untouched")
	}
}

// A session that predates the feature must remain valid, insertable, and authenticable with no
// address at all — which is what "no backfill" means in practice (FR-004).
func TestASessionWithNoAddressIsStillASession(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var userID string
	now := time.Now().UTC()
	if err := pool.QueryRow(ctx, `INSERT INTO users
		(id,email,normalized_email,display_name,role,status,created_at,updated_at,email_verified_at)
		VALUES (gen_random_uuid(),'origin@example.com','origin@example.com','Origin','owner','active',$1,$1,$1)
		RETURNING id::text`,
		now).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO sessions
		(id,user_id,token_digest,csrf_digest,created_at,last_seen_at,idle_expires_at,absolute_expires_at,
		 device_label,origin_digest)
		VALUES (gen_random_uuid(),$1,repeat('a',32)::bytea,repeat('b',32)::bytea,$2::timestamptz,$2::timestamptz,
		        $2::timestamptz + interval '8 hours',$2::timestamptz + interval '30 days','Chrome',
		        repeat('c',32)::bytea)`, userID, now); err != nil {
		t.Fatalf("a session with no address must still insert: %v", err)
	}

	var recorded int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sessions
		WHERE user_id=$1 AND created_ip IS NULL AND last_seen_ip IS NULL`, userID).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded != 1 {
		t.Fatalf("expected one session with no recorded address, found %d", recorded)
	}

	// An address that is not an address cannot be stored. This is the constraint the type provides
	// and the reason no check constraint is written by hand.
	if _, err := pool.Exec(ctx, `UPDATE sessions SET last_seen_ip='not-an-address' WHERE user_id=$1`, userID); err == nil {
		t.Fatalf("expected a non-address to be refused by the column type")
	}
}
