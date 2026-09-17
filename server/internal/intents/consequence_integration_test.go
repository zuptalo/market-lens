package intents_test

import (
	"testing"

	"market-lens/server/internal/intents"
	"market-lens/server/internal/portfolio"
	"market-lens/server/internal/risk"
)

// TestAnIntentReportsWhatItWouldDo is this feature's first requirement and its first test.
//
// The product will not tell somebody what to do. What it can do is tell them what would happen if
// they did the thing they are already considering — and that is worth nothing unless the figures
// can be checked, which is why the share arrives with the total it was measured against.
func TestAnIntentReportsWhatItWouldDo(t *testing.T) {
	f := newIntentsFixture(t)
	if _, err := f.portfolioService().SetCurrency(f.ctx, aliceID.String(), "EUR"); err != nil {
		t.Fatalf("set currency: %v", err)
	}

	// Two Finnish holdings, both priced in the accounting currency so the arithmetic is readable.
	f.record(aliceID, betaTicker, portfolio.DirectionBuy, "100", "100.00", "0", f.session("XHEL", 5))
	f.record(aliceID, alfaTicker, portfolio.DirectionBuy, "100", "100.00", "0", f.session("XSTO", 5))
	f.limit(aliceID, risk.KindInstrumentShare, "0.50")

	// Considering trebling one of them.
	f.consider(aliceID, betaTicker, intents.DirectionBuy, "200", "130.00")

	intent := f.only(f.report(aliceID, false))
	if intent.Status != intents.StatusConsidering {
		t.Fatalf("a newly recorded intent reads %q", intent.Status)
	}
	if intent.Consequence == nil {
		t.Fatalf("an intent with no consequence: %+v", intent)
	}
	consequence := *intent.Consequence

	if consequence.ResultingQuantity != "300.000000000000" {
		t.Errorf("holding 100 and buying 200 would leave %s, want 300", consequence.ResultingQuantity)
	}
	if consequence.ResultingValue == nil || consequence.ResultingShare == nil ||
		consequence.Denominator == nil {
		t.Fatalf("a consequence with no arithmetic behind it: %+v", consequence)
	}

	// The share reproduces: the resulting value over the total it was measured against.
	value := asFloat(t, *consequence.ResultingValue)
	total := asFloat(t, *consequence.Denominator)
	share := asFloat(t, *consequence.ResultingShare)
	if difference := share - value/total; difference > 1e-9 || difference < -1e-9 {
		t.Errorf("the share is %v but value/total is %v", share, value/total)
	}
	// Trebling one of two equal holdings takes it past half the portfolio.
	if share <= 0.5 {
		t.Errorf("the resulting share is %v; the fixture was built to breach a half", share)
	}

	// And the limit it would put her outside is named, with what it would reach.
	if len(consequence.Limits) != 1 {
		t.Fatalf("%d limit consequences", len(consequence.Limits))
	}
	verdict := consequence.Limits[0]
	if verdict.State != risk.StateExceeded {
		t.Errorf("the limit would be %q, want exceeded", verdict.State)
	}
	if verdict.Measured == nil {
		t.Errorf("the limit consequence reports no figure")
	}
}

// TestAnIntentInSomethingNotYetHeldIsStillPriced.
//
// The portfolio values only what is held, but the product has stored prices for everything it
// carries. Reporting "no price" for an instrument it has ten years of bars for would be untrue, and
// it would make the feature useless for the most ordinary case there is: buying something new.
func TestAnIntentInSomethingNotYetHeldIsStillPriced(t *testing.T) {
	f := newIntentsFixture(t)
	if _, err := f.portfolioService().SetCurrency(f.ctx, aliceID.String(), "EUR"); err != nil {
		t.Fatalf("set currency: %v", err)
	}
	f.record(aliceID, betaTicker, portfolio.DirectionBuy, "100", "100.00", "0", f.session("XHEL", 5))
	f.limit(aliceID, risk.KindInstrumentShare, "0.60")

	// Nothing of this one is held yet.
	f.consider(aliceID, alfaTicker, intents.DirectionBuy, "50", "120.00")

	consequence := *f.only(f.report(aliceID, false)).Consequence
	if consequence.ResultingQuantity != "50.000000000000" {
		t.Errorf("buying 50 of something unheld would leave %s", consequence.ResultingQuantity)
	}
	if consequence.ResultingValue == nil {
		t.Fatalf("an instrument the product has prices for reported none: %+v", consequence)
	}
	if consequence.ResultingShare == nil {
		t.Errorf("no share was reported for a position the product could price")
	}
	// And it counts toward the total, so the limits see it.
	if len(consequence.Limits) != 1 || consequence.Limits[0].State == risk.StateUnevaluable {
		t.Errorf("the limit came back %+v", consequence.Limits)
	}
}

