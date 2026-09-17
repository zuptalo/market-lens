# Implementation Plan: Personal Risk Limits

**Branch**: `023-personal-risk-limits` | **Date**: 2026-09-17 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/023-personal-risk-limits/spec.md`

## Summary

A person writes down the rules they want to be held to, and the product reports where they stand
against each one with the arithmetic that produced the figure.

The whole feature turns on one distinction: "you should hold no more than 25% in one company" is
advice, and "you said 25%, you are at 41%, here is the sum" is somebody's own rule applied. That is
why there are no default limits, no suggested thresholds, no statement of what would close a gap,
and no softening because a strategy likes the position.

Three things shape the implementation. It **measures against feature 022's valuation** rather than
deriving one, so every honesty rule that feature established applies here for free. Nothing about an
evaluation is **stored**, so a changed limit changes every figure at once and no second copy can
drift. And the state is **one of three**, never defaulted — the failure that matters most is
reporting compliance because something could not be measured.

## Technical Context

**Language/Version**: Go 1.26 (backend), TypeScript 5 with Vue 3 (frontend)

**Primary Dependencies**: standard library `net/http`, pgx/PostgreSQL 18, PrimeVue 4. No new
dependency and no chart — a limit is a figure beside a threshold.

**Storage**: PostgreSQL. One ordered migration: `risk_limits`, with no seed data. Holdings, prices,
sectors and exchanges are read only.

**Testing**: Go tests including a migration test, cross-user isolation on every path, an
unevaluable-state test per kind, an arithmetic test whose answers are computed by hand, and a budget
test; Vitest; Playwright across the three viewport projects.

**Responsive UI Verification**: 360x800 asserts each limit's state, its arithmetic and the
no-advice statement are reachable without horizontal page scrolling, with limits as stacked cards.
768x1024 and 1440x900 assert the same at the shared content width with limits tabular. The
320-pixel floor asserts nothing clips. The three states are distinguished by a word, never by
colour alone.

**Live Delivery**: one new event, `risk_limits.changed.v1`, **scope `user`**, published in the
transaction that states, changes or removes a limit. A recorded trade and a new stored price both
change where a person stands and already publish their own events; the screen re-reads on either
rather than publishing a third that says the same thing.

**Identity and Ownership**: private to one person, inheriting feature 022's boundary — `user_id` on
the row, read directly by every predicate, with the owner role granting no access to anybody else's.

**PWA and Notifications**: N/A, and deliberately so. A breach is a good reason to want a
notification and FR-020 forbids one here: consented delivery is its own backlog item with its own
consent, quiet-hours and per-device revocation requirements.

**Red-Green-Refactor Proof**: the designated first red is
`TestABreachIsReportedWithTheArithmeticBehindIt`: a person holds two instruments in a known
proportion and states a concentration limit one of them exceeds; the evaluation reports it exceeded
with the holding's value, the portfolio total, the percentage and the threshold. It fails on stored
data — no limit can be stated, so nothing is evaluated — which is a value failure, not a compilation
or setup failure.

**Database Evolution**: `0027_risk_limits.sql`, with a migration test proving a clean install and an
upgrade arrive with the table present, `user_id` not nullable, one limit per kind enforced, a share
above one refused, a non-positive threshold refused, a fractional holding count refused, and no
manual step.

**Target Platform**: Linux container serving the built SPA from the Go process; PostgreSQL 18.

**Performance Goals**: one evaluation is one portfolio read plus a group-by over its holdings. The
budget test exists to catch a per-holding query, not to police milliseconds.

**Constraints**: no default limit is ever published; no evaluation is stored; no limit is reported
within because something could not be measured; no surface says what would close a gap; no strategy
view changes an evaluation; no stored backtest result changes.

**Scale/Scope**: one person, at most four limits, up to a hundred holdings.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Assessment |
|---|---|
| **I. Specification-driven** | PASS. Implements a reviewed specification whose three open decisions were put to the owner and resolved before planning. Scope is bounded by exclusion in FR-014 to FR-021, each written so its absence is testable. |
| **II. Modular monolith** | PASS. One new Go package reading the portfolio service. No new infrastructure, no scheduler, no job. |
| **III. Migration-only evolution** | PASS. One ordered migration with no seed data, exercised by a test. The absence of seed data is itself a requirement (FR-002). |
| **IV. Versioned contracts** | PASS. Three operations and one new event type, with the access boundary declared as `user_private`. |
| **V. Correctness and reproducibility** | PASS. Every figure is computed from the person's own holdings at read time and shown with the values behind it, so any reported percentage can be reproduced by the reader. Absence is stated with a reason in both cases it can arise. |
| **VI. Test-driven development** | PASS. The first red is behavioural. The three-state rule gets a test per kind rather than one example, because "reported within by default" is the failure that shows least. |
| **VII. PrimeVue-first, accessible, responsive** | PASS. Existing components, no new chart. The three states carry a word, because somebody who cannot tell red from green must not have to guess whether they are inside their own rule. |
| **VIII. Operational simplicity** | PASS. Nothing to operate: no command, no scheduler change, no stored state to reconcile. |
| **IX. Identity, ownership, isolation** | PASS. Ownership on the row, enforced in services and queries, proved by cross-user tests on every read, write, removal, evaluation and event. |
| **X. Live updates and consented notifications** | PASS. `risk_limits.changed.v1` is durable, versioned, user-scoped, resumable and transactionally coupled. No notification is sent, which FR-020 requires. |

**Post-design re-check**: PASS. Phase 1 adds one table, one package, three operations, one event and
one route, plus three additive fields on an existing response. Nothing introduces a stored
evaluation, a default limit, a suggested threshold, an order, or a notification.

## Project Structure

### Documentation (this feature)

```text
specs/023-personal-risk-limits/
├── plan.md              # This file
├── spec.md              # Reviewed specification, three decisions resolved by the owner
├── research.md          # Phase 0: R-001..R-010
├── data-model.md        # Phase 1: one table, and everything deliberately not stored
├── quickstart.md        # Phase 1: state a limit, read it, and check the arithmetic
├── contracts/
│   └── openapi.yaml     # Phase 1: three operations and risk_limits.changed.v1
├── checklists/
│   └── requirements.md  # Specification quality checklist
└── tasks.md             # Phase 2 output (/speckit-tasks — not created here)
```

### Source Code (repository root)

```text
server/
└── internal/
    ├── db/migrations/
    │   └── 0027_risk_limits.sql                  # NEW: one table, no seed data
    ├── portfolio/
    │   ├── model.go                              # Holding gains Sector, SectorName, MIC
    │   └── repository.go                         # the existing join reads them
    ├── risk/                                     # NEW package
    │   ├── model.go                              # limit, evaluation, state, contribution
    │   ├── evaluate.go                           # the four kinds, and the three states
    │   ├── service.go                            # state, change, remove, report
    │   ├── repository.go                         # owner-scoped queries and the event
    │   └── *_test.go                             # the first red lives here
    └── api/
        ├── risk.go                               # NEW: the three operations
        └── router.go                             # register them

