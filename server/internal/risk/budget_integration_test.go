package risk_test

import (
	"fmt"
	"testing"
	"time"

	"market-lens/server/internal/portfolio"
	"market-lens/server/internal/risk"
)

// TestEvaluatingAPortfolioStaysWithinItsBudget.
//
// An evaluation is one portfolio read plus a group-by over what it returns. The risk it buys is a
// read whose cost grows per holding — a price lookup or a sector query inside the loop — and that is
// what this measures. The bound is deliberately generous: it exists to catch an order-of-magnitude
// mistake, not to police milliseconds on a shared machine.
func TestEvaluatingAPortfolioStaysWithinItsBudget(t *testing.T) {
	f := newRiskFixture(t)
	if _, err := f.portfolioService().SetCurrency(f.ctx, aliceID.String(), "SEK"); err != nil {
		t.Fatalf("set currency: %v", err)
	}

	// Three markets and three currencies, so the conversion path is exercised too.
	tickers := []string{alfaTicker, betaTicker, danaTicker}
	mics := map[string]string{alfaTicker: "XSTO", betaTicker: "XHEL", danaTicker: "XCSE"}
	for _, ticker := range tickers {
		for index := range 60 {
			f.record(aliceID, ticker, portfolio.DirectionBuy, "10", "120.00", "9",
				f.session(mics[ticker], index%35))
		}
	}
	for _, kind := range risk.Kinds {
		threshold := "0.20"
		if kind == risk.KindHoldingCount {
			threshold = "2"
		}
		f.state(aliceID, kind, threshold)
	}

	started := time.Now()
	report := f.report(aliceID)
	elapsed := time.Since(started)

	if len(report.Limits) != len(risk.Kinds) {
		t.Fatalf("%d evaluations for %d limits", len(report.Limits), len(risk.Kinds))
	}
	for _, evaluation := range report.Limits {
		if evaluation.State == risk.StateUnevaluable {
			t.Fatalf("%s came back unevaluable, so this measured the wrong thing: %+v",
				evaluation.Kind, evaluation)
		}
	}
	if elapsed > 2*time.Second {
		t.Errorf("evaluating four limits over %d trades took %s", 180, elapsed)
	}
	fmt.Printf("evaluated %d limits over 180 trades in %s\n", len(report.Limits), elapsed)
}
