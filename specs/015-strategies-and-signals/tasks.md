---

description: "Task list for feature 015: deterministic strategies and signals"
---

# Tasks: Deterministic Strategies and Signals

**Input**: Design documents from `/specs/015-strategies-and-signals/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/openapi.yaml](contracts/openapi.yaml)

**Tests**: Mandatory. Every production task is preceded by a test task, and each test task
includes running the test and recording that it failed for the expected *behavioural* reason
before any production code is written. A compilation failure is not a red.

**Format**: `[ID] [P?] [Story] Description`. `[P]` means it touches different files from its
neighbours and may run in parallel.

---

## Phase 1: Setup

- [ ] T001 Register `../../../specs/015-strategies-and-signals/contracts/openapi.yaml` in `contractPaths` in `server/internal/api/contract_test.go` and record which of the four operations the contract suite reports as unimplemented — that list is the checklist for Phases 5–7

---

## Phase 2: Foundational (blocking prerequisites)

**Purpose**: The schema, the first strategy version, and the fixture every story's tests read.
No user-visible behaviour.

- [ ] T002 Write `server/internal/db/strategy_migration_test.go`: after migrating a clean database, `strategies` holds exactly one current version with a non-blank intent and caveat, its factors name only published feature definitions, its action bands are contiguous and cover [-1,+1], and `signals` refuses a row that is neither scored nor absent and one that is both. Record the red: the tables do not exist
- [ ] T003 Write `server/internal/db/migrations/0021_strategies_and_signals.sql`: the four tables from data-model.md with their checks, the `(strategy_id, session_date, score DESC)` index, and the first `momentum_trend` version published as reference data; green T002
- [ ] T004 [P] Add `server/internal/strategies/fixture_test.go`: a strategy fixture over the engine's existing feature fixture, so a test can compute signals against known feature values rather than against whatever the universe happens to hold
- [ ] T005 [P] Add the signal and strategy builders to `src/services/__fixtures__/marketData.ts` — a scored signal with contributions, an absent one with a reason, and a strategy reference carrying its caveat

**Checkpoint**: The schema exists and one version is published; nothing computes yet.

---

## Phase 3: User Story 1 — A strategy states a view, and says why (Priority: P1) 🎯 MVP

**Goal**: An instrument's signal is computed, stored with its reasons, and readable — action,
score, confidence, and the per-factor contributions that produced them.

**Independent test**: Compute over the fixture, open one instrument, and confirm the score, the
action and the contributions are shown, that they reconcile, and that each names its feature.

### Failing tests for User Story 1 (MANDATORY) ⚠️

- [ ] T006 [P] [US1] Write `TestAFactorNormalisesItsFeatureIntoTheScoringRange` in `server/internal/strategies/factor_test.go`: an absolute factor maps stated feature values to stated positions in [-1,+1] including both bounds, and a cross-sectional factor maps a universe of known values to percentile ranks with ties equal and the ordering total. Record the red
- [ ] T007 [P] [US1] Write `TestTheContributionsAccountForTheScore` in `server/internal/strategies/score_test.go`: for several factor sets, the recorded contributions divided by the stored divisor equal the score exactly at the stored precision, with no remainder. Record the red
- [ ] T008 [P] [US1] Write `TestAnUnavailableFactorLeavesBothSides` in the same file: a factor whose feature is absent is excluded from the numerator *and* the divisor, is recorded with its reason, and does not drag the score toward zero — assert against the same inputs with the factor present and absent. Record the red
- [ ] T009 [P] [US1] Write `TestConfidenceIsAgreementScaledByCoverage` in the same file: unanimous factors give the maximum, disagreement lowers it, and a signal resting on one available factor scores below one where every factor agrees. Record the red — this is FR-013a
- [ ] T010 [P] [US1] Write `TestAScoreOnABandBoundaryTakesTheUpperAction` in `server/internal/strategies/registry_test.go`: bands are contiguous, cover the range, and a score exactly on a boundary maps to exactly one action, the same way every time. Record the red
- [ ] T011 [P] [US1] Write `TestAnUnscorableInstrumentRecordsAStatedAbsence` in `server/internal/strategies/service_integration_test.go`: instruments with too little history, with an absent feature, and excluded by the liquidity rule each store an absence with its own reason and no score, action or confidence. Record the red
- [ ] T012 [P] [US1] Write `TestReadingOneInstrumentsSignalAsOfASession` in `server/internal/strategies/repository_integration_test.go`: the read returns the signal, its strategy version with its caveat, and its contributions in order; an unknown session and an unknown instrument are distinguishable errors. Record the red
- [ ] T013 [P] [US1] Write `TestInstrumentSignalEndpointReturnsTheContractsShape` in `server/internal/api/signals_test.go`: the body matches the contract, decimals are strings, exactly one of scored or absent is settled, and the strategy caveat is present on every response. Record the red
- [ ] T014 [P] [US1] Write `TestSignalEndpointsRequireAnActiveSession` in the same file: anonymous, revoked and deactivated callers are refused before any data is read. Record the red
- [ ] T015 [P] [US1] Write `SignalCard.test.ts` and `ContributionList.test.ts` in `src/components/finance/`: the card shows action, score, confidence and the caveat; the list shows every contribution with its factor, feature, feature value and contribution **as text**, and states an unavailable factor's reason. Record the red
- [ ] T016 [P] [US1] Write `TestAnAbsentSignalStatesItsReasonRatherThanHolding` in `SignalCard.test.ts`: an absent signal renders its reason and renders no action at all — in particular never HOLD. Record the red
- [ ] T017 [P] [US1] Extend `e2e/instrument-exploration.spec.ts` across the three viewports: an instrument's signal, its contributions and the not-advice statement are visible without horizontal page scrolling, and at 320 pixels nothing clips. Record the red

### Implementation for User Story 1

- [ ] T018 [US1] Implement the model in `server/internal/strategies/model.go`: strategy version, factor, action band, signal, contribution, absence reason — the shapes the tests compile against
- [ ] T019 [US1] Implement `server/internal/strategies/factor.go`: absolute transforms and cross-sectional percentile rank with a total, stable tie order; green T006
- [ ] T020 [US1] Implement `server/internal/strategies/score.go`: the weighted mean over available factors, contributions recorded before the division, the divisor, and confidence as agreement × coverage; green T007, T008, T009
- [ ] T021 [US1] Implement `server/internal/strategies/registry.go`: read published versions from the store, resolve factors to feature definitions, and map a score to an action by band; green T010
- [ ] T022 [US1] Implement `server/internal/strategies/service.go` far enough to compute and store one instrument's signals for a session range, recording an absence where it cannot score; green T011
- [ ] T023 [US1] Implement `server/internal/strategies/repository.go`: the point-in-time read and the transactional write; green T012
- [ ] T024 [US1] Add `getInstrumentSignalHandler` to `server/internal/api/signals.go` and register `GET /api/v1/instruments/{id}/signal`; green T013, T014
- [ ] T025 [P] [US1] Add the signal types and read to `src/types/marketData.ts` and `src/services/marketData.ts`, mapping decimals as strings at the boundary
- [ ] T026 [P] [US1] Build `src/components/finance/ContributionList.vue` — every contribution's direction and magnitude as text, never colour or bar length alone; green the list half of T015
- [ ] T027 [US1] Build `src/components/finance/SignalCard.vue` — action, score, confidence stated as factor agreement, the version, and the caveat; green T015 and T016
- [ ] T028 [US1] Show the signal on `src/views/InstrumentMarketDataView.vue`; green T017

**Checkpoint**: One instrument's view is computed, explained and readable. Shippable alone.

---

## Phase 4: User Story 2 — The same inputs always produce the same signal (Priority: P1)

**Goal**: Recomputation is identical, history never moves, and a superseded version keeps its
signals.

**Independent test**: Compute twice and diff every field; publish a second version and confirm
the first's signals are untouched and still attributed.

### Failing tests for User Story 2 (MANDATORY) ⚠️

- [ ] T029 [US2] **The designated first red.** Write `TestTheSameStrategyVersionScoresAnInstrumentIdentically` in `server/internal/strategies/determinism_integration_test.go`: compute one instrument's signal for one session, recompute, and assert every stored field — score, action, confidence, absence reason, every contribution, the divisor — is identical. Record the red as a value failure: no signal is produced
- [ ] T030 [P] [US2] Write `TestRecomputingTheWholeHistoryChangesNothing` in the same file: a full computation over the fixture, snapshotted, then recomputed — zero differing rows. Record the red
- [ ] T031 [P] [US2] Write `TestNoSignalReadsAFeatureFromALaterSession` in `server/internal/strategies/leakage_integration_test.go`: for every stored signal, every contribution's feature session is on or before the signal's session; and extending the fixture's history by sixty sessions and recomputing changes no earlier signal. Record the red
- [ ] T032 [P] [US2] Write `TestASupersededVersionKeepsItsSignals` in `server/internal/strategies/determinism_integration_test.go`: publishing a second version and computing it leaves the first version's signals unchanged and still attributed to it. Record the red
- [ ] T033 [P] [US2] Write `TestARevisedBarRewritesOnlyTheSessionsItAffects` in `server/internal/strategies/recompute_integration_test.go`: revising one bar deep in history, recomputing features and then signals, rewrites signals only within the affected sessions — and, because factors are cross-sectional, for every instrument at those sessions rather than only the revised one. Record the red
- [ ] T034 [P] [US2] Write `TestAFullSignalComputationStaysWithinItsScaledBudget` in `server/internal/strategies/budget_integration_test.go`, and an incremental one, both asserting the linear scaling of the production bounds rather than the quiet-machine measurement (R-008). Record the red or the immediate green

### Implementation for User Story 2

- [ ] T035 [US2] Make the computation deterministic end to end in `server/internal/strategies/service.go`: a stated evaluation order over factors and instruments, decimals rounded once at the stored precision, and no map iteration reaching a stored value; green T029, T030
- [ ] T036 [US2] Enforce the point-in-time read in the feature access path so a signal can only see values as of its own session; green T031
- [ ] T037 [US2] Key signals by strategy version throughout the write path, so computing one version cannot touch another's rows; green T032
- [ ] T038 [US2] Implement the incremental scope in `service.go`: the affected session range from the feature run, every instrument in the universe over that range (R-007); green T033
- [ ] T039 [US2] Measure and, if needed, reduce the full and incremental passes to their budgets; green T034

**Checkpoint**: A recorded view is a record — it cannot drift.

---

## Phase 5: User Story 3 — The universe in the strategy's order (Priority: P2)

**Goal**: A ranked view of the universe for a session, with unscored instruments stated as such.

### Failing tests for User Story 3 (MANDATORY) ⚠️

- [ ] T040 [P] [US3] Write `TestRankingOrdersScoredInstrumentsAndSeparatesTheRest` in `server/internal/strategies/repository_integration_test.go`: scored instruments in descending score with stable ties, unscored instruments returned separately with their reasons and never ranked, and counts of both. Record the red
- [ ] T041 [P] [US3] Write `TestRankingIsStableForIdenticalScores` in the same file: a universe where every instrument scores identically returns the same order on repeated reads. Record the red
- [ ] T042 [P] [US3] Write `TestSignalsEndpointRanksTheUniverse` in `server/internal/api/signals_test.go`: the contract's shape including `scored`, `unscored`, the strategy reference and keyset paging with a total on the cursor-less request. Record the red
- [ ] T043 [P] [US3] Write `SignalsView.test.ts` in `src/views/`: the ranking lists instruments in order with action and score, states the strategy version and session, separates unscored instruments with their reasons, and links each row to its instrument. Record the red
- [ ] T044 [P] [US3] Write `e2e/signals.spec.ts` across the three viewports: the ranking is reachable from the primary navigation, ordered, states its version, and moving from a row reaches that instrument's reasons; 320 pixels does not clip. Record the red

### Implementation for User Story 3

- [ ] T045 [US3] Implement the ranked read in `server/internal/strategies/repository.go` with keyset paging and a cursor-less total, reusing the pagination rules feature 014 established; green T040, T041
- [ ] T046 [US3] Add `listSignalsHandler` and `listStrategiesHandler` to `server/internal/api/signals.go` and register `GET /api/v1/signals` and `GET /api/v1/strategies`; green T042
- [ ] T047 [P] [US3] Add the ranking and strategy reads to `src/services/marketData.ts`
- [ ] T048 [US3] Build `src/views/SignalsView.vue` and add the `/signals` route and navigation entry; green T043
- [ ] T049 [US3] Run the three-viewport scenario; green T044

**Checkpoint**: The strategy has a view of the whole universe, and it is legible.

---

## Phase 6: User Story 4 — Signals follow the data (Priority: P3)

**Goal**: A feature run triggers a signal pass; an owner can trigger one deliberately; a screen
watching signals sees changes arrive.

### Failing tests for User Story 4 (MANDATORY) ⚠️

- [ ] T050 [P] [US4] Write `TestAFeatureRunTriggersTheSignalPass` in `server/internal/features/service_integration_test.go`: a successful feature run asks the signal computer for the sessions it wrote; a failed one does not; a signal failure is logged and does not fail the feature run. Record the red
- [ ] T051 [P] [US4] Write `TestSignalsComputeAcceptsItsRunKinds` in `server/cmd/market-lens/main_test.go`: `signals compute` accepts a universe, `--since-feature-run`, and `--strategy`/`--version`, rejects the mutually exclusive combinations, and forwards each. Record the red
- [ ] T052 [P] [US4] Write `TestAFailedInstrumentKeepsItsPreviousSignals` in `server/internal/strategies/service_integration_test.go`: an instrument whose computation fails keeps its stored signals, records an item with a reason, and the run ends partial with the others written. Record the red
- [ ] T053 [P] [US4] Write `TestSignalsChangedEventIsSharedScopeAndTransactional` in `server/internal/strategies/repository_integration_test.go` and `server/internal/events/isolation_integration_test.go`: the event commits with the signals it describes, is replayed to members and the owner, and is refused to a deactivated or unauthenticated caller. Record the red
- [ ] T054 [P] [US4] Write `TestListingStrategyRunsReturnsTheMostRecentFirst` in `server/internal/strategies/repository_integration_test.go` and its endpoint test in `server/internal/api/signals_test.go`. Record the red
- [ ] T055 [P] [US4] Write `TestASignalChangeRefreshesTheOpenView` in `src/views/SignalsView.test.ts` and `src/services/marketData.test.ts`: `signals.changed.v1` is subscribed by name, a change to a listed instrument re-reads without losing the reader's place, and a reconnection resumes from the last delivered event. Record the red
- [ ] T056 [P] [US4] Write `TestOperationsShowsStrategyRuns` in `src/views/OperationsView.test.ts`: strategy runs appear beside feature runs, a partial run states how many instruments failed, and a deployment where no strategy has run says so. Record the red

### Implementation for User Story 4

- [ ] T057 [US4] Add the `Signals` collaborator to `server/internal/features/service.go` — an interface the strategy service satisfies — called after a successful run, its error logged and swallowed; green T050
- [ ] T058 [US4] Add the `signals compute` command to `server/cmd/market-lens/main.go` with its three kinds, and wire the collaborator in the composition root; green T051
- [ ] T059 [US4] Implement per-instrument failure containment in `server/internal/strategies/service.go` — recover a panic, roll that instrument's scope back, record the item, continue; green T052
- [ ] T060 [US4] Publish `signals.changed.v1` inside the write transaction; green T053
- [ ] T061 [US4] Implement `ListRuns` and `GET /api/v1/strategy-runs`; green T054
- [ ] T062 [P] [US4] Subscribe `signals.changed.v1` in `src/services/marketData.ts` and apply it in `SignalsView.vue`; green T055
- [ ] T063 [P] [US4] Build `src/components/finance/StrategyRunList.vue` and show it on `OperationsView.vue`; green T056

**Checkpoint**: Signals stay current on their own, and their runs are visible.

---

## Phase 7: Polish & cross-cutting concerns

- [ ] T064 [P] Write `TestEveryInstrumentSessionHasASignal` in `server/internal/strategies/invariants_integration_test.go`: every stored bar's `(instrument, session)` has a signal under the current version — scored or absent (FR-008a) — and every signal settles exactly one of the two
- [ ] T065 [P] Write `TestNoSurfaceCallsASignalAdvice` in `server/internal/strategies/vocabulary_test.go`: scan the strategy package, the API, `src/` and this feature's specification for advice vocabulary — "recommend", "you should buy", "guaranteed", "will rise" — outside a denial, in the spirit of feature 013's composite-vocabulary guard
- [ ] T066 [P] Write `TestEveryPublishedStrategyFactorNamesALivingFeature` in `server/internal/strategies/registry_test.go`: every factor of every published version names a feature definition that exists and is not superseded — the guard against a strategy silently reading a definition that was retired
- [ ] T067 [P] Confirm `src/components/library-usage.test.ts` still passes with the new components
- [ ] T068 Run `make verify` and fix what it reports without weakening a test
- [ ] T069 Run `npm run test:e2e` across `mobile-chromium`, `tablet-chromium` and `desktop-chromium`
- [ ] T070 Run `docker build -t market-lens:local .` and `docker compose config`
- [ ] T071 Ship as one PR from `015-strategies-and-signals` (`feat(strategies): …`), wait for Keel to roll it, then run `signals compute --universe nordic-liquid-v1` on the pod per quickstart.md
- [ ] T072 [P] Run the quickstart's verification queries against production and record each result in `specs/015-strategies-and-signals/quickstart.md` under *Recorded evidence*, with the date and app version
- [ ] T073 [P] Update `specs/015-strategies-and-signals/spec.md` status to `shipped`, the 015 row in `specs/README.md`, `ROADMAP.md`, and the SPECKIT block in `AGENTS.md`

---

## Dependencies

- **Setup**: T001 first; its failure list is the checklist for the endpoint work.
- **Foundational**: T002 → T003 (the schema blocks everything); T004 and T005 in parallel after.
- **US1 (Phase 3)**: T006–T017 are parallel reds; then T018 → T019 → T020 → T021 → T022 → T023 →
  T024, with T025 → T026 ∥ T027 → T028.
- **US2 (Phase 4)**: needs US1's compute path. **T029 is the designated first red for the feature
  and must be observed before any production code in this phase.** T030–T034 parallel; then
  T035 → T036 → T037 → T038 → T039.
- **US3 (Phase 5)**: needs US1's stored signals. T040–T044 parallel; then T045 → T046 → T047 →
  T048 → T049.
- **US4 (Phase 6)**: needs US1 and US2. T050–T056 parallel; then T057 → T058 → T059 → T060 →
  T061 → T062 ∥ T063.
- **Polish (Phase 7)**: T064–T067 after their stories; T068–T070 before the release; T071 after
  they pass; T072–T073 after production confirms.

## Parallel opportunities

- Each story's reds are written together — twelve for US1, six for US2, five for US3, seven for
  US4 — because they touch different files and none depends on another's outcome.
- US3 and US4 can be built simultaneously once US1 and US2 are green; they share only
  `src/services/marketData.ts`.

## Implementation strategy

**MVP is User Story 1 plus User Story 2.** Unusually, the two P1 stories must ship together: a
signal that is explained but not reproducible is a number that changes when nobody changed
anything, and a signal that is reproducible but unexplained is an oracle. Either alone is worse
than neither, because both would look finished.

US3 makes the strategy useful rather than merely correct. US4 keeps it current. Both are
shippable increments on top, and the verification tasks in Phase 7 apply to whichever subset
ships.