src/
├── router/index.ts                               # /risk route
├── views/RiskLimitsView.vue                      # NEW: the limits, their states, the arithmetic
├── components/finance/
│   ├── LimitTable.vue                            # NEW: state, figure, threshold, breakdown
│   └── LimitForm.vue                             # NEW: state or change one limit
├── views/PortfolioView.vue                       # a line saying how many limits are exceeded
├── services/marketData.ts                        # the three calls and the new event type
└── types/marketData.ts                           # limit, evaluation, contribution

e2e/
└── risk-limits.spec.ts                           # NEW: state, read and breach at three viewports
```

**Structure Decision**: one new package. `risk` calls `portfolio.Service.View` and measures what it
returns; it reads no price, performs no valuation and holds no copy of a holding. That is what keeps
one implementation of "what is this worth" in the product, and it means the unvaluable-holding rule
arrives here already correct rather than being reimplemented and quietly weakened.

## Complexity Tracking

No constitution violations require justification. Three choices are less obvious than the
alternative, recorded so a reviewer can challenge them:

| Choice | Why | Simpler alternative rejected because |
|---|---|---|
| The whole breakdown is returned, not the largest contributor | A reader sees where their money is rather than being pointed at one holding, which is the first step toward telling them what to sell (FR-015) | Returning only the offender is smaller and is a recommendation wearing a data structure |
| Allocation limits go unevaluable when one holding cannot be priced | The denominator is a total feature 022 itself refuses to state; dividing by it anyway would be arithmetic on a number the product says it does not have (R-005) | Measuring against the holdings that *could* be valued produces a plausible percentage that is wrong in the safe-looking direction |
| `Holding` gains sector and market rather than risk querying for them | One join already fetches ticker and name from the same rows, and two sources would be two places to get `unclassified` wrong (R-002) | A separate lookup in the risk package keeps feature 022 untouched, at the cost of fetching the same data twice |
