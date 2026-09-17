# Implementation Plan: Personal Portfolio and Holdings

**Branch**: `022-personal-portfolio` | **Date**: 2026-09-17 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/022-personal-portfolio/spec.md`

## Summary

A person records what they actually hold; the product derives what it is worth, what it cost, and
what has been made or lost — and never stores any of those as a number somebody could edit.

Three things shape the implementation. **Ownership is inherited, not invented**: feature 004 built
`ScopeUser`, `PrivateScopeFor` and user-scoped events, so the work here is making every query use
them and proving it, rather than designing a boundary. **Everything is derived**, which makes a
correction a re-read instead of a repair — the reason the correction story is affordable at all.
And **cash is deliberately absent**, so the product reports no portfolio return and says why, which
narrows the benchmark comparison to per-holding and makes that narrowing a requirement rather than
an omission.

## Technical Context

**Language/Version**: Go 1.26 (backend), TypeScript 5 with Vue 3 (frontend)

**Primary Dependencies**: standard library `net/http`, pgx/PostgreSQL 18, PrimeVue 4. No new
dependency, and no chart — a portfolio is a table of figures.

**Storage**: PostgreSQL. One ordered migration: `portfolios` and `portfolio_trades`. Instruments,
bars, signals, benchmarks and rates are read only.

**Testing**: Go tests including a migration test, cross-user isolation tests on every read and
write path, a FIFO property test, an accounting-reconciliation test and a budget test; Vitest;
Playwright across the three viewport projects.

**Responsive UI Verification**: 360x800 asserts the total, the per-holding comparison, the
no-advice statement and the action to record a trade are reachable without horizontal page
scrolling, with holdings as stacked cards. 768x1024 and 1440x900 assert the same at the shared
content width with holdings tabular. The 320-pixel floor asserts nothing clips. Profit and loss
carry a sign and a word, never colour alone.

**Live Delivery**: one new event, `portfolio.changed.v1`, **scope `user`** with `subject_user_id`
set to the owner, published in the transaction that records, corrects or withdraws a trade. The
replay query already filters it; this feature is its first domain use.

**Identity and Ownership**: the first user-owned domain record. `user_id` is denormalised onto
`portfolio_trades` so every predicate reads it directly, held equal to the portfolio's owner by a
composite foreign key. Reads and writes are refused to an unauthenticated and to a deactivated
caller, and the owner role grants no access to another person's portfolio.

**PWA and Notifications**: N/A.

**Red-Green-Refactor Proof**: the designated first red is `TestAPersonSeesOnlyTheirOwnHoldings`:
two people each record a trade; each reads the portfolio, the totals and the event stream, and
observes only their own. It fails on stored data — no portfolio exists, so a person who has just
recorded a trade reads nothing back — which is a value failure, not a compilation or setup failure.

**Database Evolution**: `0026_personal_portfolios.sql`, with a migration test proving a clean
install and an upgrade arrive with both tables, `user_id` not nullable on either, the
owner-matching constraint refusing a mismatched pair, a future-dated trade refused, and no manual
step.

**Target Platform**: Linux container serving the built SPA from the Go process; PostgreSQL 18.

**Performance Goals**: a hundred holdings and a thousand recorded trades read within a stated
budget — enough to catch a per-trade query, not to police milliseconds.

**Constraints**: no figure is stored that could drift from the trades behind it; no holding is
valued at a price from a session its instrument did not trade; no conversion reads a rate from
another session; history is never silently rewritten; no portfolio return is ever reported.

**Scale/Scope**: one portfolio per person, ~100 instruments available, trades in the hundreds.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Assessment |
|---|---|
| **I. Specification-driven** | PASS. Implements a reviewed specification whose three open decisions were put to the owner and resolved before planning. Scope is bounded by exclusion in FR-019 to FR-022, each written so its absence is testable. |
| **II. Modular monolith** | PASS. One new Go package reading the instrument, price, signal and series stores. No service, no new infrastructure. |
| **III. Migration-only evolution** | PASS. One ordered migration, exercised by a test. No seed data: a portfolio is created when somebody first records a trade. |
| **IV. Versioned contracts** | PASS. Six operations and one new event type, specified in `contracts/openapi.yaml`, with the access boundary declared as `user_private` rather than `authenticated`. |
| **V. Correctness and reproducibility** | PASS. Every figure is a fold over the person's own trades plus stored prices, so recomputation is the only way it is ever produced. Absence is stated with a reason in all four cases it can arise. |
| **VI. Test-driven development** | PASS. The first red is behavioural and is the isolation property the whole feature exists to establish correctly. FIFO gets a property test rather than one worked example. |
| **VII. PrimeVue-first, accessible, responsive** | PASS. Existing components; no new chart. Gain and loss carry a sign and a word, because colour alone is exactly what a colour-blind reader loses here. |
| **VIII. Operational simplicity** | PASS. No command, no scheduler change, no new process. Nothing to operate. |
| **IX. Identity, ownership, isolation** | PASS — and this is the principle the feature exists to satisfy. Ownership is explicit on both tables, enforced in services and queries, and proved by cross-user tests on every read, write, correction, withdrawal, total and event. |
| **X. Live updates and consented notifications** | PASS. `portfolio.changed.v1` is durable, versioned, user-scoped, resumable and transactionally coupled. A second person connected throughout receives nothing. |

**Post-design re-check**: PASS. Phase 1 adds two tables, one package, six operations, one event and
one route. Nothing introduces a stored derived figure, an order, a risk rule, a tax claim, or a
second implementation of feature 021's arithmetic.

## Project Structure

### Documentation (this feature)

```text
specs/022-personal-portfolio/
├── plan.md              # This file
├── spec.md              # Reviewed specification, three decisions resolved by the owner
├── research.md          # Phase 0: R-001..R-009
├── data-model.md        # Phase 1: two tables, and everything that is deliberately not one
├── quickstart.md        # Phase 1: record a trade, read it, and check it is not lying
├── contracts/
│   └── openapi.yaml     # Phase 1: six operations and portfolio.changed.v1
├── checklists/
│   └── requirements.md  # Specification quality checklist
└── tasks.md             # Phase 2 output (/speckit-tasks — not created here)
```

### Source Code (repository root)

```text
server/
└── internal/
    ├── db/migrations/
    │   └── 0026_personal_portfolios.sql          # NEW: the two tables and their constraints
    ├── portfolio/                                # NEW package
    │   ├── model.go                              # portfolio, trade, holding, realised result
    │   ├── fifo.go                               # the fold: position, cost, realised
    │   ├── valuation.go                          # latest close, the euro cross, absence
    │   ├── comparison.go                         # holding against its market, over its window
    │   ├── service.go                            # record, correct, withdraw, read
    │   ├── repository.go                         # owner-scoped queries and the event
    │   └── *_test.go                             # the first red lives here
    └── api/
        ├── portfolio.go                          # NEW: the six operations
        └── router.go                             # register them

