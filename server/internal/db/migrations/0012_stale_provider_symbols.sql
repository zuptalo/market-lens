-- Correct two provider symbols that went stale after the universe was seeded.
--
-- Both companies were renamed and their tickers changed with them, so the symbols the
-- importer asks for no longer exist. Neither instrument imported a single bar, and the
-- provider reported only that it had no data for them — which reads as a provider problem
-- rather than as an identifier of ours that went out of date.
--
-- The replacements are not inferred. They are what `marketdata resolve` reported after
-- matching each stored ISIN against the provider's own catalog. That distinction earned its
-- keep: the obvious guess for Sydbank was SYDB.CO, and the provider actually lists ALSYDB.CO.
--
-- Matched on ISIN rather than on the stale symbol, because the ISIN is the identifier that
-- did not change and is the reason the replacement could be found at all.

UPDATE provider_instruments p
SET provider_symbol = 'ALSYDB.CO'
FROM instruments i
WHERE i.id = p.instrument_id
  AND i.isin = 'DK0010311471'
  AND p.provider = 'eodhd'
  AND p.provider_symbol = 'AL.CO';

UPDATE provider_instruments p
SET provider_symbol = 'METSO.HE'
FROM instruments i
WHERE i.id = p.instrument_id
  AND i.isin = 'FI0009014575'
  AND p.provider = 'eodhd'
  AND p.provider_symbol = 'MOCORP.HE';
