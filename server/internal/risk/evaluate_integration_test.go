package risk_test

import (
	"testing"

	"market-lens/server/internal/portfolio"
	"market-lens/server/internal/risk"
)

// TestABreachIsReportedWithTheArithmeticBehindIt is this feature's first requirement and its first
// test.
//
// A percentage with no numerator and no denominator is an assertion, and an assertion about
// somebody's own money is exactly what this product refuses to make. So the breach has to arrive
// with the value that produced it, the total it was divided by, and the threshold it passed — all
// of it checkable by the person reading it.
func TestABreachIsReportedWithTheArithmeticBehindIt(t *testing.T) {
	f := newRiskFixture(t)
	if _, err := f.portfolioService().SetCurrency(f.ctx, aliceID.String(), "EUR"); err != nil {
		t.Fatalf("set currency: %v", err)
	}

	// Two Finnish holdings, priced in the accounting currency so the arithmetic is readable.
	// The fixture's closes rise by 1.00 a session from a per-instrument base; both are valued at
	// the same latest session, so the proportion is fixed by the quantities and those closes.
	f.record(aliceID, betaTicker, portfolio.DirectionBuy, "300", "100.00", "0", f.session("XHEL", 5))
	f.record(aliceID, alfaTicker, portfolio.DirectionBuy, "10", "100.00", "0", f.session("XSTO", 5))

	f.state(aliceID, risk.KindInstrumentShare, "0.25")

	evaluation := f.evaluation(f.report(aliceID), risk.KindInstrumentShare)

	if evaluation.State != risk.StateExceeded {
		t.Fatalf("the largest holding is far more than a quarter, but the state is %q (%+v)",
			evaluation.State, evaluation)
	}
	if evaluation.Measured == nil || evaluation.Denominator == nil {
		t.Fatalf("a breach with no arithmetic behind it: %+v", evaluation)
	}
	if evaluation.Threshold != "0.250000000000" {
		t.Errorf("the threshold reads %q, not what was stated", evaluation.Threshold)
	}

	// The contributions are the working: each holding's value, and the share it produced.
	if len(evaluation.Contributions) != 2 {
		t.Fatalf("%d contributions for two holdings: %+v", len(evaluation.Contributions), evaluation.Contributions)
	}
	// Largest first, and the largest is what was measured.
	if evaluation.Contributions[0].Share != *evaluation.Measured {
		t.Errorf("the measured figure %s is not the largest contribution %s",
			*evaluation.Measured, evaluation.Contributions[0].Share)
	}

	// And the sum reproduces: every contribution's value over the denominator is its share, and the
	// values add up to the denominator.
	total, err := parseFloat(*evaluation.Denominator)
	if err != nil {
		t.Fatal(err)
	}
	var summed float64
	for _, contribution := range evaluation.Contributions {
		value, err := parseFloat(contribution.Value)
		if err != nil {
			t.Fatal(err)
		}
		share, err := parseFloat(contribution.Share)
		if err != nil {
			t.Fatal(err)
		}
		summed += value
		if difference := share - value/total; difference > 1e-9 || difference < -1e-9 {
			t.Errorf("%s reports share %v but its value over the total is %v",
				contribution.Label, share, value/total)
		}
	}
	if difference := summed - total; difference > 1e-6 || difference < -1e-6 {
		t.Errorf("the contributions sum to %v against a denominator of %v", summed, total)
	}
}
