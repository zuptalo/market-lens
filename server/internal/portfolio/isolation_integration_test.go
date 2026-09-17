package portfolio_test

import (
	"errors"
	"testing"

	"market-lens/server/internal/portfolio"
)

// TestAPersonSeesOnlyTheirOwnHoldings is this feature's first requirement and its first test.
//
// Everything else here is arithmetic, and arithmetic that is wrong is a bug. This is the one
// property whose failure is a breach: the product's first user-owned domain record, and the
// boundary that risk limits and order intents will inherit without re-examining. So it is asserted
// before a single figure is computed, across every surface a person could reach — the holdings, the
// totals, the trade history and the event stream.
func TestAPersonSeesOnlyTheirOwnHoldings(t *testing.T) {
	f := newPortfolioFixture(t)
	bought := f.session("XSTO", 10)

	f.record(aliceID, alfaTicker, portfolio.DirectionBuy, "100", "245.80", "39", bought)
	f.record(bobID, betaTicker, portfolio.DirectionBuy, "50", "88.20", "12", f.session("XHEL", 10))

	alice := f.view(aliceID)
	bob := f.view(bobID)

	if len(alice.Holdings) != 1 || alice.Holdings[0].Ticker != alfaTicker {
		t.Fatalf("alice holds %+v", alice.Holdings)
	}
	if len(bob.Holdings) != 1 || bob.Holdings[0].Ticker != betaTicker {
		t.Fatalf("bob holds %+v", bob.Holdings)
	}
	// Not merely a different list — neither person's instrument may appear anywhere in the other's
	// view, including in a total that quietly summed both.
	for _, holding := range alice.Holdings {
		if holding.Ticker == betaTicker {
			t.Errorf("alice can see bob's holding")
		}
	}
	if alice.Totals.Cost == bob.Totals.Cost {
		t.Errorf("both totals are %s; one portfolio is being summed for both people", alice.Totals.Cost)
	}

	// The trade history is the same boundary asked a different way.
	aliceTrades, err := f.service().Trades(f.ctx, aliceID.String(), portfolio.TradeQuery{})
	if err != nil {
		t.Fatalf("list alice's trades: %v", err)
	}
	if len(aliceTrades.Items) != 1 || aliceTrades.Items[0].InstrumentID != f.instruments[alfaTicker] {
		t.Errorf("alice's history is %+v", aliceTrades.Items)
	}

	// And the stored events: a portfolio change reaches its owner and nobody else.
	if mine := f.count(`SELECT count(*) FROM client_events
		WHERE event_type = $1 AND scope = 'user' AND subject_user_id = $2`,
		portfolio.EventChanged, aliceID.String()); mine == 0 {
		t.Errorf("alice's change published no event scoped to her")
	}
	if leaked := f.count(`SELECT count(*) FROM client_events
		WHERE event_type = $1 AND (scope <> 'user' OR subject_user_id IS NULL)`,
		portfolio.EventChanged); leaked != 0 {
		t.Errorf("%d portfolio events are not scoped to one person", leaked)
	}
}

// TestEveryPathIsRefusedAcrossUsers covers the surfaces the first test does not: the write paths.
//
// A trade belonging to somebody else answers exactly as one that does not exist, so a response
// never confirms which identifiers are real — the same discipline the signal read already follows.
func TestEveryPathIsRefusedAcrossUsers(t *testing.T) {
	f := newPortfolioFixture(t)
	alices := f.record(aliceID, alfaTicker, portfolio.DirectionBuy, "100", "245.80", "39",
		f.session("XSTO", 10))

	service := f.service()
	if _, err := service.Correct(f.ctx, bobID.String(), alices.ID.String(), portfolio.RecordRequest{
		InstrumentID: f.instruments[alfaTicker], Direction: portfolio.DirectionBuy,
		Quantity: "1", Price: "1", TradeDate: f.session("XSTO", 10),
	}); !errors.Is(err, portfolio.ErrNotFound) {
		t.Errorf("bob correcting alice's trade returned %v, want not found", err)
	}
	if err := service.Withdraw(f.ctx, bobID.String(), alices.ID.String()); !errors.Is(err, portfolio.ErrNotFound) {
		t.Errorf("bob withdrawing alice's trade returned %v, want not found", err)
	}

	// Bob's own reads are simply empty, not an error: he has no portfolio, which is not a failure.
	bob := f.view(bobID)
	if len(bob.Holdings) != 0 {
		t.Errorf("bob sees %d holdings without having recorded anything", len(bob.Holdings))
	}

	// The owner role is not an administrative key here. Alice is the owner; that grants her nothing
	// over bob's portfolio, which is the distinction between ownership and administration.
	f.record(bobID, betaTicker, portfolio.DirectionBuy, "50", "88.20", "12", f.session("XHEL", 10))
	alice := f.view(aliceID)
	for _, holding := range alice.Holdings {
		if holding.Ticker == betaTicker {
			t.Errorf("the owner role granted access to another person's portfolio")
		}
	}

	// A caller with no identity at all reaches nothing.
	if _, err := service.View(f.ctx, ""); err == nil {
		t.Errorf("an unauthenticated caller read a portfolio")
	}
}
