CREATE TABLE users (
    id uuid PRIMARY KEY,
    email text NOT NULL CHECK (
        char_length(email) BETWEEN 3 AND 320
        AND email = btrim(email)
        AND email !~ '[[:cntrl:]]'
    ),
    normalized_email text NOT NULL UNIQUE CHECK (
        char_length(normalized_email) BETWEEN 3 AND 320
        AND normalized_email = btrim(normalized_email)
        AND normalized_email = lower(normalized_email)
        AND normalized_email !~ '[[:cntrl:]]'
    ),
    display_name text NOT NULL CHECK (
        char_length(display_name) BETWEEN 1 AND 120
        AND display_name = btrim(display_name)
        AND display_name !~ '[[:cntrl:]]'
    ),
    role text NOT NULL CHECK (role IN ('owner', 'member')),
    status text NOT NULL CHECK (status IN ('active', 'deactivated')),
    email_verified_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    deactivated_at timestamptz,
    CHECK (updated_at >= created_at),
    CHECK (
        (status = 'active' AND email_verified_at IS NOT NULL AND deactivated_at IS NULL)
        OR (status = 'deactivated' AND deactivated_at IS NOT NULL)
    ),
    CHECK (role <> 'owner' OR status = 'active')
);

CREATE UNIQUE INDEX users_one_owner_idx ON users (role) WHERE role = 'owner';
CREATE INDEX users_role_status_idx ON users (role, status);

CREATE FUNCTION prevent_owner_lifecycle_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' AND OLD.role = 'owner' THEN
        RAISE EXCEPTION 'owner lifecycle changes require a future ownership-transfer migration';
    END IF;
    IF TG_OP = 'UPDATE' AND OLD.role = 'owner'
       AND (NEW.role <> OLD.role OR NEW.status <> OLD.status) THEN
        RAISE EXCEPTION 'owner lifecycle changes require a future ownership-transfer migration';
    END IF;
    RETURN CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
END;
$$;

CREATE TRIGGER users_owner_lifecycle_immutable
    BEFORE UPDATE OR DELETE ON users
    FOR EACH ROW EXECUTE FUNCTION prevent_owner_lifecycle_change();

CREATE TABLE bootstrap_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    closed_at timestamptz,
    owner_user_id uuid UNIQUE REFERENCES users(id),
    CHECK ((closed_at IS NULL) = (owner_user_id IS NULL))
);

INSERT INTO bootstrap_state (singleton) VALUES (true);

CREATE TABLE auth_capabilities (
    id uuid PRIMARY KEY,
    kind text NOT NULL CHECK (kind IN ('owner_setup', 'owner_recovery')),
    user_id uuid REFERENCES users(id),
    token_digest bytea NOT NULL UNIQUE CHECK (octet_length(token_digest) = 32),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL,
    CHECK (
        (kind = 'owner_setup' AND user_id IS NULL)
        OR (kind = 'owner_recovery' AND user_id IS NOT NULL)
    ),
    CHECK (expires_at > created_at),
    CHECK (consumed_at IS NULL OR consumed_at >= created_at),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at),
    CHECK (consumed_at IS NULL OR revoked_at IS NULL)
);

CREATE UNIQUE INDEX auth_capabilities_one_usable_idx
    ON auth_capabilities (kind, coalesce(user_id, '00000000-0000-0000-0000-000000000000'::uuid))
    WHERE consumed_at IS NULL AND revoked_at IS NULL;
CREATE INDEX auth_capabilities_expiry_idx ON auth_capabilities (expires_at)
    WHERE consumed_at IS NULL AND revoked_at IS NULL;

CREATE TABLE owner_credentials (
    user_id uuid PRIMARY KEY REFERENCES users(id),
    password_hash text NOT NULL CHECK (
        char_length(password_hash) BETWEEN 32 AND 512
        AND password_hash LIKE '$argon2id$%'
    ),
    changed_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    CHECK (changed_at >= created_at)
);

