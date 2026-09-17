package portfolio

import "market-lens/server/internal/decimal"

// The arithmetic is the shared kernel, not a second copy.
//
// A portfolio and a backtest value the same instruments with the same rules — round to the stored
// precision at every step, carry the rounded value forward, round half to even — and two
// implementations of that would eventually disagree about the same trade.
type dec = decimal.Dec

var (
	decZero = decimal.Zero
	decOne  = decimal.One
)

func parseDec(value string) (dec, error) { return decimal.ParseDec(value) }

func mustDec(value string) dec { return decimal.MustDec(value) }
