-- Widen the due index to match the query that reads it (feature 027).
--
-- The index was written for `state = 'pending'` while the delivery pass reads
-- `state IN ('pending', 'failed')`, so a failed notification waiting for its retry was never found
-- by it and every pass fell back to scanning the table.
--
-- That cost nothing while delivery ran once a night. It now runs every minute — because quiet hours
-- end and retry backoffs expire on their own schedule, neither of which has anything to do with
-- market data — so the same query runs 1440 times a day against a table that only grows.
DROP INDEX notifications_due_idx;

CREATE INDEX notifications_due_idx ON notifications (available_at)
    WHERE state IN ('pending', 'failed');

COMMENT ON INDEX notifications_due_idx IS
    'Serves the delivery pass, which asks for pending and failed together: a failed notification is one waiting for its next attempt, not one that has stopped.';
