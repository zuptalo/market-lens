-- Feature 021: reproducible backtesting.
--
-- Feature 015 records what a strategy said and refuses to say whether it was right. This records
-- what would have happened if somebody had followed it — the first thing in this product that
-- produces a number a person might act on, which is why the bar for honesty is higher here and
-- not lower.
--
-- Four rules are enforced by the database rather than by the simulation, because each of them is
-- a way to make a result look better than it was, and each is invisible in a chart:
--
--   * a trade may not execute on the session whose close produced its signal, nor earlier;
--   * a trade must name a signal that actually exists, so every trade has a reason;
--   * an equity point carries a value or a stated reason it has none, never yesterday's number;
--   * a measure set reports all six figures or states why it reports none, never a subset.
--
-- And a published configuration cannot be edited. A result recorded months ago must stay
-- reproducible from the rules that produced it, so a change publishes a new version and
-- supersedes the old one, exactly as a strategy version does.

CREATE TABLE backtest_configurations (
    id uuid PRIMARY KEY,
    name text NOT NULL CHECK (btrim(name) <> ''),
    version int NOT NULL CHECK (version >= 1),
    title text NOT NULL CHECK (btrim(title) <> ''),
    -- What the configuration is trying to express, in prose a person can disagree with.
    intent text NOT NULL CHECK (btrim(intent) <> ''),
    -- Why a result under it is not a prediction. Stored rather than templated so no surface can
    -- show a curve without it.
    caveat text NOT NULL CHECK (btrim(caveat) <> ''),
    -- The whole configuration: strategy version, universe, range, capital, accounting currency,
    -- sizing rule, rebalance schedule, costs and the give-up window. One document because it *is*
    -- the configuration — splitting it into columns would let one part change without publishing
    -- a version, which is exactly what FR-002 forbids.
    parameters jsonb NOT NULL,
    published_at timestamptz NOT NULL,
    superseded_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (name, version),
    CHECK (superseded_at IS NULL OR superseded_at >= published_at)
);

CREATE UNIQUE INDEX backtest_configurations_current_idx
    ON backtest_configurations (name) WHERE superseded_at IS NULL;

-- Superseding is the one change allowed: it records that the rules were replaced, rather than
-- altering rules a stored result was computed under.
CREATE FUNCTION backtest_configuration_is_immutable() RETURNS trigger AS $$
BEGIN
    IF (OLD.id, OLD.name, OLD.version, OLD.title, OLD.intent, OLD.caveat, OLD.parameters,
        OLD.published_at) IS DISTINCT FROM
       (NEW.id, NEW.name, NEW.version, NEW.title, NEW.intent, NEW.caveat, NEW.parameters,
        NEW.published_at) THEN
        RAISE EXCEPTION 'a published backtest configuration is superseded, never edited';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER backtest_configurations_immutable
    BEFORE UPDATE ON backtest_configurations
    FOR EACH ROW EXECUTE FUNCTION backtest_configuration_is_immutable();

COMMENT ON TABLE backtest_configurations IS
    'Stated, immutable backtest rules. Written down by a person and reviewed; never fitted, tuned or searched.';

CREATE TABLE backtest_runs (
    id uuid PRIMARY KEY,
    configuration_id uuid NOT NULL REFERENCES backtest_configurations(id),
    status text NOT NULL CHECK (status IN ('running', 'succeeded', 'failed')),
    from_session date NOT NULL,
    to_session date NOT NULL,
    -- The strategy version whose signals were replayed. Signals are read, never recomputed.
    strategy_id uuid NOT NULL REFERENCES strategies(id),
    -- What the run read, so a reader can tell a result predates a later correction (FR-017).
    -- A completed result is never silently rewritten when a bar is restated.
    signals_computed_through timestamptz,
    bars_observed_through timestamptz,
    started_at timestamptz NOT NULL,
    finished_at timestamptz,
    trade_count bigint NOT NULL DEFAULT 0 CHECK (trade_count >= 0),
    skip_count bigint NOT NULL DEFAULT 0 CHECK (skip_count >= 0),
    -- The sessions the schedule actually traded on. A signal off the schedule was never
    -- considered, and this is what accounts for it (FR-015a).
    rebalance_count bigint NOT NULL DEFAULT 0 CHECK (rebalance_count >= 0),
    error_code text,
    error_summary text,
    app_version text NOT NULL CHECK (btrim(app_version) <> ''),
    CHECK (to_session >= from_session),
    CHECK ((status = 'running' AND finished_at IS NULL) OR (status <> 'running' AND finished_at IS NOT NULL)),
    CHECK ((status = 'failed') = (error_code IS NOT NULL))
);

