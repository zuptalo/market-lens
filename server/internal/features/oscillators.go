package features

// RSI is Wilder's relative strength index over a fixed window of closes (research R-009):
// the average gain and loss are seeded by the simple mean of the first period changes and
// smoothed by (average * (period-1) + change) / period over the rest. A window with no
// losses is 100, one with no gains is 0.
func RSI(closes []float64, period int) float64 {
	gains := make([]float64, 0, len(closes)-1)
	losses := make([]float64, 0, len(closes)-1)
	for i := 1; i < len(closes); i++ {
		change := closes[i] - closes[i-1]
		gain, loss := 0.0, 0.0
		if change > 0 {
			gain = change
		}
		if change < 0 {
			loss = -change
		}
		gains = append(gains, gain)
		losses = append(losses, loss)
	}
	averageGain, averageLoss := mean(gains[:period]), mean(losses[:period])
	for i := period; i < len(gains); i++ {
		averageGain = (float64(averageGain*float64(period-1)) + gains[i]) / float64(period)
		averageLoss = (float64(averageLoss*float64(period-1)) + losses[i]) / float64(period)
	}
	if averageLoss == 0 {
		return 100
	}
	return 100 - 100/(1+averageGain/averageLoss)
}

// ema is the exponential moving average seeded by the simple mean of the first period
// values, returned from that seed onward: index j describes values[period-1+j].
func ema(values []float64, period int) []float64 {
	k := 2 / float64(period+1)
	current := mean(values[:period])
	out := make([]float64, 0, len(values)-period+1)
	out = append(out, current)
	for _, value := range values[period:] {
		current = float64((value-current)*k) + current
		out = append(out, current)
	}
	return out
}

// MACD returns the line, its signal and the histogram at the last close of a fixed window
// (research R-009): line = EMA(fast) - EMA(slow), defined from the slow seed onward; signal =
// EMA(signal) over the line; histogram = line - signal.
func MACD(closes []float64, fast, slow, signal int) (float64, float64, float64) {
	fastSeries := ema(closes, fast)
	slowSeries := ema(closes, slow)
	line := make([]float64, len(slowSeries))
	for j := range slowSeries {
		line[j] = fastSeries[j+slow-fast] - slowSeries[j]
	}
	signalSeries := ema(line, signal)
	last, lastSignal := line[len(line)-1], signalSeries[len(signalSeries)-1]
	return last, lastSignal, last - lastSignal
}
