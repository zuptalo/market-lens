package features_test

import (
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