// TestSellingMoreThanIsHeldIsReportedNotRefused. A recorded trade that cannot be true is an error
// worth refusing; an intent is a thought, and a notebook that refuses a thought is a strange one.
func TestSellingMoreThanIsHeldIsReportedNotRefused(t *testing.T) {
	f := newIntentsFixture(t)
	f.record(aliceID, betaTicker, portfolio.DirectionBuy, "100", "100.00", "0", f.session("XHEL", 5))

	f.consider(aliceID, betaTicker, intents.DirectionSell, "150", "130.00")

	consequence := *f.only(f.report(aliceID, false)).Consequence
	if consequence.ResultingQuantity != "-50.000000000000" {
		t.Errorf("selling 150 of 100 held would leave %s, want -50", consequence.ResultingQuantity)
	}
	// The same trade recorded in the portfolio is still refused, because that is a claim about the
	// past rather than a thought about the future.
	_, err := f.portfolioService().Record(f.ctx, aliceID.String(), portfolio.RecordRequest{
		InstrumentID: f.instruments[betaTicker], Direction: portfolio.DirectionSell,
		Quantity: "150", Price: "130.00", TradeDate: f.session("XHEL", 10),
	})
	if err == nil {
		t.Errorf("the portfolio accepted a sale larger than the position")
	}
}

// TestAnUnpriceablePortfolioMakesTheShareUnevaluable — the same rule feature 023 applies, reached
// through the evaluator this reuses rather than reimplemented here.
func TestAnUnpriceablePortfolioMakesTheShareUnevaluable(t *testing.T) {
	f := newIntentsFixture(t)
	f.record(aliceID, betaTicker, portfolio.DirectionBuy, "100", "100.00", "0", f.session("XHEL", 5))
	f.record(aliceID, alfaTicker, portfolio.DirectionBuy, "100", "100.00", "0", f.session("XSTO", 5))
	f.limit(aliceID, risk.KindInstrumentShare, "0.50")
	f.consider(aliceID, betaTicker, intents.DirectionBuy, "50", "130.00")

	// One holding stops being priceable.
	f.exec(`DELETE FROM daily_price_bars WHERE instrument_id = $1`, f.instruments[alfaTicker].String())

	consequence := *f.only(f.report(aliceID, false)).Consequence
	if consequence.AbsenceReason == nil || *consequence.AbsenceReason != intents.AbsencePortfolioIncomplete {
		t.Errorf("the consequence reads %+v, want portfolio_incomplete", consequence.AbsenceReason)
	}
	if consequence.ResultingShare != nil {
		t.Errorf("a share was reported against a total the product declines to state")
	}
	// The resulting quantity needs no price, and is still reported.
	if consequence.ResultingQuantity != "150.000000000000" {
		t.Errorf("the resulting quantity is %s", consequence.ResultingQuantity)
	}
	// And no limit is reported satisfied because something could not be measured.
	for _, verdict := range consequence.Limits {
		if verdict.State == risk.StateWithin {
			t.Errorf("a limit reported within while the portfolio total was unstated")
		}
	}
}

// TestIntentsAreNotCombined. Two intents that each look fine may together breach a limit. Combining
// them would need an order of application nobody stated.
func TestIntentsAreNotCombined(t *testing.T) {
	f := newIntentsFixture(t)
	if _, err := f.portfolioService().SetCurrency(f.ctx, aliceID.String(), "EUR"); err != nil {
		t.Fatalf("set currency: %v", err)
	}
	f.record(aliceID, betaTicker, portfolio.DirectionBuy, "100", "100.00", "0", f.session("XHEL", 5))
	f.consider(aliceID, betaTicker, intents.DirectionBuy, "40", "130.00")
	f.consider(aliceID, betaTicker, intents.DirectionBuy, "40", "130.00")

	report := f.report(aliceID, false)
	if len(report.Intents) != 2 {
		t.Fatalf("%d intents", len(report.Intents))
	}
	// Each measured against the portfolio as it stands: 100 + 40, twice. Not 180.
	for _, intent := range report.Intents {
		if intent.Consequence.ResultingQuantity != "140.000000000000" {
			t.Errorf("an intent reports %s; intents are measured independently",
				intent.Consequence.ResultingQuantity)
		}
	}
	if !report.EvaluatedIndependently {
		t.Errorf("the report does not say the intents were evaluated independently")
	}
}
