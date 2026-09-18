package intents

import (
	"context"

	"market-lens/server/internal/portfolio"
	"market-lens/server/internal/risk"
)

// What an intent would do.
//
// The proposed change is applied to a copy of the portfolio's holdings, and feature 023's existing
// evaluator is run over the result. That reuse is the point: a second implementation of "what share
// would this be" would eventually disagree with the limits screen about the same portfolio, and the
// unevaluable rule — a holding that cannot be priced makes a share unevaluable — arrives here
// already correct rather than being written a second time and quietly weakened.

// Valuer prices a hypothetical quantity through the portfolio's own path, so an intent in something
// not yet held still reports a share rather than a shrug.
type Valuer interface {
	ValueOf(ctx context.Context, userID string, instrumentID UUID, quantity string) (portfolio.Valuation, error)
}

func consequenceOf(ctx context.Context, userID string, intent Intent, view portfolio.View,
	limits []risk.Limit, valuer Valuer) (Consequence, error) {
	quantity, err := parseDec(intent.Quantity)
	if err != nil {
		return Consequence{}, err
	}
	held := decZero
	var existing *portfolio.Holding
	for index := range view.Holdings {
		if view.Holdings[index].InstrumentID == intent.InstrumentID {
			existing = &view.Holdings[index]
			if held, err = parseDec(existing.Quantity); err != nil {
				return Consequence{}, err
			}
			break
		}
	}

	resulting := held.Add(quantity)
	if intent.Direction == DirectionSell {
		resulting = held.Sub(quantity)
	}
	consequence := Consequence{ResultingQuantity: resulting.String()}

	// What the resulting position would be worth, priced by the portfolio's own valuation path —
	// the instrument's latest stored close, converted into the accounting currency. Never the
	// person's expected price: that is their guess, and valuing a position at it would put a number
	// in the figures that nobody quoted.
	if resulting.Sign() > 0 {
		valuation, err := valuer.ValueOf(ctx, userID, intent.InstrumentID, resulting.String())
		if err != nil {
			return Consequence{}, err
		}
		consequence.ResultingValue = valuation.Value
	}

	// The holdings as this intent would leave them.
	hypothetical := view
	hypothetical.Holdings = applyTo(view.Holdings, intent, resulting, consequence.ResultingValue)

	if !view.Totals.Complete || view.Totals.Value == nil {
		reason := AbsencePortfolioIncomplete
		consequence.AbsenceReason = &reason
	} else if consequence.ResultingValue == nil {
		// Nothing stored prices this instrument, so its share cannot be stated. The intent is still
		// recorded and its resulting quantity still reported.
		reason := AbsenceNoPrice
		consequence.AbsenceReason = &reason
	} else {
		total, err := totalOf(hypothetical)
		if err != nil {
			return Consequence{}, err
		}
		if total.Sign() > 0 {
			value, err := parseDec(*consequence.ResultingValue)
			if err != nil {
				return Consequence{}, err
			}
			share := value.Div(total)
			rendered, denominator := share.String(), total.String()
			consequence.ResultingShare, consequence.Denominator = &rendered, &denominator
		}
	}

	evaluations, err := risk.EvaluateAgainst(limits, hypothetical)
	if err != nil {
		return Consequence{}, err
	}
	consequence.Limits = evaluations
	return consequence, nil
}

// applyTo returns the holdings an intent would leave behind: the affected one adjusted, removed
// when nothing would remain, or added when the person holds none yet.
func applyTo(holdings []portfolio.Holding, intent Intent, resulting dec, value *string) []portfolio.Holding {
	applied := make([]portfolio.Holding, 0, len(holdings)+1)
	var found bool
	for _, holding := range holdings {
		if holding.InstrumentID != intent.InstrumentID {
			applied = append(applied, holding)
			continue
		}
		found = true
		if resulting.Sign() <= 0 {
			continue
		}
		scaled := holding
		scaled.Quantity = resulting.String()
		if holding.Valuation.Value != nil {
			if value, err := scaleValue(*holding.Valuation.Value, holding.Quantity, resulting); err == nil {
				scaled.Valuation.Value = &value
			}
		}
		applied = append(applied, scaled)
	}
	if !found && resulting.Sign() > 0 {
		// A position the person does not hold yet, valued by the caller at the product's own stored
		// close so that limits measured against the portfolio total include it.
		holding := portfolio.Holding{
			InstrumentID: intent.InstrumentID, Ticker: intent.Ticker, Name: intent.Name,
			Currency: intent.Currency, Quantity: resulting.String(),
		}
		holding.Valuation.Value = value
		applied = append(applied, holding)
	}
	return applied
}

func scaleValue(value, from string, to dec) (string, error) {
	current, err := parseDec(value)
	if err != nil {
		return "", err
	}
	held, err := parseDec(from)
	if err != nil || held.Sign() <= 0 {
		return "", err
	}
	return current.Div(held).Mul(to).String(), nil
}

// totalOf sums the valued holdings of a hypothetical portfolio. Only holdings the portfolio could
// value contribute, which is the same rule that makes a real total incomplete.
func totalOf(view portfolio.View) (dec, error) {
	total := decZero
	for _, holding := range view.Holdings {
		if holding.Valuation.Value == nil {
			continue
		}
		value, err := parseDec(*holding.Valuation.Value)
		if err != nil {
			return decZero, err
		}
		total = total.Add(value)
	}
	return total, nil
}

var _ = decOne
