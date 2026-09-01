-- Feature 013: reserved for the index the Markets listing needs once it reads its three
-- adopted statistics from feature_values instead of deriving them.
--
-- Kept as its own version so the adoption is revertible independently of the engine's
-- schema. It is filled only if measuring the adopted listing query against its two-second
-- budget shows that (definition_id, session_date) is not enough; until then it changes
-- nothing, and that is deliberate.
SELECT 1;
