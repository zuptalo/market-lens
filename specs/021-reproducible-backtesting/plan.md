# Implementation Plan: Reproducible Backtesting

**Branch**: `021-reproducible-backtesting` | **Date**: 2026-09-16 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/021-reproducible-backtesting/spec.md`

## Summary

A backtest replays stored signals over stored sessions under a stated, immutable configuration and
records what would have happened: trades that name the signal behind them, cash and positions at
every session, an equity curve, and six measures beside the benchmark's over the identical range.

Three decisions shape the implementation. Execution happens at the **open of the next session the
instrument actually traded**, because a signal computed from a close cannot honestly be acted on
at that close. Benchmarks and rates are **their own series**, not instruments, because an index
stored as an instrument would appear on Markets, be computed over by the feature engine, and be
scored by the strategy it is meant to judge. And the simulation **reads stored data only** — no
provider call is on the path — which is what makes a recomputation reproducible at all.

## Technical Context

**Language/Version**: Go 1.26 (backend), TypeScript 5 with Vue 3 (frontend)

**Primary Dependencies**: standard library `net/http`, pgx/PostgreSQL 18, PrimeVue 4,
lightweight-charts for the curve, as the instrument chart already uses. No new dependency.

**Storage**: PostgreSQL. Two ordered migrations: series (benchmarks and rates), then the
backtest tables and the first published configuration. Bars, features and signals are read only.

**Testing**: Go tests including migration, determinism, no-lookahead, accounting-identity and
budget integration tests; Vitest; Playwright across the three viewport projects.

**Responsive UI Verification**: 360x800 asserts the measures, the benchmark comparison and the
not-a-prediction statement are reachable without horizontal page scrolling, with the curve reduced
to its figures. 768x1024 and 1440x900 assert the same at the shared content width with the curve
drawn and its figures still present as text. The 320-pixel floor asserts nothing clips.

**Live Delivery**: one new event, `backtest.completed.v1`, shared scope, published in the
transaction that commits the run, carrying the run and configuration rather than the result.

**Identity and Ownership**: no user-owned data — Milestone 6 owns that. Reads are authenticated
and refused to a deactivated account. Running is an owner command, never an interface action.

**PWA and Notifications**: N/A.

**Red-Green-Refactor Proof**: the designated first red is
`TestTheSameConfigurationProducesTheIdenticalResult`: run a backtest over the fixture, snapshot
every trade, equity point and measure, recompute, and diff. It fails on stored data — no result is
produced — which is a value failure, not a compilation or setup failure.

**Database Evolution**: `0024_market_series.sql` and `0025_backtests.sql`, each with a migration
test proving a clean install and an upgrade arrive with the tables, the constraints in force, the
first configuration published, and no manual step. The two constraints that carry the feature's
honesty are in the schema rather than in code: a trade's execution session is strictly later than
its signal session, and an equity point carries a total or an absence reason, never both.

**Target Platform**: Linux container serving the built SPA from the Go process; PostgreSQL 18.

**Performance Goals**: a full backtest over the curated universe and its stored history within a
stated budget, scaled from the strategy layer's the same way.

**Constraints**: no value may come from a session later than the one being simulated; no position
may be valued at a price from a session its instrument did not trade; recomputation must be
identical field for field; cash plus positions must equal equity at every session.

**Scale/Scope**: 100 instruments × ~2,546 sessions of signals read; one equity point per session;
positions only for what is held; trades in the hundreds.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Assessment |
|---|---|
| **I. Specification-driven** | PASS. Implements a reviewed specification whose seven decisions were resolved before planning, against provider coverage probed through the product's own commands rather than assumed. Scope is stated by exclusion, and four requirements make the absence of behaviour testable. |
| **II. Modular monolith** | PASS. One new Go package reading the strategy and market-data stores; no service, no broker, no new infrastructure. |
| **III. Migration-only evolution** | PASS. Two ordered migrations, each exercised by a test. A configuration change publishes a new version by forward migration; nothing is edited in place. |
| **IV. Versioned contracts** | PASS. Four authenticated reads and one new event type, specified in `contracts/openapi.yaml`. |
| **V. Correctness and reproducibility** | PASS. Determinism is SC-001 and the designated first red. The no-lookahead rule extends to execution: a trade may not occur on the session whose close produced its signal, asserted by schema constraint, by test, and by a quickstart query. |
| **VI. Test-driven development** | PASS. The first red is behavioural and named. Each story carries its own reds, and the accounting identity is asserted at every session rather than only at the end. |
| **VII. PrimeVue-first, accessible, responsive** | PASS. Every figure the curve conveys is available as text — the rule the instrument chart already follows — and no direction is carried by colour alone. |
| **VIII. Operational simplicity** | PASS. Two owner commands, no new process, no scheduler change. Series are imported deliberately because a backtest reads stored data. |
| **IX. Identity, ownership, isolation** | PASS. No user-owned record introduced; reads authenticated and refused to a deactivated account, with tests. |
| **X. Live updates and consented notifications** | PASS. `backtest.completed.v1` is durable, versioned, authorization-scoped, resumable and transactionally coupled to the run it reports. |

**Post-design re-check**: PASS. Phase 1 adds nine tables, two commands, four reads, one event and
one route. Nothing introduces a service, a user-owned record, a hover-dependent interaction, an
order, or a fitted parameter.

## Project Structure

### Documentation (this feature)

```text
specs/021-reproducible-backtesting/
├── plan.md              # This file
├── spec.md              # Reviewed specification, seven decisions resolved
├── research.md          # Phase 0: R-001..R-009
├── data-model.md        # Phase 1: series, configuration, and the record of a simulation
├── quickstart.md        # Phase 1: run one, check it, and read it honestly
├── contracts/
│   └── openapi.yaml     # Phase 1: four reads and backtest.completed.v1
├── checklists/
│   └── requirements.md  # Specification quality checklist
└── tasks.md             # Phase 2 output (/speckit-tasks — not created here)
```

### Source Code (repository root)

```text
server/
├── cmd/market-lens/
│   └── main.go                                   # series import and backtest run commands
└── internal/
    ├── db/migrations/
    │   ├── 0024_market_series.sql                # NEW: benchmarks and rates
    │   └── 0025_backtests.sql                    # NEW: the simulation, and the first configuration
    ├── series/                                   # NEW package: stored benchmark and rate series
    │   ├── model.go
    │   ├── repository.go
    │   └── service.go                            # import through the existing provider client
    ├── backtest/                                 # NEW package
    │   ├── model.go                              # configuration, trade, position, equity, measures
    │   ├── portfolio.go                          # cash, positions, and the accounting identity
    │   ├── execution.go                          # next traded session, price, costs, conversion
    │   ├── measures.go                           # the six figures, and the benchmark's
    │   ├── service.go                            # the replay, and per-instrument containment
    │   ├── repository.go                         # the transactional write with its event
    │   └── *_test.go                             # the first red lives here
    └── api/
        ├── backtests.go                          # NEW: the four reads
        └── router.go                             # register them

