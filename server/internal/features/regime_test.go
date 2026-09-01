package features_test

import (
	"testing"

	"market-lens/server/internal/features"
)

var v1Thresholds = features.RegimeThresholds{
	VolatileAtLeast: "0.40", TrendingUpAbove: "0.05", DrawdownAbove: "-0.10", TrendingDownBelow: "-0.05",
}

func s(value string) *string { return &value }

func TestRegimeBoundariesAreExactlyAsStated(t *testing.T) {
	cases := []struct {
		name                        string
		volatility, trend, drawdown *string
		want                        string
	}{
		{"volatility at the boundary is volatile", s("0.400000000000"), s("0.000000000000"), s("0.000000000000"), "volatile"},
		{"a hair under the boundary is not", s("0.399999999999"), s("0.000000000000"), s("0.000000000000"), "range_bound"},
		{"trend at the boundary is not trending up", s("0.100000000000"), s("0.050000000000"), s("0.000000000000"), "range_bound"},
		{"a hair over the boundary is", s("0.100000000000"), s("0.050000000001"), s("0.000000000000"), "trending_up"},
		{"trending up needs the drawdown above its floor", s("0.100000000000"), s("0.200000000000"), s("-0.100000000000"), "range_bound"},
		{"a hair above the drawdown floor is trending up", s("0.100000000000"), s("0.200000000000"), s("-0.099999999999"), "trending_up"},
		{"trend at the lower boundary is not trending down", s("0.100000000000"), s("-0.050000000000"), s("-0.300000000000"), "range_bound"},
		{"a hair under the lower boundary is trending down", s("0.100000000000"), s("-0.050000000001"), s("-0.300000000000"), "trending_down"},
		{"volatile takes precedence over trending up", s("0.400000000000"), s("0.200000000000"), s("0.000000000000"), "volatile"},
		{"volatile takes precedence over trending down", s("0.900000000000"), s("-0.200000000000"), s("-0.500000000000"), "volatile"},
		{"trending up takes precedence over the drawdown alone", s("0.100000000000"), s("0.200000000000"), s("-0.050000000000"), "trending_up"},
	}
	for _, tc := range cases {
		label, reason := features.Regime(tc.volatility, tc.trend, tc.drawdown, v1Thresholds)
		if reason != "" || label != tc.want {
			t.Errorf("%s: %q (%q), expected %s", tc.name, label, reason, tc.want)
		}
	}
}

func TestRegimeWithAnyUndefinedInputDoesNotExist(t *testing.T) {
	for name, inputs := range map[string][3]*string{
		"no volatility": {nil, s("0.2"), s("0")},
		"no trend":      {s("0.1"), nil, s("0")},
		"no drawdown":   {s("0.1"), s("0.2"), nil},
	} {
		label, reason := features.Regime(inputs[0], inputs[1], inputs[2], v1Thresholds)
		if label != "" || reason != features.AbsenceInsufficientHistory {
			t.Errorf("%s: %q (%q), expected no label and %s", name, label, reason, features.AbsenceInsufficientHistory)
		}
	}
}

func TestRegimeMatchesTheGoldenLabels(t *testing.T) {
	golden := loadGoldenA(t)
	for _, position := range []int{319, 250} {
		want := goldenAt(t, golden, position)
		label, reason := features.Regime(want.Features["volatility_20"].Value, want.Features["trend_50_200"].Value,
			want.Features["drawdown_250"].Value, v1Thresholds)
		if reason != "" || want.Features["regime"].Label == nil || label != *want.Features["regime"].Label {
			t.Errorf("regime@%s = %q (%q), expected %+v", want.Note, label, reason, want.Features["regime"])
		}
	}
}
