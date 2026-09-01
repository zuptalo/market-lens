# Quickstart: Reusable Feature Engine

**Feature**: 013-feature-engine · **Date**: 2026-09-01

How to run, read and verify the engine once it exists. Written for the person who has to
operate it, which for this deployment is the same person who wrote it.

---

## Compute the feature history

```bash
# Everything, from empty. Budget: 10 minutes on the deployment's hardware (SC-006).
market-lens features compute --universe nordic-liquid-v1

# Only what a given import changed. Budget: 30 seconds.
market-lens features compute --since-run <import-run-id>

# One definition across the full history, after publishing a new version.
market-lens features compute --definition rsi_14
```

Mirrors `marketdata backfill` deliberately — same run/item vocabulary, same advisory locking,
same `run_id=... status=... ` summary line. In production:

```bash
POD=$(kubectl --context k3s -n market-lens get pods -o name | grep -v postgres | head -1)
kubectl --context k3s -n market-lens exec ${POD#pod/} -- \
  /app/market-lens features compute --universe nordic-liquid-v1
```

An import triggers the incremental pass on its own; the CLI is for a full rebuild and for
recovering from a failed run.

---

## Read the values

```bash
# As of the latest stored session.
curl -s -H "Cookie: $SESSION" \
  https://market-lens.zuptalo.com/api/v1/instruments/$ID/features | jq

# As of a historical session — returns only what was computable then (FR-018).
curl -s -H "Cookie: $SESSION" \
  "https://market-lens.zuptalo.com/api/v1/instruments/$ID/features?asOf=2024-06-28" | jq

# The definitions, including superseded ones.
curl -s -H "Cookie: $SESSION" \
  https://market-lens.zuptalo.com/api/v1/feature-definitions | jq
```

Reading three things in a response, and what each means:

| You see | It means |
|---|---|
| `"value": "0.042100000000"` | computed, this is the number |
| `"value": null, "absenceReason": "insufficient_history"` | computed, undefined — the instrument is too young for the window |
| `"value": null, "absenceReason": "window_gap"` | computed, undefined — a session inside the window has no stored bar |
| name listed in `notComputed` | the engine has not run for this feature yet |

The last one is the operationally interesting case, and it is why it is a separate field
rather than a fourth `absenceReason`. An engine that has not run is a thing to fix; an
undefined value is a fact about the data.

---

## Verify it

The four checks worth running by hand after a first computation, each mapping to a success
criterion the automated suite also covers.

**Determinism (SC-001)** — recompute and diff. Zero rows.

```sql
-- Run the full computation twice, keeping the first result in a temp table, then:
SELECT count(*) FROM feature_values v
JOIN previous_values p USING (instrument_id, session_date, definition_id)
WHERE v.value IS DISTINCT FROM p.value
   OR v.absence_reason IS DISTINCT FROM p.absence_reason;
-- expect 0
```

**No lookahead (SC-002)** — the property that makes the whole engine worth having.

```sql
-- After extending history with later sessions and recomputing, no earlier session moved.
SELECT count(*) FROM feature_values v
JOIN before_extension b USING (instrument_id, session_date, definition_id)
WHERE v.session_date <= :truncation_point
  AND (v.value IS DISTINCT FROM b.value
       OR v.absence_reason IS DISTINCT FROM b.absence_reason);
-- expect 0
```

*Verification record (T059, 2026-09-02).* The four leakage suites in
`server/internal/features/leakage_integration_test.go` — extending A's history by 60 sessions,
walking every satisfied window of A, D and E, adding a split to E first after the last session
and then inside the history, and listing a newcomer 30 sessions before the end — all passed on
first run and exposed nothing; no production code changed for US3. That they can fail was
proven by a throwaway one-session lookahead in `History.Window` (the window's last bar replaced
by the next stored bar): the extension test then reported *63 values on or before 2026-03-31
changed* and *1 composite session changed*, the window walk reported every definition reading a
bar after its session, the split test reported *20 values before the ex-date changed*, and the
newcomer test reported *20 values on or before the cut changed*. The lookahead was reverted
before commit. One expectation was corrected during the work: a newcomer's own first bar has no
prior bar, so the composite counts it only from the second session, not the first.

**Every value has a resolvable definition (SC-003)**

```sql
SELECT count(*) FROM feature_values v
LEFT JOIN feature_definitions d ON d.id = v.definition_id
WHERE d.id IS NULL;
-- expect 0
```

**Nothing on a closed date (SC-004)**

