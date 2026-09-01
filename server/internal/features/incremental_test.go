package features_test

import (
	"testing"

	"market-lens/server/internal/features"
)

// R-004: a bar at session S takes part in the window of every session from S until it falls
// out the back of the longest window — [S, S + (W_max − 1)] counted in stored sessions.
func TestAffectedRangeCountsStoredSessions(t *testing.T) {
	open, closed := features.SessionOpen, features.SessionClosed
	t.Run("spans wMax sessions from the revised one", func(t *testing.T) {
		cal := calendar(open, open, open, open, open, open, open, open, open, open)
		got := features.AffectedRange(cal, cal[2].Date, 4)
		if got.From != cal[2].Date || got.To != cal[5].Date {
			t.Errorf("range = %s..%s, expected %s..%s", got.From, got.To, cal[2].Date, cal[5].Date)
		}
	})
	t.Run("a window of one affects only the session itself", func(t *testing.T) {
		cal := calendar(open, open, open)
		if got := features.AffectedRange(cal, cal[1].Date, 1); got.From != cal[1].Date || got.To != cal[1].Date {
			t.Errorf("range = %s..%s", got.From, got.To)
		}
	})
	t.Run("is clipped at the last session", func(t *testing.T) {
		cal := calendar(open, open, open, open, open)
		if got := features.AffectedRange(cal, cal[3].Date, 10); got.From != cal[3].Date || got.To != cal[4].Date {
			t.Errorf("range = %s..%s, expected %s..%s", got.From, got.To, cal[3].Date, cal[4].Date)
		}
	})
	t.Run("a closed date between two sessions is no session", func(t *testing.T) {
		cal := calendar(open, open, closed, open, open, open)
		got := features.AffectedRange(cal, cal[1].Date, 3)
		if got.From != cal[1].Date || got.To != cal[4].Date {
			t.Errorf("range = %s..%s, expected %s..%s: the closed day must not consume a slot", got.From, got.To, cal[1].Date, cal[4].Date)
		}
		// A half day is a session.
		cal = calendar(open, open, features.SessionHalfDay, open, open)
		if got := features.AffectedRange(cal, cal[1].Date, 3); got.To != cal[3].Date {
			t.Errorf("range ends at %s, expected the half day to count: %s", got.To, cal[3].Date)
		}
	})
	t.Run("a session outside the calendar affects only itself", func(t *testing.T) {
		cal := calendar(open, open, open)
		if got := features.AffectedRange(cal, "2026-02-01", 5); got.From != "2026-02-01" || got.To != "2026-02-01" {
			t.Errorf("range = %s..%s", got.From, got.To)
		}
	})
}
