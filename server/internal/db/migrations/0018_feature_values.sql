-- Feature 013: runs, per-instrument outcomes, values and the universe composite.
--
-- All four tables are shared reference data derived from shared reference data. None carries
-- an owner, because none is user-owned; every read still sits behind an active session.
--
-- feature_values is long-format: one row per (instrument, session, definition). A row holds
-- exactly one of a numeric value, a label (the regime is a name) or an absence reason. That
-- check is what makes FR-014 structural: a zero standing in for an absence cannot be written,
-- and neither can a number standing in for a regime. No row at all means "not yet computed",
-- which the spec requires be distinguishable from "computed and undefined".

CREATE TABLE feature_runs (
    id uuid PRIMARY KEY,
    kind text NOT NULL CHECK (kind IN ('full', 'incremental', 'definition')),
    status text NOT NULL CHECK (status IN ('running', 'succeeded', 'partial', 'failed')),
    universe_id uuid NOT NULL REFERENCES research_universes(id),
    definition_name text CHECK (definition_name IS NULL OR btrim(definition_name) <> ''),
    trigger_run_id uuid REFERENCES import_runs(id),
    started_at timestamptz NOT NULL,
    finished_at timestamptz,
    instrument_count bigint NOT NULL DEFAULT 0 CHECK (instrument_count >= 0),
    value_count bigint NOT NULL DEFAULT 0 CHECK (value_count >= 0),
    app_version text NOT NULL CHECK (btrim(app_version) <> ''),
    CHECK ((status = 'running' AND finished_at IS NULL) OR (status <> 'running' AND finished_at IS NOT NULL)),
    CHECK ((kind = 'definition') = (definition_name IS NOT NULL))
);

CREATE INDEX feature_runs_started_idx ON feature_runs (started_at DESC);

CREATE TABLE feature_run_items (
    run_id uuid NOT NULL REFERENCES feature_runs(id),
    instrument_id uuid NOT NULL REFERENCES instruments(id),
    status text NOT NULL CHECK (status IN ('running', 'succeeded', 'failed', 'skipped')),
    from_session date,
    to_session date,
    value_count bigint NOT NULL DEFAULT 0 CHECK (value_count >= 0),
    error_code text,
    error_summary text,
    started_at timestamptz NOT NULL,
    finished_at timestamptz,
    PRIMARY KEY (run_id, instrument_id),
    CHECK (from_session IS NULL OR to_session IS NULL OR to_session >= from_session),
    CHECK ((status = 'failed') = (error_code IS NOT NULL))
);

CREATE INDEX feature_run_items_instrument_idx ON feature_run_items (instrument_id, started_at DESC);

CREATE TABLE feature_values (
    instrument_id uuid NOT NULL REFERENCES instruments(id),
    session_date date NOT NULL,
    definition_id uuid NOT NULL REFERENCES feature_definitions(id),
    value numeric(24,12),
    label text CHECK (label IS NULL OR btrim(label) <> ''),
    absence_reason text CHECK (absence_reason IS NULL OR absence_reason IN
        ('insufficient_history', 'window_gap', 'composite_undefined', 'zero_denominator')),
    currency text CHECK (currency IS NULL OR currency ~ '^[A-Z]{3}$'),
    computed_at timestamptz NOT NULL,
    run_id uuid NOT NULL REFERENCES feature_runs(id),
    PRIMARY KEY (instrument_id, session_date, definition_id),
    CHECK ((value IS NOT NULL)::int + (label IS NOT NULL)::int + (absence_reason IS NOT NULL)::int = 1)
);

CREATE INDEX feature_values_definition_session_idx ON feature_values (definition_id, session_date);

CREATE TABLE universe_composites (
    universe_id uuid NOT NULL REFERENCES research_universes(id),
    session_date date NOT NULL,
    definition_id uuid NOT NULL REFERENCES feature_definitions(id),
    mean_return numeric(24,12),
    contributor_count int NOT NULL CHECK (contributor_count >= 0),
    absence_reason text CHECK (absence_reason IS NULL OR absence_reason IN ('insufficient_contributors')),
    computed_at timestamptz NOT NULL,
    run_id uuid NOT NULL REFERENCES feature_runs(id),
    PRIMARY KEY (universe_id, session_date, definition_id),
    CHECK ((mean_return IS NOT NULL)::int + (absence_reason IS NOT NULL)::int = 1)
);
