---

description: "Task list for feature 023: personal risk limits"
---

# Tasks: Personal Risk Limits

**Input**: Design documents from `/specs/023-personal-risk-limits/`

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

- [ ] T001 Register `../../../specs/023-personal-risk-limits/contracts/openapi.yaml` in `contractPaths` in `server/internal/api/contract_test.go` and record that all three operations are reported unimplemented — that list is the checklist for Phase 6

---

## Phase 2: Foundational (blocking prerequisites)

- [ ] T002 Write `TestRiskLimitsMigration` in `server/internal/db/risk_limits_migration_test.go`: after a clean install and after an upgrade, `risk_limits` exists with `user_id` NOT NULL; a second limit of the same kind for one person is refused; a share above 1, a share of 0 and a negative threshold are refused; a fractional `holding_count` is refused; **and the table is empty, because the product publishes no defaults**. Record the red: the table does not exist
- [ ] T003 Write `server/internal/db/migrations/0027_risk_limits.sql` with the table, the unique `(user_id, kind)` constraint, the per-kind threshold checks, and no seed data; green T002
- [ ] T004 Update the migration bookkeeping assertions in `server/internal/db/migrate_test.go` and `server/internal/db/feature_engine_migrations_test.go` for the new head
- [ ] T005 [P] Write `TestAHoldingCarriesItsSectorAndMarket` in `server/internal/portfolio/service_integration_test.go`: a holding reports the sector feature 014 curated and the MIC of its listing exchange, including `unclassified` where that is the classification. Record the red
- [ ] T006 [P] Add `Sector`, `SectorName` and `MIC` to `portfolio.Holding` and read them in the existing instrument join in `server/internal/portfolio/repository.go`; add the three fields to the portfolio contract's `Holding` schema; green T005
- [ ] T007 [P] Write `server/internal/risk/model.go` with the limit, kind, state, evaluation and contribution types — declarations only, so the first red is a value failure and not a compilation one

**Checkpoint**: a limit can be stored and cannot be stored wrongly; a holding knows its sector and market; nothing evaluates yet.

---

## Phase 3: User Story 1 — A person writes down the rules (Priority: P1) 🎯 MVP

### Failing tests for User Story 1 (MANDATORY) ⚠️

- [ ] T008 [US1] Write `TestAPersonSeesOnlyTheirOwnLimits` in `server/internal/risk/isolation_integration_test.go`: two people each state a different limit; each reads the report and the event stream and observes only their own; changing and removing another person's limit answers as though it does not exist. Record the red
- [ ] T009 [P] [US1] Write `TestStatingALimitReplacesTheOneOfThatKind` in `server/internal/risk/service_integration_test.go`: stating a second threshold for a kind replaces the first rather than adding one, and the report still holds one entry for it. Record the red
- [ ] T010 [P] [US1] Write `TestAThresholdThatCannotMeanAnythingIsRefused` in the same file: a share above 1, a share of 0, a negative share, a fractional holding count and an unknown kind are each refused with their stated code and a message naming what the threshold can be. Record the red
- [ ] T011 [P] [US1] Write `TestTheProductSuppliesNoLimits` in the same file: a person who has stated nothing reads an empty list, and no code path anywhere writes a limit that person did not state. Record the red or the immediate green

### Implementation for User Story 1

- [ ] T012 [US1] Implement `server/internal/risk/repository.go`: owner-scoped read, upsert and delete, publishing `risk_limits.changed.v1` with scope `user` on the same transaction; green T008
- [ ] T013 [US1] Implement `server/internal/risk/service.go`: state, change and remove, with the threshold refusals and their messages; green T009, T010, T011

**Checkpoint**: a person can write down their rules and nobody else can see them. Shippable alone, but see the strategy in Phase 8.

---

## Phase 4: User Story 2 — Where they stand, with the working shown (Priority: P1)

### Failing tests for User Story 2 (MANDATORY) ⚠️

