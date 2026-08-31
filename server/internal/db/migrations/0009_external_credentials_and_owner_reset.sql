CREATE TABLE external_service_credentials (
    id uuid PRIMARY KEY,
    kind text NOT NULL UNIQUE CHECK (kind IN ('eodhd_api', 'smtp')),
    ciphertext bytea NOT NULL CHECK (octet_length(ciphertext) BETWEEN 29 AND 16412),
    payload_version smallint NOT NULL CHECK (payload_version > 0),
    key_version integer NOT NULL CHECK (key_version > 0),
    validated_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (
        (kind = 'eodhd_api' AND validated_at IS NOT NULL)
        OR (kind = 'smtp' AND validated_at IS NULL)
    ),
    CHECK (updated_at >= created_at),
    CHECK (validated_at IS NULL OR validated_at >= created_at)
);

CREATE INDEX external_service_credentials_key_version_idx
    ON external_service_credentials (key_version, kind);

INSERT INTO security_audit_events
    (occurred_at, event_type, actor_user_id, subject_user_id, outcome, metadata)
SELECT now(), 'owner.recovery_retired.v1', users.id, users.id, 'succeeded',
       '{"source":"migration_0009"}'::jsonb
FROM users
WHERE users.role = 'owner'
  AND EXISTS (SELECT 1 FROM auth_capabilities WHERE kind = 'owner_recovery');

INSERT INTO client_events
    (event_type, version, scope, entity_type, entity_id, payload, occurred_at)
SELECT 'owner.recovery_retired.v1', 1, 'owner', 'credential', users.id::text,
       '{"version":1,"status":"retired"}'::jsonb, now()
FROM users
WHERE users.role = 'owner'
  AND EXISTS (SELECT 1 FROM auth_capabilities WHERE kind = 'owner_recovery');

DELETE FROM account_email_deliveries WHERE kind = 'owner_recovery';
DELETE FROM auth_rate_events WHERE bucket_kind = 'owner_recovery';
DELETE FROM auth_capabilities WHERE kind = 'owner_recovery';

UPDATE sessions
SET revoked_reason = 'credential_changed'
WHERE revoked_reason = 'owner_recovery';

DO $$
DECLARE
    constraint_record record;
BEGIN
    FOR constraint_record IN
        SELECT conrelid::regclass AS relation_name, conname
        FROM pg_constraint
        WHERE contype = 'c'
          AND conrelid IN (
              'auth_capabilities'::regclass,
              'account_email_deliveries'::regclass,
              'auth_rate_events'::regclass,
              'sessions'::regclass
          )
          AND pg_get_constraintdef(oid) LIKE '%owner_recovery%'
    LOOP
        EXECUTE format('ALTER TABLE %s DROP CONSTRAINT %I',
            constraint_record.relation_name, constraint_record.conname);
    END LOOP;
END
$$;

ALTER TABLE auth_capabilities
    ADD CONSTRAINT auth_capabilities_setup_only_check
    CHECK (kind = 'owner_setup' AND user_id IS NULL);

ALTER TABLE account_email_deliveries
    ADD CONSTRAINT account_email_deliveries_kind_check_v2
    CHECK (kind IN ('invitation', 'member_login_code', 'security_notice'));

ALTER TABLE auth_rate_events
    ADD CONSTRAINT auth_rate_events_bucket_kind_check_v2
    CHECK (bucket_kind IN (
        'member_code_delivery', 'origin_code_request', 'origin_code_verify', 'owner_login'
    ));

ALTER TABLE sessions
    ADD CONSTRAINT sessions_revoked_reason_check_v2
    CHECK (revoked_reason IN (
        'logout', 'owner_password_reset', 'user_deactivated', 'user_requested',
        'all_devices', 'credential_changed', 'administrative'
    ));
