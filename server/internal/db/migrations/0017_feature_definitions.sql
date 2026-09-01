-- Feature 013: the definition table and its version-1 reference data.
--
-- A definition is additive and never edited in place (FR-001, FR-022). A later version is a
-- new row; the earlier row gains superseded_at and changes in no other way. Every value the
-- engine stores references one of these rows, so a value can always be explained (SC-003).
--
-- Windows are in stored exchange sessions, never calendar days (FR-004). parameters holds
-- every constant a computation reads, so a changed constant is a new version rather than a
-- silent redefinition. undefined_conditions is the prose a reader needs; the computation
-- enforces it through absence rows, never through zeros (FR-014).
--
-- price_basis 'adjusted' is engine-applied: closes, highs and lows before a split's ex-date
-- are divided by its ratio, considering only splits whose ex-date is on or before the session
-- being computed. It never reads daily_price_bars.adjusted_close, which is back-adjusted for
-- splits that had not yet happened at the session — the lookahead FR-019 forbids.
--
-- return_20, return_90 and volatility_20 are adopted verbatim from feature 005's listing
-- query (server/internal/instruments/listing.go, listingStatisticsCTE) as version 1, on raw
-- closes as that query reads them, so that no number the Markets table displays moves when
-- its source changes (spec 013, Assumptions).

CREATE TABLE feature_definitions (
    id uuid PRIMARY KEY,
    name text NOT NULL CHECK (name ~ '^[a-z][a-z0-9_]*$'),
    version int NOT NULL CHECK (version >= 1),
    window_sessions int CHECK (window_sessions IS NULL OR window_sessions > 0),
    price_basis text NOT NULL CHECK (price_basis IN ('raw', 'adjusted')),
    parameters jsonb NOT NULL DEFAULT '{}'::jsonb,
    undefined_conditions text NOT NULL CHECK (btrim(undefined_conditions) <> ''),
    session_length_sensitive boolean NOT NULL DEFAULT false,
    published_at timestamptz NOT NULL,
    superseded_at timestamptz,
    UNIQUE (name, version),
    CHECK (superseded_at IS NULL OR superseded_at >= published_at)
);

INSERT INTO feature_definitions
    (id, name, version, window_sessions, price_basis, parameters, undefined_conditions, session_length_sensitive, published_at)
