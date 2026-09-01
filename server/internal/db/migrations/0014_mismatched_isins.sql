-- Correct two seeded ISINs that did not identify the listing they are imported from.
--
-- A mismatch is quieter than a stale ticker and worse for it. The symbol resolves, prices
-- import, and the row looks entirely healthy while carrying an identifier that belongs to a
-- different listing of the same company. Nothing breaks today; anything that later joins on
-- ISIN — a benchmark series, a second provider, a corporate-action feed — would attach its
-- data to the wrong row, and the error would be very hard to see.
--
-- The replacements are what `marketdata resolve` reported for each stored symbol once the
-- audit printed the provider's identifier beside our own. They are not inferred, which matters
-- here as much as it did for the stale tickers: the obvious guess was wrong that time.
--
--   BWLPG.OL   stored SGXZ69436764 (a Singapore identifier)  ->  BMG173841013
--   ROCK-B.CO  stored DK0010219153 (superseded)              ->  DK0063855168
--
-- BW LPG is Bermuda-incorporated and listed in Oslo, so a Bermuda identifier is the one its
-- Oslo line carries. ROCKWOOL's Copenhagen B share carries a newer identifier than the one the
-- universe was seeded with.
--
-- Matched on the provider symbol, because that is the thing known to be correct: both
-- instruments import their full history under it. Only the identifier was wrong.

UPDATE instruments i
SET isin = 'BMG173841013'
FROM provider_instruments p
WHERE p.instrument_id = i.id
  AND p.provider = 'eodhd'
  AND p.provider_symbol = 'BWLPG.OL'
  AND i.isin = 'SGXZ69436764';

UPDATE instruments i
SET isin = 'DK0063855168'
FROM provider_instruments p
WHERE p.instrument_id = i.id
  AND p.provider = 'eodhd'
  AND p.provider_symbol = 'ROCK-B.CO'
  AND i.isin = 'DK0010219153';