- [ ] T014 [US2] **The designated first red.** Write `TestABreachIsReportedWithTheArithmeticBehindIt` in `server/internal/risk/evaluate_integration_test.go`: two holdings in a known proportion and a concentration limit one exceeds; the evaluation reports `exceeded` with the measured share, the threshold, the portfolio total it divided by, and the contributions that produced it. Record the red as a value failure — no limit can be stated, so nothing is evaluated
- [ ] T015 [P] [US2] Write `TestEachKindMeasuresWhatItSays` in `server/internal/risk/evaluate_test.go` over holdings with answers worked out by hand: instrument share is the largest single holding, sector share sums by sector, market share sums by listing exchange, and holding count counts open holdings. Record the red
- [ ] T016 [P] [US2] Write `TestTheContributionsReconcileWithTheMeasurement` in the same file as a property: every contribution's value over the denominator equals its share, the shares sum to one, and the largest equals the measured figure. Record the red
- [ ] T017 [P] [US2] Write `TestNothingHeldIsNotCompliance` in `server/internal/risk/evaluate_integration_test.go`: an empty portfolio reports every kind `unevaluable` with `nothing_held`, never `within`. Record the red
- [ ] T018 [P] [US2] Write `TestNoSurfaceSaysWhatWouldCloseTheGap` in the same file: the evaluation carries no field naming a quantity, an amount to move, or a holding to reduce — FR-015 asserted on the shape rather than trusted to review. Record the red or the immediate green

### Implementation for User Story 2

- [ ] T019 [US2] Implement `server/internal/risk/evaluate.go`: the four kinds, grouped from `portfolio.Service.View`, each producing a state, a measured figure, a denominator and the ordered contributions; green T014, T015, T016, T017, T018

**Checkpoint**: the limits do the work they were written down for.

---

## Phase 5: User Story 3 — Missing data cannot make a limit look satisfied (Priority: P2)

### Failing tests for User Story 3 (MANDATORY) ⚠️

- [ ] T020 [US3] Write `TestAnIncompleteTotalMakesAShareLimitUnevaluable` in `server/internal/risk/evaluate_integration_test.go`: one holding loses its stored price; every share limit reports `unevaluable` with `portfolio_incomplete`, and none reports a figure. Record the red
- [ ] T021 [P] [US3] Write `TestCountingNeedsNoPrice` in the same file: with the same unpriceable holding, `holding_count` still reports normally. Record the red
- [ ] T022 [P] [US3] Write `TestUnclassifiedIsASectorAndIsStated` in the same file: a holding whose sector is `unclassified` appears in the sector contributions under its own label and is inside the denominator. Record the red

### Implementation for User Story 3

- [ ] T023 [US3] Extend `evaluate.go` with the unevaluable branches and the `unclassified` label; green T020, T021, T022

**Checkpoint**: a limit can no longer look satisfied because something was missing.

---

## Phase 6: User Story 4 — Nothing softens a limit, and the reading surface (Priority: P2)

### Failing tests for User Story 4 (MANDATORY) ⚠️

- [ ] T024 [P] [US4] Write `TestAStrategysViewDoesNotChangeAnEvaluation` in `server/internal/risk/evaluate_integration_test.go`: the same breach reports identically whether the instrument carries a BUY or a SELL signal. Record the red or the immediate green
- [ ] T025 [P] [US4] Write `TestRecordingATradeThatBreachesALimitSucceeds` in the same file: the recording is accepted and the breach appears afterwards — feature 022's FR-020 still stands. Record the red or the immediate green
- [ ] T026 [P] [US4] Write `TestLimitsDoNotTouchAStoredBacktest` in the same file: stating and changing limits leaves every `backtest_measures` row byte-identical. Record the red or the immediate green
- [ ] T027 [P] [US4] Write `TestRiskReadsMatchTheContract` in `server/internal/api/risk_test.go`: the three operations match `contracts/openapi.yaml`, decimals survive as strings, `limits_are_your_own` is always present, writes require CSRF, and every operation is refused to an unauthenticated and to a deactivated caller. Record the red
- [ ] T028 [P] [US4] Write `TestALimitChangeReachesOnlyItsOwner` in `server/internal/api/sse_integration_test.go`: the owner sees `risk_limits.changed.v1`; a second person connected throughout receives nothing. Record the red

### Implementation for User Story 4

- [ ] T029 [US4] Implement the three operations in `server/internal/api/risk.go` and register them in `router.go`; green T027, T028

