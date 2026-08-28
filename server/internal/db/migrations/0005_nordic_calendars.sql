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

WITH weekdays AS (
    SELECT e.id AS exchange_id, e.mic, e.timezone, day::date AS session_date,
           market_lens_easter(extract(year FROM day)::integer) AS easter
    FROM exchanges e
    CROSS JOIN generate_series(DATE '2016-08-29', DATE '2027-12-31', INTERVAL '1 day') day
    WHERE e.mic IN ('XSTO','XCSE','XHEL','XOSL') AND extract(isodow FROM day) BETWEEN 1 AND 5
), classified AS (
    SELECT *,
        CASE
            WHEN mic = 'XSTO' AND (
                to_char(session_date,'MM-DD') IN ('01-01','01-06','05-01','06-06','12-24','12-25','12-26','12-31') OR
                session_date IN (easter - 2, easter + 1, easter + 39) OR
                (extract(month FROM session_date)=6 AND extract(day FROM session_date) BETWEEN 19 AND 25 AND extract(isodow FROM session_date)=5)
            ) THEN 'closed'
            WHEN mic = 'XCSE' AND (
                to_char(session_date,'MM-DD') IN ('01-01','06-05','12-24','12-25','12-26','12-31') OR
                session_date IN (easter - 3, easter - 2, easter + 1, easter + 39, easter + 50)
            ) THEN 'closed'
            WHEN mic = 'XHEL' AND (
                to_char(session_date,'MM-DD') IN ('01-01','01-06','05-01','12-06','12-24','12-25','12-26','12-31') OR
                session_date IN (easter - 2, easter + 1, easter + 39) OR
                (extract(month FROM session_date)=6 AND extract(day FROM session_date) BETWEEN 19 AND 25 AND extract(isodow FROM session_date)=5)
            ) THEN 'closed'
            WHEN mic = 'XOSL' AND (
                to_char(session_date,'MM-DD') IN ('01-01','05-01','05-17','12-24','12-25','12-26','12-31') OR
                session_date IN (easter - 3, easter - 2, easter + 1, easter + 39, easter + 50)
            ) THEN 'closed'
            WHEN mic = 'XSTO' AND to_char(session_date,'MM-DD') IN ('01-05','04-30') THEN 'half_day'
            ELSE 'open'
        END AS status
    FROM weekdays
)
INSERT INTO exchange_sessions (exchange_id,session_date,status,opens_at,closes_at,source_reference)
SELECT exchange_id, session_date, status,
    CASE WHEN status='closed' THEN NULL ELSE (session_date::timestamp + time '09:00') AT TIME ZONE timezone END,
    CASE
        WHEN status='closed' THEN NULL
        WHEN status='half_day' THEN (session_date::timestamp + time '13:00') AT TIME ZONE timezone
        WHEN mic='XSTO' THEN (session_date::timestamp + time '17:30') AT TIME ZONE timezone
        WHEN mic='XCSE' THEN (session_date::timestamp + time '17:00') AT TIME ZONE timezone
        WHEN mic='XHEL' THEN (session_date::timestamp + time '18:30') AT TIME ZONE timezone
        ELSE (session_date::timestamp + time '16:20') AT TIME ZONE timezone
    END,
    CASE WHEN mic='XOSL'
        THEN 'https://live.euronext.com/en/resources/trading-hours-holidays'
        ELSE 'https://www.nasdaq.com/market-activity/stock-market-holiday-schedule'
    END
FROM classified;

DROP FUNCTION market_lens_easter(integer);
