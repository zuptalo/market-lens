# Quickstart: A Finding Re-observation Cannot Settle

What changed, what the first morning will look like, and how to check it.

---

## What changed

The scheduled pass used to reach back to every open finding's session, every night. For a
condition the source keeps reporting — an absent session, most often — that never terminated: the
same row was rejected, the same finding re-raised, the run ended partial, and tomorrow it repeated.

Now, when an import covers a finding's session and raises the same rule again, the finding records
that it was re-examined and the condition still holds. It stops driving the reach-back and starts
waiting for a person.

Three consequences:

- The nightly observation count returns to the ordinary re-observation window's size.
- A run reports success when nothing about *that night* went wrong. A rejection matching a finding
  already awaiting a decision no longer decides the status — though it is still counted.
- Findings that re-observation cannot settle appear on the operational screen, where the owner can
  accept one as a limitation of the data.

The product never accepts one on its own. It can say *asking again did not change the answer*; it
cannot say *and therefore this is fine*.

---

## The first morning

Every finding currently open has never been re-examined, so the first pass re-examines each one
within the bound, marks it, and stops reaching for it. Expect:

- **A backlog appearing at once** — every long-standing finding shows as awaiting a decision on the
  first morning. That is the backlog becoming visible, not growing.
- **The observation count falling** over one or two nights, back to the ordinary window.
- **The run going green**, assuming nothing else is wrong.

---

## Configure the bound

```sh
MARKET_DATA_MAX_REACH_SESSIONS=260   # about a trading year
```

Bounded 1..2600 and refused at startup outside that rather than clamped. It caps how far back the
scheduled pass may reach on account of any finding, so the pass's cost stays predictable whatever
the findings say. Recovering history beyond it is an explicit backfill.

---

## Check it

**The loop has stopped (SC-001, SC-003)**

```sql
SELECT to_char(started_at,'MM-DD') AS night, processed_count, rejected_count, status
FROM import_runs WHERE kind='daily_update' ORDER BY started_at DESC LIMIT 7;
-- processed_count should fall to the ordinary window's size within two nights,
-- and status should be 'succeeded' unless something genuinely failed that night
```

**Nothing was accepted without a person (SC-004)**

```sql
SELECT count(*) FROM data_quality_findings
WHERE status='accepted_limitation' AND accepted_by IS NULL;
-- expect 0 — and the check constraint means it can only ever be 0
```

**No request reached past the bound (SC-005)**

```sql
SELECT count(*) FROM import_items it
JOIN import_runs r ON r.id=it.run_id AND r.kind='daily_update'
JOIN instruments i ON i.id=it.instrument_id
WHERE (SELECT count(*) FROM exchange_sessions s
       WHERE s.exchange_id=i.exchange_id AND s.status IN ('open','half_day')
         AND s.session_date BETWEEN it.requested_from AND it.requested_to) > 260;
-- expect 0
```

**The counts still tell the truth (SC-007)**

```sql
SELECT to_char(started_at,'MM-DD') AS night, status, rejected_count
FROM import_runs WHERE kind='daily_update' AND rejected_count > 0
ORDER BY started_at DESC LIMIT 5;
-- a succeeded run may still report rejections; that is the point, and it is not a contradiction
```

**What is waiting for you**

```sql
SELECT i.ticker, f.session_date::text, f.rule, f.detail
FROM data_quality_findings f JOIN instruments i ON i.id=f.instrument_id
WHERE f.status='open' AND f.reexamined_at IS NOT NULL
ORDER BY f.session_date;
```

---

## What this does not do

- **It does not notice a source fixing a years-old session.** A finding awaiting a decision is no
  longer re-examined automatically, because doing so costs a years-wide request every night
  against an event never once observed here. An operator who believes it has changed runs a
  backfill covering that session; the existing resolution rule then settles it.
- **It does not decide anything about your data.** Accepting a limitation is an owner action,
  recorded with a name and a time.
- **It does not change which rules raise a finding, or when one resolves.** The resolution rule is
  reused unchanged; this feature only names the state reached when that rule declines to fire.
