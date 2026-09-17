package risk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	clientevents "market-lens/server/internal/events"
	"market-lens/server/internal/instruments"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository owns every owner-scoped query and the single transactional write a change performs.
//
// The rule feature 022 established holds here: the caller's user identifier is a required argument
// and appears in the WHERE clause of every statement. It is never inferred from a limit identifier,
// because a query scoped only by identifier would return the right row for the wrong person the
// first time one leaked.
type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) ready() error {
	if r == nil || r.pool == nil {
		return errors.New("risk repository is not configured")
	}
	return nil
}

// Limits reads one person's, in the order a screen shows them.
func (r *Repository) Limits(ctx context.Context, userID string) ([]Limit, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT id::text, user_id::text, kind, threshold::text,
		created_at, updated_at
		FROM risk_limits WHERE user_id = $1
		ORDER BY array_position(ARRAY['instrument_share','sector_share','market_share','holding_count'], kind)`,
		userID)
	if err != nil {
		return nil, fmt.Errorf("read the limits: %w", err)
	}
	defer rows.Close()
	var limits []Limit
	for rows.Next() {
		var limit Limit
		if err := rows.Scan((*string)(&limit.ID), (*string)(&limit.UserID), (*string)(&limit.Kind),
			&limit.Threshold, &limit.CreatedAt, &limit.UpdatedAt); err != nil {
			return nil, err
		}
		limits = append(limits, limit)
	}
	return limits, rows.Err()
}

// Upsert writes one limit, replacing the one of that kind. One of each kind per person, so from the
// interface's point of view stating a limit is idempotent.
func (r *Repository) Upsert(ctx context.Context, userID string, kind Kind, threshold string) (Limit, error) {
	if err := r.ready(); err != nil {
		return Limit{}, err
	}
	id, err := instruments.NewUUID()
	if err != nil {
		return Limit{}, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Limit{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var stored Limit
	if err := tx.QueryRow(ctx, `INSERT INTO risk_limits (id, user_id, kind, threshold)
		VALUES ($1, $2, $3, $4::numeric)
		ON CONFLICT (user_id, kind) DO UPDATE
		  SET threshold = excluded.threshold, updated_at = now()
		RETURNING id::text, user_id::text, kind, threshold::text, created_at, updated_at`,
		id.String(), userID, string(kind), threshold).Scan(
		(*string)(&stored.ID), (*string)(&stored.UserID), (*string)(&stored.Kind),
		&stored.Threshold, &stored.CreatedAt, &stored.UpdatedAt); err != nil {
		return Limit{}, fmt.Errorf("store the limit: %w", err)
	}
	if err := publish(ctx, tx, userID, kind, "stated"); err != nil {
		return Limit{}, err
	}
	return stored, tx.Commit(ctx)
}

// Delete removes one limit, scoped to its owner. A limit belonging to somebody else answers exactly
// as one that does not exist.
func (r *Repository) Delete(ctx context.Context, userID string, kind Kind) error {
	if err := r.ready(); err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `DELETE FROM risk_limits WHERE user_id = $1 AND kind = $2`,
		userID, string(kind))
	if err != nil {
		return fmt.Errorf("remove the limit: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := publish(ctx, tx, userID, kind, "removed"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// publish writes the owner-scoped event on the same transaction as the change.
//
// It carries the kind that changed and nothing else. An evaluation in the payload would be a second
// copy of a figure computed on read, and the two would disagree the moment a price moved.
func publish(ctx context.Context, tx pgx.Tx, userID string, kind Kind, change string) error {
	payload, err := json.Marshal(map[string]string{"kind": string(kind), "change": change})
	if err != nil {
		return fmt.Errorf("encode the change: %w", err)
	}
	return clientevents.Insert(ctx, tx, clientevents.Event{
		Type: EventChanged, Version: 1, Scope: "user", SubjectUserID: userID,
		EntityType: "risk_limit", EntityID: string(kind),
		Payload: payload, OccurredAt: time.Now().UTC(),
	})
}
