# Data Model: Deterministic Strategies and Signals

Four new tables. Nothing existing changes shape; features, bars and instruments are read only.

---

## `strategies` — the published, versioned definition

| Column | Type | Notes |
|---|---|---|
| `id` | `uuid` PK | |
| `name` | `text` NOT NULL | Stable across versions, e.g. `momentum_trend`. |
| `version` | `int` NOT NULL | Unique with `name`. |
| `title` | `text` NOT NULL | What a person reads. |
| `intent` | `text` NOT NULL | What the strategy is trying to express, in prose (FR-003). |
| `caveat` | `text` NOT NULL | Why it exists — to validate the platform, not to claim optimality (FR-004). Stored so no surface can show a strategy without it. |
| `parameters` | `jsonb` NOT NULL | The whole version: factors, weights, transforms, action bands, liquidity rules. |
| `published_at` | `timestamptz` NOT NULL | |
| `superseded_at` | `timestamptz` NULL | Set when a later version publishes. Never deleted. |

- **Unique**: `(name, version)`; partial unique on `name` where `superseded_at IS NULL` — one
  current version per strategy.
- **Check**: every text column non-blank after trimming.
- **Immutability**: enforced by review and by migration-only evolution (constitution III). A
  parameter change is a new row, never an `UPDATE` of `parameters`.

### `parameters` shape

Stored as one document because it *is* the version — splitting it into rows would let a factor
be edited without publishing a version, which FR-002 forbids.

```text
factors[]        name, feature, mode (cross_sectional | absolute), transform, weight
action_bands[]   lower, upper, action        (contiguous, cover [-1,+1], upper bound wins)
liquidity        minimum stored sessions, minimum median volume, and similar gates
confidence       the agreement × coverage definition (R-003), recorded so it travels with the version
```

## `strategy_runs` — one computation

| Column | Type | Notes |
|---|---|---|
| `id` | `uuid` PK | |
| `strategy_id` | `uuid` NOT NULL → `strategies(id)` | |
| `kind` | `text` NOT NULL | `full`, `incremental`, `strategy`. |
| `status` | `text` NOT NULL | `running`, `succeeded`, `partial`, `failed`. |
| `universe_id` | `uuid` NOT NULL → `research_universes(id)` | |
| `trigger_feature_run_id` | `uuid` NULL → `feature_runs(id)` | The feature run this followed. |
| `started_at` / `finished_at` | `timestamptz` | Finished is NULL exactly while running. |
| `instrument_count`, `signal_count` | `bigint` NOT NULL DEFAULT 0 | |
| `app_version` | `text` NOT NULL | |

Mirrors `feature_runs` deliberately: the operations screen already knows how to read that shape.

## `strategy_run_items` — one instrument's outcome in a run

| Column | Type | Notes |
|---|---|---|
| `run_id` | `uuid` NOT NULL → `strategy_runs(id)` | |
| `instrument_id` | `uuid` NOT NULL → `instruments(id)` | |
| `status` | `text` NOT NULL | `running`, `succeeded`, `failed`, `skipped`. |
| `from_session` / `to_session` | `date` NULL | |
| `signal_count` | `bigint` NOT NULL DEFAULT 0 | |
| `error_code` / `error_summary` | `text` NULL | Present exactly when failed (FR-020). |

- **Primary key**: `(run_id, instrument_id)`.

## `signals` — one strategy version's view of one instrument as of one session

| Column | Type | Notes |
|---|---|---|
| `instrument_id` | `uuid` NOT NULL → `instruments(id)` | |
| `session_date` | `date` NOT NULL | |
| `strategy_id` | `uuid` NOT NULL → `strategies(id)` | The version, not the family. |
| `score` | `numeric(24,12)` NULL | In [-1, +1]. Null exactly when absent. |
| `action` | `text` NULL | `BUY`, `HOLD`, `REDUCE`, `SELL`, `WATCH`. |
| `confidence` | `numeric(24,12)` NULL | In [0, 1]. Agreement × coverage (R-003). |
| `absence_reason` | `text` NULL | `insufficient_history`, `feature_unavailable`, `composite_undefined`, `liquidity_excluded`. |
| `contributions` | `jsonb` NOT NULL | Ordered; each carries factor name, feature, the feature value read, its session, the normalised factor score, the weight, and the contribution. Empty array when absent. |
| `divisor` | `numeric(24,12)` NULL | Sum of available weights, so contributions and score reconcile exactly (R-001, FR-011). |
| `computed_at` | `timestamptz` NOT NULL | |
| `run_id` | `uuid` NOT NULL → `strategy_runs(id)` | Which run wrote it (FR-016). |

- **Primary key**: `(instrument_id, session_date, strategy_id)` — one signal per instrument, per
  session, per version (FR-008a), and the point-in-time lookup is that key.
- **Check**: exactly one of a scored state or an absence —
  `(score IS NOT NULL AND action IS NOT NULL AND confidence IS NOT NULL AND absence_reason IS NULL)`
  `OR (score IS NULL AND action IS NULL AND confidence IS NULL AND absence_reason IS NOT NULL)`.
  This is what stops a HOLD standing in for "no data" (FR-009, FR-012).
- **Check**: `score BETWEEN -1 AND 1`, `confidence BETWEEN 0 AND 1`, `action` in the five values.
- **Index**: `(strategy_id, session_date, score DESC)` for the ranked view.

Numbers are `numeric(24,12)` and cross the wire as decimal strings, exactly as feature values do:
a score that has been through a binary float is not the score that was recorded.

---

## Relationships

```text
strategies ──1:N──> signals            (by version, so a superseded version keeps its signals)
strategies ──1:N──> strategy_runs ──1:N──> strategy_run_items

instruments ──1:N──> signals
feature_runs ──0:1──> strategy_runs    (trigger_feature_run_id)

feature_values ─── read only, never written by this feature
```

## Validation rules

- A signal is scored or absent, never both and never neither.
- A scored signal's contributions sum, divided by its divisor, equal its score to the stored
  precision. This is asserted by test rather than by constraint, because the database cannot
  reasonably check arithmetic over a document.
- An instrument with stored history has a signal for every one of its sessions under every
  published version — scored or absent (FR-008a).
- Signals are never deleted when an instrument leaves the universe (FR-026); membership is not
  part of the key.
