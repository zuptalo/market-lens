package intents

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
// As in features 022 and 023, the caller's user identifier is a required argument and appears in the
// WHERE clause of every statement. It is never inferred from an intent identifier.
type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) ready() error {
	if r == nil || r.pool == nil {
		return errors.New("intents repository is not configured")
	}
	return nil
}

const intentColumns = `o.id::text, o.user_id::text, o.instrument_id::text, i.ticker, i.name,
	i.currency, o.direction, o.quantity::text, o.price::text, o.costs::text, o.status,
	o.recorded_at, o.settled_at`

const intentSource = `order_intents o JOIN instruments i ON i.id = o.instrument_id`

func scanIntent(row interface{ Scan(...any) error }) (Intent, error) {
	var intent Intent
	if err := row.Scan((*string)(&intent.ID), (*string)(&intent.UserID),
		(*string)(&intent.InstrumentID), &intent.Ticker, &intent.Name, &intent.Currency,
		(*string)(&intent.Direction), &intent.Quantity, &intent.Price, &intent.Costs,
		(*string)(&intent.Status), &intent.RecordedAt, &intent.SettledAt); err != nil {
		return Intent{}, err
	}
	return intent, nil
}

// List reads one person's intents, newest first.
func (r *Repository) List(ctx context.Context, userID string, includeSettled bool) ([]Intent, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	condition := ` AND o.status = 'considering'`
	if includeSettled {
		condition = ``
	}
	rows, err := r.pool.Query(ctx, `SELECT `+intentColumns+`
		FROM `+intentSource+`
		WHERE o.user_id = $1`+condition+`
		ORDER BY o.recorded_at DESC, o.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("read the intents: %w", err)
	}
	defer rows.Close()
	var intents []Intent
	for rows.Next() {
		intent, err := scanIntent(rows)
		if err != nil {
			return nil, err
		}
		intents = append(intents, intent)
	}
	return intents, rows.Err()
}

// Intent reads one, scoped to its owner.
func (r *Repository) Intent(ctx context.Context, userID, intentID string) (Intent, error) {
	if err := r.ready(); err != nil {
		return Intent{}, err
	}
	row := r.pool.QueryRow(ctx, `SELECT `+intentColumns+` FROM `+intentSource+`
		WHERE o.user_id = $1 AND o.id = $2`, userID, intentID)
	intent, err := scanIntent(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Intent{}, ErrNotFound
		}
		return Intent{}, fmt.Errorf("read the intent: %w", err)
	}
	return intent, nil
}

// InstrumentIsCarried reports whether this product has the instrument at all.
func (r *Repository) InstrumentIsCarried(ctx context.Context, instrumentID string) (bool, error) {
	if err := r.ready(); err != nil {
		return false, err
	}
	var carried bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM instruments WHERE id = $1 AND active)`, instrumentID).Scan(&carried); err != nil {
		return false, fmt.Errorf("read the instrument: %w", err)
	}
	return carried, nil
}

// Insert writes one intent and publishes the owner-scoped event on the same transaction.
func (r *Repository) Insert(ctx context.Context, userID string, request RecordRequest,
	quantity, price, costs string) (Intent, error) {
	if err := r.ready(); err != nil {
		return Intent{}, err
	}
	id, err := instruments.NewUUID()
	if err != nil {
		return Intent{}, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Intent{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `INSERT INTO order_intents
		(id, user_id, instrument_id, direction, quantity, price, costs, status)
		VALUES ($1,$2,$3,$4,$5::numeric,$6::numeric,$7::numeric,'considering')`,
		id.String(), userID, request.InstrumentID.String(), string(request.Direction),
		quantity, price, costs); err != nil {
		return Intent{}, fmt.Errorf("record the intent: %w", err)
	}
	if err := publish(ctx, tx, userID, id.String(), "recorded"); err != nil {
		return Intent{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Intent{}, fmt.Errorf("commit: %w", err)
	}
	return r.Intent(ctx, userID, id.String())
}

// Settle marks an intent withdrawn or acted on. It touches nothing else: acting on an intent is not
// a trade, and the portfolio is where a trade is asserted.
func (r *Repository) Settle(ctx context.Context, userID, intentID string, status Status) error {
	if err := r.ready(); err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `UPDATE order_intents SET status = $3, settled_at = now()
		WHERE id = $1 AND user_id = $2 AND status = 'considering'`, intentID, userID, string(status))
	if err != nil {
		return fmt.Errorf("settle the intent: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := publish(ctx, tx, userID, intentID, string(status)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// publish writes the owner-scoped event on the same transaction as the change. It carries the
// intent's identity and what happened to it, never its consequence — that is computed on read, and
// a copy in the payload would disagree the moment a price moved.
func publish(ctx context.Context, tx pgx.Tx, userID, intentID, change string) error {
	payload, err := json.Marshal(map[string]string{"intent_id": intentID, "change": change})
	if err != nil {
		return fmt.Errorf("encode the change: %w", err)
	}
	return clientevents.Insert(ctx, tx, clientevents.Event{
		Type: EventChanged, Version: 1, Scope: "user", SubjectUserID: userID,
		EntityType: "order_intent", EntityID: intentID,
		Payload: payload, OccurredAt: time.Now().UTC(),
	})
}
