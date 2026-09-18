package paper_test

import (
	"testing"

	"market-lens/server/internal/decimal"
	"market-lens/server/internal/intents"
)

// TestTheAccountReconcilesExactly is the property that makes the figures trustworthy.
//
// Cash plus what is held, against what the account started with, must equal the reported return —
// at twelve decimal places, not nearly. Feature 022's property test found that arithmetic which is
// only nearly right drifts over a few dozen trades, and by then nobody can say which figure is
// wrong.
func TestTheAccountReconcilesExactly(t *testing.T) {
	f := newFixture(t)
	f.open(aliceID, "1000000", "SEK")

	// A dozen fills across two instruments and several sessions, including sales that empty a lot
	// exactly and sales that take part of one.
	buy := func(ticker string, quantity string, session int) {
		intent := f.consider(aliceID, ticker, intents.DirectionBuy, quantity, "120.00")
		f.promote(aliceID, intent, f.session(session))
		f.fillPass()
	}
	sell := func(ticker string, quantity string, session int) {
		intent := f.consider(aliceID, ticker, intents.DirectionSell, quantity, "130.00")
		f.promote(aliceID, intent, f.session(session))
		f.fillPass()
	}
	buy(aTicker, "100", 5)
	buy(aTicker, "60", 7)
	buy(bTicker, "40", 8)
	sell(aTicker, "100", 10) // empties the first lot exactly
	buy(bTicker, "25", 12)
	sell(bTicker, "50", 14) // takes all of one lot and part of the next
	buy(aTicker, "30", 16)
	sell(aTicker, "45", 18)

	view := f.view(aliceID)
	if !view.Totals.Complete {
		t.Fatalf("the account is incomplete: %v", view.Totals.IncompleteReason)
	}
	if view.Totals.TotalReturn == nil {
		t.Fatal("the account states no return, and this is the one place a return is honest")
	}

	starting := decimal.MustDec(view.Account.StartingCash)
	cash := decimal.MustDec(view.Cash)
	value := decimal.MustDec(*view.Totals.Value)
	wanted := cash.Add(value).Sub(starting).Div(starting)
	if got := decimal.MustDec(*view.Totals.TotalReturn); got.Cmp(wanted) != 0 {
		t.Errorf("the return reads %s, and cash plus holdings gives %s", got, wanted)
	}

	// Cash is exactly the starting balance plus every recorded cash effect. Nothing else moves it:
	// there is no deposit and no withdrawal.
	var effects string
	if err := f.pool.QueryRow(f.ctx, `SELECT COALESCE(sum(cash_effect), 0)::text
		FROM paper_fills WHERE user_id = $1`, aliceID.String()).Scan(&effects); err != nil {
		t.Fatal(err)
	}
	if want := starting.Add(decimal.MustDec(effects)); cash.Cmp(want) != 0 {
		t.Errorf("cash reads %s, and the fills sum to %s", cash, want)
	}

	// Every holding's cost is what is left in its lots, so cost never exceeds what was paid.
	for _, holding := range view.Holdings {
		if decimal.MustDec(holding.Cost).Sign() <= 0 {
			t.Errorf("%s is held at a cost of %s", holding.Ticker, holding.Cost)
		}
	}
}

// TestSellingOutLeavesNoHoldingAndKeepsTheResult. The realised result survives the position that
// produced it; a holding that is gone is gone.
func TestSellingOutLeavesNoHoldingAndKeepsTheResult(t *testing.T) {
	f := newFixture(t)
	f.open(aliceID, "1000000", "SEK")

	bought := f.consider(aliceID, aTicker, intents.DirectionBuy, "100", "120.00")
	f.promote(aliceID, bought, f.session(5))
	f.fillPass()
	sold := f.consider(aliceID, aTicker, intents.DirectionSell, "100", "130.00")
	f.promote(aliceID, sold, f.session(15))
	f.fillPass()

	view := f.view(aliceID)
	if len(view.Holdings) != 0 {
		t.Errorf("selling out left %d holdings", len(view.Holdings))
	}
	// The fixture's closes rise steadily, so buying early and selling later made money.
	if decimal.MustDec(view.Totals.Realised).Sign() <= 0 {
		t.Errorf("a position bought low and sold high realised %s", view.Totals.Realised)
	}
	if view.Totals.Cost != "0.000000000000" {
		t.Errorf("nothing is held, and the cost reads %s", view.Totals.Cost)
	}
}

// TestTheSameOrdersAgainstTheSameBarsGiveTheSameAccount. SC-002. A simulation that cannot be
// replayed is an anecdote.
func TestTheSameOrdersAgainstTheSameBarsGiveTheSameAccount(t *testing.T) {
	run := func(t *testing.T) (string, string, string) {
		f := newFixture(t)
		f.open(aliceID, "1000000", "SEK")
		for _, step := range []struct {
			ticker   string
			side     intents.Direction
			quantity string
			session  int
		}{
			{aTicker, intents.DirectionBuy, "100", 5},
			{bTicker, intents.DirectionBuy, "70", 6},
			{aTicker, intents.DirectionSell, "40", 12},
			{euroTicker, intents.DirectionBuy, "25", 14},
		} {
			intent := f.consider(aliceID, step.ticker, step.side, step.quantity, "125.00")
			f.promote(aliceID, intent, f.session(step.session))
			f.fillPass()
		}
		view := f.view(aliceID)
		return view.Cash, *view.Totals.Value, *view.Totals.TotalReturn
	}

	firstCash, firstValue, firstReturn := run(t)
	secondCash, secondValue, secondReturn := run(t)

	if firstCash != secondCash || firstValue != secondValue || firstReturn != secondReturn {
		t.Errorf("two runs over the same bars disagreed:\n  %s / %s / %s\n  %s / %s / %s",
			firstCash, firstValue, firstReturn, secondCash, secondValue, secondReturn)
	}
}

// TestAHoldingInAnotherCurrencyIsConvertedNotIgnored. The euro instrument lists in something the
// Swedish account does not use, so its fill crosses a currency and pays a spread for doing so.
func TestAHoldingInAnotherCurrencyIsConvertedNotIgnored(t *testing.T) {
	f := newFixture(t)
	f.open(aliceID, "1000000", "SEK")

	intent := f.consider(aliceID, euroTicker, intents.DirectionBuy, "100", "120.00")
	f.promote(aliceID, intent, f.session(10))
	f.fillPass()

	order := f.only(f.view(aliceID))
	if order.Fill == nil {
		t.Fatalf("the order is %s with reason %v", order.State, order.AbsenceReason)
	}
	// The rate is recorded with the fill, so the conversion can be checked rather than believed.
	if order.Fill.ConversionRate == "1.000000000000" {
		t.Errorf("a euro holding in a krona account converted at 1")
	}
	// A euro price converted into kronor is a larger number, so the cash effect is larger than the
	// price times the quantity would be in euros.
	if decimal.MustDec(order.Fill.CashEffect).Neg().Cmp(
		decimal.MustDec(order.Fill.OpenPrice).Mul(decimal.MustDec("100"))) <= 0 {
		t.Errorf("the cash effect %s does not look converted", order.Fill.CashEffect)
	}
}