src/
├── router/index.ts                               # /portfolio route
├── views/PortfolioView.vue                       # NEW: holdings, totals, and the statement
├── components/finance/
│   ├── HoldingTable.vue                          # NEW: what is held, worth, cost, profit
│   ├── TradeEntryForm.vue                        # NEW: record and correct
│   └── TradeHistory.vue                          # NEW: every entry, including superseded
├── services/marketData.ts                        # the six calls and the new event type
└── types/marketData.ts                           # portfolio, holding, trade, realised result

e2e/
└── portfolio.spec.ts                             # NEW: record, read and correct at three viewports
```

**Structure Decision**: one new package. `portfolio` reads the instrument, price, signal and series
stores and reimplements none of them — in particular it reuses feature 021's conversion rule and
rounding discipline rather than forking a second copy that would eventually disagree. It does not
reuse the `backtest` package's types: a simulated position belongs to one run and a holding belongs
to a person, and sharing a struct between them would couple two things that only look alike.

## Complexity Tracking

No constitution violations require justification. Three choices are less obvious than the
alternative, recorded so a reviewer can challenge them:

| Choice | Why | Simpler alternative rejected because |
|---|---|---|
| `user_id` denormalised onto every trade | Every ownership predicate reads it directly, so a query cannot scope by portfolio alone and a forgotten join cannot widen access (R-001) | Joining through `portfolios` is normalised and correct, and one missing join is a data leak rather than a bug |
| FIFO derived on read, never materialised | A correction becomes a re-read instead of a repair, which is what makes the correction story affordable (R-002) | Stored lots are faster and are a second source of truth every correction must unwind |
| Each holding valued at its own latest session | A portfolio-wide session makes Helsinki unvalued whenever Stockholm traded later — the union-calendar problem feature 021 already solved this way (R-005) | One session per portfolio is simpler and silently reports a multi-market portfolio as incomplete most days |
