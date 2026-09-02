# Phase 0 Research: Rolling Re-observation of Recent Sessions

Everything the specification assumed about cost, confirmed against the code that would have to
change, plus the four decisions the design turns on.

---

## R-001: Does widening the requested range cost more source requests?

**Confirmed: no.** The provider client asks once per instrument with a range:

```go
query := url.Values{"from": {request.From.String()}, "to": {request.To.String()}}
```

One request per instrument per import, whatever the width. Requesting five sessions costs exactly
what requesting one costs — the same 100 calls for the curated universe — and the response is a
handful of rows larger.

**Consequence for the design**: the window width is not a cost dial. It can be configured freely
(FR-012), and SC-002 is satisfied by construction rather than by measurement. This is what makes
the feature small.

---

## R-002: Does re-observing an unchanged session write anything?

**Confirmed: no.** `upsertBar` reads the stored bar, compares source hashes, and returns before
any write when they match:

```go
if existing.SourceHash == candidate.SourceHash {
    return false, nil
}
```

Nothing is written, so the stored bar keeps the `import_run_id` of the run that first stored it.

**Why that matters more than it looks.** The incremental feature scope is derived from bars
carrying the *current* run's identifier:

```sql
SELECT instrument_id, session_date FROM daily_price_bars WHERE import_run_id = $1
UNION
SELECT instrument_id, session_date FROM price_bar_revisions WHERE superseding_run_id = $1
```

So an unchanged re-observation is invisible to it: no feature recomputation, and therefore no
strategy pass. A quiet night costs one slightly larger provider response and nothing else. This
is SC-003, and it is a property of code that already exists rather than something to build.

Had the unchanged path instead touched `last_observed_at` or `import_run_id`, every night would
have triggered a five-session recomputation for all 100 instruments, and the cascade would have
rescored the whole universe daily for no reason. The design depends on this staying true, so a
test must pin it rather than leave it as an incidental property.

---

## R-003: How is the window's start session found?

**Decision**: per exchange, from the stored exchange calendar, then clamped per instrument to its
first stored session.

The four exchanges keep different holiday calendars, so "five sessions back" is a different date
on each (FR-002). The calendar is already stored and already the authority for deciding whether
an absent session is a closure or a gap, so it answers this too: the Nth most recent row in
`exchange_sessions` with an open status, on or before the target date.

The per-instrument clamp (FR-004) follows the pattern the codebase already uses for exactly this
shape of problem. `TargetsForUniverse` carries `EarliestUnsettled` along with each target, with
the reason stated in its own comment — "so a hundred instruments cost one query rather than a
hundred, and so no caller can forget to ask for it". The first stored session rides along the
same way.

**Alternatives rejected**: five calendar days, which reaches a different distance into each
exchange and lands mid-holiday differently every week; and a per-instrument calendar query, which
is a hundred round trips for four distinct answers.

---

## R-004: How is a correction counted separately from an insertion?

**Decision**: `upsertBar` reports which of the three things happened, and the persist loop counts
the middle one.

Today it returns `changed bool`, which is true for a fresh insert *and* for a revision — the
caller only needs to know whether to publish a change event. A corrected-session count (FR-009)
needs the distinction, so the return becomes a stated outcome: inserted, revised, or unchanged.
The event still fires for the first two, which is exactly what `changed` meant.

`ImportCounts` gains `Revised`, alongside the processed, accepted, rejected and flagged counts it
already carries, and the value is persisted on both the run and its per-instrument items.

**Alternative rejected**: deriving the count afterwards by querying `price_bar_revisions` for
`superseding_run_id = <run>`. It would need no change to the write path, but it makes a reported
figure depend on a second query that could disagree with the run it describes, and it cannot be
constrained against the processed count in the schema (FR-010).

---

## R-005: Same run kind, or a new one?

**Decision**: the same kind.

`daily_update` names the scheduled pass that keeps stored history current. Widening how far back
it looks does not change what it is for, and a second kind would split the operational report so
that an operator has to read two rows to learn what one pass did. The existing check constraint
on run kind, and every screen and query that groups by it, stay as they are.

This also keeps the migration to one column rather than a column plus a vocabulary change.

---

## R-006: What does the operator see?

**Decision**: a stated count of corrected sessions on the run, beside the counts already shown.

The operational screen lists processed, accepted, rejected and flagged. Corrected joins them as a
fifth labelled value, as text, with no reliance on colour — a correction is the interesting case
precisely because it means derived values changed underneath, so it must be legible rather than
merely present.

A run that corrected nothing must not imply otherwise (FR-011). Zero is shown the way the other
zero counts are shown, without emphasis.

---

## R-007: How is the window configured, and what stops a bad value?

**Decision**: one environment value, `MARKET_DATA_REOBSERVE_SESSIONS`, defaulting to 5, validated
at startup against a stated range.

The project's configuration already validates bounded integers this way — `MARKET_DATA_WORKERS`
is bounded 1..16 and `MARKET_DATA_MAX_RETRIES` 0..10, both refused at startup rather than
clamped. This follows that, bounded 1..60: one session is the behaviour before this feature, and
sixty is about three months, past which an operator wanting history should be running a backfill
and deciding its scope deliberately (FR-013).

Refusing rather than clamping matters here. A silently clamped value would mean an operator who
set 500 believing they had covered a quarter's corrections would actually be covering three
months, and would have no way to discover the difference.

---

## R-008: What is the budget?

**Decision**: the existing daily-update budget, unchanged.

The pass makes the same number of requests (R-001) and, on a night with no restatement, performs
the same number of writes: none (R-002). What grows is the number of rows validated and hashed
per response — five sessions instead of one, for 100 instruments — which is arithmetic on a few
hundred rows against a pass whose time is dominated by 100 network round trips.

A night *with* a restatement costs the recomputation that restatement deserves, which is the
feature working rather than an overrun.

---

## R-009: A related gap this feature deliberately does not close

`WidenToUnsettled` exists to reach back far enough to re-examine a session with an open data
quality finding, and its comment records why: without it such findings "can never be re-examined
and stay open for good. Production held eight of them."

The scheduled daily pass does not apply it — only the backfill command does. So the same class of
problem this feature fixes for restatements still exists for open findings on the scheduled path.

It is one line to compose the two widenings, and the mechanism is identical. It is **out of scope
here** because it is a different claim about a different record, and folding it in would mean
shipping an unspecified behaviour change inside a specified one. It is written down so it is a
decision somebody makes rather than something nobody noticed.

---

## Summary of decisions

| # | Decision | Rejected alternative |
|---|---|---|
| R-001 | Width is free: one request per instrument per range | — (measured, not chosen) |
| R-002 | Unchanged re-observation writes nothing, so no cascade | — (measured, not chosen) |
| R-003 | Window start per exchange from the stored calendar, clamped per instrument | Calendar days; per-instrument queries |
| R-004 | `upsertBar` reports inserted/revised/unchanged; the count is persisted | Deriving the count from the revisions table afterwards |
| R-005 | Same `daily_update` kind | A new kind, splitting the operational report |
| R-006 | Corrected count as a labelled value beside the existing counts | Colour or emphasis alone |
| R-007 | `MARKET_DATA_REOBSERVE_SESSIONS`, default 5, bounded 1..60, refused not clamped | A fixed constant; silent clamping |
| R-008 | The existing daily budget, unchanged | A new budget for a pass that does the same work |
| R-009 | Findings widening on the scheduled path stays out of scope | Folding an unspecified change into a specified one |
