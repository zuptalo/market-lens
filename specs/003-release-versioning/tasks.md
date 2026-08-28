# Tasks: Release Versioning and Protected Delivery

**Input**: Design documents from `specs/003-release-versioning/`

**Tests**: Mandatory red-green-refactor evidence is recorded in each story phase.

## Phase 1: Setup

- [x] T001 Record feature 003 as current/in-progress in `ROADMAP.md` and `specs/README.md`
- [x] T002 [P] Add release/title conventions to `.github/PULL_REQUEST_TEMPLATE.md`

---

## Phase 2: Foundational

- [x] T003 Write `scripts/release-version.test.sh`, run it, and record the expected red failure caused by missing `scripts/release-version.sh`
- [x] T004 Implement branch/title validation, bump classification, bootstrap, and next-version calculation in `scripts/release-version.sh` until T003 is green
- [x] T005 Add the green release-policy test to `Makefile` verification targets

**Checkpoint**: Deterministic branch/title/SemVer behavior is executable and tested.

---

## Phase 3: User Story 1 - Ship only reviewed, verified pull requests (Priority: P1)

**Goal**: Only conforming, fully checked pull requests can reach main through squash.

**Independent Test**: Static workflow tests pass, live GitHub settings show squash-only
and active no-bypass main protection, and a direct main update is rejected.

### Failing tests for User Story 1

- [x] T006 [US1] Write `scripts/workflow-contract.test.sh` assertions for PR policy, always-running required jobs, and repository settings contract; run and record the expected red failures

### Implementation for User Story 1

- [x] T007 [US1] Add the `PR policy` branch/title validation job and stable display names to `.github/workflows/ci.yml`
- [x] T008 [US1] Update contribution/title/squash rules in `CONTRIBUTING.md` and `.github/PULL_REQUEST_TEMPLATE.md`
- [x] T009 [US1] Add `scripts/workflow-contract.test.sh` to `Makefile` and make all local contract assertions green
- [x] T010 [US1] Configure squash-only merge settings, automatic branch deletion, and active no-bypass `main` rules through `gh`; verify the returned repository/ruleset state

**Checkpoint**: User Story 1 is enforced independently of release publication.

---

## Phase 4: User Story 2 - Produce a deterministic release automatically (Priority: P1)

**Goal**: Every verified merged PR creates one SemVer release and one multi-tag image.

**Independent Test**: Workflow contract tests verify serialization/order/tags/permissions,
then the feature PR merge produces `v0.2.0` and all required image aliases.

### Failing tests for User Story 2

- [x] T011 [US2] Extend `scripts/workflow-contract.test.sh` with release trigger, concurrency, permissions, version calculation, verification-before-publication, tag, attestation, and release-last assertions; run and record expected red failures

### Implementation for User Story 2

- [x] T012 [US2] Create serialized verify/build/publish/attest/release automation in `.github/workflows/release.yml`
- [x] T013 [US2] Update image/version build arguments and labels in `Dockerfile` and `.github/workflows/release.yml`
- [x] T014 [US2] Document SemVer rules, tags, permissions, failure/retry behavior, and deployment confirmation in `docs/GITHUB-ACTIONS.md` and `README.md`
- [x] T015 [US2] Create and inspect the `v0.1.0` foundation release through `gh` before activating immutable tag protection
- [x] T016 [US2] Make release workflow contract tests green and verify Actions workflow syntax

**Checkpoint**: User Story 2 is locally complete; live release acceptance occurs after PR merge.

---

## Phase 5: User Story 3 - Identify the running version (Priority: P2)

**Goal**: Every route visibly shows the same build identity returned by health.

**Independent Test**: Known-version Docker build shows/returns `0.2.0`; local development
shows `development`; Playwright passes at 360x800, 768x1024, 1440x900, and 320x800.

### Failing tests for User Story 3

- [x] T017 [P] [US3] Write `src/utils/version.test.ts` for released/development formatting and run it to the expected red missing-module failure
- [x] T018 [P] [US3] Add global version visibility and 320px overflow assertions to `e2e/smoke.spec.ts` and run them to the expected red missing-version failure

### Implementation for User Story 3

- [x] T019 [US3] Implement compile-time identity normalization in `src/utils/version.ts`, `src/vite-env.d.ts`, and `vite.config.ts`
- [x] T020 [US3] Render the accessible global version text in `src/components/AppShell.vue` and responsive/theme styling in `src/styles/main.css`
- [x] T021 [US3] Pass the same `VERSION` argument into frontend and backend build stages in `Dockerfile`
- [x] T022 [US3] Make Vitest/Playwright version scenarios green and verify the known-version health response from a container

**Checkpoint**: All three user stories are complete locally.

---

## Phase 6: Ship and Resume Product Feature

- [x] T023 Update `specs/003-release-versioning/spec.md`, `ROADMAP.md`, and `specs/README.md` to in-review and run `make verify`, Playwright, Docker, Compose, and k3s validation
- [x] T024 Commit/push `003-release-versioning`, create a conventionally titled PR through `gh`, and wait for every required check
- [ ] T025 Squash merge through `gh`, verify feature-branch deletion and protected linear `main`, then monitor automatic `v0.2.0` release and GHCR tags
- [ ] T026 Mark feature 003 shipped in `specs/003-release-versioning/spec.md`, `ROADMAP.md`, and `specs/README.md` through the next protected PR if post-merge evidence requires a documentation update
- [ ] T027 Create/switch to `002-instruments-market-data`, restore `.specify/feature.json` and the `AGENTS.md` active-plan pointer to feature 002, then run `/speckit-tasks`

---

## Dependencies & Execution Order

- T003–T005 block every story because branch/title/version logic is shared.
- User Story 1 and the local parts of User Story 2 can proceed after T005; GitHub rules
  are activated only after required check names exist on the pushed branch.
- User Story 3 can proceed after T005 independently of GitHub configuration.
- T023–T025 depend on all stories; T026 depends on live release evidence; T027 follows
  successful delivery configuration.

## Parallel Opportunities

- T002 can run while foundational tests are prepared.
- T017 and T018 are independent red tests.
- Documentation in T014 can proceed while UI implementation begins after T011 exists.

## Implementation Strategy

Implement the tested policy core first, then enforce PR delivery, then release automation,
then visible identity. Do not configure required checks until the branch containing those
job names has been pushed. Do not create the tag-protection rule until the `v0.1.0`
baseline exists. No production implementation task begins before its listed red test.
