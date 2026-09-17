package portfolio_test

import (
	"errors"
	"testing"

	"market-lens/server/internal/portfolio"
)

// Currency conversion in a personal portfolio is new behaviour. Feature 021 avoided it by keeping
// backtesting in euro, because every stored rate has the euro as its base. A personal tracker
// cannot: telling somebody in Stockholm their holdings are worth €48,000 answers a question nobody
// asked. So a holding in a third currency crosses through the euro, in one session.

func TestConversionCrossesThroughTheEuroInOneSession(t *testing.T) {
	f := newPortfolioFixture(t)
	if _, err := f.service().SetCurrency(f.ctx, aliceID.String(), "SEK"); err != nil {
		t.Fatalf("set currency: %v", err)
	}

	// A Danish holding in a Swedish portfolio: DKK → EUR → SEK. The fixture stores EURDKK at 7.5
	// and EURSEK at 11, so one krone is worth 11/7.5 = 1.466666666667 kronor.
	f.record(aliceID, danaTicker, portfolio.DirectionBuy, "100", "100.00", "0", f.session("XCSE", 5))

	view := f.view(aliceID)
	if len(view.Holdings) != 1 {
		t.Fatalf("%d holdings", len(view.Holdings))
	}
	holding := view.Holdings[0]
	if holding.Valuation.AbsenceReason != nil {
		t.Fatalf("the holding is unvalued: %s", *holding.Valuation.AbsenceReason)
	}
	if holding.Valuation.ConversionRate == nil {
		t.Fatalf("a converted holding records no rate, so nobody can reproduce the figure")
	}
	if got := *holding.Valuation.ConversionRate; got != "1.466666666667" {
		t.Errorf("the recorded rate is %s, want 1.466666666667", got)
	}

	// The Danish instrument's last stored close is 115.00 + 39 × 1.00 = 154.00 DKK.
	// 100 shares × 154 = 15,400 DKK → ÷7.5 = 2,053.333333333333 EUR → ×11 = 22,586.666666666663 SEK.
	// Worked through in two steps here because that is what the conversion does: the euro amount is
	// rounded to stored precision before the second leg reads it, so a single combined multiply
	// would disagree in the last places.
	if got := *holding.Valuation.Value; got != "22586.666666666663" {
		t.Errorf("the converted value is %s, want 22586.666666666663", got)
	}

	// A euro holding in the same portfolio crosses one leg, not two.
	f.record(aliceID, betaTicker, portfolio.DirectionBuy, "10", "100.00", "0", f.session("XHEL", 5))
	view = f.view(aliceID)
	for _, held := range view.Holdings {
		if held.Currency == "EUR" && held.Valuation.ConversionRate == nil {
			t.Errorf("a euro holding in a Swedish portfolio recorded no rate")
		}
	}
}

func TestASingleCurrencyPortfolioNeedsNoRate(t *testing.T) {
	f := newPortfolioFixture(t)
	if _, err := f.service().SetCurrency(f.ctx, aliceID.String(), "EUR"); err != nil {
		t.Fatalf("set currency: %v", err)
	}
	f.record(aliceID, betaTicker, portfolio.DirectionBuy, "10", "100.00", "0", f.session("XHEL", 5))

	// The Finnish listing is already in euro, so nothing converts and no rate is recorded.
	view := f.view(aliceID)
	if view.Holdings[0].Valuation.ConversionRate != nil {
		t.Errorf("a same-currency holding recorded a conversion rate")
	}
	if view.Holdings[0].Valuation.AbsenceReason != nil {
		t.Errorf("a same-currency holding could not be valued: %s", *view.Holdings[0].Valuation.AbsenceReason)
	}
}

// TestAMissingRateIsStatedNotCarriedForward. Yesterday's rate is not today's, and using it would
// value somebody's money at a number nobody quoted.
func TestAMissingRateIsStatedNotCarriedForward(t *testing.T) {
	f := newPortfolioFixture(t)
	if _, err := f.service().SetCurrency(f.ctx, aliceID.String(), "SEK"); err != nil {
		t.Fatalf("set currency: %v", err)
	}
	f.record(aliceID, danaTicker, portfolio.DirectionBuy, "100", "100.00", "0", f.session("XCSE", 5))

	// The rate for the session the holding would be valued at stops being stored.
	latest := f.session("XCSE", 39)
	f.exec(`DELETE FROM fx_rates WHERE quote = 'DKK' AND session_date = $1::date`, latest.String())

	view := f.view(aliceID)
	holding := view.Holdings[0]
	if holding.Valuation.AbsenceReason == nil || *holding.Valuation.AbsenceReason != portfolio.ValuationNoRate {
		t.Fatalf("a holding with no rate for its session reads %+v", holding.Valuation)
	}
	if holding.Valuation.Value != nil {
		t.Errorf("an unrateable holding carries a value anyway")
	}
	if view.Totals.Complete {
		t.Errorf("the total is complete despite a holding it could not convert")
	}
}

// TestACurrencyTheProductCannotConvertIsRefused. Accepting one would leave every holding
// permanently unvalued with the person unable to see why.
func TestACurrencyTheProductCannotConvertIsRefused(t *testing.T) {
	f := newPortfolioFixture(t)
	_, err := f.service().SetCurrency(f.ctx, aliceID.String(), "JPY")
	var refusal portfolio.Refusal
	if !errors.As(err, &refusal) || refusal.Code != portfolio.RefusalUnsupportedCurrency {
		t.Errorf("an unconvertible currency returned %v", err)
	}
	if _, err := f.service().SetCurrency(f.ctx, aliceID.String(), "not a currency"); !errors.As(err, &refusal) {
		t.Errorf("a malformed currency returned %v", err)
	}
}
