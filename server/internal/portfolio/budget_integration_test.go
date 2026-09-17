package portfolio_test

import (
	"testing"
	"time"

	"market-lens/server/internal/portfolio"
)

// TestAPortfolioOfManyTradesReadsWithinItsBudget.
//
// Every figure here is a fold over the trades rather than a stored number, which is the decision
// that makes a correction a re-read instead of a repair. The risk that buys is a read whose cost
// grows with the history — a per-trade query, or a per-holding rate lookup — and that is what this
// measures. The budget is deliberately generous: it exists to catch an order-of-magnitude mistake,
// not to police milliseconds on a shared machine.
func TestAPortfolioOfManyTradesReadsWithinItsBudget(t *testing.T) {
	f := newPortfolioFixture(t)
	if _, err := f.service().SetCurrency(f.ctx, aliceID.String(), "SEK"); err != nil {
		t.Fatalf("set currency: %v", err)
	}

	// Three instruments across three currencies, so every read path is exercised: two of them
	// convert through the euro and one does not.
	tickers := []string{alfaTicker, betaTicker, danaTicker}
	mics := map[string]string{alfaTicker: "XSTO", betaTicker: "XHEL", danaTicker: "XCSE"}
	const perInstrument = 100

	for _, ticker := range tickers {
		for index := range perInstrument {
			session := f.session(mics[ticker], index%35)
			direction := portfolio.DirectionBuy
			quantity := "10"
			// Sell a little back every few trades, so the fold actually consumes lots rather than
			// walking an append-only list.
			if index%4 == 3 {
				direction, quantity = portfolio.DirectionSell, "5"
			}
			f.record(aliceID, ticker, direction, quantity, "120.00", "9", session)
		}
	}

	started := time.Now()
	view := f.view(aliceID)
	elapsed := time.Since(started)

	if len(view.Holdings) != len(tickers) {
		t.Fatalf("%d holdings from %d instruments", len(view.Holdings), len(tickers))
	}
	if view.Totals.Value == nil {
		t.Fatalf("the portfolio could not be valued: %+v", view.Totals)
	}
	// 300 trades across 3 instruments. A per-trade query would be hundreds of round trips and
	// would not come close to this.
	if elapsed > 2*time.Second {
		t.Errorf("reading %d trades took %s; the cost is growing with the history", perInstrument*len(tickers), elapsed)
	}
	t.Logf("read %d trades across %d holdings in %s", perInstrument*len(tickers), len(view.Holdings), elapsed)
}
