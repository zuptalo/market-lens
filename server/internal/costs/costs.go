// Package costs prices one execution: what it costs to trade, and what that does to cash.
//
// It exists in its own package because three features need the same answer — feature 021 valuing a
// simulated run, feature 026 filling a paper order, and any future one that executes anything. A
// second implementation would eventually disagree with this one about a single trade, and the
// disagreement would surface as two screens reporting different figures for the same execution,
// which is the failure this codebase has refused at every turn.
//
// Two rules are load-bearing and neither is obvious:
//
//   - Every intermediate is rounded to the stored precision before the next step reads it, so
//     stored figures reconcile with each other exactly rather than nearly.
//   - Conversion divides by the accounting currency's stored rate rather than multiplying by an
//     inverse nobody stored, so the two directions cannot disagree.
package costs

import "market-lens/server/internal/decimal"

type dec = decimal.Dec

// Direction is which way the execution goes. Costs are charged the same either way; what differs
// is which side of the consideration they fall on.
type Direction string

const (
	Buy  Direction = "buy"
	Sell Direction = "sell"
)

// Model is the stated cost of trading, as rates rather than as amounts.
//
// Rates rather than amounts because the same model has to price a hundred-share order and a
// hundred-thousand-share one. The brokerage minimum is the one amount, because a broker's floor is
// an amount.
type Model struct {
	Slippage         dec
	Spread           dec
	BrokerageRate    dec
	BrokerageMinimum dec
}

// FromBasisPoints builds a model from the four figures a person states, each in basis points except
// the brokerage minimum, which is money.
func FromBasisPoints(slippageBps, spreadBps, brokerageBps, brokerageMinimum dec) Model {
	return Model{
		Slippage:         decimal.BasisPoints(slippageBps),
		Spread:           decimal.BasisPoints(spreadBps),
		BrokerageRate:    decimal.BasisPoints(brokerageBps),
		BrokerageMinimum: brokerageMinimum,
	}
}

// Execution is what one trade cost and what it did to cash.
type Execution struct {
	// Gross is the consideration in the accounting currency, before any cost.
	Gross     dec
	Slippage  dec
	Spread    dec
	Brokerage dec
	// CashEffect is negative for a buy and positive for a sale: the consideration and every cost
	// together, so a caller cannot forget one.
	CashEffect dec
}

// Total is what the execution cost, which is every component except the consideration itself.
func (e Execution) Total() dec {
	return e.Slippage.Add(e.Spread).Add(e.Brokerage)
}

// ToAccounting converts a listing-currency amount into the accounting currency.
//
// Divides by the stored rate rather than multiplying by an inverse nobody stored, so a conversion
// and its reverse cannot disagree at the twelfth decimal place.
func ToAccounting(amount, rate dec, converted bool) dec {
	if !converted {
		return amount
	}
	return amount.Div(rate)
}

// Price prices one execution of `quantity` at `price` in the instrument's own currency.
//
// `rate` is the accounting currency's stored rate for the listing currency, and `converted` says
// whether the two differ at all — passing a rate of one and `converted` true would charge a
// currency spread on a trade that never crossed a currency.
func Price(model Model, direction Direction, quantity, price, rate dec, converted bool) Execution {
	grossListing := price.Mul(quantity)
	slippageListing := grossListing.Mul(model.Slippage)
	gross := ToAccounting(grossListing, rate, converted)
	slippage := ToAccounting(slippageListing, rate, converted)

	var spread dec
	if converted {
		// The spread is charged on what actually crossed the currency, which is the consideration
		// plus or minus the slippage rather than the headline amount.
		base := gross.Add(slippage)
		if direction == Sell {
			base = gross.Sub(slippage)
		}
		spread = base.Mul(model.Spread)
	}
	brokerage := gross.Mul(model.BrokerageRate).Max(model.BrokerageMinimum)

	execution := Execution{Gross: gross, Slippage: slippage, Spread: spread, Brokerage: brokerage}
	if direction == Buy {
		execution.CashEffect = gross.Add(slippage).Add(spread).Add(brokerage).Neg()
	} else {
		execution.CashEffect = gross.Sub(slippage).Sub(spread).Sub(brokerage)
	}
	return execution
}
