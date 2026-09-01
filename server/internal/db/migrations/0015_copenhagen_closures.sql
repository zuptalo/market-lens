-- Copenhagen was missing two annual closures.
--
-- Every instrument on the exchange was reported as missing data on each of them, which is the
-- one failure this product most needs not to make: its central claim is that a day the
-- exchange was closed is never reported as a missing session. An incomplete calendar breaks
-- that claim silently, because a holiday looks exactly like a hole in the data.
--
-- Both were found from evidence rather than from memory. On each of these dates all 25
-- Copenhagen instruments were flagged at once, and a provider does not lose 25 companies on a
-- single day.
--
--   Store Bededag, the fourth Friday after Easter (Easter + 26). Denmark abolished it as a
--   public holiday from 2024, and the data agrees: the pattern stops after 2023.
--
--   The Friday after Ascension (Easter + 40), every year.
--
-- Migration 0005 built these calendars from an Easter calculation and dropped the helper when
-- it finished, so the same function is recreated here for the length of this migration and
-- dropped again. Repeating the definition is better than leaving a function behind that
-- nothing owns. Closed sessions carry no opening times, which the table's own check requires.

CREATE FUNCTION market_lens_easter(year_number integer) RETURNS date
LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE
    a integer := year_number % 19;
    b integer := year_number / 100;
    c integer := year_number % 100;
    d integer := b / 4;
    e integer := b % 4;
    f integer := (b + 8) / 25;
    g integer := (b - f + 1) / 3;
    h integer := (19 * a + b - d - g + 15) % 30;
    i integer := c / 4;
    k integer := c % 4;
    l integer := (32 + 2 * e + 2 * i - h - k) % 7;
    m integer := (a + 11 * h + 22 * l) / 451;
    month_number integer := (h + l - 7 * m + 114) / 31;
    day_number integer := ((h + l - 7 * m + 114) % 31) + 1;
BEGIN
    RETURN make_date(year_number, month_number, day_number);
END;
$$;

UPDATE exchange_sessions s
SET status = 'closed', opens_at = NULL, closes_at = NULL
FROM exchanges e
WHERE e.id = s.exchange_id
  AND e.mic = 'XCSE'
  AND s.status <> 'closed'
  AND (
        -- Store Bededag, observed through 2023 only.
        (extract(year FROM s.session_date) <= 2023
         AND s.session_date = market_lens_easter(extract(year FROM s.session_date)::integer) + 26)
        -- The Friday after Ascension.
     OR s.session_date = market_lens_easter(extract(year FROM s.session_date)::integer) + 40
  );

DROP FUNCTION market_lens_easter(integer);
