package strategies_test

import (
	"testing"
	"time"

	"market-lens/server/internal/features"
	"market-lens/server/internal/strategies"
)

// The stated production bounds (R-008): ten minutes for a full signal history over the curated
// universe, thirty seconds for the incremental pass that follows one import.
const (
	productionFullBudget        = 10 * time.Minute
	productionIncrementalBudget = 30 * time.Second
	// The production shape the bounds are stated for: a hundred instruments across roughly ten
	// years of sessions, and one import's worth of affected sessions.
	productionFullSignals        = 100 * 2546
	productionIncrementalSignals = 100 * 250
)

// TestAFullSignalComputationStaysWithinItsScaledBudget scales the production bound linearly to
// the fixture's size rather than pinning a measurement taken on a quiet machine.
//
// The budget is deliberately loose. `go test ./...` runs packages concurrently, so a tight bound
// would fail on contention rather than on a regression, and a test that fails for reasons
// unrelated to its claim gets weakened until it means nothing. What this catches is an algorithm
// that turned quadratic — a per-instrument query inside the session loop, say — not a machine
// having a slow afternoon.
func TestAFullSignalComputationStaysWithinItsScaledBudget(t *testing.T) {
	fixture := newStrategyFixture(t)

	started := time.Now()
	run := fixture.compute(strategies.ComputeRequest{Kind: strategies.RunKindFull})
	elapsed := time.Since(started)

	budget := scaledBudget(productionFullBudget, productionFullSignals, run.SignalCount)
	t.Logf("full pass: %d signals in %s, budget %s", run.SignalCount, elapsed.Round(time.Millisecond), budget)
	if elapsed > budget {
		t.Fatalf("the full pass took %s for %d signals; the scaled budget is %s",
			elapsed.Round(time.Millisecond), run.SignalCount, budget)
	}
}

func TestAnIncrementalSignalPassStaysWithinItsScaledBudget(t *testing.T) {
	fixture := newStrategyFixture(t)
	fixture.compute(strategies.ComputeRequest{Kind: strategies.RunKindFull})

	importRun, _ := fixture.reviseBar(strategyRanked(1), 30, 190000)
	featureRun := fixture.computeFeatureRun(features.ComputeRequest{
		Kind: features.RunKindIncremental, SinceRun: importRun,
	})

	started := time.Now()
	run := fixture.compute(strategies.ComputeRequest{
		Kind: strategies.RunKindIncremental, SinceFeatureRun: strategies.UUID(featureRun.ID),
	})
	elapsed := time.Since(started)

	budget := scaledBudget(productionIncrementalBudget, productionIncrementalSignals, run.SignalCount)
	t.Logf("incremental pass: %d signals in %s, budget %s", run.SignalCount, elapsed.Round(time.Millisecond), budget)
	if elapsed > budget {
		t.Fatalf("the incremental pass took %s for %d signals; the scaled budget is %s",
			elapsed.Round(time.Millisecond), run.SignalCount, budget)
	}
}

// scaledBudget is the production bound scaled by the share of the production workload the
// fixture actually represents, with a floor so a tiny run is not held to a millisecond.
func scaledBudget(bound time.Duration, productionSignals int64, actual int64) time.Duration {
	if actual <= 0 {
		return time.Second
	}
	scaled := time.Duration(float64(bound) * float64(actual) / float64(productionSignals))
	if scaled < 5*time.Second {
		return 5 * time.Second
	}
	return scaled
}
