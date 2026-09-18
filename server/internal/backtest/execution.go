package backtest

import (
	"sort"

	"market-lens/server/internal/costs"
)

// The execution rules.
//
// A signal as of a session is computed from that session's close. Executing at that close would
// assume somebody acted on a price they only learned when the session ended, which is the single
// easiest way to make a backtest look good and is invisible in the resulting chart. So execution
// happens at the *open* of the next session the instrument actually traded — the earliest price a
// person could really have paid — and the database refuses any row that says otherwise.

// nextTradedSession is the first session strictly after `after` on which this instrument traded,
// within the configuration's give-up window.
//
// The window matters: without it an instrument that stopped trading in 2018 would fill at its
// next quoted price in 2019 as though the intervening year had not happened.
func (e *engine) nextTradedSession(member *instrument, after SessionDate) (SessionDate, SkipReason, bool) {
	position := e.in.index[after]
	if position+1 >= len(e.in.calendar) {
		return "", SkipNoNextSession, false
	}
	found := sort.Search(len(member.traded), func(i int) bool { return member.traded[i] > after })
	if found == len(member.traded) {
		return "", SkipNotExecutable, false
	}
	candidate := member.traded[found]
	if e.in.index[candidate]-position > e.configuration.GiveUpSessions {
		return "", SkipNotExecutable, false
	}
	return candidate, "", true
}

// priceAsOf is the close the simulation values a holding at: this session's, or the most recent
// one within the give-up window when the instrument's market was closed while another was open.
//
// Beyond the window the instrument has stopped trading and the position is unvalued instead.
// Carrying a stale price forward indefinitely would be the product asserting something it stopped
// knowing, which is exactly what FR-011 forbids.
func (e *engine) priceAsOf(member *instrument, session SessionDate) (dec, SessionDate, bool) {
	if price, traded := member.closes[session]; traded {
		return price, session, true
	}
	position := e.in.index[session]
	found := sort.Search(len(member.traded), func(i int) bool { return member.traded[i] > session })
	if found == 0 {
		return dec{}, "", false
	}
	candidate := member.traded[found-1]
	if position-e.in.index[candidate] > e.configuration.GiveUpSessions {
		return dec{}, "", false
	}
	return member.closes[candidate], candidate, true
}

// rate is the accounting currency's rate against a listing currency as of a session.
//
// A single-currency backtest never reaches the second branch: it requires no rate and performs no
// conversion, which is FR-013 and is why the returned flag exists at all.
func (e *engine) rate(currency string, session SessionDate) (value dec, converted bool, ok bool) {
	if currency == e.configuration.AccountingCurrency {
		return decOne, false, true
	}
	quoted, exists := e.in.rates[currency][session]
	if !exists {
		return dec{}, true, false
	}
	return quoted, true, true
}

// The cost arithmetic lives in internal/costs, because feature 026 fills a paper order with the
// same model and a second implementation would eventually disagree with this one about one trade.
// The wrappers below keep this package's own vocabulary.
func toAccounting(amount, rate dec, converted bool) dec {
	return costs.ToAccounting(amount, rate, converted)
}

type executionCosts struct {
	grossAccounting dec
	slippage        dec
	spread          dec
	brokerage       dec
	cashEffect      dec
}

func (e *engine) costsOf(direction Direction, quantity, price, rate dec, converted bool) executionCosts {
	priced := costs.Price(costs.Model{
		Slippage: e.slippage, Spread: e.spread,
		BrokerageRate: e.brokerageRate, BrokerageMinimum: e.brokerageMinimum,
	}, costs.Direction(direction), quantity, price, rate, converted)
	return executionCosts{
		grossAccounting: priced.Gross, slippage: priced.Slippage, spread: priced.Spread,
		brokerage: priced.Brokerage, cashEffect: priced.CashEffect,
	}
}

// affordableQuantity is how many whole shares a budget buys once every stated cost is allowed for.
//
// Whole shares, not fractions: nobody can buy 3.7 shares of a Nordic large cap, and allowing it
// would quietly remove the reason a portfolio cannot always hold N equal positions. The estimate
// is deliberately conservative — it charges the brokerage rate per share *and* reserves the
// minimum — so the exact costs computed afterwards can only come in under the budget.
func (e *engine) affordableQuantity(budget, price, rate dec, converted bool) dec {
	perShare := price.Add(price.Mul(e.slippage))
	perShare = toAccounting(perShare, rate, converted)
	if converted {
		perShare = perShare.Add(perShare.Mul(e.spread))
	}
	perShare = perShare.Add(perShare.Mul(e.brokerageRate))
	if perShare.Sign() <= 0 {
		return decZero
	}
	available := budget.Sub(e.brokerageMinimum)
	if available.Sign() <= 0 {
		return decZero
	}
	return decFromInt(available.Div(perShare).Floor())
}