VALUES
    ('00000000-0013-4000-8000-000000000001', 'return_1', 1, 2, 'adjusted',
     '{"lookback_sessions": 1, "formula": "close[t] / close[t-1] - 1"}',
     'Fewer than 2 stored sessions ending at the session; an exchange-open session without a stored bar inside the window; a prior close of zero.',
     false, '2026-09-01T00:00:00Z'),
    ('00000000-0013-4000-8000-000000000002', 'return_5', 1, 6, 'adjusted',
     '{"lookback_sessions": 5, "formula": "close[t] / close[t-5] - 1"}',
     'Fewer than 6 stored sessions ending at the session; a gap inside the window; a prior close of zero.',
     false, '2026-09-01T00:00:00Z'),
    ('00000000-0013-4000-8000-000000000003', 'return_20', 1, 21, 'raw',
     '{"lookback_sessions": 20, "formula": "close[t] / close[t-20] - 1", "adopted_from": "specs/005-instrument-exploration listingStatisticsCTE return_20"}',
     'Fewer than 21 stored sessions ending at the session; a gap inside the window; a prior close of zero. Raw closes, exactly as the Markets listing computed it before this engine.',
     false, '2026-09-01T00:00:00Z'),
    ('00000000-0013-4000-8000-000000000004', 'return_60', 1, 61, 'adjusted',
     '{"lookback_sessions": 60, "formula": "close[t] / close[t-60] - 1"}',
     'Fewer than 61 stored sessions ending at the session; a gap inside the window; a prior close of zero.',
     false, '2026-09-01T00:00:00Z'),
    ('00000000-0013-4000-8000-000000000005', 'return_90', 1, 91, 'raw',
     '{"lookback_sessions": 90, "formula": "close[t] / close[t-90] - 1", "adopted_from": "specs/005-instrument-exploration listingStatisticsCTE return_90"}',
     'Fewer than 91 stored sessions ending at the session; a gap inside the window; a prior close of zero. Raw closes, exactly as the Markets listing computed it before this engine.',
     false, '2026-09-01T00:00:00Z'),
    ('00000000-0013-4000-8000-000000000006', 'return_250', 1, 251, 'adjusted',
     '{"lookback_sessions": 250, "formula": "close[t] / close[t-250] - 1"}',
     'Fewer than 251 stored sessions ending at the session; a gap inside the window; a prior close of zero.',
     false, '2026-09-01T00:00:00Z'),
    ('00000000-0013-4000-8000-000000000007', 'log_return_1', 1, 2, 'adjusted',
     '{"lookback_sessions": 1, "formula": "ln(close[t] / close[t-1])"}',
     'Fewer than 2 stored sessions ending at the session; a gap inside the window; a prior close of zero.',
     false, '2026-09-01T00:00:00Z'),
    ('00000000-0013-4000-8000-000000000008', 'sma_20', 1, 20, 'adjusted',
     '{"formula": "mean(close[t-19..t])"}',
     'Fewer than 20 stored sessions ending at the session; a gap inside the window.',
     false, '2026-09-01T00:00:00Z'),
    ('00000000-0013-4000-8000-000000000009', 'sma_50', 1, 50, 'adjusted',
     '{"formula": "mean(close[t-49..t])"}',
     'Fewer than 50 stored sessions ending at the session; a gap inside the window.',
     false, '2026-09-01T00:00:00Z'),
    ('00000000-0013-4000-8000-000000000010', 'sma_200', 1, 200, 'adjusted',
     '{"formula": "mean(close[t-199..t])"}',
     'Fewer than 200 stored sessions ending at the session; a gap inside the window.',
     false, '2026-09-01T00:00:00Z'),
    ('00000000-0013-4000-8000-000000000011', 'trend_50_200', 1, 200, 'adjusted',
     '{"fast_sessions": 50, "slow_sessions": 200, "formula": "sma_50[t] / sma_200[t] - 1"}',
     'Whenever sma_200 is undefined: fewer than 200 stored sessions ending at the session, or a gap inside the window.',
     false, '2026-09-01T00:00:00Z'),
    ('00000000-0013-4000-8000-000000000012', 'momentum_20', 1, 20, 'adjusted',
     '{"sessions": 20, "formula": "close[t] / sma_20[t] - 1"}',
     'Whenever sma_20 is undefined: fewer than 20 stored sessions ending at the session, or a gap inside the window.',
     false, '2026-09-01T00:00:00Z'),
    ('00000000-0013-4000-8000-000000000013', 'relative_strength_20', 1, 21, 'adjusted',
     '{"lookback_sessions": 20, "composite": "composite_return_1", "formula": "(1 + return_20_adjusted[t]) / prod(1 + composite_return_1[s], s in t-19..t) - 1"}',
     'Whenever the instrument''s own 20-session adjusted return is undefined, or the universe composite is undefined for any of the 20 sessions ending at the session (composite_undefined). Never carried forward from the previous session.',
     false, '2026-09-01T00:00:00Z'),
    ('00000000-0013-4000-8000-000000000014', 'relative_strength_90', 1, 91, 'adjusted',
     '{"lookback_sessions": 90, "composite": "composite_return_1", "formula": "(1 + return_90_adjusted[t]) / prod(1 + composite_return_1[s], s in t-89..t) - 1"}',
     'Whenever the instrument''s own 90-session adjusted return is undefined, or the universe composite is undefined for any of the 90 sessions ending at the session (composite_undefined). Never carried forward from the previous session.',
     false, '2026-09-01T00:00:00Z'),
    ('00000000-0013-4000-8000-000000000015', 'volatility_20', 1, 21, 'raw',
     '{"log_returns": 20, "annualisation_sessions": 252, "formula": "stddev_samp(ln(close[s] / close[s-1]), s in t-19..t) * sqrt(252)", "adopted_from": "specs/005-instrument-exploration listingStatisticsCTE volatility"}',
     'Fewer than 21 stored sessions ending at the session; a gap inside the window. Raw closes, exactly as the Markets listing computed it before this engine. A half day contributes one full return and so one session''s share of the annualised figure.',
     true, '2026-09-01T00:00:00Z'),
    ('00000000-0013-4000-8000-000000000016', 'atr_14', 1, 15, 'adjusted',
     '{"sessions": 14, "true_range": "max(high - low, |high - close[s-1]|, |low - close[s-1]|)", "formula": "mean(true_range[s], s in t-13..t)"}',
     'Fewer than 15 stored sessions ending at the session (the true range needs the prior close); a gap inside the window. A half day''s range is a shorter session''s range and is not scaled.',
     true, '2026-09-01T00:00:00Z'),
    ('00000000-0013-4000-8000-000000000017', 'rsi_14', 1, 140, 'adjusted',
     '{"period": 14, "window_sessions": 140, "smoothing": "wilder", "seed": "simple mean of the first 14 gains and losses inside the window", "formula": "100 - 100 / (1 + average_gain / average_loss); 100 when average_loss is zero"}',
     'Fewer than 140 stored sessions ending at the session; a gap inside the window. Smoothing runs over the fixed window only, never from the start of history (research R-009).',
     false, '2026-09-01T00:00:00Z'),
    ('00000000-0013-4000-8000-000000000018', 'macd_12_26', 1, 130, 'adjusted',
     '{"fast": 12, "slow": 26, "window_sessions": 130, "seed": "simple mean of the first period closes inside the window", "formula": "ema_12[t] - ema_26[t]"}',
     'Fewer than 130 stored sessions ending at the session; a gap inside the window. Both averages run over the fixed window only (research R-009).',
     false, '2026-09-01T00:00:00Z'),
    ('00000000-0013-4000-8000-000000000019', 'macd_signal_9', 1, 130, 'adjusted',
     '{"fast": 12, "slow": 26, "signal": 9, "window_sessions": 130, "seed": "simple mean of the first 9 macd values inside the window", "formula": "ema_9(macd_12_26)[t]"}',
     'Whenever macd_12_26 is undefined.',
     false, '2026-09-01T00:00:00Z'),
    ('00000000-0013-4000-8000-000000000020', 'macd_histogram', 1, 130, 'adjusted',
     '{"fast": 12, "slow": 26, "signal": 9, "window_sessions": 130, "formula": "macd_12_26[t] - macd_signal_9[t]"}',
     'Whenever macd_signal_9 is undefined.',
     false, '2026-09-01T00:00:00Z'),
    ('00000000-0013-4000-8000-000000000021', 'drawdown_250', 1, 250, 'adjusted',
     '{"sessions": 250, "formula": "close[t] / max(close[t-249..t]) - 1"}',
     'Fewer than 250 stored sessions ending at the session; a gap inside the window. Zero at a new 250-session peak, negative below it, never positive.',
     false, '2026-09-01T00:00:00Z'),
    ('00000000-0013-4000-8000-000000000022', 'volume_sma_20', 1, 20, 'raw',
     '{"sessions": 20, "formula": "mean(volume[t-19..t])"}',
     'Fewer than 20 stored sessions ending at the session; a gap inside the window. A zero-volume bar is an observation and counts; volume is not adjusted for splits in this version.',
     true, '2026-09-01T00:00:00Z'),
    ('00000000-0013-4000-8000-000000000023', 'volume_ratio_20', 1, 20, 'raw',
     '{"sessions": 20, "formula": "volume[t] / volume_sma_20[t]"}',
     'Whenever volume_sma_20 is undefined, or is zero (zero_denominator).',
     true, '2026-09-01T00:00:00Z'),
    ('00000000-0013-4000-8000-000000000024', 'regime', 1, 250, 'adjusted',
     '{"inputs": ["volatility_20", "trend_50_200", "drawdown_250"], "precedence": ["volatile", "trending_up", "trending_down", "range_bound"], "volatile": {"volatility_20_at_least": 0.40}, "trending_up": {"trend_50_200_above": 0.05, "drawdown_250_above": -0.10}, "trending_down": {"trend_50_200_below": -0.05}}',
     'Whenever any input is undefined. A regime is a label, never a number; it is stored in feature_values.label.',
     false, '2026-09-01T00:00:00Z'),
    ('00000000-0013-4000-8000-000000000025', 'composite_return_1', 1, 2, 'adjusted',
     '{"weighting": "equal", "min_contributors": 10, "formula": "mean(return_1[i, t]) over every active universe instrument with a stored bar at t and at t-1"}',
     'Fewer than 10 instruments have a stored bar for both the session and the session before it (insufficient_contributors). An instrument missing either bar is excluded, never carried forward. This is a composite of one curated list; it is not, and must never be described as, a market index or a benchmark.',
     false, '2026-09-01T00:00:00Z');
