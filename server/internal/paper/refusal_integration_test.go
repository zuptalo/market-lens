package paper_test

import (
	"errors"
	"testing"

	"market-lens/server/internal/intents"
	"market-lens/server/internal/paper"
)

// TestABuyTheAccountCannotAffordIsRefused. Cash is tracked here, unlike feature 022, because an
// account that cannot run out of money measures nothing — every order fills, and position sizing
// stops mattering entirely.
func TestABuyTheAccountCannotAffordIsRefused(t *testing.T) {
	f := newFixture(t)
	f.open(aliceID, "1000", "SEK")

	intent := f.consider(aliceID, aTicker, intents.DirectionBuy, "100", "120.00")
	f.promote(aliceID, intent, f.session(10))
	f.fillPass()

	order := f.only(f.view(aliceID))
	if order.State != paper.StateUnfillable {
		t.Fatalf("an order beyond the balance is %s", order.State)
	}
	if order.AbsenceReason == nil || *order.AbsenceReason != paper.AbsenceInsufficientCash {
		t.Errorf("it was refused for %v, want insufficient cash", order.AbsenceReason)
	}
	if order.Fill != nil {
		t.Errorf("an order that could not be afforded recorded a fill")
	}

	// Nothing moved. A refused order must not take a slice of the balance with it.
	view := f.view(aliceID)
	if view.Cash != "1000.000000000000" {
		t.Errorf("cash reads %s after a refusal", view.Cash)
	}
	if len(view.Holdings) != 0 {
		t.Errorf("a refused order produced %d holdings", len(view.Holdings))
	}
}

// TestASaleLargerThanThePositionIsRefusedRatherThanShorted. A paper account cannot go short, and it
// does not quietly sell what there is instead: a partial fill nobody asked for is a decision the
// product would be making on somebody's behalf.
func TestASaleLargerThanThePositionIsRefusedRatherThanShorted(t *testing.T) {
	f := newFixture(t)
	f.open(aliceID, "1000000", "SEK")

	bought := f.consider(aliceID, aTicker, intents.DirectionBuy, "100", "120.00")
	f.promote(aliceID, bought, f.session(10))
	f.fillPass()

	sold := f.consider(aliceID, aTicker, intents.DirectionSell, "150", "130.00")
	f.promote(aliceID, sold, f.session(12))
	f.fillPass()

	var refused *paper.Order
	for _, order := range f.view(aliceID).Orders {
		if order.Direction == paper.DirectionSell {
			copy := order
			refused = &copy
		}
	}
	if refused == nil {
		t.Fatal("the sale is not in the account")
	}
	if refused.State != paper.StateUnfillable {
		t.Fatalf("selling 150 of 100 held is %s", refused.State)
	}
	if refused.AbsenceReason == nil || *refused.AbsenceReason != paper.AbsenceExceedsPosition {
		t.Errorf("it was refused for %v, want exceeds position", refused.AbsenceReason)
	}

	// The position is untouched, and no short exists.
	holdings := f.view(aliceID).Holdings
	if len(holdings) != 1 || holdings[0].Quantity != "100.000000000000" {
		t.Errorf("the holding reads %+v", holdings)
	}
}

// TestAnIntentBecomesAtMostOneOrder. Enforced by the index rather than by a read-then-write, which
// is what makes it true when two requests arrive at once.
func TestAnIntentBecomesAtMostOneOrder(t *testing.T) {
	f := newFixture(t)
	f.open(aliceID, "1000000", "SEK")
	intent := f.consider(aliceID, aTicker, intents.DirectionBuy, "100", "120.00")
	f.promote(aliceID, intent, f.session(10))

	_, err := f.service().Promote(f.ctx, aliceID.String(), paper.PromoteRequest{
		IntentID: paper.UUID(intent.ID), PlacedSession: paper.SessionDate(f.session(11)),
	})
	var refusal paper.Refusal
	if !errors.As(err, &refusal) {
		t.Fatalf("promoting the same intent twice returned %v", err)
	}
	if refusal.Code != paper.RefusalIntentAlreadyPromoted &&
		refusal.Code != paper.RefusalIntentNotConsidering {
		t.Errorf("it was refused with %q", refusal.Code)
	}
	if orders := f.count(`SELECT count(*) FROM paper_orders`); orders != 1 {
		t.Errorf("%d orders exist for one intent", orders)
	}
}

