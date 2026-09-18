package paper

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"market-lens/server/internal/decimal"
)

// isNoRows is "the database had nothing to say", which is a normal answer here rather than a
// failure: an instrument with no stored bar has no price, and that is reported with a reason.
func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// The fold from recorded fills to what the account holds and what it has done.
//
// Everything here is derived and nothing is stored, so no figure can drift from the fills behind
// it — feature 022's rule, and what makes the totals reconcile at twelve decimal places. The fills
// themselves are stored, because each is a statement about a moment rather than about now.
//
// Cost basis is first-in, first-out, the same rule and the same shape as feature 022: a lot carries
// its **remaining total cost** rather than a unit cost, a sale that empties a lot takes everything
// left in it, and only a partial take divides. That was arrived at by a property test which found
// that dividing and multiplying back loses fractions at the twelfth place, so consumed cost plus
// remaining cost stopped equalling what was paid.

type lot struct {
	quantity dec
	cost     dec
}

// position is one instrument's standing, folded from its fills in order.
type position struct {
	quantity dec
	cost     dec
	realised dec
}

func foldFills(entries []fillEntry) position {
	var lots []lot
	var realised = decimal.Zero

	for _, entry := range entries {
		if entry.direction == DirectionBuy {
			// A buy's cost is what left the account: the consideration and every stated cost.
			lots = append(lots, lot{quantity: entry.quantity, cost: entry.cashEffect.Neg()})
			continue
		}
		// A sale consumes the oldest lots first, and its proceeds are what reached the account.
		remaining := entry.quantity
		consumed := decimal.Zero
		for remaining.Sign() > 0 && len(lots) > 0 {
			first := &lots[0]
			take := remaining
			if take.Cmp(first.quantity) > 0 {
				take = first.quantity
			}
			var taken dec
			if take.Cmp(first.quantity) == 0 {
				// Everything left in the lot, exactly. Dividing here is what loses the fraction.
				taken = first.cost
			} else {
				taken = first.cost.Mul(take).Div(first.quantity)
			}
			consumed = consumed.Add(taken)
			first.quantity = first.quantity.Sub(take)
			first.cost = first.cost.Sub(taken)
			remaining = remaining.Sub(take)
			if first.quantity.Sign() <= 0 {
				lots = lots[1:]
			}
		}
		realised = realised.Add(entry.cashEffect.Sub(consumed))
	}

	held := decimal.Zero
	cost := decimal.Zero
	for _, remaining := range lots {
		held = held.Add(remaining.quantity)
		cost = cost.Add(remaining.cost)
	}
	return position{quantity: held, cost: cost, realised: realised}
}

// fillEntry is one recorded fill, in the order it happened.
type fillEntry struct {
	instrumentID string
	ticker       string
	name         string
	currency     string
	direction    Direction
	quantity     dec
	cashEffect   dec
	session      string
}

