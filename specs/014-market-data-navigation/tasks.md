---

description: "Task list for feature 014: Market Data navigation, sector data, continuous listing"
---

# Tasks: Market Data Navigation, Sector Data, and Continuous Listing

**Input**: Design documents from `/specs/014-market-data-navigation/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/openapi.yaml](contracts/openapi.yaml)

**Tests**: Mandatory. Every production-code task is preceded by a test task, and each test task
includes running the test and recording that it failed for the expected *behavioural* reason
before any production code is written. A compilation failure is not a red.

**Format**: `[ID] [P?] [Story] Description`. `[P]` means it touches different files from its
neighbours and may run in parallel.

---

## Phase 1: Setup

- [x] T001 Register `../../../specs/014-market-data-navigation/contracts/openapi.yaml` in `contractPaths` in `server/internal/api/contract_test.go`, and run the contract suite to record which of the three new operations it reports as unimplemented — that list is the checklist for Phases 4–6

---

## Phase 2: Foundational (blocking prerequisites)

**Purpose**: The shared fixture work every story's tests need. No user-visible behaviour.

- [ ] T002 [P] Extend `newExplorationFixture` in `server/internal/instruments/exploration_fixture_test.go` with a helper that seeds enough instruments to exceed one page under a stated filter, so that paging, totals and scrolling can be tested against a known number rather than against however many the Nordic seed happens to contain
- [x] T003 [P] Add a Vitest helper in `src/services/__fixtures__/marketData.ts` that builds a listing response with a `total` and a `next_cursor`, and one that builds a page without a total, so component tests can express "first page" and "later page" without hand-writing envelopes
- [ ] T004 [P] Add a Playwright helper in `e2e/support/` that stubs a multi-page listing — a stated total, cursors that chain, and a last page with `next_cursor: null` — so every viewport scenario drives the same data

**Checkpoint**: Fixtures exist; no behaviour has changed.

---

## Phase 3: User Story 1 — Operational state has its own place (Priority: P1) 🎯 MVP

**Goal**: Import runs, item outcomes, quality findings and the feature engine's runs live on
their own screen. Market Data keeps one line about freshness that links there.

**Independent test**: Open `/operations` directly and see every operational fact without
visiting Market Data; open Market Data and find nothing below the instrument table.

### Failing tests for User Story 1 (MANDATORY) ⚠️

- [x] T005 [P] [US1] Write `TestListingFeatureRunsReturnsTheMostRecentFirst` in `server/internal/features/repository_integration_test.go`: seed three runs of different kinds and statuses, assert the read returns them newest first with kind, status, timestamps, instrument count, value count and failed count. Record the red: no such read exists
- [x] T006 [P] [US1] Write `TestFeatureRunsEndpointReportsRecentRuns` in `server/internal/api/features_test.go`: the endpoint returns the contract's shape, defaults to ten, honours `limit`, and rejects a limit outside 1..50. Record the red
- [x] T007 [P] [US1] Write `TestFeatureRunsEndpointRequiresAnActiveSession` in the same file: anonymous and deactivated callers are refused before any data is read, and no response carries a credential, token or raw provider error. Record the red
- [x] T008 [P] [US1] Write `OperationsView.test.ts` in `src/views/`: the view renders import runs with their counts, the selected run's per-instrument outcomes, open quality findings, and feature-engine runs including a failed one, and explains a fresh installation with no runs at all rather than rendering empty tables. Record the red: the view does not exist
- [x] T009 [P] [US1] Write `TestMarketsViewCarriesNoOperationalReport` in `src/views/MarketsView.test.ts`: the Markets view renders no import-run list, and renders exactly one compact freshness statement that links to the operations route. Record the red: it renders the full report today
- [x] T010 [P] [US1] Write `e2e/operations.spec.ts` covering 360x800, 768x1024 and 1440x900: the operational screen is reachable from the primary navigation, shows a failed import with its sanitized reason, shows the engine's most recent run, and fits 320 CSS pixels without horizontal page scrolling. Record the red

### Implementation for User Story 1

- [x] T011 [US1] Implement `ListRuns(ctx, limit)` in `server/internal/features/repository.go` returning runs newest first with their counts, deriving `failed_count` from `feature_run_items`; green T005
- [x] T012 [US1] Add `listFeatureRunsHandler` to `server/internal/api/features.go`, extend the `FeatureReader` interface, and register `GET /api/v1/feature-runs` in `server/internal/api/router.go` inside the protected mux; green T006 and T007
- [x] T013 [P] [US1] Add `fetchFeatureRuns` and its types to `src/services/marketData.ts` and `src/types/marketData.ts`, mapping the wire shape once at the boundary
- [x] T014 [P] [US1] Build `src/components/finance/FeatureRunList.vue` from PrimeVue primitives: kind, status, timing, instrument and value counts, failed count, and an explicit empty state for a deployment where the engine has never run
- [x] T015 [US1] Build `src/views/OperationsView.vue` composing the existing `MarketDataStatus.vue`, the quality findings already fetched by the Markets view, and `FeatureRunList.vue`; green T008
- [x] T016 [US1] Add the `/operations` route to `src/router/index.ts` behind the authenticated guard, and a link in `src/components/AppShell.vue` beside Overview, Market data and Account
- [x] T017 [US1] Remove the operational report from `src/views/MarketsView.vue` and replace it with a compact freshness statement near the top of the page that links to `/operations`, reusing the import-status read it already performs with a limit of one; green T009
- [x] T018 [US1] Run the three-viewport Playwright scenario; green T010

**Checkpoint**: Operational state has a home, the engine's runs are visible for the first time,
and Market Data is a research screen. Shippable on its own.

---

## Phase 4: User Story 2 — The universe reads as one continuous list (Priority: P2)

**Goal**: Pages arrive on scroll, the reader always knows their position and the size of the
result set, the end is stated, and nobody is stranded in a list without an end.

**Independent test**: Scroll from the first instrument to the last without touching a control,
with the stated position advancing and the stated total matching a direct count.

### Failing tests for User Story 2 (MANDATORY) ⚠️

- [ ] T019 [US2] **The designated first red.** Write `TestTheMarketsListingReportsItsTotalAndPosition` in `server/internal/instruments/listing_test.go`: a filter matching more rows than one page reports the number of matching instruments alongside the page. Write it so it compiles against `ListingPage.Total` before the query populates it, and record the red as a value failure — the total is absent where a known count was expected
- [ ] T020 [P] [US2] Write `TestTheTotalIsCountedOnlyForACursorlessRequest` in the same file: a first page carries the total, a cursor-carrying page carries none, and the total counts the filtered set rather than the page. Record the red
- [ ] T021 [P] [US2] Write `TestTheTotalMatchesWhatTheFilterActuallyReturns` in the same file: for each of several filters — search term, exchange, status, and one matching nothing — the reported total equals the number of rows the same filter yields when paged to exhaustion. Record the red
- [ ] T022 [P] [US2] Write `TestListingEndpointReportsTheTotalOnTheFirstPageOnly` in `server/internal/api/instruments_test.go`: the body carries `total` on a cursor-less request and `null` on a later one, and `null` is never confused with zero. Record the red
- [ ] T023 [P] [US2] Extend `TestTheFirstPageOfTheUniverseStaysWithinItsBudget` in `server/internal/instruments/listing_test.go` to cover the first page *including* its total, and add a case for a later page, both against the bound R-008 states. Record the red or the immediate green
- [ ] T024 [P] [US2] Write `TestTheListingLoadsTheNextPageWhenTheEndComesIntoView` in `src/views/MarketsView.test.ts`: triggering the injected intersection observer requests exactly one further page, appends its rows, and leaves the loaded rows untouched. Record the red
- [ ] T025 [P] [US2] Write `TestTheListingStatesItsPositionAndTotalAndItsEnd` in the same file: the position statement reflects loaded and total counts as pages arrive, and the end of the result set is stated when `next_cursor` is null — including for a result set that fits one page. Record the red
- [ ] T026 [P] [US2] Write `TestTheNextPageIsReachableWithoutScrolling` in the same file: a focusable control loads the next page, remains present while automatic loading works, and disappears only when the end is reached. Record the red
- [ ] T027 [P] [US2] Write `TestArrivingRowsAreAnnouncedPolitelyWithoutMovingFocus` in the same file: a polite live region states how many rows arrived and the new position, focus is unchanged, and the end is announced once. Record the red
- [ ] T028 [P] [US2] Write `TestChangingAFilterReturnsToTheFirstPage` in the same file: changing filter, search or sort discards loaded rows, re-requests without a cursor, and re-states the total. Record the red
- [ ] T029 [P] [US2] Write `TestAFailedPageKeepsTheRowsAlreadyRead` in the same file: a rejected page request leaves loaded rows intact, states the failure, offers a retry, and a successful retry appends without duplicating. Record the red
- [ ] T030 [P] [US2] Write `TestFastScrollingIssuesOnePageRequestAtATime` in the same file: repeated intersections while a request is in flight produce no additional requests, and the pages arrive in order. Record the red
- [ ] T031 [P] [US2] Write `TestALiveChangeToALoadedRowUpdatesItInPlace` in the same file: an event for an instrument on the second page updates that row without reordering, without changing which rows are loaded, and without refetching the pages. Record the red
- [ ] T032 [P] [US2] Write `TestAMembershipChangeCorrectsTheTotalWithoutMovingRows` in the same file: an event that alters the filtered set updates the stated total and offers a refresh, and no loaded row is removed or reordered until the reader accepts. Record the red
- [ ] T033 [P] [US2] Write `TestReturningFromAnInstrumentRestoresTheListing` in the same file: navigating to a detail view and back with an unchanged query restores the loaded pages and the position, while a changed query loads page one. Record the red
- [ ] T034 [P] [US2] Extend `e2e/instrument-exploration.spec.ts` across all three viewports: scrolling loads further pages without a tap; the stated position advances; the full scroll neither repeats nor omits an instrument; the end is stated. Record the red
- [ ] T035 [P] [US2] Extend `e2e/accessibility.spec.ts`: the next-page control is reachable and operable by keyboard alone at the end of the loaded rows, the live region announces arrivals, and at 320 CSS pixels the position statement neither clips nor wraps into nonsense. Record the red

### Implementation for User Story 2

- [ ] T036 [US2] Add `Total *int64` to `ListingPage` in `server/internal/instruments/model.go` and count the filtered set in `server/internal/instruments/listing.go` only when the request carries no cursor, reusing the same `WHERE` construction as the page so the two can never disagree (R-001); green T019–T021, T023
- [ ] T037 [US2] Serialise `total` in `listInstrumentsHandler` in `server/internal/api/instruments.go` as a nullable integer, and carry it through `src/services/marketData.ts` and `src/types/marketData.ts`; green T022
- [ ] T038 [US2] Build `src/components/finance/ListingProgress.vue`: the position and total statement, the end-of-list statement, the focusable next-page control, and the polite live region, from PrimeVue primitives with no raw controls; green T025–T027
- [ ] T039 [US2] Add the intersection sentinel and its injectable observer factory to the listing in `src/views/MarketsView.vue`, requesting the next page with a root margin of roughly one viewport so rows arrive before the reader meets blank space (R-002); green T024
- [ ] T040 [US2] Implement single-flight page loading and request coalescing in `src/views/MarketsView.vue`, reusing the existing coalescer rather than adding a second mechanism; green T029, T030
- [ ] T041 [US2] Apply R-003 to `applyLiveChange` in `src/views/MarketsView.vue`: update a loaded row in place, never reorder or remove, correct the stated total from the cursor-less refresh the view already issues, and offer a refresh when membership changed; green T031, T032
- [ ] T042 [US2] Reset to the first page on any filter, search or sort change, discarding loaded rows and the cached listing state; green T028
- [ ] T043 [US2] Add the query-keyed listing cache to `src/services/marketData.ts` and restore it on mount, with `scrollBehavior` in `src/router/index.ts` restoring the offset (R-004); green T033
- [ ] T044 [US2] Remove the "Load more" button's page-at-a-time role from `src/views/MarketsView.vue`, keeping the control as the keyboard path defined in T038
- [ ] T045 [US2] Run the Playwright scenarios at all three viewports and the 320-pixel floor; green T034, T035

**Checkpoint**: The universe reads as one list, states its size, and strands nobody.

---

## Phase 5: User Story 3 — Sector is real information (Priority: P3)

**Goal**: Every instrument states a sector or states that it is unclassified, the filter offers
only values that exist, and a classification carries its source and review date.

**Independent test**: Open Market Data and confirm no blank sector cell exists, and that
filtering by any offered sector returns exactly the instruments classified in it.

### Failing tests for User Story 3 (MANDATORY) ⚠️

- [ ] T046 [P] [US3] Write `TestSectorClassificationMigrationClassifiesTheCuratedUniverse` in `server/internal/db/sector_classification_migration_test.go`: after migrating a clean database, `sectors` holds the vocabulary including `unclassified`, every instrument in the curated universe carries a code from it, and every row carries a source and a review date. Record the red: the table does not exist
- [ ] T047 [P] [US3] Write `TestAnInstrumentCannotExistWithoutAClassificationState` in the same file: inserting an instrument without a sector takes the default rather than storing null, and a sector outside the vocabulary is refused. Record the red — this is the constraint that makes today's failure unrepeatable
- [ ] T048 [P] [US3] Write `TestUpgradeFromTheCurrentSchemaClassifiesEveryInstrument` in the same file: applying migrations through 0019 first, then the new one, leaves no instrument unclassified and requires no manual step. Record the red
- [ ] T049 [P] [US3] Write `TestListingReportsTheSectorCodeAndItsDisplayName` in `server/internal/instruments/listing_test.go`: every row carries a non-empty code and the name to display, `unclassified` included. Record the red
- [ ] T050 [P] [US3] Write `TestFilteringBySectorReturnsExactlyThatSector` in the same file: filtering by a code returns exactly its members and the reported total agrees; an unknown code is a client error, not an empty result. Record the red
- [ ] T051 [P] [US3] Write `TestSectorVocabularyEndpointOffersOnlyValuesThatExist` in `server/internal/api/instruments_test.go`: the endpoint returns the vocabulary in display order with per-sector instrument counts. Record the red
- [ ] T052 [P] [US3] Write `TestTheSectorsPathIsNotReadAsAnInstrumentIdentifier` in the same file: `GET /api/v1/instruments/sectors` reaches the vocabulary rather than the instrument handler's identifier validation — the literal segment must win over `{id}`. Record the red or the immediate green
- [ ] T053 [P] [US3] Write `TestTheSectorFilterOffersOnlyServedValues` in `src/components/finance/InstrumentFilters.test.ts`: the options come from the served vocabulary, and no option exists that the server did not send. Record the red: the list is a constant in the component today, and it offers both "Information Technology" and "Technology" (R-006)
- [ ] T054 [P] [US3] Write `TestAnUnclassifiedInstrumentSaysSo` in `src/components/finance/InstrumentTable.test.ts`: an instrument classified `unclassified` renders that word, never an empty cell. Record the red
- [ ] T055 [P] [US3] Extend `e2e/instrument-exploration.spec.ts`: filtering by a sector narrows the list and the stated total agrees, at all three viewports. Record the red

### Implementation for User Story 3

- [ ] T056 [US3] Compile the classification set: for each instrument in the curated universe, its sector from the eleven-name vocabulary based on the company's primary business, with the source recorded as the project's own review and a review date. Record any company whose classification is genuinely ambiguous as `unclassified` rather than guessing
- [ ] T057 [US3] Write `server/internal/db/migrations/0020_sector_classification.sql`: create `sectors` with `code`, `name`, `display_order` and a non-blank check; seed the vocabulary with `unclassified` ordered last; add `sector_source` and `sector_reviewed_on` to `instruments`; classify the curated universe by provider symbol; then set `instruments.sector` NOT NULL with a foreign key and a default of `unclassified`; green T046–T048
- [ ] T058 [US3] Join the vocabulary in `server/internal/instruments/listing.go` so a row carries both code and display name, update `sortColumns` to sort by the display name rather than the code, and validate the sector filter against the vocabulary; green T049, T050
- [ ] T059 [US3] Add `Sectors(ctx)` to `server/internal/instruments/repository.go` returning the vocabulary with per-sector instrument counts, add `listSectorsHandler` to `server/internal/api/instruments.go`, and register `GET /api/v1/instruments/sectors` before the `{id}` route; green T051, T052
- [ ] T060 [US3] Serve the vocabulary to the client through `src/services/marketData.ts`, and **delete** the `SECTORS` constant from `src/components/finance/InstrumentFilters.vue`, rendering the filter from the response; green T053
- [ ] T061 [US3] Render the display name in `src/components/finance/InstrumentTable.vue`, with `unclassified` shown as a stated value rather than a blank; green T054
- [ ] T062 [US3] Run the Playwright sector scenario at all three viewports; green T055

**Checkpoint**: No filter in the interface can return nothing for every choice.

---

## Phase 6: Polish & cross-cutting concerns

- [ ] T063 [P] Write `TestNoFilterOffersOnlyEmptyChoices` in `server/internal/api/instruments_test.go`: for every value the sector vocabulary offers, either instruments hold it or it is `unclassified` — the standing guard behind FR-020 and SC-008
- [ ] T064 [P] Write `TestTheListingContractMatchesItsSpecification` coverage for the three new operations by running the registered contract suite from T001; green when all three are implemented
- [ ] T065 [P] Confirm `src/components/library-usage.test.ts` still passes with the new components: no raw controls, no restyling of control chrome
- [ ] T066 Run `make verify` and fix anything it reports without weakening a test
- [ ] T067 Run `npm run test:e2e` across `mobile-chromium`, `tablet-chromium` and `desktop-chromium`
- [ ] T068 Run `docker build -t market-lens:local .` and `docker compose config`
- [ ] T069 Ship as one PR from `014-market-data-navigation` (`feat(markets): …`), wait for Keel to roll it, and confirm on production that every instrument states a sector, that the operational screen shows the engine's runs, and that the listing states a total matching a direct count
- [ ] T070 [P] Record the production result in `specs/014-market-data-navigation/quickstart.md` under *Recorded evidence* with the date and app version, and update `specs/013-feature-engine/quickstart.md` if the engine's runs are now reachable from a screen rather than only from SQL
- [ ] T071 [P] Update `specs/014-market-data-navigation/spec.md` status to `shipped`, the 014 row in `specs/README.md`, `ROADMAP.md`, and the SPECKIT block in `AGENTS.md`

---

## Dependencies

- **Setup (Phase 1)**: T001 first; its output names what Phases 4–6 must satisfy.
- **Foundational (Phase 2)**: T002–T004 in parallel, before any story's tests.
- **US1 (Phase 3)**: independent of US2 and US3. T005–T010 are parallel reds; then
  T011 → T012, T013 ∥ T014 → T015 → T016 → T017 → T018.
- **US2 (Phase 4)**: independent of US1 and US3. **T019 is the designated first red for the
  feature and must be observed before any production code in this phase.** T020–T035 are
  parallel reds; then T036 → T037 → T038 → T039 → T040 → T041 → T042 → T043 → T044 → T045.
- **US3 (Phase 5)**: independent of US1 and US2. T046–T055 are parallel reds; then
  T056 → T057 → T058 → T059 → T060 ∥ T061 → T062.
- **Polish (Phase 6)**: T063–T065 after their stories; T066–T068 before the release; T069
  after they pass; T070–T071 after production confirms.

## Parallel opportunities

- Phase 2: three fixture tasks at once.
- Each story's red tasks are written together — six for US1, seventeen for US2, ten for US3 —
  because they touch different files and none depends on another's outcome.
- The three stories can be implemented by three people at once. They share only
  `src/views/MarketsView.vue`, which US1 edits at the bottom (T017) and US2 in the listing
  (T039–T044); sequence those two edits or accept one merge.

## Implementation strategy

**MVP is User Story 1 alone.** It is the smallest change that repairs something actively wrong:
operational state gets a home, and the feature engine's runs become visible for the first time
since v0.9.0 — today a failed computation leaves the Markets statistics stale with nothing on
any screen to say so.

Then US2, which is the interaction a researcher performs every session. Then US3, which is one
column and one filter but removes a standing lie from the interface.

Each story is shippable alone. Nothing here requires the others to be useful, and the
verification tasks in Phase 6 apply to whichever subset ships.