// TestPromotingAnIntentSettlesIt. The intent's own history has to stay truthful: it was acted on,
// and what it became is recorded.
func TestPromotingAnIntentSettlesIt(t *testing.T) {
	f := newFixture(t)
	f.open(aliceID, "1000000", "SEK")
	intent := f.consider(aliceID, aTicker, intents.DirectionBuy, "100", "120.00")
	f.promote(aliceID, intent, f.session(10))

	report, err := f.intentsService().Report(f.ctx, aliceID.String(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Intents) != 1 {
		t.Fatalf("%d intents", len(report.Intents))
	}
	if report.Intents[0].Status != intents.StatusActedOn {
		t.Errorf("the promoted intent reads %s", report.Intents[0].Status)
	}
	// And it is no longer among what is under consideration.
	considering, err := f.intentsService().Report(f.ctx, aliceID.String(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(considering.Intents) != 0 {
		t.Errorf("a promoted intent is still being considered")
	}
}

// TestTheProductCreatesNoOrders is the requirement this feature most needs to still be true in a
// year, and reading the code is not proof. FR-001, SC-003.
func TestTheProductCreatesNoOrders(t *testing.T) {
	f := newFixture(t)
	f.open(aliceID, "1000000", "SEK")
	// An account, a written-down intent nobody promoted, and the pass that runs after every import.
	f.consider(aliceID, aTicker, intents.DirectionBuy, "100", "120.00")
	f.consider(aliceID, bTicker, intents.DirectionBuy, "50", "140.00")
	f.fillPass()
	f.fillPass()

	if orders := f.count(`SELECT count(*) FROM paper_orders`); orders != 0 {
		t.Errorf("%d orders exist that nobody promoted", orders)
	}
	if fills := f.count(`SELECT count(*) FROM paper_fills`); fills != 0 {
		t.Errorf("%d fills exist for orders nobody placed", fills)
	}
	view := f.view(aliceID)
	if len(view.Orders) != 0 || len(view.Holdings) != 0 {
		t.Errorf("the account holds something nobody asked for: %+v", view)
	}
	if !view.IsASimulation {
		t.Errorf("the account does not say it is a simulation")
	}
}

// TestAFilledOrderIsPermanent. A track record that can be edited after the fact measures nothing.
func TestAFilledOrderIsPermanent(t *testing.T) {
	f := newFixture(t)
	f.open(aliceID, "1000000", "SEK")
	intent := f.consider(aliceID, aTicker, intents.DirectionBuy, "100", "120.00")
	order := f.promote(aliceID, intent, f.session(10))

	// Pending, it can be withdrawn.
	if err := f.service().Cancel(f.ctx, aliceID.String(), string(order.ID)); err != nil {
		t.Fatalf("cancel a pending order: %v", err)
	}
	if state := f.only(f.view(aliceID)).State; state != paper.StateCancelled {
		t.Fatalf("the cancelled order reads %s", state)
	}
	// And a cancelled order never fills.
	if filled := f.fillPass(); filled != 0 {
		t.Errorf("the pass filled %d cancelled orders", filled)
	}

	second := f.consider(aliceID, bTicker, intents.DirectionBuy, "10", "150.00")
	filledOrder := f.promote(aliceID, second, f.session(10))
	f.fillPass()

	if err := f.service().Cancel(f.ctx, aliceID.String(), string(filledOrder.ID)); err == nil {
		t.Errorf("a filled order was cancelled")
	}
	if fills := f.count(`SELECT count(*) FROM paper_fills`); fills != 1 {
		t.Errorf("%d fills after trying to cancel one", fills)
	}
}

// TestAnOrderWithNoPriceWaitsRatherThanFailing. The day after an order is placed there is often no
// bar yet, and that is ordinary rather than a verdict.
func TestAnOrderWithNoPriceWaitsRatherThanFailing(t *testing.T) {
	f := newFixture(t)
	f.open(aliceID, "1000000", "SEK")
	// Placed at the last session the fixture stored, so nothing follows it yet.
	last := f.session(fixtureSessions - 1)
	intent := f.consider(aliceID, aTicker, intents.DirectionBuy, "100", "120.00")
	f.promote(aliceID, intent, last)

	if filled := f.fillPass(); filled != 0 {
		t.Errorf("an order with no later session filled")
	}
	order := f.only(f.view(aliceID))
	if order.State != paper.StatePending {
		t.Errorf("an order still waiting for a price reads %s with reason %v",
			order.State, order.AbsenceReason)
	}
}
