# Tasks: Order Intents

**Feature**: 025 | **Branch**: `025-order-intents` | **Plan**: [plan.md](plan.md)

Tests come first throughout. Every production task below names the test that must be observed
failing — on a value, not on a compilation error — before the code that satisfies it is written.

## Phase 1: Setup

- [x] T001 Write the reviewed specification in `specs/025-order-intents/spec.md`, with the three
      absences (no venue, no order type, no destination) stated as testable requirements
- [x] T002 Record the quality checklist in `specs/025-order-intents/checklists/requirements.md`
- [x] T003 Resolve the open questions in `specs/025-order-intents/research.md` (R-001..R-008)
- [x] T004 Describe the one table and everything deliberately absent from it in
      `specs/025-order-intents/data-model.md`
- [x] T005 Specify three operations and one event in `specs/025-order-intents/contracts/openapi.yaml`
- [x] T006 Write `specs/025-order-intents/plan.md` with the constitution gate

## Phase 2: Foundational

- [x] T007 Write `server/internal/db/order_intents_migration_test.go`, asserting a clean install and
      an upgrade both arrive with the table, and that the nine forbidden columns are **absent**
- [x] T008 Add `server/internal/db/migrations/0028_order_intents.sql` — one table, the
      settled-exactly-when-not-considering check, positive quantity and price, no seed data
- [x] T009 [P] Add `risk.EvaluateAgainst(limits, view)` in `server/internal/risk/evaluate.go` so a
      hypothetical portfolio can be evaluated by the same code that evaluates the real one
- [x] T010 [P] Add `risk.Service.Limits` in `server/internal/risk/service.go`
- [x] T011 [P] Add `portfolio.Service.ValueOf` and `portfolio.Repository.InstrumentCurrency`, so an
      intent in something not yet held can still be priced

## Phase 3: US1 — what an intent would do

**Goal**: a person writes down what they are considering and reads the consequence.
**Independently testable**: record one intent, read the report, check the resulting position, its
value, its share of the portfolio, and what each stated limit would then say.

- [x] T012 [US1] `TestAnIntentReportsWhatItWouldDo` in
      `server/internal/intents/consequence_integration_test.go` — the designated first red
- [x] T013 [US1] `server/internal/intents/model.go`, `decimal.go` — the intent, the consequence, and
      the two absence reasons
- [x] T014 [US1] `server/internal/intents/consequence.go` — the fold, reusing the risk evaluator
      rather than reimplementing it
- [x] T015 [US1] `server/internal/intents/service.go`, `repository.go` — record, settle, report, and
      the user-scoped event in the same transaction
- [x] T016 [P] [US1] `TestAnIntentInSomethingNotYetHeldIsStillPriced`
- [x] T017 [P] [US1] `TestSellingMoreThanIsHeldIsReportedNotRefused` — reported, while the same
      trade recorded in the portfolio stays refused
- [x] T018 [P] [US1] `TestAnUnpriceablePortfolioMakesTheShareUnevaluable`
- [x] T019 [P] [US1] `TestIntentsAreNotCombined` — each measured against the portfolio as it stands

## Phase 4: US2 — it is yours, and it is not an order

**Goal**: an intent is private to the person, and carries nothing that could be transmitted.
**Independently testable**: two people each record one; each reads only their own, and neither can
settle the other's. The encoded intent contains no field a broker could act on.

- [x] T020 [US2] `TestAPersonSeesOnlyTheirOwnIntents` in
      `server/internal/intents/isolation_integration_test.go`, covering read, settle and event scope
- [x] T021 [P] [US2] `TestSettlingAnIntentRecordsNoTrade` — and a settled intent stops being
      evaluated rather than reporting a zeroed consequence
- [x] T022 [P] [US2] `TestTheProductCreatesNoIntents`
- [x] T023 [P] [US2] `TestNothingAnIntentCarriesCouldBeActedOn` — asserted on the encoding
- [x] T024 [P] [US2] `TestAnIntentInSomethingTheProductDoesNotCarryIsRefused`

## Phase 5: US3 — the transport

- [x] T025 [US3] `server/internal/api/intents_test.go` — the contract, the CSRF requirement, the
      refusal codes, and that no response field could be sent to a broker
- [x] T026 [US3] `server/internal/api/intents.go` and the three routes in `router.go`
- [x] T027 [US3] Share `riskEvaluationDTO` with the risk handler so a verdict cannot read
      differently depending on which screen asked
- [x] T028 [US3] Register the 025 contract in `server/internal/api/contract_test.go`
- [x] T029 [US3] Wire the service in `cmd/market-lens/main.go`, handing it the same portfolio and
      risk services rather than second copies

## Phase 6: US4 — the screen

- [x] T030 [US4] `src/types/marketData.ts` and `src/services/marketData.ts` — the three calls, the
      wire mapping, and `order_intents.changed.v1` in the subscribed event types
- [x] T031 [US4] `src/components/finance/IntentList.test.ts`, then `IntentList.vue`
- [x] T032 [US4] `src/components/finance/IntentForm.test.ts`, then `IntentForm.vue`
- [x] T033 [US4] `src/views/IntentsView.test.ts`, then `IntentsView.vue`, the `/intents` route and
      the navigation link
- [x] T034 [US4] The instrument field waits for its list rather than opening an empty dropdown that
      never repopulates
- [x] T035 [US4] `e2e/order-intents.spec.ts` at 360x800, 768x1024, 1440x900 and the 320 floor
- [x] T036 [US4] Add `/intents` to `e2e/tab-consistency.spec.ts`, and confirm the ninth navigation
      link is still clickable at 1440, 1024, 768, 390 and 320

## Phase 7: Polish

- [ ] T037 `make verify`, the full Playwright suite, `docker build`, `docker compose config`
- [ ] T038 Update `specs/README.md`, `ROADMAP.md` and `AGENTS.md`
- [ ] T039 Ship, and record production evidence without seeding any data

## Dependencies

T007–T008 block everything. T009–T011 block T014. T012 blocks T013–T015. Phase 4 depends on
Phase 3's service. Phase 5 depends on Phase 4. Phase 6 depends on Phase 5's contract. Within a
phase, tasks marked [P] touch different files and may proceed together.

## Implementation strategy

US1 is the MVP: without the consequence the feature is a list of notes. US2 is not optional — it is
the property that makes the feature safe to have, and it ships in the same change. US3 and US4 make
it reachable.
