package db

import (
	"context"
	"testing"

	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestExternalCredentialMigrationIsEmbedded(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 10 {
		t.Fatalf("embedded migration count = %d, want 10", len(migrations))
	}
	external := migrations[8]
	if external.version != 9 || external.name != "0009_external_credentials_and_owner_reset.sql" {
		t.Fatalf("external credential migration = %d %q, want 9 %q", external.version, external.name, "0009_external_credentials_and_owner_reset.sql")
	}
}

func TestExternalCredentialMigrationCleanInstall(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	assertExternalCredentialSchema(t, ctx, pool)
}

func TestExternalCredentialMigrationUpgradesVersionEight(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	applyMigrationsThrough(t, ctx, pool, 8)
	seedHistoricalOwnerRecoveryCapability(t, ctx, pool)

	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	assertExternalCredentialSchema(t, ctx, pool)
}

func assertExternalCredentialSchema(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.external_service_credentials') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("external_service_credentials table is absent")
	}

	columns := map[string]string{}
	rows, err := pool.Query(ctx, `
		SELECT column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'external_service_credentials'
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			t.Fatal(err)
		}
		columns[name] = dataType
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for name, dataType := range map[string]string{
		"id":              "uuid",
		"kind":            "text",
		"ciphertext":      "bytea",
		"payload_version": "smallint",
		"key_version":     "integer",
		"validated_at":    "timestamp with time zone",
		"created_at":      "timestamp with time zone",
		"updated_at":      "timestamp with time zone",
	} {
		if got := columns[name]; got != dataType {
			t.Errorf("column %s type = %q, want %q", name, got, dataType)
		}
	}

	assertConstraintRejects(t, ctx, pool, `
		INSERT INTO external_service_credentials
		(id, kind, ciphertext, payload_version, key_version, validated_at, created_at, updated_at)
		VALUES ('00000000-0000-4000-8000-000000000001', 'unknown', decode('00', 'hex'), 1, 1, now(), now(), now())
	`, "unknown credential kind")
	assertConstraintRejects(t, ctx, pool, `
		INSERT INTO external_service_credentials
		(id, kind, ciphertext, payload_version, key_version, validated_at, created_at, updated_at)
		VALUES ('00000000-0000-4000-8000-000000000002', 'eodhd_api', decode('00', 'hex'), 0, 1, now(), now(), now())
	`, "non-positive payload version")
	assertConstraintRejects(t, ctx, pool, `
		INSERT INTO external_service_credentials
		(id, kind, ciphertext, payload_version, key_version, validated_at, created_at, updated_at)
		VALUES ('00000000-0000-4000-8000-000000000003', 'smtp', decode(repeat('00', 29), 'hex'), 1, 0, NULL, now(), now())
	`, "non-positive key version")

	var recoveryMentions, setupOnlyMentions, usableRecovery int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_constraint
		WHERE conrelid = to_regclass(current_schema() || '.auth_capabilities')
		  AND contype = 'c'
		  AND pg_get_constraintdef(oid) LIKE '%owner_recovery%'
	`).Scan(&recoveryMentions); err != nil {
		t.Fatal(err)
	}
	if recoveryMentions != 0 {
		t.Fatalf("auth capability constraints still permit or describe owner_recovery: %d", recoveryMentions)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_constraint
		WHERE conrelid = to_regclass(current_schema() || '.auth_capabilities')
		  AND contype = 'c'
		  AND pg_get_constraintdef(oid) LIKE '%kind = ''owner_setup''%'
	`).Scan(&setupOnlyMentions); err != nil {
		t.Fatal(err)
	}
	if setupOnlyMentions == 0 {
		t.Fatal("auth capability constraints do not restrict kind to owner_setup")
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM auth_capabilities
		WHERE kind = 'owner_recovery' AND consumed_at IS NULL AND revoked_at IS NULL
	`).Scan(&usableRecovery); err != nil {
		t.Fatal(err)
	}
	if usableRecovery != 0 {
		t.Fatalf("usable historical owner recovery capabilities = %d, want 0", usableRecovery)
	}
}

func assertConstraintRejects(t *testing.T, ctx context.Context, pool *pgxpool.Pool, statement, description string) {
	t.Helper()
	if _, err := pool.Exec(ctx, statement); err == nil {
		t.Fatalf("%s insert succeeded", description)
	}
}

func applyMigrationsThrough(t *testing.T, ctx context.Context, pool *pgxpool.Pool, maximumVersion int) {
	t.Helper()
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (
		version int PRIMARY KEY,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		t.Fatal(err)
	}
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations {
		if migration.version > maximumVersion {
			break
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, migration.sql); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("apply migration %d: %v", migration.version, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ($1)`, migration.version); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

func seedHistoricalOwnerRecoveryCapability(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO users
		(id, email, normalized_email, display_name, role, status, email_verified_at, created_at, updated_at)
		VALUES (
			'00000000-0000-4000-8000-000000000010', 'owner@example.test',
			'owner@example.test', 'Owner', 'owner', 'active',
			'2026-08-30T10:00:00Z', '2026-08-30T10:00:00Z', '2026-08-30T10:00:00Z'
		);
		INSERT INTO auth_capabilities
		(id, kind, user_id, token_digest, expires_at, created_at)
		VALUES (
			'00000000-0000-4000-8000-000000000011', 'owner_recovery',
			'00000000-0000-4000-8000-000000000010', decode(repeat('11', 32), 'hex'),
			'2026-08-30T10:30:00Z', '2026-08-30T10:00:00Z'
		)
	`); err != nil {
		t.Fatal(err)
	}
}
