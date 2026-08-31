# Tasks: Instrument Exploration and Financial Charts

**Input**: Design documents from `/specs/005-instrument-exploration/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md),
[data-model.md](./data-model.md), [contracts/openapi.yaml](./contracts/openapi.yaml),
[quickstart.md](./quickstart.md)

**Tests**: MANDATORY. Every production-code task below is preceded by a test task, and each
test task includes *running* the test and recording that it failed for the stated
behavioral reason before any production code is written. A compile error, a missing
fixture, an unavailable database, or an already-green test is **not** valid red evidence.

**The designated first red** is T013 — a PostgreSQL-backed listing query test asserting
latest close, prior-session change, freshness, and requested ordering. It fails
behaviorally because `Repository.SearchPage` returns identity only, with no price, no
change, no freshness, and no ordering by anything but ticker.

**Responsive UI**: Every user-facing story carries failing-first acceptance tasks at
360x800, 768x1024, and 1440x900 through the existing Playwright projects, plus explicit
320px overflow checks, touch and keyboard parity, and no hover-only interaction.

**Live Delivery**: US4 covers versioned authorized SSE events, `Last-Event-ID` replay,
duplicate-safe application, and reconnecting/stale/offline reporting. The one new event,
`corporate_action.changed.v1`, is written in the same transaction as the action it reports.

**Identity**: No new identity behavior. This feature stores no private server-side data
(research R4), so its isolation evidence is that every read requires an active session and
that an unknown and an unauthorized instrument identifier are indistinguishable.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on incomplete work)
- **[Story]**: US1, US2, US3, US4 — maps to the user stories in spec.md

## Path Conventions

Web application, per plan.md: Go modular monolith under `server/internal/`, Vue client
under `src/`, Playwright journeys under `e2e/`. All paths below are repository-relative.

---

## Phase 1: Setup

**Purpose**: Add and record the one new dependency, and prepare the contract harness.

- [ ] T001 Add `lightweight-charts` to `package.json` pinned to an exact version (no `^`, no `~`), install it, and confirm the client still builds with `npm run build`
- [ ] T002 Record the charting dependency in the *Dependency note* section of `specs/005-instrument-exploration/quickstart.md`: package name, exact installed version, licence, and any attribution requirement, each read from the installed package metadata rather than assumed
- [ ] T003 [P] Generalize `server/internal/api/contract_test.go` so `contractPath` becomes a list of contract files rather than the single hard-coded `specs/004-owner-access/contracts/openapi.yaml`. `contractOperations` MUST parse every registered file into **one merged operation set** and apply its existing `len(operations) < 10` guard to that union — applying the guard per file would make any small contract fatal with `parsed only N operations`, which is a harness failure, not a valid red. Keep the suite green with only the 004 contract registered
- [ ] T004 [P] Add a `lightweight-charts` stub/mock module under `src/components/finance/__mocks__/` so Vitest component tests can assert chart inputs without a real canvas, and register it in `vitest.config.ts`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The shared test fixture every story asserts against, and the read-model type
declarations that let each story's first test fail *behaviorally* rather than fail to
compile.

**⚠️ CRITICAL**: No user story work begins until this phase is complete.

**Note on TDD**: T007 and T008 declare data shapes only — struct and interface
declarations carrying no query, computation, or rendering behavior. They exist so the red
tests that follow assert on returned *values* instead of failing to compile, which is what
plan.md requires of the first red. No behavior is added here; every behavioral change from
T013 onward follows an observed red.

- [ ] T005 Build the shared PostgreSQL fixture in `server/internal/instruments/testdata_fixture_test.go`: two exchanges with different calendars, an instrument with ~300 stored sessions, one with exactly 20 stored sessions (too few for `return_20`/`volatility`), one with zero stored bars, one with an interior gap of open sessions that have no bar, and `exchange_sessions` rows covering `open`, `half_day`, and `closed` across the same dates
- [ ] T006 Extend the fixture in `server/internal/instruments/testdata_fixture_test.go` with a recorded split and a recorded dividend inside the charted range, plus one open and one resolved `data_quality_findings` row for the same instrument
- [ ] T007 [P] Declare the read-model types in `server/internal/instruments/model.go` — `ListingRow`, `Freshness`, `HistoryWindow`, `Coverage`, `ChartAction`, `ChartFinding`, `ListingFilter`, `HistoryFilter` — with nullable fields modelled as pointers so an absent statistic is distinguishable from zero, per data-model.md
- [ ] T008 [P] Declare the matching client types in `src/types/marketData.ts` — `InstrumentListingRow`, `Freshness`, `HistoryWindow`, `Bar`, `ChartAnnotation`, `ColumnPreference` — with `null` rather than `0` for absent statistics
- [ ] T009 [P] Add a Vitest fixture factory in `src/services/__fixtures__/marketData.ts` producing a history window with a gap, an action, a finding, and an instrument with too few sessions, so component tests share one honest sample

**Checkpoint**: Fixtures and type declarations exist; no behavior has been added.

---

## Phase 3: User Story 1 - Find an instrument worth looking at (Priority: P1) 🎯 MVP

**Goal**: The curated universe is a browsable table with exchange-qualified identity,
native price and change, descriptive statistics, and honest freshness — filterable,
searchable, sortable across the whole result set, and paginated.

**Independent Test**: Sign in, open Markets, confirm identity, price, change, and freshness
are listed; then filter, search, sort, and page, and confirm the result set changes
correctly and the visible state survives a reload.

### Failing tests for User Story 1 (MANDATORY) ⚠️

- [ ] T010 [P] [US1] Write the freshness derivation test in `server/internal/instruments/query_test.go`: `current` when a bar exists for the exchange's most recent open session, `stale` with a count of open sessions missed when it does not, `no_history` when no bar exists at all — and that a `closed` calendar day never counts toward `sessions_behind`. Run it and record the red
- [ ] T011 [P] [US1] Write the derived-statistics test in `server/internal/instruments/query_test.go`: `return_20`, `return_90`, and `volatility` computed over *stored sessions* per data-model.md, and **null, never zero**, for the instrument with only 20 stored sessions. Run it and record the red
- [ ] T012 [P] [US1] Write the keyset-pagination test in `server/internal/instruments/repository_integration_test.go`: paging by `(sort value, instrument id)` returns every row exactly once with no repeat or skip across pages, including when the sort value is null for several rows (research R6). Run it and record the red
- [ ] T013 [US1] **Designated first red.** Write the listing query test in `server/internal/instruments/repository_integration_test.go` asserting that `Repository.Listing` returns each instrument's latest close, prior-session change (absolute and percent), and coverage freshness, ordered by a requested sort key with the instrument identifier as a total-order tiebreaker. Run it and record the red: the returned rows carry no latest close and no change, and the requested ordering is ignored
- [ ] T014 [P] [US1] Write the listing handler test in `server/internal/api/instruments_test.go` covering the `q`, `mic`, `country`, `sector`, `status`, `sort`, `order`, `cursor`, and `limit` parameters of `contracts/openapi.yaml`, that out-of-range `sort` and `limit` values yield 400, and that an unauthenticated request yields 401. Run it and record the red
- [ ] T015 [P] [US1] Write the Markets view tests in `src/views/MarketsView.test.ts`: rows render exchange-qualified identity, price in the row's own currency, change, and freshness; a null statistic renders as an explicit absence rather than `0`; an empty result renders an explanation and a control that clears the filters. Run them and record the red
- [ ] T016 [P] [US1] Write `src/components/finance/InstrumentTable.test.ts`: sorting emits a request for the whole result set rather than reordering the loaded page, filters are individually removable, and the optional-column selection round-trips through browser storage and survives a remount (FR-008, research R4). Run it and record the red
- [ ] T017 [P] [US1] Write the browsing journey in `e2e/instrument-exploration.spec.ts` — sign in, open Markets, filter, search, sort, page, reload and confirm filters/sort/page survive — running under the `mobile-chromium`, `tablet-chromium`, and `desktop-chromium` projects, with a 320x800 check that the list neither scrolls the page horizontally nor clips a control. Run it and record the red

### Implementation for User Story 1

- [ ] T018 [US1] Implement `Repository.Listing` in `server/internal/instruments/repository.go`: one SQL statement joining `instruments`, `exchanges`, and `daily_price_bars` with window functions for latest close, previous stored close, 20/90-session returns, and volatility, per the derivations in data-model.md, returning null for every statistic with too few stored sessions (makes T011, T013 green)
- [ ] T019 [US1] Add the freshness projection to the listing statement in `server/internal/instruments/repository.go`, comparing each instrument's latest stored session against the most recent `open` or `half_day` session for its exchange in `exchange_sessions` (makes T010 green)
- [ ] T020 [US1] Add keyset pagination over `(sort value, instrument id)` to `Repository.Listing` in `server/internal/instruments/repository.go`, with an opaque encoded cursor and null-safe ordering so null statistics sort deterministically (makes T012 green)
- [ ] T021 [US1] Implement `Service.Listing` in `server/internal/instruments/service.go`: validate and default `sort`, `order`, and `limit` against the enumerations in `contracts/openapi.yaml`, and reject anything outside them with `ErrInvalidQuery`
- [ ] T022 [US1] Reconcile the listing query vocabulary with the contract in `server/internal/api/instruments.go` and `src/services/marketData.ts`: rename the `exchange` parameter to `mic` (the client's own `InstrumentSearchParams` already calls it `mic` and rewrites it on the wire), replace the boolean `active` with the `status` enum, and add `sector`; then add the `currency` filter the handler already supports to `contracts/openapi.yaml` so the contract and the router describe the same surface (makes the parameter half of T014 green)
- [ ] T023 [US1] Extend `listInstrumentsHandler` in `server/internal/api/instruments.go` to accept `sort` and `order`, call `Service.Listing`, and serialize `InstrumentListingRow` exactly as `contracts/openapi.yaml` defines it — decimal strings for money, `null` for absent statistics, and the nested `freshness` object (makes T014 green)
- [ ] T024 [US1] Add `fetchInstrumentListing` to `src/services/marketData.ts` returning typed `InstrumentListingRow` values, mapping wire `null` to `null` and never to `0`
- [ ] T025 [US1] Build `src/components/finance/InstrumentTable.vue` on PrimeVue `DataTable` with server-side sort and lazy paging, an explicit absence marker for null statistics, per-row currency, and removable filter chips (makes T016 green)
- [ ] T026 [US1] Add the device-local column preference to `src/stores/instrumentTable.ts`, reading and writing browser storage with a safe fallback to the default column set when storage is unavailable or empty (completes T016)
- [ ] T027 [US1] Rebuild `src/views/MarketsView.vue` around `InstrumentTable.vue`: filter and search controls, an empty state that explains itself and offers a way back, and query state mirrored into the route so it survives reload and back navigation (makes T015 green, FR-006)
- [ ] T028 [US1] Give the Markets list its mobile behavior in `src/components/finance/InstrumentTable.vue` and `src/views/MarketsView.vue`: a single-column card list below 768px leading with identity, price, change, and freshness, with filters and sort in a PrimeVue `Drawer` sheet rather than occupying the list (makes T017 green on `mobile-chromium`)

**Checkpoint**: The universe is browsable, sortable, filterable, and paginated on its own.

---

## Phase 4: User Story 2 - Read one instrument's price history (Priority: P1)

**Goal**: One instrument's stored daily history renders as an aligned candlestick and
volume chart with selectable ranges, zoom, pan, and moving-average overlays — showing the
history that exists and nothing more.

**Independent Test**: Open one instrument with stored history and confirm the candlestick
and volume series render the stored sessions, that range, zoom, pan, and overlays change
what is displayed, and that a gap in the stored history is visible as a gap.

### Failing tests for User Story 2 (MANDATORY) ⚠️

- [ ] T029 [P] [US2] Write the missing-sessions test in `server/internal/instruments/repository_integration_test.go`: the left join of `exchange_sessions` against `daily_price_bars` in data-model.md reports every `open`/`half_day` date with no stored bar, and **never** reports a `closed` date — asserted against the Nordic holiday rows in the fixture. Run it and record the red
- [ ] T030 [P] [US2] Write the history-window test in `server/internal/instruments/repository_integration_test.go`: bars ascending by session, `coverage` reporting the instrument's full first and last stored session independently of the requested window, `series_basis` of `raw` or `provider_adjusted` from whether adjusted closes are present, and `provider`/`observed_at` taken from the most recent bar in range. Run it and record the red
- [ ] T031 [P] [US2] Write the range-resolution test in `server/internal/instruments/service_test.go`: `sessions` counts *stored exchange sessions*, never calendar days (research R7); a window beginning before the first stored session starts at that first session and reports the shortfall; `sessions` outside 2–5000 is rejected. Run it and record the red
- [ ] T032 [P] [US2] Write the history handler test in `server/internal/api/instruments_test.go`: `GET /api/v1/instruments/{id}/history` returns the documented shape, 401 without an active session, and an **identical** 404 body for an unknown identifier and for one the caller may not read (FR-018, SC-010). Run it and record the red
- [ ] T033 [US2] Register `specs/005-instrument-exploration/contracts/openapi.yaml` in the contract list added by T003 in `server/internal/api/contract_test.go`, and remove `"GET /instruments": true` from the `inherited` allowlist now that 005 documents that endpoint — leaving it there would let the test keep exempting the very endpoint this feature changes. Run it and record the red: `/instruments/{id}/history` is documented but not routed
- [ ] T034 [P] [US2] Write `src/components/finance/PriceChart.test.ts`: candlestick and volume series receive exactly the stored bars, a missing session produces a break in the series rather than a bridged segment, and the component emits its visible window on zoom and pan without mutating the bars it was given. Run it and record the red
- [ ] T035 [P] [US2] Write the overlay test in `src/components/finance/movingAverage.test.ts`: a moving average is undefined for any session with fewer than its window of prior stored sessions, starts where it becomes defined rather than at the left edge, and **breaks at a gap instead of spanning it** (FR-012). Run it and record the red
- [ ] T036 [P] [US2] Write `src/components/finance/ChartRangeControls.test.ts`: range selection, zoom in/out, and pan are each operable by keyboard alone with a visible focus indicator, ranges are labelled in sessions or in named periods whose session count is stated (research R7), and no control requires hover. Run it and record the red
- [ ] T037 [P] [US2] Write the chart journey in `e2e/instrument-exploration.spec.ts`: open an instrument, change range, toggle an overlay, zoom and pan by pinch and drag on the touch-enabled projects and by the equivalent buttons and keys elsewhere, and confirm the window never moves outside stored coverage — across all three viewport projects plus a 320x800 legibility check. Run it and record the red
- [ ] T038 [P] [US2] Write the text-equivalence test in `src/views/InstrumentMarketDataView.test.ts`: every value the chart conveys visually — the visible window's first and last session, the coverage range, the count of missing sessions in view, and the latest bar's values — is also present as text (FR-017). Run it and record the red

### Implementation for User Story 2

- [ ] T039 [US2] Implement missing-session detection in `server/internal/instruments/repository.go` using the calendar left join from data-model.md, returning the dates as data rather than a count (makes T029 green)
- [ ] T040 [US2] Implement `Repository.History` in `server/internal/instruments/repository.go`: stored bars in the window ascending, full coverage independent of the window, `series_basis`, `provider`, and `observed_at` — interpolating, forward-filling, and padding nothing (makes T030 green)
- [ ] T041 [US2] Implement `Service.History` in `server/internal/instruments/service.go`: resolve `sessions` and `to` into session-date boundaries counted in stored sessions, clamp a window that begins before the first stored session, and reject out-of-range parameters (makes T031 green)
- [ ] T042a [US2] Trim `x-market-lens-access-boundary.authenticated` in `specs/005-instrument-exploration/contracts/openapi.yaml` to the two operations this contract actually documents (`/instruments` and `/instruments/{id}/history`); `/instruments/{id}` and `/instruments/{id}/prices` stay declared by the contract that defines them
- [ ] T042 [US2] Add `getInstrumentHistoryHandler` to `server/internal/api/instruments.go` and register `GET /api/v1/instruments/{id}/history` on the protected router in `server/internal/api/router.go`, answering an unknown and an unauthorized identifier identically (makes T032 and T033 green)
- [ ] T043 [US2] Add `fetchInstrumentHistory` to `src/services/marketData.ts` returning a typed `HistoryWindow`, preserving `missing_sessions` as dates rather than collapsing them to a count
- [ ] T044 [US2] Implement the moving-average derivation in `src/components/finance/movingAverage.ts`: computed client-side from the bars already loaded, undefined before its window is satisfied, and segmented at every missing session (makes T035 green)
- [ ] T045 [US2] Build `src/components/finance/PriceChart.vue` — the **only** file that imports `lightweight-charts` — creating the candlestick and volume panes, passing theme tokens rather than shipping per-theme stylesheets, and exposing create/update/dispose with an explicit teardown on unmount (makes T034 green)
- [ ] T046 [US2] Feed the chart whitespace or segmented series at every missing session in `src/components/finance/PriceChart.vue` so a gap is drawn as an interruption and no candle spans it (FR-013, completes T034)
- [ ] T047 [US2] Build `src/components/finance/ChartRangeControls.vue` on PrimeVue primitives: session-labelled range buttons, zoom and pan controls with keyboard parity, overlay toggles, and touch targets meeting the minimum size (makes T036 green)
- [ ] T048 [US2] Constrain the visible window to stored coverage in `src/components/finance/PriceChart.vue`, so zoom and pan cannot move outside it and a range change preserves the active overlay selections (FR-011, completes T037)
- [ ] T049 [US2] Rebuild `src/views/InstrumentMarketDataView.vue` around identity, `PriceChart.vue`, and `ChartRangeControls.vue`, stating the coverage range, the window actually shown, and the count of missing sessions in view as text (makes T038 green)
- [ ] T050 [US2] Give the detail view its per-breakpoint layout in `src/views/InstrumentMarketDataView.vue`: full-width chart panel with a legible minimum height and a row of range targets below 768px, identity beside the chart at tablet width, and context panels beside a larger chart at desktop width (completes T037)

- [x] T050a [US2] **Decided: retired.** `fetchDailyPrices` and `InstrumentIdentity.vue` are removed from the client, superseded by the history window and the rebuilt detail view. `GET /api/v1/instruments/{id}/prices` stays: feature 002 owns and documents it, and it remains a valid paginated raw-bar API. Original task: decide and record the fate of `GET /api/v1/instruments/{id}/prices` and `fetchDailyPrices` in `src/services/marketData.ts`, which T049 orphans: either keep both with a test that still exercises them, or retire the client function and leave the endpoint documented by feature 002's contract. Do not leave dead client code behind

**Checkpoint**: One instrument's stored history is readable, interactive, and honest about
what it does not contain.

---

## Phase 5: User Story 3 - Trust what the chart is showing (Priority: P2)

**Goal**: Corporate actions, open quality findings, the raw-or-adjusted basis, and the
provider and observation time are visible in the detail view, so a reader can tell a real
50% move from an unadjusted split.

**Independent Test**: Open an instrument that has a recorded corporate action and an open
quality finding, and confirm both are visible, anchored to their dates, with the displayed
series stating whether it is raw or adjusted.

### Failing tests for User Story 3 (MANDATORY) ⚠️

- [ ] T051 [P] [US3] Write the annotation query test in `server/internal/instruments/repository_integration_test.go`: `corporate_actions` whose `ex_date` falls in the window and `data_quality_findings` touching it are returned with the fields `contracts/openapi.yaml` documents, and a finding outside the window is not. Run it and record the red
- [ ] T052 [P] [US3] Write the annotation serialization test in `server/internal/api/instruments_test.go`: `actions` and `findings` appear in the history response with action type, ratio, amount and currency, or old and new symbol, and with each finding's rule, status, and affected session. Run it and record the red
- [ ] T053 [P] [US3] Write `src/components/finance/ChartAnnotations.test.ts`: an action is marked at its ex-date with type and value **readable without hovering**, an affected session carrying a finding is marked rather than smoothed, and the annotation list is reachable by keyboard. Run it and record the red
- [ ] T054 [P] [US3] Extend `src/views/InstrumentMarketDataView.test.ts`: the view states whether the series is raw or provider-adjusted and names the provider and the observation time of the latest bar (FR-014), and lists open findings with rule, affected sessions, and status (FR-016). Run it and record the red
- [ ] T055 [P] [US3] Extend the journey in `e2e/instrument-exploration.spec.ts`: open the fixture instrument with a recorded split and an open finding, and confirm both are visible without hovering at all three viewports. Run it and record the red

### Implementation for User Story 3

- [ ] T056 [US3] Add the corporate-action and quality-finding queries to `Repository.History` in `server/internal/instruments/repository.go`, scoped to the requested window (makes T051 green)
- [ ] T057 [US3] Serialize `actions` and `findings` in the history response in `server/internal/api/instruments.go` per `contracts/openapi.yaml` (makes T052 green)
- [ ] T058 [US3] Build `src/components/finance/ChartAnnotations.vue`: session markers plus an adjacent always-visible list, so every annotation is readable without hover and reachable by keyboard (makes T053 green)
- [ ] T059 [US3] Render the annotation markers on the chart in `src/components/finance/PriceChart.vue`, keeping the marker API behind the wrapper so nothing outside it depends on the charting library's types
- [ ] T060 [US3] Add the provenance and findings panels to `src/views/InstrumentMarketDataView.vue`: series basis, provider, observation time, and the open findings list with rule, sessions, and status (makes T054 and T055 green)

**Checkpoint**: The chart carries the context that keeps it from being misleading.

---

## Phase 6: User Story 4 - Keep the view current without reloading (Priority: P3)

**Goal**: Committed changes reach open views over the existing authorized stream without a
reload and without losing filters, sort, page, range, zoom, overlays, or scroll position —
including the corporate-action event that does not exist yet.

**Independent Test**: With a detail view open, commit a new bar for the displayed
instrument and confirm the chart updates in place with range, zoom, and overlays intact;
then drop the connection, commit another change, and confirm it arrives on reconnection
exactly once.

### Failing tests for User Story 4 (MANDATORY) ⚠️

- [ ] T061 [US4] Write the corporate-action event test in `server/internal/marketdata/service_integration_test.go`: upserting a corporate action during an import writes a `corporate_action.changed.v1` shared event carrying `instrument_id` and `ex_date`, **in the same transaction**, and a rolled-back import writes neither the action nor the event. Run it and record the red: `upsertAction` records the action silently while every neighbouring write calls `emitEvent`
- [ ] T062 [P] [US4] Write the event-authorization test in `server/internal/api/events_isolation_test.go` extension: `corporate_action.changed.v1` is delivered to any authenticated active user, never to an unauthenticated caller, and carries no private data. Run it and record the red
- [ ] T063 [P] [US4] Write the targeted-invalidation test in `src/services/marketData.test.ts`: a `daily_bar.changed.v1` naming an instrument refreshes only that listing row, a `corporate_action.changed.v1` refreshes only that instrument's annotations, and an event for an instrument outside the current filters changes nothing. Run it and record the red
- [ ] T064 [P] [US4] Write the state-preservation test in `src/views/MarketsView.test.ts` and `src/views/InstrumentMarketDataView.test.ts`: applying a live change preserves filters, sort, page, scroll position, selected range, zoom window, and overlay selections (FR-020). Run it and record the red
- [ ] T065 [P] [US4] Write the delivery-reliability test in `src/services/events.test.ts` extension: a repeated event identifier is applied exactly once, a reconnection resumes from the last applied `Last-Event-ID` and replays only what was missed, and the connected/reconnecting/stale/offline states are reported as they change (FR-021). Run it and record the red
- [ ] T066 [P] [US4] Write the live journey in `e2e/instrument-exploration.spec.ts`: with a detail view open at a custom zoom, commit a bar and confirm the chart incorporates it within five seconds with range, zoom, and overlays intact (SC-009). Run it and record the red

### Implementation for User Story 4

- [ ] T067 [US4] Emit `corporate_action.changed.v1` from `upsertAction` in `server/internal/marketdata/repository.go` through the existing `emitEvent` helper on the import transaction `s.tx`, with `instrument_id` and `ex_date` in the payload (makes T061 green)
- [ ] T068 [US4] Register `corporate_action` in the authorized shared-event type list in `server/internal/api/events.go` and `src/services/events.ts` so the stream forwards it and the client accepts it (makes T062 green)
- [ ] T069 [US4] Add targeted invalidation to `src/services/marketData.ts`: route each event to the listing row or history window its payload names, leaving everything else untouched (makes T063 green)
- [ ] T070 [US4] Apply live changes in place in `src/views/MarketsView.vue` and `src/views/InstrumentMarketDataView.vue` — patching the affected row or window rather than refetching the view — so no view state is reset (makes T064 and T066 green)
- [ ] T071 [US4] Surface the connection state in `src/views/MarketsView.vue` and `src/views/InstrumentMarketDataView.vue` using the existing `MarketDataStatus.vue`, reporting connected, reconnecting, stale, and offline (completes T065)

**Checkpoint**: Open views stay current without polling and without losing the person's place.

---

## Phase 7: Polish & Cross-Cutting Concerns

- [ ] T072 [P] Extend `e2e/accessibility.spec.ts` to cover Markets and Instrument Detail: accessible names, visible focus, AA text contrast in system, light, and dark themes, minimum touch-target size, and the absence of hover-only interaction (SC-007, SC-008)
- [ ] T073 [P] Add the 320-pixel check for both views to `e2e/instrument-exploration.spec.ts`: no horizontal page scrolling and no clipped control at 320x800
- [ ] T074 [P] Add an orientation-change and theme-change assertion to `e2e/instrument-exploration.spec.ts` confirming range, zoom, and scroll position survive both
- [ ] T075 Add the universe-wide honesty check as a Go test in `server/internal/instruments/repository_integration_test.go`: over every instrument in the fixture universe, every session in a returned window exists in `daily_price_bars`, and no returned session is a `closed` calendar day — zero interpolated, forward-filled, or invented sessions (SC-005)
- [ ] T076 Add the coverage check in `server/internal/instruments/repository_integration_test.go`: every recorded corporate action and every open finding falling inside a returned window appears in that window's `actions` or `findings` (SC-006)
- [ ] T077 [P] Assert the listing performance bound in `server/internal/instruments/repository_integration_test.go`: the first page of the full fixture universe returns within the SC-002 budget under every supported sort key. Note in the test that this measures query time, not the end-to-end "typical connection" SC-002 describes
- [ ] T077a Assert the chart performance bound for SC-003 in `e2e/instrument-exploration.spec.ts`: load the fixture instrument with the longest stored history, and confirm it renders and that a zoom and a pan each settle within an explicit budget rather than stalling
- [ ] T077b Assert SC-004 in `server/internal/instruments/repository_integration_test.go`: every instrument in the fixture universe is returned by a listing query searching its own name, its ticker, and its ISIN — so no instrument is unreachable
- [ ] T078 [P] Confirm no file outside `src/components/finance/PriceChart.vue` imports `lightweight-charts`, as a lint rule or a test in `src/components/finance/PriceChart.test.ts`
- [ ] T079 Fill in the *Red-green evidence*, *Honesty matrix*, *Responsive and accessibility evidence*, and *Live delivery evidence* tables in `specs/005-instrument-exploration/quickstart.md` from the runs actually performed
- [ ] T080 Run `make verify`, `npm run test:e2e`, `docker build -t market-lens:local .`, and `docker compose config` and record the results in `specs/005-instrument-exploration/quickstart.md`
- [ ] T081 Update `specs/README.md` and `ROADMAP.md` to move feature 005 to its shipped state with the release it belongs to

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies
- **Foundational (Phase 2)**: Depends on Phase 1 — **blocks every user story**
- **US1 (Phase 3)**: Depends on Phase 2. Independent of US2, US3, US4
- **US2 (Phase 4)**: Depends on Phase 2. Independent of US1 — it reads one instrument by
  identifier and does not need the list to reach it
- **US3 (Phase 5)**: Depends on **US2** — annotations are drawn on the chart US2 builds
- **US4 (Phase 6)**: Depends on Phase 2; its client half needs at least one of US1 or US2
  to have a view to keep current. T061, T067, and T068 (the server event) are independent
  of both and can be done at any point after Phase 2
- **Polish (Phase 7)**: Depends on all four stories

### Within Each Story

Tests are written and observed red → repository/query → service → handler → client service
→ components → view → responsive behavior.

### Parallel Opportunities

- T003 and T004 in Setup
- T007, T008, and T009 in Foundational, after T005 and T006
- All of T010–T012 and T014–T017 in US1, then T013 (same file as T012)
- All of T029–T032 and T034–T038 in US2
- All of T051–T055 in US3
- T062–T066 in US4, after T061
- T072, T073, T074, T077, and T078 in Polish
- **US1 and US2 can be built in parallel by two people** once Phase 2 is complete

---

## Parallel Example: User Story 1

```bash
# Write these together, then run each and record its red before any implementation:
Task: "Freshness derivation test in server/internal/instruments/query_test.go"
Task: "Derived-statistics null-not-zero test in server/internal/instruments/query_test.go"
Task: "Listing handler parameter and 401 test in server/internal/api/instruments_test.go"
Task: "Markets view rendering test in src/views/MarketsView.test.ts"
Task: "Instrument table sort/filter/column-preference test in src/components/finance/InstrumentTable.test.ts"
Task: "Browsing journey across three viewports in e2e/instrument-exploration.spec.ts"
```

---

## Implementation Strategy

### MVP (User Story 1 only)

1. Phase 1 Setup → Phase 2 Foundational
2. Phase 3 US1
3. **Stop and validate**: the universe is browsable, sortable, filterable, and paginated,
   and every statistic that cannot be computed is absent rather than zero
4. Deployable on its own — Markets becomes useful the day it ships

### Incremental Delivery

1. Setup + Foundational → foundation ready
2. US1 → browsable universe → **MVP**
3. US2 → readable price history → the research half of the product exists
4. US3 → the chart becomes trustworthy rather than merely correct-looking
5. US4 → views stay current without a reload

### Notes

- [P] means different files with no dependency on incomplete work
- Verify every test fails for its stated behavioral reason before implementing
- Never weaken, skip, or rewrite a test to obtain green
- Commit after each task or logical group
