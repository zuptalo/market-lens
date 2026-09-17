package portfolio

import (
	"fmt"
	"math/rand"
	"testing"
)

// The cost basis is tested against answers worked out by hand, because the one thing a basis cannot
// be checked against is the code that computed it.

func buy(date SessionDate, sequence int64, quantity, price, costs string) Trade {
	return Trade{InstrumentID: "i", Ticker: "T", Direction: DirectionBuy, TradeDate: date,
		Sequence: sequence, Quantity: quantity, Price: price, Costs: costs}
}

func sell(date SessionDate, sequence int64, quantity, price, costs string) Trade {
	return Trade{InstrumentID: "i", Ticker: "T", Direction: DirectionSell, TradeDate: date,
		Sequence: sequence, Quantity: quantity, Price: price, Costs: costs}
}

// TestFirstInFirstOutConsumesTheOldestShares is the whole basis in one example.
//
// The failure it guards against is last-in-first-out, or an average, either of which produces a
// plausible number that would not match what a person's tax authority expects — and the difference
// is invisible unless the lots have different prices.
func TestFirstInFirstOutConsumesTheOldestShares(t *testing.T) {
	folded, err := walk([]Trade{
		buy("2026-01-05", 1, "100", "100", "0"),
		buy("2026-02-05", 2, "100", "200", "0"),
		// Sells 150: all 100 of the first lot at 100, then 50 of the second at 200.
		// Cost consumed = 10,000 + 10,000 = 20,000. Proceeds = 150 × 250 = 37,500.
		sell("2026-03-05", 3, "150", "250", "0"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := folded.quantity.String(); got != "50.000000000000" {
		t.Errorf("remaining quantity is %s, want 50", got)
	}
	// What is left is 50 of the second lot at 200.
	if got := folded.cost.String(); got != "10000.000000000000" {
		t.Errorf("remaining cost is %s, want 10000", got)
	}
	if len(folded.realised) != 1 {
		t.Fatalf("%d realised results", len(folded.realised))
	}
	if got := folded.realised[0].Cost; got != "20000.000000000000" {
		t.Errorf("consumed cost is %s, want 20000 — last-in-first-out would say 25000", got)
	}
	if got := folded.realised[0].Realised; got != "17500.000000000000" {
		t.Errorf("realised is %s, want 17500", got)
	}
	// The window for the benchmark comparison opens at the oldest purchase still contributing, not
	// at the first purchase ever made — those shares are gone.
	if folded.costOpenedAt != "2026-02-05" {
		t.Errorf("the cost window opens at %s, want 2026-02-05", folded.costOpenedAt)
	}
}

// TestTwoPurchasesOnOneDateAreConsumedInEntryOrder is why the sequence is stored.
func TestTwoPurchasesOnOneDateAreConsumedInEntryOrder(t *testing.T) {
	folded, err := walk([]Trade{
		buy("2026-01-05", 2, "10", "300", "0"),
		buy("2026-01-05", 1, "10", "100", "0"),
		sell("2026-01-06", 3, "10", "400", "0"),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Sequence 1 was entered first, so its shares are the ones sold — regardless of the order the
	// rows happened to arrive in.
	if got := folded.realised[0].Cost; got != "1000.000000000000" {
		t.Errorf("consumed cost is %s, want 1000: the sequence decides, not the row order", got)
	}
	if got := folded.cost.String(); got != "3000.000000000000" {
		t.Errorf("remaining cost is %s, want 3000", got)
	}
}

// TestTradeCostsAreSpreadAcrossTheSharesTheyBought, so a partial sale takes a proportional share of
// them rather than all or none.
func TestTradeCostsAreSpreadAcrossTheSharesTheyBought(t *testing.T) {
	folded, err := walk([]Trade{
		buy("2026-01-05", 1, "100", "100", "100"), // unit cost 101
		sell("2026-02-05", 2, "40", "150", "20"),  // proceeds 6000 − 20 = 5980, cost 40 × 101 = 4040
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := folded.realised[0].Cost; got != "4040.000000000000" {
		t.Errorf("consumed cost is %s, want 4040", got)
	}
	if got := folded.realised[0].Realised; got != "1940.000000000000" {
		t.Errorf("realised is %s, want 1940", got)
	}
	if got := folded.cost.String(); got != "6060.000000000000" {
		t.Errorf("remaining cost is %s, want 6060", got)
	}
}

// TestRealisedAndRemainingCostReconcile is the property behind every worked example above: whatever
// sequence of trades a person records, the cost that left and the cost that stayed add up to the
// cost that went in. A basis that lost or invented cost would pass a single example and fail here.
func TestRealisedAndRemainingCostReconcile(t *testing.T) {
	random := rand.New(rand.NewSource(20260917))
	for attempt := range 200 {
		var trades []Trade
		held := 0
		purchased := decZero
		sequence := int64(0)
		for step := range 12 {
			sequence++
			date := SessionDate(fmt.Sprintf("2026-%02d-05", step+1))
			if held == 0 || random.Intn(2) == 0 {
				quantity := random.Intn(90) + 10
				price := random.Intn(400) + 50
				costs := random.Intn(60)
				trades = append(trades, buy(date, sequence,
					fmt.Sprintf("%d", quantity), fmt.Sprintf("%d", price), fmt.Sprintf("%d", costs)))
				held += quantity
				purchased = purchased.Add(mustDec(fmt.Sprintf("%d", quantity*price+costs)))
				continue
			}
			quantity := random.Intn(held) + 1
			trades = append(trades, sell(date, sequence,
				fmt.Sprintf("%d", quantity), fmt.Sprintf("%d", random.Intn(400)+50), "0"))
			held -= quantity
		}

		folded, err := walk(trades)
		if err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
		consumed := decZero
		for _, realised := range folded.realised {
			consumed = consumed.Add(mustDec(realised.Cost))
		}
		if got := consumed.Add(folded.cost); got.Cmp(purchased) != 0 {
			t.Fatalf("attempt %d: cost consumed %s plus cost remaining %s is %s, but %s was paid",
				attempt, consumed, folded.cost, got, purchased)
		}
		if folded.quantity.Sign() < 0 {
			t.Fatalf("attempt %d: a negative position was folded", attempt)
		}
	}
}
