package backtest

import (
	"testing"

	"market-lens/server/internal/marketdata"
)

// The backtest may not import the market-data package (TestABacktestNeverCallsTheProvider), so it
// names the fallback provider itself. This keeps the two names from drifting apart, which would
// quietly let fallback prices back into a simulation.
func TestTheBacktestNamesTheSameFallbackProvider(t *testing.T) {
	if fallbackProvider != marketdata.FallbackProvider {
		t.Fatalf("the backtest excludes %q, the fallback is %q", fallbackProvider, marketdata.FallbackProvider)
	}
}
