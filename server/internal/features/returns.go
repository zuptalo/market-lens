package features

import "math"

// Return is the simple return over the closes: last / first - 1. The closes are the full
// window, so return_20 receives 21 of them.
func Return(closes []float64) (float64, AbsenceReason) {
	first, last := closes[0], closes[len(closes)-1]
	if first == 0 {
		return 0, AbsenceZeroDenominator
	}
	return last/first - 1, ""
}

// LogReturn is ln(last / first) over the closes.
func LogReturn(closes []float64) (float64, AbsenceReason) {
	first, last := closes[0], closes[len(closes)-1]
	if first == 0 {
		return 0, AbsenceZeroDenominator
	}
	return math.Log(last / first), ""
}

// mean sums left to right and divides once: the stated evaluation order, with no
// compensation that could differ between implementations.
func mean(values []float64) float64 {
	var total float64
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}
