// Package events owns the durable client-event outbox and replay queries.
package events

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"market-lens/server/internal/authorization"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrAudienceUnavailable refuses a replay without saying why, because the reason is itself
// information about the account being asked about.
var ErrAudienceUnavailable = errors.New("event audience is unavailable")

type Event struct {
	ID            int64
	Type          string
	Version       int
	Scope         string
	SubjectUserID string
	EntityType    string
	EntityID      string
	Payload       json.RawMessage
	OccurredAt    time.Time
}

// Audience is who a replay is for. It is built from the persisted session and user record, so
// Deactivated reflects durable account state rather than anything the client asserts.
type Audience struct {
	UserID      string
	Role        string
	Deactivated bool
}

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func Insert(ctx context.Context, tx pgx.Tx, event Event) error {
	payload := event.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	_, err := tx.Exec(ctx, `INSERT INTO client_events
		(event_type,version,scope,subject_user_id,entity_type,entity_id,payload,occurred_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, event.Type, event.Version, event.Scope,
		nullableSubject(event.SubjectUserID), event.EntityType, event.EntityID, payload, event.OccurredAt)
	if err != nil {
		return fmt.Errorf("insert client event: %w", err)
	}
	return nil
}

func (r *Repository) ListAuthorized(ctx context.Context, audience Audience, after int64, limit int) ([]Event, error) {
	if r == nil || r.pool == nil || !validAudience(audience) || after < 0 || limit < 1 || limit > 1000 {
		return nil, errors.New("event replay audience or bounds are invalid")
	}
	// A deactivated account keeps no replay access, including with a cursor it held while active.
	if err := authorization.Require(authorization.Principal{
		UserID: audience.UserID, Role: authorization.Role(audience.Role),
		Deactivated: audience.Deactivated, Authenticated: true,
	}, authorization.Resource{Scope: authorization.ScopeShared}); err != nil {
		return nil, ErrAudienceUnavailable
	}
	rows, err := r.pool.Query(ctx, `SELECT id,event_type,version,scope,subject_user_id,entity_type,entity_id,payload,occurred_at
		FROM client_events
		WHERE id>$2 AND (
			scope='shared'
			OR (scope='user' AND subject_user_id=$1)
			OR (scope='owner' AND $3='owner')
		)
		ORDER BY id LIMIT $4`, audience.UserID, after, audience.Role, limit)
	if err != nil {
		return nil, fmt.Errorf("list authorized client events: %w", err)
	}
	defer rows.Close()
	result := make([]Event, 0)
	for rows.Next() {
		var event Event
		var subjectUserID *string
		if err := rows.Scan(&event.ID, &event.Type, &event.Version, &event.Scope, &subjectUserID,
			&event.EntityType, &event.EntityID, &event.Payload, &event.OccurredAt); err != nil {
			return nil, err
		}
		if subjectUserID != nil {
			event.SubjectUserID = *subjectUserID
		}
		result = append(result, event)
	}
	return result, rows.Err()
}

func validAudience(audience Audience) bool {
	return (audience.Role == "owner" || audience.Role == "member") && validUUID(audience.UserID)
}

func validUUID(value string) bool {
	decoded, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	return err == nil && len(value) == 36 && len(decoded) == 16
}

func nullableSubject(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// Audience resolves who a stream is for from the durable user record. Role and status are read
// here rather than taken from the session snapshot, so a demotion or deactivation narrows an
// already-open stream at its next check instead of at its next sign-in.
func (r *Repository) Audience(ctx context.Context, userID string) (Audience, error) {
	if r == nil || r.pool == nil || !validUUID(userID) {
		return Audience{}, ErrAudienceUnavailable
	}
	var role, status string
	err := r.pool.QueryRow(ctx, `SELECT role,status FROM users WHERE id=$1`, userID).Scan(&role, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Audience{}, ErrAudienceUnavailable
	}
	if err != nil {
		return Audience{}, fmt.Errorf("resolve event audience: %w", err)
	}
	return Audience{UserID: userID, Role: role, Deactivated: status != "active"}, nil
}
