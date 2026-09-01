package features

// Drawdown is the last close relative to the highest close in the window: zero at a peak,
// negative below one, never positive.
func Drawdown(closes []float64) float64 {
	peak := closes[0]
	for _, close := range closes {
		if close > peak {
			peak = close
		}
	}
	return closes[len(closes)-1]/peak - 1
}
