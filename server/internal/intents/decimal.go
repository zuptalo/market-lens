package intents

import "market-lens/server/internal/decimal"

// The shared numeric kernel, as features 021, 022 and 023 use. A second implementation would
// eventually disagree with the limits screen about the same portfolio.
type dec = decimal.Dec

var (
	decZero = decimal.Zero
	decOne  = decimal.One
)

func parseDec(value string) (dec, error) { return decimal.ParseDec(value) }
