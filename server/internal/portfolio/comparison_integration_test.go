package portfolio_test

import (
	"fmt"
	"testing"

	"market-lens/server/internal/portfolio"
)

// A profit means little on its own. A holding that gained 8% while its market gained 20% lost money
// in every sense that matters, and somebody looking at a green number has no way to know that.

func TestAHoldingIsComparedWithItsOwnMarketOverItsOwnWindow(t *testing.T) {
	f := newPortfolioFixture(t)
	bought := f.session("XHEL", 5)
	f.record(aliceID, betaTicker, portfolio.DirectionBuy, "100", "110.00", "0", bought)

	view := f.view(aliceID)
	comparison := view.Holdings[0].Comparison
	if comparison.AbsenceReason != nil {
		t.Fatalf("the comparison is unavailable: %s", *comparison.AbsenceReason)
	}
	if comparison.Series == "" {
		t.Fatalf("the comparison does not say what it compared against")
	}
	// Both ends of the window are stated, so a reader can check the comparison rather than take it
	// on trust.
	if comparison.FromSession == nil || *comparison.FromSession != bought {
		t.Errorf("the window opens at %v, want the purchase session %s", comparison.FromSession, bought)
	}
	if comparison.ToSession == nil || *comparison.ToSession != f.session("XHEL", 39) {
		t.Errorf("the window closes at %v, want the valuation session", comparison.ToSession)
	}
	if comparison.BenchmarkReturn == nil || comparison.HoldingReturn == nil {
		t.Fatalf("a comparison with no figures: %+v", comparison)
	}

	// The fixture's instruments rise faster than its benchmarks, so a holding that beat its market
	// is what this configuration should produce. The point is not the direction but that both
	// figures exist and are measured over the same two dates.
	holding, benchmark := mustParse(t, *comparison.HoldingReturn), mustParse(t, *comparison.BenchmarkReturn)
	if holding <= benchmark {
		t.Errorf("holding returned %v against benchmark %v; the fixture was built the other way",
			holding, benchmark)
	}
}

// TestTheWindowOpensAtTheOldestSharesStillHeld. A window starting at a purchase whose shares are
// already sold would compare a period the holding no longer represents.
func TestTheWindowOpensAtTheOldestSharesStillHeld(t *testing.T) {
	f := newPortfolioFixture(t)
	first := f.session("XHEL", 3)
	second := f.session("XHEL", 20)
	f.record(aliceID, betaTicker, portfolio.DirectionBuy, "100", "110.00", "0", first)
	f.record(aliceID, betaTicker, portfolio.DirectionBuy, "100", "130.00", "0", second)
	// Sells the whole first lot, so only the second purchase still contributes.
	f.record(aliceID, betaTicker, portfolio.DirectionSell, "100", "140.00", "0", f.session("XHEL", 30))

	view := f.view(aliceID)
	comparison := view.Holdings[0].Comparison
	if comparison.FromSession == nil {
		t.Fatalf("no window: %+v", comparison)
	}
	if *comparison.FromSession != second {
		t.Errorf("the window opens at %s, want %s — the first lot's shares are gone",
			*comparison.FromSession, second)
	}
}

// TestAnUncoveredWindowIsStatedUnavailable is the rule feature 021 settled for the Danish index,
// applied here for the same reason: a comparison measured over a shorter window looks like evidence
// and is not.
func TestAnUncoveredWindowIsStatedUnavailable(t *testing.T) {
	f := newPortfolioFixture(t)
	bought := f.session("XHEL", 5)
	f.record(aliceID, betaTicker, portfolio.DirectionBuy, "100", "110.00", "0", bought)

	// The series no longer reaches back to the purchase.
	f.exec(`DELETE FROM benchmark_points p USING benchmark_series b
		WHERE b.id = p.series_id AND b.mic = 'XHEL' AND p.session_date <= $1::date`,
		f.session("XHEL", 10).String())

	view := f.view(aliceID)
	comparison := view.Holdings[0].Comparison
	if comparison.AbsenceReason == nil || *comparison.AbsenceReason != portfolio.ComparisonSeriesUncovered {
		t.Fatalf("an uncovered window reads %+v", comparison)
	}
	// And no figure is invented for it: a half-filled comparison would read as one.
	if comparison.BenchmarkReturn != nil || comparison.FromSession != nil {
		t.Errorf("an unavailable comparison reported figures anyway: %+v", comparison)
	}
}

func TestAnUnvaluedHoldingIsNotCompared(t *testing.T) {
	f := newPortfolioFixture(t)
	f.record(aliceID, betaTicker, portfolio.DirectionBuy, "100", "110.00", "0", f.session("XHEL", 5))
	f.exec(`DELETE FROM daily_price_bars WHERE instrument_id = $1`, f.instruments[betaTicker].String())

	view := f.view(aliceID)
	comparison := view.Holdings[0].Comparison
	if comparison.AbsenceReason == nil || *comparison.AbsenceReason != portfolio.ComparisonNotValued {
		t.Errorf("an unvalued holding's comparison reads %+v", comparison)
	}
}

func mustParse(t *testing.T, value string) float64 {
	t.Helper()
	var parsed float64
	if _, err := fmt.Sscanf(value, "%g", &parsed); err != nil {
		t.Fatalf("parse %s: %v", value, err)
	}
	return parsed
}
