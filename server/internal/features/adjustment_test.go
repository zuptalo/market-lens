package features_test

import (
	"testing"

	"market-lens/server/internal/features"
)

// Adjustment is engine-applied from recorded splits as of the session being computed, so a
// value at a session before a split never knows the split happened (FR-019).
func TestAdjustedAppliesOnlySplitsAlreadyKnownAtTheSession(t *testing.T) {
	bars := []features.Bar{
		{Session: "2026-01-02", Open: 200, High: 210, Low: 190, Close: 204, Volume: 500},
		{Session: "2026-01-05", Open: 204, High: 212, Low: 198, Close: 208, Volume: 600},
		{Session: "2026-01-06", Open: 104, High: 108, Low: 100, Close: 105, Volume: 1300}, // ex-date
		{Session: "2026-01-07", Open: 105, High: 109, Low: 101, Close: 106, Volume: 1100},
	}
	split := features.Split{ExDate: "2026-01-06", Ratio: 2}

	t.Run("on or after the ex-date, earlier bars are divided by the ratio", func(t *testing.T) {
		got := features.Adjusted(bars, []features.Split{split}, "2026-01-07")
		if got[0].Close != 102 || got[0].High != 105 || got[0].Low != 95 || got[0].Open != 100 {
			t.Errorf("pre-split bar was not divided: %+v", got[0])
		}
		if got[1].Close != 104 {
			t.Errorf("the bar before the ex-date was not divided: %+v", got[1])
		}
		if got[2].Close != 105 || got[3].Close != 106 {
			t.Errorf("bars from the ex-date on must be untouched: %+v %+v", got[2], got[3])
		}
		if got[0].Volume != 500 {
			t.Errorf("volume is not adjusted in this version, got %d", got[0].Volume)
		}
		if bars[0].Close != 204 {
			t.Error("the input must not be mutated")
		}
	})
	t.Run("exactly at the ex-date the split is known", func(t *testing.T) {
		got := features.Adjusted(bars, []features.Split{split}, "2026-01-06")
		if got[1].Close != 104 || got[2].Close != 105 {
			t.Errorf("as of the ex-date: %+v %+v", got[1], got[2])
		}
	})
	t.Run("before the ex-date the split has not happened", func(t *testing.T) {
		got := features.Adjusted(bars, []features.Split{split}, "2026-01-05")
		if got[0].Close != 204 || got[1].Close != 208 {
			t.Errorf("a split in the future adjusted the past: %+v %+v", got[0], got[1])
		}
	})
	t.Run("a split recorded with an ex-date after the window has no effect", func(t *testing.T) {
		later := features.Split{ExDate: "2026-02-01", Ratio: 3}
		got := features.Adjusted(bars, []features.Split{later}, "2026-01-07")
		for index := range bars {
			if got[index].Close != bars[index].Close {
				t.Errorf("bar %d moved: %v -> %v", index, bars[index].Close, got[index].Close)
			}
		}
	})
	t.Run("two splits compound", func(t *testing.T) {
		second := features.Split{ExDate: "2026-01-07", Ratio: 2}
		got := features.Adjusted(bars, []features.Split{second, split}, "2026-01-07")
		if got[0].Close != 51 || got[2].Close != 52.5 || got[3].Close != 106 {
			t.Errorf("compounded: %v %v %v", got[0].Close, got[2].Close, got[3].Close)
		}
	})
}
