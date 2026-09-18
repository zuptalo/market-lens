package intents_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"market-lens/server/internal/intents"
	"market-lens/server/internal/portfolio"
)

func TestAPersonSeesOnlyTheirOwnIntents(t *testing.T) {
	f := newIntentsFixture(t)
	alices := f.consider(aliceID, betaTicker, intents.DirectionBuy, "40", "130.00")
	f.consider(bobID, alfaTicker, intents.DirectionSell, "10", "120.00")

	alice := f.report(aliceID, true)
	bob := f.report(bobID, true)
	if len(alice.Intents) != 1 || alice.Intents[0].Ticker != betaTicker {
		t.Fatalf("alice's intents are %+v", alice.Intents)
	}
	if len(bob.Intents) != 1 || bob.Intents[0].Ticker != alfaTicker {
		t.Fatalf("bob's intents are %+v", bob.Intents)
	}

	// Settling somebody else's answers exactly as settling one that does not exist.
	if err := f.service().Settle(f.ctx, bobID.String(), alices.ID.String(),
		intents.StatusWithdrawn); !errors.Is(err, intents.ErrNotFound) {
		t.Errorf("bob settling alice's intent returned %v, want not found", err)
	}
	if f.report(aliceID, false).Intents[0].Status != intents.StatusConsidering {
		t.Errorf("alice's intent was settled by somebody else")
	}

	// Events are scoped to one person each.
	if mine := f.count(`SELECT count(*) FROM client_events
		WHERE event_type = $1 AND scope = 'user' AND subject_user_id = $2`,
		intents.EventChanged, aliceID.String()); mine == 0 {
		t.Errorf("alice's intent published no event scoped to her")
	}
	if leaked := f.count(`SELECT count(*) FROM client_events
		WHERE event_type = $1 AND (scope <> 'user' OR subject_user_id IS NULL)`,
		intents.EventChanged); leaked != 0 {
		t.Errorf("%d intent events are not scoped to one person", leaked)
	}

	if _, err := f.service().Report(f.ctx, "", false); err == nil {
		t.Errorf("an unauthenticated caller read intents")
	}
}

func TestSettlingAnIntentRecordsNoTrade(t *testing.T) {
	f := newIntentsFixture(t)
	f.record(aliceID, betaTicker, portfolio.DirectionBuy, "100", "100.00", "0", f.session("XHEL", 5))
	intent := f.consider(aliceID, betaTicker, intents.DirectionBuy, "40", "130.00")

	before := f.view(aliceID).Holdings[0].Quantity
	if err := f.service().Settle(f.ctx, aliceID.String(), intent.ID.String(), intents.StatusActedOn); err != nil {
		t.Fatalf("settle: %v", err)
	}

	// Acting on an intent records that the person acted. What they actually paid is a fact only
	// they can assert, so the portfolio is unchanged until they record the trade themselves.
	after := f.view(aliceID)
	if after.Holdings[0].Quantity != before {
		t.Errorf("the portfolio moved from %s to %s because an intent was marked acted on",
			before, after.Holdings[0].Quantity)
	}
	if trades := f.count(`SELECT count(*) FROM portfolio_trades WHERE user_id = $1`,
		aliceID.String()); trades != 1 {
		t.Errorf("%d trades exist; settling an intent created one", trades)
	}

	// It stays readable, with when.
	settled := f.only(f.report(aliceID, true))
	if settled.Status != intents.StatusActedOn || settled.SettledAt == nil {
		t.Errorf("the settled intent reads %+v", settled)
	}
	// And it is no longer evaluated: the question has stopped being asked.
	if settled.Consequence != nil {
		t.Errorf("a settled intent still carries a consequence")
	}
	// Nor does it appear among what is still under consideration.
	if len(f.report(aliceID, false).Intents) != 0 {
		t.Errorf("a settled intent is still listed as under consideration")
	}

	// Settling it again is refused rather than silently repeated.
	var refusal intents.Refusal
	if err := f.service().Settle(f.ctx, aliceID.String(), intent.ID.String(),
		intents.StatusWithdrawn); !errors.As(err, &refusal) {
		t.Errorf("settling a settled intent returned %v", err)
	}
}

func TestTheProductCreatesNoIntents(t *testing.T) {
	f := newIntentsFixture(t)
	// A portfolio, a limit, signals, and a strategy that has an opinion — and still nothing.
	f.record(aliceID, betaTicker, portfolio.DirectionBuy, "100", "100.00", "0", f.session("XHEL", 5))
	f.limit(aliceID, "instrument_share", "0.10")

	if stored := f.count(`SELECT count(*) FROM order_intents`); stored != 0 {
		t.Errorf("%d intents exist that nobody wrote", stored)
	}
	report := f.report(aliceID, true)
	if len(report.Intents) != 0 {
		t.Errorf("the product proposed %d intents", len(report.Intents))
	}
	if !report.RecordsWhatYouAreConsidering {
		t.Errorf("the report does not state what it records")
	}
}

// TestNothingAnIntentCarriesCouldBeActedOn. FR-006 asserted on the shape rather than trusted to
// review: a field that exists will eventually be filled in, and then sent.
func TestNothingAnIntentCarriesCouldBeActedOn(t *testing.T) {
	f := newIntentsFixture(t)
	f.consider(aliceID, betaTicker, intents.DirectionBuy, "40", "130.00")

	encoded, err := json.Marshal(f.only(f.report(aliceID, false)))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"venue", "order_type", "ordertype", "time_in_force", "timeinforce", "destination",
		"broker", "expires", "limit_price", "stop_price", "submit", "route",
	} {
		if strings.Contains(strings.ToLower(string(encoded)), forbidden) {
			t.Errorf("an intent carries %q: %s", forbidden, encoded)
		}
	}
}

func TestAnIntentInSomethingTheProductDoesNotCarryIsRefused(t *testing.T) {
	f := newIntentsFixture(t)
	_, err := f.service().Record(f.ctx, aliceID.String(), intents.RecordRequest{
		InstrumentID: "00000000-0000-4000-8000-00000000dead",
		Direction:    intents.DirectionBuy, Quantity: "10", Price: "10",
	})
	var refusal intents.Refusal
	if !errors.As(err, &refusal) || refusal.Code != intents.RefusalInstrumentNotCarried {
		t.Errorf("an uncarried instrument returned %v", err)
	}
	for _, bad := range []intents.RecordRequest{
		{Direction: intents.DirectionBuy, Quantity: "0", Price: "10"},
		{Direction: intents.DirectionBuy, Quantity: "10", Price: "-1"},
		{Direction: "short", Quantity: "10", Price: "10"},
	} {
		bad.InstrumentID = f.instruments[betaTicker]
		if _, err := f.service().Record(f.ctx, aliceID.String(), bad); !errors.As(err, &refusal) {
			t.Errorf("%+v returned %v, want a refusal", bad, err)
		}
	}
	if stored := f.count(`SELECT count(*) FROM order_intents`); stored != 0 {
		t.Errorf("a refused intent was stored anyway")
	}
}
