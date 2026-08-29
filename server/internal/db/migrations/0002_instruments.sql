CREATE TABLE exchanges (
    id uuid PRIMARY KEY,
    mic text NOT NULL UNIQUE CHECK (mic ~ '^[A-Z0-9]{4}$'),
    name text NOT NULL CHECK (btrim(name) <> ''),
    country text NOT NULL CHECK (country ~ '^[A-Z]{2}$'),
    currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    timezone text NOT NULL CHECK (btrim(timezone) <> ''),
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE instruments (
    id uuid PRIMARY KEY,
    exchange_id uuid NOT NULL REFERENCES exchanges(id),
    isin text NOT NULL CHECK (isin ~ '^[A-Z]{2}[A-Z0-9]{9}[0-9]$'),
    ticker text NOT NULL CHECK (btrim(ticker) <> '' AND ticker = upper(ticker)),
    name text NOT NULL CHECK (btrim(name) <> ''),
    currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    country text NOT NULL CHECK (country ~ '^[A-Z]{2}$'),
    instrument_type text NOT NULL CHECK (instrument_type IN ('common_stock')),
    sector text,
    industry text,
    active boolean NOT NULL DEFAULT true,
    purchasability_status text NOT NULL CHECK (purchasability_status IN ('user_confirmed', 'unverified', 'unavailable')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX instruments_active_exchange_ticker_key
    ON instruments (exchange_id, ticker) WHERE active;
CREATE UNIQUE INDEX instruments_active_exchange_isin_key
    ON instruments (exchange_id, isin) WHERE active;
CREATE INDEX instruments_ticker_search_idx ON instruments (lower(ticker));
CREATE INDEX instruments_name_search_idx ON instruments (lower(name));
CREATE INDEX instruments_isin_search_idx ON instruments (isin);
CREATE INDEX instruments_filter_idx ON instruments (exchange_id, country, currency, active);

CREATE TABLE provider_instruments (
    provider text NOT NULL CHECK (btrim(provider) <> ''),
    provider_symbol text NOT NULL CHECK (btrim(provider_symbol) <> ''),
    instrument_id uuid NOT NULL REFERENCES instruments(id),
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, provider_symbol)
);

CREATE UNIQUE INDEX provider_instruments_one_active_mapping_idx
    ON provider_instruments (provider, instrument_id) WHERE active;

CREATE TABLE research_universes (
    id uuid PRIMARY KEY,
    code text NOT NULL UNIQUE CHECK (code ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$'),
    name text NOT NULL CHECK (btrim(name) <> ''),
    description text NOT NULL DEFAULT '',
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE universe_memberships (
    universe_id uuid NOT NULL REFERENCES research_universes(id),
    instrument_id uuid NOT NULL REFERENCES instruments(id),
    included_from date NOT NULL,
    included_to date,
    curation_source text NOT NULL CHECK (btrim(curation_source) <> ''),
    curation_note text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (universe_id, instrument_id, included_from),
    CHECK (included_to IS NULL OR included_to >= included_from)
);

CREATE UNIQUE INDEX universe_memberships_current_idx
    ON universe_memberships (universe_id, instrument_id) WHERE included_to IS NULL;
CREATE INDEX universe_memberships_effective_idx
    ON universe_memberships (universe_id, included_from, included_to);

CREATE TABLE import_runs (
    id uuid PRIMARY KEY,
    kind text NOT NULL CHECK (kind IN ('universe_sync', 'backfill', 'daily_update', 'retry')),
    provider text NOT NULL CHECK (btrim(provider) <> ''),
    requested_from date,
    requested_to date,
    status text NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'partial', 'failed', 'cancelled')),
    parent_run_id uuid REFERENCES import_runs(id),
    started_at timestamptz NOT NULL,
    finished_at timestamptz,
    processed_count bigint NOT NULL DEFAULT 0 CHECK (processed_count >= 0),
    accepted_count bigint NOT NULL DEFAULT 0 CHECK (accepted_count >= 0),
    rejected_count bigint NOT NULL DEFAULT 0 CHECK (rejected_count >= 0),
    flagged_count bigint NOT NULL DEFAULT 0 CHECK (flagged_count >= 0),
    error_code text,
    error_summary text,
    app_version text NOT NULL,
    CHECK (requested_to IS NULL OR requested_from IS NOT NULL),
    CHECK (requested_to IS NULL OR requested_to >= requested_from),
    CHECK (accepted_count + rejected_count <= processed_count),
    CHECK (flagged_count <= processed_count),
    CHECK ((status IN ('queued', 'running') AND finished_at IS NULL) OR
           (status IN ('succeeded', 'partial', 'failed', 'cancelled') AND finished_at IS NOT NULL))
);

CREATE INDEX import_runs_started_idx ON import_runs (started_at DESC);
CREATE INDEX import_runs_status_idx ON import_runs (status, started_at DESC);

CREATE FUNCTION prevent_terminal_import_run_update() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.status IN ('succeeded', 'partial', 'failed', 'cancelled') THEN
        RAISE EXCEPTION 'terminal import runs are immutable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER import_runs_terminal_immutable
    BEFORE UPDATE OR DELETE ON import_runs
    FOR EACH ROW EXECUTE FUNCTION prevent_terminal_import_run_update();
