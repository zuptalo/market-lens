# Phase 1 Data Model: A Finding Re-observation Cannot Settle

Four columns on a table that already exists. No new table, no new entity, and deliberately no new
status value.

---

## Data quality finding — what it gains

| Column | Type | Meaning |
|---|---|---|
| `reexamined_at` | `timestamptz` | When an import last covered this finding's session and raised the same rule again. Null until that happens. |
| `reexamining_run_id` | `uuid` → `import_runs(id)` | Which run established it, so the claim is attributable. |
| `accepted_at` | `timestamptz` | When a person accepted the condition as a limitation. |
| `accepted_by` | `uuid` → `users(id)` | Who accepted it. A judgement about somebody's data carries their name. |

Constraints:

- `reexamined_at` and `reexamining_run_id` are set together or not at all.
- `accepted_at` and `accepted_by` are set together or not at all.
- A finding may only be accepted when it has been re-examined — the product must not offer, and an
  operator must not be asked for, a decision about a condition nothing has checked twice.
- `accepted_at IS NOT NULL` implies `status = 'accepted_limitation'`, so the two cannot disagree.

## The state a reader derives

There is no fourth status. "Awaiting a decision" is a question asked of two columns:

| State | Condition |
|---|---|
| Open, unexamined | `status='open' AND reexamined_at IS NULL` — drives the reach-back |
| Awaiting a decision | `status='open' AND reexamined_at IS NOT NULL` — does not |
| Resolved | `status='resolved'` — the condition ended |
| Accepted as a limitation | `status='accepted_limitation'` — a person judged it acceptable |

The reason is in research R-002: `status='open'` is the predicate the *resolution* rule uses, so
moving a re-examined finding out of it would stop it ever resolving. It is still open. It has just
been asked twice.

---

## What does not change

- **Which rules raise a finding**, and what each means.
- **The resolution rule** — an import that covers a session and does not raise the rule again
  resolves the finding, exactly as today. This feature adds a state reached when that rule
  declines to fire; it does not alter when it fires.
- **`rejected_count` and every other count on a run or item.** A known rejection stops deciding a
  *status*; it is still counted (FR-009).
- **The findings read and its event.** Both exist and are reused.

---

## Migration

One ordered migration, `0023_finding_reexamination.sql`: four columns and their constraints.

Its test must prove that a clean database and an upgrade from the current schema both arrive with
the columns present and null, every existing finding still `open` and untouched, the paired
constraints in force, and a finding refused acceptance while unexamined — with no manual step.

Existing findings are deliberately **not** migrated into the re-examined state. Nothing has
examined them twice yet; recording that it had would be the product asserting something it had not
done. They earn the state on the first pass that re-examines them, which is also what makes the
first night's screen an honest picture of the backlog (R-006).
