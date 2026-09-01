package features

import (
	"math/big"
	"strconv"
)

// Places is the stored precision of every numeric feature value: numeric(24,12).
const Places = 12

// Round renders a computed float64 as the twelve-place decimal string the store holds,
// rounding half-to-even (research R-001). The rounding is applied to the shortest decimal
// that round-trips the float64 — the number the computation actually produced — never to its
// full binary expansion, so 1.5e-12 is a true tie and rounds to 0.000000000002.
func Round(value float64) string {
	shortest := strconv.FormatFloat(value, 'f', -1, 64)
	rounded := new(big.Rat)
	if _, ok := rounded.SetString(shortest); !ok {
		// Only NaN and the infinities reach here; the callers report them as absences
		// before storing anything, so a panic is the correct response to a caller that did not.
		panic("features: Round of a non-finite value " + shortest)
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(Places), nil)
	scaled := new(big.Rat).Mul(rounded, new(big.Rat).SetInt(scale))
	quotient, remainder := new(big.Int).QuoRem(scaled.Num(), scaled.Denom(), new(big.Int))
	// QuoRem truncates toward zero; decide the last unit from twice the remainder.
	twice := new(big.Int).Mul(remainder.Abs(remainder), big.NewInt(2))
	switch twice.Cmp(scaled.Denom()) {
	case 1:
		if scaled.Sign() < 0 {
			quotient.Sub(quotient, big.NewInt(1))
		} else {
			quotient.Add(quotient, big.NewInt(1))
		}
	case 0:
		if quotient.Bit(0) == 1 {
			if scaled.Sign() < 0 {
				quotient.Sub(quotient, big.NewInt(1))
			} else {
				quotient.Add(quotient, big.NewInt(1))
			}
		}
	}
	return new(big.Rat).SetFrac(quotient, scale).FloatString(Places)
}
