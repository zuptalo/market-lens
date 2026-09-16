package db_test

import (
	"context"
	"testing"

	"market-lens/server/internal/db"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestBacktestMigration covers the constraints that carry this feature's honesty.
//
// Two of them are in the schema rather than in code on purpose. A trade that executed on the
// session whose close produced its signal is the single easiest way to make a backtest look good,
// and it is invisible in a chart; an equity point that carries yesterday's number instead of
// saying it could not be valued is the second. Neither can be introduced by a future change to
// the simulation, because the database refuses the row.
func TestBacktestMigration(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, table := range []string{
		"backtest_configurations", "backtest_runs", "backtest_trades",
		"backtest_positions", "backtest_equity", "backtest_measures", "backtest_skips",
	} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("table %s does not exist", table)
		}
	}

	// The first configuration arrives with the migration, exactly as the first strategy version
	// did: written down by a person and reviewed, never derived from data.
	var configurationID, currency, caveat string
	var version int
	if err := pool.QueryRow(ctx, `SELECT id::text, version, parameters->>'accounting_currency', caveat
		FROM backtest_configurations WHERE name = 'momentum_trend_equal_weight' AND superseded_at IS NULL`).
		Scan(&configurationID, &version, &currency, &caveat); err != nil {
		t.Fatalf("the first configuration is not published: %v", err)
	}
	if version != 1 || currency != "EUR" {
		t.Errorf("the first configuration is version %d in %s, want version 1 in EUR", version, currency)
	}
	if caveat == "" {
		t.Errorf("a configuration with no caveat lets a result be shown without one")
	}

	// It is immutable. A published configuration is superseded, never edited, so a result
	// recorded months ago stays reproducible from the rules that produced it. Superseding is the
	// one change allowed, because it records that the rules were replaced rather than altered.
	if _, err := pool.Exec(ctx, `UPDATE backtest_configurations
		SET parameters = jsonb_set(parameters, '{starting_capital}', '2000000')
		WHERE id = $1`, configurationID); err == nil {
		t.Errorf("a published configuration was edited in place")
	}
	mustExec(t, ctx, pool, `UPDATE backtest_configurations SET superseded_at = now() WHERE id = $1`,
		configurationID)
	mustExec(t, ctx, pool, `UPDATE backtest_configurations SET superseded_at = NULL WHERE id = $1`,
		configurationID)

	instrumentID, strategyID, strategyRunID := seedBacktestContext(t, ctx, pool)

	runID := "00000000-0021-4000-8000-0000000000aa"
	mustExec(t, ctx, pool, `INSERT INTO backtest_runs
		(id, configuration_id, status, from_session, to_session, started_at, finished_at,
		 strategy_id, app_version)
		VALUES ($1, $2, 'succeeded', '2017-01-02', '2017-03-31', now(), now(), $3, 'test')`,
		runID, configurationID, strategyID)

	// A signal is computed from a session's close. A trade on that same session assumes somebody
	// acted on a price they only learned when the session ended.
	if _, err := pool.Exec(ctx, `INSERT INTO backtest_trades
		(id, run_id, instrument_id, strategy_id, signal_session, execution_session, direction,
		 quantity, price, price_currency, brokerage, slippage, spread_cost, cash_effect)
		VALUES ('00000000-0021-4000-8000-0000000000b1', $1, $2, $3, '2017-01-03', '2017-01-03',
		        'buy', 100, 42.5, 'SEK', 5, 2, 1, -4258)`,
		runID, instrumentID, strategyID); err == nil {
		t.Errorf("a trade executed on the session whose close produced its signal")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO backtest_trades
		(id, run_id, instrument_id, strategy_id, signal_session, execution_session, direction,
		 quantity, price, price_currency, brokerage, slippage, spread_cost, cash_effect)
		VALUES ('00000000-0021-4000-8000-0000000000b2', $1, $2, $3, '2017-01-03', '2017-01-02',
		        'buy', 100, 42.5, 'SEK', 5, 2, 1, -4258)`,
		runID, instrumentID, strategyID); err == nil {
		t.Errorf("a trade executed before the signal that caused it")
	}

	// A trade must name a signal that exists. FR-014 is true by construction rather than by
	// anybody remembering to fill the column in.
	if _, err := pool.Exec(ctx, `INSERT INTO backtest_trades
		(id, run_id, instrument_id, strategy_id, signal_session, execution_session, direction,
		 quantity, price, price_currency, brokerage, slippage, spread_cost, cash_effect)
		VALUES ('00000000-0021-4000-8000-0000000000b3', $1, $2, $3, '2017-02-14', '2017-02-15',
		        'buy', 100, 42.5, 'SEK', 5, 2, 1, -4258)`,
		runID, instrumentID, strategyID); err == nil {
		t.Errorf("a trade named a signal that does not exist")
	}
	mustExec(t, ctx, pool, `INSERT INTO backtest_trades
		(id, run_id, instrument_id, strategy_id, signal_session, execution_session, direction,
		 quantity, price, price_currency, brokerage, slippage, spread_cost, cash_effect)
		VALUES ('00000000-0021-4000-8000-0000000000b4', $1, $2, $3, '2017-01-03', '2017-01-04',
		        'buy', 100, 42.5, 'SEK', 5, 2, 1, -4258)`,
		runID, instrumentID, strategyID)

	// An equity point says what the portfolio was worth, or why it could not say. Never both,
	// and never neither — the shape a signal already has, for the same reason.
	if _, err := pool.Exec(ctx, `INSERT INTO backtest_equity
		(run_id, session_date, cash, position_value, total, absence_reason)
		VALUES ($1, '2017-01-04', 100, 50, 150, 'position_unvalued')`, runID); err == nil {
		t.Errorf("an equity point carried both a total and a reason it had none")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO backtest_equity
		(run_id, session_date, cash, position_value, total, absence_reason)
		VALUES ($1, '2017-01-05', 100, NULL, NULL, NULL)`, runID); err == nil {
		t.Errorf("an equity point carried neither a total nor a reason")
	}
	mustExec(t, ctx, pool, `INSERT INTO backtest_equity
		(run_id, session_date, cash, position_value, total) VALUES ($1, '2017-01-04', 100, 50, 150)`, runID)
	mustExec(t, ctx, pool, `INSERT INTO backtest_equity
		(run_id, session_date, cash, absence_reason) VALUES ($1, '2017-01-05', 100, 'position_unvalued')`, runID)

	// A measure set reports all six figures or states why it reports none. A result free to
	// report a subset would report the flattering one.
	if _, err := pool.Exec(ctx, `INSERT INTO backtest_measures
		(run_id, subject, total_return, trade_count) VALUES ($1, 'strategy', 0.12, 1)`, runID); err == nil {
		t.Errorf("a measure set reported a subset of the six figures")
	}
	mustExec(t, ctx, pool, `INSERT INTO backtest_measures
		(run_id, subject, from_session, to_session, total_return, annualised_return, volatility,
		 max_drawdown, trade_count, total_costs)
		VALUES ($1, 'strategy', '2017-01-02', '2017-03-31', 0.12, 0.48, 0.19, -0.07, 1, 8)`, runID)
	mustExec(t, ctx, pool, `INSERT INTO backtest_measures
		(run_id, subject, benchmark_series_id, absence_reason)
		VALUES ($1, 'OMXC25.INDX', '00000000-0021-4000-8000-000000000004', 'series_starts_after_range')`, runID)

	// A skip names a reason from the stated vocabulary, and nothing else.
	if _, err := pool.Exec(ctx, `INSERT INTO backtest_skips
		(run_id, instrument_id, signal_session, reason) VALUES ($1, $2, '2017-01-03', 'felt_wrong')`,
		runID, instrumentID); err == nil {
		t.Errorf("a skip was accepted with a reason outside the vocabulary")
	}
	mustExec(t, ctx, pool, `INSERT INTO backtest_skips
		(run_id, instrument_id, signal_session, reason) VALUES ($1, $2, '2017-01-03', 'not_selected')`,
		runID, instrumentID)

	_ = strategyRunID
}

