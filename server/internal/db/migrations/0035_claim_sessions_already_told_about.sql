-- Sessions already told about under the per-instrument scheme (feature 029).
--
-- 0034 made a telling about a session and made the pass claim the session before raising one. An
-- installation upgrading into that has a session which already produced one telling per instrument
-- and no claim to show for it — so the first pass after the upgrade would claim that session and
-- say the whole thing once more. A twelfth message about a day somebody already heard about eleven
-- times is exactly the complaint this feature exists to answer.
--
-- The claim is derived from what was actually sent: old-style tellings are keyed by ticker, new
-- ones by session, so a subject key that is not a session key is evidence of the old scheme. The
-- session they were about is the latest one signals were computed for, because that is the only
-- session the pass ever raised for.
--
-- On a clean installation this inserts nothing: no old tellings, no claim, and the first real pass
-- speaks normally about a day that did change.
INSERT INTO signal_change_sessions (session_date, changes)
SELECT (SELECT max(session_date) FROM signals),
       count(DISTINCT n.subject_key)
FROM notifications n
WHERE n.kind = 'signal_change'
  AND n.subject_key NOT LIKE 'signals:%'
  AND EXISTS (SELECT 1 FROM signals)
HAVING count(DISTINCT n.subject_key) > 0
ON CONFLICT (session_date) DO NOTHING;
