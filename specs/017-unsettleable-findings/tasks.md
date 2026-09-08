---

description: "Task list for feature 017: a finding re-observation cannot settle"
---

# Tasks: A Finding Re-observation Cannot Settle

**Input**: Design documents from `/specs/017-unsettleable-findings/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/openapi.yaml](contracts/openapi.yaml)

**Tests**: Mandatory. Every production task is preceded by a test task, and each test task
includes running the test and recording that it failed for the expected *behavioural* reason
before any production code is written. A compilation failure is not a red.

**Format**: `[ID] [P?] [Story] Description`. `[P]` means it touches different files from its
neighbours and may run in parallel.

---

## Phase 1: Setup

- [ ] T001 Register `../../../specs/017-unsettleable-findings/contracts/openapi.yaml` in `contractPaths` in `server/internal/api/contract_test.go` and record that the accept operation is reported unimplemented — that one line is the checklist for Phase 5

---

## Phase 2: Foundational (blocking prerequisites)

- [ ] T002 Write `TestFindingReexaminationMigration` in `server/internal/db/finding_reexamination_migration_test.go`: after a clean install and after an upgrade, `data_quality_findings` carries the four columns, every existing finding is still `open` with all four null, the paired constraints refuse a half-set pair, and a finding cannot be accepted while unexamined. Record the red: the columns do not exist
- [ ] T003 Write `server/internal/db/migrations/0023_finding_reexamination.sql`: `reexamined_at`, `reexamining_run_id`, `accepted_at`, `accepted_by` with their pairing and status constraints; green T002
- [ ] T004 Update the migration bookkeeping assertions in `server/internal/db/migrate_test.go` and `server/internal/db/feature_engine_migrations_test.go` for the new head
- [ ] T005 [P] Write `TestMaxReachIsBoundedAndRefused` in `server/internal/config/config_test.go`: the maximum reach defaults to 260, accepts 1 and 2600, and refuses 0, 2601 and a non-integer at startup with a message naming the setting. Record the red
- [ ] T006 [P] Add `MaxReachSessions` to `server/internal/config/config.go` using the existing bounded-integer helper; green T005

**Checkpoint**: the state can be stored and the bound configured; nothing reads either yet.

---

## Phase 3: User Story 1 — The nightly run tells the truth again (Priority: P1) 🎯 MVP

### Failing tests for User Story 1 (MANDATORY) ⚠️

- [ ] T007 [US1] **The designated first red.** Write `TestAReExaminedFindingStopsDrivingTheReachBack` in `server/internal/scheduler/marketdata_integration_test.go`: an import covers a finding's session and raises the same rule again; the next scheduled pass is asserted not to request from that session. Record the red as a value failure on the requested range
- [ ] T008 [P] [US1] Write `TestSuccessiveScheduledPassesStopReachingBack` in the same file: three passes over a source that keeps reporting the same condition; the observation count falls to the ordinary window's by the second and stays there. Record the red
- [ ] T009 [P] [US1] Write `TestAKnownRejectionDoesNotDecideTheStatus` in `server/internal/marketdata/service_integration_test.go`: a rejection matching a finding already awaiting a decision leaves the item and the run succeeded, while `rejected_count` still reports it; an unknown rejection still makes both partial. Record the red
- [ ] T010 [P] [US1] Write `TestTheScheduledPassNeverReachesPastItsBound` in `server/internal/scheduler/marketdata_integration_test.go`: a finding far older than the bound does not make the pass request past it. Record the red

### Implementation for User Story 1

- [ ] T011 [US1] Record a re-examination in `server/internal/marketdata/repository.go` where the resolution rule declines to fire, publishing the existing finding event on the same transaction; green T007
- [ ] T012 [US1] Narrow the reach-back predicate in `TargetsForUniverse` to findings that have not been re-examined; green T007, T008
- [ ] T013 [US1] Decide an item's status by rejections not already known, leaving every count untouched; green T009
- [ ] T014 [US1] Apply the configured maximum reach in `server/internal/scheduler/marketdata.go` and wire it from `server/cmd/market-lens/main.go`; green T010

**Checkpoint**: the loop terminates and the nightly status means something. Shippable alone.

---

## Phase 4: User Story 2 — A finding that cannot be settled says so (Priority: P2)

### Failing tests for User Story 2 (MANDATORY) ⚠️

- [ ] T015 [P] [US2] Write `TestAReExaminedFindingStillResolves` in `server/internal/marketdata/service_integration_test.go`: a re-examined finding whose condition later ends resolves through the existing rule — being examined twice must not make it permanent. Record the red or the immediate green
- [ ] T016 [P] [US2] Write `TestTheProductNeverAcceptsAFindingOnItsOwn` in the same file: however many passes run, no finding reaches `accepted_limitation` without a person. Record the red or the immediate green
- [ ] T017 [P] [US2] Write `TestFindingsReadStatesWhatIsAwaitingADecision` in `server/internal/api/marketdata_test.go`: the read matches the contract including `awaiting_decision` and `reexamined_at`, and the filter returns only findings that are open and re-examined. Record the red

### Implementation for User Story 2

- [ ] T018 [US2] Add the awaiting-a-decision filter and the new fields to the findings read in `server/internal/marketdata/repository.go` and `server/internal/api/marketdata.go`; green T017

**Checkpoint**: the state is recorded, readable, and cannot be reached by accident.

---

## Phase 5: User Story 3 — An operator can settle what the product cannot (Priority: P3)

### Failing tests for User Story 3 (MANDATORY) ⚠️

- [ ] T019 [P] [US3] Write `TestAcceptingAFindingRecordsWhoAndWhen` in `server/internal/marketdata/service_integration_test.go`: accepting records the person and the time, moves the finding to `accepted_limitation`, and publishes the existing event on the same transaction. Record the red
- [ ] T020 [P] [US3] Write `TestAcceptingIsRefusedWhenThereIsNothingToDecide` in the same file: a finding that has not been re-examined cannot be accepted, and one no longer open is refused distinguishably. Record the red
- [ ] T021 [P] [US3] Write `TestAcceptingAFindingIsOwnerOnly` in `server/internal/api/marketdata_test.go`: refused to a member, to an unauthenticated caller and to a deactivated one, in the service rather than only in the interface. Record the red
- [ ] T022 [P] [US3] Write `QualityFindingList.test.ts` in `src/components/finance/`: each finding states its instrument, session, rule and detail; the action names the finding it acts on; an empty list says so rather than showing a table. Record the red
- [ ] T023 [P] [US3] Extend `e2e/operations.spec.ts` across the three viewports: a finding awaiting a decision is readable and its action reachable, and nothing clips at 320 pixels. Record the red

### Implementation for User Story 3

- [ ] T024 [US3] Implement acceptance in `server/internal/marketdata/repository.go` and `service.go` with its refusals; green T019, T020
- [ ] T025 [US3] Add the owner-only handler and register `POST /api/v1/market-data/quality-findings/{id}/accept`; green T021
- [ ] T026 [P] [US3] Add the fields and the accept call to `src/types/marketData.ts` and `src/services/marketData.ts`
- [ ] T027 [US3] Build `src/components/finance/QualityFindingList.vue` and show it on `OperationsView.vue`; green T022, T023

**Checkpoint**: what the product cannot settle, a person can.

---

## Phase 6: Polish & cross-cutting concerns

- [ ] T028 [P] Confirm `src/components/library-usage.test.ts` and `e2e/tab-consistency.spec.ts` still pass with the new component
- [ ] T029 [P] Confirm the existing resolution, re-observation and widening suites pass unchanged — the resolution rule is reused, not replaced
- [ ] T030 Run `make verify` and fix what it reports without weakening a test
- [ ] T031 Run `npm run test:e2e` across the three viewport projects
- [ ] T032 Run `docker build -t market-lens:local .` and `docker compose config`
- [ ] T033 Ship as one PR from `017-unsettleable-findings`, wait for Keel to roll it, and watch the next scheduled pass
- [ ] T034 [P] Run the quickstart's checks against production and record each result under *Recorded evidence*, with the date and app version
- [ ] T035 [P] Update `specs/017-unsettleable-findings/spec.md` status to `shipped`, the 017 row in `specs/README.md`, `ROADMAP.md`, and the SPECKIT block in `AGENTS.md`

---

## Dependencies

- **Setup**: T001 first.
- **Foundational**: T002 → T003 → T004; T005 → T006 in parallel with them.
- **US1 (Phase 3)**: **T007 is the designated first red and must be observed before any production
  code in this phase.** T008–T010 parallel; then T011 → T012 → T013 → T014.
- **US2 (Phase 4)**: needs US1's recording. T015–T017 parallel; then T018.
- **US3 (Phase 5)**: needs US2's read. T019–T023 parallel; then T024 → T025 → T026 ∥ T027.
- **Polish (Phase 6)**: T028–T029 after their stories; T030–T032 before the release; T033 after
  they pass; T034–T035 after production confirms.

## Implementation strategy

**MVP is User Story 1.** It stops the loop and gives the nightly status its meaning back, which is
the live regression. US2 makes the state readable and US3 makes it actionable — without the third,
the second is a quieter dead end, which is the failure this feature exists to avoid repeating.
