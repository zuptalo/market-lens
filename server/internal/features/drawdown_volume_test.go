package features_test

import (
	"testing"

	"market-lens/server/internal/features"
)

func TestDrawdownIsZeroAtAPeakAndNegativeBelowIt(t *testing.T) {
	golden := loadGoldenA(t)
	bars := seriesA()
	for _, position := range []int{319, 250} {
		want := goldenAt(t, golden, position)
		expectValue(t, "drawdown_250@"+want.Note, features.Drawdown(closesOf(window(bars, position, 250))), "", want.Features["drawdown_250"])
	}
	if got := features.Drawdown([]float64{90, 95, 100, 120}); got != 0 {
		t.Errorf("drawdown at a new peak = %v, expected 0", got)
	}
	if got := features.Round(features.Drawdown([]float64{90, 120, 100, 96})); got != "-0.200000000000" {
		t.Errorf("drawdown below the peak = %s, expected -0.200000000000", got)
	}
	if got := features.Drawdown([]float64{100, 100, 100}); got != 0 {
		t.Errorf("drawdown on a flat series = %v, expected 0, never positive", got)
	}
}

func TestVolumeFeaturesTreatZeroVolumeAsAnObservation(t *testing.T) {
	golden := loadGoldenA(t)
	bars := seriesA()
	for _, position := range []int{319, 250, 100} {
		want := goldenAt(t, golden, position)
		sma := features.VolumeSMA(window(bars, position, 20))
		expectValue(t, "volume_sma_20@"+want.Note, sma, "", want.Features["volume_sma_20"])
		ratio, reason := features.VolumeRatio(bars[position].Volume, sma)
		expectValue(t, "volume_ratio_20@"+want.Note, ratio, reason, want.Features["volume_ratio_20"])
	}
	// The fixture's zero-volume session is an observation: the ratio is 0, not undefined.
	zeroVolumePosition := fixtureASessions - 1 - fixtureZeroVolumeOffset
	if bars[zeroVolumePosition].Volume != 0 {
		t.Fatalf("position %d should be the zero-volume session", zeroVolumePosition)
	}
	ratio, reason := features.VolumeRatio(0, features.VolumeSMA(window(bars, zeroVolumePosition, 20)))
	if reason != "" || ratio != 0 {
		t.Errorf("ratio on a zero-volume bar = %v (%q), expected 0 and no reason", ratio, reason)
	}
	// Twenty zero-volume sessions have nothing to divide by.
	if _, reason := features.VolumeRatio(0, 0); reason != features.AbsenceZeroDenominator {
		t.Errorf("ratio over a zero average: %q, expected %s", reason, features.AbsenceZeroDenominator)
	}
}

func TestAMissingSessionInsideAVolumeWindowIsAGapNotZero(t *testing.T) {
	open := features.SessionOpen
	statuses := make([]features.SessionStatus, 25)
	for index := range statuses {
		statuses[index] = open
	}
	cal := calendar(statuses...)
	all := dates(cal, open)
	withGap := append(append([]features.SessionDate{}, all[:15]...), all[16:]...)
	if _, reason := features.Window(barsOn(withGap...), cal, cal[24].Date, 20); reason != features.AbsenceWindowGap {
		t.Errorf("reason %q, expected %s", reason, features.AbsenceWindowGap)
	}
}
