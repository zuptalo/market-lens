package strategies_test

import (
	"testing"

	"market-lens/server/internal/strategies"
)

// An absolute factor maps a feature value onto [-1, +1] by a stated, bounded transform. Bounded
// matters: an unbounded transform would let one extreme instrument dominate a weighted mean that
// is supposed to be a comparison.
func TestAnAbsoluteFactorMapsItsFeatureIntoTheScoringRange(t *testing.T) {
	linear := strategies.Transform{Kind: strategies.LinearClamped, Lower: -0.15, Upper: 0.15}
	for name, expectation := range map[string]struct{ value, want float64 }{
		"at the lower bound": {-0.15, -1},
		"at the upper bound": {0.15, 1},
		"in the middle":      {0, 0},
		"beyond the lower":   {-5, -1},
		"beyond the upper":   {5, 1},
		"three quarters up":  {0.075, 0.5},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := strategies.Normalise(linear, expectation.value, "")
			if err != nil {
				t.Fatal(err)
			}
			if diff := got - expectation.want; diff > 1e-9 || diff < -1e-9 {
				t.Errorf("%v mapped to %v, expected %v", expectation.value, got, expectation.want)
			}
		})
	}

	// An inverted transform is how a penalty is expressed: more volatility, lower score.
	inverted := strategies.Transform{Kind: strategies.LinearClampedInverted, Lower: 0.15, Upper: 0.60}
	low, _ := strategies.Normalise(inverted, 0.15, "")
	high, _ := strategies.Normalise(inverted, 0.60, "")
	if low <= high {
		t.Errorf("an inverted transform scored low volatility %v and high volatility %v; "+
			"a penalty must fall as the value rises", low, high)
	}

	// A label transform maps a stated vocabulary; an unknown label is an error, not a zero.
	labels := strategies.Transform{Kind: strategies.LabelMap, Values: map[string]float64{
		"trending_up": 1, "range_bound": 0, "trending_down": -1, "volatile": -0.5,
	}}
	if got, err := strategies.Normalise(labels, 0, "trending_up"); err != nil || got != 1 {
		t.Errorf("trending_up mapped to %v (%v)", got, err)
	}
	if _, err := strategies.Normalise(labels, 0, "euphoric"); err == nil {
		t.Error("an unknown label was accepted; it must be an error rather than a neutral zero")
	}
}

// A cross-sectional factor ranks an instrument against the universe for the same session.
// Percentile rank is bounded by construction, ties are equal, and the order is total — so the
// same universe can never produce two different rankings.
func TestACrossSectionalFactorRanksAgainstTheUniverse(t *testing.T) {
	universe := []strategies.Observation{
		{Instrument: "d", Value: 0.10},
		{Instrument: "a", Value: -0.20},
		{Instrument: "c", Value: 0.10},
		{Instrument: "b", Value: 0.00},
	}
	ranks := strategies.PercentileRanks(universe)

	if ranks["a"] != -1 {
		t.Errorf("the lowest value ranked %v, expected -1", ranks["a"])
	}
	if ranks["c"] != 1 || ranks["d"] != 1 {
		t.Errorf("the joint highest ranked %v and %v, expected both at 1", ranks["c"], ranks["d"])
	}
	if ranks["c"] != ranks["d"] {
		t.Errorf("equal values ranked differently: %v and %v", ranks["c"], ranks["d"])
	}
	for instrument, rank := range ranks {
		if rank < -1 || rank > 1 {
			t.Errorf("%s ranked %v, outside the scoring range", instrument, rank)
		}
	}

	// A single-instrument universe has nothing to compare with; the middle is the honest answer.
	if only := strategies.PercentileRanks([]strategies.Observation{{Instrument: "a", Value: 5}}); only["a"] != 0 {
		t.Errorf("a universe of one ranked %v, expected the middle", only["a"])
	}
}

// FR-010 and the boundary edge case: one score maps to exactly one action, the same way every
// time, and the upper band owns the boundary.
func TestAScoreOnABandBoundaryTakesTheUpperAction(t *testing.T) {
	bands := []strategies.ActionBand{
		{Lower: -1, Upper: -0.5, Action: "SELL"},
		{Lower: -0.5, Upper: -0.2, Action: "REDUCE"},
		{Lower: -0.2, Upper: 0.2, Action: "HOLD"},
		{Lower: 0.2, Upper: 0.5, Action: "WATCH"},
		{Lower: 0.5, Upper: 1, Action: "BUY"},
	}
	for score, want := range map[float64]string{
		-1: "SELL", -0.75: "SELL",
		-0.5: "REDUCE", -0.35: "REDUCE",
		-0.2: "HOLD", 0: "HOLD", 0.19: "HOLD",
		0.2: "WATCH", 0.49: "WATCH",
		0.5: "BUY", 1: "BUY",
	} {
		got, err := strategies.ActionFor(bands, score)
		if err != nil {
			t.Fatalf("%v: %v", score, err)
		}
		if got != want {
			t.Errorf("score %v mapped to %s, expected %s", score, got, want)
		}
	}

	// Repeatable: the same score never maps two ways.
	for range 100 {
		if got, _ := strategies.ActionFor(bands, -0.2); got != "HOLD" {
			t.Fatalf("a boundary score mapped to %s on a later call", got)
		}
	}
}
