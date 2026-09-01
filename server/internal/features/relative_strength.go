package features

// CompositeValue is the composite at one session, as stored: undefined when its contributor
// count fell below the minimum.
type CompositeValue struct {
	MeanReturn float64
	Defined    bool
}

// RelativeStrength is the instrument's growth over the window relative to the composite's:
// (1 + own) / prod(1 + composite[s]) - 1 over the window's sessions in ascending order. A
// single undefined composite session inside the window makes it undefined (FR-008); it is
// never carried forward from a session where it was defined.
func RelativeStrength(own float64, composites []CompositeValue) (float64, AbsenceReason) {
	product := 1.0
	for _, composite := range composites {
		if !composite.Defined {
			return 0, AbsenceCompositeUndefined
		}
		product *= 1 + composite.MeanReturn
	}
	if product == 0 {
		return 0, AbsenceZeroDenominator
	}
	return (1+own)/product - 1, ""
}
