package risk

import (
	"math/big"
	"sort"

	"market-lens/server/internal/decimal"
	"market-lens/server/internal/portfolio"
)

// Measuring a portfolio against the limits its owner wrote down.
//
// Nothing here values anything. It reads what feature 022 already derived, which is what keeps one
// implementation of "what is this worth" in the product — and means every honesty rule that feature
// established arrives here already correct rather than being reimplemented and quietly weakened.
//
// The rule that matters most: a limit is never reported within because something could not be
// measured. Every branch below either produces a figure or produces an absence with a reason, and
// there is no path that falls through to "within".

type dec = decimal.Dec

// group is one label's worth of a portfolio — an instrument, a sector or a market.
type group struct {
	label string
	value dec
}

// evaluate measures one limit against a portfolio.
func evaluate(limit Limit, view portfolio.View) (Evaluation, error) {
	threshold, err := decimal.ParseDec(limit.Threshold)
	if err != nil {
		return Evaluation{}, err
	}
	result := Evaluation{Kind: limit.Kind, Threshold: threshold.String(),
		Contributions: []Contribution{}}

	if len(view.Holdings) == 0 {
		// Nothing to measure is not compliance.
		reason := AbsenceNothingHeld
		result.State, result.AbsenceReason = StateUnevaluable, &reason
		return result, nil
	}

	if limit.Kind == KindHoldingCount {
		// Counting needs no price, so one unpriceable holding does not silence this limit. Silencing
		// it would be its own dishonesty: the person's holding count is perfectly well known.
		count := decimal.FromInt(bigFromInt(len(view.Holdings)))
		measured := count.String()
		result.Measured = &measured
		result.State = StateWithin
		if count.Cmp(threshold) > 0 {
			result.State = StateExceeded
		}
		return result, nil
	}

	// Every share divides by the portfolio's total, and feature 022 states that total only when
	// every holding could be valued. Dividing by it anyway would be arithmetic on a number the
	// product itself declines to state — and it would be wrong in the flattering direction, because
	// a smaller denominator makes every share look larger while a missing holding makes it smaller.
	if !view.Totals.Complete || view.Totals.Value == nil {
		reason := AbsencePortfolioIncomplete
		result.State, result.AbsenceReason = StateUnevaluable, &reason
		return result, nil
	}
	total, err := decimal.ParseDec(*view.Totals.Value)
	if err != nil {
		return Evaluation{}, err
	}
	if total.Sign() <= 0 {
		reason := AbsenceNothingHeld
		result.State, result.AbsenceReason = StateUnevaluable, &reason
		return result, nil
	}

	groups, err := groupBy(limit.Kind, view)
	if err != nil {
		return Evaluation{}, err
	}
	// Largest first. The whole breakdown is reported rather than only the offender: a reader then
	// sees where their money is, instead of being pointed at one holding to sell.
	sort.SliceStable(groups, func(i, j int) bool {
		if cmp := groups[i].value.Cmp(groups[j].value); cmp != 0 {
			return cmp > 0
		}
		return groups[i].label < groups[j].label
	})

	largest := decimal.Zero
	for index, item := range groups {
		share := item.value.Div(total)
		result.Contributions = append(result.Contributions, Contribution{
			Label: item.label, Value: item.value.String(), Share: share.String()})
		if index == 0 {
			largest = share
		}
	}

	denominator, measured := total.String(), largest.String()
	result.Denominator, result.Measured = &denominator, &measured
	result.State = StateWithin
	if largest.Cmp(threshold) > 0 {
		result.State = StateExceeded
	}
	return result, nil
}

// groupBy sums a portfolio's valued holdings under the labels a kind measures.
//
// Only holdings that could be valued reach here, because a portfolio with any unvalued holding is
// already unevaluable above. That is what makes the sum of the groups equal the total.
func groupBy(kind Kind, view portfolio.View) ([]group, error) {
	totals := map[string]dec{}
	order := []string{}
	for _, holding := range view.Holdings {
		if holding.Valuation.Value == nil {
			continue
		}
		value, err := decimal.ParseDec(*holding.Valuation.Value)
		if err != nil {
			return nil, err
		}
		label := labelFor(kind, holding)
		if _, seen := totals[label]; !seen {
			order = append(order, label)
			totals[label] = decimal.Zero
		}
		totals[label] = totals[label].Add(value)
	}

	groups := make([]group, 0, len(order))
	for _, label := range order {
		groups = append(groups, group{label: label, value: totals[label]})
	}
	return groups, nil
}

// labelFor names the group a holding belongs to.
//
// A sector of `unclassified` is a label like any other. Feature 014 made it an explicit value
// precisely so an instrument could not enter the universe with no classification state, and
// dropping those holdings here would shrink the denominator while leaving every other numerator
// alone — understating every sector's share and hiding the gap from somebody who could go and
// classify it.
func labelFor(kind Kind, holding portfolio.Holding) string {
	switch kind {
	case KindSectorShare:
		if holding.SectorName != "" {
			return holding.SectorName
		}
		return holding.Sector
	case KindMarketShare:
		return holding.MIC
	default:
		return holding.Ticker
	}
}

func bigFromInt(value int) *big.Int { return big.NewInt(int64(value)) }
