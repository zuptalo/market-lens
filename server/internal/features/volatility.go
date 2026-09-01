package features

import "math"

// tradingSessionsPerYear annualises a daily standard deviation, as the Markets listing did.
const tradingSessionsPerYear = 252

// Volatility is the annualised sample standard deviation of the log ratios between
// consecutive closes, in the stated order: ratios ascending, their mean, the sum of squared
// deviations, divided by n-1, square-rooted, times sqrt(252). Every product is materialised
// as a float64 before it is added so no architecture fuses it into a different rounding.
func Volatility(closes []float64) float64 {
	ratios := make([]float64, 0, len(closes)-1)
	for i := 1; i < len(closes); i++ {
		ratios = append(ratios, math.Log(closes[i]/closes[i-1]))
	}
	average := mean(ratios)
	var squares float64
	for _, ratio := range ratios {
		deviation := ratio - average
		squares += float64(deviation * deviation)
	}
	return math.Sqrt(squares/float64(len(ratios)-1)) * math.Sqrt(tradingSessionsPerYear)
}

// ATR is the mean true range over the bars after the first, each true range being the
// largest of the session's own range and its high's and low's distance from the prior close.
func ATR(bars []Bar) float64 {
	ranges := make([]float64, 0, len(bars)-1)
	for i := 1; i < len(bars); i++ {
		previous := bars[i-1].Close
		ranges = append(ranges, math.Max(bars[i].High-bars[i].Low,
			math.Max(math.Abs(bars[i].High-previous), math.Abs(bars[i].Low-previous))))
	}
	return mean(ranges)
}
