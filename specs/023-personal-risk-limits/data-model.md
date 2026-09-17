# Phase 1 Data Model: Personal Risk Limits

One new table. Everything a limit says about a portfolio is computed when it is read.

---

## `risk_limits`

| Column | Rule |
|---|---|
| `id` | |
| `user_id` | `NOT NULL REFERENCES users`. The owner, read directly by every predicate — the same discipline feature 022 established, for the same reason |
| `kind` | One of `instrument_share`, `sector_share`, `market_share`, `holding_count` |
| `threshold` | `numeric(24,12)`. A share for the first three, a count for the last |
| `created_at`, `updated_at` | |

**Constraints in the schema rather than in code**

- `UNIQUE (user_id, kind)` — one limit of each kind per person (R-008). Two thresholds for the same
  thing is a contradiction, not a refinement.
- A share threshold is greater than zero and at most one; a count threshold is a positive whole
  number. A share above 100% is not a rule, it is a typo, and the database is where that stops
  being possible.

**What is not here.** No breach row, no evaluation row, no observed-at timestamp, no state column.
An evaluation is computed on every read (R-003), so there is nothing stored that could disagree with
the holdings behind it.

**No seed data.** The product publishes no default limits (FR-002) — a default threshold is the
product telling somebody what is prudent, which is the line this feature exists to stay on the right
side of. A person with no limits has none, and the screen says so.

---

## What an evaluation is, and what it is made of

Computed per read from `portfolio.View`:

| Field | Meaning |
|---|---|
| `kind`, `threshold` | Copied from the stored limit |
| `state` | `within`, `exceeded`, or `unevaluable` — never absent, never defaulted |
| `measured` | The figure compared against the threshold, or absent when unevaluable |
| `absence_reason` | Set exactly when the state is `unevaluable` |
| `denominator` | The portfolio total the share was measured against, so the percentage can be checked |
| `contributions` | The groups that produced the measurement — each with its label, its value, and its share |

The worst offender is not singled out. The contributions are the whole breakdown in descending
order, so a reader sees where their money is rather than being pointed at one holding — which would
be a step toward telling them what to sell.

---

## How each kind is measured

| Kind | Numerator | Denominator | Unevaluable when |
|---|---|---|---|
| `instrument_share` | The largest single holding's value | Portfolio total | The total is incomplete |
| `sector_share` | The largest sector's summed value, `unclassified` included as a sector | Portfolio total | The total is incomplete |
| `market_share` | The largest market's summed value, by listing exchange | Portfolio total | The total is incomplete |
| `holding_count` | The number of open holdings | — | Never: counting needs no price |

An empty portfolio makes every kind `unevaluable` with `nothing_held` rather than `within`. Nothing
to measure is not compliance.

---

## The one change to an existing table

None. `portfolio.Holding` gains `Sector`, `SectorName` and `MIC` (R-002), but those come from the
existing join against `instruments` and `exchanges` — no column is added anywhere.

---

## Migration

`0027_risk_limits.sql` — one table, its constraints, and no seed data.

Its test proves a clean install and an upgrade both arrive with the table present, `user_id` not
nullable, a second limit of the same kind refused, a share above one refused, a zero or negative
threshold refused, a fractional holding count refused, and no manual step.
