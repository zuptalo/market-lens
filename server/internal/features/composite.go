package features

import "sort"

// Contributor is one instrument's session-over-session return at one session.
type Contributor struct {
	InstrumentID UUID
	Return       float64
}

// Composite is the equal-weighted mean of the contributors' returns, summed in instrument-id
// order so the order they arrived in cannot change the result (FR-002). Below the minimum
// the composite is undefined and the count is still reported (FR-008b).
func Composite(contributors []Contributor, minContributors int) (float64, int, string) {
	sorted := make([]Contributor, len(contributors))
	copy(sorted, contributors)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].InstrumentID < sorted[j].InstrumentID })
	if len(sorted) < minContributors {
		return 0, len(sorted), CompositeAbsenceInsufficientContributors
	}
	var total float64
	for _, contributor := range sorted {
		total += contributor.Return
	}
	return total / float64(len(sorted)), len(sorted), ""
}
