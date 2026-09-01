package features_test

import (
	"testing"

	"market-lens/server/internal/features"
)

func TestRSIMatchesTheGoldenValuesAndItsBoundaries(t *testing.T) {
	golden := loadGoldenA(t)
	bars := seriesA()
	for _, position := range []int{319, 250} {
		want := goldenAt(t, golden, position)
		expectValue(t, "rsi_14@"+want.Note, features.RSI(closesOf(window(bars, position, 140)), 14), "", want.Features["rsi_14"])
	}
	rising := make([]float64, 140)
	falling := make([]float64, 140)
	for index := range rising {
		rising[index] = 100 + float64(index)
		falling[index] = 300 - float64(index)
	}
	if got := features.RSI(rising, 14); got != 100 {
		t.Errorf("RSI of a monotonically rising series = %v, expected 100", got)
	}
	if got := features.RSI(falling, 14); got != 0 {
		t.Errorf("RSI of a monotonically falling series = %v, expected 0", got)
	}
}

// The fixed window is what makes the oscillators independent of where history starts
// (research R-009): the value at a session is the same whether or not earlier history exists.
func TestOscillatorsAreIndependentOfHistoryBeforeTheirWindow(t *testing.T) {
	bars := seriesA()
	full := closesOf(window(bars, 319, 140))
	truncated := closesOf(window(bars[100:], 219, 140))
	if features.RSI(full, 14) != features.RSI(truncated, 14) {
		t.Error("RSI changed when history before the window was removed")
	}
	fullLine, fullSignal, fullHistogram := features.MACD(closesOf(window(bars, 319, 130)), 12, 26, 9)
	line, signal, histogram := features.MACD(closesOf(window(bars[100:], 219, 130)), 12, 26, 9)
	if fullLine != line || fullSignal != signal || fullHistogram != histogram {
		t.Error("MACD changed when history before the window was removed")
	}
}

func TestMACDMatchesTheGoldenValues(t *testing.T) {
	golden := loadGoldenA(t)
	bars := seriesA()
	for _, position := range []int{319, 250} {
		want := goldenAt(t, golden, position)
		line, signal, histogram := features.MACD(closesOf(window(bars, position, 130)), 12, 26, 9)
		expectValue(t, "macd_12_26@"+want.Note, line, "", want.Features["macd_12_26"])
		expectValue(t, "macd_signal_9@"+want.Note, signal, "", want.Features["macd_signal_9"])
		expectValue(t, "macd_histogram@"+want.Note, histogram, "", want.Features["macd_histogram"])
	}
}

func TestOscillatorWindowsOneSessionShortAreInsufficientHistory(t *testing.T) {
	open := features.SessionOpen
	statuses := make([]features.SessionStatus, 150)
	for index := range statuses {
		statuses[index] = open
	}
	cal := calendar(statuses...)
	all := dates(cal, open)
	for _, size := range []int{140, 130} {
		short := barsOn(all[150-size+1:]...)
		if _, reason := features.Window(short, cal, cal[149].Date, size); reason != features.AbsenceInsufficientHistory {
			t.Errorf("%d bars for a window of %d: reason %q", size-1, size, reason)
		}
		exact := barsOn(all[150-size:]...)
		if got, reason := features.Window(exact, cal, cal[149].Date, size); reason != "" || len(got) != size {
			t.Errorf("%d bars for a window of %d: %d bars, reason %q", size, size, len(got), reason)
		}
	}
}
