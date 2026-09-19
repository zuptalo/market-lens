-- The address a session was last seen from (feature 028).
--
-- Feature 004 stored origins as keyed one-way digests and wrote down that they are never displayed
-- raw. That decision is narrowed here, not reversed: the audit log and the login-failure records
-- keep their digests, and so does this table. What is added is a readable address on the session
-- row alone, shown to the one person entitled to see it — the account the session belongs to.
--
-- inet rather than text. The type validates and canonicalises, so `203.0.113.12` has exactly one
-- stored form and one device cannot appear as two; an address that is not an address cannot be
-- written at all, which is a constraint worth more than one written by hand.
--
-- Nullable, and no backfill. Every session that exists when this migration runs was created by a
-- request that is long over: there is no address to write and none may be invented. A null means
-- "not recorded", and the next authenticated request on that session records one.
ALTER TABLE sessions
    ADD COLUMN created_ip inet,
    ADD COLUMN last_seen_ip inet;

COMMENT ON COLUMN sessions.created_ip IS
    'Client address of the request that created this session; null when it was not recorded.';
COMMENT ON COLUMN sessions.last_seen_ip IS
    'Client address of the most recent request that authenticated this session; null when it was not recorded.';
