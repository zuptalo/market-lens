# Phase 0 Research: Reusable Feature Engine

**Feature**: 013-feature-engine · **Date**: 2026-09-01

The spec left no `NEEDS CLARIFICATION` markers — its Resolved Decisions section already
settled the one open question, what relative strength is measured against. What follows are
the technical decisions the spec deliberately does not make, each of which has to be settled
before tasks can be written.

---

## R-001: What "identical values" means for irrational arithmetic

**Decision**: Compute in Go `float64` with a stated order of operations, then store as
`numeric(24,12)` after rounding half-to-even at the twelfth decimal place. Reproducibility is
defined at the stored precision, and the definition of every feature states this.

**Rationale**: FR-002 requires identical values for identical inputs and SC-001 requires zero
differences across recomputation. Taken literally at full machine precision this is not
achievable for this feature set: volatility needs a square root, logarithmic returns need a
logarithm, RSI and MACD need exponential smoothing. These are irrational functions, and Go
makes no bit-level guarantee for `math.Log` or `math.Sqrt` across architectures — the
production image runs on both `amd64` and `arm64`.

Rounding to a fixed precision converts an unachievable promise into a testable one. Twelve
decimal places is far beyond the significance of any daily price series while sitting well
inside `float64`'s ~15-17 significant digits, so the rounding absorbs last-place variation
without discarding anything a reader could act on.

Storing `numeric` rather than `float8` matters for the same reason the price columns are
`numeric`: a value read back must equal the value written, and the API must not re-round.

**Alternatives considered**:

- *Exact decimal arithmetic throughout* — rejected. It cannot express `sqrt` or `ln` exactly,
  so it would force a rounding decision anyway, at far greater cost and complexity.
- *`float8` storage with an epsilon-tolerant comparison* — rejected. It makes SC-001
  unfalsifiable: "zero differences" becomes "no differences larger than a number we chose",
  and the test that proves it would be the test most likely to hide a real defect.
- *Fixing the architecture to `amd64`* — rejected. It contradicts the multi-arch image the
  project already ships and would not help across libm versions in any case.

**Consequence for tasks**: every feature definition states its evaluation order explicitly,
and a determinism test recomputes the fixture universe twice and asserts exact equality of the
stored `numeric` values, not approximate equality.

---

## R-002: The shape of the feature store

**Decision**: One row per (instrument, session, definition), long format, referencing a
`feature_definitions` table that owns the name, version, window and parameters. A value row
carries either a value or an absence reason, never both and never neither.

**Rationale**: FR-001 and FR-022 require definitions to be additive, versioned and never
edited in place, and require values from different versions to remain distinguishable side by
side. A wide table with one column per feature cannot express that: adding a feature or a
version is a schema migration, and two versions of the same feature cannot coexist in one row
without doubling the columns.

The long format also gives FR-014 and FR-017 somewhere natural to live. An absence is a row
with a reason (`insufficient_history`, `window_gap`), which is a different fact from no row at
all (not yet computed) and from a row with a zero. The spec is emphatic that these three must
never be conflated, and a wide table with nullable columns cannot tell the first two apart.

Volume is acceptable. Production holds 244,116 bars across 100 instruments; at roughly twenty
definitions that is about 4.9M value rows, which is unremarkable for PostgreSQL and indexes
cleanly on `(instrument_id, session_date)`.

**Alternatives considered**:

- *Wide row per (instrument, session)* — rejected for the versioning reason above. It reads
  faster, but the read pattern this feature must serve is "one instrument as of one session"
  (FR-025) and "three named features across the universe" (FR-026), both of which the long
  format indexes for perfectly well.
- *JSONB blob per (instrument, session)* — rejected. It defeats indexing on a single feature,
  makes the Markets join (FR-026) expensive, and puts the definition version inside a document
  where no constraint can reach it.

---

## R-003: Where the computation runs and what triggers it

**Decision**: In-process, in a new `server/internal/features` package, driven by (a) an owner
CLI subcommand mirroring `marketdata backfill`, and (b) an incremental pass at the end of a
successful import run. No new process, no scheduler beyond the one that already exists.

**Rationale**: Constitution II puts background work inside the application and VIII keeps
production to one image plus PostgreSQL. The market-data importer already establishes the
shape this should follow — a run row, per-item rows, advisory locking, and a CLI entry point —
and reusing it means the operational story is the one already in production rather than a
second one to learn.

Triggering off a successful import is what makes FR-021 tractable: the import already knows
exactly which sessions it wrote or revised, so the incremental pass gets its scope for free
rather than having to rediscover it by comparison.

**Alternatives considered**:

- *Compute on read* — rejected by the spec's own assumption: storage is what makes a value
  point-in-time readable and its version a fact rather than a reconstruction.
- *A separate worker process* — rejected, Constitution VIII.

---

## R-004: The scope of an incremental recomputation

**Decision**: When a bar for session S is written or revised for an instrument, recompute that
instrument's features for the session range `[S, S + (W_max − 1) sessions]`, counted in stored
exchange sessions rather than calendar days, where `W_max` is the longest window of any active
definition. When a definition is added or superseded, recompute only that definition, across
the full history.

**Rationale**: FR-021 requires exactly the affected sessions to change and no others, and
SC-008 measures it as a count of changed values. A bar at session S participates in the window
of every session from S forward until it falls out the back of the longest window — that
range, and no more.

