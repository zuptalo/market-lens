package features_test

import (
	"fmt"
	"testing"

	"market-lens/server/internal/features"
)

// calendar builds an exchange calendar of consecutive dates with the given statuses, so a
// test can place a closed day or a half day exactly where it wants one.
func calendar(statuses ...features.SessionStatus) []features.Session {
	sessions := make([]features.Session, 0, len(statuses))
	for index, status := range statuses {
		sessions = append(sessions, features.Session{
			Date: features.SessionDate(fmt.Sprintf("2026-01-%02d", index+1)), Status: status,
		})
	}
	return sessions
}

// barsOn stores one bar for each of the named dates, closes rising by one.
func barsOn(dates ...features.SessionDate) []features.Bar {
	bars := make([]features.Bar, 0, len(dates))
	for index, date := range dates {
		close := 100 + float64(index)
		bars = append(bars, features.Bar{Session: date, Open: close, High: close + 1, Low: close - 1, Close: close, Volume: 1000})
	}
	return bars
}

func dates(sessions []features.Session, statuses ...features.SessionStatus) []features.SessionDate {
	var out []features.SessionDate
	for _, session := range sessions {
		for _, status := range statuses {
			if session.Status == status {
				out = append(out, session.Date)
			}
		}
	}
	return out
}

func TestWindowCountsStoredSessionsNotCalendarDays(t *testing.T) {
	open := features.SessionOpen
	t.Run("every session present returns exactly n bars ending at the session", func(t *testing.T) {
		cal := calendar(open, open, open, open, open, open, open, open, open, open)
		bars := barsOn(dates(cal, open)...)
		got, reason := features.Window(bars, cal, cal[9].Date, 5)
		if reason != "" || len(got) != 5 {
			t.Fatalf("window = %d bars, reason %q; expected 5 bars and no reason", len(got), reason)
		}
		if got[0].Session != cal[5].Date || got[4].Session != cal[9].Date {
			t.Errorf("window spans %s..%s, expected %s..%s", got[0].Session, got[4].Session, cal[5].Date, cal[9].Date)
		}
	})
	t.Run("fewer stored sessions than the window is insufficient history", func(t *testing.T) {
		cal := calendar(open, open, open, open, open, open, open, open, open, open)
		bars := barsOn(dates(cal, open)[6:]...) // listed at the seventh session: four bars
		got, reason := features.Window(bars, cal, cal[9].Date, 5)
		if reason != features.AbsenceInsufficientHistory || got != nil {
			t.Errorf("window = %d bars, reason %q; expected %s", len(got), reason, features.AbsenceInsufficientHistory)
		}
	})
	t.Run("nineteen stored sessions do not satisfy a window of twenty", func(t *testing.T) {
		statuses := make([]features.SessionStatus, 25)
		for index := range statuses {
			statuses[index] = open
		}
		cal := calendar(statuses...)
		bars := barsOn(dates(cal, open)[6:]...) // nineteen bars ending at the last session
		if _, reason := features.Window(bars, cal, cal[24].Date, 20); reason != features.AbsenceInsufficientHistory {
			t.Errorf("reason %q, expected %s", reason, features.AbsenceInsufficientHistory)
		}
		bars = barsOn(dates(cal, open)[5:]...) // twenty
		if got, reason := features.Window(bars, cal, cal[24].Date, 20); reason != "" || len(got) != 20 {
			t.Errorf("twenty bars: %d bars, reason %q", len(got), reason)
		}
	})
	t.Run("an open session inside the window with no bar is a gap", func(t *testing.T) {
		cal := calendar(open, open, open, open, open, open, open, open, open, open)
		all := dates(cal, open)
		withGap := append(append([]features.SessionDate{}, all[:7]...), all[8:]...) // the eighth session is missing
		got, reason := features.Window(barsOn(withGap...), cal, cal[9].Date, 5)
		if reason != features.AbsenceWindowGap || got != nil {
			t.Errorf("window = %d bars, reason %q; expected %s", len(got), reason, features.AbsenceWindowGap)
		}
		// The same gap outside the window is not a gap.
		if got, reason := features.Window(barsOn(withGap...), cal, cal[9].Date, 2); reason != "" || len(got) != 2 {
			t.Errorf("a gap outside the window: %d bars, reason %q", len(got), reason)
		}
	})
	t.Run("a half day counts as one session", func(t *testing.T) {
		cal := calendar(open, open, open, features.SessionHalfDay, open)
		bars := barsOn(dates(cal, open, features.SessionHalfDay)...)
		got, reason := features.Window(bars, cal, cal[4].Date, 3)
		if reason != "" || len(got) != 3 || got[1].Session != cal[3].Date {
			t.Errorf("window = %d bars, reason %q; expected the half day as the middle of three", len(got), reason)
		}
	})
	t.Run("a closed date inside the span is not a gap", func(t *testing.T) {
		cal := calendar(open, open, open, features.SessionClosed, open, open)
		bars := barsOn(dates(cal, open)...)
		got, reason := features.Window(bars, cal, cal[5].Date, 4)
		if reason != "" || len(got) != 4 {
			t.Fatalf("window = %d bars, reason %q; expected 4 bars across the closed day", len(got), reason)
		}
		if got[0].Session != cal[1].Date {
			t.Errorf("window starts at %s, expected %s: the closed day must not consume a slot", got[0].Session, cal[1].Date)
		}
	})
	t.Run("a session after the last stored bar yields nothing", func(t *testing.T) {
		cal := calendar(open, open, open, open, open)
		bars := barsOn(dates(cal, open)[:4]...)
		got, reason := features.Window(bars, cal, cal[4].Date, 2)
		if got != nil || reason != "" {
			t.Errorf("window = %d bars, reason %q; expected nothing: there is no observation to describe", len(got), reason)
		}
	})
}
