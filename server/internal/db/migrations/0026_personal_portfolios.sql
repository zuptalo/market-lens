-- Feature 022: personal portfolio and holdings.
--
-- The first user-owned domain record in this product. Everything stored until now — instruments,
-- bars, features, signals, backtests — is the same for every user, which is why cross-user
-- isolation has only ever been tested on identity itself. Risk limits and order intents are built
-- on top of this, so the boundary is established here.
--
-- Two decisions are enforced by the schema rather than by the service, because each is a breach
-- rather than a bug if it is ever wrong:
--
--   * a trade's owner cannot differ from the owner of the portfolio it belongs to, held by a
--     composite foreign key rather than a trigger, so the two literally cannot disagree;
--   * a trade is superseded or withdrawn, never both, so a reader can always say which fact a row
--     is.
--
-- And one thing is deliberately absent. There is no position table, no cost column and no
-- realised-profit column. Every figure is a fold over the trades below, because a stored copy is a
-- second source of truth that every correction would have to repair — and a correction is a
-- first-class operation here, not an administrative afterthought.

CREATE TABLE portfolios (
    id uuid PRIMARY KEY,
    -- One per person, for now. A second would make every figure ambiguous about which portfolio it
    -- belongs to, and nothing in this milestone needs one.
    user_id uuid NOT NULL UNIQUE REFERENCES users(id),
    -- The person's own currency, not the euro. Feature 021 chose the euro for backtesting because
    -- every stored rate has it as a base; a personal tracker cannot, because reporting a Stockholm
    -- holder's money in euro answers a question nobody asked. Converting into anything else
    -- crosses through the euro in one session.
    accounting_currency text NOT NULL CHECK (accounting_currency ~ '^[A-Z]{3}$'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    -- Read by the composite foreign key below. A unique constraint on (id, user_id) is redundant
    -- against the primary key and exists solely so the child table can point at both columns.
    UNIQUE (id, user_id)
);

COMMENT ON TABLE portfolios IS
    'One person''s portfolio. Private: no role grants access to another person''s, because ownership here is not administrative.';

CREATE TABLE portfolio_trades (
    id uuid PRIMARY KEY,
    portfolio_id uuid NOT NULL,
    -- Denormalised from the portfolio on purpose. Every ownership predicate reads this column
    -- directly, so a query cannot accidentally scope by portfolio alone and a forgotten join
    -- cannot widen access. The composite foreign key below is what stops the two disagreeing.
    user_id uuid NOT NULL REFERENCES users(id),
    instrument_id uuid NOT NULL REFERENCES instruments(id),
    direction text NOT NULL CHECK (direction IN ('buy', 'sell')),
    -- Fractional is allowed: brokers fill fractions, and refusing would make a real holding
    -- unrecordable. There is no shorting, so the quantity is positive and a sale that would go
    -- negative is refused by the service with the quantity actually held.
    quantity numeric(24,12) NOT NULL CHECK (quantity > 0),
    -- Per share, in the instrument's own listing currency. Conversion happens when a holding is
    -- valued, never when it is recorded: a stored converted price would freeze one session's rate
    -- into a fact about what somebody paid.
    price numeric(24,12) NOT NULL CHECK (price > 0),
    costs numeric(24,12) NOT NULL DEFAULT 0 CHECK (costs >= 0),
    trade_date date NOT NULL,
    -- With trade_date, this totally orders the first-in-first-out walk. Two purchases on one date
    -- have no inherent order, and ordering by identifier would make the cost basis depend on a
    -- UUID nobody can see. A correction does not change it: the person is fixing what a trade
    -- said, not when they told us about it.
    sequence bigint NOT NULL CHECK (sequence > 0),
    -- Deferred to commit time: a correction supersedes the old version before inserting the new
    -- one, so that only one of them is ever live against the partial unique index below — and at
    -- the moment of the update the row it points at does not exist yet.
    superseded_by uuid REFERENCES portfolio_trades(id) DEFERRABLE INITIALLY DEFERRED,
    withdrawn_at timestamptz,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    changed_at timestamptz,
    FOREIGN KEY (portfolio_id, user_id) REFERENCES portfolios (id, user_id),
    -- A holding nobody has yet is not a holding.
    CHECK (trade_date <= current_date),
    -- Superseded or withdrawn, never both.
    CHECK (superseded_by IS NULL OR withdrawn_at IS NULL)
);

-- Unique among the versions that count, not across the whole history. A correction keeps the
-- sequence of the version it replaces — the person is fixing what a trade said, not when they told
-- us about it, and first-in-first-out consumes by that order — so the superseded row keeps its
-- number too and the constraint has to allow that.
CREATE UNIQUE INDEX portfolio_trades_live_sequence_idx
    ON portfolio_trades (portfolio_id, sequence)
    WHERE superseded_by IS NULL AND withdrawn_at IS NULL;

CREATE INDEX portfolio_trades_owner_idx ON portfolio_trades (user_id, trade_date DESC, sequence DESC);
CREATE INDEX portfolio_trades_fold_idx ON portfolio_trades (portfolio_id, instrument_id, trade_date, sequence);

COMMENT ON TABLE portfolio_trades IS
    'What a person asserted they traded. Not observed by the product, so it is correctable — and a correction supersedes rather than overwrites, because a portfolio whose history changes silently cannot be reconciled against a broker statement.';
