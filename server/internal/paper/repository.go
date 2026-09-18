package paper

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

// Repository holds the owner-scoped queries.
//
// Every predicate reads the row's own `user_id`, which the database holds equal to its account's
// owner by a composite foreign key — so a query scoped only by account cannot widen access and a
// forgotten join cannot leak. FillPending is the one query that spans people, and it therefore
// writes each account's rows under that account's own identifier rather than a shared one.
type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) ready() error {
	if r == nil || r.pool == nil {
		return errors.New("paper repository is not configured")
	}
	return nil
}

// Account reads a person's account. Absent is a normal answer: most people have not opened one.
func (r *Repository) Account(ctx context.Context, userID string) (Account, error) {
	if err := r.ready(); err != nil {
		return Account{}, err
	}
	var account Account
	err := r.pool.QueryRow(ctx, `SELECT id::text, user_id::text, starting_cash::text,
		accounting_currency, brokerage_bps::text, brokerage_minimum::text, slippage_bps::text,
		currency_spread_bps::text, opened_at
		FROM paper_accounts WHERE user_id = $1`, userID).Scan(
		&account.ID, &account.UserID, &account.StartingCash, &account.AccountingCurrency,
		&account.Costs.BrokerageBps, &account.Costs.BrokerageMinimum,
		&account.Costs.SlippageBps, &account.Costs.CurrencySpreadBps, &account.OpenedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("read the account: %w", err)
	}
	return account, nil
}

