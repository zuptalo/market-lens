package strategies

import (
	"fmt"
	"sort"
)

// TransformKind names how an absolute factor maps a feature value onto the scoring range.
type TransformKind string

const (
	// LinearClamped maps [lower, upper] onto [-1, +1] and clamps beyond it.
	LinearClamped TransformKind = "linear_clamped"
	// LinearClampedInverted does the same and inverts it: this is how a penalty is expressed,
	// where a higher feature value must produce a lower score.
	LinearClampedInverted TransformKind = "linear_clamped_inverted"
	// LabelMap maps a stated vocabulary — a regime, say — onto stated positions.
	LabelMap TransformKind = "label_map"
)

// Transform is the stated, bounded mapping from a feature value to a factor score. Bounded is
// the point: an unbounded transform would let one extreme instrument dominate a weighted mean
// that exists to compare instruments with each other.
type Transform struct {
	Kind   TransformKind
	Lower  float64
	Upper  float64
	Values map[string]float64
}

// Normalise maps one feature value onto [-1, +1] under a transform. A label the transform does
// not know is an error rather than a zero: a zero would be a stated neutral view derived from
// something nobody defined.
func Normalise(transform Transform, value float64, label string) (float64, error) {
	switch transform.Kind {
	case LinearClamped, LinearClampedInverted:
		if transform.Upper == transform.Lower {
			return 0, fmt.Errorf("transform %s has no range", transform.Kind)
		}
		position := (value-transform.Lower)/(transform.Upper-transform.Lower)*2 - 1
		position = clamp(position)
		if transform.Kind == LinearClampedInverted {
			return -position, nil
		}
		return position, nil
	case LabelMap:
		mapped, known := transform.Values[label]
		if !known {
			return 0, fmt.Errorf("label %q is not in this factor's vocabulary", label)
		}
		return clamp(mapped), nil
	default:
		return 0, fmt.Errorf("unknown transform %q", transform.Kind)
	}
}

func clamp(value float64) float64 {
	if value > 1 {
		return 1
	}
	if value < -1 {
		return -1
	}
	return value
}

// Observation is one instrument's value for a cross-sectional factor at one session.
type Observation struct {
	Instrument string
	Value      float64
}

// PercentileRanks maps a universe's values onto [-1, +1] by rank.
//
// Rank rather than a z-score because rank is bounded by construction: with a z-score, one
// instrument having an extraordinary session would compress everything else toward zero and
// change every other instrument's view for reasons that have nothing to do with them.
//
// Equal values receive equal ranks, and the ordering is made total by the instrument identifier,
// so the same universe can never produce two different rankings — which the specification names
// as an edge case precisely because an arbitrary order would be invisible until it mattered.
func PercentileRanks(universe []Observation) map[string]float64 {
	ranks := make(map[string]float64, len(universe))
	if len(universe) == 0 {
		return ranks
	}
	if len(universe) == 1 {
		// Nothing to compare with. The middle is the honest answer: neither strong nor weak
		// against a universe that does not exist.
		ranks[universe[0].Instrument] = 0
		return ranks
	}

	sorted := make([]Observation, len(universe))
	copy(sorted, universe)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Value != sorted[j].Value {
			return sorted[i].Value < sorted[j].Value
		}
		return sorted[i].Instrument < sorted[j].Instrument
	})

	// Ties share the rank of the last member of their group, so equal values are equal views.
	last := len(sorted) - 1
	for index := 0; index < len(sorted); {
		end := index
		for end+1 < len(sorted) && sorted[end+1].Value == sorted[index].Value {
			end++
		}
		position := float64(end)/float64(last)*2 - 1
		for member := index; member <= end; member++ {
			ranks[sorted[member].Instrument] = position
		}
		index = end + 1
	}
	return ranks
}

// ActionBand maps a span of the scoring range to one action.
type ActionBand struct {
	Lower  float64
	Upper  float64
	Action string
}

// ActionFor maps a score to exactly one action. A score on a boundary belongs to the upper band,
// stated here once so that the same score can never map two ways (FR-010).
func ActionFor(bands []ActionBand, score float64) (string, error) {
	for index, band := range bands {
		lastBand := index == len(bands)-1
		if score >= band.Lower && (score < band.Upper || (lastBand && score <= band.Upper)) {
			return band.Action, nil
		}
	}
	return "", fmt.Errorf("score %v falls outside every action band", score)
}
