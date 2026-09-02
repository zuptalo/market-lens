-- Feature 015: deterministic strategies and signals.
--
-- The feature engine records what was true. A strategy records what a stated method made of it,
-- as of a session, with the reasons. The distinction matters: a feature is an observation and a
-- signal is a view, and the product's position is that a view must be arguable — which means the
-- reasons are stored beside it, not derived later from whatever the code says today.
--
-- Three rules are enforced here rather than by convention:
--   * a signal is a view or a stated absence, never both and never neither, so a HOLD can never
--     stand in for missing data;
--   * a score maps to exactly one action, because the bands are stored with the version;
--   * a version is superseded, never edited, so a signal recorded months ago stays reproducible
--     from the definition that produced it.

CREATE TABLE strategies (
    id uuid PRIMARY KEY,
    name text NOT NULL CHECK (btrim(name) <> ''),
    version int NOT NULL CHECK (version >= 1),
    title text NOT NULL CHECK (btrim(title) <> ''),
    -- What the version is trying to express, in prose a person can disagree with.
    intent text NOT NULL CHECK (btrim(intent) <> ''),
    -- Why it exists. Stored rather than templated so no surface can show a score without it.
    caveat text NOT NULL CHECK (btrim(caveat) <> ''),
    -- The whole version: factors, weights, transforms, action bands, liquidity gates. One
    -- document because it *is* the version — splitting it into rows would let a factor change
    -- without publishing a version.
    parameters jsonb NOT NULL,
    published_at timestamptz NOT NULL,
    superseded_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (name, version),
    CHECK (superseded_at IS NULL OR superseded_at >= published_at)
);

-- One current version per strategy family.
CREATE UNIQUE INDEX strategies_current_idx ON strategies (name) WHERE superseded_at IS NULL;

COMMENT ON TABLE strategies IS
    'Published, versioned strategy definitions. A change publishes a new version and supersedes the old one; nothing is edited in place.';

CREATE TABLE strategy_runs (
    id uuid PRIMARY KEY,
    strategy_id uuid NOT NULL REFERENCES strategies(id),
    kind text NOT NULL CHECK (kind IN ('full', 'incremental', 'strategy')),
    status text NOT NULL CHECK (status IN ('running', 'succeeded', 'partial', 'failed')),
    universe_id uuid NOT NULL REFERENCES research_universes(id),
    trigger_feature_run_id uuid REFERENCES feature_runs(id),
    started_at timestamptz NOT NULL,
    finished_at timestamptz,
    instrument_count bigint NOT NULL DEFAULT 0 CHECK (instrument_count >= 0),
    signal_count bigint NOT NULL DEFAULT 0 CHECK (signal_count >= 0),
    app_version text NOT NULL CHECK (btrim(app_version) <> ''),
    CHECK ((status = 'running' AND finished_at IS NULL) OR (status <> 'running' AND finished_at IS NOT NULL))
);

CREATE INDEX strategy_runs_started_idx ON strategy_runs (started_at DESC);

CREATE TABLE strategy_run_items (
    run_id uuid NOT NULL REFERENCES strategy_runs(id),
    instrument_id uuid NOT NULL REFERENCES instruments(id),
    status text NOT NULL CHECK (status IN ('running', 'succeeded', 'failed', 'skipped')),
    from_session date,
    to_session date,
    signal_count bigint NOT NULL DEFAULT 0 CHECK (signal_count >= 0),
    error_code text,
    error_summary text,
    started_at timestamptz NOT NULL,
    finished_at timestamptz,
    PRIMARY KEY (run_id, instrument_id),
    CHECK (from_session IS NULL OR to_session IS NULL OR to_session >= from_session),
    CHECK ((status = 'failed') = (error_code IS NOT NULL))
);

CREATE INDEX strategy_run_items_instrument_idx ON strategy_run_items (instrument_id, started_at DESC);

CREATE TABLE signals (
    instrument_id uuid NOT NULL REFERENCES instruments(id),
    session_date date NOT NULL,
    -- The version, not the family: a superseded version keeps its own signals.
    strategy_id uuid NOT NULL REFERENCES strategies(id),
    score numeric(24,12),
    action text CHECK (action IS NULL OR action IN ('BUY', 'HOLD', 'REDUCE', 'SELL', 'WATCH')),
    confidence numeric(24,12),
    absence_reason text CHECK (absence_reason IS NULL OR absence_reason IN
        ('insufficient_history', 'feature_unavailable', 'composite_undefined', 'liquidity_excluded')),
    -- Ordered contributions: factor, feature, the value read and its session, the normalised
    -- factor score, the weight, and the contribution. Read only with their signal, never across
    -- signals, so they live on the row rather than in a table seven times larger.
    contributions jsonb NOT NULL DEFAULT '[]'::jsonb,
    -- Sum of the available factors' weights. The contributions divided by this equal the score,
    -- which is what makes the explanation reconcile rather than merely accompany.
    divisor numeric(24,12),
    computed_at timestamptz NOT NULL,
    run_id uuid NOT NULL REFERENCES strategy_runs(id),
    PRIMARY KEY (instrument_id, session_date, strategy_id),
    -- A view or a stated absence. This is the constraint that stops HOLD meaning "no data".
    CHECK (
        (score IS NOT NULL AND action IS NOT NULL AND confidence IS NOT NULL AND absence_reason IS NULL)
        OR
        (score IS NULL AND action IS NULL AND confidence IS NULL AND absence_reason IS NOT NULL)
    ),
    CHECK (score IS NULL OR (score >= -1 AND score <= 1)),
    CHECK (confidence IS NULL OR (confidence >= 0 AND confidence <= 1)),
    CHECK (jsonb_typeof(contributions) = 'array')
);

