# Implementation Plan: Deterministic Strategies and Signals

**Branch**: `015-strategies-and-signals` | **Date**: 2026-09-02 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/015-strategies-and-signals/spec.md`

## Summary

Versioned strategies read stored feature values and record an immutable, explained view of every
instrument at every session: a score, an action, a confidence, and the per-factor contributions
that produced them. A strategy version is published by migration and superseded rather than
edited, so a signal recorded months ago stays reproducible from the version that produced it.

Two decisions shape the implementation. A score is a **weighted mean over the factors that were
available**, with contributions recorded before the division and the divisor stored alongside, so
the explanation reconciles with the score by construction rather than by inspection. And factors
are either **cross-sectional** — normalised by percentile rank against the universe for that
session — or **absolute**, which makes the computation ordered the way the feature engine's
composite already is: the universe's values for a session exist before any instrument's rank
does, and a change to one instrument moves every other instrument's score for that session.

## Technical Context

**Language/Version**: Go 1.26 (backend), TypeScript 5 with Vue 3 (frontend)

**Primary Dependencies**: standard library `net/http`, pgx/PostgreSQL 18, Vite, Vue Router,
PrimeVue 4. No new dependency.

**Storage**: PostgreSQL. Four new tables — `strategies`, `strategy_runs`, `strategy_run_items`,
`signals` — introduced by one ordered migration that also publishes the first strategy version.
Features, bars and instruments are read only.

**Testing**: Go tests including migration, determinism, leakage and budget integration tests;
Vitest; Playwright across the mobile, tablet and desktop projects.

**Responsive UI Verification**: 360x800 asserts the instrument's signal, its contributions and the
not-advice statement are reachable without horizontal page scrolling, with each contribution's
direction and magnitude present as text. 768x1024 and 1440x900 assert the same content and the
ranked view's table behaviour. The 320-pixel floor asserts nothing clips or overlaps. Keyboard and
screen-reader paths are asserted on the ranking and on the contribution list.

**Live Delivery**: One new event, `signals.changed.v1`, shared scope, published in the same
transaction as the signals it describes, carrying the instrument, session range and run rather
than the signals themselves. Ordering, `Last-Event-ID` resumption, duplicate-safe consumption and
bounded coalescing follow the existing market-data and feature events exactly.

**Identity and Ownership**: No user-owned data. Strategies and signals are shared reference data;
every authenticated user sees the same thing. Computation is an owner action at the command line
or a consequence of a feature run, never an interface action, so no new role appears. Tests prove
the reads are refused to an unauthenticated request and to a deactivated account.

**PWA and Notifications**: N/A.

**Red-Green-Refactor Proof**: The designated first red is
`TestTheSameStrategyVersionScoresAnInstrumentIdentically` in the strategy package: compute one
instrument's signal for one session, recompute, and assert every stored field including each
contribution is identical. It fails on the produced signal being absent — a behavioural failure,
not a compilation error, so the signal type and the compute entry point exist as empty seams
before the strategy computes anything.

**Database Evolution**: One ordered migration, `0021_strategies_and_signals.sql`: the four tables,
their constraints, the ranking index, and the first strategy version published as reference data.
A migration test proves a clean install and an upgrade from the current schema both arrive with
the version published, the constraints in force and no manual step.

**Target Platform**: Linux container serving the built SPA from the Go process; PostgreSQL 18.

**Project Type**: Web application — Go modular monolith with a Vue single-page client.

**Performance Goals**: Full computation over the fixture universe within the linear scaling of a
ten-minute production bound; incremental within the scaling of thirty seconds; the ranked view's
first page within the two-second bound the instrument listing already meets.

**Constraints**: A signal exists for every instrument at every stored session under every
published version. Contributions must reconcile with the score exactly. No signal may read a
feature value from a later session. No read may trigger a computation.

**Scale/Scope**: 100 instruments × 2,546 sessions ≈ 250,000 signals per strategy version, against
a feature store of 5.8 million values.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Assessment |
|---|---|
| **I. Specification-driven** | PASS. Implements a reviewed specification whose two open decisions were resolved and recorded before planning. Scope is stated by exclusion — no orders, risk, portfolio, backtest or execution — because the roadmap separates them. |
| **II. Modular monolith** | PASS. One new Go package reading the features package's store; no service, no broker, no new infrastructure. |
| **III. Migration-only evolution** | PASS. Four tables and the first strategy version arrive as migration `0021`, exercised by a migration test. A parameter change publishes a new version by forward migration; nothing is edited in place. |
| **IV. Versioned contracts** | PASS. Four REST reads under `/api/v1` and one new SSE event type, both specified in `contracts/openapi.yaml`. |
| **V. Correctness and reproducibility** | PASS. Determinism is SC-001 and the designated first red. The no-lookahead rule extends to signals: a contribution's feature session may never exceed the signal's session, asserted by test and by a quickstart query. |
| **VI. Test-driven development** | PASS. The first red is behavioural and named. Each story carries its own reds before its implementation. |
| **VII. PrimeVue-first, accessible, responsive** | PASS. Every viewport has a stated behaviour and an automated scenario, including the 320-pixel floor. Contribution direction and magnitude must be text, not colour or bar length — the accessibility requirement that most shapes this UI. |
| **VIII. Operational simplicity** | PASS. No new infrastructure. Strategy runs appear on the operations screen feature 014 built, beside import and feature runs. |
| **IX. Identity, ownership, isolation** | PASS. No user-owned data introduced; reads are authenticated and refused to deactivated accounts, with tests. |
| **X. Live updates and consented notifications** | PASS. `signals.changed.v1` is durable, versioned, authorization-scoped, resumable and transactionally coupled to the signals it describes. |

**Post-design re-check**: PASS. Phase 1 adds four tables, four reads, one event and one client
route. Nothing introduces a service, a user-owned record, a hover-dependent interaction, or a
claim that a signal is advice.

## Project Structure

### Documentation (this feature)

```text
specs/015-strategies-and-signals/
├── plan.md              # This file
├── spec.md              # Reviewed specification, both decisions resolved
├── research.md          # Phase 0: R-001..R-009
├── data-model.md        # Phase 1: strategies, runs, run items, signals
├── quickstart.md        # Phase 1: what a signal is, how to run it, how to check it
├── contracts/
│   └── openapi.yaml     # Phase 1: four reads and the signals.changed.v1 event
├── checklists/
│   └── requirements.md  # Specification quality checklist
└── tasks.md             # Phase 2 output (/speckit-tasks — not created here)
```

### Source Code (repository root)

```text
server/
├── cmd/market-lens/
│   └── main.go                              # the signals command; wire the feature-run trigger
└── internal/
    ├── db/migrations/
    │   └── 0021_strategies_and_signals.sql  # NEW: four tables plus the first version
    ├── strategies/                          # NEW package
    │   ├── model.go                         # strategy, signal, contribution, run
    │   ├── registry.go                      # published versions, their factors and bands
    │   ├── factor.go                        # normalisation: cross-sectional and absolute
    │   ├── score.go                         # weighted mean, contributions, confidence
    │   ├── service.go                       # run kinds, ordering, per-instrument containment
    │   ├── repository.go                    # reads and the transactional write with its event
    │   └── *_test.go                        # the first red lives here
    ├── features/
    │   └── service.go                       # trigger the signal pass after a successful run
    └── api/
        ├── signals.go                       # NEW: the four reads
        └── router.go                        # register them

