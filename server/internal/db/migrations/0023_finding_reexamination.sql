-- Feature 017: what the product learns by asking the source twice.
--
-- A finding is resolved when an import covers its session and re-validates it without raising
-- that rule again. For a condition the source keeps reporting — an absent session, most often —
-- that never fires, so the finding stayed open for ever. Once the scheduled pass began reaching
-- back to re-examine open findings, "for ever" became a nightly loop: the same session requested,
-- the same row rejected, the same run partial, and a status that reported trouble every night and
-- therefore reported nothing.
--
-- What was missing is a way to record "I asked again, and the answer did not change."
--
-- Deliberately not a fourth status. `status = 'open'` is the predicate the resolution rule itself
-- uses, so moving a re-examined finding out of it would stop the finding ever resolving — it
-- would satisfy "stop reaching back" by making every re-examined finding permanent, which is the
-- opposite of the point. The finding is still open. It has just been asked twice.

ALTER TABLE data_quality_findings
    ADD COLUMN reexamined_at timestamptz,
    ADD COLUMN reexamining_run_id uuid REFERENCES import_runs(id),
    ADD COLUMN accepted_at timestamptz,
    ADD COLUMN accepted_by uuid REFERENCES users(id);

-- An attribution nobody can read is not an attribution: a time with no run, or a name with no
-- time, would each record half of a claim.
ALTER TABLE data_quality_findings
    ADD CONSTRAINT finding_reexamination_is_attributed
        CHECK ((reexamined_at IS NULL) = (reexamining_run_id IS NULL)),
    ADD CONSTRAINT finding_acceptance_is_attributed
        CHECK ((accepted_at IS NULL) = (accepted_by IS NULL));

-- Nothing may be accepted before re-observation has had its say. Asking a person to judge a
-- condition nothing has checked twice is asking them to do the product's work.
ALTER TABLE data_quality_findings
    ADD CONSTRAINT finding_accepted_only_after_reexamination
        CHECK (accepted_at IS NULL OR reexamined_at IS NOT NULL);

-- The status and the acceptance cannot disagree about whether a person decided.
ALTER TABLE data_quality_findings
    ADD CONSTRAINT finding_acceptance_matches_status
        CHECK ((accepted_at IS NOT NULL) = (status = 'accepted_limitation'));

-- The reach-back reads this: the oldest open finding a re-observation has not already examined.
CREATE INDEX data_quality_findings_unexamined_idx
    ON data_quality_findings (instrument_id, session_date)
    WHERE status = 'open' AND reexamined_at IS NULL AND session_date IS NOT NULL;

COMMENT ON COLUMN data_quality_findings.reexamined_at IS
    'When an import last covered this session and raised the same rule again. A finding with this set is awaiting a person''s decision rather than another run: an identical request cannot tell us anything the last one did not.';
