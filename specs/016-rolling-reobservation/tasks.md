---

description: "Task list for feature 016: rolling re-observation of recent sessions"
---

# Tasks: Rolling Re-observation of Recent Sessions

**Input**: Design documents from `/specs/016-rolling-reobservation/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/openapi.yaml](contracts/openapi.yaml)

**Tests**: Mandatory. Every production task is preceded by a test task, and each test task
includes running the test and recording that it failed for the expected *behavioural* reason
before any production code is written. A compilation failure is not a red.

**Format**: `[ID] [P?] [Story] Description`. `[P]` means it touches different files from its
neighbours and may run in parallel.

---

## Phase 1: Setup

- [x] T001 Register `../../../specs/016-rolling-reobservation/contracts/openapi.yaml` in `contractPaths` in `server/internal/api/contract_test.go` and confirm the suite reports no unimplemented operation — this feature adds a field to an existing read, not an operation, so a new entry in that list would mean the contract was written wrong

---

## Phase 2: Foundational (blocking prerequisites)

**Purpose**: The stored count and the configured window. No user-visible behaviour yet.

- [x] T002 Write `TestRevisedCountMigrationUpgradesAndConstrains` in `server/internal/db/revised_count_migration_test.go`: after migrating a clean database and after upgrading from the current schema, `import_runs` and `import_run_items` both carry `revised_count`, every pre-existing run reads zero rather than null, and a row whose revised count exceeds its processed count is refused. Record the red: the column does not exist
- [x] T003 Write `server/internal/db/migrations/0022_revised_session_count.sql`: both columns, `NOT NULL DEFAULT 0`, `>= 0`, and `<= processed_count`, following the shape of the two constraints the tables already carry; green T002
- [x] T004 Update the migration bookkeeping assertions in `server/internal/db/migrate_test.go` and `server/internal/db/feature_engine_migrations_test.go` for the new head — expected maintenance, not a weakened test
- [x] T005 Write `TestReobserveSessionsIsBoundedAndRefused` in `server/internal/config/config_test.go`: the window defaults to 5, accepts 1 and 60, and refuses 0, 61 and a non-integer at startup with a message naming the setting — refused rather than clamped, because an operator who set 500 believing they had covered a quarter must find out. Record the red
- [x] T006 Add `ReobserveSessions` to `server/internal/config/config.go` using the existing bounded-integer helper; green T005

**Checkpoint**: the count can be stored and the window can be configured; nothing asks for it yet.

---

## Phase 3: User Story 1 — A restated close is corrected without anyone asking (Priority: P1) 🎯 MVP

**Goal**: The routine pass re-observes a trailing window, so a restatement inside it is corrected
and flows through the existing cascade.

**Independent test**: Restate a close three sessions back, run the routine pass, and confirm the
stored bar matches the source, the previous values are archived, and the features and signals for
that session were recomputed.

### Failing tests for User Story 1 (MANDATORY) ⚠️

- [x] T007 [US1] **The designated first red.** Write `TestARestatedCloseIsCorrectedByTheNextRoutinePass` in `server/internal/scheduler/marketdata_integration_test.go`: store a history, restate one instrument's close three sessions back in the source, run the scheduled daily pass, and assert the stored bar carries the restated close and the previous values are archived as a revision. Record the red as a value failure: the stored bar still holds the original close and no revision exists
- [x] T008 [P] [US1] Write `TestTheWindowCountsTradingSessionsPerExchange` in `server/internal/marketdata/window_test.go`: for a calendar with a multi-day closure, the window start is the fifth most recent *open* session rather than five calendar days back, and two exchanges with different holidays get different start dates for the same run. Record the red
- [x] T009 [P] [US1] Write `TestTheWindowNeverPredatesAnInstrumentsFirstSession` in the same file: an instrument with three stored sessions is asked about three, not five, and one with none is unaffected. Record the red
- [x] T010 [P] [US1] Write `TestAnUnchangedReobservationWritesNothing` in `server/internal/marketdata/service_integration_test.go`: re-observing identical sessions stores no bar, archives no revision, leaves every bar's `import_run_id` pointing at the run that first stored it, and publishes no bar-change event. Record the red or the immediate green — this is R-002, the property the whole design rests on, and it must be pinned rather than left incidental
- [x] T011 [P] [US1] Write `TestACorrectionRecomputesEveryDerivedValue` in `server/internal/scheduler/marketdata_integration_test.go`: after a restatement, every feature value reading the corrected session is recomputed by the triggered run, and every strategy signal for the affected sessions is rescored for the whole universe rather than only the corrected instrument. Record the red

### Implementation for User Story 1

- [x] T012 [US1] Add the window-start read to `server/internal/marketdata/repository.go`: the Nth most recent open session per exchange from the stored calendar, and the first stored session carried on each target the way `EarliestUnsettled` already is — one query for the universe, not one per instrument; green T008, T009
- [x] T013 [US1] Widen the scheduled pass in `server/internal/scheduler/marketdata.go` to request the window rather than a single date, clamped per instrument, and pass the configured width from `server/cmd/market-lens/main.go`; green T007, T011
- [x] T014 [US1] Confirm the unchanged path still writes nothing after the widening; green T010

**Checkpoint**: a restated close is corrected automatically. Shippable alone.

---

## Phase 4: User Story 2 — An operator can see that history was corrected (Priority: P2)

**Goal**: A run states how many sessions it corrected, distinctly from how many it stored.

### Failing tests for User Story 2 (MANDATORY) ⚠️

- [x] T015 [P] [US2] Write `TestARunCountsCorrectionsSeparatelyFromInsertions` in `server/internal/marketdata/service_integration_test.go`: a pass that stores one new session and corrects two reports processed, accepted and revised as three distinct figures, and the per-instrument items attribute the two corrections to the right instrument. Record the red
- [x] T016 [P] [US2] Write `TestImportRunResponseCarriesTheRevisedCount` in `server/internal/api/marketdata_test.go`: the run response matches the contract including `counts.revised`, and a run that corrected nothing reports zero rather than omitting the field. Record the red
- [x] T017 [P] [US2] Write the corrected-count cases in `src/components/finance/MarketDataStatus.test.ts`: a run that corrected sessions states so as labelled text, and one that corrected none does not imply a correction occurred. Record the red
- [x] T018 [P] [US2] Extend `e2e/operations.spec.ts` across the three viewports: a corrected run states its count without horizontal page scrolling, and nothing clips at 320 pixels. Record the red

### Implementation for User Story 2

- [x] T019 [US2] Change `upsertBar` in `server/internal/marketdata/repository.go` to report inserted, revised or unchanged rather than a bare `changed` boolean, keeping the change event firing for the first two; green T015
- [x] T020 [US2] Add `Revised` to `ImportCounts` in `server/internal/marketdata/service.go` and persist it on the run and its items; green T015
- [x] T021 [US2] Add `revised` to the run response in `server/internal/api/marketdata.go`; green T016
- [x] T022 [P] [US2] Add `revised` to the counts in `src/types/marketData.ts` and map it in `src/services/marketData.ts`
- [x] T023 [US2] State the count in `src/components/finance/MarketDataStatus.vue`; green T017, T018

**Checkpoint**: a correction is visible without querying the database.

---

## Phase 5: User Story 3 — The routine pass stays cheap when nothing changed (Priority: P3)

**Goal**: The widened window is safe to leave on permanently.

### Failing tests for User Story 3 (MANDATORY) ⚠️

- [x] T024 [P] [US3] Write `TestTheWidenedWindowMakesNoExtraSourceRequests` in `server/internal/marketdata/service_integration_test.go`: a pass over the fixture universe with a five-session window makes exactly as many provider calls as a one-session window, counted through the provider stub. Record the red or the immediate green — this is R-001
- [x] T025 [P] [US3] Write `TestAQuietRunTriggersNoRecomputation` in `server/internal/scheduler/marketdata_integration_test.go`: a second routine pass over an unchanged source triggers no feature computation attributable to the re-observed sessions and no strategy computation at all. Record the red or the immediate green
- [x] T026 [P] [US3] Write `TestTheWidenedPassStaysWithinTheDailyBudget` in `server/internal/marketdata/budget_integration_test.go`, asserting the scaled bound the single-session pass already meets rather than a quiet-machine measurement. Record the red or the immediate green

### Implementation for User Story 3

- [x] T027 [US3] Measure and, if needed, reduce the widened pass to its budget; green T024, T025, T026

**Checkpoint**: the window can stay on, which is the only way a correction gets noticed.

---

## Phase 6: Polish & cross-cutting concerns

- [x] T028 [P] Confirm `src/components/library-usage.test.ts` still passes with the changed component
- [x] T029 [P] Confirm the existing revision, incremental-recompute and cross-sectional rescoring suites pass unchanged — the correction path is reused, not rebuilt, and a change there would mean this feature altered behaviour it was supposed to reuse
- [x] T030 Run `make verify` and fix what it reports without weakening a test
- [x] T031 Run `npm run test:e2e` across `mobile-chromium`, `tablet-chromium` and `desktop-chromium`
- [x] T032 Run `docker build -t market-lens:local .` and `docker compose config`
- [ ] T033 Ship as one PR from `016-rolling-reobservation` (`feat(marketdata): …`), wait for Keel to roll it, and confirm the next scheduled pass ran with the widened window
- [ ] T034 [P] Run the quickstart's verification queries against production and record each result in `specs/016-rolling-reobservation/quickstart.md` under *Recorded evidence*, with the date and app version
- [ ] T035 [P] Update `specs/016-rolling-reobservation/spec.md` status to `shipped`, the 016 row in `specs/README.md`, `ROADMAP.md`, and the SPECKIT block in `AGENTS.md`

---

## Dependencies

- **Setup**: T001 first.
- **Foundational**: T002 → T003 → T004 (the schema blocks the count); T005 → T006 in parallel
  with them.
- **US1 (Phase 3)**: **T007 is the designated first red and must be observed before any
  production code in this phase.** T008–T011 parallel; then T012 → T013 → T014.
- **US2 (Phase 4)**: needs US1's widened pass to have something to correct. T015–T018 parallel;
  then T019 → T020 → T021 → T022 ∥ T023.
- **US3 (Phase 5)**: needs US1. T024–T026 parallel; then T027.
- **Polish (Phase 6)**: T028–T029 after their stories; T030–T032 before the release; T033 after
  they pass; T034–T035 after production confirms.

## Parallel opportunities

- Each story's reds are written together — five for US1, four for US2, three for US3 — because
  they touch different files and none depends on another's outcome.
- The configuration pair (T005, T006) is independent of the migration pair (T002, T003).

## Implementation strategy

**MVP is User Story 1.** A restatement being corrected automatically is the entire point; the
other two stories make it visible and make it safe to leave running.

US2 matters more than its priority suggests, because a correction nothing reports is only
marginally better than one that never happens — it becomes visible only to somebody who already
suspects it. US3 is mostly confirmation: both its central properties are already true of the
existing code (R-001, R-002), and the tests exist to stop a future change quietly breaking them,
which is exactly the kind of regression that would turn every quiet night into a full rescore
without anyone noticing.
