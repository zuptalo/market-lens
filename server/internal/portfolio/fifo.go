package portfolio

import "sort"

// First-in-first-out, folded over the trades rather than stored.
//
// Nothing here writes. A position, what it cost and what a sale realised are all a walk over one
// instrument's trades in the order the person recorded them — which is what makes a correction a
// re-read instead of a repair, and what makes FR-010 true by construction rather than by
// discipline.
//
// The oldest shares are sold first. That is what the Nordic tax authorities expect by default, so a
// person's figure has a chance of matching their own filing — though this product computes no tax
// and claims no figure is suitable for a return.

// lot is one purchase, consumed as sales take from it.
//
// It holds the cost still sitting in the lot rather than a cost per share, and that is not a
// detail. Dividing a lot's cost by its shares and multiplying back loses fractions at the twelfth
// place, and over a few dozen trades those fractions accumulate into a portfolio whose consumed
// cost and remaining cost no longer add up to what was paid — which the reconciliation property
// below found on its first attempt. Carrying the total and subtracting exactly what each sale takes
// makes conservation true by construction instead of true to twelve places.
type lot struct {
	quantity dec
	cost     dec
	date     SessionDate
}

// fold is the result of walking one instrument's trades.
type fold struct {
	quantity dec
	cost     dec
	realised []RealisedResult
	// costOpenedAt is the date of the oldest purchase still contributing to the remaining cost.
	// The benchmark comparison measures from here rather than from the first purchase ever made:
	// a window starting at a purchase whose shares are already sold compares a period the holding
	// no longer represents.
	costOpenedAt SessionDate
}

// walk folds one instrument's current trades into a position, a cost and its realised results.
//
// Trades arrive in (trade_date, sequence) order. A sale that would consume more than is held
// cannot reach here: the service refuses it at the point of recording, naming what is held.
func walk(trades []Trade) (fold, error) {
	sort.SliceStable(trades, func(i, j int) bool {
		if trades[i].TradeDate != trades[j].TradeDate {
			return trades[i].TradeDate < trades[j].TradeDate
		}
		return trades[i].Sequence < trades[j].Sequence
	})

	var open []lot
	result := fold{quantity: decZero, cost: decZero}

	for _, trade := range trades {
		quantity, err := parseDec(trade.Quantity)
		if err != nil {
			return fold{}, err
		}
		price, err := parseDec(trade.Price)
		if err != nil {
			return fold{}, err
		}
		costs := decZero
		if trade.Costs != "" {
			if costs, err = parseDec(trade.Costs); err != nil {
				return fold{}, err
			}
		}

		if trade.Direction == DirectionBuy {
			// What the shares actually cost: the consideration plus the trade's own costs. No
			// division, so nothing is lost before a sale ever reads it.
			open = append(open, lot{quantity: quantity, cost: price.Mul(quantity).Add(costs),
				date: trade.TradeDate})
			continue
		}

		remaining := quantity
		consumedCost := decZero
		for len(open) > 0 && remaining.Sign() > 0 {
			first := &open[0]
			take := remaining
			if first.quantity.Cmp(take) < 0 {
				take = first.quantity
			}

			// A sale that empties a lot takes everything left in it, to the last unit. Only a
			// partial take divides, and what it leaves behind is whatever it did not take — so the
			// two halves always sum to the whole however the rounding falls.
			var taken dec
			if take.Cmp(first.quantity) == 0 {
				taken = first.cost
			} else {
				taken = first.cost.Mul(take).Div(first.quantity)
			}
			consumedCost = consumedCost.Add(taken)
			first.cost = first.cost.Sub(taken)
			first.quantity = first.quantity.Sub(take)
			remaining = remaining.Sub(take)
			if first.quantity.Sign() <= 0 {
				open = open[1:]
			}
		}

		// Proceeds are what was received less what the sale itself cost. Both trades' costs are
		// inside the realised figure, which is the whole point of reporting one.
		proceeds := price.Mul(quantity).Sub(costs)
		result.realised = append(result.realised, RealisedResult{
			InstrumentID: trade.InstrumentID,
			Ticker:       trade.Ticker,
			Name:         trade.Name,
			Quantity:     quantity.String(),
			Proceeds:     proceeds.String(),
			Cost:         consumedCost.String(),
			Realised:     proceeds.Sub(consumedCost).String(),
			CostBasis:    CostBasisFIFO,
		})
	}

	for _, remaining := range open {
		result.quantity = result.quantity.Add(remaining.quantity)
		result.cost = result.cost.Add(remaining.cost)
	}
	if len(open) > 0 {
		result.costOpenedAt = open[0].date
	}
	return result, nil
}

// heldAfter is what would remain if these trades were the whole history. The service uses it to
// refuse a sale, and a correction, that would make a position negative — the refusal names this
// number, because "that is more than you hold" is only useful with the quantity attached.
func heldAfter(trades []Trade) (dec, error) {
	held := decZero
	for _, trade := range trades {
		quantity, err := parseDec(trade.Quantity)
		if err != nil {
			return decZero, err
		}
		if trade.Direction == DirectionBuy {
			held = held.Add(quantity)
		} else {
			held = held.Sub(quantity)
		}
	}
	return held, nil
}
