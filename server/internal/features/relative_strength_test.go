package features_test

import (
	"testing"

	"market-lens/server/internal/features"
)

// compositeSeries evaluates the composite over the offsets a relative-strength window
// covers, ascending in session order, from the in-memory universe.
func compositeSeries(series map[features.UUID]map[int]features.Bar, endOffset, sessions int) []features.CompositeValue {
	values := make([]features.CompositeValue, 0, sessions)
	for k := range sessions {
		offset := endOffset + sessions - 1 - k
		mean, _, reason := features.Composite(contributorsAt(series, offset), 10)
		values = append(values, features.CompositeValue{MeanReturn: mean, Defined: reason == ""})
	}
	return values
}

func TestRelativeStrengthDividesOwnGrowthByTheCompositesGrowth(t *testing.T) {
	golden := loadGoldenA(t)
	bars := seriesA()
	series := universeSeries()
	for _, position := range []int{319, 250} {
		want := goldenAt(t, golden, position)
		offset := fixtureASessions - 1 - position
		for name, sessions := range map[string]int{"relative_strength_20": 20, "relative_strength_90": 90} {
			own, _ := features.Return(closesOf(window(bars, position, sessions+1)))
			got, reason := features.RelativeStrength(own, compositeSeries(series, offset, sessions))
			expectValue(t, name+"@"+want.Note, got, reason, want.Features[name])
		}
	}
}

func TestRelativeStrengthIsUndefinedWhenAnyCompositeSessionIsUndefined(t *testing.T) {
	series := universeSeries()
	composites := compositeSeries(series, 0, 20)
	composites[7].Defined = false
	if _, reason := features.RelativeStrength(0.1, composites); reason != features.AbsenceCompositeUndefined {
		t.Errorf("reason %q, expected %s", reason, features.AbsenceCompositeUndefined)
	}
	// And it is not carried forward from the last defined session: the warm-up window at
	// position 100 straddles sessions before the composite existed, and the golden records
	// the whole feature as undefined there.
	warmup := goldenAt(t, loadGoldenA(t), 100)
	if got := warmup.Features["relative_strength_20"]; got.AbsenceReason == nil || *got.AbsenceReason != "composite_undefined" {
		t.Errorf("golden at the warm-up: %+v, expected composite_undefined", got)
	}
	own, _ := features.Return(closesOf(window(seriesA(), 100, 21)))
	if _, reason := features.RelativeStrength(own, compositeSeries(series, 219, 20)); reason != features.AbsenceCompositeUndefined {
		t.Errorf("reason %q at the warm-up, expected %s", reason, features.AbsenceCompositeUndefined)
	}
}
