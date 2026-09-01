package features

// Split is a corporate action that multiplies the share count by Ratio from ExDate on.
type Split struct {
	ExDate SessionDate
	Ratio  float64
}

// Adjusted returns the bars on the adjusted price basis as known at asOf: for every split
// whose ex-date is on or before asOf, the open, high, low and close of each bar before that
// ex-date are divided by its ratio. Splits with a later ex-date had not happened yet at asOf
// and are not applied (FR-019). Volume is never adjusted, and the input is not modified.
func Adjusted(bars []Bar, splits []Split, asOf SessionDate) []Bar {
	out := make([]Bar, len(bars))
	copy(out, bars)
	for _, split := range splits {
		if split.ExDate > asOf || split.Ratio == 0 {
			continue
		}
		for i := range out {
			if out[i].Session < split.ExDate {
				out[i].Open /= split.Ratio
				out[i].High /= split.Ratio
				out[i].Low /= split.Ratio
				out[i].Close /= split.Ratio
			}
		}
	}
	return out
}