src/
├── router/index.ts                          # /signals route
├── views/
│   ├── SignalsView.vue                      # NEW: the ranked universe
│   ├── InstrumentMarketDataView.vue         # the instrument's signal and its reasons
│   └── OperationsView.vue                   # strategy runs beside feature runs
├── components/finance/
│   ├── SignalCard.vue                       # NEW: action, score, confidence, caveat
│   ├── ContributionList.vue                 # NEW: each factor as text, not colour
│   └── StrategyRunList.vue                  # NEW: runs for the operations screen
├── services/marketData.ts                   # reads, and the new event type
└── types/marketData.ts                      # signal, contribution, strategy

e2e/
├── signals.spec.ts                          # NEW: ranking and reasons at three viewports
└── accessibility.spec.ts                    # contributions as text, keyboard path, 320 floor
```

**Structure Decision**: One new backend package, `internal/strategies`, which reads the features
package rather than reimplementing any part of it — the product vision forbids forking strategy
behaviour, and reading bars directly would fork the engine's no-lookahead guarantee. Frontend work
adds one route, one view and two components, and extends the operations screen built by 014.

## Complexity Tracking

No constitution violations require justification. Three choices are deliberately more elaborate
than an obvious alternative, recorded so a reviewer can challenge them:

| Choice | Why | Simpler alternative rejected because |
|---|---|---|
| Contributions stored as a document on the signal row, with the divisor | Keeps the point-in-time read one row and lets the explanation reconcile exactly (R-001, R-005) | A contributions table is ~1.8M rows per version against 250,000, for a query shape nothing needs yet |
| Cross-sectional factors normalised by percentile rank across the universe | "Ranking" is inherently comparative, and z-scores are unbounded so one outlier distorts every other score (R-002) | Absolute-only factors cannot express "strongest of the universe", which is what the strategy is for |
| An incremental pass recomputes every instrument over the affected sessions | A cross-sectional factor makes one instrument's change move every other instrument's rank for that session (R-007) | Recomputing only the touched instrument leaves the rest quietly wrong — the same trap the feature engine's composite already taught |