```sql
SELECT count(*) FROM feature_values v
JOIN instruments i ON i.id = v.instrument_id
LEFT JOIN exchange_sessions s
  ON s.exchange_id = i.exchange_id AND s.session_date = v.session_date
WHERE s.session_date IS NULL OR s.status NOT IN ('open', 'half_day');
-- expect 0
-- A half day is a session: XSTO trades a short day on 01-05 and 04-30 (migration 0005), and
-- the engine computes on it. Comparing against 'open' alone would report every one of them.
```

*Recorded evidence (T093, 2026-09-01, v0.9.0).* Run `97082913-252a-4d5d-8819-9c2db20e91ba`,
the first full computation in production, succeeded over 100 instruments and 242,921 bars,
writing 5,830,104 values in 248.7 s with no failed instrument. Against it:

| Check | Query | Result |
|---|---|---|
| SC-003 | values whose definition does not exist | **0** |
| SC-004 | values on a date the exchange did not trade | **0** |
| SC-004 | values on a session with no stored bar | **0** |
| FR-005 | rows settling other than exactly one of value/label/absence | **0** |
| SC-007 | one instrument's active definitions as of a session | **1.5 ms** (budget 2 s) |

SC-001 and SC-002 are proven by the fixture suites rather than by hand here: both need a
second copy of the values to compare against, and the volume this database lives on has under
1 GB free while `feature_values` alone is 1.1 GB. Recompute-and-diff belongs on a restored
copy, not on the deployment. SC-010 is checked by hand after the Markets adoption ships.

**Markets agrees with the engine (SC-010)** — open Markets, pick any row, read that
instrument's features as of its latest session, and confirm the three statistics match. They
must, because the engine adopts feature 005's definitions verbatim as version 1; a mismatch
means a computation defect, not a definition change.

*Recorded evidence (2026-09-02, v0.10.0).* The listing reads the same rows the features
endpoint serves — one join, no second arithmetic — so agreement is structural rather than
coincidental, and `TestMarketsAgreesWithTheEngineForEveryInstrument` walks the listing page by
page against `ReadAsOf` to prove it. In production all 100 instruments carry all three
statistics at their own latest stored session, none absent and none missing, for example
ERIC-B on 2026-09-01: `return_20 = -0.020507614213`, `return_90 = -0.086796024610`,
`volatility_20 = 0.148758541959`. The page itself is behind an authenticated session, so
comparing what a person sees to those decimals is the one step that stays manual.

---

## Adopted definitions

The Markets list shows three of the engine's definitions rather than numbers of its own:

| Column | Definition | Version |
|---|---|---|
| 20-session | `return_20` | 1 |
| 90-session | `return_90` | 1 |
| Volatility | `volatility_20` | 1 |

Each is read from `feature_values` at the instrument's own latest stored session. **No
definition changed to make this work**: version 1 of each is feature 005's derived statistic
written down — the same window, the same session counting, the same annualisation — precisely
so that adopting the engine could not move a number a person had already seen.

What was deleted is the second implementation. The `recent`, `log_returns` and `volatility`
CTEs and the `close_20`/`close_90` aggregates lived in `listingStatisticsCTE` in
`server/internal/instruments/listing.go` and computed the same three statistics from bars a
second time. They are gone; there is no fallback, because a fallback would be
indistinguishable from the engine whenever both produced a value and would silently hide the
engine failing to produce one.

Two consequences worth knowing:

- An instrument the engine has not computed shows the three as absent, not as zero. That is
  the state between an import and the pass that follows it, and between a deployment and the
  first `features compute`.
- The three now travel as decimal strings end to end — `numeric(24,12)` through the API to the
  table — because a JavaScript number cannot hold every value the column can.

---

## When something is wrong

| Symptom | Where to look |
|---|---|
| Markets shows no returns or volatility | the engine has not run — `feature_runs` for a `failed` or absent run |
| One instrument has no features | `feature_run_items` for its `error_code` |
| A value looks wrong | its `definitionVersion`, then that definition's `parameters` and `priceBasis` |
| Relative strength is undefined everywhere | `universe_composites.contributor_count` — below the stated minimum, the composite itself is undefined (FR-008b) |
| A recomputation seems to have half-finished | it cannot have. Each instrument commits its whole affected range or none of it (FR-023); check `feature_run_items` for which instruments completed |

---

## What this feature does not do

No strategies, signals, scores, rankings, recommendations or backtests. No user-owned data.
No currency conversion. No hourly data. Each is owned by a later milestone and none may be
pre-empted here.
