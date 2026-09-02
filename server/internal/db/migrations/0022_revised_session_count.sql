-- Feature 016: how many stored sessions a run corrected.
--
-- A run that stores a session for the first time and a run that replaces one it stored earlier
-- have looked identical since feature 002. Only the second means every value derived from that
-- session changed underneath it, which is precisely the case somebody should be able to notice.
--
-- Existing runs default to zero, and that is the truthful answer rather than an unknown dressed
-- as one: they really did correct nothing, because until now the scheduled pass asked the source
-- only about the session that had just closed and never gave it a chance to change its mind.

ALTER TABLE import_runs
    ADD COLUMN revised_count bigint NOT NULL DEFAULT 0 CHECK (revised_count >= 0);

-- A run cannot correct more sessions than it looked at. Same shape as the two constraints this
-- table already carries over its other counts.
ALTER TABLE import_runs
    ADD CONSTRAINT import_runs_revised_within_processed CHECK (revised_count <= processed_count);

ALTER TABLE import_items
    ADD COLUMN revised_count bigint NOT NULL DEFAULT 0 CHECK (revised_count >= 0);

ALTER TABLE import_items
    ADD CONSTRAINT import_items_revised_within_processed CHECK (revised_count <= processed_count);

COMMENT ON COLUMN import_runs.revised_count IS
    'Stored sessions whose source values had changed since they were first observed, and which this run replaced. Distinct from accepted_count, which counts everything stored.';
