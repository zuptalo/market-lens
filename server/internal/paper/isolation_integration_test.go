package paper_test

import (
	"errors"
	"testing"

	"market-lens/server/internal/intents"
	"market-lens/server/internal/paper"
)

// TestAPersonSeesOnlyTheirOwnPaperAccount.
//
// The fill pass is the one thing in this package that acts for everybody at once, so it is the one
// place a scoping mistake would be invisible: one person's pass writing into another's account
// would look exactly like a working feature until somebody compared two screens.
func TestAPersonSeesOnlyTheirOwnPaperAccount(t *testing.T) {
	f := newFixture(t)
	f.open(aliceID, "1000000", "SEK")
	f.open(bobID, "500000", "EUR")

	alicesIntent := f.consider(aliceID, aTicker, intents.DirectionBuy, "100", "120.00")
	alices := f.promote(aliceID, alicesIntent, f.session(10))
	bobsIntent := f.consider(bobID, bTicker, intents.DirectionBuy, "40", "150.00")
	f.promote(bobID, bobsIntent, f.session(10))

	if filled := f.fillPass(); filled != 2 {
		t.Fatalf("the pass filled %d orders, want one each", filled)
	}

	alice := f.view(aliceID)
	bob := f.view(bobID)
	if len(alice.Orders) != 1 || alice.Orders[0].Ticker != aTicker {
		t.Fatalf("alice's orders are %+v", alice.Orders)
	}
	if len(bob.Orders) != 1 || bob.Orders[0].Ticker != bTicker {
		t.Fatalf("bob's orders are %+v", bob.Orders)
	}
	// Each account moved by its own fill and by nothing else.
	if alice.Account.StartingCash != "1000000.000000000000" ||
		bob.Account.StartingCash != "500000.000000000000" {
		t.Errorf("the accounts read %s and %s", alice.Account.StartingCash, bob.Account.StartingCash)
	}
	if alice.Cash == "1000000.000000000000" {
		t.Errorf("alice's cash did not move")
	}
	if bob.Cash == "500000.000000000000" {
		t.Errorf("bob's cash did not move")
	}

	// Every fill is owned by the person whose order it was. The pass writes under each account's
	// own identifier, never a shared one.
	if strays := f.count(`SELECT count(*) FROM paper_fills f
		JOIN paper_orders o ON o.id = f.order_id WHERE f.user_id <> o.user_id`); strays != 0 {
		t.Errorf("%d fills are owned by somebody other than the order's owner", strays)
	}

	// Cancelling somebody else's order answers exactly as cancelling one that does not exist.
	if err := f.service().Cancel(f.ctx, bobID.String(), string(alices.ID)); !errors.Is(err, paper.ErrNotFound) {
		t.Errorf("bob cancelling alice's order returned %v, want not found", err)
	}

	// Events are scoped to one person each.
	if mine := f.count(`SELECT count(*) FROM client_events
		WHERE event_type = $1 AND scope = 'user' AND subject_user_id = $2`,
		paper.EventChanged, aliceID.String()); mine == 0 {
		t.Errorf("alice's account published no event scoped to her")
	}
	if leaked := f.count(`SELECT count(*) FROM client_events
		WHERE event_type = $1 AND (scope <> 'user' OR subject_user_id IS NULL)`,
		paper.EventChanged); leaked != 0 {
		t.Errorf("%d paper events are not scoped to one person", leaked)
	}

	if _, err := f.service().View(f.ctx, ""); err == nil {
		t.Errorf("an unauthenticated caller read a paper account")
	}
}

// TestAPersonHasOneAccountAndCannotRetuneIt. Being able to reset it, or open a second one, turns a
// track record into a collection of attempts — and the best attempt is the one that gets shown.
func TestAPersonHasOneAccountAndCannotRetuneIt(t *testing.T) {
	f := newFixture(t)
	f.open(aliceID, "1000000", "SEK")

	_, err := f.service().Open(f.ctx, aliceID.String(), paper.OpenRequest{
		StartingCash: "9000000", AccountingCurrency: "SEK",
	})
	var refusal paper.Refusal
	if !errors.As(err, &refusal) || refusal.Code != paper.RefusalAccountAlreadyOpen {
		t.Errorf("opening a second account returned %v", err)
	}
	if accounts := f.count(`SELECT count(*) FROM paper_accounts WHERE user_id = $1`,
		aliceID.String()); accounts != 1 {
		t.Errorf("%d accounts exist for one person", accounts)
	}

	for _, bad := range []paper.OpenRequest{
		{StartingCash: "0", AccountingCurrency: "SEK"},
		{StartingCash: "-1", AccountingCurrency: "SEK"},
		{StartingCash: "1000", AccountingCurrency: "kronor"},
	} {
		if _, err := f.service().Open(f.ctx, bobID.String(), bad); !errors.As(err, &refusal) {
			t.Errorf("%+v returned %v, want a refusal", bad, err)
		}
	}
}

// TestPromotingWithoutAnAccountSaysSo. A person who has written intents down but never opened an
// account is told what to do, not handed an internal failure.
func TestPromotingWithoutAnAccountSaysSo(t *testing.T) {
	f := newFixture(t)
	intent := f.consider(aliceID, aTicker, intents.DirectionBuy, "100", "120.00")

	_, err := f.service().Promote(f.ctx, aliceID.String(), paper.PromoteRequest{
		IntentID: paper.UUID(intent.ID), PlacedSession: paper.SessionDate(f.session(10)),
	})
	var refusal paper.Refusal
	if !errors.As(err, &refusal) || refusal.Code != paper.RefusalNoAccount {
		t.Errorf("promoting without an account returned %v", err)
	}

	// And somebody else's intent is not promotable at all.
	f.open(bobID, "1000000", "SEK")
	if _, err := f.service().Promote(f.ctx, bobID.String(), paper.PromoteRequest{
		IntentID: paper.UUID(intent.ID), PlacedSession: paper.SessionDate(f.session(10)),
	}); !errors.Is(err, paper.ErrNotFound) {
		t.Errorf("bob promoted alice's intent: %v", err)
	}
}
