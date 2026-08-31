DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM client_events WHERE scope = 'private') THEN
        RAISE EXCEPTION 'legacy private client events cannot be authorized without an explicit subject';
    END IF;
END
$$;

ALTER TABLE client_events
    DROP CONSTRAINT client_events_scope_check,
    DROP CONSTRAINT client_events_event_type_check,
    ADD COLUMN subject_user_id uuid REFERENCES users(id),
    ADD CONSTRAINT client_events_scope_subject_check CHECK (
        (scope IN ('shared', 'owner') AND subject_user_id IS NULL)
        OR (scope = 'user' AND subject_user_id IS NOT NULL)
    ),
    ADD CONSTRAINT client_events_event_type_check CHECK (
        event_type ~ '^[a-z_]+\.[a-z_]+\.v[1-9][0-9]*$'
    );

DROP INDEX client_events_scope_id_idx;

CREATE INDEX client_events_shared_replay_idx
    ON client_events (id) WHERE scope = 'shared';

CREATE INDEX client_events_user_replay_idx
    ON client_events (subject_user_id, id) WHERE scope = 'user';

CREATE INDEX client_events_owner_replay_idx
    ON client_events (id) WHERE scope = 'owner';
