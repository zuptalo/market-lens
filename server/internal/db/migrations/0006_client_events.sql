CREATE TABLE client_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_type text NOT NULL CHECK (event_type ~ '^[a-z_]+\.changed\.v[1-9][0-9]*$'),
    version integer NOT NULL CHECK (version > 0),
    scope text NOT NULL CHECK (scope IN ('shared', 'private')),
    entity_type text NOT NULL CHECK (btrim(entity_type) <> ''),
    entity_id text NOT NULL CHECK (btrim(entity_id) <> ''),
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    occurred_at timestamptz NOT NULL,
    expires_at timestamptz,
    CHECK (expires_at IS NULL OR expires_at > occurred_at)
);

CREATE INDEX client_events_scope_id_idx ON client_events (scope, id);
CREATE INDEX client_events_occurred_idx ON client_events (occurred_at);

CREATE TRIGGER client_events_append_only
    BEFORE UPDATE OR DELETE ON client_events
    FOR EACH ROW EXECUTE FUNCTION prevent_append_only_update();
