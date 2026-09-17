package backtest

import (
	"math/big"

	"market-lens/server/internal/decimal"
)

// The decimal arithmetic lives in its own package because two features need it: this one, valuing
// a simulated portfolio, and feature 022, valuing a person's real holdings. A second
// implementation would eventually disagree with this one, and the disagreement would surface as a
// backtest and a portfolio reporting different numbers for the same trade.
//
// The rules that matter are unchanged: every operation rounds to the stored precision immediately
// and the rounded value is carried forward, so stored figures reconcile exactly rather than nearly;
// and rounding is half-to-even, so a decade of trades is not biased in one direction.
type dec = decimal.Dec

const places = decimal.Places

var (
	decZero = decimal.Zero
	decOne  = decimal.One
)

func parseDec(value string) (dec, error) { return decimal.ParseDec(value) }
func mustDec(value string) dec           { return decimal.MustDec(value) }
func decFromInt(value *big.Int) dec      { return decimal.FromInt(value) }
func basisPoints(value dec) dec          { return decimal.BasisPoints(value) }