CREATE INDEX backtest_runs_started_idx ON backtest_runs (started_at DESC);
CREATE INDEX backtest_runs_configuration_idx ON backtest_runs (configuration_id, started_at DESC);

CREATE TABLE backtest_trades (
    id uuid PRIMARY KEY,
    run_id uuid NOT NULL REFERENCES backtest_runs(id),
    instrument_id uuid NOT NULL REFERENCES instruments(id),
    strategy_id uuid NOT NULL REFERENCES strategies(id),
    signal_session date NOT NULL,
    execution_session date NOT NULL,
    direction text NOT NULL CHECK (direction IN ('buy', 'sell')),
    quantity numeric(24,12) NOT NULL CHECK (quantity > 0),
    -- The open of the execution session, in the instrument's own listing currency. The earliest
    -- price somebody could actually have paid on a signal computed from the previous close.
    price numeric(24,12) NOT NULL CHECK (price > 0),
    price_currency text NOT NULL CHECK (price_currency ~ '^[A-Z]{3}$'),
    -- Null when the listing currency is the accounting currency: a single-currency backtest
    -- requires no rate and performs no conversion (FR-013).
    fx_rate numeric(24,12) CHECK (fx_rate IS NULL OR fx_rate > 0),
    brokerage numeric(24,12) NOT NULL CHECK (brokerage >= 0),
    slippage numeric(24,12) NOT NULL CHECK (slippage >= 0),
    spread_cost numeric(24,12) NOT NULL CHECK (spread_cost >= 0),
    -- What this trade did to cash, in the accounting currency, costs included. Negative for a
    -- buy. Stored rather than derived so the accounting identity is checkable from the rows.
    cash_effect numeric(24,12) NOT NULL,
    -- The signal that caused it. A foreign key rather than a convention: a trade cannot exist
    -- without naming its reason, which is what makes FR-014 true by construction.
    FOREIGN KEY (instrument_id, signal_session, strategy_id)
        REFERENCES signals (instrument_id, session_date, strategy_id),
    -- The rule that stops a backtest reading the future.
    CHECK (execution_session > signal_session),
    -- One decision per signal per run.
    UNIQUE (run_id, instrument_id, signal_session)
);

CREATE INDEX backtest_trades_run_session_idx ON backtest_trades (run_id, execution_session, instrument_id);

COMMENT ON TABLE backtest_trades IS
    'Simulated executions. Never an order, an order intent, or anything a broker could act on.';

CREATE TABLE backtest_positions (
    run_id uuid NOT NULL REFERENCES backtest_runs(id),
    session_date date NOT NULL,
    instrument_id uuid NOT NULL REFERENCES instruments(id),
    quantity numeric(24,12) NOT NULL CHECK (quantity > 0),
    -- The close of this session, in the listing currency, and the value it converts to. Absent
    -- together, with a reason, when the instrument did not trade or no rate was stored.
    price numeric(24,12) CHECK (price IS NULL OR price > 0),
    fx_rate numeric(24,12) CHECK (fx_rate IS NULL OR fx_rate > 0),
    value numeric(24,12),
    absence_reason text CHECK (absence_reason IS NULL OR absence_reason IN
        ('no_price', 'no_rate')),
    PRIMARY KEY (run_id, session_date, instrument_id),
    CHECK ((value IS NOT NULL AND price IS NOT NULL AND absence_reason IS NULL)
        OR (value IS NULL AND price IS NULL AND absence_reason IS NOT NULL))
);

CREATE TABLE backtest_equity (
    run_id uuid NOT NULL REFERENCES backtest_runs(id),
    session_date date NOT NULL,
    -- Cash is always known: it changes only when a trade executes.
    cash numeric(24,12) NOT NULL,
    position_value numeric(24,12),
    total numeric(24,12),
    absence_reason text CHECK (absence_reason IS NULL OR absence_reason IN ('position_unvalued')),
    PRIMARY KEY (run_id, session_date),
    -- A value or a stated reason, never both and never neither. Carrying the previous session's
    -- number forward would be the product asserting a price nobody quoted.
    CHECK ((total IS NOT NULL AND position_value IS NOT NULL AND absence_reason IS NULL)
        OR (total IS NULL AND position_value IS NULL AND absence_reason IS NOT NULL))
);

