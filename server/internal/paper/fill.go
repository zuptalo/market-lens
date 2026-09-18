package paper

import (
	"context"
	"errors"
	"fmt"
	"time"

	"market-lens/server/internal/costs"
	"market-lens/server/internal/decimal"
	"market-lens/server/internal/instruments"
	"market-lens/server/internal/notify"

	"github.com/jackc/pgx/v5"
)

type dec = decimal.Dec

// pending is one order waiting for a price, with everything the pass needs to decide about it.
type pending struct {
	orderID       string
	userID        string
	accountID     string
	instrumentID  string
	direction     Direction
	quantity      dec
	placedSession string
	listing       string
	accounting    string
	rates         CostRates
}

// FillPending fills every pending order that now has a price, for every account.
//
// It runs after an import lands, so a record accrues whether or not anybody visits. Two properties
// matter more than speed: it reads the *open* of a session strictly after the one the order was
// placed in — the database refuses anything else — and it writes each account's rows under that
// account's own identifier, so one person's pass can never touch another's.
func (s *Service) FillPending(ctx context.Context) (int, error) {
	if s == nil || s.repository == nil {
		return 0, errors.New("paper service is not configured")
	}
	orders, err := s.repository.pendingOrders(ctx)
	if err != nil {
		return 0, err
	}

	filled := 0
	for _, order := range orders {
		done, err := s.fillOne(ctx, order)
		if err != nil {
			// One order that cannot be decided must not stop the rest: the others have prices and
			// the person is waiting on them. It is logged and left pending for the next pass.
			s.logger.Error("a pending paper order could not be settled",
				"order", order.orderID, "error", err)
			continue
		}
		if done {
			filled++
		}
	}
	return filled, nil
}

// fillOne decides about a single pending order. It returns whether the order actually filled;
// an order that became unfillable is settled but did not fill.
func (s *Service) fillOne(ctx context.Context, order pending) (bool, error) {
	bar, found, err := s.repository.nextOpen(ctx, order.instrumentID, order.placedSession)
	if err != nil {
		return false, err
	}
	if !found {
		// No price yet. That is ordinary the day after an order is placed, and only becomes a
		// verdict once the product has waited long enough that no price is coming.
		stale, err := s.repository.sessionsWaited(ctx, order.placedSession)
		if err != nil {
			return false, err
		}
		if stale > GiveUpSessions {
			return false, s.repository.markUnfillable(ctx, order, AbsenceNoPrice)
		}
		return false, nil
	}

	rate, converted, err := s.repository.conversionRate(ctx,
		order.listing, order.accounting, bar.session)
	if err != nil {
		return false, err
	}

	model, err := costRatesToModel(order.rates)
	if err != nil {
		return false, err
	}
	priced := costs.Price(model, costs.Direction(order.direction),
		order.quantity, bar.open, rate, converted)

	// Cash and position are read as they stand *now*, after every fill already recorded. An order
	// is affordable or not at the moment it fills, not at the moment it was placed.
	position, cash, err := s.repository.standing(ctx, order.userID, order.instrumentID)
	if err != nil {
		return false, err
	}

	switch order.direction {
	case DirectionBuy:
		if cash.Add(priced.CashEffect).Sign() < 0 {
			return false, s.repository.markUnfillable(ctx, order, AbsenceInsufficientCash)
		}
	case DirectionSell:
		if position.Cmp(order.quantity) < 0 {
			// A paper account cannot go short. The order is reported, not silently reduced: a
			// partial fill nobody asked for is a decision the product would be making.
			return false, s.repository.markUnfillable(ctx, order, AbsenceExceedsPosition)
		}
	}

	return true, s.repository.recordFill(ctx, order, bar, priced, rate)
}

func costRatesToModel(rates CostRates) (costs.Model, error) {
	slippage, err := decimal.ParseDec(rates.SlippageBps)
	if err != nil {
		return costs.Model{}, fmt.Errorf("slippage: %w", err)
	}
	spread, err := decimal.ParseDec(rates.CurrencySpreadBps)
	if err != nil {
		return costs.Model{}, fmt.Errorf("currency spread: %w", err)
	}
	brokerage, err := decimal.ParseDec(rates.BrokerageBps)
	if err != nil {
		return costs.Model{}, fmt.Errorf("brokerage: %w", err)
	}
	minimum, err := decimal.ParseDec(rates.BrokerageMinimum)
	if err != nil {
		return costs.Model{}, fmt.Errorf("brokerage minimum: %w", err)
	}
	return costs.FromBasisPoints(slippage, spread, brokerage, minimum), nil
}