// View reads the whole account: what it holds, what that is worth, and what it has done.
func (s *Service) View(ctx context.Context, userID string) (View, error) {
	if err := s.ready(userID); err != nil {
		return View{}, err
	}
	account, err := s.repository.Account(ctx, userID)
	if err != nil {
		return View{}, err
	}
	entries, err := s.repository.fills(ctx, userID)
	if err != nil {
		return View{}, err
	}
	orders, err := s.repository.Orders(ctx, userID)
	if err != nil {
		return View{}, err
	}

	startingCash, err := decimal.ParseDec(account.StartingCash)
	if err != nil {
		return View{}, fmt.Errorf("starting cash: %w", err)
	}

	byInstrument := map[string][]fillEntry{}
	firstSession := map[string]string{}
	order := make([]string, 0, 8)
	cash := startingCash
	for _, entry := range entries {
		if _, seen := byInstrument[entry.instrumentID]; !seen {
			order = append(order, entry.instrumentID)
			firstSession[entry.instrumentID] = entry.session
		}
		byInstrument[entry.instrumentID] = append(byInstrument[entry.instrumentID], entry)
		cash = cash.Add(entry.cashEffect)
	}

	view := View{
		Account: account, Cash: cash.String(), Orders: orders,
		Holdings: make([]Holding, 0, len(order)), IsASimulation: true,
	}
	totalCost := decimal.Zero
	totalRealised := decimal.Zero
	totalValue := decimal.Zero
	complete := true
	var incomplete *string

	for _, instrumentID := range order {
		entries := byInstrument[instrumentID]
		folded := foldFills(entries)
		totalRealised = totalRealised.Add(folded.realised)
		if folded.quantity.Sign() <= 0 {
			// Sold out. Its realised result is counted above and it is no longer a holding.
			continue
		}
		totalCost = totalCost.Add(folded.cost)

		first := entries[0]
		holding := Holding{
			InstrumentID: UUID(instrumentID), Ticker: first.ticker, Name: first.name,
			Currency: first.currency, Quantity: folded.quantity.String(), Cost: folded.cost.String(),
		}

		valued, err := s.repository.valueOf(ctx, instrumentID, account.AccountingCurrency,
			folded.quantity)
		if err != nil {
			return View{}, err
		}
		if valued.absent != "" {
			// A holding the product cannot price leaves the total unstated rather than understated.
			// An account reported as smaller than it is would be a quiet, flattering lie about a
			// loss that never happened.
			reason := valued.absent
			holding.AbsenceReason = &reason
			complete = false
			if incomplete == nil {
				incomplete = &reason
			}
		} else {
			value := valued.value.String()
			session := SessionDate(valued.session)
			unrealised := valued.value.Sub(folded.cost).String()
			holding.Value, holding.Session, holding.Unrealised = &value, &session, &unrealised
			totalValue = totalValue.Add(valued.value)
		}

		// The window runs from the first fill in this instrument to the session it was last valued.
		comparison, err := s.repository.compare(ctx, instrumentID,
			firstSession[instrumentID], valued.session,
			folded.cost, valued.value, valued.absent == "")
		if err != nil {
			return View{}, err
		}
		holding.Comparison = comparison
		view.Holdings = append(view.Holdings, holding)
	}

	view.Totals = Totals{
		Cost: totalCost.String(), Realised: totalRealised.String(),
		Complete: complete, IncompleteReason: incomplete,
	}
	if complete {
		value := totalValue.String()
		unrealised := totalValue.Sub(totalCost).String()
		view.Totals.Value, view.Totals.Unrealised = &value, &unrealised

		// The one place in this product that reports a return.
		//
		// Feature 022 declines to, because it never saw the deposits and withdrawals that would
		// make one meaningful. Here the account started at a stated balance and every movement
		// since was recorded by this product, so the figure is exact rather than invented.
		if startingCash.Sign() > 0 {
			total := cash.Add(totalValue).Sub(startingCash).Div(startingCash).String()
			view.Totals.TotalReturn = &total
		}
	}
	return view, nil
}