CREATE TABLE backtest_measures (
    run_id uuid NOT NULL REFERENCES backtest_runs(id),
    -- 'strategy', or the benchmark series code this row reports.
    subject text NOT NULL CHECK (btrim(subject) <> ''),
    benchmark_series_id uuid REFERENCES benchmark_series(id),
    from_session date,
    to_session date,
    total_return numeric(24,12),
    annualised_return numeric(24,12),
    volatility numeric(24,12),
    max_drawdown numeric(24,12),
    trade_count bigint CHECK (trade_count IS NULL OR trade_count >= 0),
    total_costs numeric(24,12) CHECK (total_costs IS NULL OR total_costs >= 0),
    absence_reason text CHECK (absence_reason IS NULL OR absence_reason IN
        ('series_starts_after_range', 'series_ends_before_range', 'insufficient_sessions')),
    PRIMARY KEY (run_id, subject),
    CHECK ((subject = 'strategy') = (benchmark_series_id IS NULL)),
    -- All six figures, or a stated reason for none of them. A result free to report a subset
    -- would report the flattering one.
    CHECK (
        (total_return IS NOT NULL AND annualised_return IS NOT NULL AND volatility IS NOT NULL
         AND max_drawdown IS NOT NULL AND trade_count IS NOT NULL AND total_costs IS NOT NULL
         AND from_session IS NOT NULL AND to_session IS NOT NULL AND absence_reason IS NULL)
        OR
        (total_return IS NULL AND annualised_return IS NULL AND volatility IS NULL
         AND max_drawdown IS NULL AND trade_count IS NULL AND total_costs IS NULL
         AND absence_reason IS NOT NULL)
    ),
    -- Drawdown is the measure most easily reported in a flattering direction. It is stated as a
    -- loss, so a positive one is a sign error rather than good news.
    CHECK (max_drawdown IS NULL OR max_drawdown <= 0)
);

CREATE TABLE backtest_skips (
    run_id uuid NOT NULL REFERENCES backtest_runs(id),
    instrument_id uuid NOT NULL REFERENCES instruments(id),
    signal_session date NOT NULL,
    -- Why the simulation considered this signal and did nothing. A closed vocabulary, so
    -- "nothing happened" can never be the answer.
    reason text NOT NULL CHECK (reason IN
        ('not_selected', 'already_held', 'no_cash', 'not_executable', 'no_price', 'no_rate',
         'no_next_session')),
    PRIMARY KEY (run_id, instrument_id, signal_session)
);

COMMENT ON TABLE backtest_skips IS
    'Every signal the simulation considered and did not act on, with its reason. Signals off the rebalance schedule were never considered and are accounted for by the schedule on the run.';

-- The first configuration.
--
-- Written down and reviewed, exactly as the first strategy version was. Nothing here has been
-- fitted: no parameter was chosen by trying alternatives and keeping the best, which is the
-- practice this product's whole position on strategies exists to avoid.
--
-- The accounting currency is EUR because every rate the provider serves for this universe is
-- quoted with EUR as the base — EURSEK, EURNOK, EURDKK. Choosing SEK would have meant deriving
-- three cross rates through EUR anyway, with three more places to be wrong.
--
-- Costs are non-zero by default because a default of zero would make the first result anybody
-- runs the most flattering one, and defaults are what people keep.
INSERT INTO backtest_configurations (id, name, version, title, intent, caveat, parameters, published_at) VALUES (
    '00000000-0021-4000-8000-000000000101',
    'momentum_trend_equal_weight',
    1,
    'Momentum and trend, equal weight, monthly',
    'Hold the ten highest-scoring instruments under momentum_trend v1 in equal weight, rebalanced '
    'on the first trading session of each month, executing at the next session''s open and paying '
    'stated costs. The simplest rule that uses the strategy''s output and nothing else: anything '
    'cleverer is position sizing, which is a later milestone and needs its own specification.',
    'This is a simulation over past data, not a prediction and not advice. It replays stated rules '
    'that were never fitted to the data they are replayed over, and a result that looks good is '
    'evidence about one method over one period in one market, nothing more. Past behaviour does '
    'not establish future behaviour.',
    jsonb_build_object(
        'strategy_name', 'momentum_trend',
        'strategy_version', 1,
        'universe_code', 'nordic-liquid-v1',
        'starting_capital', '1000000',
        'accounting_currency', 'EUR',
        'sizing_rule', 'equal_weight_top_n',
        'sizing_n', 10,
        'rebalance', 'monthly',
        'brokerage_bps', '10',
        'brokerage_minimum', '5',
        'slippage_bps', '5',
        'spread_bps', '10',
        'give_up_sessions', 5
    ),
    now()
);
