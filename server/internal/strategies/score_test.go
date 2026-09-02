package strategies_test

import (
	"testing"

	"market-lens/server/internal/strategies"
)

// A factor set built for arithmetic: three factors whose weights sum to 1, so a reader can
// check the expected score by hand.
func scored(t *testing.T, values map[string]*float64) strategies.Scored {
	t.Helper()
	factors := []strategies.Factor{
		{Name: "a", Feature: "return_90", Mode: strategies.Absolute, Weight: 0.5},
		{Name: "b", Feature: "trend_50_200", Mode: strategies.Absolute, Weight: 0.3},
		{Name: "c", Feature: "regime", Mode: strategies.Absolute, Weight: 0.2},
	}
	inputs := make([]strategies.FactorInput, 0, len(factors))
	for _, factor := range factors {
		inputs = append(inputs, strategies.FactorInput{Factor: factor, Score: values[factor.Name]})
	}
	return strategies.Score(inputs)
}

func number(value float64) *float64 { return &value }

// FR-011: the reasons must account for the score. A weighted mean assembled any other way can
// only be checked afterwards; assembled this way it reconciles by construction.
func TestTheContributionsAccountForTheScore(t *testing.T) {
	for name, values := range map[string]map[string]*float64{
		"every factor available":  {"a": number(1), "b": number(-1), "c": number(0.5)},
		"all agreeing":            {"a": number(0.8), "b": number(0.8), "c": number(0.8)},
		"one at each bound":       {"a": number(1), "b": number(-1), "c": number(1)},
		"one factor unavailable":  {"a": number(0.6), "b": nil, "c": number(-0.2)},
		"two factors unavailable": {"a": nil, "b": nil, "c": number(0.9)},
	} {
		t.Run(name, func(t *testing.T) {
			result := scored(t, values)
			if result.Absent {
				t.Fatalf("no score was produced")
			}
			var summed float64
			for _, contribution := range result.Contributions {
				if contribution.Contribution != nil {
					summed += *contribution.Contribution
				}
			}
			if result.Divisor == 0 {
				t.Fatalf("the divisor is zero with %d available factors", len(result.Contributions))
			}
			expected := summed / result.Divisor
			if diff := expected - result.Score; diff > 1e-12 || diff < -1e-12 {
				t.Errorf("contributions sum to %v over a divisor of %v = %v, but the score is %v",
					summed, result.Divisor, expected, result.Score)
			}
			if result.Score > 1 || result.Score < -1 {
				t.Errorf("score %v is outside the scoring range", result.Score)
			}
		})
	}
}

// An absent feature must leave the numerator and the denominator together. Counting it as zero
// would drag a thin signal toward the middle and then call that moderate — the "absence as a
// neutral value" error this product refuses everywhere else.
func TestAnUnavailableFactorLeavesBothSides(t *testing.T) {
	present := scored(t, map[string]*float64{"a": number(0.8), "b": number(0.8), "c": number(0.8)})
	missing := scored(t, map[string]*float64{"a": number(0.8), "b": number(0.8), "c": nil})

	// Compared at the precision the value is actually stored to: these are float64
	// intermediates, and a last-bit difference between two orderings of the same mean is not a
	// behavioural difference. What matters is that dropping an agreeing factor does not move
	// the score, which counting it as zero would.
	if strategies.Round(present.Score) != strategies.Round(missing.Score) {
		t.Errorf("dropping a factor that agreed with the others moved the score from %s to %s; "+
			"an unavailable factor must leave both sides of the mean",
			strategies.Round(present.Score), strategies.Round(missing.Score))
	}
	if missing.Divisor != 0.8 {
		t.Errorf("divisor = %v, expected the sum of the available weights (0.8)", missing.Divisor)
	}
	// It is still reported, with a reason, rather than vanishing from the explanation.
	var reported bool
	for _, contribution := range missing.Contributions {
		if contribution.Factor == "c" {
			reported = true
			if contribution.Contribution != nil {
				t.Errorf("an unavailable factor contributed %v", *contribution.Contribution)
			}
			if contribution.UnavailableReason == "" {
				t.Errorf("an unavailable factor gave no reason")
			}
		}
	}
	if !reported {
		t.Error("the unavailable factor is missing from the explanation entirely")
	}
}

// FR-013 and FR-013a. Confidence is agreement scaled by coverage: agreement alone would hand a
// lone surviving factor a perfect score for agreeing with itself.
func TestConfidenceIsAgreementScaledByCoverage(t *testing.T) {
	unanimous := scored(t, map[string]*float64{"a": number(0.9), "b": number(0.9), "c": number(0.9)})
	if unanimous.Confidence < 0.999 {
		t.Errorf("every factor agreeing gave confidence %v, expected the maximum", unanimous.Confidence)
	}

	divided := scored(t, map[string]*float64{"a": number(0.9), "b": number(-0.9), "c": number(0.9)})
	if divided.Confidence >= unanimous.Confidence {
		t.Errorf("disagreement gave confidence %v against unanimity's %v", divided.Confidence, unanimous.Confidence)
	}

	// FR-013a: one factor agreeing with itself is not agreement.
	lone := scored(t, map[string]*float64{"a": number(0.9), "b": nil, "c": nil})
	if lone.Confidence >= unanimous.Confidence {
		t.Errorf("a signal resting on one factor reported confidence %v against %v where seven agreed; "+
			"unanimity among one factor is not agreement", lone.Confidence, unanimous.Confidence)
	}
	if lone.Confidence < 0 || lone.Confidence > 1 {
		t.Errorf("confidence %v is outside [0, 1]", lone.Confidence)
	}
}

// With nothing available there is no view to state, and the result must say so rather than
// return a zero that reads as neutral.
func TestNoAvailableFactorsProducesAnAbsenceNotAZero(t *testing.T) {
	result := scored(t, map[string]*float64{"a": nil, "b": nil, "c": nil})
	if !result.Absent {
		t.Fatalf("score %v was produced with no available factor", result.Score)
	}
	if result.AbsenceReason == "" {
		t.Error("the absence carries no reason")
	}
}
