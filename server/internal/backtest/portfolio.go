package backtest

import "sort"

// The portfolio: cash, what is held, and what it was worth at each session.
//
// One rule governs the whole file. Cash plus the value of the positions equals the recorded equity
// at every session, not merely at the end — and when a position cannot be valued the equity states
// that it is incomplete rather than repeating yesterday's number. A curve that quietly carried a
// stale price forward would be smooth, plausible and wrong, and nothing downstream could tell.

type holding struct {
	quantity dec
	// signalSession is the signal that caused the position, kept so a sale can be attributed and
	// a held instrument can be recognised without a second lookup.
	signalSession SessionDate
}

type portfolio struct {
	cash     dec
	holdings map[UUID]*holding
}

func newPortfolio(startingCapital dec) *portfolio {
	return &portfolio{cash: startingCapital, holdings: map[UUID]*holding{}}
}

// heldOrder is every held instrument in a stable order. Iterating a Go map is deliberately
// randomised, so anything that walks the holdings and writes a row goes through here — otherwise
// two runs over identical data would write the same rows in different orders and a reader
// comparing them would see a difference that is not one.
func (p *portfolio) heldOrder() []UUID {
	identifiers := make([]UUID, 0, len(p.holdings))
	for id := range p.holdings {
		identifiers = append(identifiers, id)
	}
	sort.Slice(identifiers, func(i, j int) bool { return identifiers[i] < identifiers[j] })
	return identifiers
}

// valuation is one session's worth of the portfolio, and the position rows behind it.
type valuation struct {
	positions     []Position
	positionValue dec
	total         dec
	// investable is cash plus what could be valued. It is what a rebalance sizes against: a
	// position the simulation cannot price is not capital it can allocate.
	investable dec
	complete   bool
}

// value prices every holding at a session. A position it cannot price is recorded as unvalued
// with its reason, and that makes the session's equity incomplete.
func (e *engine) value(p *portfolio, session SessionDate) valuation {
	result := valuation{complete: true, positionValue: decZero}
	for _, id := range p.heldOrder() {
		held := p.holdings[id]
		member := e.in.byID[id]
		position := Position{RunID: e.runID, SessionDate: session, InstrumentID: id,
			Quantity: held.quantity.String()}

		price, priceSession, priced := e.priceAsOf(member, session)
		if !priced {
			reason := PositionNoPrice
			position.AbsenceReason = &reason
			result.positions = append(result.positions, position)
			result.complete = false
			continue
		}
		rate, converted, rated := e.rate(member.currency, priceSession)
		if !rated {
			reason := PositionNoRate
			position.AbsenceReason = &reason
			result.positions = append(result.positions, position)
			result.complete = false
			continue
		}

		value := toAccounting(price.Mul(held.quantity), rate, converted)
		priceText, sessionValue, valueText := price.String(), priceSession, value.String()
		position.Price, position.PriceSession, position.Value = &priceText, &sessionValue, &valueText
		if converted {
			rateText := rate.String()
			position.FXRate = &rateText
		}
		result.positions = append(result.positions, position)
		result.positionValue = result.positionValue.Add(value)
	}
	result.total = p.cash.Add(result.positionValue)
	result.investable = result.total
	return result
}

// equityPoint states the portfolio's value, or why it cannot be stated.
func (e *engine) equityPoint(session SessionDate, cash dec, valued valuation) EquityPoint {
	point := EquityPoint{RunID: e.runID, SessionDate: session, Cash: cash.String()}
	if !valued.complete {
		reason := EquityPositionUnvalued
		point.AbsenceReason = &reason
		return point
	}
	positionValue, total := valued.positionValue.String(), valued.total.String()
	point.PositionValue, point.Total = &positionValue, &total
	return point
}
