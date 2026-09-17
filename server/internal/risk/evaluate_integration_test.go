package risk_test

import (
	"encoding/json"
	"strings"
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

// TestAnIncompleteTotalMakesALimitUnevaluableInPractice exercises the same rule through the whole
// stack rather than against a hand-built view, because the value that matters is the one feature
// 022 actually produces.
func TestAnIncompleteTotalMakesALimitUnevaluableInPractice(t *testing.T) {
	f := newRiskFixture(t)
	f.record(aliceID, betaTicker, portfolio.DirectionBuy, "100", "100.00", "0", f.session("XHEL", 5))
	f.record(aliceID, alfaTicker, portfolio.DirectionBuy, "100", "100.00", "0", f.session("XSTO", 5))
	f.state(aliceID, risk.KindInstrumentShare, "0.60")
	f.state(aliceID, risk.KindHoldingCount, "10")

	// One holding stops having prices, as a delisting looks from inside this product.
	f.exec(`DELETE FROM daily_price_bars WHERE instrument_id = $1`, f.instruments[alfaTicker].String())

	report := f.report(aliceID)
	share := f.evaluation(report, risk.KindInstrumentShare)
	if share.State != risk.StateUnevaluable {
		t.Errorf("a share limit with an unpriceable holding is %q, want unevaluable", share.State)
	}
	if share.AbsenceReason == nil || *share.AbsenceReason != risk.AbsencePortfolioIncomplete {
		t.Errorf("the reason is %v, want portfolio_incomplete", share.AbsenceReason)
	}

	// Counting needs no price, so one unpriceable holding does not silence every limit.
	count := f.evaluation(report, risk.KindHoldingCount)
	if count.State == risk.StateUnevaluable {
		t.Errorf("the holding count was silenced by a missing price")
	}
}

// TestNothingTheProductThinksSoftensALimit.
//
// This is where a risk feature rots. The pressure to add "but the signal is strong" is exactly the
// pressure this product exists to resist, so the absence is asserted rather than trusted to review.
func TestNothingTheProductThinksSoftensALimit(t *testing.T) {
	f := newRiskFixture(t)
	f.record(aliceID, betaTicker, portfolio.DirectionBuy, "300", "100.00", "0", f.session("XHEL", 5))
	f.record(aliceID, alfaTicker, portfolio.DirectionBuy, "10", "100.00", "0", f.session("XSTO", 5))
	f.state(aliceID, risk.KindInstrumentShare, "0.25")

	before := f.evaluation(f.report(aliceID), risk.KindInstrumentShare)
	if before.State != risk.StateExceeded {
		t.Fatalf("the fixture was supposed to breach: %+v", before)
	}

	// A recorded trade that puts the portfolio further over is accepted. A recorded trade is a fact
	// its owner asserted, and refusing to record reality would be a strange thing for a portfolio
	// to do — feature 022's FR-020 still stands.
	f.record(aliceID, betaTicker, portfolio.DirectionBuy, "100", "100.00", "0", f.session("XHEL", 10))
	after := f.evaluation(f.report(aliceID), risk.KindInstrumentShare)
	if after.State != risk.StateExceeded {
		t.Errorf("after deepening the breach the state is %q", after.State)
	}

	// Nothing in the evaluation names an action, a quantity to sell, or an amount to move. FR-015
	// asserted on the shape rather than on a screen, because a field that exists will be rendered.
	encoded, err := json.Marshal(after)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"sell", "reduce", "suggest", "recommend", "action", "excess", "over_by"} {
		if strings.Contains(strings.ToLower(string(encoded)), forbidden) {
			t.Errorf("the evaluation carries %q: %s", forbidden, encoded)
		}
	}
}

// TestLimitsDoNotTouchAStoredBacktest. Limits are a person's own rules about their own holdings;
// a simulation that ran without them is not retrospectively subject to them.
func TestLimitsDoNotTouchAStoredBacktest(t *testing.T) {
	f := newRiskFixture(t)
	before := f.count(`SELECT count(*) FROM backtest_measures`)

	f.state(aliceID, risk.KindSectorShare, "0.40")
	f.state(aliceID, risk.KindHoldingCount, "5")
	if err := f.service().Remove(f.ctx, aliceID.String(), risk.KindHoldingCount); err != nil {
		t.Fatal(err)
	}

	if after := f.count(`SELECT count(*) FROM backtest_measures`); after != before {
		t.Errorf("stating limits changed %d stored backtest measures", after-before)
	}
	if runs := f.count(`SELECT count(*) FROM backtest_runs`); runs != 0 {
		t.Errorf("a backtest ran because somebody stated a limit")
	}
}
