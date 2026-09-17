-- Feature 023: personal risk limits.
--
-- A limit here is somebody's own rule, written down. The product publishes none, suggests none, and
-- has no opinion about whether a quarter in one company is too much — which is why this migration
-- seeds nothing at all. "You should hold no more than 25% in one company" is advice; "you said 25%,
-- you are at 41%" is a rule its owner wrote, applied. The absence of seed data is what keeps the
-- product on the second side of that line, and the migration test asserts it.
--
-- Nothing about an evaluation is stored. There is no breach row, no observed-at column and no state
-- column, because a limit's state is the person's holdings now against their limits now — the same
-- discipline feature 022 applies to positions and profit, and for the same reason: a stored derived
-- figure is a second source of truth that every change has to repair, and here the changes arrive
-- nightly with every new price.

CREATE TABLE risk_limits (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users(id),
    -- Exactly the four kinds the product can measure from what it stores. A kind it cannot evaluate
    -- is not a kind it may accept: a stored limit that could never report anything would be a rule
    -- somebody believed they were being held to.
    kind text NOT NULL CHECK (kind IN
        ('instrument_share', 'sector_share', 'market_share', 'holding_count')),
    threshold numeric(24,12) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    -- One of each kind per person. Two thresholds for the same thing is a contradiction rather than
    -- a refinement, and "which one applies" has no good answer.
    UNIQUE (user_id, kind),
    -- A share is a proportion of a portfolio: more than all of it is a typo, and none of it is not
    -- a rule anybody could satisfy.
    CHECK (
        kind = 'holding_count'
        OR (threshold > 0 AND threshold <= 1)
    ),
    -- A holding count is a whole number of things somebody owns.
    CHECK (
        kind <> 'holding_count'
        OR (threshold >= 1 AND threshold = trunc(threshold))
    )
);

CREATE INDEX risk_limits_owner_idx ON risk_limits (user_id, kind);

COMMENT ON TABLE risk_limits IS
    'One person''s own rules about what they hold. The product stores what they wrote down and reports where they stand; it publishes no defaults and suggests no thresholds.';
