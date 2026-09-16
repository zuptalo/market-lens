package backtest_test

import (
	"testing"
)

// TestCashAndPositionsReconcileAtEverySession is the accounting identity, asserted at every
// session rather than only at the end.
//
// Checking only the final equity would let a whole decade of arithmetic drift and cancel. The
// figures reconcile exactly rather than nearly because every intermediate is rounded to the
// stored precision before the next step reads it — the discipline feature 015 arrived at when an
// explanation stopped reconciling with the score it explained.
func TestCashAndPositionsReconcileAtEverySession(t *testing.T) {
	f := newBacktestFixture(t)
	run := f.run()

	if broken := f.count(`SELECT count(*) FROM backtest_equity e
		WHERE e.run_id = $1 AND e.total IS NOT NULL
		  AND e.total <> e.cash + coalesce((SELECT sum(p.value) FROM backtest_positions p
			WHERE p.run_id = e.run_id AND p.session_date = e.session_date), 0)`,
		run.ID.String()); broken != 0 {
		t.Errorf("cash plus positions does not equal the stored equity at %d sessions", broken)
	}

	// Cash moves only when a trade executes, and by exactly what the trade recorded. A curve
	// whose cash drifted for any other reason would be spending money nothing accounts for.
	if broken := f.count(`WITH ordered AS (
			SELECT session_date, cash, lag(cash) OVER (ORDER BY session_date) AS previous
			FROM backtest_equity WHERE run_id = $1)
		SELECT count(*) FROM ordered o
		WHERE o.previous IS NOT NULL
		  AND o.cash <> o.previous + coalesce((SELECT sum(t.cash_effect) FROM backtest_trades t
			WHERE t.run_id = $1 AND t.execution_session = o.session_date), 0)`,
		run.ID.String()); broken != 0 {
		t.Errorf("cash changed without a trade to account for it at %d sessions", broken)
	}

	// Nothing is ever bought with money the portfolio did not have.
	if negative := f.count(`SELECT count(*) FROM backtest_equity
		WHERE run_id = $1 AND cash < 0`, run.ID.String()); negative != 0 {
		t.Errorf("the portfolio held negative cash at %d sessions, which is borrowing", negative)
	}

	// The position rows are the holdings, not a parallel record of them: a quantity that appears
	// without a trade behind it would make the curve unaccountable.
	if orphans := f.count(`SELECT count(*) FROM backtest_positions p
		WHERE p.run_id = $1 AND NOT EXISTS (SELECT 1 FROM backtest_trades t
			WHERE t.run_id = p.run_id AND t.instrument_id = p.instrument_id
			  AND t.execution_session <= p.session_date AND t.direction = 'buy')`,
		run.ID.String()); orphans != 0 {
		t.Errorf("%d positions were held without a purchase behind them", orphans)
	}
}

// TestEveryTradeNamesTheSignalThatCausedIt and its companion below are FR-014 and FR-015.
//
// The first is true by construction — a foreign key, not a convention — and is asserted anyway,
// because the point is that a reader can get from a trade to the strategy's own contributions
// rather than to an assertion that the trade was justified.
func TestEveryTradeNamesTheSignalThatCausedIt(t *testing.T) {
	f := newBacktestFixture(t)
	run := f.run()

	if unexplained := f.count(`SELECT count(*) FROM backtest_trades t
		WHERE t.run_id = $1 AND NOT EXISTS (SELECT 1 FROM signals s
			WHERE s.instrument_id = t.instrument_id AND s.session_date = t.signal_session
			  AND s.strategy_id = t.strategy_id)`, run.ID.String()); unexplained != 0 {
		t.Errorf("%d trades name a signal that does not exist", unexplained)
	}
	// And the signal has its reasons attached, so the path from a trade to "why" is complete.
	if bare := f.count(`SELECT count(*) FROM backtest_trades t
		JOIN signals s ON s.instrument_id = t.instrument_id AND s.session_date = t.signal_session
			AND s.strategy_id = t.strategy_id
		WHERE t.run_id = $1 AND jsonb_array_length(s.contributions) = 0`,
		run.ID.String()); bare != 0 {
		t.Errorf("%d trades lead to a signal with no contributions behind it", bare)
	}
}

// TestEverySignalWithoutATradeRecordsAReason closes the other half. "The strategy said buy and
// nothing happened" must always be answerable, and the vocabulary is closed so the answer can
// never be silence.
func TestEverySignalWithoutATradeRecordsAReason(t *testing.T) {
	f := newBacktestFixture(t)
	run := f.run()

	// Every signal on a session the schedule traded on is accounted for by a trade or a skip.
	unaccounted := f.count(`WITH rebalances AS (
			SELECT DISTINCT signal_session FROM backtest_trades WHERE run_id = $1
			UNION SELECT DISTINCT signal_session FROM backtest_skips WHERE run_id = $1)
		SELECT count(*) FROM signals s
		JOIN rebalances r ON r.signal_session = s.session_date
		JOIN universe_memberships m ON m.instrument_id = s.instrument_id
		WHERE m.universe_id = $2 AND s.strategy_id = $3
		  AND NOT EXISTS (SELECT 1 FROM backtest_trades t WHERE t.run_id = $1
			AND t.instrument_id = s.instrument_id AND t.signal_session = s.session_date)
		  AND NOT EXISTS (SELECT 1 FROM backtest_skips k WHERE k.run_id = $1
			AND k.instrument_id = s.instrument_id AND k.signal_session = s.session_date)`,
		run.ID.String(), backtestUnivID.String(), run.StrategyID.String())
	if unaccounted != 0 {
		t.Errorf("%d considered signals produced neither a trade nor a recorded reason", unaccounted)
	}

	// And no signal is accounted for twice, which would make "why" ambiguous.
	if doubled := f.count(`SELECT count(*) FROM backtest_trades t
		JOIN backtest_skips k ON k.run_id = t.run_id AND k.instrument_id = t.instrument_id
			AND k.signal_session = t.signal_session
		WHERE t.run_id = $1`, run.ID.String()); doubled != 0 {
		t.Errorf("%d signals both traded and recorded a reason they did not", doubled)
	}

	// The run states the schedule it followed, which is what accounts for every signal it never
	// considered. Without it the silence about those signals would be unexplained.
	if run.RebalanceCount == 0 {
		t.Errorf("the run reports no rebalance sessions, so nothing accounts for the signals it skipped")
	}
}

