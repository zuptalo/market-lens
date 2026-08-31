package db_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"market-lens/server/internal/db"
	"market-lens/server/internal/instruments"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIdentityAccessMigrationCleanInstall(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{
		"users", "bootstrap_state", "auth_capabilities", "owner_credentials", "sessions",
		"invitations", "member_login_challenges", "member_login_state", "login_failure_events",
		"auth_rate_events", "account_email_deliveries", "security_audit_events",
	} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("identity access table %s is absent", table)
		}
	}

	var bootstrapRows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM bootstrap_state WHERE singleton`).Scan(&bootstrapRows); err != nil {
		t.Fatal(err)
	}
	if bootstrapRows != 1 {
		t.Fatalf("bootstrap singleton rows = %d, want 1", bootstrapRows)
	}
}

func TestIdentityAccessMigrationUpgradesFeature002Baseline(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (
		version int PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now()
	); INSERT INTO schema_migrations(version) VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	var versions int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations WHERE version BETWEEN 1 AND 7`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 7 {
		t.Fatalf("applied migration versions = %d, want 7", versions)
	}
	var ownerIndexDefinition string
	if err := pool.QueryRow(ctx, `SELECT pg_get_indexdef(to_regclass(current_schema() || '.users_one_owner_idx'))`).Scan(&ownerIndexDefinition); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ownerIndexDefinition, "UNIQUE INDEX") || !strings.Contains(ownerIndexDefinition, "WHERE (role = 'owner'") {
		t.Fatalf("one-owner index definition = %q", ownerIndexDefinition)
	}
}

func TestIdentityAccessMigrationAllowsExactlyOneOfOneHundredConcurrentOwnerBootstraps(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	const contenders = 100
	var successes atomic.Int32
	errorsFound := make(chan error, contenders)
	var wait sync.WaitGroup
	ready := make(chan struct{})
	for index := range contenders {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-ready
			created, err := tryBootstrapOwner(ctx, pool, index)
			if err != nil {
				errorsFound <- err
				return
			}
			if created {
				successes.Add(1)
			}
		}()
	}
	close(ready)
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Error(err)
	}
	if got := successes.Load(); got != 1 {
		t.Fatalf("successful owner bootstraps = %d, want 1", got)
	}
	var owners int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE role='owner'`).Scan(&owners); err != nil {
		t.Fatal(err)
	}
	if owners != 1 {
		t.Fatalf("persisted owners = %d, want 1", owners)
	}
}

func tryBootstrapOwner(ctx context.Context, pool *pgxpool.Pool, contender int) (bool, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	var closedAt *time.Time
	if err := tx.QueryRow(ctx, `SELECT closed_at FROM bootstrap_state WHERE singleton FOR UPDATE`).Scan(&closedAt); err != nil {
		return false, err
	}
	if closedAt != nil {
		return false, nil
	}
	userID, err := instruments.NewUUID()
	if err != nil {
		return false, err
	}
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	email := fmt.Sprintf("owner-%03d@example.test", contender)
	if _, err := tx.Exec(ctx, `INSERT INTO users
		(id,email,normalized_email,display_name,role,status,email_verified_at,created_at,updated_at)
		VALUES ($1,$2,$2,$3,'owner','active',$4,$4,$4)`, userID.String(), email, fmt.Sprintf("Owner %03d", contender), now); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE bootstrap_state SET closed_at=$1, owner_user_id=$2 WHERE singleton`, now, userID.String()); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}
