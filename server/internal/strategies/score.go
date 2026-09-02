// Package strategies turns stored feature values into recorded, explained views.
//
// The feature engine records what was true; a strategy records what a stated method made of it.
// Everything here is built so that the explanation and the score cannot disagree: contributions
// are assembled first and the score is derived from them, rather than the other way round.
package strategies

import (
	"strconv"

	"market-lens/server/internal/features"
)

// Mode says whether a factor compares an instrument with the rest of the universe or reads its
// feature on an absolute scale.
type Mode string

const (
	CrossSectional Mode = "cross_sectional"
	Absolute       Mode = "absolute"
)

// Factor is one named component of a strategy's view, reading one published feature.
type Factor struct {
	Name        string
	Feature     string
	Mode        Mode
	Weight      float64
	Description string
}

// FactorInput is a factor together with its normalised score in [-1, +1] for one instrument and
// session. A nil score means the feature was unavailable — which is different from zero.
type FactorInput struct {
	Factor            Factor
	Score             *float64
	FeatureValue      *string
	FeatureSession    string
	UnavailableReason string
}

// Contribution is one factor's part of a score, recorded so a reader can check the arithmetic.
type Contribution struct {
	Factor            string
	Feature           string
	FeatureValue      *string
	FeatureSession    string
	FactorScore       *float64
	Weight            float64
	Contribution      *float64
	UnavailableReason string
}

// Scored is a strategy's view of one instrument at one session, or its refusal to form one.
type Scored struct {
	Score         float64
	Confidence    float64
	Divisor       float64
	Contributions []Contribution
	Absent        bool
	AbsenceReason string
}

// AbsenceFeatureUnavailable is recorded when no factor had a usable feature value.
const AbsenceFeatureUnavailable = "feature_unavailable"

// Score assembles a view from its factors.
//
// The score is the weighted mean over the factors that were available: an unavailable factor
// leaves the numerator and the denominator together rather than counting as zero, because a
// zero would drag a thin signal toward the middle and then present that as a moderate view.
//
// Contributions are recorded before the division and the divisor is kept, so the explanation
// reconciles with the score exactly (FR-011) rather than approximately.
func Score(inputs []FactorInput) Scored {
	result := Scored{Contributions: make([]Contribution, 0, len(inputs))}
	var numerator float64
	for _, input := range inputs {
		contribution := Contribution{
			Factor: input.Factor.Name, Feature: input.Factor.Feature, Weight: input.Factor.Weight,
			FeatureValue: input.FeatureValue, FeatureSession: input.FeatureSession,
			FactorScore: input.Score, UnavailableReason: input.UnavailableReason,
		}
		if input.Score != nil {
			value := roundToPlaces(input.Factor.Weight * *input.Score)
			contribution.Contribution = &value
			numerator += value
			result.Divisor += input.Factor.Weight
		} else if contribution.UnavailableReason == "" {
			contribution.UnavailableReason = AbsenceFeatureUnavailable
		}
		result.Contributions = append(result.Contributions, contribution)
	}

	if result.Divisor == 0 {
		// No factor had anything to say. There is no view here, and saying "zero" would be a
		// statement the data does not support.
		result.Absent = true
		result.AbsenceReason = AbsenceFeatureUnavailable
		return result
	}

	result.Score = numerator / result.Divisor
	result.Confidence = confidence(result.Contributions, result.Divisor, totalWeight(inputs), result.Score)
	return result
}

func totalWeight(inputs []FactorInput) float64 {
	var total float64
	for _, input := range inputs {
		total += input.Factor.Weight
	}
	return total
}

// confidence is agreement scaled by coverage.
//
// Agreement is the share of available contribution weight pointing the same way as the score.
// Coverage is the share of the strategy's total weight that was available at all. The second
// term is what makes a signal resting on one factor report less than one where every factor
// agrees (FR-013a): a lone factor trivially agrees with itself, and calling that certainty
// would be the most misleading number on the screen.
//
// It measures agreement between factors. It is not, and must never be presented as, the
// probability that the view is correct.
func confidence(contributions []Contribution, divisor, total, score float64) float64 {
	if divisor == 0 || total == 0 {
		return 0
	}
	var agreeing, magnitude float64
	for _, contribution := range contributions {
		if contribution.Contribution == nil {
			continue
		}
		value := *contribution.Contribution
		if value < 0 {
			magnitude -= value
		} else {
			magnitude += value
		}
		if (value >= 0) == (score >= 0) {
			if value < 0 {
				agreeing -= value
			} else {
				agreeing += value
			}
		}
	}
	if magnitude == 0 {
		// Every available factor scored exactly zero: they agree, but about nothing.
		return 0
	}
	return (agreeing / magnitude) * (divisor / total)
}

// Places is the stored precision, matching the feature engine's: a score is compared,
// reproduced and displayed at twelve decimal places, and never as a binary float.
const Places = 12

// roundToPlaces snaps a value to the stored precision.
//
// Contributions are snapped as they are recorded and the score is derived from the snapped
// values, so the stored explanation is what the score was computed from rather than a rendering
// of it. Without this, summing the stored contributions and dividing by the stored divisor
// misses the stored score by the accumulated rounding of every contribution — a discrepancy
// small enough to look like a display artefact and large enough to mean the explanation is not
// the reason.
func roundToPlaces(value float64) float64 {
	rounded, err := strconv.ParseFloat(Round(value), 64)
	if err != nil {
		return value
	}
	return rounded
}

// Round renders a score at the stored precision. Reproducibility is defined here rather than in
// the float: two orderings of the same weighted mean can differ in the last bit without
// differing in any way a reader or a test should care about.
func Round(value float64) string { return features.Round(value) }
