CREATE TABLE exchange_sessions (
    exchange_id uuid NOT NULL REFERENCES exchanges(id),
    session_date date NOT NULL,
    status text NOT NULL CHECK (status IN ('open', 'half_day', 'closed')),
    opens_at timestamptz,
    closes_at timestamptz,
    source_reference text NOT NULL CHECK (btrim(source_reference) <> ''),
    PRIMARY KEY (exchange_id, session_date),
    CHECK ((status = 'closed' AND opens_at IS NULL AND closes_at IS NULL) OR
           (status IN ('open', 'half_day') AND opens_at IS NOT NULL AND closes_at IS NOT NULL AND closes_at > opens_at))
);

CREATE TABLE import_items (
    run_id uuid NOT NULL REFERENCES import_runs(id),
    instrument_id uuid NOT NULL REFERENCES instruments(id),
    requested_from date NOT NULL,
    requested_to date NOT NULL,
    status text NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'partial', 'failed', 'cancelled')),
    processed_count bigint NOT NULL DEFAULT 0 CHECK (processed_count >= 0),
    accepted_count bigint NOT NULL DEFAULT 0 CHECK (accepted_count >= 0),
    rejected_count bigint NOT NULL DEFAULT 0 CHECK (rejected_count >= 0),
    flagged_count bigint NOT NULL DEFAULT 0 CHECK (flagged_count >= 0),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    started_at timestamptz,
    finished_at timestamptz,
    error_code text,
    error_summary text,
    PRIMARY KEY (run_id, instrument_id),
    CHECK (requested_to >= requested_from),
    CHECK (accepted_count + rejected_count <= processed_count),
    CHECK (flagged_count <= processed_count),
    CHECK ((status IN ('queued', 'running') AND finished_at IS NULL) OR
           (status IN ('succeeded', 'partial', 'failed', 'cancelled') AND finished_at IS NOT NULL))
);

CREATE TABLE daily_price_bars (
    instrument_id uuid NOT NULL REFERENCES instruments(id),
    session_date date NOT NULL,
    open numeric(20,8) NOT NULL CHECK (open > 0),
    high numeric(20,8) NOT NULL CHECK (high > 0),
    low numeric(20,8) NOT NULL CHECK (low > 0),
    close numeric(20,8) NOT NULL CHECK (close > 0),
    adjusted_close numeric(20,8) CHECK (adjusted_close > 0),
    volume bigint NOT NULL CHECK (volume >= 0),
    currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    provider text NOT NULL CHECK (btrim(provider) <> ''),
    source_hash text NOT NULL CHECK (btrim(source_hash) <> ''),
    import_run_id uuid NOT NULL REFERENCES import_runs(id),
    first_observed_at timestamptz NOT NULL,
    last_observed_at timestamptz NOT NULL,
    PRIMARY KEY (instrument_id, session_date),
    CHECK (low <= open AND open <= high AND low <= close AND close <= high),
    CHECK (last_observed_at >= first_observed_at)
);

CREATE INDEX daily_price_bars_instrument_session_idx ON daily_price_bars (instrument_id, session_date DESC);
CREATE INDEX daily_price_bars_session_instrument_idx ON daily_price_bars (session_date, instrument_id);

CREATE TABLE price_bar_revisions (
    id uuid PRIMARY KEY,
    instrument_id uuid NOT NULL REFERENCES instruments(id),
    session_date date NOT NULL,
    revision integer NOT NULL CHECK (revision > 0),
    open numeric(20,8) NOT NULL CHECK (open > 0),
    high numeric(20,8) NOT NULL CHECK (high > 0),
    low numeric(20,8) NOT NULL CHECK (low > 0),
    close numeric(20,8) NOT NULL CHECK (close > 0),
    adjusted_close numeric(20,8) CHECK (adjusted_close > 0),
    volume bigint NOT NULL CHECK (volume >= 0),
    currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    provider text NOT NULL CHECK (btrim(provider) <> ''),
    source_hash text NOT NULL CHECK (btrim(source_hash) <> ''),
    import_run_id uuid NOT NULL REFERENCES import_runs(id),
    first_observed_at timestamptz NOT NULL,
    last_observed_at timestamptz NOT NULL,
    superseding_run_id uuid NOT NULL REFERENCES import_runs(id),
    superseded_at timestamptz NOT NULL,
    UNIQUE (instrument_id, session_date, revision),
    CHECK (low <= open AND open <= high AND low <= close AND close <= high)
);

CREATE TABLE corporate_actions (
    id uuid PRIMARY KEY,
    instrument_id uuid NOT NULL REFERENCES instruments(id),
    provider text NOT NULL CHECK (btrim(provider) <> ''),
    provider_action_id text NOT NULL CHECK (btrim(provider_action_id) <> ''),
    action_type text NOT NULL CHECK (action_type IN ('split','reverse_split','dividend','symbol_change','delisting')),
    ex_date date NOT NULL,
    effective_date date,
    ratio numeric(20,8) CHECK (ratio > 0),
    amount numeric(20,8) CHECK (amount >= 0),
    currency text CHECK (currency IS NULL OR currency ~ '^[A-Z]{3}$'),
    old_symbol text,
    new_symbol text,
    source_hash text NOT NULL CHECK (btrim(source_hash) <> ''),
    import_run_id uuid NOT NULL REFERENCES import_runs(id),
    first_observed_at timestamptz NOT NULL,
    last_observed_at timestamptz NOT NULL,
    UNIQUE (provider, provider_action_id),
    CHECK (last_observed_at >= first_observed_at)
);

CREATE TABLE data_quality_findings (
    id uuid PRIMARY KEY,
    instrument_id uuid NOT NULL REFERENCES instruments(id),
    session_date date,
    run_id uuid NOT NULL REFERENCES import_runs(id),
    rule text NOT NULL CHECK (rule IN ('duplicate_source_row','out_of_order_source_row','invalid_ohlc','non_positive_price',
        'negative_volume','zero_volume','missing_session','provider_gap','suspicious_jump','possible_corporate_action_discontinuity')),
    severity text NOT NULL CHECK (severity IN ('warning','error')),
    disposition text NOT NULL CHECK (disposition IN ('flagged','rejected')),
    detail text NOT NULL CHECK (btrim(detail) <> ''),
    status text NOT NULL CHECK (status IN ('open','resolved','accepted_limitation')),
    created_at timestamptz NOT NULL,
    resolved_at timestamptz,
    resolving_run_id uuid REFERENCES import_runs(id),
    CHECK ((status = 'open' AND resolved_at IS NULL AND resolving_run_id IS NULL) OR status <> 'open')
);

CREATE INDEX data_quality_findings_status_idx ON data_quality_findings (status, severity, session_date DESC);
CREATE INDEX data_quality_findings_instrument_idx ON data_quality_findings (instrument_id, status);

CREATE FUNCTION prevent_append_only_update() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION '% rows are append-only', TG_TABLE_NAME;
END;
$$;

CREATE TRIGGER price_bar_revisions_append_only
    BEFORE UPDATE OR DELETE ON price_bar_revisions
    FOR EACH ROW EXECUTE FUNCTION prevent_append_only_update();
