package features

// VolumeSMA is the mean volume over the bars. A zero volume is an observation and counts.
func VolumeSMA(bars []Bar) float64 {
	volumes := make([]float64, len(bars))
	for i, bar := range bars {
		volumes[i] = float64(bar.Volume)
	}
	return mean(volumes)
}

// VolumeRatio is the session's volume over the average; an average of zero has nothing to
// divide by.
func VolumeRatio(volume int64, average float64) (float64, AbsenceReason) {
	if average == 0 {
		return 0, AbsenceZeroDenominator
	}
	return float64(volume) / average, ""
}
