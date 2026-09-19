-- Which versions this installation has run (feature 027).
--
-- A deployment notification has to fire once per version, not once per process start: Keel rolls
-- pods and a crash loop restarts them, so the claim being made is about the version rather than
-- about this instance having booted.
--
-- The primary key is what makes that true without coordination. Provisioning is
-- `INSERT ... ON CONFLICT DO NOTHING`, and whichever pod inserts the row is the one that announces;
-- the others find it already there and say nothing. The same shape as the signing key and the push
-- key, for the same reason: no advisory lock, no ordering assumption.
CREATE TABLE deployed_versions (
    version       text PRIMARY KEY CHECK (btrim(version) <> ''),
    -- What changed, as the release said it. One line, because that is what a release commit is, and
    -- because a person reading it on a phone is reading a notification rather than a changelog.
    summary       text NOT NULL DEFAULT '',
    first_seen_at timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE deployed_versions IS
    'Every version this installation has started. Written once per version by whichever process sees it first, which is what makes "tell me when it changed" fire once rather than once per pod.';

-- And a fifth kind of thing worth being told about.
--
-- The kind lives in a check constraint rather than in code alone, so a kind the product does not
-- send cannot be stored — which is why adding one is a migration. Both tables are widened together:
-- a preference for a kind that no notification could carry would be a switch with nothing behind it.
ALTER TABLE notification_preferences
    DROP CONSTRAINT notification_preferences_kind_check,
    ADD CONSTRAINT notification_preferences_kind_check CHECK (kind IN
        ('decision_waiting', 'paper_fill', 'pipeline_failure', 'signal_change', 'release_deployed'));

ALTER TABLE notifications
    DROP CONSTRAINT notifications_kind_check,
    ADD CONSTRAINT notifications_kind_check CHECK (kind IN
        ('decision_waiting', 'paper_fill', 'pipeline_failure', 'signal_change', 'release_deployed'));
