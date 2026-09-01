# Implementation Plan: Reusable Feature Engine

**Branch**: `013-feature-engine` | **Date**: 2026-09-01 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/013-feature-engine/spec.md`

## Summary

Compute, store and serve versioned quantitative features derived from the daily history
feature 002 owns — returns, trend, momentum, relative strength against a universe composite,
realised volatility, ATR, RSI, MACD, drawdown, volume features and a regime classification.
Every value carries the definition version that produced it, is undefined rather than
imputed when its window is not satisfied, and is proven free of lookahead by recomputation
tests.

The technical approach is a new `server/internal/features` package modelled closely on
`marketdata`: a run/item pair for observability, per-instrument transactions with advisory
locking, an owner CLI entry point, and an incremental pass triggered by a successful import.
Values are stored long-format against a versioned definition table, in `numeric(24,12)` after
a stated rounding, which is what converts "identical values" from an unachievable promise
about irrational arithmetic into a testable one (research R-001).

The Markets list's three derived statistics move from the listing query to the engine, with
their definitions adopted verbatim as version 1 so no displayed number changes — deliberately,
so that a definition change can never be confused with a computation defect.

## Technical Context

**Language/Version**: Go 1.26 (backend), TypeScript 5 strict + Vue 3 (client)

**Primary Dependencies**: standard library, `pgx/v5`, `slog`. No new dependency. The feature
set is arithmetic over stored sessions; a numerical library would add a supply-chain surface
and a version to pin in exchange for functions this project can define precisely and test
exhaustively — and FR-001 requires each definition be stated explicitly anyway, which a
third-party implementation would put out of reach.

**Storage**: PostgreSQL 18. Five new tables — `feature_definitions`, `feature_values`,
`universe_composites`, `feature_runs`, `feature_run_items` — all shared reference data, none
carrying an owner.

**Testing**: Go tests (unit for every definition, integration against a migrated database for
computation, readback, leakage and recomputation), Vitest for the client mapping, Playwright
for the Markets regression.

**Responsive UI Verification**: No new page and no new layout. The only user-facing change is
the source of three columns the Markets table already renders. The existing Markets
responsive and accessibility journeys at 360x800, 768x1024, 1440x900 and the 320-pixel floor
must pass **unchanged** — that they need no modification is the proof this feature altered
where a number comes from and nothing else.

**Live Delivery**: REST snapshot at `GET /api/v1/instruments/{id}/features`. A completed
per-instrument recomputation publishes `feature_values.changed.v1`, shared scope, written in
the same transaction as the values it reports — not an outbox, not an after-commit hook.
Delivery rides the existing authorized stream: ordered by event id, resumable from the last
applied id, duplicate-safe. The client applies a change to the affected row only, without
disturbing filters, sort, page or scroll position.

> Note carried from this session's defect history: the stream sends **named** events. The
> client must subscribe to `feature_values.changed.v1` by name; a listener on `message`
> receives nothing, silently, and a test double that dispatches under the subscribed name
> will agree with the bug.

**Identity and Ownership**: N/A for ownership — every entity here is shared reference data
derived from shared reference data, and the feature stores no user-owned record, adds no
private scope and adds no per-user event. Access control still applies: every read requires an
active session, an unknown and an unauthorized instrument identifier are indistinguishable,
and triggering a computation is an owner action because it consumes resources and changes what
every user sees.

**PWA and Notifications**: N/A. Neither required nor changed.

**Red-Green-Refactor Proof**:

- *First failing test*: a query-layer test asserting that reading the feature set for an
  instrument as of a stored session returns a value or an explicit absence for every defined
  feature, each carrying its definition version.
- *Expected red reason*: the response contains no features and no versions, because nothing is
  computed or stored. A behavioural assertion on returned values — not a compile error, not a
  missing fixture. Per Constitution VI those would not be valid red evidence.
- *Green evidence*: the definition unit suites, the computation and readback integration
  suites, the leakage suite, the recomputation suite, and the Markets listing suite once it
  adopts the engine's values.

**Database Evolution**: **Required.** The first schema change since feature 002. Ordered
migrations `0017`–`0019` (numbering assumes no intervening merge; the next free version at
implementation time governs):

| Migration | Content |
|---|---|
| `0017_feature_definitions.sql` | definition table + the version-1 definitions as seeded reference data, including the three adopted from feature 005 verbatim |
| `0018_feature_values.sql` | values, composites, runs and run items, with the exactly-one-of-value-or-reason checks |
| `0019_markets_adopt_engine_statistics.sql` | nothing structural — reserved for any index the adopted listing query needs, kept separate so the adoption is revertible independently |

Migration tests must prove a clean install, an upgrade from the current schema with no manual
step, and that an upgrade leaves the existing Markets statistics readable until the engine's
values exist.

**Target Platform**: Linux container, `amd64` and `arm64`, one image plus PostgreSQL.

**Project Type**: Web application — Go modular monolith serving a Vue SPA.

**Performance Goals**: Full computation over the curated universe from empty within 10
minutes; incremental pass after one daily import within 30 seconds; per-instrument feature
read within the bound the Markets list already meets (research R-008).

**Constraints**: Determinism at the stored precision, independent of wall-clock time,
iteration order, machine and instrument processing order. No lookahead, proven by
recomputation rather than asserted.

**Scale/Scope**: 100 instruments, 244,116 stored bars as of 2026-09-01, ~20 definitions —
roughly 4.9M value rows, plus one composite series of ~2,600 sessions.

## Constitution Check

*GATE: evaluated before Phase 0 and re-evaluated after Phase 1 design. Both passes recorded.*

| Principle | Assessment | Verdict |
|---|---|---|
| **I. Specification-driven** | Spec reviewed, requirements independently testable, its one open question resolved in the spec itself. This plan pre-empts no later milestone: no strategy, signal, score, ranking or backtest appears in any artifact. | **Pass** |
| **II. Modular monolith** | One new package, `server/internal/features`, with a narrow interface. Computation runs in-process and stops with the application's context. No new service. | **Pass** |
| **III. Migration-only evolution** | Three ordered migrations; definitions seeded as reference data through a migration, not a console. No manual SQL in the operational path — the quickstart's queries are read-only verification. | **Pass** |
| **IV. Versioned contracts** | REST under `/api/v1`; `feature_values.changed.v1` versioned, authorization-scoped, resumable, duplicate-safe. Polling is not the live path. | **Pass** |
| **V. Correctness and reproducibility** | The whole feature. Determinism, calendars, missing data and numerical assumptions are explicit — R-001 states the numerical assumption rather than leaving it implied. | **Pass** |
| **VI. Test-driven development** | Red test and its expected failure reason stated above. Every definition gets a unit test at its boundaries before implementation. | **Pass** |
| **VII. PrimeVue-first, responsive, accessible** | No new surface. Existing Markets journeys must pass unchanged, which is a stricter bar than adding new coverage. | **Pass** |
| **VIII. Operational simplicity** | No new infrastructure, no new dependency, no new process. CLI mirrors the importer's. | **Pass** |
| **IX. Identity, ownership, isolation** | No user-owned data, so no ownership column and no per-user scope — and the plan says so explicitly rather than leaving it inferred, which is what keeps the shared/private distinction visible in contracts, logs, events and migrations. Read boundary and owner-only triggering are both covered by test. | **Pass** |
| **X. Live updates** | Event written in the same transaction as the values. Client resumes from the last applied id and tolerates duplicates. | **Pass** |

**Post-Phase-1 re-evaluation**: no gate changed verdict. The design added one constraint worth
recording — the `exactly one of value or absence_reason` check makes FR-014 structural rather
than behavioural, so a zero standing in for an absence cannot be written even by a defective
computation. That strengthens principle V at the schema level.

No violations. Complexity Tracking is therefore empty and omitted.

## Project Structure

### Documentation (this feature)

```text
specs/013-feature-engine/
├── plan.md              # This file
├── research.md          # Phase 0 — eight decisions, R-001..R-008
├── data-model.md        # Phase 1 — five entities, constraints, transactional boundaries
├── quickstart.md        # Phase 1 — operating and verifying the engine
├── contracts/
│   └── openapi.yaml     # Phase 1 — two read operations, one produced event
├── checklists/
└── tasks.md             # Phase 2 — NOT created by /speckit-plan
```

### Source Code (repository root)

```text
server/
├── cmd/market-lens/
│   └── main.go                      # `features compute` subcommand, mirroring marketdata
├── internal/
│   ├── features/                    # NEW — the engine
│   │   ├── definition.go            # definition registry, versions, parameters
│   │   ├── compute.go               # per-instrument computation over a session window
│   │   ├── window.go                # session-counted windows, gap detection
│   │   ├── composite.go             # universe composite (stage one of every run)
│   │   ├── regime.go                # classification with explicit boundaries
│   │   ├── rounding.go              # the stated half-to-even rounding (R-001)
│   │   ├── repository.go            # runs, items, values, composites; advisory locking
│   │   └── service.go               # run orchestration, incremental scoping
│   ├── db/migrations/
│   │   ├── 0017_feature_definitions.sql
│   │   ├── 0018_feature_values.sql
│   │   └── 0019_markets_adopt_engine_statistics.sql
│   ├── instruments/
│   │   └── listing.go               # CHANGED — reads engine values, derived CTE deleted
│   └── api/                         # CHANGED — two new read handlers, thin
└── ...

