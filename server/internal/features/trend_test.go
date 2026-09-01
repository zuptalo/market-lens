package features_test

import (
	"testing"

	"market-lens/server/internal/features"
)

func TestMovingAveragesTrendAndMomentumMatchTheGoldenValues(t *testing.T) {
	golden := loadGoldenA(t)
	bars := seriesA()
	for _, position := range []int{319, 250} {
		want := goldenAt(t, golden, position)
		sma20 := features.SMA(closesOf(window(bars, position, 20)))
		sma50 := features.SMA(closesOf(window(bars, position, 50)))
		sma200 := features.SMA(closesOf(window(bars, position, 200)))
		expectValue(t, "sma_20@"+want.Note, sma20, "", want.Features["sma_20"])
		expectValue(t, "sma_50@"+want.Note, sma50, "", want.Features["sma_50"])
		expectValue(t, "sma_200@"+want.Note, sma200, "", want.Features["sma_200"])
		trend, reason := features.Trend(sma50, sma200)
		expectValue(t, "trend_50_200@"+want.Note, trend, reason, want.Features["trend_50_200"])
		momentum, reason := features.Momentum(bars[position].Close, sma20)
		expectValue(t, "momentum_20@"+want.Note, momentum, reason, want.Features["momentum_20"])
	}
	// Inside the warm-up the shorter averages exist and the 200 does not; that is the
	// window's verdict, not the average's, so it is asserted through Window.
	warmup := goldenAt(t, golden, 100)
	if warmup.Features["sma_200"].AbsenceReason == nil || *warmup.Features["sma_200"].AbsenceReason != "insufficient_history" {
		t.Fatalf("golden at position 100 should hold sma_200 undefined, got %+v", warmup.Features["sma_200"])
	}
	expectValue(t, "sma_50@"+warmup.Note, features.SMA(closesOf(window(bars, 100, 50))), "", warmup.Features["sma_50"])
}

func TestTrendAndMomentumOverAZeroAverageAreUndefined(t *testing.T) {
	if _, reason := features.Trend(1, 0); reason != features.AbsenceZeroDenominator {
		t.Errorf("Trend over a zero slow average: %q", reason)
	}
	if _, reason := features.Momentum(1, 0); reason != features.AbsenceZeroDenominator {
		t.Errorf("Momentum over a zero average: %q", reason)
	}
}
