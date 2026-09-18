# Implementation Plan: Paper Trading

**Branch**: `026-paper-trading` | **Date**: 2026-09-18 | **Spec**: [spec.md](spec.md)

## Summary

A person promotes an intent; the product fills it at the next session's open, records the fill
permanently, and keeps score. Three things shape the implementation.

**Almost nothing here is new arithmetic.** Feature 021 owns the cost model and the euro conversion,
feature 022 owns the FIFO fold and the benchmark comparison, feature 023 owns limit evaluation, and
feature 025 already extracted `risk.EvaluateAgainst` so a hypothetical portfolio could be measured
by the code that measures the real one. The work is assembling those, and the main risk is
reimplementing one of them slightly differently.

**A fill is a record, not a derivation.** Everything else user-owned in this product is derived on
read, which is what makes corrections cheap. A fill cannot be, because it is a statement about a
moment: re-deriving it would let a corrected bar silently rewrite the past. So fills are stored,
and the divergence between a fill and its bar's current value is reported instead.

**Cash is tracked, and this is the one place a return is honest.** Feature 022 declines to report
one because it never saw the deposits. Here the product recorded every movement, so FR-015 makes
this the single surface in the product that states a total return — and the reason it may.

## Technical Context

**Language/Version**: Go 1.26 (backend), TypeScript 5 with Vue 3 (frontend)

**Primary Dependencies**: standard library `net/http`, pgx/PostgreSQL 18, PrimeVue 4. No new
dependency. No chart beyond the equity curve feature 021 already built.

**Storage**: PostgreSQL. One ordered migration: `paper_accounts`, `paper_orders`, `paper_fills`.
Instruments, bars, rates, benchmarks, intents and limits are read only.

**Testing**: Go tests including a migration test asserting twelve forbidden columns are absent,
cross-user isolation on every read and write path, a reproducibility test replaying the same orders
against the same bars, a reconciliation test at twelve decimal places, and a scheduler test proving
no order exists that nobody promoted. Vitest; Playwright across the three viewport projects plus
the 320 floor, through `e2e/mobile-layout.spec.ts`.

**Responsive UI Verification**: the new destination joins `ROUTES` in the mobile-layout guard, so
page-level sideways scrolling and cramped tables are caught at 320 and 390 without a bespoke test.
Every figure is text; gain and loss carry a sign and a word, never colour alone.

**Live Delivery**: one new event, `paper_account.changed.v1`, **scope `user`** with
`subject_user_id`, published in the transaction that promotes, cancels or fills. The fill pass
publishes one per affected account, not one per fill.

**Identity and Ownership**: `user_id` denormalised onto orders and fills, held equal to the
account's owner by composite foreign keys — feature 022's pattern, for feature 022's reason. Service
methods take the caller's identifier as the first argument, so a scoping mistake is a compile error.

**PWA and Notifications**: N/A. A filled order is a plausible future notification and is not one now.

**Red-Green-Refactor Proof**: the designated first red is
`TestAPromotedIntentFillsAtTheNextSessionOpen`: promote an intent in a session whose next stored
session has a known open, run the fill pass, and read the fill price, costs and cash effect. It
fails on a value — there is no fill — rather than on compilation, because the package and its types
are written in the same step as the test and the service simply returns nothing yet.

**Database Evolution**: `0029_paper_trading.sql`, with a migration test proving a clean install and
an upgrade both arrive with three tables, `user_id` not nullable on all three, the one-account-per-
person index, the immutability trigger refusing a changed starting balance, the
`fill_session > placed_session` trigger refusing a same-session fill, and the twelve forbidden
columns absent.

**Target Platform**: Linux container serving the built SPA from the Go process; PostgreSQL 18.

**Performance Goals**: a pass over a thousand pending orders across a hundred instruments completes
within a stated budget — enough to catch a per-order query, not to police milliseconds.

**Constraints**: no fill at a session on or before the placement session, enforced in the database;
no stored derived figure; no order created by anything but a promotion; no paper figure combined
with a real holding; no figure framed as a reason to act.

**Scale/Scope**: one account per person, ~100 instruments, orders in the hundreds.

## Constitution Check

