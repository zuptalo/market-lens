---

description: "Task list for feature 022: personal portfolio and holdings"
---

# Tasks: Personal Portfolio and Holdings

**Input**: Design documents from `/specs/022-personal-portfolio/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/openapi.yaml](contracts/openapi.yaml),
[quickstart.md](quickstart.md)

**Tests**: Mandatory. Every production task is preceded by a test task, and each test task includes
running the test and recording that it failed for the expected *behavioural* reason before any
production code is written. A compilation failure is not a red.

**Format**: `[ID] [P?] [Story] Description`. `[P]` means it touches different files from its
neighbours and may run in parallel.

---

## Phase 1: Setup

- [ ] T001 Register `../../../specs/022-personal-portfolio/contracts/openapi.yaml` in `contractPaths` in `server/internal/api/contract_test.go` and record that all six operations are reported unimplemented — that list is the checklist for Phase 6

---

## Phase 2: Foundational (blocking prerequisites)

- [ ] T002 Write `TestPersonalPortfolioMigration` in `server/internal/db/personal_portfolio_migration_test.go`: after a clean install and after an upgrade, `portfolios` and `portfolio_trades` exist; `user_id` is NOT NULL on both; a trade whose `user_id` differs from its portfolio's owner is refused; a future-dated trade is refused; a row cannot be both superseded and withdrawn; `(portfolio_id, sequence)` is unique. Record the red: the tables do not exist
- [ ] T003 Write `server/internal/db/migrations/0026_personal_portfolios.sql` with both tables, the composite foreign key holding `user_id` equal to the portfolio's owner, and the constraints above; green T002
- [ ] T004 Update the migration bookkeeping assertions in `server/internal/db/migrate_test.go` and `server/internal/db/feature_engine_migrations_test.go` for the new head
- [ ] T005 [P] Write `server/internal/portfolio/model.go` with the portfolio, trade, holding, valuation, realised-result and absence types — declarations only, no behaviour, so the first red is a value failure and not a compilation one

**Checkpoint**: a trade can be stored and cannot be stored wrongly; nothing reads one yet.

---

## Phase 3: User Story 1 — A person records what they own, and sees what it is worth (Priority: P1) 🎯 MVP

### Failing tests for User Story 1 (MANDATORY) ⚠️

- [ ] T006 [US1] **The designated first red.** Write `TestAPersonSeesOnlyTheirOwnHoldings` in `server/internal/portfolio/isolation_integration_test.go`: two people each record a trade; each reads the portfolio, its totals and its events, and observes only their own. Record the red as a value failure — a person who has just recorded a trade reads nothing back
- [ ] T007 [P] [US1] Write `TestEveryPathIsRefusedAcrossUsers` in the same file: reading, correcting, withdrawing and listing another person's trade each answer as though it does not exist, and the owner role grants no access. Also refused to an unauthenticated and to a deactivated caller. Record the red
- [ ] T008 [P] [US1] Write `TestRecordingAccumulatesAPosition` in `server/internal/portfolio/service_integration_test.go`: two purchases of one instrument make one position of their sum, both trades stay individually visible, and a partial sale reduces it. Record the red
- [ ] T009 [P] [US1] Write `TestAnUnpriceableHoldingIsStatedNotGuessed` in the same file: a holding whose instrument has no stored close is reported unvalued with `no_price`, and the total says it is incomplete rather than omitting the holding. Record the red
- [ ] T010 [P] [US1] Write `TestTheProductRefusesWhatItCannotTrack` in the same file: an instrument outside the universe, a sale larger than the position (naming the held quantity), and a future-dated trade are each refused with their stated code. Record the red
- [ ] T011 [P] [US1] Write `TestEachHoldingIsValuedAtItsOwnLatestSession` in `server/internal/portfolio/valuation_test.go`: two instruments whose latest stored sessions differ are each valued at their own, and each states which. Record the red

### Implementation for User Story 1

- [ ] T012 [US1] Implement `server/internal/portfolio/repository.go`: owner-scoped reads and the single transactional write, publishing `portfolio.changed.v1` with scope `user` on the same transaction; green T006, T007
- [ ] T013 [US1] Implement `server/internal/portfolio/fifo.go`: the fold over `(trade_date, sequence)` producing position and cost; green T008
- [ ] T014 [US1] Implement `server/internal/portfolio/valuation.go`: latest stored close per instrument, the euro cross, and the two absence reasons; green T009, T011
- [ ] T015 [US1] Implement `server/internal/portfolio/service.go`: record, with the three refusals and their messages; green T010

**Checkpoint**: a person can record what they hold and see it, and nobody else can. Shippable alone, but see the strategy in Phase 8.

---

## Phase 4: User Story 2 — What it cost, and what it has made or lost (Priority: P1)

### Failing tests for User Story 2 (MANDATORY) ⚠️

- [ ] T016 [P] [US2] Write `TestFirstInFirstOutConsumesTheOldestShares` in `server/internal/portfolio/fifo_test.go` over worked examples with answers computed by hand, including a sale spanning two purchase lots and two purchases on one date distinguished only by entry order. Record the red
- [ ] T017 [P] [US2] Write `TestRealisedAndRemainingCostReconcile` in the same file as a property over generated trade sequences: for any sequence, realised cost plus remaining cost equals total purchase cost, and no sale consumes shares that were never bought. Record the red
- [ ] T018 [P] [US2] Write `TestProfitIncludesTheCostsOfTheTradesThatProducedIt` in `server/internal/portfolio/service_integration_test.go`: brokerage on both the purchase and the sale is inside the realised figure, and a zero-cost trade reports zero rather than omitting it. Record the red
- [ ] T019 [P] [US2] Write `TestAFullySoldPositionKeepsItsResult` in the same file: the holding leaves the open list, its realised result stays readable, and a later re-purchase starts a new cost rather than merging. Record the red
- [ ] T020 [P] [US2] Write `TestConversionCrossesThroughTheEuroInOneSession` in `server/internal/portfolio/valuation_test.go`: a Danish holding in a Swedish portfolio converts DKK→EUR→SEK at one session's two rates, the rate is stated, and a session missing either leg reports `no_rate` rather than carrying one forward. Record the red

### Implementation for User Story 2

- [ ] T021 [US2] Extend `fifo.go` with realised results per sale; green T016, T017, T018, T019
- [ ] T022 [US2] Extend `valuation.go` with the euro cross and its absence; green T020

**Checkpoint**: the numbers a person came for, each reconciling with the trades behind it.

---

## Phase 5: User Story 3 — A mistake can be corrected, and the correction is visible (Priority: P2)

### Failing tests for User Story 3 (MANDATORY) ⚠️

- [ ] T023 [P] [US3] Write `TestACorrectionSupersedesRatherThanOverwrites` in `server/internal/portfolio/service_integration_test.go`: every derived figure moves, the previous version stays readable as superseded with when it changed, and the sequence does not change. Record the red
- [ ] T024 [P] [US3] Write `TestAWithdrawnTradeStopsCountingAndStaysReadable` in the same file. Record the red
- [ ] T025 [P] [US3] Write `TestACorrectionThatWouldGoNegativeIsRefused` in the same file: reducing a purchase below what a later sale consumed is refused, naming that sale. Record the red

### Implementation for User Story 3

- [ ] T026 [US3] Implement correction and withdrawal in `repository.go` and `service.go`, publishing the event on the same transaction; green T023, T024, T025

**Checkpoint**: a typo is fixable and the fix is visible, so the portfolio can be reconciled.

---

## Phase 6: User Story 4 — What the market did, and what a strategy thinks (Priority: P2)

### Failing tests for User Story 4 (MANDATORY) ⚠️

- [ ] T027 [P] [US4] Write `TestAHoldingIsComparedWithItsOwnMarketOverItsOwnWindow` in `server/internal/portfolio/comparison_test.go`: the benchmark return is measured between the first purchase still contributing to the cost and the valuation session, and both dates are stated. Record the red
- [ ] T028 [P] [US4] Write `TestAnUncoveredWindowIsStatedUnavailable` in the same file: a holding older than its market's stored series reports the comparison unavailable with its reason, never truncated. Record the red
- [ ] T029 [P] [US4] Write `TestNoPortfolioReturnIsEverReported` in `server/internal/portfolio/service_integration_test.go`: the total carries a stated `return_absence` and no field anywhere in the response is a portfolio return. Record the red
- [ ] T030 [P] [US4] Write `TestPortfolioReadsMatchTheContract` in `server/internal/api/portfolio_test.go`: the six operations match `contracts/openapi.yaml`, the trade listing pages by keyset, decimals survive as strings, `records_what_you_entered` is always present, and every operation is refused to an unauthenticated and to a deactivated caller. Record the red
- [ ] T031 [P] [US4] Write `TestAPortfolioChangeReachesOnlyItsOwner` in `server/internal/api/sse_integration_test.go`: the owner sees `portfolio.changed.v1` without reloading; a second person connected throughout receives nothing; a reconnecting owner replays their own exactly once and none of anybody else's. Record the red

### Implementation for User Story 4

- [ ] T032 [US4] Implement `server/internal/portfolio/comparison.go`; green T027, T028
- [ ] T033 [US4] Implement the six operations in `server/internal/api/portfolio.go` and register them in `router.go`; green T029, T030, T031

**Checkpoint**: a profit can no longer be read as a success without seeing what the market did.

---

## Phase 7: The interface

- [ ] T034 [P] Write `src/components/finance/HoldingTable.test.ts` and `TradeEntryForm.test.ts` in Vitest: gain and loss carry a sign and a word rather than colour alone; an unvalued holding states its reason; a refusal is shown with what to do about it. Record the red
- [ ] T035 [P] Write `e2e/portfolio.spec.ts`: at 360x800, 768x1024 and 1440x900 a person records a trade, sees the holding, its comparison and the no-advice statement with no horizontal page scrolling; the absent portfolio return says why; a correction is visible as one; at the 320-pixel floor nothing clips. Record the red
- [ ] T036 [P] Add the portfolio types and the six calls to `src/types/marketData.ts` and `src/services/marketData.ts`, including the new event type
- [ ] T037 Build `src/components/finance/HoldingTable.vue`, `TradeEntryForm.vue` and `TradeHistory.vue`; green T034
- [ ] T038 Build `src/views/PortfolioView.vue` with the no-advice statement and the stated absence of a portfolio return, and register `/portfolio` in `src/router/index.ts` and the navigation; green T035

---

## Phase 8: Polish & cross-cutting concerns

- [ ] T039 [P] Write `TestAPortfolioOfAThousandTradesReadsWithinItsBudget` in `server/internal/portfolio/budget_integration_test.go` at the scale SC-008 states, and record the figure
- [ ] T040 [P] Confirm `src/components/library-usage.test.ts`, `e2e/tab-consistency.spec.ts` and `e2e/accessibility.spec.ts` pass with the new components and route
- [ ] T041 [P] Confirm the identity, strategy, backtest and market-data suites pass unchanged — this feature reads shared reference data and must not alter it
- [ ] T042 Run `make verify` with `go test -p 3` and fix what it reports without weakening a test
- [ ] T043 Run `npm run test:e2e` across the three viewport projects
- [ ] T044 Run `docker build -t market-lens:local .` and `docker compose config`
- [ ] T045 Ship as one PR from `022-personal-portfolio`, wait for Keel to roll it, and confirm the migration applied
- [ ] T046 [P] Run the quickstart's checks against production and record each result under *Recorded evidence*, with the date and app version
- [ ] T047 [P] Update `specs/022-personal-portfolio/spec.md` status to `shipped`, the 022 row in `specs/README.md`, `ROADMAP.md`, and the SPECKIT block in `AGENTS.md`

---

## Dependencies

- **Setup**: T001 first.
- **Foundational**: T002 → T003 → T004; T005 in parallel with them.
- **US1 (Phase 3)**: **T006 is the designated first red and must be observed before any production
  code in this phase.** T007–T011 parallel; then T012 → T013 → T014 → T015.
- **US2 (Phase 4)**: needs US1's fold. T016–T020 parallel; then T021 ∥ T022.
- **US3 (Phase 5)**: needs US1's write path. T023–T025 parallel; then T026.
- **US4 (Phase 6)**: needs US2's figures. T027–T031 parallel; then T032 → T033.
- **Interface (Phase 7)**: needs Phase 6's operations. T034–T036 parallel; then T037 → T038.
- **Polish (Phase 8)**: T039–T041 after their phases; T042–T044 before the release; T045 after they
  pass; T046–T047 after production confirms.

## Implementation strategy

**MVP is User Stories 1 and 2 together.** They ship as one for the reason the spec gives: a
portfolio showing quantity and value without cost and profit is a number nobody can act on, and the
cost is entered with the trade anyway. US3 makes the first typo survivable, which is what stops a
person moving back to a spreadsheet. US4 stops a profit reading as a success.

The ordering is deliberate in one respect worth stating: the isolation test comes first, before any
figure is computed. This is the product's first user-owned domain record, and every later feature in
Milestone 6 inherits whatever boundary is built here. Getting a number wrong is a bug; getting this
wrong is a breach.