// fills reads every recorded fill for a person, oldest first, which is the order the fold needs.
func (r *Repository) fills(ctx context.Context, userID string) ([]fillEntry, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT o.instrument_id::text, i.ticker, i.name, i.currency,
			o.direction, f.quantity::text, f.cash_effect::text, f.fill_session::text
		FROM paper_fills f
		JOIN paper_orders o ON o.id = f.order_id
		JOIN instruments i ON i.id = o.instrument_id
		WHERE f.user_id = $1
		ORDER BY f.fill_session, f.filled_at, f.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("read the fills: %w", err)
	}
	defer rows.Close()

	var entries []fillEntry
	for rows.Next() {
		var entry fillEntry
		var quantity, cashEffect string
		if err := rows.Scan(&entry.instrumentID, &entry.ticker, &entry.name, &entry.currency,
			&entry.direction, &quantity, &cashEffect, &entry.session); err != nil {
			return nil, fmt.Errorf("scan a fill: %w", err)
		}
		if entry.quantity, err = decimal.ParseDec(quantity); err != nil {
			return nil, fmt.Errorf("fill quantity: %w", err)
		}
		if entry.cashEffect, err = decimal.ParseDec(cashEffect); err != nil {
			return nil, fmt.Errorf("fill cash effect: %w", err)
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// valuation is what a holding is worth, or why it cannot be said.
type valuation struct {
	value   dec
	session string
	absent  string
}

// valueOf prices a holding at the latest session that instrument actually traded, converted into
// the account's currency at that session's rate.
//
// Each holding at its own latest session, rather than one session for the whole account: a
// portfolio-wide session leaves Helsinki unvalued whenever Stockholm traded later, which is the
// union-calendar problem features 021 and 022 already solved this way.
func (r *Repository) valueOf(ctx context.Context, instrumentID, accounting string, quantity dec) (valuation, error) {
	var closeText, session, listing string
	err := r.pool.QueryRow(ctx, `SELECT b.close::text, b.session_date::text, i.currency
		FROM daily_price_bars b JOIN instruments i ON i.id = b.instrument_id
		WHERE b.instrument_id = $1 ORDER BY b.session_date DESC LIMIT 1`, instrumentID).
		Scan(&closeText, &session, &listing)
	if isNoRows(err) {
		return valuation{absent: "no_price"}, nil
	}
	if err != nil {
		return valuation{}, fmt.Errorf("read the latest close: %w", err)
	}
	price, err := decimal.ParseDec(closeText)
	if err != nil {
		return valuation{}, fmt.Errorf("close of %s: %w", session, err)
	}
	rate, converted, err := r.conversionRate(ctx, listing, accounting, session)
	if err != nil {
		return valuation{absent: "no_rate"}, nil
	}
	gross := price.Mul(quantity)
	if converted {
		gross = gross.Div(rate)
	}
	return valuation{value: gross, session: session}, nil
}

// compare measures a holding against the benchmark for its market, over the period it was held.
//
// Its own period, not a fixed window: a holding bought last month and one held for five years
// cannot be compared against the same stretch of index without saying something untrue about one
// of them. The window runs from the session of its first fill to the session it was last valued.
func (r *Repository) compare(ctx context.Context, instrumentID string, from, to string,
	cost, value dec, valued bool) (Comparison, error) {
	var code string
	err := r.pool.QueryRow(ctx, `SELECT b.code
		FROM instruments i
		JOIN exchanges e ON e.id = i.exchange_id
		JOIN benchmark_series b ON b.mic = e.mic
		WHERE i.id = $1`, instrumentID).Scan(&code)
	if isNoRows(err) {
		// No benchmark for this market is a stated absence, not a failure.
		reason := "no_benchmark"
		return Comparison{AbsenceReason: &reason}, nil
	}
	if err != nil {
		return Comparison{}, fmt.Errorf("read the benchmark: %w", err)
	}

	comparison := Comparison{Series: code}
	if !valued || cost.Sign() <= 0 {
		reason := "holding_not_valued"
		comparison.AbsenceReason = &reason
		return comparison, nil
	}
	// What the holding did, on the same basis as the benchmark: the change against what it cost.
	holdingReturn := value.Sub(cost).Div(cost).String()
	comparison.HoldingReturn = &holdingReturn

	var opening, closing *string
	if err := r.pool.QueryRow(ctx, `SELECT
		(SELECT p.close::text FROM benchmark_points p JOIN benchmark_series b ON b.id = p.series_id
		 WHERE b.code = $1 AND p.session_date >= $2::date ORDER BY p.session_date LIMIT 1),
		(SELECT p.close::text FROM benchmark_points p JOIN benchmark_series b ON b.id = p.series_id
		 WHERE b.code = $1 AND p.session_date <= $3::date ORDER BY p.session_date DESC LIMIT 1)`,
		code, from, to).Scan(&opening, &closing); err != nil {
		return Comparison{}, fmt.Errorf("read %s over the holding period: %w", code, err)
	}
	if opening == nil || closing == nil {
		reason := "benchmark_incomplete"
		comparison.AbsenceReason = &reason
		return comparison, nil
	}
	first, err := decimal.ParseDec(*opening)
	if err != nil {
		return Comparison{}, fmt.Errorf("%s opening: %w", code, err)
	}
	last, err := decimal.ParseDec(*closing)
	if err != nil {
		return Comparison{}, fmt.Errorf("%s closing: %w", code, err)
	}
	if first.Sign() <= 0 {
		reason := "benchmark_incomplete"
		comparison.AbsenceReason = &reason
		return comparison, nil
	}
	benchmarkReturn := last.Sub(first).Div(first).String()
	comparison.BenchmarkReturn = &benchmarkReturn
	return comparison, nil
}
