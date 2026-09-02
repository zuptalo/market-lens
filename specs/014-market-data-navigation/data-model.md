# Data Model: Market Data Navigation, Sector Data, and Continuous Listing

Only the sector story adds persistent data. The listing total is derived at read time, and the
operational screen reads tables that already exist.

---

## New: `sectors` — the classification vocabulary

Reference data, introduced and maintained by ordered migration (FR-022, FR-023).

| Column | Type | Notes |
|---|---|---|
| `code` | `text` PRIMARY KEY | Stable identifier, lower snake case (`health_care`). Never displayed. |
| `name` | `text` NOT NULL | What a reader sees (`Health Care`). Unique. |
| `display_order` | `int` NOT NULL | The order the filter offers them in. Unique. |
| `created_at` | `timestamptz` NOT NULL | |

- **Check**: `name` is non-blank after trimming.
- **Contents**: the eleven conventional sector names plus an explicit `unclassified` member,
  ordered so that `unclassified` sorts last.
- **Why a table and not an enum**: adding a value is then an ordinary migration (R-005).

## Changed: `instruments`

| Column | Change | Notes |
|---|---|---|
| `sector` | `text` NULL → **NOT NULL REFERENCES sectors(code)**, default `unclassified` | Holds the code, not the display name. The NOT NULL constraint is what makes "no classification at all" — today's state for all 100 rows — unrepresentable (FR-025). |
| `sector_source` | **new** `text` NOT NULL, default `unclassified` | Where the classification came from, e.g. `curated-2026-09` for the project's own review. |
| `sector_reviewed_on` | **new** `date` NOT NULL, default `CURRENT_DATE` | When it was last checked. What makes staleness visible (FR-024). |
| `industry` | unchanged | Out of scope; stays nullable and unused. |

**Migration path**: the existing column already holds `NULL` in every production row, so the
migration classifies each curated instrument by its provider symbol, then applies the constraint.
Any instrument not named by the classification set lands on `unclassified` through the default —
the migration must not fail because a company was missed, but the classification set must cover
the seeded universe and a test asserts it.

## Derived: listing result summary

Not stored. `ListingPage` gains a total, populated only for cursor-less requests (R-001):

| Field | Type | Notes |
|---|---|---|
| `Total` | `*int64` | Number of instruments matching the filter, ignoring the page limit. Absent on cursor-carrying requests, where the client already holds it. |

## Read-only: feature engine runs

`feature_runs` and `feature_run_items` exist from feature 013 and are unchanged. This feature
adds a read of the most recent runs — kind, status, timestamps, instrument count, value count,
failed count — and nothing else. No column is added, no row is written.

---

## Entity relationships

```text
sectors ──1:N──> instruments.sector          (NOT NULL, default 'unclassified')

instruments ──1:N──> feature_values          (unchanged, feature 013)
instruments ──1:N──> daily_price_bars        (unchanged, feature 002)

feature_runs ──1:N──> feature_run_items      (unchanged, feature 013; read-only here)
import_runs  ──1:N──> import_items           (unchanged, feature 002; read-only here)
```

## Validation rules

- An instrument's sector must be a code present in `sectors`. Enforced by foreign key.
- An instrument always has a sector state. Enforced by NOT NULL plus the default.
- `unclassified` is a member of the vocabulary, not an absence. The interface states it
  (FR-026) and the filter offers it when any instrument holds it (FR-021).
- The sector filter accepts a code; an unknown code is a client error (400), consistent with how
  the listing already rejects an unsupported sort.
