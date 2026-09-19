-- Which sessions of signal changes have been told about (feature 029).
--
-- A session's changes produce one telling per person. "Has this session been told about?" cannot
-- be answered by looking for those notifications: on a day when nobody had the kind switched on
-- there are none, and the first person to switch it on afterwards would then be sent a telling
-- about a session that closed before they asked. Consent is not retroactive, and idempotency must
-- not quietly turn into a backlog.
--
-- So the pass records the session itself, once, whether or not anybody was told. The primary key
-- is what makes a second pass say nothing without coordination: the insert is
-- `ON CONFLICT DO NOTHING` inside the raising transaction, and whichever pass inserts the row is
-- the one that raises. The same shape as deployed_versions, for the same reason.
CREATE TABLE signal_change_sessions (
    session_date date PRIMARY KEY,
    told_at      timestamptz NOT NULL DEFAULT now(),
    -- How many instruments changed view on it. Not read by anything: it is here so an operator
    -- asking why a quiet night was quiet gets an answer from the row rather than a guess.
    changes      integer NOT NULL CHECK (changes > 0)
);

COMMENT ON TABLE signal_change_sessions IS
    'One row per signal session already told about, so a repeated pass says nothing twice.';