-- The ranked view: one strategy version, one session, descending score.
CREATE INDEX signals_ranking_idx ON signals (strategy_id, session_date, score DESC);

COMMENT ON TABLE signals IS
    'One strategy version''s view of one instrument as of one session — or the stated reason no view could be formed. Never advice.';

-- The first strategy version.
--
-- Seven factors over definitions the engine already publishes. Weights are stated, not tuned:
-- nothing here has been fitted to data, and the caveat says so, because a weight that looks
-- optimised invites a reader to believe it was.
INSERT INTO strategies (id, name, version, title, intent, caveat, parameters, published_at) VALUES (
    '00000000-0015-4000-8000-000000000001',
    'momentum_trend',
    1,
    'Momentum and trend',
    'Ranks the curated universe by medium-term momentum and trend agreement, comparing each '
    || 'instrument with the rest of the universe for the same session rather than against a '
    || 'fixed threshold, and penalising instruments whose recent movement has been unusually '
    || 'volatile. It expresses one common way of reading a market; it does not express the only '
    || 'way, or the best one.',
    'This strategy exists to prove the platform can hold a view reproducibly and explain it. Its '
    || 'weights are stated rather than fitted, it has not been tested against historical outcomes, '
    || 'and nothing it produces is advice or a prediction. Whether its views are any good is a '
    || 'question backtesting answers.',
    jsonb_build_object(
        'factors', jsonb_build_array(
            jsonb_build_object('name', 'momentum_90', 'feature', 'return_90',
                'mode', 'cross_sectional', 'weight', '0.25',
                'description', 'Ninety-session return, ranked against the universe for the session.'),
            jsonb_build_object('name', 'momentum_20', 'feature', 'return_20',
                'mode', 'cross_sectional', 'weight', '0.15',
                'description', 'Twenty-session return, ranked against the universe.'),
            jsonb_build_object('name', 'trend', 'feature', 'trend_50_200',
                'mode', 'absolute', 'weight', '0.20',
                'transform', jsonb_build_object('kind', 'linear_clamped', 'lower', '-0.15', 'upper', '0.15'),
                'description', 'How far the fifty-session average sits above or below the two-hundred-session average.'),
            jsonb_build_object('name', 'relative_strength', 'feature', 'relative_strength_90',
                'mode', 'cross_sectional', 'weight', '0.20',
                'description', 'Ninety-session return against the universe composite, ranked.'),
            jsonb_build_object('name', 'volume_confirmation', 'feature', 'volume_ratio_20',
                'mode', 'absolute', 'weight', '0.05',
                'transform', jsonb_build_object('kind', 'linear_clamped', 'lower', '0.5', 'upper', '2.0'),
                'description', 'Whether recent volume supports the move or contradicts it.'),
            jsonb_build_object('name', 'volatility_penalty', 'feature', 'volatility_20',
                'mode', 'absolute', 'weight', '0.10',
                'transform', jsonb_build_object('kind', 'linear_clamped_inverted', 'lower', '0.15', 'upper', '0.60'),
                'description', 'Penalises unusually volatile movement; higher volatility scores lower.'),
            jsonb_build_object('name', 'regime', 'feature', 'regime',
                'mode', 'absolute', 'weight', '0.05',
                'transform', jsonb_build_object('kind', 'label_map', 'values', jsonb_build_object(
                    'trending_up', '1', 'range_bound', '0', 'trending_down', '-1', 'volatile', '-0.5')),
                'description', 'The market regime the engine labelled for this instrument and session.')
        ),
        'action_bands', jsonb_build_array(
            jsonb_build_object('lower', '-1', 'upper', '-0.5', 'action', 'SELL'),
            jsonb_build_object('lower', '-0.5', 'upper', '-0.2', 'action', 'REDUCE'),
            jsonb_build_object('lower', '-0.2', 'upper', '0.2', 'action', 'HOLD'),
            jsonb_build_object('lower', '0.2', 'upper', '0.5', 'action', 'WATCH'),
            jsonb_build_object('lower', '0.5', 'upper', '1', 'action', 'BUY')
        ),
        'liquidity', jsonb_build_object(
            'minimum_stored_sessions', 200,
            'description', 'An instrument with less history than the longest factor window cannot be scored.'),
        'confidence', jsonb_build_object(
            'kind', 'agreement_times_coverage',
            'description', 'The share of available contribution weight agreeing with the score, '
                || 'scaled by the share of the strategy''s weight that was available. It measures '
                || 'agreement between factors, not the probability that the view is correct.')
    ),
    TIMESTAMPTZ '2026-09-02 00:00:00Z'
);