**Checkpoint**: the boundary and the contract hold, and nothing the product thinks moves a limit.

---

## Phase 7: The interface

- [ ] T030 [P] Write `src/components/finance/LimitTable.test.ts` in Vitest: the three states are distinguished by a word rather than colour alone; an unevaluable limit states its reason and is counted as neither within nor exceeded; the breakdown is shown; no rendered text suggests an action. Record the red
- [ ] T031 [P] Write `e2e/risk-limits.spec.ts`: at 360x800, 768x1024 and 1440x900 a person states a limit, sees the state and the arithmetic, and reads the no-advice statement with no horizontal page scrolling; a person with no limits is told so and offered no default; at the 320-pixel floor nothing clips. Record the red
- [ ] T032 [P] Add the risk types and the three calls to `src/types/marketData.ts` and `src/services/marketData.ts`, including the new event type
- [ ] T033 Build `src/components/finance/LimitTable.vue` and `LimitForm.vue`; green T030
- [ ] T034 Build `src/views/RiskLimitsView.vue` with the no-advice statement, register `/risk` in `src/router/index.ts` and the navigation, **and check the navigation still fits at 768 on CI** — adding a destination is a responsive change, and the header has clipped a wrapped row before; green T031
- [ ] T035 Add a line to `src/views/PortfolioView.vue` naming how many limits are exceeded and linking to the screen, without repeating the figures

---

## Phase 8: Polish & cross-cutting concerns

- [ ] T036 [P] Write `TestEvaluatingALargePortfolioStaysWithinItsBudget` in `server/internal/risk/budget_integration_test.go` at a hundred holdings, and record the figure
- [ ] T037 [P] Confirm `src/components/library-usage.test.ts`, `e2e/tab-consistency.spec.ts` and `e2e/accessibility.spec.ts` pass with the new components and route
- [ ] T038 [P] Confirm the portfolio, strategy, backtest and market-data suites pass unchanged — this feature reads holdings and reference data and must alter neither
- [ ] T039 Run `make verify` with `go test -p 3` and fix what it reports without weakening a test
- [ ] T040 Run `npm run test:e2e` across the three viewport projects
- [ ] T041 Run `docker build -t market-lens:local .` and `docker compose config`
- [ ] T042 Ship as one PR from `023-personal-risk-limits`, wait for Keel to roll it, and confirm the migration applied and the table is empty
- [ ] T043 [P] Run the quickstart's checks against production and record each result under *Recorded evidence*, with the date and app version
- [ ] T044 [P] Update `specs/023-personal-risk-limits/spec.md` status to `shipped`, the 023 row in `specs/README.md`, `ROADMAP.md`, and the SPECKIT block in `AGENTS.md`

---

## Dependencies

- **Setup**: T001 first.
- **Foundational**: T002 → T003 → T004; T005 → T006 in parallel; T007 alongside.
- **US1 (Phase 3)**: T008–T011 parallel; then T012 → T013.
- **US2 (Phase 4)**: **T014 is the designated first red and must be observed before any evaluation
  code is written.** T015–T018 parallel; then T019.
- **US3 (Phase 5)**: needs US2's evaluator. T020–T022 parallel; then T023.
- **US4 (Phase 6)**: needs US2's report. T024–T028 parallel; then T029.
- **Interface (Phase 7)**: needs Phase 6's operations. T030–T032 parallel; then T033 → T034 → T035.
- **Polish (Phase 8)**: T036–T038 after their phases; T039–T041 before the release; T042 after they
  pass; T043–T044 after production confirms.

## Implementation strategy

**MVP is User Stories 1 and 2 together.** A product that stores limits and never evaluates them
looks finished and is useless, which is the same reason features 015, 021 and 022 each shipped their
P1 pair as one. US3 is what stops a limit looking satisfied because something was missing — the
failure that matters most and shows least — and US4 pins down the constraint that a risk feature
rots by losing.

The ordering has one deliberate property: **the arithmetic test comes before the evaluator**, and
its answers are worked out by hand. A percentage is the easiest thing in this feature to compute
plausibly and wrongly, and a test that agreed with the implementation would catch nothing.
