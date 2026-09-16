package backtest_test

import (
	"testing"

	"market-lens/server/internal/backtest"
)

// TestTheSameConfigurationProducesTheIdenticalResult is this feature's first requirement and its
// first test.
//
// Everything else a backtest reports is worth nothing if the same rules over the same data can
// produce two different answers: a result recorded last month could not be argued with, only
// believed. So the simulation reads stored data only, orders every decision totally, and rounds
// to the stored precision at each step rather than at the end — and this asserts the consequence
// rather than any of those mechanisms, field by field across every table a reader can see.
func TestTheSameConfigurationProducesTheIdenticalResult(t *testing.T) {
	f := newBacktestFixture(t)

	first := f.run()
	if first.Status != backtest.RunStatusSucceeded {
		t.Fatalf("the backtest ended %s: %s", first.Status, first.ErrorSummary)
	}
	if first.TradeCount == 0 {
		t.Fatalf("the fixture produced no trades, so this proves nothing about reproducing one")
	}
	f.snapshot("previous_trades", "backtest_trades")
	f.snapshot("previous_equity", "backtest_equity")
	f.snapshot("previous_positions", "backtest_positions")
	f.snapshot("previous_measures", "backtest_measures")
	f.snapshot("previous_skips", "backtest_skips")

	second := f.run()
	if second.ID == first.ID {
		t.Fatalf("the second run reused the first run's identity, so nothing was recomputed")
	}

	if changed := f.count(`SELECT count(*) FROM backtest_trades t JOIN previous_trades p
		USING (instrument_id, signal_session)
		WHERE t.run_id = $1 AND p.run_id = $2
		  AND (t.execution_session IS DISTINCT FROM p.execution_session
		    OR t.direction IS DISTINCT FROM p.direction
		    OR t.quantity IS DISTINCT FROM p.quantity
		    OR t.price IS DISTINCT FROM p.price
		    OR t.price_currency IS DISTINCT FROM p.price_currency
		    OR t.fx_rate IS DISTINCT FROM p.fx_rate
		    OR t.brokerage IS DISTINCT FROM p.brokerage
		    OR t.slippage IS DISTINCT FROM p.slippage
		    OR t.spread_cost IS DISTINCT FROM p.spread_cost
		    OR t.cash_effect IS DISTINCT FROM p.cash_effect)`,
		second.ID.String(), first.ID.String()); changed != 0 {
		t.Errorf("%d trades differ on recomputation", changed)
	}
	if missing := f.count(`SELECT count(*) FROM previous_trades p WHERE p.run_id = $2
		AND NOT EXISTS (SELECT 1 FROM backtest_trades t WHERE t.run_id = $1
		  AND t.instrument_id = p.instrument_id AND t.signal_session = p.signal_session)`,
		second.ID.String(), first.ID.String()); missing != 0 {
		t.Errorf("%d trades the first run made did not happen again", missing)
	}
	if extra := f.count(`SELECT count(*) FROM backtest_trades t WHERE t.run_id = $1
		AND NOT EXISTS (SELECT 1 FROM previous_trades p WHERE p.run_id = $2
		  AND p.instrument_id = t.instrument_id AND p.signal_session = t.signal_session)`,
		second.ID.String(), first.ID.String()); extra != 0 {
		t.Errorf("the second run made %d trades the first did not", extra)
	}

	if changed := f.count(`SELECT count(*) FROM backtest_equity e JOIN previous_equity p
		USING (session_date) WHERE e.run_id = $1 AND p.run_id = $2
		  AND (e.cash IS DISTINCT FROM p.cash
		    OR e.position_value IS DISTINCT FROM p.position_value
		    OR e.total IS DISTINCT FROM p.total
		    OR e.absence_reason IS DISTINCT FROM p.absence_reason)`,
		second.ID.String(), first.ID.String()); changed != 0 {
		t.Errorf("%d equity points differ on recomputation", changed)
	}

	if changed := f.count(`SELECT count(*) FROM backtest_positions q JOIN previous_positions p
		USING (session_date, instrument_id) WHERE q.run_id = $1 AND p.run_id = $2
		  AND (q.quantity IS DISTINCT FROM p.quantity
		    OR q.price IS DISTINCT FROM p.price
		    OR q.price_session IS DISTINCT FROM p.price_session
		    OR q.fx_rate IS DISTINCT FROM p.fx_rate
		    OR q.value IS DISTINCT FROM p.value
		    OR q.absence_reason IS DISTINCT FROM p.absence_reason)`,
		second.ID.String(), first.ID.String()); changed != 0 {
		t.Errorf("%d positions differ on recomputation", changed)
	}

	if changed := f.count(`SELECT count(*) FROM backtest_measures m JOIN previous_measures p
		USING (subject) WHERE m.run_id = $1 AND p.run_id = $2
		  AND (m.total_return IS DISTINCT FROM p.total_return
		    OR m.annualised_return IS DISTINCT FROM p.annualised_return
		    OR m.volatility IS DISTINCT FROM p.volatility
		    OR m.max_drawdown IS DISTINCT FROM p.max_drawdown
		    OR m.trade_count IS DISTINCT FROM p.trade_count
		    OR m.total_costs IS DISTINCT FROM p.total_costs
		    OR m.absence_reason IS DISTINCT FROM p.absence_reason
		    OR m.from_session IS DISTINCT FROM p.from_session
		    OR m.to_session IS DISTINCT FROM p.to_session)`,
		second.ID.String(), first.ID.String()); changed != 0 {
		t.Errorf("%d measure sets differ on recomputation", changed)
	}

	if changed := f.count(`SELECT count(*) FROM
		(SELECT instrument_id, signal_session, reason FROM backtest_skips WHERE run_id = $1) s
		FULL JOIN
		(SELECT instrument_id, signal_session, reason FROM previous_skips WHERE run_id = $2) p
		USING (instrument_id, signal_session)
		WHERE s.reason IS DISTINCT FROM p.reason`,
		second.ID.String(), first.ID.String()); changed != 0 {
		t.Errorf("%d skipped signals differ on recomputation", changed)
	}

	if first.TradeCount != second.TradeCount || first.SkipCount != second.SkipCount ||
		first.RebalanceCount != second.RebalanceCount {
		t.Errorf("the two runs report different totals: %+v then %+v", first, second)
	}
}