| Principle | Assessment |
|---|---|
| **I. Specification-driven** | PASS. Implements a reviewed specification whose three open decisions were put to the owner and resolved before planning. Six exclusions, each written so its absence is testable. |
| **II. Modular monolith** | PASS. One new Go package reading the instrument, price, series, intent and risk stores. No service, no new infrastructure. The fill pass runs in process, after the import, like the feature engine. |
| **III. Migration-only evolution** | PASS. One ordered migration with two triggers, exercised by a test. No seed data: an account exists when somebody opens one. |
| **IV. Versioned contracts** | PASS. Five operations and one new event type, declared `user_private` rather than `authenticated`. |
| **V. Correctness and reproducibility** | PASS. Replaying the same orders against the same bars produces identical fills (SC-002). Absence is stated with a reason in all four cases it can arise. A corrected bar is reported as a divergence rather than silently re-priced. |
| **VI. Test-driven development** | PASS. The first red is behavioural and is the property the feature exists to establish: a fill at a price that did not exist when the order was placed. |
| **VII. PrimeVue-first, accessible, responsive** | PASS. Existing components and feature 021's equity curve. The new destination joins the mobile-layout guard, so 320 and 390 are checked by the same test as every other screen. |
| **VIII. Operational simplicity** | PASS. No new process and no new schedule: the pass hangs off the import that already runs. |
| **IX. Identity, ownership, isolation** | PASS. Ownership explicit on all three tables, enforced in services and queries, proved by cross-user tests on every path including the fill pass — which must fill one person's orders without touching another's. |
| **X. Live updates and consented notifications** | PASS. `paper_account.changed.v1` is durable, versioned, user-scoped, resumable and transactionally coupled. |

**Post-design re-check**: PASS. Phase 1 adds three tables, one package, five operations, one event,
one route and one hook on an existing scheduler. Nothing introduces a stored derived figure, a
broker field, a short position, or a second implementation of arithmetic that already exists.

## Project Structure

```text
specs/026-paper-trading/
├── plan.md              # This file
├── spec.md              # Reviewed specification, three decisions resolved by the owner
├── research.md          # Phase 0: R-001..R-009
├── data-model.md        # Phase 1: three tables, two triggers, twelve absent columns
├── quickstart.md        # Phase 1: open it, promote an intent, check it is not lying
├── contracts/
│   └── openapi.yaml     # Phase 1: five operations and paper_account.changed.v1
├── checklists/
│   └── requirements.md  # Specification quality checklist
└── tasks.md             # Phase 2 output
```

```text
server/
└── internal/
    ├── db/migrations/
    │   └── 0029_paper_trading.sql               # NEW: three tables, two triggers
    ├── paper/                                   # NEW package
    │   ├── model.go                             # account, order, fill, position, absence
    │   ├── fill.go                              # the pass: next open, costs, cash, refusals
    │   ├── account.go                           # the fold: positions, cash, value, return
    │   ├── service.go                           # open, promote, cancel, read
    │   ├── repository.go                        # owner-scoped queries and the event
    │   └── *_test.go                            # the first red lives here
    └── api/
        ├── paper.go                             # NEW: the five operations
        └── router.go                            # register them

src/
├── router/index.ts                              # /paper route
├── views/PaperAccountView.vue                   # NEW: the account, its orders, its score
├── components/finance/
│   ├── PaperOrderList.vue                       # NEW: orders, states, fills, reasons
│   └── PaperAccountSummary.vue                  # NEW: cash, value, return, and the caveat
├── services/marketData.ts                       # the five calls and the new event type
└── types/marketData.ts                          # account, order, fill, holding

e2e/
├── fixtures/api.mjs                             # the new endpoints, and /paper in ROUTES
└── paper-trading.spec.ts                        # NEW: promote, fill, refuse, at three viewports
```

**Structure Decision**: one new package. `paper` reuses `internal/decimal`, feature 021's cost
model, feature 022's FIFO fold and comparison, and `risk.EvaluateAgainst`. It does not reuse the
`portfolio` package's types: a paper position belongs to a simulation and a holding belongs to a
person, and a shared struct would be one refactor away from a screen adding them together — which
FR-020 forbids.

## Complexity Tracking

No constitution violations require justification. Four choices are less obvious than the
alternative, recorded so a reviewer can challenge them:

| Choice | Why | Simpler alternative rejected because |
|---|---|---|
| Fills are stored, not derived | A fill is a statement about a moment; re-deriving it lets a corrected bar rewrite the past (R-005) | Deriving matches every other user-owned figure here, and would make the account unreconcilable week to week |
| Cash is tracked, unlike feature 022 | The product recorded every movement, so the return is exact rather than invented (R-004) | Not tracking it keeps the two features symmetrical, and an account that cannot run out of money measures nothing |
| `fill_session > placed_session` enforced by trigger | A check constraint cannot subquery, and this is the constraint that makes lookahead impossible rather than unlikely | Enforcing it in the service is readable, and a lookahead bug produces a flattering result that looks plausible |
| The account's terms are immutable | A record that can be tuned after the result is known is not a record (R-006) | Allowing a reset is friendlier, and turns a track record into a collection of attempts |