src/
├── router/index.ts                               # /backtests route
├── views/BacktestsView.vue                       # NEW: results, measures, benchmark, trades
├── components/finance/
│   ├── EquityCurve.vue                           # NEW: the curve, and its figures as text
│   ├── MeasureTable.vue                          # NEW: six measures beside the benchmark's
│   └── TradeList.vue                             # NEW: each trade, and the way to its signal
├── services/marketData.ts                        # the reads and the new event type
└── types/marketData.ts                           # backtest, trade, equity point, measures

e2e/
└── backtests.spec.ts                             # NEW: the result at three viewports
```

**Structure Decision**: two new packages. `series` exists because a benchmark is not an instrument
and putting it in `marketdata` would invite exactly the merge the research rejects. `backtest`
reads the strategy layer rather than reimplementing any part of it — the product's position is
that strategy behaviour has one implementation, and a simulation that recomputed signals would
fork it.

## Complexity Tracking

No constitution violations require justification. Three choices are more elaborate than the
obvious alternative, recorded so a reviewer can challenge them:

| Choice | Why | Simpler alternative rejected because |
|---|---|---|
| Benchmarks and rates as their own tables | An index stored as an instrument appears on Markets, is computed over by the feature engine, and is scored by the strategy it is meant to judge (R-002) | Instruments on a synthetic exchange need excluding from every existing read — the exclusions are the tell |
| Execution at the next traded session's open, with a stated give-up window | A signal from a close cannot be acted on at that close, and an instrument that stops trading must produce a stated absence rather than a fill (R-001, FR-006) | Filling at the signal session's close is one line and flatters every result this product will ever produce |
| Every skipped signal gets a row and a reason | FR-015 becomes true by construction, and "the strategy said buy and nothing happened" is answerable (R-006) | Counting skips in aggregate loses which instrument, which session, and therefore the answer |