Counting in sessions rather than days is FR-004 applied to the recomputation itself; a
calendar-day approximation would over-recompute across holidays and under-recompute across
none, and would make SC-008 fail for reasons unrelated to correctness.

**Alternatives considered**:

- *Recompute the whole instrument on any change* — rejected. It satisfies correctness and
  fails SC-008, which exists precisely to stop this.
- *Recompute per feature using each feature's own window* — deferred, not rejected. It is
  strictly tighter than `W_max` and can be introduced later without changing any stored shape.
  `W_max` is chosen first because it is one number to get right rather than twenty.

---

## R-005: The universe composite as a stored series

**Decision**: The composite is its own table keyed by session, carrying the equal-weighted
mean return, the contributor count, and its definition version. It is computed before any
instrument-level relative-strength feature in the same run, and relative strength reads it
rather than recomputing it.

**Rationale**: FR-008a defines the composite per session across all instruments, so it is not
a per-instrument feature and does not fit the value table's grain. FR-008b requires the
contributor count to be stored per session, which is a property of the composite and of
nothing else. Computing it once per run and reading it back also makes the ordering
requirement in FR-002 trivially satisfiable: instrument processing order cannot affect a
series that was finished before instrument processing began.

**Consequence**: the run has two ordered stages. Any parallelism within stage two is safe;
across the boundary it is not.

---

## R-006: How the Markets table adopts the engine's values

**Decision**: The listing query reads the engine's values for the three statistics when a
value exists for the instrument's latest stored session, and otherwise reports them absent.
The existing derived-statistics CTE is deleted in the same change, not left as a fallback.

**Rationale**: FR-026 and SC-010 require the displayed numbers to be the engine's. The spec's
assumptions bind the engine to feature 005's definitions verbatim as version 1, precisely so
that adopting the engine cannot change a displayed number — which means a fallback would be
indistinguishable from the engine in every case where both produce a value, and would silently
mask the engine failing to produce one.

Keeping both would also violate the spirit of FR-026's "remain absent rather than zero": an
instrument whose features have not been computed must read as absent, and a fallback would
give it a number instead, hiding exactly the condition an operator needs to see.

**Alternatives considered**:

- *Dual-read with the CTE as fallback* — rejected above.
- *Dual-write and compare for a release before switching* — rejected as unnecessary. The
  equivalence is provable by test against fixture data with no production exposure, and
  SC-010 already requires the comparison across the full universe.

**Risk accepted and mitigated**: the Markets table shows nothing for these three columns until
the first computation completes. The migration test required by the spec covers exactly this —
an upgrade leaves the existing statistics readable until the engine's values exist — so the
adoption lands only once values are present.

---

## R-007: Regime boundaries

**Decision**: Four regimes — `trending_up`, `trending_down`, `volatile`, `range_bound` —
classified by explicit numeric boundaries over this engine's own trend, realised-volatility
and drawdown outputs, evaluated in a stated precedence order. The regime is undefined whenever
any input it reads is undefined.

**Rationale**: FR-013 requires explicit, testable boundaries and the spec's assumptions forbid
judgement, external classification or any model. A fixed precedence order is what makes the
classification a function rather than a set of overlapping rules, which is what FR-002 needs.

Propagating undefined is required by FR-014: a regime computed from a missing input would be
an invented value wearing a category name, which is harder to detect than an invented number.

**Version-1 parameters** (decided at tasking, stored in the definition's `parameters` so a
later change is a version rather than a silent redefinition): in precedence order,
`volatile` when `volatility_20 >= 0.40`; else `trending_up` when `trend_50_200 > 0.05` and
`drawdown_250 > -0.10`; else `trending_down` when `trend_50_200 < -0.05`; else
`range_bound`. The boundaries are inclusive exactly as written, and the regime is a *label*
— `feature_values` carries it in a `label` column, never encoded as a number.

---

## R-008: Time budgets

**Decision**: Full computation from empty over the curated universe within **10 minutes**; an
incremental pass after one daily import within **30 seconds**. Both measured on the deployment's
own hardware, both asserted by test at fixture scale and recorded from production on first run.

**Rationale**: SC-006 requires stated budgets rather than any particular value. The full
computation is ~4.9M values over 244k bars; ten minutes is comfortable for a single-threaded
implementation and leaves room for the engine to stay simple. The incremental case touches one
session times `W_max` per instrument, which is thousands of values, not millions.

These are budgets, not measurements. The task list must include recording the first production
figures against them.

---

## R-009: Fixed windows for the smoothed oscillators

**Decision**: RSI and MACD are computed over a *fixed* window of stored sessions — 140 for
`rsi_14`, 130 for the MACD family — with each smoothing seeded by the simple average of its
first period inside that window. They are not smoothed recursively from the start of history.

**Rationale**: The textbook recursion makes the value at session t depend on every bar since
the instrument listed. That is still point-in-time correct, but it means a revision to one
bar at session S changes every later session, which defeats R-004's bounded recomputation
range and makes SC-008 unmeasurable. A fixed window restores a finite `W_max`, and the seed
rule is stated so the evaluation order is explicit (R-001). The values differ slightly from
an unbounded recursion; that difference is a property of the definition, which is versioned.

**Alternatives considered**: recursion from the first stored bar, rejected above; a
per-feature "affected range" rule that treats these two as unbounded, deferred with the rest
of per-feature scoping in R-004.

## Open items carried into tasks

None blocking. R-007's thresholds and R-008's confirmed figures are decided during
implementation and recorded in the definition table and the feature documentation
respectively.
