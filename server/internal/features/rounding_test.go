package features_test

import (
	"math"
	"math/big"
	"math/rand"
	"strconv"
	"testing"

	"market-lens/server/internal/features"
)

// Reproducibility is defined at the stored precision: twelve decimal places, half-to-even,
// applied to the shortest decimal that round-trips the float64 (research R-001). The ties
// below are ties of that decimal, which is what makes "half-to-even" a testable statement
// about a binary float.
func TestRoundIsHalfToEvenAtTwelvePlaces(t *testing.T) {
	cases := []struct {
		name  string
		value float64
		want  string
	}{
		{"tie rounds down to even", 0.0000000000005, "0.000000000000"},
		{"tie rounds up to even", 0.0000000000015, "0.000000000002"},
		{"negative tie rounds to even", -0.0000000000025, "-0.000000000002"},
		{"already at twelve places is unchanged", 0.123456789012, "0.123456789012"},
		{"a third rounds to the nearest twelfth place", 1.0 / 3, "0.333333333333"},
		{"two thirds rounds up", 2.0 / 3, "0.666666666667"},
		{"a short decimal is padded, not extended with binary noise", 0.1, "0.100000000000"},
		{"a price-scale value keeps its integer part", 182.4475, "182.447500000000"},
		{"below the twelfth place vanishes without a sign", -0.0000000000001, "0.000000000000"},
		{"negative zero is zero", -0.0, "0.000000000000"},
		{"an integer", 100, "100.000000000000"},
	}
	for _, tc := range cases {
		if got := features.Round(tc.value); got != tc.want {
			t.Errorf("%s: Round(%v) = %q, expected %q", tc.name, tc.value, got, tc.want)
		}
	}
}

// referenceRound is the rational-arithmetic rounding the engine used first, kept here as the
// oracle the digit implementation must agree with on every input.
func referenceRound(value float64) string {
	shortest := strconv.FormatFloat(value, 'f', -1, 64)
	rounded, _ := new(big.Rat).SetString(shortest)
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(features.Places), nil)
	scaled := new(big.Rat).Mul(rounded, new(big.Rat).SetInt(scale))
	quotient, remainder := new(big.Int).QuoRem(scaled.Num(), scaled.Denom(), new(big.Int))
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
	return new(big.Rat).SetFrac(quotient, scale).FloatString(features.Places)
}

func TestRoundAgreesWithRationalArithmeticEverywhere(t *testing.T) {
	random := rand.New(rand.NewSource(13))
	values := []float64{0, -0.0, 1, -1, 0.5, 9.9999999999995, -9.9999999999995, 0.9999999999999, 99999.9999999999996,
		1e-13, -1e-13, 5e-13, 1.5e-12, 2.5e-12, 123456789.123456789012345, 1e15, -1e15 + 0.3}
	for range 20000 {
		magnitude := math.Pow(10, float64(random.Intn(24)-16))
		values = append(values, (random.Float64()*2-1)*magnitude)
	}
	for range 2000 {
		// Values near a twelfth-place tie, where the two implementations could disagree.
		units := float64(random.Int63n(1_000_000_000_000))
		values = append(values, units/1e12+5e-13, -(units/1e12 + 5e-13), units/1e12+4.9999e-13)
	}
	for _, value := range values {
		if got, want := features.Round(value), referenceRound(value); got != want {
			t.Fatalf("Round(%v) = %q, the rational reference says %q", value, got, want)
		}
	}
}
