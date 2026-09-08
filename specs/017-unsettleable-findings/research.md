# Phase 0 Research: A Finding Re-observation Cannot Settle

Four decisions, each checked against the code that would have to change.

---

## R-001: How does a finding drive the reach-back today?

`TargetsForUniverse` carries the oldest open session along with each target:

```sql
coalesce((SELECT min(d.session_date)::text FROM data_quality_findings d
    WHERE d.instrument_id=i.id AND d.status='open' AND d.session_date IS NOT NULL),'')
```

`WidenToUnsettled` then moves the request's lower bound back to it. So the loop is driven by one
predicate — `status='open'` — and a finding leaves the loop the moment it stops matching.

**Consequence**: the fix is a narrowing of that predicate, not a new mechanism.

---

## R-002: A new status, or a new fact about an open finding?

**Decision**: a new fact. `reexamined_at` and `reexamining_run_id` on the finding; the status
column is left alone.

A finding that has been re-examined is still open — the condition it describes is still true, and
it must still resolve if a later import finds the condition gone. Making it a *status* would take
it out of `status='open'`, which is the predicate the resolution rule itself uses:

```sql
WHERE d.instrument_id=$1 AND d.status='open' AND d.session_date BETWEEN $2 AND $3
  AND NOT EXISTS (... raised this rule again ...)
```

So a new status would have satisfied FR-002 by breaking FR-003. "Awaiting a decision" is
therefore a state a reader derives — open, and already re-examined — rather than a fourth value
in a column that already means something else.

**Alternatives rejected**: a fourth status value, which would require every existing `status='open'`
read to be revisited and would silently stop those findings resolving; and deleting the finding,
which would lose the record that the condition was ever observed.

---

## R-003: What stops a known rejection from making the run partial?

**Decision**: the item's status is decided by rejections the product has not already examined.

Today it is decided by the count alone:

```go
status := ImportSucceeded
if counts.Rejected > 0 { status = ImportPartial }
```

A rejection matching a finding that is open *and* already re-examined for that instrument and
session is a standing known condition, not news about tonight. It is excluded from the count that
decides the status — and from nothing else. `rejected_count` still reports every rejection
(FR-009), so suppressing a status never suppresses a number.

**Alternative rejected**: suppressing the rejection itself. It happened; the row was not stored;
a count that hid it would make the import report say something untrue in order to make a badge
look right.

---

## R-004: What bounds the pass regardless?

**Decision**: `MARKET_DATA_MAX_REACH_SESSIONS`, default 260 — about a trading year — validated at
startup and refused outside 1..2600 rather than clamped.

The narrowing in R-002 terminates the loop, but only for findings the product has re-examined. A
newly raised finding on an old session, or a backfill that raises many, would still widen the next
scheduled pass without a bound. The bound makes the pass's cost predictable whatever the findings
say, and beyond it recovering history is an explicit operator action — the rule this project
already follows.

260 rather than something smaller because the reach exists to re-examine findings, and a bound
shorter than the interval between an operator's backfills would make the mechanism useless. The
same reasoning as feature 016's window: refused rather than clamped, so an operator who sets a
value believing it covers a decade finds out.

---

## R-005: What does an operator do, and where?

**Decision**: read on the operational screen, accept from there, refused to anyone but the owner
in the service rather than only hidden in the interface.

The findings read already exists and is authenticated. Accepting is new, and is the only mutation
this feature adds. It records `accepted_at` and `accepted_by`, because FR-014 requires every state
change to be attributable and "a person decided this" is exactly the kind of claim that needs a
name against it.

**Alternative rejected**: a command-line action. The operator is already reading the finding on
the screen when they form the judgement, and the constitution reserves the command line for
computation rather than for every owner action.

---

## R-006: What happens to the 27 findings already open in production?

They are open and have never been re-examined, so the first scheduled pass after this ships
re-examines each one whose session is within the bound, marks it, and stops reaching for it. The
observation count returns to the ordinary window within one or two nights, and 27 findings appear
awaiting a decision.

That is the backlog becoming visible rather than growing. It is worth stating plainly in the
quickstart so the first morning's screen is not mistaken for a new problem.

---

## Summary of decisions

| # | Decision | Rejected alternative |
|---|---|---|
| R-001 | The loop is one predicate; narrow it | — (measured, not chosen) |
| R-002 | Record re-examination as a fact, keep the status | A fourth status, which breaks the resolution rule it shares |
| R-003 | Known rejections stop deciding the status, never the counts | Suppressing the rejection itself |
| R-004 | A stated maximum reach, refused not clamped | Relying on the narrowing alone, unbounded |
| R-005 | Accept in the interface, owner only, attributed | A command-line action |
| R-006 | The existing 27 surface as a visible backlog | Migrating them to a state nobody chose |
