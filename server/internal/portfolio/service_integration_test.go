package portfolio_test

import (
	"errors"
	"testing"

	"market-lens/server/internal/portfolio"
)

func TestRecordingAccumulatesAPosition(t *testing.T) {
	f := newPortfolioFixture(t)
	first := f.session("XSTO", 5)
	second := f.session("XSTO", 15)

	f.record(aliceID, alfaTicker, portfolio.DirectionBuy, "100", "105.00", "39", first)
	f.record(aliceID, alfaTicker, portfolio.DirectionBuy, "50", "115.00", "39", second)

	view := f.view(aliceID)
	if len(view.Holdings) != 1 {
		t.Fatalf("two purchases of one instrument made %d holdings", len(view.Holdings))
	}
	if got := view.Holdings[0].Quantity; got != "150.000000000000" {
		t.Errorf("position is %s, want 150", got)
	}
	// 100 × 105 + 39, plus 50 × 115 + 39 = 10,539 + 5,789 = 16,328.
	if got := view.Holdings[0].Cost; got != "16328.000000000000" {
		t.Errorf("cost is %s, want 16328 — the purchases' own costs belong inside it", got)
	}

	// Both trades stay individually visible: a position is a derived figure, not a replacement for
	// the history behind it.
	page, err := f.service().Trades(f.ctx, aliceID.String(), portfolio.TradeQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Total == nil || *page.Total != 2 {
		t.Errorf("history holds %d trades", len(page.Items))
	}

	f.record(aliceID, alfaTicker, portfolio.DirectionSell, "60", "125.00", "39", f.session("XSTO", 25))
	view = f.view(aliceID)
	if got := view.Holdings[0].Quantity; got != "90.000000000000" {
		t.Errorf("after selling 60 of 150 the position is %s, want 90", got)
	}
}

func TestTheProductRefusesWhatItCannotTrack(t *testing.T) {
	f := newPortfolioFixture(t)
	service := f.service()
	traded := f.session("XSTO", 5)

	// An instrument outside the curated universe. A holding with no stored prices could never be
	// valued or compared, so it is refused rather than accepted as a permanently broken row.
	_, err := service.Record(f.ctx, aliceID.String(), portfolio.RecordRequest{
		InstrumentID: "00000000-0000-4000-8000-00000000dead",
		Direction:    portfolio.DirectionBuy, Quantity: "10", Price: "10", TradeDate: traded})
	var refusal portfolio.Refusal
	if !errors.As(err, &refusal) || refusal.Code != portfolio.RefusalInstrumentNotCarried {
		t.Errorf("recording an uncarried instrument returned %v", err)
	}
	if refusal.Message == "" {
		t.Errorf("the refusal says nothing a person could act on")
	}

	// A sale larger than the position, naming what is actually held. "That is more than you hold"
	// is only useful with the quantity attached.
	f.record(aliceID, alfaTicker, portfolio.DirectionBuy, "100", "105.00", "0", traded)
	_, err = service.Record(f.ctx, aliceID.String(), portfolio.RecordRequest{
		InstrumentID: f.instruments[alfaTicker], Direction: portfolio.DirectionSell,
		Quantity: "150", Price: "110", TradeDate: traded})
	if !errors.As(err, &refusal) || refusal.Code != portfolio.RefusalSaleExceedsPosition {
		t.Fatalf("overselling returned %v", err)
	}
	if refusal.Held != "100.000000000000" {
		t.Errorf("the refusal reports %s held, want 100", refusal.Held)
	}

	// A holding nobody has yet is not a holding.
	_, err = service.Record(f.ctx, aliceID.String(), portfolio.RecordRequest{
		InstrumentID: f.instruments[alfaTicker], Direction: portfolio.DirectionBuy,
		Quantity: "10", Price: "10", TradeDate: "2099-01-01"})
	if !errors.As(err, &refusal) || refusal.Code != portfolio.RefusalTradeDateInFuture {
		t.Errorf("a future-dated trade returned %v", err)
	}
}

// TestAnUnpriceableHoldingIsStatedNotGuessed is the honesty rule applied to a person's own money.
func TestAnUnpriceableHoldingIsStatedNotGuessed(t *testing.T) {
	f := newPortfolioFixture(t)
	f.record(aliceID, alfaTicker, portfolio.DirectionBuy, "100", "105.00", "0", f.session("XSTO", 5))
	f.record(aliceID, betaTicker, portfolio.DirectionBuy, "10", "120.00", "0", f.session("XHEL", 5))

	// One instrument stops having prices, as a delisting looks from inside this product.
	f.exec(`DELETE FROM daily_price_bars WHERE instrument_id = $1`, f.instruments[alfaTicker].String())

	view := f.view(aliceID)
	var unvalued, valued int
	for _, holding := range view.Holdings {
		if holding.Valuation.AbsenceReason != nil {
			unvalued++
			if *holding.Valuation.AbsenceReason != portfolio.ValuationNoPrice {
				t.Errorf("the reason is %s, want no_price", *holding.Valuation.AbsenceReason)
			}
			if holding.Valuation.Value != nil {
				t.Errorf("an unvalued holding carries a value anyway")
			}
		} else {
			valued++
		}
	}
	if unvalued != 1 || valued != 1 {
		t.Fatalf("%d unvalued and %d valued holdings", unvalued, valued)
	}
	// The holding stays in the list. Omitting it would make the total look whole, which is the one
	// thing a total must never do.
	if len(view.Holdings) != 2 {
		t.Errorf("the unvaluable holding was dropped from the list")
	}
	if view.Totals.Complete || view.Totals.Value != nil {
		t.Errorf("a total was stated for a portfolio one holding of which could not be valued")
	}
	if view.Totals.IncompleteReason == nil {
		t.Errorf("the total is incomplete and does not say why")
	}
}

func TestProfitIncludesTheCostsOfTheTradesThatProducedIt(t *testing.T) {
	f := newPortfolioFixture(t)
	// Bought 100 at 100 with 50 of costs, sold 100 at 120 with 50 of costs.
	// Cost = 10,050. Proceeds = 12,000 − 50 = 11,950. Realised = 1,900.
	f.record(aliceID, betaTicker, portfolio.DirectionBuy, "100", "100.00", "50", f.session("XHEL", 5))
	f.record(aliceID, betaTicker, portfolio.DirectionSell, "100", "120.00", "50", f.session("XHEL", 20))

	view := f.view(aliceID)
	if len(view.Realised) != 1 {
		t.Fatalf("%d realised results", len(view.Realised))
	}
	realised := view.Realised[0]
	if realised.Realised != "1900.000000000000" {
		t.Errorf("realised is %s, want 1900 — both trades' costs belong inside it", realised.Realised)
	}
	if realised.CostBasis != portfolio.CostBasisFIFO {
		t.Errorf("the realised figure does not state its cost basis")
	}

	// A fully sold position leaves the open list and keeps its result.
	if len(view.Holdings) != 0 {
		t.Errorf("a fully sold position is still listed as held")
	}
	if view.Totals.Realised != "1900.000000000000" {
		t.Errorf("total realised is %s", view.Totals.Realised)
	}
}

func TestAFullySoldPositionDoesNotMergeIntoARepurchase(t *testing.T) {
	f := newPortfolioFixture(t)
	f.record(aliceID, betaTicker, portfolio.DirectionBuy, "100", "100.00", "0", f.session("XHEL", 5))
	f.record(aliceID, betaTicker, portfolio.DirectionSell, "100", "120.00", "0", f.session("XHEL", 10))
	f.record(aliceID, betaTicker, portfolio.DirectionBuy, "40", "130.00", "0", f.session("XHEL", 20))

	view := f.view(aliceID)
	if len(view.Holdings) != 1 || view.Holdings[0].Quantity != "40.000000000000" {
		t.Fatalf("the re-purchase reads as %+v", view.Holdings)
	}
	// The new position costs what it cost, not an average with shares already sold.
	if view.Holdings[0].Cost != "5200.000000000000" {
		t.Errorf("the re-purchased cost is %s, want 5200", view.Holdings[0].Cost)
	}
	if view.Totals.Realised != "2000.000000000000" {
		t.Errorf("the earlier holding period's result is %s, want 2000", view.Totals.Realised)
	}
}

func TestNoPortfolioReturnIsEverReported(t *testing.T) {
	f := newPortfolioFixture(t)
	f.record(aliceID, betaTicker, portfolio.DirectionBuy, "100", "100.00", "0", f.session("XHEL", 5))

	view := f.view(aliceID)
	// The absence is a requirement, not an omission: the product does not know what was paid in, so
	// a return would divide by a number it invented.
	if view.Totals.ReturnAbsence != portfolio.ReturnAbsence {
		t.Errorf("the total does not state why there is no return: %q", view.Totals.ReturnAbsence)
	}

	// And an empty portfolio says the same thing rather than reporting zero.
	empty := f.view(bobID)
	if empty.Totals.ReturnAbsence != portfolio.ReturnAbsence {
		t.Errorf("an empty portfolio does not state the absent return")
	}
	if !empty.RecordsWhatYouEntered {
		t.Errorf("the view does not state that it records what a person entered")
	}
}
