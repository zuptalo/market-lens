# Quickstart: Rolling Re-observation of Recent Sessions

What changed, how to see it working, and how to tell whether five sessions is the right number.

---

## What changed

The scheduled daily pass used to ask the source about exactly one session — the one that just
closed. It now asks about the last five trading sessions, so a close the source restates a day or
two later is noticed on the next run and corrected through the path that already existed.

Nothing else about the pass changed. Same run kind, same schedule, same number of provider
requests: a range is asked for once per instrument regardless of how wide it is.

---

## Configure the window

```sh
MARKET_DATA_REOBSERVE_SESSIONS=5    # default; trading sessions, not calendar days
```

Bounded 1..60 and refused at startup outside that range rather than clamped, so a value set in
the belief that it covers a quarter cannot silently cover three months instead. One session is
the behaviour before this feature. Past about sixty, an operator wanting history should run a
backfill and decide its scope deliberately.

The unit is trading sessions on the instrument's own exchange. The four Nordic exchanges keep
different holiday calendars, so five sessions is a different date on each — which is the point.

---

## See it work

```sh
# Watch a routine pass and what it did
make cli ARGS="marketdata update --universe nordic-liquid-v1"
```

```sql
-- Did any run correct history rather than only extend it?
SELECT started_at::date, kind, processed_count, accepted_count, revised_count
FROM import_runs ORDER BY started_at DESC LIMIT 14;

-- Which instrument and session, for a run that corrected something
SELECT i.ticker, r.session_date, r.revision, r.close AS superseded_close, b.close AS current_close
FROM price_bar_revisions r
JOIN instruments i ON i.id = r.instrument_id
JOIN daily_price_bars b ON b.instrument_id = r.instrument_id AND b.session_date = r.session_date
WHERE r.superseding_run_id = '<run id>'
ORDER BY i.ticker, r.session_date;
```

The operations screen states the corrected count on each run, beside the counts it already shows.

---

## Verify it

**A quiet night is genuinely quiet (SC-003)** — the property the whole design rests on.

```sql
-- A run that corrected nothing must have triggered no recomputation.
SELECT r.id, r.revised_count,
       (SELECT count(*) FROM feature_runs f WHERE f.trigger_run_id = r.id) AS feature_runs
FROM import_runs r
WHERE r.kind = 'daily_update' AND r.revised_count = 0
ORDER BY r.started_at DESC LIMIT 7;
-- expect feature_runs = 1 for the run that stored the new session, and no recomputation
-- attributable to the four re-observed sessions that had not changed
```

**A correction reaches all the way through (SC-004)**

```sql
-- Every session this run corrected has features recomputed by the run it triggered
SELECT count(*) FROM price_bar_revisions r
WHERE r.superseding_run_id = '<run id>'
  AND NOT EXISTS (
    SELECT 1 FROM feature_values v
    WHERE v.instrument_id = r.instrument_id AND v.session_date = r.session_date
      AND v.run_id IN (SELECT id FROM feature_runs WHERE trigger_run_id = '<run id>'));
-- expect 0
```

**The counts stay honest (FR-010)**

```sql
SELECT count(*) FROM import_runs WHERE revised_count > processed_count;
-- expect 0 — and the table's check constraint means it can only ever be 0
```

---

## Recorded evidence

`v0.13.0`, deployed 2026-09-02 23:21 UTC.

**The migration, on the live database.** Schema at version 22, with `revised_count` present on
both tables and all 12 pre-existing runs reading `0` rather than null — the upgrade behaved as
the migration test predicted, with no manual step.

```
 schema
--------
     22

 runs_with_column
------------------
               12
```

**The window in effect.** `MARKET_DATA_REOBSERVE_SESSIONS` is not set in the deployment, so the
built-in default of five sessions applies. Nothing needs configuring for the feature to work; the
setting exists to change it later on evidence.

**What is not yet verified in production, and why.** The widened window belongs to the *scheduled*
pass, which runs at 20:00 Europe/Stockholm. This shipped at 01:21 the same night, so no scheduled
pass has run under it yet. The first one will produce a `daily_update` run whose targets span five
sessions rather than one, and a `revised_count` — almost certainly zero, since a restatement is a
rare event and the point of the window is to catch one whenever it happens rather than to find one
on the first night.

The behaviour itself is proven against a real store and a source that answers only for the range
it was asked about, in `server/internal/scheduler/marketdata_integration_test.go`: a close restated
three sessions back is corrected, the previous values are archived, the recomputation scope of a
quiet run is empty, and a five-session window makes exactly as many source requests as a
one-session window.

To confirm the first scheduled pass:

```sql
SELECT started_at, kind, processed_count, accepted_count, revised_count
FROM import_runs WHERE kind = 'daily_update' ORDER BY started_at DESC LIMIT 3;
-- processed_count should be about five times what it was before this shipped

SELECT requested_from, requested_to FROM import_items
WHERE run_id = (SELECT id FROM import_runs ORDER BY started_at DESC LIMIT 1) LIMIT 5;
-- requested_from should be four trading sessions before requested_to
```

---

## Is five the right number?

This is the question the feature makes answerable, and it was a guess when it shipped. After a
few weeks:

```sql
-- How often does this source restate anything at all?
SELECT count(*) FILTER (WHERE revised_count > 0) AS runs_that_corrected,
       count(*) AS runs,
       sum(revised_count) AS sessions_corrected
FROM import_runs WHERE kind = 'daily_update';

-- How far back did the corrections reach? If they cluster at the window's edge,
-- the window is too narrow and corrections are being missed beyond it.
SELECT r.superseding_run_id, i.ticker, r.session_date,
       (SELECT count(*) FROM exchange_sessions s
        JOIN instruments ii ON ii.id = r.instrument_id
        WHERE s.exchange_id = ii.exchange_id AND s.status IN ('open','half_day')
          AND s.session_date > r.session_date
          AND s.session_date <= (SELECT max(session_date) FROM daily_price_bars)) AS sessions_back
FROM price_bar_revisions r
JOIN instruments i ON i.id = r.instrument_id
ORDER BY r.superseded_at DESC LIMIT 50;
```

If `sessions_back` never approaches five, the window is comfortable. If corrections pile up at
four and five, raise `MARKET_DATA_REOBSERVE_SESSIONS` — it costs no additional provider requests.

---

## What this does not do

- **It does not catch a restatement older than the window.** That remains an explicit backfill,
  and it is a stated limit rather than an oversight: covering it automatically would mean
  re-asking for a decade every night to catch something that, past a few days, effectively never
  happens.
- **It does not widen after an outage.** A pass that runs after a week of downtime still looks
  back five sessions. Recovering missed history is an operator decision about scope.
- **It does not fetch intraday data.** It re-asks for daily sessions the product already stores.
- **It does not re-examine open data quality findings on the scheduled path.** The mechanism to
  do that exists (`WidenToUnsettled`) and only the backfill command uses it. That gap is recorded
  in research R-009 and is deliberately not closed here.
