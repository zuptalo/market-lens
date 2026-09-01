package features_test

import (
	"testing"
	"time"
)

// SC-006 / research R-008: a full computation from empty over the curated universe must
// finish within ten minutes on the deployment's own hardware. The fixture holds about
// 3,200 bars against production's ~244,000 — roughly 1.3% — so ten minutes scales to about
// eight seconds. The budget here is three, leaving the production figure (recorded on first
// run, T094) more than 2.5× headroom over a linear extrapolation.
func TestAFullFixtureComputationStaysWithinItsScaledBudget(t *testing.T) {
	const budget = 3 * time.Second
	f := newEngineFixture(t)
	bars := count(t, f, `SELECT count(*) FROM daily_price_bars`)
	started := time.Now()
	_, run := computeFixture(t, f, 4)
	elapsed := time.Since(started)
	t.Logf("full computation: %d bars, %d values, %d instruments in %s (budget %s; production budget 10m over ~244k bars)",
		bars, run.ValueCount, run.InstrumentCount, elapsed.Round(time.Millisecond), budget)
	if elapsed > budget {
		t.Errorf("full fixture computation took %s, over the %s budget", elapsed.Round(time.Millisecond), budget)
	}
}
