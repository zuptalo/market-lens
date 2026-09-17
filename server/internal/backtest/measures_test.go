package backtest

import (
	"math"
	"testing"
)

// The measures are tested against series with answers worked out by hand, because the one thing a
// measure cannot do is be checked against the code that produced it.

func curveOf(values ...float64) []point {
	// Sessions a week apart so the annualisation has a real span to work with.
	days := []string{"2026-01-05", "2026-01-12", "2026-01-19", "2026-01-26", "2026-02-02",
		"2026-02-09", "2026-02-16", "2026-02-23"}
	curve := make([]point, 0, len(values))
	for index, value := range values {
		curve = append(curve, point{session: SessionDate(days[index%len(days)]), value: value})
	}
	return curve
}

func newEngine() *engine {
	return &engine{configuration: Configuration{GiveUpSessions: 5}, in: &inputs{index: map[SessionDate]int{}}}
}

// TestAllSixMeasuresArePresent is FR-018. A result free to report a subset would report the
// flattering one, and the two that flatter least are exactly the two a curve makes easy to omit.
func TestAllSixMeasuresArePresent(t *testing.T) {
	measures := newEngine().measureSeries(MeasureSubjectStrategy, nil,
		curveOf(100, 110, 105, 120), 4, mustDec("37.5"))

	if measures.AbsenceReason != nil {
		t.Fatalf("a measurable curve reported an absence: %s", *measures.AbsenceReason)
	}
	for name, value := range map[string]*string{
		"total return":      measures.TotalReturn,
		"annualised return": measures.AnnualisedReturn,
		"volatility":        measures.Volatility,
		"maximum drawdown":  measures.MaxDrawdown,
		"total costs":       measures.TotalCosts,
	} {
		if value == nil {
			t.Errorf("%s was not reported", name)
		}
	}
	if measures.TradeCount == nil || *measures.TradeCount != 4 {
		t.Errorf("the trade count was not reported")
	}
	if got := *measures.TotalReturn; got != "0.200000000000" {
		t.Errorf("total return is %s, want 0.200000000000", got)
	}
	if got := *measures.TotalCosts; got != "37.500000000000" {
		t.Errorf("total costs are %s, want 37.500000000000", got)
	}
}

// TestMaximumDrawdownIsTheWorstPeakToTrough is the measure most easily computed in a flattering
// direction. Measured from the start rather than from a running peak, a rising series reports no
// drawdown at all — which is exactly the mistake that makes a backtest look safe.
func TestMaximumDrawdownIsTheWorstPeakToTrough(t *testing.T) {
	// Peaks at 120, falls to 90: a quarter lost from the high, while the series still ends above
	// where it began.
	measures := newEngine().measureSeries(MeasureSubjectStrategy, nil,
		curveOf(100, 120, 90, 110), 0, decZero)

	if got := *measures.MaxDrawdown; got != "-0.250000000000" {
		t.Errorf("maximum drawdown is %s, want -0.250000000000", got)
	}
	// Stated as a loss. A positive drawdown is a sign error, not good news, and the database
	// refuses the row.
	if *measures.TotalReturn != "0.100000000000" {
		t.Errorf("total return is %s, want 0.100000000000", *measures.TotalReturn)
	}

	// A series that only ever rises still reports a drawdown of zero, not an absence.
	rising := newEngine().measureSeries(MeasureSubjectStrategy, nil, curveOf(100, 110, 120), 0, decZero)
	if got := *rising.MaxDrawdown; got != "0.000000000000" {
		t.Errorf("a rising series reports a drawdown of %s, want zero", got)
	}
}

func TestAnnualisedReturnScalesTheWholeSpan(t *testing.T) {
	// Seven weeks, so annualising a 20% gain should give something a good deal larger.
	measures := newEngine().measureSeries(MeasureSubjectStrategy, nil,
		curveOf(100, 100, 100, 100, 100, 100, 100, 120), 0, decZero)
	annualised, total := parsedFloat(t, *measures.AnnualisedReturn), parsedFloat(t, *measures.TotalReturn)
	if annualised <= total {
		t.Errorf("annualising a seven-week gain of %v produced %v", total, annualised)
	}
	// 1.2 ^ (365.25/49) - 1, worked out independently.
	if want := math.Pow(1.2, 365.25/49) - 1; math.Abs(annualised-want)/want > 1e-6 {
		t.Errorf("annualised return is %v, want %v", annualised, want)
	}
}

func TestASingleSessionCannotBeMeasured(t *testing.T) {
	measures := newEngine().measureSeries(MeasureSubjectStrategy, nil, curveOf(100), 0, decZero)
	if measures.AbsenceReason == nil || *measures.AbsenceReason != MeasureInsufficientSessions {
		t.Errorf("one session produced %+v, want a stated absence", measures)
	}
	if measures.TotalReturn != nil {
		t.Errorf("a return was computed from a single number")
	}
}

func parsedFloat(t *testing.T, value string) float64 {
	t.Helper()
	parsed, err := parseDec(value)
	if err != nil {
		t.Fatalf("parse %s: %v", value, err)
	}
	return parsed.Float()
}
