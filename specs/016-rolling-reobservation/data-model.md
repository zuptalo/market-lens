# Phase 1 Data Model: Rolling Re-observation of Recent Sessions

One new stored fact, on a record that already exists. No new table, no new entity, no change to
what a bar or a revision means.

---

## Import run — one added count

`import_runs` gains `revised_count`: how many stored sessions this run replaced with corrected
source values, as distinct from how many it stored for the first time.

| Column | Type | Rule |
|---|---|---|
| `revised_count` | `bigint NOT NULL DEFAULT 0` | `>= 0`, and `<= processed_count` |

The default is what makes the upgrade honest for runs that predate the feature: they corrected
nothing that anybody counted, and zero is the truthful answer rather than an unknown dressed as
one. Every historical run genuinely did correct zero sessions in the sense the column means,
because nothing ever asked the source to reconsider one.

The constraint against `processed_count` is the same shape as the two the table already carries
(`accepted + rejected <= processed`, `flagged <= processed`) and encodes FR-010: a run cannot
correct more sessions than it looked at.

## Import run item — the same count, per instrument

`import_run_items` gains `revised_count` on the same terms, so a run that corrected three
sessions can say which instrument they belonged to. This is what makes the count actionable
rather than merely alarming.

---

## Observation window — a value, not a record

How far back a routine pass re-asks is configuration, not data:
`MARKET_DATA_REOBSERVE_SESSIONS`, default 5, bounded 1..60, refused at startup outside that
range. It is deliberately not stored in the database: it describes how this deployment operates,
not what is true about the market, and a value in the database would make two deployments of the
same image behave differently for reasons no backup would explain.

---

## What does not change

- **`daily_price_bars`** — a corrected bar is replaced in place, as it always was.
- **`price_bar_revisions`** — the archive of superseded values, written by the path that already
  exists. This feature causes it to be written in normal operation rather than only when an
  operator intervenes; it does not change what it holds.
- **`feature_values`, `signals`** — recomputed by the existing cascade, keyed and scoped exactly
  as they are today.
- **Run kind vocabulary** — `daily_update` keeps its meaning (R-005).

---

## Migration

One ordered migration, `0022_revised_session_count.sql`: two columns and their constraints.

Its test must prove that a clean database and an upgrade from the current schema both arrive with
the columns present, defaulted to zero on every existing run, and constrained so a count cannot
exceed the processed count — with no manual step, and with existing runs, bars, revisions,
features and signals undisturbed.
