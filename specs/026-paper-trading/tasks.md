# Tasks: Paper Trading

**Feature**: 026 | **Branch**: `026-paper-trading` | **Plan**: [plan.md](plan.md)

Tests come first throughout. Every production task names the test that must be observed failing —
on a value, not on a compilation error — before the code that satisfies it is written.

## Phase 1: Setup

- [x] T001 Write the reviewed specification, with the three owner decisions recorded
- [x] T002 Record the quality checklist, naming FR-024 as the exclusion that matters most
- [x] T003 Resolve the open questions in `research.md` (R-001..R-009)
- [x] T004 Describe three tables, two triggers and twelve absent columns in `data-model.md`
- [x] T005 Specify five operations and one event in `contracts/openapi.yaml`
- [x] T006 Write `plan.md` with the constitution gate

## Phase 2: Foundational

- [x] T007 Write `server/internal/db/paper_trading_migration_test.go` — a clean install, the
      one-account index, the immutability trigger, the `fill_session > placed_session` trigger, and
      the twelve forbidden columns absent
- [x] T008 Add `server/internal/db/migrations/0029_paper_trading.sql`
- [x] T009 Update the pinned schema version in `migrate_test.go` and
      `feature_engine_migrations_test.go`
- [x] T010 Extract feature 021's cost model into `server/internal/costs`, with `backtest` using it —
      a pure refactor, proved by feature 021's own suite staying green

## Phase 3: US1 — a fill at a price nobody had seen

**Goal**: a promoted intent fills at the open of a later session.
**Independently testable**: promote in one session, run the pass, read the fill price against the
stored open — and against the close, which is what a lookahead bug would produce.

- [x] T011 [US1] `TestAPromotedIntentFillsAtTheNextSessionOpen` — the designated first red
- [x] T012 [US1] `server/internal/paper/model.go` — account, order, fill, holding, absence
- [x] T013 [US1] `server/internal/paper/repository.go` — owner-scoped queries and the event
- [x] T014 [US1] `server/internal/paper/fill.go` — the pass: next open, costs, cash, refusals
- [x] T015 [US1] `server/internal/paper/service.go` — open, promote, cancel
- [x] T016 [P] [US1] A fixture whose bar opens two units below its close, so a test can tell a
      correct implementation from one that read the close

## Phase 4: US2 — keeping score

- [x] T017 [US2] `server/internal/paper/account.go` — the FIFO fold, cash, value, the return
- [x] T018 [P] [US2] `TestTheAccountReconcilesExactly` — cash plus holdings against starting cash,
      at twelve decimal places
- [x] T019 [P] [US2] `TestSellingOutLeavesNoHoldingAndKeepsTheResult`
- [x] T020 [P] [US2] `TestTheSameOrdersAgainstTheSameBarsGiveTheSameAccount` (SC-002)
- [x] T021 [P] [US2] `TestAHoldingInAnotherCurrencyIsConvertedNotIgnored`

## Phase 5: US3 — an order that cannot fill says so

- [x] T022 [US3] `TestABuyTheAccountCannotAffordIsRefused`
- [x] T023 [P] [US3] `TestASaleLargerThanThePositionIsRefusedRatherThanShorted`
- [x] T024 [P] [US3] `TestAnOrderWithNoPriceWaitsRatherThanFailing`
- [x] T025 [P] [US3] `TestAFilledOrderIsPermanent`

## Phase 6: US4 — it is mine, and it is not real

- [x] T026 [US4] `TestAPersonSeesOnlyTheirOwnPaperAccount`, covering the fill pass — the one thing
      here that acts for everybody at once
- [x] T027 [P] [US4] `TestAPersonHasOneAccountAndCannotRetuneIt`
- [x] T028 [P] [US4] `TestPromotingWithoutAnAccountSaysSo`
- [x] T029 [P] [US4] `TestTheProductCreatesNoOrders` (SC-003)
- [x] T030 [P] [US4] `TestAnIntentBecomesAtMostOneOrder` and `TestPromotingAnIntentSettlesIt`

## Phase 7: Transport and schedule

- [x] T031 `server/internal/api/paper_test.go`, then `paper.go` and the five routes
- [x] T032 Register the 026 contract in `contract_test.go`
- [x] T033 `TestPendingPaperOrdersAreFilledAfterAnImport` and
      `TestAFailedPaperPassDoesNotFailTheImport`, then `MarketData.PaperFills`
- [x] T034 Wire the service and the pass in `cmd/market-lens/main.go`

## Phase 8: The screen

- [x] T035 `src/types/marketData.ts`, `src/services/marketData.ts`, and
      `paper_account.changed.v1` among the subscribed event types
- [x] T036 `PaperOrderList.test.ts`, then `PaperOrderList.vue`
- [x] T037 `PaperAccountSummary.test.ts`, then `PaperAccountSummary.vue`
- [x] T038 `PaperTradingView.test.ts`, then `PaperTradingView.vue`, `PaperPromoteForm.vue`, the
      `/paper` route and the navigation destination
- [x] T039 Add `/paper` to `e2e/fixtures/api.mjs` ROUTES, so the mobile guard covers it at 320
      and 390 without a bespoke test
- [x] T040 `e2e/paper-trading.spec.ts` at three viewports and the 320 floor

## Phase 9: Polish

- [ ] T041 `make verify`, the full Playwright suite, `docker build`, `docker compose config`
- [ ] T042 Update `specs/README.md`, `ROADMAP.md` and `AGENTS.md`
- [ ] T043 Ship, and record production evidence without seeding any data

## Dependencies

T007–T010 block everything. T011 blocks T012–T015. Phase 4 depends on Phase 3's fills existing.
Phase 7 depends on the service. Phase 8 depends on Phase 7's contract. Within a phase, tasks marked
[P] touch different files.

## Implementation strategy

US1 is the MVP and is also the property the feature exists to establish: without a fill at a price
nobody had seen, the account is a spreadsheet. US4 is not optional and ships in the same change —
the fill pass is the only thing in this product that acts for everybody at once, and an isolation
mistake there would look exactly like a working feature.