// bar is the price a fill used, and when the product last observed it.
type bar struct {
	session    string
	open       dec
	observedAt time.Time
}

func (r *Repository) pendingOrders(ctx context.Context) ([]pending, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT o.id::text, o.user_id::text, o.account_id::text,
			o.instrument_id::text, o.direction, o.quantity::text, o.placed_session::text,
			i.currency, a.accounting_currency, a.brokerage_bps::text, a.brokerage_minimum::text,
			a.slippage_bps::text, a.currency_spread_bps::text
		FROM paper_orders o
		JOIN instruments i ON i.id = o.instrument_id
		JOIN paper_accounts a ON a.id = o.account_id
		WHERE o.state = 'pending'
		ORDER BY o.placed_session, o.placed_at, o.id`)
	if err != nil {
		return nil, fmt.Errorf("read the pending orders: %w", err)
	}
	defer rows.Close()

	var orders []pending
	for rows.Next() {
		var order pending
		var quantity string
		if err := rows.Scan(&order.orderID, &order.userID, &order.accountID, &order.instrumentID,
			&order.direction, &quantity, &order.placedSession, &order.listing, &order.accounting,
			&order.rates.BrokerageBps, &order.rates.BrokerageMinimum,
			&order.rates.SlippageBps, &order.rates.CurrencySpreadBps); err != nil {
			return nil, fmt.Errorf("scan a pending order: %w", err)
		}
		parsed, err := decimal.ParseDec(quantity)
		if err != nil {
			return nil, fmt.Errorf("quantity of %s: %w", order.orderID, err)
		}
		order.quantity = parsed
		orders = append(orders, order)
	}
	return orders, rows.Err()
}

// nextOpen is the open of the first stored session strictly after `after`.
//
// Strictly after, and the open rather than the close: that is the earliest price the person could
// really have paid, and it is the one price in the stored data that provably did not exist when
// they placed the order.
func (r *Repository) nextOpen(ctx context.Context, instrumentID, after string) (bar, bool, error) {
	var found bar
	var open string
	err := r.pool.QueryRow(ctx, `SELECT session_date::text, open::text, last_observed_at
		FROM daily_price_bars
		WHERE instrument_id = $1 AND session_date > $2::date
		ORDER BY session_date LIMIT 1`, instrumentID, after).
		Scan(&found.session, &open, &found.observedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return bar{}, false, nil
	}
	if err != nil {
		return bar{}, false, fmt.Errorf("read the next open: %w", err)
	}
	parsed, err := decimal.ParseDec(open)
	if err != nil {
		return bar{}, false, fmt.Errorf("open of %s: %w", found.session, err)
	}
	found.open = parsed
	return found, true, nil
}

// sessionsWaited is how many days have passed since the order was placed. Days rather than
// sessions, because the thing being waited for is a session that has not happened.
func (r *Repository) sessionsWaited(ctx context.Context, placed string) (int, error) {
	var days int
	if err := r.pool.QueryRow(ctx,
		`SELECT (current_date - $1::date)::int`, placed).Scan(&days); err != nil {
		return 0, fmt.Errorf("measure the wait: %w", err)
	}
	return days, nil
}

// conversionRate is the accounting currency's stored rate for the listing currency on that session.
//
// Both are read against the euro and divided, rather than multiplying by an inverse nobody stored,
// so a conversion and its reverse cannot disagree at the twelfth decimal place. Feature 021 stores
// the rates one direction only for that reason.
func (r *Repository) conversionRate(ctx context.Context, listing, accounting, session string) (dec, bool, error) {
	if listing == accounting {
		return decimal.One, false, nil
	}
	rateOf := func(currency string) (dec, error) {
		if currency == "EUR" {
			return decimal.One, nil
		}
		var value string
		err := r.pool.QueryRow(ctx, `SELECT rate::text FROM fx_rates
			WHERE base = 'EUR' AND quote = $1 AND session_date <= $2::date
			ORDER BY session_date DESC LIMIT 1`, currency, session).Scan(&value)
		if errors.Is(err, pgx.ErrNoRows) {
			return decimal.Zero, fmt.Errorf("no stored rate for %s on or before %s", currency, session)
		}
		if err != nil {
			return decimal.Zero, fmt.Errorf("read the rate for %s: %w", currency, err)
		}
		return decimal.ParseDec(value)
	}
	listingRate, err := rateOf(listing)
	if err != nil {
		return decimal.Zero, false, err
	}
	accountingRate, err := rateOf(accounting)
	if err != nil {
		return decimal.Zero, false, err
	}
	// costs.ToAccounting divides by this, so it is the listing units per accounting unit.
	return listingRate.Div(accountingRate), true, nil
}

// standing is the position and cash an account holds right now, folded from its recorded fills.
//
// Derived rather than stored, so it cannot drift from the fills behind it — but the fills
// themselves are stored, because each is a statement about a moment.
func (r *Repository) standing(ctx context.Context, userID, instrumentID string) (dec, dec, error) {
	var held, cash string
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE((
			SELECT sum(CASE WHEN o.direction = 'buy' THEN f.quantity ELSE -f.quantity END)
			FROM paper_fills f JOIN paper_orders o ON o.id = f.order_id
			WHERE f.user_id = $1 AND o.instrument_id = $2
		), 0)::text,
		(SELECT a.starting_cash + COALESCE((
			SELECT sum(f.cash_effect) FROM paper_fills f WHERE f.user_id = $1
		), 0) FROM paper_accounts a WHERE a.user_id = $1)::text`,
		userID, instrumentID).Scan(&held, &cash)
	if err != nil {
		return decimal.Zero, decimal.Zero, fmt.Errorf("read what the account stands at: %w", err)
	}
	position, err := decimal.ParseDec(held)
	if err != nil {
		return decimal.Zero, decimal.Zero, fmt.Errorf("position: %w", err)
	}
	balance, err := decimal.ParseDec(cash)
	if err != nil {
		return decimal.Zero, decimal.Zero, fmt.Errorf("cash: %w", err)
	}
	return position, balance, nil
}