// seedBacktestContext returns an instrument, a strategy version and a strategy run, with one
// signal a trade can legitimately name.
func seedBacktestContext(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (string, string, string) {
	t.Helper()
	var instrumentID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM instruments LIMIT 1`).Scan(&instrumentID); err != nil {
		t.Fatalf("find a seeded instrument: %v", err)
	}
	var strategyID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM strategies WHERE superseded_at IS NULL LIMIT 1`).
		Scan(&strategyID); err != nil {
		t.Fatalf("find the published strategy: %v", err)
	}
	var universeID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM research_universes LIMIT 1`).Scan(&universeID); err != nil {
		t.Fatalf("find the universe: %v", err)
	}
	runID := "00000000-0021-4000-8000-0000000000cc"
	mustExec(t, ctx, pool, `INSERT INTO strategy_runs
		(id, strategy_id, kind, status, universe_id, started_at, finished_at, app_version)
		VALUES ($1, $2, 'full', 'succeeded', $3, now(), now(), 'test')`, runID, strategyID, universeID)
	mustExec(t, ctx, pool, `INSERT INTO signals
		(instrument_id, session_date, strategy_id, score, action, confidence, divisor, computed_at, run_id)
		VALUES ($1, '2017-01-03', $2, 0.42, 'BUY', 0.9, 1, now(), $3)`, instrumentID, strategyID, runID)
	return instrumentID, strategyID, runID
}
