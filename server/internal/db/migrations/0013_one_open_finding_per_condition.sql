-- One open finding per condition.
--
-- A finding records a condition — this session has no stored bar, this bar's volume is zero —
-- not an observation of one. Every import re-inserted a row for a condition already recorded,
-- so repeated backfills multiplied the same facts: production held 25,853 missing-session
-- findings describing 8,662 distinct conditions. The open count then measured how often
-- imports had run rather than how much was actually wrong.
--
-- The duplicates are removed rather than resolved. Marking them resolved would assert that
-- something ended, which is not what happened; they were never separate facts. What is lost is
-- "this same gap was seen again on run N", and import_runs already records every run.
--
-- The earliest row per condition is kept, because that is when the condition was first
-- observed and its created_at is the only one that means anything.

DELETE FROM data_quality_findings d
USING data_quality_findings keep
WHERE d.status = 'open'
  AND keep.status = 'open'
  AND d.instrument_id = keep.instrument_id
  AND d.rule = keep.rule
  AND d.session_date IS NOT DISTINCT FROM keep.session_date
  AND (keep.created_at, keep.id) < (d.created_at, d.id);

-- Enforced by the database rather than by remembering. NULLS NOT DISTINCT so that an
-- instrument-level finding, which has no session, is deduplicated too; without it Postgres
-- treats every NULL session as unique and the constraint would quietly not apply to them.
CREATE UNIQUE INDEX data_quality_findings_open_condition_idx
    ON data_quality_findings (instrument_id, session_date, rule)
    NULLS NOT DISTINCT
    WHERE status = 'open';
