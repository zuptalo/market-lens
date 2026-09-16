package backtest_test

import (
	"testing"
)

// Currency conversion is new behaviour in this product, and confined to backtesting. Nothing else
// converts: the Markets screens still state every price in its listing currency, and this does not
// change that. A portfolio holding instruments from four markets simply cannot avoid the question.

// TestASingleCurrencyBacktestNeedsNoRate is FR-013. It matters because the alternative — always
// converting, with a rate of one where the currencies match — would make a missing rate table a
// silent failure rather than an obvious one.
func TestASingleCurrencyBacktestNeedsNoRate(t *testing.T) {
	f := newBacktestFixture(t)
	f.exec(`DELETE FROM fx_rates`)
	// The Finnish listings are already in the accounting currency, so the run needs no rate at
	// all — and with none stored, anything that reached for one would fail loudly.
	f.confineToCurrency("EUR")

	run := f.run()

	if run.TradeCount == 0 {
		t.Fatalf("no trades in the single-currency universe, so this proves nothing")
	}
	if converted := f.count(`SELECT count(*) FROM backtest_trades
		WHERE run_id = $1 AND fx_rate IS NOT NULL`, run.ID.String()); converted != 0 {
		t.Errorf("%d trades recorded a conversion in a single-currency backtest", converted)
	}
	if charged := f.count(`SELECT count(*) FROM backtest_trades
		WHERE run_id = $1 AND spread_cost <> 0`, run.ID.String()); charged != 0 {
		t.Errorf("%d trades paid a currency spread without crossing a currency", charged)
	}
	if unrated := f.count(`SELECT count(*) FROM backtest_skips
		WHERE run_id = $1 AND reason = 'no_rate'`, run.ID.String()); unrated != 0 {
		t.Errorf("%d signals were skipped for want of a rate that was never needed", unrated)
	}
	if unvalued := f.count(`SELECT count(*) FROM backtest_positions
		WHERE run_id = $1 AND absence_reason = 'no_rate'`, run.ID.String()); unvalued != 0 {
		t.Errorf("%d positions went unvalued for want of a rate that was never needed", unvalued)
	}
}

// TestConversionUsesTheSessionsOwnRateAndRecordsIt is FR-012. The rate is stored on the trade
// because a conversion nobody can reproduce is a number, not a fact.
func TestConversionUsesTheSessionsOwnRateAndRecordsIt(t *testing.T) {
	f := newBacktestFixture(t)
	run := f.run()

	crossed := f.count(`SELECT count(*) FROM backtest_trades t
		JOIN instruments i ON i.id = t.instrument_id
		WHERE t.run_id = $1 AND i.currency <> 'EUR'`, run.ID.String())
	if crossed == 0 {
		t.Fatalf("no cross-currency trades, so this proves nothing")
	}
	if bare := f.count(`SELECT count(*) FROM backtest_trades t
		JOIN instruments i ON i.id = t.instrument_id
		WHERE t.run_id = $1 AND i.currency <> 'EUR' AND t.fx_rate IS NULL`,
		run.ID.String()); bare != 0 {
		t.Errorf("%d cross-currency trades recorded no rate", bare)
	}
	// The rate is the execution session's own, never an earlier one carried forward.
	if wrong := f.count(`SELECT count(*) FROM backtest_trades t
		JOIN instruments i ON i.id = t.instrument_id
		WHERE t.run_id = $1 AND t.fx_rate IS NOT NULL
		  AND t.fx_rate IS DISTINCT FROM (SELECT r.rate FROM fx_rates r
			WHERE r.base = 'EUR' AND r.quote = i.currency AND r.session_date = t.execution_session)`,
		run.ID.String()); wrong != 0 {
		t.Errorf("%d trades converted at a rate from another session", wrong)
	}
	// And the cash a converted trade moved is the consideration divided by that rate, plus costs.
	if unreconciled := f.count(`SELECT count(*) FROM backtest_trades t
		WHERE t.run_id = $1 AND t.fx_rate IS NOT NULL AND t.direction = 'buy'
		  AND t.cash_effect <> -(round(t.quantity * t.price / t.fx_rate, 12)
			+ t.slippage + t.spread_cost + t.brokerage)`, run.ID.String()); unreconciled != 0 {
		t.Errorf("%d converted purchases do not reconcile with their stated rate", unreconciled)
	}
	// A converted trade bears the stated spread; the spread is the cost of crossing, so an
	// unconverted one bears none.
	if free := f.count(`SELECT count(*) FROM backtest_trades
		WHERE run_id = $1 AND fx_rate IS NOT NULL AND spread_cost = 0`, run.ID.String()); free != 0 {
		t.Errorf("%d conversions were free under a configuration that states a spread", free)
	}
}

// TestAMissingRateIsStatedNotCarriedForward is the honesty rule applied to currency. Yesterday's
// rate is not today's, and using it would value a position at a number nobody quoted.
func TestAMissingRateIsStatedNotCarriedForward(t *testing.T) {
	f := newBacktestFixture(t)
	reference := f.run()

	// Take the rate away from the sessions after a Swedish position was established, leaving the
	// earlier ones alone so the position is genuinely held when the rate stops.
	var held string
	if err := f.pool.QueryRow(f.ctx, `SELECT min(p.session_date)::text
		FROM backtest_positions p JOIN instruments i ON i.id = p.instrument_id
		WHERE p.run_id = $1 AND i.currency = 'SEK' AND p.value IS NOT NULL`,
		reference.ID.String()).Scan(&held); err != nil || held == "" {
		t.Fatalf("no Swedish position was ever valued, so this proves nothing: %v", err)
	}
	f.exec(`DELETE FROM fx_rates WHERE quote = 'SEK' AND session_date > $1::date`, held)

	run := f.run()

	if unvalued := f.count(`SELECT count(*) FROM backtest_positions
		WHERE run_id = $1 AND absence_reason = 'no_rate'`, run.ID.String()); unvalued == 0 {
		t.Errorf("a position kept a value after the rate that valued it stopped being stored")
	}
	if incomplete := f.count(`SELECT count(*) FROM backtest_equity
		WHERE run_id = $1 AND absence_reason = 'position_unvalued'`, run.ID.String()); incomplete == 0 {
		t.Errorf("the equity curve stated a total for a session it had no rate for")
	}
	// No trade ever converts at a rate that is not stored for its own session.
	if invented := f.count(`SELECT count(*) FROM backtest_trades t
		WHERE t.run_id = $1 AND t.fx_rate IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM fx_rates r WHERE r.base = 'EUR'
			AND r.session_date = t.execution_session AND r.rate = t.fx_rate)`,
		run.ID.String()); invented != 0 {
		t.Errorf("%d trades converted at a rate nobody stored", invented)
	}
}
