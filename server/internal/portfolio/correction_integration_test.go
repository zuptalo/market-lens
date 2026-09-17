package portfolio_test

import (
	"errors"
	"testing"

	"market-lens/server/internal/portfolio"
)

// A trade here is a fact a person asserted, not one the product observed. People mistype. The
// product must let them fix it and must not let the fix be invisible — a portfolio whose history
// changes silently cannot be reconciled against a broker statement, because last month's figure is
// no longer reproducible.

func TestACorrectionSupersedesRatherThanOverwrites(t *testing.T) {
	f := newPortfolioFixture(t)
	traded := f.session("XSTO", 5)
	original := f.record(aliceID, alfaTicker, portfolio.DirectionBuy, "100", "105.00", "39", traded)

	corrected, err := f.service().Correct(f.ctx, aliceID.String(), original.ID.String(),
		portfolio.RecordRequest{InstrumentID: f.instruments[alfaTicker],
			Direction: portfolio.DirectionBuy, Quantity: "120", Price: "105.00",
			Costs: "39", TradeDate: traded})
	if err != nil {
		t.Fatalf("correct: %v", err)
	}

	// Every derived figure moves with it.
	view := f.view(aliceID)
	if got := view.Holdings[0].Quantity; got != "120.000000000000" {
		t.Errorf("after correcting to 120 the position is %s", got)
	}

	// And the version that produced last month's figure is still there.
	page, err := f.service().Trades(f.ctx, aliceID.String(), portfolio.TradeQuery{})
	if err != nil {
		t.Fatal(err)
	}
	var current, superseded int
	for _, trade := range page.Items {
		switch trade.Status {
		case portfolio.TradeCurrent:
			current++
		case portfolio.TradeSuperseded:
			superseded++
			if trade.ID != original.ID {
				t.Errorf("the superseded version is %s, want the original", trade.ID)
			}
		}
	}
	if current != 1 || superseded != 1 {
		t.Fatalf("history holds %d current and %d superseded versions", current, superseded)
	}

	// The sequence does not change: the person is fixing what a trade said, not when they told us
	// about it, and first-in-first-out consumes by that order.
	if corrected.Sequence != original.Sequence {
		t.Errorf("the correction took sequence %d, the original had %d",
			corrected.Sequence, original.Sequence)
	}
	if corrected.ChangedAt == nil {
		t.Errorf("a correction does not record when it was made")
	}

	// A superseded version cannot be corrected again: there is one live version, and editing a
	// dead one would fork the history.
	if _, err := f.service().Correct(f.ctx, aliceID.String(), original.ID.String(),
		portfolio.RecordRequest{InstrumentID: f.instruments[alfaTicker],
			Direction: portfolio.DirectionBuy, Quantity: "1", Price: "1", TradeDate: traded}); !errors.Is(err, portfolio.ErrNotFound) {
		t.Errorf("correcting a superseded version returned %v", err)
	}
}

func TestAWithdrawnTradeStopsCountingAndStaysReadable(t *testing.T) {
	f := newPortfolioFixture(t)
	traded := f.session("XSTO", 5)
	keep := f.record(aliceID, alfaTicker, portfolio.DirectionBuy, "100", "105.00", "0", traded)
	mistake := f.record(aliceID, alfaTicker, portfolio.DirectionBuy, "999", "105.00", "0", traded)

	if err := f.service().Withdraw(f.ctx, aliceID.String(), mistake.ID.String()); err != nil {
		t.Fatalf("withdraw: %v", err)
	}

	view := f.view(aliceID)
	if got := view.Holdings[0].Quantity; got != "100.000000000000" {
		t.Errorf("after withdrawing the mistake the position is %s, want 100", got)
	}

	// Gone from the figures, still in the history when asked for.
	page, err := f.service().Trades(f.ctx, aliceID.String(), portfolio.TradeQuery{IncludeWithdrawn: true})
	if err != nil {
		t.Fatal(err)
	}
	var withdrawn bool
	for _, trade := range page.Items {
		if trade.ID == mistake.ID && trade.Status == portfolio.TradeWithdrawn {
			withdrawn = true
		}
	}
	if !withdrawn {
		t.Errorf("the withdrawn trade disappeared instead of being marked")
	}

	// And it is out of the ordinary listing, which is what somebody reconciling wants by default.
	page, err = f.service().Trades(f.ctx, aliceID.String(), portfolio.TradeQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != keep.ID {
		t.Errorf("the default listing holds %d trades", len(page.Items))
	}
}

// TestACorrectionThatWouldGoNegativeIsRefused. There is no shorting here, so a position that went
// negative would be a data-entry error the product had accepted and then reported as a holding.
func TestACorrectionThatWouldGoNegativeIsRefused(t *testing.T) {
	f := newPortfolioFixture(t)
	bought := f.record(aliceID, alfaTicker, portfolio.DirectionBuy, "100", "105.00", "0",
		f.session("XSTO", 5))
	f.record(aliceID, alfaTicker, portfolio.DirectionSell, "80", "115.00", "0", f.session("XSTO", 15))

	// Reducing the purchase below what the sale already consumed.
	_, err := f.service().Correct(f.ctx, aliceID.String(), bought.ID.String(),
		portfolio.RecordRequest{InstrumentID: f.instruments[alfaTicker],
			Direction: portfolio.DirectionBuy, Quantity: "50", Price: "105.00",
			TradeDate: f.session("XSTO", 5)})
	var refusal portfolio.Refusal
	if !errors.As(err, &refusal) || refusal.Code != portfolio.RefusalSaleExceedsPosition {
		t.Errorf("correcting a purchase below a later sale returned %v", err)
	}

	// Withdrawing it entirely is the same problem wearing a different hat.
	if err := f.service().Withdraw(f.ctx, aliceID.String(), bought.ID.String()); !errors.As(err, &refusal) {
		t.Errorf("withdrawing a purchase a later sale depends on returned %v", err)
	}

	// The portfolio is untouched by either refusal.
	view := f.view(aliceID)
	if got := view.Holdings[0].Quantity; got != "20.000000000000" {
		t.Errorf("a refused correction changed the position to %s", got)
	}
}
