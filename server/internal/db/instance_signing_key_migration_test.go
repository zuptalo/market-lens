package db

import (
	"context"
	"testing"

	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestInstanceSigningKeyMigrationIsEmbedded(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	// Found by version rather than by index and total. What this test is about is that the
	// signing-key migration is embedded at version 11; asserting how many migrations exist
	// altogether made every later feature break it for no reason. TestLoadMigrations owns
	// the full ordered list.
	var signingKey migration
	for _, candidate := range migrations {
		if candidate.version == 11 {
			signingKey = candidate
			break
		}
	}
	if signingKey.name != "0011_instance_signing_key.sql" {
		t.Fatalf("migration 11 is %q, want %q", signingKey.name, "0011_instance_signing_key.sql")
	}
}

func TestInstanceSigningKeyMigrationCleanInstall(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	assertInstanceSigningKeySchema(t, ctx, pool)
}

func TestInstanceSigningKeyMigrationUpgradesVersionTen(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	applyMigrationsThrough(t, ctx, pool, 10)
	seedRevokedSessionAtVersionTen(t, ctx, pool)

	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	assertInstanceSigningKeySchema(t, ctx, pool)

	// The upgrade must not invalidate sessions that a pre-0011 database already revoked.
	var preserved int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM sessions WHERE revoked_reason = 'owner_password_reset'`).Scan(&preserved); err != nil {
		t.Fatal(err)
	}
	if preserved != 1 {
		t.Fatalf("sessions revoked before the upgrade = %d, want 1", preserved)
	}
}

func assertInstanceSigningKeySchema(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	var exists bool
	if err := pool.QueryRow(ctx,
		`SELECT to_regclass(current_schema() || '.instance_signing_key') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("instance_signing_key table is absent")
	}

	columns := map[string]string{}
	rows, err := pool.Query(ctx, `
		SELECT column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'instance_signing_key'
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
		"id":           "uuid",
		"source":       "text",
		"key_material": "bytea",
		"fingerprint":  "bytea",
		"generation":   "integer",
		"created_at":   "timestamp with time zone",
		"rotated_at":   "timestamp with time zone",
	} {
		if got := columns[name]; got != dataType {
			t.Errorf("column %s type = %q, want %q", name, got, dataType)
		}
	}

	// The table records the boundary at the point where it would be violated: the signing
	// key protects rows in this database, so it may live here; EXTERNAL_CREDENTIAL_KEY
	// encrypts secrets against this database being read, so it may not.
	var comment string
	if err := pool.QueryRow(ctx,
		`SELECT coalesce(obj_description(to_regclass(current_schema() || '.instance_signing_key'), 'pg_class'), '')`,
	).Scan(&comment); err != nil {
		t.Fatal(err)
	}
	if comment == "" {
		t.Error("instance_signing_key has no table comment recording the key boundary")
	}

	assertConstraintRejects(t, ctx, pool, `
		INSERT INTO instance_signing_key (id, source, key_material, fingerprint, generation, created_at)
		VALUES ('00000000-0000-4000-8000-000000000101', 'imported', decode(repeat('aa', 48), 'hex'),
		        decode(repeat('bb', 32), 'hex'), 1, now())
	`, "unknown signing key source")
	assertConstraintRejects(t, ctx, pool, `
		INSERT INTO instance_signing_key (id, source, key_material, fingerprint, generation, created_at)
		VALUES ('00000000-0000-4000-8000-000000000102', 'provisioned', NULL,
		        decode(repeat('bb', 32), 'hex'), 1, now())
	`, "provisioned row without key material")
	assertConstraintRejects(t, ctx, pool, `
		INSERT INTO instance_signing_key (id, source, key_material, fingerprint, generation, created_at)
		VALUES ('00000000-0000-4000-8000-000000000103', 'supplied', decode(repeat('aa', 48), 'hex'),
		        decode(repeat('bb', 32), 'hex'), 1, now())
	`, "supplied row carrying key material")
	assertConstraintRejects(t, ctx, pool, `
		INSERT INTO instance_signing_key (id, source, key_material, fingerprint, generation, created_at)
		VALUES ('00000000-0000-4000-8000-000000000104', 'provisioned', decode(repeat('aa', 48), 'hex'),
		        decode(repeat('bb', 16), 'hex'), 1, now())
	`, "fingerprint that is not 32 bytes")
	assertConstraintRejects(t, ctx, pool, `
		INSERT INTO instance_signing_key (id, source, key_material, fingerprint, generation, created_at)
		VALUES ('00000000-0000-4000-8000-000000000105', 'provisioned', decode(repeat('aa', 16), 'hex'),
		        decode(repeat('bb', 32), 'hex'), 1, now())
	`, "key material shorter than 32 bytes")
	assertConstraintRejects(t, ctx, pool, `
		INSERT INTO instance_signing_key (id, source, key_material, fingerprint, generation, created_at)
		VALUES ('00000000-0000-4000-8000-000000000106', 'provisioned', decode(repeat('aa', 48), 'hex'),
		        decode(repeat('bb', 32), 'hex'), 0, now())
	`, "non-positive generation")
	assertConstraintRejects(t, ctx, pool, `
		INSERT INTO instance_signing_key (id, source, key_material, fingerprint, generation, created_at, rotated_at)
		VALUES ('00000000-0000-4000-8000-000000000107', 'provisioned', decode(repeat('aa', 48), 'hex'),
		        decode(repeat('bb', 32), 'hex'), 2, now(), now() - interval '1 hour')
	`, "rotation recorded before creation")

	// Exactly one key, forever. This invariant is what makes concurrent first starts
	// converge without an advisory lock.
	if _, err := pool.Exec(ctx, `
		INSERT INTO instance_signing_key (id, source, key_material, fingerprint, generation, created_at)
		VALUES ('00000000-0000-4000-8000-000000000108', 'provisioned', decode(repeat('aa', 48), 'hex'),
		        decode(repeat('bb', 32), 'hex'), 1, now())
	`); err != nil {
		t.Fatalf("first signing key insert failed: %v", err)
	}
	assertConstraintRejects(t, ctx, pool, `
		INSERT INTO instance_signing_key (id, source, key_material, fingerprint, generation, created_at)
		VALUES ('00000000-0000-4000-8000-000000000109', 'provisioned', decode(repeat('cc', 48), 'hex'),
		        decode(repeat('dd', 32), 'hex'), 1, now())
	`, "second signing key row")
	if _, err := pool.Exec(ctx, `DELETE FROM instance_signing_key`); err != nil {
		t.Fatal(err)
	}

	assertSessionRevokeReasons(t, ctx, pool)
}

// assertSessionRevokeReasons proves rotation can record why it ended every session, and that
// every reason 0009 allowed still works. A migration that widened the set by replacing it
// with a narrower one would silently break existing revocation paths.
func assertSessionRevokeReasons(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	var definition string
	if err := pool.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conrelid = to_regclass(current_schema() || '.sessions')
		  AND contype = 'c'
		  AND pg_get_constraintdef(oid) LIKE '%revoked_reason%'
		  AND pg_get_constraintdef(oid) LIKE '%logout%'
	`).Scan(&definition); err != nil {
		t.Fatalf("locate sessions revoked_reason constraint: %v", err)
	}
	for _, reason := range []string{
		"logout", "owner_password_reset", "user_deactivated", "user_requested",
		"all_devices", "credential_changed", "administrative", "signing_key_rotated",
	} {
		if !contains(definition, reason) {
			t.Errorf("sessions revoked_reason constraint does not permit %q: %s", reason, definition)
		}
	}

	var stale int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_constraint
		WHERE conrelid = to_regclass(current_schema() || '.sessions')
		  AND contype = 'c'
		  AND conname = 'sessions_revoked_reason_check_v2'
	`).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if stale != 0 {
		t.Error("the superseded sessions_revoked_reason_check_v2 constraint is still present")
	}
}

func contains(haystack, needle string) bool {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}

func seedRevokedSessionAtVersionTen(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO users
		(id, email, normalized_email, display_name, role, status, email_verified_at, created_at, updated_at)
		VALUES (
			'00000000-0000-4000-8000-000000000020', 'owner2@example.test',
			'owner2@example.test', 'Owner', 'owner', 'active',
			'2026-08-30T10:00:00Z', '2026-08-30T10:00:00Z', '2026-08-30T10:00:00Z'
		);
		INSERT INTO sessions
		(id, user_id, token_digest, csrf_digest, created_at, last_seen_at,
		 idle_expires_at, absolute_expires_at, revoked_at, revoked_reason, device_label, origin_digest)
		VALUES (
			'00000000-0000-4000-8000-000000000021', '00000000-0000-4000-8000-000000000020',
			decode(repeat('21', 32), 'hex'), decode(repeat('22', 32), 'hex'),
			'2026-08-30T10:00:00Z', '2026-08-30T10:00:00Z',
			'2026-08-30T18:00:00Z', '2026-09-29T10:00:00Z',
			'2026-08-30T11:00:00Z', 'owner_password_reset', 'seed device', decode(repeat('23', 32), 'hex')
		)
	`); err != nil {
		t.Fatal(err)
	}
}
