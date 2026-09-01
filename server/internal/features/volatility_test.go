package features_test

import (
	"math"
	"testing"

	"market-lens/server/internal/features"
)

// listingVolatility is the stated evaluation order of the adopted definition, written out
// independently of the implementation: twenty log ratios in ascending session order, the
// sample standard deviation, times the square root of 252 — what listing.go's CTE computes
// as stddev_samp(ln(close / lag(close))) * sqrt(252).
func listingVolatility(closes []float64) float64 {
	ratios := make([]float64, 0, len(closes)-1)
	for index := 1; index < len(closes); index++ {
		ratios = append(ratios, math.Log(closes[index]/closes[index-1]))
	}
	var total float64
	for _, ratio := range ratios {
		total += ratio
	}
	mean := total / float64(len(ratios))
	var squares float64
	for _, ratio := range ratios {
		deviation := ratio - mean
		squares += deviation * deviation
	}
	return math.Sqrt(squares/float64(len(ratios)-1)) * math.Sqrt(252)
}

func TestVolatilityReproducesTheListingsDefinitionAndTheGoldenValues(t *testing.T) {
	golden := loadGoldenA(t)
	bars := seriesA()
	for _, position := range []int{319, 250, 100} {
		want := goldenAt(t, golden, position)
		closes := closesOf(window(bars, position, 21))
		got := features.Volatility(closes)
		expectValue(t, "volatility_20@"+want.Note, got, "", want.Features["volatility_20"])
		if independent := features.Round(listingVolatility(closes)); independent != features.Round(got) {
			t.Errorf("volatility_20@%s: engine %s, listing arithmetic %s", want.Note, features.Round(got), independent)
		}
	}
}

func TestVolatilityNeedsExactlyTwentyOneSessions(t *testing.T) {
	open := features.SessionOpen
	statuses := make([]features.SessionStatus, 30)
	for index := range statuses {
		statuses[index] = open
	}
	cal := calendar(statuses...)
	all := dates(cal, open)
	twentyOne := barsOn(all[9:]...)
	if got, reason := features.Window(twentyOne, cal, cal[29].Date, 21); reason != "" || len(got) != 21 {
		t.Errorf("21 stored sessions: %d bars, reason %q", len(got), reason)
	}
	twenty := barsOn(all[10:]...)
	if _, reason := features.Window(twenty, cal, cal[29].Date, 21); reason != features.AbsenceInsufficientHistory {
		t.Errorf("20 stored sessions: reason %q, expected %s", reason, features.AbsenceInsufficientHistory)
	}
}

func TestAverageTrueRangeMatchesTheGoldenValuesAndReadsThePriorClose(t *testing.T) {
	golden := loadGoldenA(t)
	bars := seriesA()
	for _, position := range []int{319, 250, 100} {
		want := goldenAt(t, golden, position)
		expectValue(t, "atr_14@"+want.Note, features.ATR(window(bars, position, 15)), "", want.Features["atr_14"])
	}
	// A session that gapped down: its own range is 5, but the distance from the prior close
	// to its low is 15, and the true range is the larger.
	gapped := []features.Bar{
		{Session: "a", Open: 100, High: 101, Low: 99, Close: 100},
		{Session: "b", Open: 89, High: 90, Low: 85, Close: 88},
	}
	if got := features.ATR(gapped); got != 15 {
		t.Errorf("ATR over a gapped session = %v, expected 15 (low to prior close)", got)
	}
}
