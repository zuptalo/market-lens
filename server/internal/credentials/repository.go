package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Executor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type StoredCredential struct {
	Record      Record
	ValidatedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Status struct {
	Kind        Kind
	Configured  bool
	Ready       bool
	ValidatedAt *time.Time
	KeyVersion  uint32
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (repository *Repository) ValidateConfiguration(ctx context.Context, cipher *Cipher) error {
	if repository == nil || repository.pool == nil || cipher == nil {
		return errors.New("external credential configuration is unavailable")
	}
	rows, err := repository.pool.Query(ctx, `SELECT id::text,kind,ciphertext,payload_version,key_version
		FROM external_service_credentials ORDER BY kind`)
	if err != nil {
		return errors.New("validate external credential configuration")
	}
	defer rows.Close()
	records := make([]Record, 0, 2)
	for rows.Next() {
		var record Record
		if err := rows.Scan(&record.Metadata.ID, &record.Metadata.Kind, &record.Ciphertext,
			&record.Metadata.PayloadVersion, &record.Metadata.KeyVersion); err != nil {
			return errors.New("validate external credential configuration")
		}
		records = append(records, record)
	}
	if rows.Err() != nil {
		return errors.New("validate external credential configuration")
	}
	if len(records) == 0 {
		return nil
	}
	if len(records) != 2 || records[0].Metadata.Kind != KindEODHDAPI || records[1].Metadata.Kind != KindSMTP {
		return errors.New("external credential configuration is incomplete")
	}
	for _, record := range records {
		plaintext, err := cipher.Open(record.Metadata, record.Ciphertext)
		clear(plaintext)
		if err != nil {
			return errors.New("external credential configuration does not match stored credentials")
		}
	}
	return nil
}

func Insert(ctx context.Context, executor Executor, credential StoredCredential) error {
	if executor == nil {
		return errors.New("external credential persistence is unavailable")
	}
	if err := validateStoredCredential(credential); err != nil {
		return err
	}
	_, err := executor.Exec(ctx, `INSERT INTO external_service_credentials
		(id, kind, ciphertext, payload_version, key_version, validated_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		credential.Record.Metadata.ID, credential.Record.Metadata.Kind, credential.Record.Ciphertext,
		credential.Record.Metadata.PayloadVersion, credential.Record.Metadata.KeyVersion,
		credential.ValidatedAt, credential.CreatedAt.UTC(), credential.UpdatedAt.UTC())
	if err != nil {
		return errors.New("persist external credential")
	}
	return nil
}

func (repository *Repository) Statuses(ctx context.Context) ([]Status, error) {
	if repository == nil || repository.pool == nil {
		return nil, errors.New("external credential repository is unavailable")
	}
	rows, err := repository.pool.Query(ctx, `SELECT kind, validated_at, key_version
		FROM external_service_credentials ORDER BY kind`)
	if err != nil {
		return nil, errors.New("load external credential status")
	}
	defer rows.Close()
	statuses := make([]Status, 0, 2)
	for rows.Next() {
		var status Status
		if err := rows.Scan(&status.Kind, &status.ValidatedAt, &status.KeyVersion); err != nil {
			return nil, errors.New("scan external credential status")
		}
		status.Configured = true
		status.Ready = status.Kind == KindSMTP || status.ValidatedAt != nil
		statuses = append(statuses, status)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("load external credential status")
	}
	return statuses, nil
}

func (repository *Repository) Rotate(ctx context.Context, oldCipher, newCipher *Cipher, now time.Time) error {
	if repository == nil || repository.pool == nil || oldCipher == nil || newCipher == nil || now.IsZero() {
		return errors.New("external credential rotation input is invalid")
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return errors.New("begin external credential rotation")
	}
	defer tx.Rollback(ctx)

	records, err := lockedRecords(ctx, tx)
	if err != nil {
		return err
	}
	if len(records) != 2 {
		return errors.New("external credential set is incomplete")
	}
	rotated, err := ReencryptBatch(records, oldCipher, newCipher)
	if err != nil {
		return err
	}
	for _, record := range rotated {
		result, err := tx.Exec(ctx, `UPDATE external_service_credentials
			SET ciphertext=$1, key_version=$2, updated_at=$3
			WHERE id=$4 AND key_version=$5`, record.Ciphertext, record.Metadata.KeyVersion,
			now.UTC(), record.Metadata.ID, recordsKeyVersion(records, record.Metadata.ID))
		if err != nil || result.RowsAffected() != 1 {
			return errors.New("rotate external credential")
		}
	}
	var ownerID string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM users WHERE role='owner'`).Scan(&ownerID); err != nil {
		return errors.New("load owner for external credential rotation")
	}
	metadata, _ := json.Marshal(map[string]any{"key_version": rotated[0].Metadata.KeyVersion})
	if _, err := tx.Exec(ctx, `INSERT INTO security_audit_events
		(occurred_at,event_type,actor_user_id,subject_user_id,outcome,metadata)
		VALUES ($1,'credential.key_rotated.v1',$2,$2,'succeeded',$3)`, now.UTC(), ownerID, metadata); err != nil {
		return errors.New("audit external credential rotation")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO client_events
		(event_type,version,scope,entity_type,entity_id,payload,occurred_at)
		VALUES ('credential.key_rotated.v1',1,'owner','credential','external-services',$1,$2)`, metadata, now.UTC()); err != nil {
		return errors.New("publish external credential rotation")
	}
	if err := tx.Commit(ctx); err != nil {
		return errors.New("commit external credential rotation")
	}
	return nil
}

func lockedRecords(ctx context.Context, tx pgx.Tx) ([]Record, error) {
	rows, err := tx.Query(ctx, `SELECT id::text, kind, ciphertext, payload_version, key_version
		FROM external_service_credentials ORDER BY kind FOR UPDATE`)
	if err != nil {
		return nil, errors.New("lock external credentials")
	}
	defer rows.Close()
	records := make([]Record, 0, 2)
	for rows.Next() {
		var record Record
		if err := rows.Scan(&record.Metadata.ID, &record.Metadata.Kind, &record.Ciphertext,
			&record.Metadata.PayloadVersion, &record.Metadata.KeyVersion); err != nil {
			return nil, errors.New("scan external credential")
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("load external credentials")
	}
	return records, nil
}

func recordsKeyVersion(records []Record, id string) uint32 {
	for _, record := range records {
		if record.Metadata.ID == id {
			return record.Metadata.KeyVersion
		}
	}
	return 0
}

func validateStoredCredential(credential StoredCredential) error {
	metadata := credential.Record.Metadata
	if !validUUID(metadata.ID) || (metadata.Kind != KindEODHDAPI && metadata.Kind != KindSMTP) ||
		metadata.PayloadVersion == 0 || metadata.KeyVersion == 0 ||
		len(credential.Record.Ciphertext) < 29 || len(credential.Record.Ciphertext) > maxPlaintextBytes+28 ||
		credential.CreatedAt.IsZero() || credential.UpdatedAt.Before(credential.CreatedAt) ||
		(metadata.Kind == KindEODHDAPI) != (credential.ValidatedAt != nil) {
		return fmt.Errorf("external credential record is invalid")
	}
	return nil
}

// SMTPSettings is the decrypted transactional-email configuration held for this deployment.
type SMTPSettings struct {
	Host     string
	Port     int
	From     string
	Username string
	Password string
}

// SMTPSettings decrypts the stored transactional-email configuration. It fails closed so a
// wrong or rotated key never yields partial configuration that would silently misdeliver mail.
func (repository *Repository) SMTPSettings(ctx context.Context, cipher *Cipher) (SMTPSettings, error) {
	if cipher == nil {
		return SMTPSettings{}, errors.New("SMTP settings require a credential cipher")
	}
	var record Record
	err := repository.pool.QueryRow(ctx, `SELECT id::text,kind,ciphertext,payload_version,key_version
		FROM external_service_credentials WHERE kind=$1`, string(KindSMTP)).Scan(
		&record.Metadata.ID, &record.Metadata.Kind, &record.Ciphertext,
		&record.Metadata.PayloadVersion, &record.Metadata.KeyVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return SMTPSettings{}, errors.New("SMTP configuration is not available")
	}
	if err != nil {
		return SMTPSettings{}, err
	}
	plaintext, err := cipher.Open(record.Metadata, record.Ciphertext)
	if err != nil {
		return SMTPSettings{}, errors.New("SMTP configuration could not be decrypted")
	}
	var payload struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		From     string `json:"from"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return SMTPSettings{}, errors.New("SMTP configuration is malformed")
	}
	if payload.Host == "" || payload.Port < 1 || payload.Port > 65535 || payload.From == "" {
		return SMTPSettings{}, errors.New("SMTP configuration is incomplete")
	}
	return SMTPSettings{
		Host: payload.Host, Port: payload.Port, From: payload.From,
		Username: payload.Username, Password: payload.Password,
	}, nil
}
