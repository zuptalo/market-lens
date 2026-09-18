-- Consented email and Web Push alerts (feature 027).
--
-- Nothing here is on by default and nothing is sent to somebody who did not ask. A product that
-- mails a person unasked has made a decision on their behalf, which is the thing this codebase has
-- refused at every turn.

-- The instance's VAPID key pair, self-provisioned on first start.
--
-- Shaped after instance_signing_key (migration 0011) on purpose, so there is one pattern for "a
-- secret this instance owns" rather than two. The reason is the same and worse: restoring a backup
-- without this key invalidates every push subscription in existence, and the failure is silent —
-- pushes simply stop arriving, with no error anywhere.
--
-- EXTERNAL_CREDENTIAL_KEY still does not belong here. That key encrypts rows in this same database,
-- so storing it here would be circular. This one encrypts nothing here: it is an identity this
-- server proves to a push service, which is the shape of secret the signing key already is.
CREATE TABLE instance_push_key (
    id           uuid PRIMARY KEY,
    -- A P-256 private scalar and the uncompressed public point it belongs to.
    private_key  bytea NOT NULL CHECK (octet_length(private_key) = 32),
    public_key   bytea NOT NULL CHECK (octet_length(public_key) = 65),
    -- The `sub` claim of the VAPID assertion: how a push service reaches whoever runs this.
    subject      text NOT NULL CHECK (btrim(subject) <> ''),
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- Exactly one key, forever. This is what makes simultaneous first starts converge without an
-- advisory lock: provisioning is INSERT ... ON CONFLICT DO NOTHING followed by an unconditional
-- SELECT, so the loser of the race adopts the winner's key.
CREATE UNIQUE INDEX instance_push_key_singleton ON instance_push_key ((true));

COMMENT ON TABLE instance_push_key IS
    'The VAPID key pair this instance signs push requests with. Self-provisioned and database-resident so a restored backup keeps working; rotating it would silently invalidate every subscription, which is why nothing rotates it.';

-- What a person has asked to be told about, per kind and per channel.
--
-- No seed data, deliberately. An absent row and enabled = false mean the same thing, and both mean
-- nothing is sent; writing eight rows for every new account would look like a decision had been
-- made for them.
CREATE TABLE notification_preferences (
    user_id    uuid NOT NULL REFERENCES users(id),
    kind       text NOT NULL CHECK (kind IN
                   ('decision_waiting', 'paper_fill', 'pipeline_failure', 'signal_change')),
    channel    text NOT NULL CHECK (channel IN ('email', 'web_push')),
    enabled    boolean NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, kind, channel)
);

COMMENT ON TABLE notification_preferences IS
    'One row per person per kind per channel. Off until asked for: an absent row and a disabled one mean the same thing.';

-- The hours during which nothing arrives.
--
-- Local wall-clock times plus an IANA zone rather than a UTC offset, because a person means "while
-- I am asleep" and daylight saving moves that against UTC twice a year. A window whose end is
-- before its start crosses midnight, which is the ordinary case.
CREATE TABLE notification_quiet_hours (
    user_id    uuid PRIMARY KEY REFERENCES users(id),
    starts_at  time NOT NULL,
    ends_at    time NOT NULL,
    timezone   text NOT NULL CHECK (btrim(timezone) <> ''),
    updated_at timestamptz NOT NULL DEFAULT now(),
    -- A window of zero length would silence everything forever while looking like a setting.
    CHECK (starts_at <> ends_at)
);

-- One per device a person subscribed.
--
-- Nothing is recorded about the device beyond a label the person chose. A user agent string is a
-- fingerprint, a push does not need one, and a subscription list would otherwise quietly become a
-- record of where somebody reads their mail.
CREATE TABLE push_subscriptions (
    id           uuid PRIMARY KEY,
    user_id      uuid NOT NULL REFERENCES users(id),
    endpoint     text NOT NULL CHECK (btrim(endpoint) <> ''),
    -- Exactly what the browser produced: its public point and its shared secret.
    p256dh       bytea NOT NULL CHECK (octet_length(p256dh) = 65),
    auth         bytea NOT NULL CHECK (octet_length(auth) = 16),
    label        text NOT NULL CHECK (btrim(label) <> '' AND length(label) <= 120),
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    -- Subscribing the same browser twice replaces rather than duplicates.
    UNIQUE (user_id, endpoint)
);

-- One row per person per kind per event. Never one shared row with a list of recipients: that would
-- make "delivered" ambiguous the moment one channel succeeded and another failed, and it would put
-- one person's delivery state where another person's query could read it.
CREATE TABLE notifications (
    id           uuid PRIMARY KEY,
    user_id      uuid NOT NULL REFERENCES users(id),
    kind         text NOT NULL CHECK (kind IN
                     ('decision_waiting', 'paper_fill', 'pipeline_failure', 'signal_change')),
    channel      text NOT NULL CHECK (channel IN ('email', 'web_push')),
    -- What it is about, so several of the same thing collapse into one telling.
    subject_key  text NOT NULL CHECK (btrim(subject_key) <> ''),
    count        integer NOT NULL DEFAULT 1 CHECK (count > 0),
    -- The minimum a template needs, and no more. A push carries a kind and a count; an email may
    -- add a ticker. Neither may hold a figure, which a test asserts per kind.
    detail       jsonb NOT NULL DEFAULT '{}'::jsonb,
    state        text NOT NULL DEFAULT 'pending'
                     CHECK (state IN ('pending', 'sending', 'sent', 'failed', 'abandoned')),
    -- Quiet hours and retry backoff both move this. A notification raised inside the quiet window
    -- is held until it ends: never sent early, and never dropped.
    available_at timestamptz NOT NULL DEFAULT now(),
    attempts     integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error   text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    sent_at      timestamptz,
    -- Read from both sides, so neither half of the state can drift from the other.
    CHECK ((state = 'sent') = (sent_at IS NOT NULL))
);

-- The pass reads only what is due, so a long history costs it nothing.
CREATE INDEX notifications_due_idx ON notifications (available_at) WHERE state = 'pending';
CREATE INDEX notifications_owner_idx ON notifications (user_id, created_at DESC);

COMMENT ON TABLE notifications IS
    'One telling, for one person, on one channel. At most once per channel comes from this row''s state machine rather than from the delivery pass being careful.';
