-- Paper trading: a simulated account over stored prices (feature 026).
--
-- Nothing here was traded and nothing here can be transmitted. An order exists only because a
-- person promoted an order intent they wrote down themselves, and it fills at a price that did not
-- exist when it was placed. Both of those are enforced below rather than in a service, because a
-- lookahead bug produces a flattering result that looks entirely plausible.

CREATE TABLE paper_accounts (
    id uuid PRIMARY KEY,
    -- One per person, enforced by the index rather than by a service. Being able to open a second
    -- account, or reset this one, turns a track record into a collection of attempts, and the best
    -- attempt is the one that gets shown.
    user_id uuid NOT NULL UNIQUE REFERENCES users(id),
    starting_cash numeric(24,12) NOT NULL CHECK (starting_cash > 0),
    accounting_currency text NOT NULL CHECK (accounting_currency ~ '^[A-Z]{3}$'),
    -- Feature 021's cost model, fixed when the account is opened so a paper run and a backtest of
    -- the same strategy are comparable without anybody configuring anything.
    brokerage_bps numeric(24,12) NOT NULL CHECK (brokerage_bps >= 0),
    brokerage_minimum numeric(24,12) NOT NULL CHECK (brokerage_minimum >= 0),
    slippage_bps numeric(24,12) NOT NULL CHECK (slippage_bps >= 0),
    currency_spread_bps numeric(24,12) NOT NULL CHECK (currency_spread_bps >= 0),
    opened_at timestamptz NOT NULL DEFAULT now(),
    -- Read by the composite foreign key on paper_orders, so a child row's own user_id cannot
    -- disagree with its account's owner.
    UNIQUE (id, user_id)
);

COMMENT ON TABLE paper_accounts IS
    'One simulated account per person. Its terms are fixed at opening: a record that can be tuned after the result is known is not a record.';

CREATE FUNCTION paper_account_terms_are_immutable() RETURNS trigger AS $$
BEGIN
    IF (OLD.user_id, OLD.starting_cash, OLD.accounting_currency, OLD.brokerage_bps,
        OLD.brokerage_minimum, OLD.slippage_bps, OLD.currency_spread_bps, OLD.opened_at)
       IS DISTINCT FROM
       (NEW.user_id, NEW.starting_cash, NEW.accounting_currency, NEW.brokerage_bps,
        NEW.brokerage_minimum, NEW.slippage_bps, NEW.currency_spread_bps, NEW.opened_at) THEN
        RAISE EXCEPTION 'a paper account''s terms are fixed when it is opened';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER paper_accounts_immutable
    BEFORE UPDATE ON paper_accounts
    FOR EACH ROW EXECUTE FUNCTION paper_account_terms_are_immutable();

CREATE TABLE paper_orders (
    id uuid PRIMARY KEY,
    account_id uuid NOT NULL,
    -- Denormalised, and held equal to the account's owner by the composite foreign key below. Every
    -- ownership predicate then reads this row's own column, so a query scoped only by account
    -- cannot widen access and a forgotten join cannot leak.
    user_id uuid NOT NULL REFERENCES users(id),
    -- The only way an order comes into existence, and it comes from exactly one intent. There is no
    -- path from a strategy, a signal or a scheduler: feature 021 measured the strategy and found it
    -- lost to two of its three benchmarks over ten years.
    intent_id uuid NOT NULL UNIQUE REFERENCES order_intents(id),
    instrument_id uuid NOT NULL REFERENCES instruments(id),
    direction text NOT NULL CHECK (direction IN ('buy', 'sell')),
    quantity numeric(24,12) NOT NULL CHECK (quantity > 0),
    -- What the person thought they would pay. Kept so the fill can be shown against it.
    expected_price numeric(24,12) NOT NULL CHECK (expected_price > 0),
    placed_session date NOT NULL,
    state text NOT NULL DEFAULT 'pending'
        CHECK (state IN ('pending', 'filled', 'cancelled', 'unfillable')),
    absence_reason text
        CHECK (absence_reason IS NULL OR absence_reason IN
               ('insufficient_cash', 'exceeds_position', 'no_price')),
    placed_at timestamptz NOT NULL DEFAULT now(),
    settled_at timestamptz,
    -- A reason is present exactly when the order could not be filled, and a settled moment is
    -- present exactly when it is no longer pending. Read from both sides so neither can drift.
    CHECK ((state = 'unfillable') = (absence_reason IS NOT NULL)),
    CHECK ((state = 'pending') = (settled_at IS NULL)),
    FOREIGN KEY (account_id, user_id) REFERENCES paper_accounts (id, user_id),
    UNIQUE (id, user_id)
);

CREATE INDEX paper_orders_owner_idx ON paper_orders (user_id, placed_at DESC);
CREATE INDEX paper_orders_pending_idx ON paper_orders (instrument_id, placed_session)
    WHERE state = 'pending';

COMMENT ON TABLE paper_orders IS
    'An order a person promoted from an intent they wrote down. No venue, order type, time in force or destination: the absence is the safeguard.';

CREATE TABLE paper_fills (
    id uuid PRIMARY KEY,
    -- One fill per order. There are no partial fills, so there is nothing to reconcile.
    order_id uuid NOT NULL UNIQUE,
    user_id uuid NOT NULL REFERENCES users(id),
    fill_session date NOT NULL,
    -- The stored open of that session, unmodified. Costs are recorded separately so the price can
    -- be checked against the bar.
    open_price numeric(24,12) NOT NULL CHECK (open_price > 0),
    quantity numeric(24,12) NOT NULL CHECK (quantity > 0),
    costs numeric(24,12) NOT NULL CHECK (costs >= 0),
    -- Negative for a buy, positive for a sale: value and costs together.
    cash_effect numeric(24,12) NOT NULL,
    conversion_rate numeric(24,12) NOT NULL CHECK (conversion_rate > 0),
    -- What the bar said when it was read. Feature 016 re-observes and corrects bars; a fill that
    -- silently moved with its bar would make the account unreconcilable week to week, so the
    -- divergence is reported instead of re-priced.
    bar_observed_at timestamptz NOT NULL,
    filled_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (order_id, user_id) REFERENCES paper_orders (id, user_id)
);

CREATE INDEX paper_fills_owner_idx ON paper_fills (user_id, fill_session);

COMMENT ON TABLE paper_fills IS
    'What an order became, recorded rather than derived: a fill is a statement about a moment, and re-deriving it would let a corrected bar rewrite the past.';

-- The constraint the whole feature rests on.
--
-- A check constraint cannot subquery, so this is a trigger. It stays in the database rather than in
-- a service because it is what makes filling at a price the person could already see impossible
-- rather than merely unlikely.
CREATE FUNCTION paper_fill_follows_its_order() RETURNS trigger AS $$
DECLARE
    placed date;
BEGIN
    SELECT placed_session INTO placed FROM paper_orders WHERE id = NEW.order_id;
    IF placed IS NULL THEN
        RAISE EXCEPTION 'a fill needs an order';
    END IF;
    IF NEW.fill_session <= placed THEN
        RAISE EXCEPTION
            'a paper order placed on % cannot fill at %: that price already existed when it was placed',
            placed, NEW.fill_session;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER paper_fills_follow_their_order
    BEFORE INSERT OR UPDATE ON paper_fills
    FOR EACH ROW EXECUTE FUNCTION paper_fill_follows_its_order();