// TestCostsAreRecordedOnTheTradeAndInTheCash is why a backtest is not a marketing document.
//
// Costs move a result over thousands of sessions more than most readers expect, which is exactly
// why a default of zero would be the most flattering default in the product.
func TestCostsAreRecordedOnTheTradeAndInTheCash(t *testing.T) {
	f := newBacktestFixture(t)
	run := f.run()

	if free := f.count(`SELECT count(*) FROM backtest_trades
		WHERE run_id = $1 AND brokerage = 0`, run.ID.String()); free != 0 {
		t.Errorf("%d trades paid no brokerage under a configuration that states some", free)
	}
	// The cash a trade moved is its consideration and its costs, and nothing else.
	if wrong := f.count(`SELECT count(*) FROM backtest_trades t
		WHERE t.run_id = $1 AND t.direction = 'buy'
		  AND t.cash_effect <> -(round(t.quantity * t.price / coalesce(t.fx_rate, 1), 12)
			+ t.slippage + t.spread_cost + t.brokerage)`, run.ID.String()); wrong != 0 {
		t.Errorf("%d purchases moved cash by something other than their consideration and costs", wrong)
	}
	if reported := f.count(`SELECT count(*) FROM backtest_measures m
		WHERE m.run_id = $1 AND m.subject = 'strategy' AND m.total_costs = (
			SELECT coalesce(sum(t.brokerage + t.slippage + t.spread_cost), 0)
			FROM backtest_trades t WHERE t.run_id = $1)`, run.ID.String()); reported != 1 {
		t.Errorf("the reported total costs do not equal what the trades actually bore")
	}

	// A zero-cost configuration says zero rather than omitting the figure. A missing number reads
	// as "not applicable"; a zero reads as a decision somebody made.
	f.publishConfiguration(map[string]any{
		"brokerage_bps": "0", "brokerage_minimum": "0", "slippage_bps": "0", "spread_bps": "0"})
	free := f.run()
	if stated := f.count(`SELECT count(*) FROM backtest_measures
		WHERE run_id = $1 AND subject = 'strategy' AND total_costs = 0`, free.ID.String()); stated != 1 {
		t.Errorf("a costless configuration omitted its total costs instead of stating zero")
	}
	if charged := f.count(`SELECT count(*) FROM backtest_trades
		WHERE run_id = $1 AND (brokerage <> 0 OR slippage <> 0 OR spread_cost <> 0)`,
		free.ID.String()); charged != 0 {
		t.Errorf("%d trades were charged under a configuration that states no costs", charged)
	}
}

// TestTradesForAnInstrumentAlternate is the invariant that catches a decision taken twice.
//
// You cannot buy what you already hold or sell what you do not, so an instrument's trades must
// read buy, sell, buy, sell in execution order. The way that could break is a decision still
// outstanding when the next one is taken: the instrument is not held yet, so a schedule looking
// only at the holdings would buy it twice and the second purchase would replace the first —
// paying twice and holding once.
//
// It cannot happen, and the reason is worth pinning down rather than trusting. An instrument is
// only decided on for a session it has a signal for, which is a session it traded; its execution
// is the first session it trades after that; and executions are applied before the next session's
// planning. So the earlier decision is always resolved before the later one is taken. This test
// runs the schedule that would expose a violation soonest.
func TestTradesForAnInstrumentAlternate(t *testing.T) {
	f := newBacktestFixture(t)
	// Daily, so any outstanding decision meets the next one immediately. A monthly schedule
	// leaves a month of slack and would hide a violation entirely.
	f.publishConfiguration(map[string]any{"rebalance": "daily"})
	run := f.run()

	if run.TradeCount == 0 {
		t.Fatalf("no trades, so this proves nothing")
	}
	repeated := f.count(`WITH ordered AS (
			SELECT instrument_id, direction,
			       lag(direction) OVER (PARTITION BY instrument_id
			                            ORDER BY execution_session, signal_session) AS previous
			FROM backtest_trades WHERE run_id = $1)
		SELECT count(*) FROM ordered WHERE previous = direction`, run.ID.String())
	if repeated != 0 {
		t.Errorf("%d trades repeat the previous direction: something was bought twice or sold twice", repeated)
	}
	// And no instrument is decided on twice for the same session.
	if doubled := f.count(`SELECT count(*) FROM (
			SELECT instrument_id, execution_session FROM backtest_trades WHERE run_id = $1
			GROUP BY 1, 2 HAVING count(*) > 1) d`, run.ID.String()); doubled != 0 {
		t.Errorf("%d instruments traded twice on the same session", doubled)
	}
}
