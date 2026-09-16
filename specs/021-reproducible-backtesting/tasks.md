---

description: "Task list for feature 021: reproducible backtesting"
---

# Tasks: Reproducible Backtesting

**Input**: Design documents from `/specs/021-reproducible-backtesting/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/openapi.yaml](contracts/openapi.yaml),
[quickstart.md](quickstart.md)

**Tests**: Mandatory. Every production task is preceded by a test task, and each test task
includes running the test and recording that it failed for the expected *behavioural* reason
before any production code is written. A compilation failure is not a red.

**Format**: `[ID] [P?] [Story] Description`. `[P]` means it touches different files from its
neighbours and may run in parallel.

---

## Phase 1: Setup

- [ ] T001 Register `../../../specs/021-reproducible-backtesting/contracts/openapi.yaml` in `contractPaths` in `server/internal/api/contract_test.go` and record that all four reads are reported unimplemented — that list is the checklist for Phase 6

---

## Phase 2: Foundational (blocking prerequisites)

- [ ] T002 Write `TestMarketSeriesMigration` in `server/internal/db/market_series_migration_test.go`: after a clean install and after an upgrade, `benchmark_series`, `benchmark_points` and `fx_rates` exist, a point is unique per series and session, a rate is unique per pair and session, and `rate` is `numeric(24,12)`. Record the red: the tables do not exist
- [ ] T003 Write `server/internal/db/migrations/0024_market_series.sql`; green T002
- [ ] T004 Write `TestBacktestMigration` in `server/internal/db/backtest_migration_test.go`: the seven backtest tables exist, the first configuration is published, a trade whose execution session is not strictly later than its signal session is refused, an equity point with both a total and an absence reason is refused, and one with neither is refused. Record the red
- [ ] T005 Write `server/internal/db/migrations/0025_backtests.sql` with `backtest_configurations`, `backtest_runs`, `backtest_trades`, `backtest_positions`, `backtest_equity`, `backtest_measures`, `backtest_skips`, their constraints, and the first published configuration `momentum_trend_equal_weight_v1`; green T004
- [ ] T006 Update the migration bookkeeping assertions in `server/internal/db/migrate_test.go` and `server/internal/db/feature_engine_migrations_test.go` for the new head
- [ ] T007 [P] Write `server/internal/backtest/model.go` and `server/internal/series/model.go` with the configuration, trade, position, equity point, measure set, skip reason vocabulary and series types — declarations only, no behaviour, so the first red is a value failure and not a compilation one

**Checkpoint**: the result can be stored and the first configuration exists; nothing computes one yet.

---

## Phase 3: User Story 1 — A strategy is measured against what happened (Priority: P1) 🎯 MVP

### Failing tests for User Story 1 (MANDATORY) ⚠️

- [ ] T008 [US1] **The designated first red.** Write `TestTheSameConfigurationProducesTheIdenticalResult` in `server/internal/backtest/service_integration_test.go`: over a fixture of instruments, bars and signals, run the configuration, snapshot every trade, position, equity point and measure, recompute, and diff every stored field. Record the red as a value failure — no result is produced
- [ ] T009 [P] [US1] Write `TestATradeNeverExecutesOnItsSignalSession` in the same file: every trade's execution session is strictly later than its signal session and falls on a session its instrument actually traded (SC-002, SC-009). Record the red
- [ ] T010 [P] [US1] Write `TestAnInstrumentThatStopsTradingIsNotFilled` in the same file: a signal whose instrument never trades again within the give-up window produces no trade and a `not_executable` skip with its reason (FR-006). Record the red
- [ ] T011 [P] [US1] Write `TestCashAndPositionsReconcileAtEverySession` in `server/internal/backtest/portfolio_test.go`: at every session cash plus valued positions equals the recorded equity, and a session with an unvaluable holding records an absence reason instead of a total (FR-010, FR-011). Record the red
- [ ] T012 [P] [US1] Write `TestEveryTradeNamesTheSignalThatCausedIt` and `TestEverySignalWithoutATradeRecordsAReason` in `server/internal/backtest/service_integration_test.go`: no trade has a null signal, and every signal in the range is accounted for by a trade or a skip row with a reason from the vocabulary (SC-003, SC-006). Record the red
- [ ] T013 [P] [US1] Write `TestCostsAreRecordedOnTheTradeAndInTheCash` in the same file: brokerage and slippage appear on the trade that bore them and are reflected in the resulting cash, and a zero-cost configuration reports zero rather than omitting the figure (FR-008, SC-005). Record the red
- [ ] T014 [P] [US1] Write `TestARangeThatProducesNoTradeIsAResult` in the same file: an empty range completes `succeeded` with zero trades, a flat curve and reported measures, not a failure. Record the red

### Implementation for User Story 1

- [ ] T015 [US1] Implement `server/internal/backtest/execution.go`: the next session the instrument actually traded, the open of that session as the execution price, the give-up window, and the cost arithmetic; green T009, T010, T013
- [ ] T016 [US1] Implement `server/internal/backtest/portfolio.go`: cash, positions, equal-weight top N by score, the rebalance schedule, valuation at each session, and the absence reason when a holding cannot be priced; green T011
- [ ] T017 [US1] Implement `server/internal/backtest/service.go`: session-major replay reading `strategies.Repository` signals, recording trades, positions, equity and skips deterministically; green T008, T012, T014
- [ ] T018 [US1] Implement `server/internal/backtest/repository.go`: the single transactional write of the run and its rows, publishing `backtest.completed.v1` on the same transaction; green T008
- [ ] T019 [US1] Add the `backtest run --configuration <name>` command to `server/cmd/market-lens/main.go`
- [ ] T020 [P] [US1] Write `TestABacktestNeverCallsTheProvider` in `server/internal/backtest/service_integration_test.go` with a provider that fails the test if entered, and confirm it passes — the property that makes SC-001 possible

**Checkpoint**: a result exists, reproduces, and cannot read the future. Shippable alone, but see the strategy in Phase 8.

---

## Phase 4: User Story 2 — The result is compared with doing nothing clever (Priority: P1)

### Failing tests for User Story 2 (MANDATORY) ⚠️

- [ ] T021 [P] [US2] Write `TestSeriesImportStoresABenchmarkAndIsIdempotent` in `server/internal/series/service_integration_test.go`: importing a benchmark stores its points, re-importing changes nothing, and a revised point is updated rather than duplicated. Record the red
- [ ] T022 [P] [US2] Write `TestAllSixMeasuresArePresent` in `server/internal/backtest/measures_test.go`: total return, annualised return, volatility, maximum drawdown, trade count and total costs are all recorded, and a result missing any one is refused (FR-018, SC-004). Record the red
- [ ] T023 [P] [US2] Write `TestMaximumDrawdownIsTheWorstPeakToTrough` in the same file over a curve with a known answer — the measure most easily computed wrongly in a flattering direction. Record the red
- [ ] T024 [P] [US2] Write `TestTheBenchmarkIsMeasuredOverTheIdenticalRange` in `server/internal/backtest/service_integration_test.go`: the benchmark's return uses the same first and last sessions as the strategy's. Record the red
- [ ] T025 [P] [US2] Write `TestAnUncoveredBenchmarkIsStatedUnavailable` in the same file: a range beginning before the series does records the comparison as unavailable with its reason and never truncates or back-fills — the Danish case (FR-020). Record the red

### Implementation for User Story 2

- [ ] T026 [US2] Implement `server/internal/series/repository.go` and `service.go` and the `series import --benchmark|--rate` command in `server/cmd/market-lens/main.go`, reading through the existing provider client; green T021
- [ ] T027 [US2] Implement `server/internal/backtest/measures.go` with all six figures and the per-market benchmark comparison with its absence reason; green T022, T023, T024, T025

**Checkpoint**: a result can no longer be read without what it is being compared against.

---

## Phase 5: User Story 3 — A portfolio can hold more than one currency (Priority: P2)

### Failing tests for User Story 3 (MANDATORY) ⚠️

- [ ] T028 [P] [US3] Write `TestASingleCurrencyBacktestNeedsNoRate` in `server/internal/backtest/service_integration_test.go`: a single-market configuration completes with no stored rate present and records no conversion (FR-013). Record the red or the immediate green
- [ ] T029 [P] [US3] Write `TestConversionUsesTheSessionsOwnRateAndRecordsIt` in the same file: a cross-currency trade converts at the execution session's rate and stores that rate on the trade (FR-012). Record the red
- [ ] T030 [P] [US3] Write `TestAMissingRateIsStatedNotCarriedForward` in the same file: a session with no stored rate records the position as unvalued with a reason rather than reusing the previous rate (FR-011, SC-009). Record the red
- [ ] T031 [P] [US3] Write `TestTheCurrencySpreadIsRecordedAndIncluded` in the same file: the configured spread appears on the converted trade and in total costs. Record the red

### Implementation for User Story 3

- [ ] T032 [US3] Implement conversion in `server/internal/backtest/execution.go` and valuation in `portfolio.go`, dividing for the inverse direction rather than reading a second row; green T028, T029, T030, T031

**Checkpoint**: the universe's four currencies can be held in one portfolio, and what cannot be converted says so.

---

## Phase 6: User Story 4 — A person can read the result, and argue with it (Priority: P2)

### Failing tests for User Story 4 (MANDATORY) ⚠️

- [ ] T033 [P] [US4] Write `TestBacktestReadsMatchTheContract` in `server/internal/api/backtests_test.go`: the four reads match `contracts/openapi.yaml`, the list pages by keyset like every other listing, and each read is refused to an unauthenticated and to a deactivated caller. Record the red
- [ ] T034 [P] [US4] Write `TestACompletedBacktestArrivesOverTheStream` in `server/internal/api/sse_integration_test.go`: a reader watching sees `backtest.completed.v1` without reloading, and a client resuming from `Last-Event-ID` replays it exactly once. Record the red
- [ ] T035 [P] [US4] Write `src/components/finance/EquityCurve.test.ts` and `MeasureTable.test.ts` in Vitest: every figure the curve conveys is present as text, an unavailable comparison renders its reason, and direction is never carried by colour alone. Record the red
- [ ] T036 [P] [US4] Write `e2e/backtests.spec.ts`: at 360x800, 768x1024 and 1440x900 the measures, the benchmark comparison and the not-a-prediction statement are reachable with no horizontal page scrolling; a trade leads by keyboard to its signal's contributions; at the 320-pixel floor nothing clips. Record the red

### Implementation for User Story 4

- [ ] T037 [US4] Implement the four reads in `server/internal/api/backtests.go` and register them in `router.go`; green T033, T034
- [ ] T038 [P] [US4] Add the backtest types and reads to `src/types/marketData.ts` and `src/services/marketData.ts`, including the new event type
- [ ] T039 [US4] Build `src/components/finance/EquityCurve.vue`, `MeasureTable.vue` and `TradeList.vue`; green T035
- [ ] T040 [US4] Build `src/views/BacktestsView.vue` with the not-a-prediction statement and the path from a trade to its signal, and register `/backtests` in `src/router/index.ts`; green T036

**Checkpoint**: the result can be read, questioned, and traced back to the strategy's own reasons.

---

## Phase 7: Polish & cross-cutting concerns

- [ ] T041 [P] Write `TestABacktestOverTheUniverseCompletesWithinTheBudget` in `server/internal/backtest/service_integration_test.go` at the scale the plan states, and record the figure
- [ ] T042 [P] Confirm `src/components/library-usage.test.ts` and `e2e/tab-consistency.spec.ts` pass with the new components and route
- [ ] T043 [P] Confirm the strategy, feature-engine and market-data suites pass unchanged — signals are read, not recomputed
- [ ] T044 Run `make verify` with `go test -p 3` and fix what it reports without weakening a test
- [ ] T045 Run `npm run test:e2e` across the three viewport projects
- [ ] T046 Run `docker build -t market-lens:local .` and `docker compose config`
- [ ] T047 Ship as one PR from `021-reproducible-backtesting`, wait for Keel to roll it, import the four benchmark series and three rate series in production, and run the first backtest
- [ ] T048 [P] Run the quickstart's checks against production and record each result under *Recorded evidence*, with the date and app version
- [ ] T049 [P] Update `specs/021-reproducible-backtesting/spec.md` status to `shipped`, the 021 row in `specs/README.md`, `ROADMAP.md`, and the SPECKIT block in `AGENTS.md`

---

## Dependencies

- **Setup**: T001 first.
- **Foundational**: T002 → T003 → T004 → T005 → T006; T007 in parallel with them.
- **US1 (Phase 3)**: **T008 is the designated first red and must be observed before any production
  code in this phase.** T009–T014 parallel; then T015 → T016 → T017 → T018 → T019; T020 last.
- **US2 (Phase 4)**: needs US1's result. T021–T025 parallel; then T026 ∥ T027.
- **US3 (Phase 5)**: needs US2's stored rates. T028–T031 parallel; then T032.
- **US4 (Phase 6)**: needs a stored result. T033–T036 parallel; then T037 → T038 → T039 → T040.
- **Polish (Phase 7)**: T041–T043 after their stories; T044–T046 before the release; T047 after
  they pass; T048–T049 after production confirms.

## Implementation strategy

**MVP is User Stories 1 and 2 together.** They ship as one for the reason the spec states: a
result with no benchmark is the most misleading artefact this product could produce, and it is
what US1 alone produces by default. US3 widens the portfolio to the universe it actually has, and
US4 makes the whole thing readable — until then a result exists only to the command line and to
tests, which is the right order but not a stopping point.