// recordFill writes the fill, settles the order, and publishes — on one transaction, so an order
// can never read as filled beside no fill.
func (r *Repository) recordFill(ctx context.Context, order pending, priced bar,
	execution costs.Execution, rate dec) error {
	id, err := instruments.NewUUID()
	if err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `INSERT INTO paper_fills
		(id, order_id, user_id, fill_session, open_price, quantity, costs, cash_effect,
		 conversion_rate, bar_observed_at)
		VALUES ($1,$2,$3,$4::date,$5::numeric,$6::numeric,$7::numeric,$8::numeric,$9::numeric,$10)`,
		id.String(), order.orderID, order.userID, priced.session, priced.open.String(),
		order.quantity.String(), execution.Total().String(), execution.CashEffect.String(),
		rate.String(), priced.observedAt); err != nil {
		return fmt.Errorf("record the fill: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE paper_orders SET state = 'filled', settled_at = now()
		WHERE id = $1 AND user_id = $2 AND state = 'pending'`,
		order.orderID, order.userID); err != nil {
		return fmt.Errorf("settle the order: %w", err)
	}
	if err := publish(ctx, tx, order.userID, order.orderID, "filled"); err != nil {
		return err
	}
	// Tell the person, if they asked to be told. On this transaction: if the fill rolls back, so
	// does the telling — nobody is informed about something that did not happen.
	if err := raiseFillNotice(ctx, tx, order, "filled"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// raiseFillNotice is the one place this package knows notifications exist.
//
// It carries a ticker and an outcome and nothing else: no price, no quantity, no cash. What
// somebody owns is not sent anywhere, and the notification schema refuses it if this ever tries.
func raiseFillNotice(ctx context.Context, tx pgx.Tx, order pending, outcome string) error {
	var ticker string
	if err := tx.QueryRow(ctx, `SELECT ticker FROM instruments WHERE id = $1`,
		order.instrumentID).Scan(&ticker); err != nil {
		ticker = ""
	}
	_, err := notify.RaiseIn(ctx, tx, notify.Raise{
		Kind:       notify.KindPaperFill,
		SubjectKey: order.orderID,
		Count:      1,
		Detail:     map[string]string{"ticker": ticker, "outcome": outcome},
		Audience:   []notify.UUID{notify.UUID(order.userID)},
	}, time.Now().UTC())
	return err
}

// markUnfillable settles an order that cannot be filled, with the reason it could not.
func (r *Repository) markUnfillable(ctx context.Context, order pending, reason AbsenceReason) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `UPDATE paper_orders
		SET state = 'unfillable', absence_reason = $3, settled_at = now()
		WHERE id = $1 AND user_id = $2 AND state = 'pending'`,
		order.orderID, order.userID, string(reason)); err != nil {
		return fmt.Errorf("settle the order: %w", err)
	}
	if err := publish(ctx, tx, order.userID, order.orderID, string(reason)); err != nil {
		return err
	}
	if err := raiseFillNotice(ctx, tx, order, string(reason)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
