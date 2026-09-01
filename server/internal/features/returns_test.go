package features_test

import (
	"math"
	"testing"

	"market-lens/server/internal/features"
)

func TestReturnsMatchTheGoldenValues(t *testing.T) {
	golden := loadGoldenA(t)
	bars := seriesA()
	for _, position := range []int{319, 250} {
		want := goldenAt(t, golden, position)
		for name, lookback := range map[string]int{
			"return_1": 1, "return_5": 5, "return_20": 20, "return_60": 60, "return_90": 90, "return_250": 250,
		} {
			got, reason := features.Return(closesOf(window(bars, position, lookback+1)))
			expectValue(t, name+"@"+want.Note, got, reason, want.Features[name])
		}
		got, reason := features.LogReturn(closesOf(window(bars, position, 2)))
		expectValue(t, "log_return_1@"+want.Note, got, reason, want.Features["log_return_1"])
	}
}

// The adopted definitions must reproduce what the Markets listing computed before the engine
// existed: latest_close / close_20 - 1 over the same closes, which for the exploration
// fixture's arithmetic series is what TestListingReturnsLatestCloseChangeAndFreshness
// asserts in server/internal/instruments/listing_test.go.
func TestAdoptedRawReturnsReproduceTheListingsArithmetic(t *testing.T) {
	const sessions = 300
	closes := make([]float64, sessions)
	for index := range closes {
		closes[index] = 100 + float64(index)*0.25 + 0.5
	}
	last := 100.0 + float64(sessions-1)*0.25 + 0.5
	for _, lookback := range []int{20, 90} {
		back := last - float64(lookback)*0.25
		want := features.Round(last/back - 1)
		got, reason := features.Return(closes[sessions-lookback-1:])
		if reason != "" || features.Round(got) != want {
			t.Errorf("return_%d = %s (%q), expected %s", lookback, features.Round(got), reason, want)
		}
	}
}

func TestAReturnOverAZeroPriorCloseIsUndefinedNotInfinite(t *testing.T) {
	got, reason := features.Return([]float64{0, 5})
	if reason != features.AbsenceZeroDenominator || !math.IsNaN(got) && got != 0 {
		t.Errorf("Return over a zero prior close = %v (%q), expected %s", got, reason, features.AbsenceZeroDenominator)
	}
	if _, reason := features.LogReturn([]float64{0, 5}); reason != features.AbsenceZeroDenominator {
		t.Errorf("LogReturn over a zero prior close: %q, expected %s", reason, features.AbsenceZeroDenominator)
	}
}
