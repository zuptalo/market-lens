package backtest_test

import (
	"testing"
	"time"

	"market-lens/server/internal/backtest"
)

// TestABacktestScalesWithItsSessionsRatherThanTheirSquare.
//
// The honest evidence for SC-007 is the production run itself, and it is recorded in the
// quickstart. What a test can add cheaply is the shape of the cost: a simulation that re-read the
// signals for every rebalance, or re-valued the whole history at every session, would still finish
// this fixture and would not finish the real universe. Comparing a daily schedule against a
// monthly one over the same data measures exactly that — a daily run does eight times the
// planning, and if it costs eight times as much, the per-session work is not constant.
func TestABacktestScalesWithItsSessionsRatherThanTheirSquare(t *testing.T) {
	f := newBacktestFixture(t)

	f.publishConfiguration(map[string]any{"rebalance": "monthly"})
	monthlyStart := time.Now()
	monthly := f.run()
	monthlyElapsed := time.Since(monthlyStart)

	f.publishConfiguration(map[string]any{"rebalance": "daily"})
	dailyStart := time.Now()
	daily := f.run()
	dailyElapsed := time.Since(dailyStart)

	if monthly.Status != backtest.RunStatusSucceeded || daily.Status != backtest.RunStatusSucceeded {
		t.Fatalf("a run failed: %s then %s", monthly.Status, daily.Status)
	}
	if daily.RebalanceCount <= monthly.RebalanceCount*5 {
		t.Fatalf("the daily schedule rebalanced %d times against the monthly %d; the two are too "+
			"close for this comparison to mean anything", daily.RebalanceCount, monthly.RebalanceCount)
	}

	// Twenty times the planning for twenty times the rebalances would be linear and fine. The
	// bound is deliberately generous — this is a timing test on a shared machine, and it exists to
	// catch an order-of-magnitude change, not to police a few milliseconds.
	if dailyElapsed > monthlyElapsed*20 {
		t.Errorf("a daily schedule took %s against a monthly %s over identical data; the per-session "+
			"cost is not constant", dailyElapsed, monthlyElapsed)
	}
	t.Logf("monthly: %d rebalances in %s; daily: %d rebalances in %s",
		monthly.RebalanceCount, monthlyElapsed, daily.RebalanceCount, dailyElapsed)
}
