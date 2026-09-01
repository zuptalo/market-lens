package features

// SMA is the simple moving average of the closes.
func SMA(closes []float64) float64 { return mean(closes) }

// Trend is fast / slow - 1 over two moving averages.
func Trend(fast, slow float64) (float64, AbsenceReason) {
	if slow == 0 {
		return 0, AbsenceZeroDenominator
	}
	return fast/slow - 1, ""
}

// Momentum is close / average - 1.
func Momentum(close, average float64) (float64, AbsenceReason) {
	if average == 0 {
		return 0, AbsenceZeroDenominator
	}
	return close/average - 1, ""
}
