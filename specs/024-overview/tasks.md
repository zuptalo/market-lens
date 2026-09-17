---

description: "Task list for feature 024: an Overview that says what needs you"
---

# Tasks: An Overview That Says What Needs You

**Input**: Design documents from `/specs/024-overview/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[quickstart.md](quickstart.md)

**Tests**: Mandatory. Every production task is preceded by a test task, and each test task includes
running the test and recording that it failed for the expected *behavioural* reason before any
production code is written. A compilation failure is not a red.

**Format**: `[ID] [P?] [Story] Description`. `[P]` means it touches different files from its
neighbours and may run in parallel.

---

## Phase 1: Setup

- [ ] T001 Confirm no setup is needed: every read this feature composes already exists in `src/services/marketData.ts`, and the absence of a contract change is why `server/internal/api/contract_test.go` is untouched

---

## Phase 2: User Story 1 — What is waiting (Priority: P1) 🎯 MVP

### Failing tests for User Story 1 (MANDATORY) ⚠️

- [ ] T002 [US1] **The designated first red.** Write `TestTheOverviewSaysWhatIsWaiting` in `src/views/DashboardView.test.ts`: with a finding awaiting a decision and a limit exceeded, the screen names both with their counts and links to the screens that own them. Record the red — the stub renders foundation-stage prose
- [ ] T003 [P] [US1] Write `src/components/finance/WaitingList.test.ts`: a populated list names each item, its count and its destination; an empty list says nothing needs the person in one sentence; an unreadable source says it could not be read and **does not render zero**. Record the red
- [ ] T004 [P] [US1] Write `TestTheOverviewNeverStatesAValue` in `src/views/DashboardView.test.ts`: no rendered text contains a currency code, a percentage sign, or any of the words this product uses for money. Record the red
- [ ] T005 [P] [US1] Write `TestTheOverviewMakesNoClaimAboutUnshippedFeatures` in the same file: no rendered text says anything "will be implemented" — the specific defect FR-009 exists to remove. Record the red

### Implementation for User Story 1

- [ ] T006 [US1] Build `src/components/finance/WaitingList.vue` with its three states; green T003
- [ ] T007 [US1] Replace `src/views/DashboardView.vue` with the composed screen, reading findings, limits and recent runs; green T002, T004, T005

**Checkpoint**: a person can tell whether anything needs them. Shippable alone.

---

## Phase 3: User Story 2 — What changed (Priority: P1)

### Failing tests for User Story 2 (MANDATORY) ⚠️

- [ ] T008 [P] [US2] Write `src/components/finance/ChangeList.test.ts`: the last import's time and status, the sessions it corrected, and when features and signals last ran; a failed or partial run is **not** shown here, because it belongs among the things that are waiting. Record the red
- [ ] T009 [P] [US2] Write `TestACorrectedSessionIsTheStatisticShown` in the same file: the correction count is named and the stored-session count is not — a correction means every derived value moved, and sessions stored is noise beside it. Record the red

### Implementation for User Story 2

- [ ] T010 [US2] Build `src/components/finance/ChangeList.vue`; green T008, T009
- [ ] T011 [US2] Compose it into the view, routing failed and partial runs into the waiting section instead; green T008

**Checkpoint**: the screen is worth opening on a day when nothing needs anybody.

---

## Phase 4: User Story 3 — What could not be determined (Priority: P2)

### Failing tests for User Story 3 (MANDATORY) ⚠️

- [ ] T012 [P] [US3] Write `src/components/finance/UnknownList.test.ts`: holdings that could not be valued and limits that could not be evaluated are each named with their reason; with nothing missing the component renders nothing at all. Record the red
- [ ] T013 [P] [US3] Write `TestAnEmptySectionIsAbsentRatherThanEmpty` in `src/views/DashboardView.test.ts`: with nothing missing, no "what could not be determined" heading appears. Record the red

### Implementation for User Story 3

- [ ] T014 [US3] Build `src/components/finance/UnknownList.vue` and compose it; green T012, T013

**Checkpoint**: scattered absences are collected in one place for the first time.

---

## Phase 5: User Story 4 — The screen states nothing it cannot source (Priority: P2)

### Failing tests for User Story 4 (MANDATORY) ⚠️

- [ ] T015 [P] [US4] Write `TestEveryItemLinksToTheScreenThatOwnsIt` in `src/views/DashboardView.test.ts`: every item carries a link, and the destinations are the existing routes. Record the red or the immediate green
- [ ] T016 [P] [US4] Write `e2e/overview.spec.ts`: at 360x800, 768x1024 and 1440x900 a person sees what is waiting, follows a link to the screen that owns it, and reads no value or percentage; a deployment with nothing waiting says so; a section whose source fails says it could not be read; at the 320-pixel floor nothing clips. Record the red

### Implementation for User Story 4

- [ ] T017 [US4] Finish the view's composition, error handling and landmarks; green T015, T016

---

## Phase 6: Polish & cross-cutting concerns

- [ ] T018 [P] Confirm `src/components/library-usage.test.ts`, `e2e/tab-consistency.spec.ts` and `e2e/accessibility.spec.ts` pass — the Overview is the first tab in the navigation and the tab-consistency assertions apply to it directly
- [ ] T019 [P] Confirm every Go suite passes **unchanged**: this feature adds no backend, and a change to one would mean it had grown an endpoint it was not supposed to have
- [ ] T020 Run `make verify` and fix what it reports without weakening a test
- [ ] T021 Run `npm run test:e2e` across the three viewport projects
- [ ] T022 Run `docker build -t market-lens:local .` and `docker compose config`
- [ ] T023 Ship as one PR from `024-overview`, wait for Keel to roll it, and confirm the screen in production
- [ ] T024 [P] Record the production evidence under *Recorded evidence*, with the date and app version
- [ ] T025 [P] Update `specs/024-overview/spec.md` status to `shipped`, add the 024 row to `specs/README.md`, and update `ROADMAP.md` and the SPECKIT block in `AGENTS.md`

---

## Dependencies

- **Setup**: T001 is a confirmation, not work.
- **US1 (Phase 2)**: **T002 is the designated first red.** T003–T005 parallel; then T006 → T007.
- **US2 (Phase 3)**: T008–T009 parallel; then T010 → T011.
- **US3 (Phase 4)**: T012–T013 parallel; then T014.
- **US4 (Phase 5)**: T015–T016 parallel; then T017.
- **Polish (Phase 6)**: T018–T019 after their phases; T020–T022 before the release; T023 after they
  pass; T024–T025 after production confirms.

## Implementation strategy

**MVP is User Stories 1 and 2 together.** US1 is the reason to have the screen; US2 is the reason to
open it on the days US1 says nothing. Either alone is a screen somebody stops visiting.

One property of this feature is worth stating because it shapes every task: **no task touches Go**.
If one does, the feature has grown an endpoint it was told not to have, and T019 is the check that
notices.
