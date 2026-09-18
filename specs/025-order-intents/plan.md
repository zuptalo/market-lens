# Implementation Plan: Order Intents

**Branch**: `025-order-intents` | **Date**: 2026-09-18 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/025-order-intents/spec.md`

## Summary

A person writes down what they are considering, and the product reports what it would do — to the
position, to the concentration, and to the limits they set themselves.

The defining decision is authorship: the product evaluates intents and never proposes one. That is
what keeps this feature on the same side of the line as every other, and it is why the vision's
strategy-driven risk engine is not what gets built. The second decision follows from it: risk
*states* a consequence rather than rejecting an intent, because rejection needs execution to reject
into and this product has none.

## Technical Context

**Language/Version**: Go 1.26 (backend), TypeScript 5 with Vue 3 (frontend)

**Primary Dependencies**: standard library `net/http`, pgx/PostgreSQL 18, PrimeVue 4. No new
dependency.

**Storage**: PostgreSQL. One ordered migration: `order_intents`, no seed data. Holdings, limits and
prices are read only.

**Testing**: Go tests including a migration test that asserts the broker-actionable columns are
*absent*, cross-user isolation on every path, consequence arithmetic against answers computed by
hand, and the three-state rule per limit; Vitest; Playwright across the three viewport projects.

**Responsive UI Verification**: 360x800 asserts the consequence and the no-advice statement are
reachable without horizontal page scrolling; 768x1024 and 1440x900 assert the same at the shared
content width; the 320-pixel floor asserts nothing clips. Inside-or-outside is carried by a word.

**Live Delivery**: one new event, `order_intents.changed.v1`, scope `user`, published in the
transaction that records, withdraws or settles an intent. A new price and a changed limit both move
a consequence and both already publish; the screen re-reads on either.

**Identity and Ownership**: private to one person, inheriting feature 022's boundary — `user_id` on
the row, read directly by every predicate, the owner role granting nothing.

**PWA and Notifications**: N/A, and FR-016 forbids a notification here.

**Red-Green-Refactor Proof**: the designated first red is `TestAnIntentReportsWhatItWouldDo`: a
person holding one instrument records an intent to buy more, with a concentration limit it would
breach; the evaluation reports the resulting position, the resulting share, the values behind it and
the limit it would put them outside. It fails on stored data — no intent can be recorded — which is
a value failure, not a compilation or setup failure.

**Database Evolution**: `0028_order_intents.sql`, with a migration test proving a clean install and
an upgrade arrive with the table present, ownership not nullable, the status vocabulary enforced, a
settled state without a date refused, and **none of `venue`, `order_type`, `time_in_force`,
`destination` or `expires_at` present**.

**Target Platform**: Linux container serving the built SPA from the Go process; PostgreSQL 18.

**Performance Goals**: one portfolio read plus one pass of the existing limit evaluator per intent.
Nothing here needs its own budget test (R-008).

**Constraints**: no intent is ever created by the product; no consequence is stored; no limit is
reported satisfied because something could not be measured; no surface says whether to act, ranks
intents, or modifies one; nothing carries a field a broker could act on.

**Scale/Scope**: one person, intents in the tens.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Assessment |
|---|---|
| **I. Specification-driven** | PASS. A reviewed specification that records two deliberate departures from the product vision, with the evidence for each, rather than quietly building something narrower. |
| **II. Modular monolith** | PASS. One new Go package reading the portfolio and risk services. No new infrastructure. |
| **III. Migration-only evolution** | PASS. One ordered migration with no seed data, whose test asserts an absence as well as a presence. |
| **IV. Versioned contracts** | PASS. Three operations and one new event type, boundary declared `user_private`. |
| **V. Correctness and reproducibility** | PASS. Every consequence is computed from stored holdings and stated limits and shown with the values behind it. Absence is stated with a reason. |
| **VI. Test-driven development** | PASS. The first red is behavioural. The consequence arithmetic is tested against answers computed by hand, because a share is easy to compute plausibly and wrongly. |
| **VII. PrimeVue-first, accessible, responsive** | PASS. Existing components; inside-or-outside carried by a word, not colour. |
| **VIII. Operational simplicity** | PASS. No command, no scheduler, no stored derived state. |
| **IX. Identity, ownership, isolation** | PASS. Ownership on the row, proven by cross-user tests on every path. |
| **X. Live updates and consented notifications** | PASS. One durable, versioned, user-scoped, resumable event. No notification. |

**Post-design re-check**: PASS. Phase 1 adds one table, one package, three operations, one event and
one route, plus one exported entry point on the risk evaluator. Nothing introduces a derived intent,
a stored consequence, an order, or a field a broker could act on.

## Project Structure

### Documentation (this feature)

```text
specs/025-order-intents/
├── plan.md              # This file
├── spec.md              # Reviewed specification; two vision departures recorded
├── research.md          # Phase 0: R-001..R-008
├── data-model.md        # Phase 1: one table, and the columns deliberately absent
├── quickstart.md        # Phase 1: write one down, read what it would do, check the arithmetic
├── contracts/
│   └── openapi.yaml     # Phase 1: three operations and order_intents.changed.v1
├── checklists/
│   └── requirements.md  # Quality checklist, including where this departs from the vision
└── tasks.md             # Phase 2 output
```

### Source Code (repository root)

```text
server/
└── internal/
    ├── db/migrations/
    │   └── 0028_order_intents.sql                # NEW: one table, no seed data
    ├── risk/
    │   └── evaluate.go                           # exported entry point for a hypothetical portfolio
    ├── intents/                                  # NEW package
    │   ├── model.go                              # intent, status, consequence
    │   ├── consequence.go                        # apply the change, then reuse the limit evaluator
    │   ├── service.go                            # record, withdraw, settle, report
    │   ├── repository.go                         # owner-scoped queries and the event
    │   └── *_test.go                             # the first red lives here
    └── api/
        ├── intents.go                            # NEW: the three operations
        └── router.go                             # register them

src/
├── router/index.ts                               # /intents route
├── views/IntentsView.vue                         # NEW: what you are considering, and what it would do
├── components/finance/
│   ├── IntentList.vue                            # NEW: each intent and its consequence
│   └── IntentForm.vue                            # NEW: write one down
├── services/marketData.ts                        # the three calls and the new event type
└── types/marketData.ts                           # intent, consequence, limit consequence

e2e/
└── order-intents.spec.ts                         # NEW: record, read, withdraw at three viewports
```

**Structure Decision**: one new package, and one small change to `risk`. The consequence is computed
by applying the proposed change to the portfolio's holdings and running feature 023's *existing*
evaluator over the result, so the limits screen and this screen can never disagree about the same
portfolio. The alternative — reimplementing "what share would this be" — is the kind of second copy
this codebase has refused throughout.

## Complexity Tracking

No constitution violations require justification. Three choices are less obvious than the
alternative, recorded so a reviewer can challenge them:

| Choice | Why | Simpler alternative rejected because |
|---|---|---|
| The person authors every intent; the product proposes none | Generating trades from a strategy the product's own backtest found losing would contradict every other refusal, in the place it costs money (R-001) | The vision describes the opposite, and it is what most people expect a feature called "order intents" to do |
| An impossible sale is recorded as an intent, though refused as a trade | A trade is a claim about the past and can be wrong; an intent is a thought, and a notebook that refuses a thought is a strange notebook (R-004) | Refusing both is more consistent on its face and stops somebody exploring a position they cannot take |
| Intents are never combined | Combining needs an order of application nobody stated, and would report a consequence nobody proposed (R-005) | Combining is what somebody holding three intents actually wants to know, and this leaves them to do it themselves |
