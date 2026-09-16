package backtest_test

import (
	"testing"

	"market-lens/server/internal/backtest"
)

// TestATradeNeverExecutesOnItsSignalSession is the no-lookahead rule at the point it matters most.
//
// A signal as of a session is computed from that session's close. Filling at that close is one
// line of code, it flatters every result this product will ever produce, and it is invisible in
// the chart that results. The database refuses the row; this asserts the simulation never tries,
// and that the session it did choose is one the instrument actually traded on.
func TestATradeNeverExecutesOnItsSignalSession(t *testing.T) {
	f := newBacktestFixture(t)
	run := f.run()

	if early := f.count(`SELECT count(*) FROM backtest_trades
		WHERE run_id = $1 AND execution_session <= signal_session`, run.ID.String()); early != 0 {
		t.Errorf("%d trades executed no later than the session whose close caused them", early)
	}
	if untraded := f.count(`SELECT count(*) FROM backtest_trades t WHERE t.run_id = $1
		AND NOT EXISTS (SELECT 1 FROM daily_price_bars b
			WHERE b.instrument_id = t.instrument_id AND b.session_date = t.execution_session)`,
		run.ID.String()); untraded != 0 {
		t.Errorf("%d trades executed on a session their instrument did not trade", untraded)
	}
	// The price paid is the execution session's open, not its close: the close is the number the
	// next signal is computed from, and paying it would be the same lookahead wearing a hat.
	if wrong := f.count(`SELECT count(*) FROM backtest_trades t
		JOIN daily_price_bars b ON b.instrument_id = t.instrument_id AND b.session_date = t.execution_session
		WHERE t.run_id = $1 AND t.price <> b.open`, run.ID.String()); wrong != 0 {
		t.Errorf("%d trades paid a price that is not the execution session's open", wrong)
	}
}

// TestAnInstrumentThatStopsTradingIsNotFilled covers the edge case a give-up window exists for.
//
// Without one, an instrument that stopped trading in one year would fill at its next quoted price
// in the next, as though the intervening months had not happened — the most flattering fill in the
// product, available only to positions that went wrong.
func TestAnInstrumentThatStopsTradingIsNotFilled(t *testing.T) {
	f := newBacktestFixture(t)
	// Every instrument is wanted, so the one that stops trading is certainly asked for.
	f.publishConfiguration(map[string]any{"sizing_n": len(f.listings)})
	stopped := f.listings[0].id
	first := f.session(0)
	f.stopTrading(stopped, first)

	run := f.run()

	if traded := f.count(`SELECT count(*) FROM backtest_trades
		WHERE run_id = $1 AND instrument_id = $2`, run.ID.String(), stopped.String()); traded != 0 {
		t.Errorf("%d trades filled an instrument that never traded again", traded)
	}
	if reasons := f.count(`SELECT count(*) FROM backtest_skips
		WHERE run_id = $1 AND instrument_id = $2 AND reason = 'not_executable'`,
		run.ID.String(), stopped.String()); reasons == 0 {
		t.Errorf("nothing recorded why the instrument could not be bought")
	}
}

// TestAHoldingThatStopsTradingIsUnvaluedNotCarriedForward is the other half of the same story.
//
// A position that can no longer be priced makes the portfolio's value unknown. Repeating the last
// close would produce a smooth, plausible curve nobody could tell was wrong, so the session says
// it is incomplete instead — and the reason names the position, not the portfolio.
func TestAHoldingThatStopsTradingIsUnvaluedNotCarriedForward(t *testing.T) {
	f := newBacktestFixture(t)
	f.publishConfiguration(map[string]any{"sizing_n": len(f.listings)})
	stopped := f.listings[0].id

	// Run once over undisturbed data to find a session the instrument was actually held on. The
	// alternative — guessing a session — would produce a test that passed for the wrong reason
	// the first time the strategy's ranking changed.
	reference := f.run()
	var stopsAfter string
	if err := f.pool.QueryRow(f.ctx, `SELECT min(session_date)::text FROM backtest_positions
		WHERE run_id = $1 AND instrument_id = $2 AND value IS NOT NULL`,
		reference.ID.String(), stopped.String()).Scan(&stopsAfter); err != nil || stopsAfter == "" {
		t.Fatalf("the instrument was never held, so this proves nothing: %v", err)
	}
	// Everything decided on or before that session is unaffected by what follows, so the position
	// exists in the second run exactly as it did in the first — and then the prices stop.
	f.stopTrading(stopped, stopsAfter)

	run := f.run()

	if held := f.count(`SELECT count(*) FROM backtest_positions
		WHERE run_id = $1 AND instrument_id = $2 AND session_date <= $3::date AND value IS NOT NULL`,
		run.ID.String(), stopped.String(), stopsAfter); held == 0 {
		t.Fatalf("the instrument was never held and valued, so this proves nothing")
	}
	if unvalued := f.count(`SELECT count(*) FROM backtest_positions
		WHERE run_id = $1 AND instrument_id = $2 AND absence_reason = 'no_price'`,
		run.ID.String(), stopped.String()); unvalued == 0 {
		t.Errorf("the position kept a value after its instrument stopped trading")
	}
	if incomplete := f.count(`SELECT count(*) FROM backtest_equity
		WHERE run_id = $1 AND absence_reason = 'position_unvalued'`, run.ID.String()); incomplete == 0 {
		t.Errorf("the equity curve stated a total for a session it could not value")
	}
	// A price is never invented, and never borrowed from a session the instrument did not trade.
	if borrowed := f.count(`SELECT count(*) FROM backtest_positions p WHERE p.run_id = $1
		AND p.price_session IS NOT NULL
		AND NOT EXISTS (SELECT 1 FROM daily_price_bars b
			WHERE b.instrument_id = p.instrument_id AND b.session_date = p.price_session)`,
		run.ID.String()); borrowed != 0 {
		t.Errorf("%d positions were valued at a price from a session their instrument did not trade", borrowed)
	}
}

// TestARangeThatProducesNoTradeIsAResult states that "this method did nothing" is an answer.
//
// Reporting it as a failure would invite someone to change the rules until it stopped failing,
// which is the search this feature exists to refuse.
func TestARangeThatProducesNoTradeIsAResult(t *testing.T) {
	f := newBacktestFixture(t)
	// Nothing can ever execute: every decision gives up before the next session arrives.
	f.publishConfiguration(map[string]any{"give_up_sessions": 0})

	run := f.run()

	if run.Status != backtest.RunStatusSucceeded {
		t.Errorf("a backtest that made no trade ended %s, not succeeded", run.Status)
	}
	if run.TradeCount != 0 {
		t.Fatalf("the configuration was supposed to make execution impossible, but made %d trades", run.TradeCount)
	}
	if run.SkipCount == 0 {
		t.Errorf("no trade happened and nothing said why")
	}
	if measured := f.count(`SELECT count(*) FROM backtest_measures
		WHERE run_id = $1 AND subject = 'strategy' AND total_return IS NOT NULL`,
		run.ID.String()); measured != 1 {
		t.Errorf("a result with no trades reported no measures; a flat curve is still a curve")
	}
	if costs := f.count(`SELECT count(*) FROM backtest_measures
		WHERE run_id = $1 AND subject = 'strategy' AND total_costs = 0`, run.ID.String()); costs != 1 {
		t.Errorf("costs of zero were omitted rather than stated")
	}
}
