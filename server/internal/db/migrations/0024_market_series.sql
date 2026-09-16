-- Feature 021: the series a backtest compares against, and converts with.
--
-- A benchmark is not an instrument, and this is the migration that makes that structural rather
-- than a convention somebody has to remember. An index stored in `instruments` would appear in
-- the Markets listing, be computed over by the feature engine, enter a universe by way of a
-- membership row, and end up scored by the very strategy it exists to judge — all without anyone
-- deciding it should. The alternative was a synthetic exchange plus an exclusion in every
-- existing read; the exclusions would have been the tell that the model was wrong.
--
-- Two rules are enforced here rather than in code:
--   * one market has one benchmark, so a result cannot pick whichever series flatters it;
--   * a rate is stored in one direction only, so the two directions can never disagree.

CREATE TABLE benchmark_series (
    id uuid PRIMARY KEY,
    -- The provider's own symbol. Stored as the provider states it so a coverage question can be
    -- asked of the provider in its own vocabulary.
    code text NOT NULL UNIQUE CHECK (btrim(code) <> ''),
    name text NOT NULL CHECK (btrim(name) <> ''),
    currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    -- The market this series is the benchmark *for*. A Swedish holding compared against a
    -- Norwegian index would be a claim about a relationship that does not exist.
    mic text NOT NULL UNIQUE REFERENCES exchanges(mic),
    created_at timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE benchmark_series IS
    'Index series the product compares results against. Not instruments: no exchange membership, no sector, never scored.';

CREATE TABLE benchmark_points (
    series_id uuid NOT NULL REFERENCES benchmark_series(id),
    session_date date NOT NULL,
    close numeric(20,8) NOT NULL CHECK (close > 0),
    first_observed_at timestamptz NOT NULL DEFAULT now(),
    last_observed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (series_id, session_date)
);

CREATE TABLE fx_rates (
    base text NOT NULL CHECK (base ~ '^[A-Z]{3}$'),
    quote text NOT NULL CHECK (quote ~ '^[A-Z]{3}$'),
    session_date date NOT NULL,
    -- The precision the feature and strategy layers already use. A rate rounded to fewer places
    -- than the money it converts makes a conversion irreproducible at the last digit, which is
    -- the whole feature's first success criterion.
    rate numeric(24,12) NOT NULL CHECK (rate > 0),
    first_observed_at timestamptz NOT NULL DEFAULT now(),
    last_observed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (base, quote, session_date),
    -- A currency against itself is one, never a stored fact.
    CHECK (base <> quote)
);

COMMENT ON TABLE fx_rates IS
    'Daily rates as the provider quotes them. The inverse direction divides rather than being stored, so the two can never disagree.';

-- The four benchmarks for the four markets in the curated universe.
--
-- OMXC25.INDX begins 2016-12-19, 110 days after this product''s stored history begins. That gap
-- is reported as an unavailable comparison, not papered over: OMXC20, the index it replaced, is
-- a different thing, and splicing them would present two series as one.
INSERT INTO benchmark_series (id, code, name, currency, mic) VALUES
    ('00000000-0021-4000-8000-000000000001', 'OMXS30.INDX', 'OMX Stockholm 30', 'SEK', 'XSTO'),
    ('00000000-0021-4000-8000-000000000002', 'OMXH25.INDX', 'OMX Helsinki 25', 'EUR', 'XHEL'),
    ('00000000-0021-4000-8000-000000000003', 'OBX.INDX', 'Oslo Børs OBX', 'NOK', 'XOSL'),
    ('00000000-0021-4000-8000-000000000004', 'OMXC25.INDX', 'OMX Copenhagen 25', 'DKK', 'XCSE');