// Open writes the account and publishes the owner-scoped event on the same transaction.
func (r *Repository) Open(ctx context.Context, userID, cash, currency string, rates CostRates) (Account, error) {
	if err := r.ready(); err != nil {
		return Account{}, err
	}
	id, err := instruments.NewUUID()
	if err != nil {
		return Account{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Account{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `INSERT INTO paper_accounts
		(id, user_id, starting_cash, accounting_currency,
		 brokerage_bps, brokerage_minimum, slippage_bps, currency_spread_bps)
		VALUES ($1,$2,$3::numeric,$4,$5::numeric,$6::numeric,$7::numeric,$8::numeric)`,
		id.String(), userID, cash, currency, rates.BrokerageBps, rates.BrokerageMinimum,
		rates.SlippageBps, rates.CurrencySpreadBps); err != nil {
		return Account{}, fmt.Errorf("open the account: %w", err)
	}
	if err := publish(ctx, tx, userID, id.String(), "opened"); err != nil {
		return Account{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Account{}, fmt.Errorf("commit: %w", err)
	}
	return r.Account(ctx, userID)
}

// Promote writes the pending order, settles the intent it came from, and publishes — all on one
// transaction, so an order can never exist beside an intent that still says it is being considered.
func (r *Repository) Promote(ctx context.Context, userID, accountID string, intent promotedIntent,
	session SessionDate) (string, error) {
	if err := r.ready(); err != nil {
		return "", err
	}
	id, err := instruments.NewUUID()
	if err != nil {
		return "", err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `INSERT INTO paper_orders
		(id, account_id, user_id, intent_id, instrument_id, direction, quantity,
		 expected_price, placed_session, state)
		VALUES ($1,$2,$3,$4,$5,$6,$7::numeric,$8::numeric,$9::date,'pending')`,
		id.String(), accountID, userID, intent.id, intent.instrumentID, string(intent.direction),
		intent.quantity, intent.price, string(session)); err != nil {
		return "", fmt.Errorf("promote the intent: %w", err)
	}
	// The intent becomes acted on in the same transaction. A person acted: they committed it to a
	// simulated account. What they would have paid in the real world is still theirs to assert.
	tag, err := tx.Exec(ctx, `UPDATE order_intents SET status = 'acted_on', settled_at = now()
		WHERE id = $1 AND user_id = $2 AND status = 'considering'`, intent.id, userID)
	if err != nil {
		return "", fmt.Errorf("settle the intent: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return "", ErrNotFound
	}
	if err := publish(ctx, tx, userID, id.String(), "promoted"); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}
	return id.String(), nil
}

// Cancel withdraws a pending order. A filled one is untouched: a track record that can be edited
// after the fact measures nothing.
func (r *Repository) Cancel(ctx context.Context, userID, orderID string) error {
	if err := r.ready(); err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `UPDATE paper_orders
		SET state = 'cancelled', settled_at = now()
		WHERE id = $1 AND user_id = $2 AND state = 'pending'`, orderID, userID)
	if err != nil {
		return fmt.Errorf("cancel the order: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Somebody else's order, an order that never existed, and one that is already settled all
		// answer the same way to the layer above, which decides which of those it can say.
		return ErrNotFound
	}
	if err := publish(ctx, tx, userID, orderID, "cancelled"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Orders reads a person's orders, newest first, with the fill each one became.
func (r *Repository) Orders(ctx context.Context, userID string) ([]Order, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT o.id::text, o.intent_id::text, o.instrument_id::text,
			i.ticker, i.name, i.currency, o.direction, o.quantity::text, o.expected_price::text,
			o.placed_session::text, o.state, o.absence_reason, o.placed_at, o.settled_at,
			f.fill_session::text, f.open_price::text, f.quantity::text, f.costs::text,
			f.cash_effect::text, f.conversion_rate::text, f.bar_observed_at, f.filled_at,
			b.last_observed_at
		FROM paper_orders o
		JOIN instruments i ON i.id = o.instrument_id
		LEFT JOIN paper_fills f ON f.order_id = o.id
		LEFT JOIN daily_price_bars b
			ON b.instrument_id = o.instrument_id AND b.session_date = f.fill_session
		WHERE o.user_id = $1
		ORDER BY o.placed_at DESC, o.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("read the orders: %w", err)
	}
	defer rows.Close()

	orders := make([]Order, 0, 8)
	for rows.Next() {
		var order Order
		var reason *string
		var fillSession, openPrice, fillQuantity, fillCosts, cashEffect, rate *string
		var barObserved, filledAt, barLatest *time.Time
		if err := rows.Scan(&order.ID, &order.IntentID, &order.InstrumentID, &order.Ticker,
			&order.Name, &order.Currency, &order.Direction, &order.Quantity, &order.ExpectedPrice,
			&order.PlacedSession, &order.State, &reason, &order.PlacedAt, &order.SettledAt,
			&fillSession, &openPrice, &fillQuantity, &fillCosts, &cashEffect, &rate,
			&barObserved, &filledAt, &barLatest); err != nil {
			return nil, fmt.Errorf("scan an order: %w", err)
		}
		if reason != nil {
			value := AbsenceReason(*reason)
			order.AbsenceReason = &value
		}
		if fillSession != nil {
			fill := Fill{
				Session: SessionDate(*fillSession), OpenPrice: *openPrice,
				Quantity: *fillQuantity, Costs: *fillCosts, CashEffect: *cashEffect,
				ConversionRate: *rate,
			}
			if filledAt != nil {
				fill.FilledAt = *filledAt
			}
			// The bar has been re-observed since this fill read it. The fill is never re-priced:
			// saying so keeps both facts, which is what makes the history reconcilable.
			if barObserved != nil && barLatest != nil && barLatest.After(*barObserved) {
				fill.BarDiverged = true
			}
			order.Fill = &fill
		}
		orders = append(orders, order)
	}
	return orders, rows.Err()
}

// publish writes the owner-scoped event on the same transaction as the change. It carries the
// account's identity and what happened, never its figures: those are computed on read, and a copy
// in the payload would disagree the moment a price moved.
func publish(ctx context.Context, tx pgx.Tx, userID, entityID, change string) error {
	payload, err := json.Marshal(map[string]string{"entity_id": entityID, "change": change})
	if err != nil {
		return fmt.Errorf("encode the change: %w", err)
	}
	return clientevents.Insert(ctx, tx, clientevents.Event{
		Type: EventChanged, Version: 1, Scope: "user", SubjectUserID: userID,
		EntityType: "paper_account", EntityID: entityID,
		Payload: payload, OccurredAt: time.Now().UTC(),
	})
}

// promotedIntent is the part of an intent an order needs, read once and passed along rather than
// re-read inside the transaction.
type promotedIntent struct {
	id           string
	instrumentID string
	direction    Direction
	quantity     string
	price        string
}
