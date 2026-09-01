# Phase 1 Data Model: Reusable Feature Engine

**Feature**: 013-feature-engine · **Date**: 2026-09-01

All five entities are **shared reference data**. None carries an owner, because none is
user-owned (spec, Identity section). Every read still requires an authenticated active
session (FR-024); that is a boundary property, not an ownership one, and the distinction is
the one Constitution IX requires be kept visible in contracts, logs, events and migrations.

---

## `feature_definitions`

One named quantitative feature at one version. Additive only — a definition is never edited in
place (FR-001, FR-022).

| Column | Type | Notes |
|---|---|---|
| `id` | `uuid` PK | |
| `name` | `text` | e.g. `return`, `rsi`, `macd_signal`, `regime` |
| `version` | `int` | starts at 1, increments per name |
| `window_sessions` | `int` NULL | in stored exchange sessions (FR-004); NULL where a feature has no window |
| `price_basis` | `text` | `raw` \| `adjusted` — FR-001 requires this be explicit; see *What adjusted means* below |
| `parameters` | `jsonb` | definition-specific constants, e.g. `{"fast":12,"slow":26}` |
| `undefined_conditions` | `text` | prose, the conditions under which the feature is undefined |
| `session_length_sensitive` | `boolean` | FR-005: features whose meaning depends on session length must say so |
| `published_at` | `timestamptz` | |
| `superseded_at` | `timestamptz` NULL | set when a later version is published; the row itself never changes otherwise |

- **Unique**: `(name, version)`.
- **Check**: `window_sessions IS NULL OR window_sessions > 0`; `price_basis IN ('raw','adjusted')`.
- **Immutability**: enforced by convention and migration review, not by trigger — the same
  posture the project already takes toward applied migrations.

**What `adjusted` means.** Engine-applied, point-in-time adjustment: the raw closes, highs
and lows before a split's ex-date are divided by that split's ratio, considering only splits
whose ex-date is on or before the session being computed. It never reads the provider's
`adjusted_close` column. That column is back-adjusted as of import time, so a historical
session's adjusted close already reflects splits that had not happened yet at that session —
exactly the lookahead FR-019 forbids. The three statistics adopted from feature 005 read
`raw`, verbatim.

FR-022's "a version is published with no change in computed values" (edge case) is naturally
supported: a new row exists, values carry it, and version equality never implies value
equality.

---

## `feature_values`

One feature's value for one instrument on one session — or an explicit absence with its
reason. Long format, per R-002.

| Column | Type | Notes |
|---|---|---|
| `instrument_id` | `uuid` FK → `instruments` | |
| `session_date` | `date` | must be a session the exchange was open for (FR-016) |
| `definition_id` | `uuid` FK → `feature_definitions` | carries name + version (FR-003) |
| `value` | `numeric(24,12)` NULL | rounded half-to-even, per R-001 |
| `label` | `text` NULL | the value of a categorical feature — the regime is a name, not a number |
| `absence_reason` | `text` NULL | `insufficient_history` \| `window_gap` \| `composite_undefined` \| `zero_denominator` |
| `currency` | `text` NULL | set only for currency-denominated features (FR/spec US2-4) |
| `computed_at` | `timestamptz` | FR-003 |
| `run_id` | `uuid` FK → `feature_runs` | traceability (Key Entities: computation run) |

- **Primary key**: `(instrument_id, session_date, definition_id)`.
- **Check**: exactly one of `value`, `label` and `absence_reason` is non-null. This is the
  constraint that makes FR-014 structural rather than aspirational — a zero standing in for an
  absence cannot be written, and neither can a number standing in for a regime.
- **Check**: `absence_reason IN ('insufficient_history','window_gap','composite_undefined',
  'zero_denominator')` — FR-017 needs the first two distinguishable, the third is what FR-008
  propagates, and the fourth is a ratio whose denominator is zero (a twenty-session volume
  average of zero, a prior close of zero), which is none of the other three.
- **Index**: `(definition_id, session_date)` for the universe-wide reads FR-026 and SC-010 do.
- **Foreign key** to `exchange_sessions` is not expressible directly (sessions are keyed by
  exchange, not instrument); FR-016 is enforced in the computation and proven by SC-004's
  universe-wide assertion.

