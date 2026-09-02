# Quickstart: Market Data Navigation, Sector Data, and Continuous Listing

What changes for a person using Market Lens, and how to check each part is working.

---

## What moves where

| Before | After |
|---|---|
| Market Data: instruments, then the import report below it | Market Data: instruments, with a one-line freshness statement near the top that links onward |
| Nowhere | Operations: import runs, their per-instrument outcomes, quality findings, and the feature engine's runs |
| "Load more" after every 50 rows | Rows arrive as you scroll; a focusable control does the same for keyboard use |
| No sense of size | "Showing 100 of 342" throughout, and an explicit end |
| Sector column blank in every row | Every instrument states its sector or states that it is unclassified |

---

## Read the operational state

```bash
# Whether the last import ran, and whether the engine recomputed after it.
# Both are now on one screen rather than one being invisible.
open https://<deployment>/operations
```

The engine's runs appear there for the first time. Until now a failed computation left the
statistics on Market Data stale with nothing anywhere to say so.

---

## Classify the universe

Sector is curated reference data, carried by ordered migration — never typed into the deployed
database, and never fetched from the market-data provider, whose plan for this deployment
excludes fundamentals.

```bash
# The vocabulary the filter offers, and how many instruments hold each value.
curl -s https://<deployment>/api/v1/instruments/sectors | jq '.items[] | "\(.name): \(.instrument_count)"'
```

Adding an instrument to the curated universe means classifying it in the same migration that
adds it. The database will not accept an instrument with no classification state at all: the
column is NOT NULL against a vocabulary that contains `unclassified`, so the failure this
feature exists to end cannot recur silently.

To reclassify a company — a genuine change of business, not a correction of taste — write a
new forward migration that updates the code, the source and the review date together. Applied
migrations are immutable.

---

## Verify it

**The listing knows its size (SC-004)**

```sql
-- What the screen says it is showing, against what the filter actually matches.
SELECT count(*) FROM instruments i JOIN exchanges e ON e.id = i.exchange_id
WHERE i.sector = 'health_care';
-- expect: the number the screen states as the total under the same filter
```

**Nothing repeats or is skipped across a full scroll (SC-005)**

```sql
-- After scrolling the whole universe, the identifiers rendered must be distinct
-- and must equal the identifiers the same filter returns.
SELECT count(*), count(DISTINCT id) FROM instruments;
-- expect: equal, and equal to the number of rows the reader scrolled past
```

**Every instrument is classified (SC-009)**

```sql
SELECT count(*) FROM instruments WHERE sector IS NULL;
-- expect 0 — and the column is NOT NULL, so this can only ever be 0

SELECT s.name, count(*) FROM instruments i JOIN sectors s ON s.code = i.sector
GROUP BY s.name ORDER BY count(*) DESC;
-- expect: every curated instrument accounted for, with 'Unclassified' visible if any hold it
```

**No filter can only ever return nothing (SC-008)**

```sql
-- Every choice the filter offers either has members or is 'unclassified'.
SELECT s.code, count(i.id) FROM sectors s LEFT JOIN instruments i ON i.sector = s.code
GROUP BY s.code HAVING count(i.id) = 0 AND s.code <> 'unclassified';
-- expect 0 rows, or a deliberate decision to keep an empty vocabulary member
```

**Classification provenance is visible (FR-024)**

```sql
SELECT sector_source, sector_reviewed_on, count(*) FROM instruments
GROUP BY sector_source, sector_reviewed_on ORDER BY 2 DESC;
-- expect: a source and a review date on every row; an old date is the signal to re-review
```

---

## When something is wrong

| Symptom | Where to look |
|---|---|
| The list stops loading mid-scroll | The connection state on the page; a failed page states itself and offers a retry rather than truncating silently |
| The stated total disagrees with the rows | A live change altered membership; the total is corrected and a refresh offered — the rows are deliberately not moved under the reader |
| A sector filter returns nothing | `instrument_count` in `/api/v1/instruments/sectors` for that code; an empty vocabulary member is a classification gap, not a query fault |
| Sector shows "Unclassified" for a company you can classify | A migration, not an UPDATE. Applied migrations are immutable; write a forward one |
| The engine's last run is missing from Operations | The engine has never run in this deployment, which the screen states explicitly rather than implying the statistics are current |

---

## What this feature does not do

- It does not fetch sector from the market-data provider. That plan excludes fundamental data,
  and the decision to curate instead is recorded in the specification's *Resolved decisions*.
- It does not add industry classification, which stays out of scope.
- It does not introduce numbered pages. The reader gains automatic loading and a stated total;
  keyset pagination is retained precisely so that a changing result set cannot repeat or skip a
  row underneath them.
- It adds no new event type. It defines what the client does with the existing ones once more
  than one page is loaded.
