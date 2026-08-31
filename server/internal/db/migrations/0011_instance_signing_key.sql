-- The instance signing key becomes self-provisioned and database-resident, so that a
-- deployment needs only DATABASE_URL and a database backup is a complete, restorable
-- installation. Restoring without the old AUTH_SECRET used to sign everybody out with no
-- error; keeping the key with the data removes that failure entirely.
--
-- EXTERNAL_CREDENTIAL_KEY deliberately does NOT move here. See the table comment below.

CREATE TABLE instance_signing_key (
    id            uuid PRIMARY KEY,
    source        text NOT NULL CHECK (source IN ('provisioned', 'supplied')),
    key_material  bytea CHECK (key_material IS NULL OR octet_length(key_material) BETWEEN 32 AND 128),
    fingerprint   bytea NOT NULL CHECK (octet_length(fingerprint) = 32),
    generation    integer NOT NULL CHECK (generation > 0),
    created_at    timestamptz NOT NULL,
    rotated_at    timestamptz,
    -- A supplied AUTH_SECRET is never persisted; only its fingerprint is, which is what
    -- lets a changed or removed value be reported instead of silently re-keying.
    CHECK ((source = 'provisioned') = (key_material IS NOT NULL)),
    CHECK (rotated_at IS NULL OR rotated_at >= created_at)
);

-- Exactly one key, forever. This is what makes simultaneous first starts converge on one
-- key without an advisory lock: provisioning is INSERT ... ON CONFLICT DO NOTHING followed
-- by an unconditional SELECT, so the loser of the race adopts the winner's key.
CREATE UNIQUE INDEX instance_signing_key_singleton ON instance_signing_key ((true));

COMMENT ON TABLE instance_signing_key IS
    'The signing key protects rows in this same database - session, CSRF, capability and '
    'login-code digests - and a session is only usable against the live database, so a '
    'stolen backup containing both gains nothing and the key may live here. '
    'EXTERNAL_CREDENTIAL_KEY is different: it encrypts the EODHD key and SMTP password '
    'INSIDE this database, so storing it here would put the lock and its key in one file '
    'and turn a leaked backup into working credentials. It must never be stored in '
    'PostgreSQL. See specs/009-self-provisioned-keys/spec.md.';

-- Rotation ends every session, and an operator reading the audit trail afterwards must be
-- able to tell why. Reusing 'administrative' would have hidden the cause.
ALTER TABLE sessions
    DROP CONSTRAINT sessions_revoked_reason_check_v2;

ALTER TABLE sessions
    ADD CONSTRAINT sessions_revoked_reason_check_v3
    CHECK (revoked_reason IN (
        'logout', 'owner_password_reset', 'user_deactivated', 'user_requested',
        'all_devices', 'credential_changed', 'administrative', 'signing_key_rotated'
    ));