**Absent row vs. absence row.** No row means *not yet computed*. A row with an
`absence_reason` means *computed, and undefined for this stated reason*. The spec requires
these be distinguishable (edge case: "distinguishable from not yet computed") and the schema
makes them so.

---

## `universe_composites`

The equal-weighted series relative strength is measured against (FR-008a, FR-008b). Its own
grain — per session, not per instrument — per R-005.

| Column | Type | Notes |
|---|---|---|
| `universe_id` | `uuid` FK → `research_universes` | the composite is a composite *of one curated list*; a second universe gets its own series |
| `session_date` | `date` | |
| `definition_id` | `uuid` FK → `feature_definitions` | the composite is versioned like any other definition |
| `mean_return` | `numeric(24,12)` NULL | equal-weighted mean of session-over-session returns |
| `contributor_count` | `int` | FR-008b: recorded for every session |
| `absence_reason` | `text` NULL | `insufficient_contributors` |
| `computed_at` | `timestamptz` | |
| `run_id` | `uuid` FK → `feature_runs` | |

- **Primary key**: `(universe_id, session_date, definition_id)`.
- **Check**: exactly one of `mean_return` and `absence_reason` is non-null.
- **Check**: `contributor_count >= 0`, recorded even when the composite is undefined — the
  count is *why* it is undefined and FR-008b asks for it unconditionally.

**Naming.** The table, the API field names and every log line say *composite*. FR-008c forbids
the words *index* and *benchmark* anywhere in the product, and the schema is part of the
product a future reader will learn the domain from.

---

## `feature_runs`

One execution of the engine, so a value can always be traced to what produced it.

| Column | Type | Notes |
|---|---|---|
| `id` | `uuid` PK | |
| `kind` | `text` | `full` \| `incremental` \| `definition` |
| `status` | `text` | `running` \| `succeeded` \| `partial` \| `failed` |
| `universe_id` | `uuid` FK → `research_universes` | the list the run computed; the composite it wrote is keyed by it |
| `definition_name` | `text` NULL | set exactly when `kind = definition`: the one definition recomputed across history (R-004) |
| `started_at` / `finished_at` | `timestamptz` | `finished_at` is null exactly while `status = running` |
| `instrument_count` / `value_count` | `bigint` | what it covered |
| `app_version` | `text` | mirrors `import_runs`, which has already earned its keep once this session |
| `trigger_run_id` | `uuid` NULL FK → `import_runs` | set when an import triggered it (R-003) |

Mirrors `import_runs` deliberately. The operational vocabulary is then one vocabulary.

---

## `feature_run_items`

Per-instrument outcome within a run, mirroring `import_items`. Carries `status`, `error_code`,
`error_summary`, and the session range covered — which is what makes FR-023's partial-failure
requirement observable rather than merely asserted.

---

## Transactional boundaries

**FR-023** — a recomputation that fails partway leaves no partially updated session readable.
Each instrument's recomputation commits as one transaction covering its whole affected session
range. An instrument either has its new values or its previous ones; it never has a mixture.
Per-instrument rather than per-run, matching the importer's existing "commits each instrument
independently" behaviour, which the run/item status pair already reports honestly as `partial`.

**Concurrency** (edge case: two recomputations at once) — a PostgreSQL advisory lock keyed on
the instrument, exactly as the importer takes one. The second waiter observes the first's
committed result rather than producing a mixture of versions.

**Live updates** (Constitution X, spec Live Update section) — the `client_events` insert for a
completed instrument is written in the same transaction as its values. Not an outbox, not an
after-commit hook: the same transaction, which is what feature 002 already does for
`corporate_action` and what this session's audit of that path confirmed.

---

## Relationships

```
instruments ──< feature_values >── feature_definitions
                     │                     │
                     │                     └──< universe_composites >── research_universes
                     │                                                         │
feature_runs ──< feature_run_items >── instruments                             │
       ├──────────< feature_values                                             │
       └──────────────────────────────────────────────────────────────────────┘
```

---

## What is deliberately absent

No strategy, signal, score, ranking or backtest table. No user-owned table, no ownership
column, no per-user scope. The spec forbids all of these, and the milestones that follow own
them.