CREATE TABLE sessions (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users(id),
    token_digest bytea NOT NULL UNIQUE CHECK (octet_length(token_digest) = 32),
    csrf_digest bytea NOT NULL CHECK (octet_length(csrf_digest) = 32),
    created_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    idle_expires_at timestamptz NOT NULL,
    absolute_expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    revoked_reason text CHECK (revoked_reason IN (
        'logout', 'owner_recovery', 'user_deactivated', 'user_requested',
        'all_devices', 'credential_changed', 'administrative'
    )),
    device_label text NOT NULL CHECK (
        char_length(device_label) BETWEEN 1 AND 160
        AND device_label = btrim(device_label)
        AND device_label !~ '[[:cntrl:]]'
    ),
    origin_digest bytea NOT NULL CHECK (octet_length(origin_digest) = 32),
    CHECK (last_seen_at >= created_at),
    CHECK (idle_expires_at > last_seen_at),
    CHECK (absolute_expires_at > created_at),
    CHECK (idle_expires_at <= absolute_expires_at),
    CHECK ((revoked_at IS NULL) = (revoked_reason IS NULL)),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);

CREATE INDEX sessions_user_activity_idx ON sessions (user_id, revoked_at, absolute_expires_at);
CREATE INDEX sessions_expiry_idx ON sessions (idle_expires_at, absolute_expires_at)
    WHERE revoked_at IS NULL;

CREATE TABLE invitations (
    id uuid PRIMARY KEY,
    email text NOT NULL CHECK (
        char_length(email) BETWEEN 3 AND 320
        AND email = btrim(email)
        AND email !~ '[[:cntrl:]]'
    ),
    normalized_email text NOT NULL CHECK (
        char_length(normalized_email) BETWEEN 3 AND 320
        AND normalized_email = btrim(normalized_email)
        AND normalized_email = lower(normalized_email)
        AND normalized_email !~ '[[:cntrl:]]'
    ),
    token_digest bytea NOT NULL UNIQUE CHECK (octet_length(token_digest) = 32),
    state text NOT NULL CHECK (state IN ('pending', 'accepted', 'revoked', 'expired')),
    expires_at timestamptz NOT NULL,
    accepted_by_user_id uuid REFERENCES users(id),
    accepted_at timestamptz,
    revoked_at timestamptz,
    created_by_user_id uuid NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    resend_count integer NOT NULL DEFAULT 0 CHECK (resend_count BETWEEN 0 AND 100),
    CHECK (expires_at > created_at),
    CHECK (updated_at >= created_at),
    CHECK ((state = 'accepted') = (accepted_by_user_id IS NOT NULL AND accepted_at IS NOT NULL)),
    CHECK ((state = 'revoked') = (revoked_at IS NOT NULL)),
    CHECK (accepted_at IS NULL OR accepted_at >= created_at),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);

CREATE UNIQUE INDEX invitations_one_pending_email_idx ON invitations (normalized_email)
    WHERE state = 'pending';
CREATE INDEX invitations_owner_created_idx ON invitations (created_by_user_id, created_at DESC);
CREATE INDEX invitations_expiry_idx ON invitations (expires_at) WHERE state = 'pending';

CREATE TABLE member_login_state (
    user_id uuid PRIMARY KEY REFERENCES users(id),
    consecutive_failures smallint NOT NULL DEFAULT 0 CHECK (consecutive_failures BETWEEN 0 AND 3),
    blocked_until timestamptz,
    administratively_locked_at timestamptz,
    locked_reason text CHECK (locked_reason = 'wrong_code_limit'),
    last_code_sent_at timestamptz,
    updated_at timestamptz NOT NULL,
    CHECK ((administratively_locked_at IS NULL) = (locked_reason IS NULL))
);

CREATE TABLE member_login_challenges (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users(id),
    code_digest bytea NOT NULL UNIQUE CHECK (octet_length(code_digest) = 32),
    state text NOT NULL CHECK (state IN ('active', 'used', 'superseded', 'expired', 'revoked')),
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    invalidated_at timestamptz,
    created_at timestamptz NOT NULL,
    delivery_id uuid,
    CHECK (expires_at > created_at),
    CHECK ((state = 'used') = (used_at IS NOT NULL)),
    CHECK ((state IN ('superseded', 'revoked')) = (invalidated_at IS NOT NULL)),
    CHECK (used_at IS NULL OR used_at >= created_at),
    CHECK (invalidated_at IS NULL OR invalidated_at >= created_at)
);

