package paper_test

import (
	"testing"

	"market-lens/server/internal/intents"
	"market-lens/server/internal/paper"
)

// TestAPromotedIntentFillsAtTheNextSessionOpen is the property the whole feature rests on.
//
// An order placed in one session fills at the *open of the next* — a price that did not exist when
// it was placed. Filling at anything the person could already see is the single easiest way to make
// a simulated account look good, and it is invisible in the resulting figures.
//
// The fixture's open is two whole units below its close on purpose, so this test can tell a correct
// implementation from one that read the close.
func TestAPromotedIntentFillsAtTheNextSessionOpen(t *testing.T) {
	f := newFixture(t)
	f.open(aliceID, "1000000", "SEK")

	placed := f.session(10)
	next := f.session(11)
	intent := f.consider(aliceID, aTicker, intents.DirectionBuy, "100", "120.00")
	order := f.promote(aliceID, intent, placed)

	// Nothing has happened yet. The price it will fill at has not been published.
	if order.State != paper.StatePending {
		t.Fatalf("a promoted order is %s, want pending", order.State)
	}
	if order.Fill != nil {
		t.Fatalf("an order filled before the pass ran")
	}

	if filled := f.fillPass(); filled != 1 {
		t.Fatalf("the pass filled %d orders, want 1", filled)
	}

	settled := f.only(f.view(aliceID))
	if settled.State != paper.StateFilled {
		t.Fatalf("the order is %s with reason %v", settled.State, settled.AbsenceReason)
	}
	if settled.Fill == nil {
		t.Fatalf("a filled order carries no fill")
	}
	if string(settled.Fill.Session) != next {
		t.Errorf("it filled at %s, want the next session %s", settled.Fill.Session, next)
	}

	// The price is the stored open of that session, unmodified. Costs are recorded separately so
	// the price itself can be checked against the bar.
	wantOpen := f.openOf(aTicker, next)
	if settled.Fill.OpenPrice != wantOpen {
		t.Errorf("it filled at %s, want the stored open %s", settled.Fill.OpenPrice, wantOpen)
	}
	// And not at the close, which is what a lookahead bug would produce.
	var close string
	if err := f.pool.QueryRow(f.ctx, `SELECT close::text FROM daily_price_bars
		WHERE instrument_id = $1 AND session_date = $2::date`,
		f.instruments[aTicker].String(), next).Scan(&close); err != nil {
		t.Fatal(err)
	}
	if settled.Fill.OpenPrice == close {
		t.Errorf("it filled at the close, which nobody could have paid at the open")
	}

	// Cash moved by the consideration and every stated cost, and by nothing else.
	if settled.Fill.CashEffect == "" || settled.Fill.CashEffect[0] != '-' {
		t.Errorf("a purchase moved cash by %s, which is not a reduction", settled.Fill.CashEffect)
	}
}
