package instruments_test

import (
	"context"
	"testing"

	"market-lens/server/internal/db"
	"market-lens/server/internal/instruments"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigrationsPreserveSameTickerAcrossExchanges(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (
		version int PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now()
	); INSERT INTO schema_migrations (version) VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	stockholmID := exchangeID(t, ctx, pool, "XSTO")
	copenhagenID := exchangeID(t, ctx, pool, "XCSE")

	stockholmInstrument := mustUUID(t)
	copenhagenInstrument := mustUUID(t)
	insertInstrument(t, ctx, pool, stockholmInstrument, stockholmID, "SE0000000001", "SAME", "Swedish Listing", "SEK", "SE")
	insertInstrument(t, ctx, pool, copenhagenInstrument, copenhagenID, "DK0000000001", "SAME", "Danish Listing", "DKK", "DK")

	rows, err := pool.Query(ctx, `SELECT id::text, exchange_id::text FROM instruments WHERE ticker = 'SAME' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := map[string]string{}
	for rows.Next() {
		var id, exchangeID string
		if err := rows.Scan(&id, &exchangeID); err != nil {
			t.Fatal(err)
		}
		found[id] = exchangeID
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 || found[stockholmInstrument.String()] != stockholmID.String() || found[copenhagenInstrument.String()] != copenhagenID.String() {
		t.Fatalf("exchange-qualified identities were not preserved: %#v", found)
	}
}

func TestInstrumentIdentityAndForeignKeyConstraints(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	exchangeID := exchangeID(t, ctx, pool, "XSTO")
	insertInstrument(t, ctx, pool, mustUUID(t), exchangeID, "SE0000000001", "AAA", "First", "SEK", "SE")

	if _, err := pool.Exec(ctx, `INSERT INTO instruments
		(id, exchange_id, isin, ticker, name, currency, country, instrument_type, active, purchasability_status)
		VALUES ($1, $2, 'SE0000000002', 'AAA', 'Duplicate', 'SEK', 'SE', 'common_stock', true, 'unverified')`,
		mustUUID(t).String(), exchangeID.String()); err == nil {
		t.Fatal("duplicate ticker on one exchange was accepted")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO instruments
		(id, exchange_id, isin, ticker, name, currency, country, instrument_type, active, purchasability_status)
		VALUES ($1, $2, 'SE0000000003', 'BBB', 'Orphan', 'SEK', 'SE', 'common_stock', true, 'unverified')`,
		mustUUID(t).String(), mustUUID(t).String()); err == nil {
		t.Fatal("instrument with unknown exchange was accepted")
	}
}

func mustUUID(t *testing.T) instruments.UUID {
	t.Helper()
	id, err := instruments.NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func exchangeID(t *testing.T, ctx context.Context, pool *pgxpool.Pool, mic string) instruments.UUID {
	t.Helper()
	var value string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM exchanges WHERE mic = $1`, mic).Scan(&value); err != nil {
		t.Fatal(err)
	}
	id, err := instruments.ParseUUID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func insertInstrument(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id, exchangeID instruments.UUID, isin, ticker, name, currency, country string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO instruments
		(id, exchange_id, isin, ticker, name, currency, country, instrument_type, active, purchasability_status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'common_stock', true, 'unverified')`,
		id.String(), exchangeID.String(), isin, ticker, name, currency, country); err != nil {
		t.Fatal(err)
	}
}