CREATE UNIQUE INDEX member_login_challenges_one_active_idx
    ON member_login_challenges (user_id) WHERE state = 'active';
CREATE INDEX member_login_challenges_expiry_idx
    ON member_login_challenges (expires_at) WHERE state = 'active';

CREATE TABLE account_email_deliveries (
    id uuid PRIMARY KEY,
    kind text NOT NULL CHECK (kind IN ('invitation', 'member_login_code', 'owner_recovery', 'security_notice')),
    recipient_email text NOT NULL CHECK (
        char_length(recipient_email) BETWEEN 3 AND 320
        AND recipient_email = btrim(recipient_email)
        AND recipient_email !~ '[[:cntrl:]]'
    ),
    subject_user_id uuid REFERENCES users(id),
    invitation_id uuid REFERENCES invitations(id),
    challenge_id uuid REFERENCES member_login_challenges(id),
    state text NOT NULL CHECK (state IN ('pending', 'sending', 'sent', 'failed', 'abandoned')),
    attempt_count smallint NOT NULL DEFAULT 0 CHECK (attempt_count BETWEEN 0 AND 20),
    last_attempt_at timestamptz,
    sent_at timestamptz,
    error_code text CHECK (error_code IN ('temporary_failure', 'permanent_failure', 'abandoned')),
    created_at timestamptz NOT NULL,
    CHECK ((state = 'sent') = (sent_at IS NOT NULL)),
    CHECK ((state IN ('failed', 'abandoned')) = (error_code IS NOT NULL)),
    CHECK (last_attempt_at IS NULL OR last_attempt_at >= created_at),
    CHECK (sent_at IS NULL OR sent_at >= created_at)
);

ALTER TABLE member_login_challenges
    ADD CONSTRAINT member_login_challenges_delivery_fk
    FOREIGN KEY (delivery_id) REFERENCES account_email_deliveries(id);

CREATE INDEX account_email_deliveries_state_idx
    ON account_email_deliveries (state, created_at) WHERE state IN ('pending', 'sending', 'failed');
CREATE INDEX account_email_deliveries_subject_idx
    ON account_email_deliveries (subject_user_id, created_at DESC);

CREATE TABLE login_failure_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users(id),
    challenge_id uuid NOT NULL REFERENCES member_login_challenges(id),
    occurred_at timestamptz NOT NULL,
    origin_digest bytea NOT NULL CHECK (octet_length(origin_digest) = 32)
);

CREATE INDEX login_failure_events_user_time_idx
    ON login_failure_events (user_id, occurred_at DESC);

CREATE TABLE auth_rate_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    bucket_kind text NOT NULL CHECK (bucket_kind IN (
        'member_code_delivery', 'origin_code_request', 'origin_code_verify',
        'owner_login', 'owner_recovery'
    )),
    bucket_digest bytea NOT NULL CHECK (octet_length(bucket_digest) = 32),
    occurred_at timestamptz NOT NULL
);

CREATE INDEX auth_rate_events_bucket_time_idx
    ON auth_rate_events (bucket_kind, bucket_digest, occurred_at DESC);
CREATE INDEX auth_rate_events_cleanup_idx ON auth_rate_events (occurred_at);

CREATE TABLE security_audit_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    occurred_at timestamptz NOT NULL,
    event_type text NOT NULL CHECK (event_type ~ '^[a-z_]+\.[a-z_]+\.v[1-9][0-9]*$'),
    actor_user_id uuid REFERENCES users(id),
    subject_user_id uuid REFERENCES users(id),
    session_id uuid REFERENCES sessions(id),
    outcome text NOT NULL CHECK (outcome IN ('succeeded', 'failed', 'blocked', 'locked')),
    origin_digest bytea CHECK (origin_digest IS NULL OR octet_length(origin_digest) = 32),
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX security_audit_events_occurred_idx ON security_audit_events (occurred_at DESC);
CREATE INDEX security_audit_events_subject_idx ON security_audit_events (subject_user_id, occurred_at DESC);

CREATE TRIGGER security_audit_events_append_only
    BEFORE UPDATE OR DELETE ON security_audit_events
    FOR EACH ROW EXECUTE FUNCTION prevent_append_only_update();
