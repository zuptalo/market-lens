package credentials

import (
	"bytes"
	"context"
	"testing"
	"time"

	"market-lens/server/internal/db"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCredentialRepositoryPersistsSafeStatusAndRotatesAllRowsAtomically(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	seedCredentialOwner(t, ctx, pool)
	oldCipher, err := NewCipher(bytes.Repeat([]byte{0x41}, 32), 1)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	insertCredentialSet(t, ctx, pool, oldCipher, now)

	repository := NewRepository(pool)
	statuses, err := repository.Statuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 || statuses[0].Kind != KindEODHDAPI || !statuses[0].Configured ||
		!statuses[0].Ready || statuses[0].ValidatedAt == nil || statuses[0].KeyVersion != 1 ||
		statuses[1].Kind != KindSMTP || !statuses[1].Configured || !statuses[1].Ready ||
		statuses[1].ValidatedAt != nil || statuses[1].KeyVersion != 1 {
		t.Fatalf("safe statuses = %#v", statuses)
	}

	newCipher, err := NewCipher(bytes.Repeat([]byte{0x51}, 32), 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Rotate(ctx, oldCipher, newCipher, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, `SELECT id::text, kind, ciphertext, payload_version, key_version
		FROM external_service_credentials ORDER BY kind`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	rotated := 0
	for rows.Next() {
		var record Record
		if err := rows.Scan(&record.Metadata.ID, &record.Metadata.Kind, &record.Ciphertext,
			&record.Metadata.PayloadVersion, &record.Metadata.KeyVersion); err != nil {
			t.Fatal(err)
		}
		plaintext, err := newCipher.Open(record.Metadata, record.Ciphertext)
		if err != nil || len(plaintext) == 0 {
			t.Fatalf("rotated %s envelope did not authenticate: %v", record.Metadata.Kind, err)
		}
		rotated++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	var audits, events int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM security_audit_events WHERE event_type='credential.key_rotated.v1'),
		(SELECT count(*) FROM client_events WHERE event_type='credential.key_rotated.v1' AND scope='owner')
	`).Scan(&audits, &events); err != nil {
		t.Fatal(err)
	}
	if rotated != 2 || audits != 1 || events != 1 {
		t.Fatalf("rotation rows=%d audits=%d events=%d", rotated, audits, events)
	}
}

func TestCredentialRepositoryRotationFailureLeavesEveryRowUnchanged(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	seedCredentialOwner(t, ctx, pool)
	oldCipher, err := NewCipher(bytes.Repeat([]byte{0x61}, 32), 1)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	insertCredentialSet(t, ctx, pool, oldCipher, now)
	if _, err := pool.Exec(ctx, `UPDATE external_service_credentials
		SET ciphertext=set_byte(ciphertext, octet_length(ciphertext)-1,
			get_byte(ciphertext, octet_length(ciphertext)-1) # 255)
		WHERE kind='smtp'`); err != nil {
		t.Fatal(err)
	}
	var before []byte
	if err := pool.QueryRow(ctx, `SELECT ciphertext FROM external_service_credentials WHERE kind='eodhd_api'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	newCipher, err := NewCipher(bytes.Repeat([]byte{0x71}, 32), 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewRepository(pool).Rotate(ctx, oldCipher, newCipher, now.Add(time.Hour)); err == nil {
		t.Fatal("rotation with tampered row unexpectedly succeeded")
	}
	var unchanged []byte
	var versionOne int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT ciphertext FROM external_service_credentials WHERE kind='eodhd_api'),
		(SELECT count(*) FROM external_service_credentials WHERE key_version=1)
	`).Scan(&unchanged, &versionOne); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, unchanged) || versionOne != 2 {
		t.Fatalf("failed rotation changed state: unchanged=%v old_version_rows=%d", bytes.Equal(before, unchanged), versionOne)
	}
}

func TestCredentialRepositoryValidatesConfiguredKeyAndCompleteStoredSet(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	repository := NewRepository(pool)
	cipher, err := NewCipher(bytes.Repeat([]byte{0x31}, 32), 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ValidateConfiguration(ctx, cipher); err != nil {
		t.Fatalf("open setup should accept configured key: %v", err)
	}
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	insertCredentialSet(t, ctx, pool, cipher, now)
	if err := repository.ValidateConfiguration(ctx, cipher); err != nil {
		t.Fatalf("matching stored credentials rejected: %v", err)
	}
	wrongKey, _ := NewCipher(bytes.Repeat([]byte{0x32}, 32), 1)
	wrongVersion, _ := NewCipher(bytes.Repeat([]byte{0x31}, 32), 2)
	for name, candidate := range map[string]*Cipher{"wrong key": wrongKey, "wrong version": wrongVersion} {
		t.Run(name, func(t *testing.T) {
			if err := repository.ValidateConfiguration(ctx, candidate); err == nil {
				t.Fatal("invalid configured credential key unexpectedly passed")
			}
		})
	}
	if _, err := pool.Exec(ctx, `DELETE FROM external_service_credentials WHERE kind='smtp'`); err != nil {
		t.Fatal(err)
	}
	if err := repository.ValidateConfiguration(ctx, cipher); err == nil {
		t.Fatal("incomplete external credential set unexpectedly passed")
	}
}

func insertCredentialSet(t *testing.T, ctx context.Context, pool *pgxpool.Pool, envelope *Cipher, now time.Time) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	for _, item := range []struct {
		metadata  Metadata
		plaintext string
		validated *time.Time
	}{
		{metadata: Metadata{ID: "00000000-0000-4000-8000-000000000201", Kind: KindEODHDAPI, PayloadVersion: 1, KeyVersion: 1}, plaintext: `{"api_key":"secret"}`, validated: &now},
		{metadata: Metadata{ID: "00000000-0000-4000-8000-000000000202", Kind: KindSMTP, PayloadVersion: 1, KeyVersion: 1}, plaintext: `{"host":"smtp.example.test","port":587,"from":"access@example.test"}`},
	} {
		ciphertext, err := envelope.Seal(item.metadata, []byte(item.plaintext))
		if err != nil {
			t.Fatal(err)
		}
		if err := Insert(ctx, tx, StoredCredential{
			Record:      Record{Metadata: item.metadata, Ciphertext: ciphertext},
			ValidatedAt: item.validated, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func seedCredentialOwner(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO users
		(id,email,normalized_email,display_name,role,status,email_verified_at,created_at,updated_at)
		VALUES ('00000000-0000-4000-8000-000000000200','owner@example.test',
		'owner@example.test','Owner','owner','active',now(),now(),now())`); err != nil {
		t.Fatal(err)
	}
}

func TestCredentialRepositoryReturnsDecryptedSMTPSettingsForDelivery(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	seedCredentialOwner(t, ctx, pool)
	cipher, err := NewCipher(bytes.Repeat([]byte{0x61}, 32), 1)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	insertCredentialSet(t, ctx, pool, cipher, now)
	repository := NewRepository(pool)

	settings, err := repository.SMTPSettings(ctx, cipher)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Host != "smtp.example.test" || settings.Port != 587 || settings.From != "access@example.test" {
		t.Fatalf("SMTP settings = %#v", settings)
	}

	// A wrong key must fail closed rather than yield partial or empty configuration.
	wrongKey, err := NewCipher(bytes.Repeat([]byte{0x62}, 32), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SMTPSettings(ctx, wrongKey); err == nil {
		t.Fatal("SMTP settings decrypted under the wrong key")
	}
}

func TestCredentialRepositorySMTPSettingsAreUnavailableBeforeSetup(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	cipher, err := NewCipher(bytes.Repeat([]byte{0x63}, 32), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRepository(pool).SMTPSettings(ctx, cipher); err == nil {
		t.Fatal("SMTP settings were available before owner setup stored them")
	}
}