src/
├── services/marketData.ts           # CHANGED — subscribe to feature_values.changed.v1 by name
├── types/features.ts                # NEW — decimal strings, never numbers
└── components/finance/
    └── InstrumentTable.vue          # UNCHANGED — same three columns, same absent treatment
```

**Structure Decision**: The existing backend/frontend split is kept as-is. The engine is a new
peer package under `server/internal/`, not a subpackage of `marketdata`: it *reads* market data
and must not be able to write it, and package boundaries are how that stays true. The only
changed production files outside the new package are the listing query, two thin handlers and
the client's event subscription — which is the surface area a feature that changes where a
number comes from ought to have.

## Implementation Phasing

Follows the spec's own priorities, each stage independently valuable and independently
testable.

| Stage | Stories | Delivers |
|---|---|---|
| 1 | US1 | Definitions, storage, computation, the composite. A stored versioned feature history — a research artifact even with nothing reading it. |
| 2 | US2 | Point-in-time readback over REST. Proves the store is usable, which computation alone does not. |
| 3 | US3 | The leakage suite. Deliberately after computation because there must be something to test; deliberately before adoption because nothing should consume values not yet proven honest. |
| 4 | US4 | Incremental recomputation, definition versioning, partial-failure containment. |
| 5 | US5 | Markets adopts the engine's three statistics. Last, and only once stages 3 and 4 hold. |

## Risks

| Risk | Mitigation |
|---|---|
| Markets shows nothing for three columns between the migration and the first computation | The migration test explicitly covers an upgrade leaving existing statistics readable until engine values exist (R-006); adoption lands only once values are present |
| Cross-architecture float divergence | Stated rounding at `numeric(24,12)`, determinism asserted on stored values, not in-memory ones (R-001) |
| A leakage defect that tests agree with | The leakage test extends real history and recomputes rather than asserting over a fixture the implementation also produced — the failure mode is a test that shares the implementation's assumption, and only real extension breaks that symmetry |
| Regime thresholds chosen to fit observed data | Thresholds live in the versioned definition table with explicit boundaries, so a later change is visible as a version rather than as a silent redefinition |
| 4.9M rows makes the Markets join slow | The universe read is `(definition_id, session_date)`-indexed and touches three definitions at one session per instrument; measured against SC-007 before adoption |

## Next Step

`/speckit-tasks` to generate `tasks.md`. Nothing in this plan may be implemented before that
task list exists and its first test has been observed failing for the stated reason.
