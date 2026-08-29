// Package events owns the durable client-event outbox and replay queries.
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Event struct {
	ID         int64
	Type       string
	Version    int
	Scope      string
	EntityType string
	EntityID   string
	Payload    json.RawMessage
	OccurredAt time.Time
}

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func Insert(ctx context.Context, tx pgx.Tx, event Event) error {
	payload := event.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	_, err := tx.Exec(ctx, `INSERT INTO client_events
		(event_type,version,scope,entity_type,entity_id,payload,occurred_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, event.Type, event.Version, event.Scope,
		event.EntityType, event.EntityID, payload, event.OccurredAt)
	if err != nil {
		return fmt.Errorf("insert client event: %w", err)
	}
	return nil
}

func (r *Repository) ListAfter(ctx context.Context, scope string, after int64, limit int) ([]Event, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,event_type,version,scope,entity_type,entity_id,payload,occurred_at
		FROM client_events WHERE scope=$1 AND id>$2 ORDER BY id LIMIT $3`, scope, after, limit)
	if err != nil {
		return nil, fmt.Errorf("list client events: %w", err)
	}
	defer rows.Close()
	result := make([]Event, 0)
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.Type, &event.Version, &event.Scope, &event.EntityType,
			&event.EntityID, &event.Payload, &event.OccurredAt); err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, rows.Err()
}

func (r *Repository) ListClientEvents(ctx context.Context, scope string, after int64, limit int) ([]Event, error) {
	return r.ListAfter(ctx, scope, after, limit)
}
