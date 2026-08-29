# Tasks: Instruments and Daily Market Data

**Input**: Design documents from `specs/002-instruments-market-data/`

**Prerequisites**: `spec.md`, `plan.md`, `research.md`, `data-model.md`,
`contracts/openapi.yaml`, and `quickstart.md`

**Tests**: Tests are mandatory. Every production-code task below is preceded by a
focused automated test task that must be run and observed failing for the expected
behavioral reason. A compile/setup failure is not valid red evidence.

**Responsive UI**: User-facing phases cover 360x800, 768x1024, 1440x900, and a
320x800 overflow/clipping check, with keyboard, touch, non-hover, state-retention, and
system/light/dark theme behavior.

## Phase 1: Setup and External Preconditions

**Purpose**: Confirm the provider assumption and establish deterministic test support
before financial-domain production code begins.

- [x] T001 Verify the configured EODHD account can resolve and retrieve daily history for representative Stockholm, Copenhagen, Helsinki, and Oslo listings, and record entitlement/endpoint evidence without credentials in `specs/002-instruments-market-data/research.md`
- [x] T002 [P] Add license-safe representative provider, calendar, correction, corporate-action, and malformed-record fixtures under `server/internal/marketdata/testdata/`
- [x] T003 [P] Add disposable PostgreSQL integration-test lifecycle helpers in `server/internal/testdb/testdb.go` and `server/internal/testdb/testdb_test.go`, and provide the isolated CI PostgreSQL service through `.github/workflows/ci.yml`
- [x] T004 Update feature 002 to in-progress in `specs/002-instruments-market-data/spec.md`, `ROADMAP.md`, and `specs/README.md` when the first valid red production-behavior test is recorded

---

## Phase 2: Foundational Configuration and Domain Types

**Purpose**: Shared, tested configuration and exact domain representations required by
all four stories.

**Critical**: This phase blocks every user-story implementation phase.

### Failing tests

- [x] T005 [P] Add market-data environment parsing, safe missing-token behavior, schedule, timeout, retry, and bounded-worker assertions to `server/internal/config/config_test.go`; run and record the expected red behavior
- [x] T006 [P] Add exact decimal, exchange-local session date, UUID identity, import status/count, and sanitized-error unit assertions in `server/internal/instruments/model_test.go` and `server/internal/marketdata/model_test.go`; run and record the expected red behavior

### Implementation

- [x] T007 Implement market-data provider and scheduler configuration without logging or returning secrets in `server/internal/config/config.go`
- [x] T008 [P] Implement exchange, instrument, provider mapping, universe, and membership domain types in `server/internal/instruments/model.go`
- [x] T009 [P] Implement daily bar, action, import run/item, revision, and quality-finding domain types with exact decimal/session semantics in `server/internal/marketdata/model.go`

**Checkpoint**: Shared types and configuration are green; no domain data is persisted yet.

---

## Phase 3: User Story 1 - Maintain a Trustworthy Instrument Universe (Priority: P1) MVP

**Goal**: Persist and synchronize exchange-qualified instrument identities without
ticker collisions, destructive removals, or provider coupling.

**Independent Test**: Synchronize representative listings from multiple Nordic MICs,
including repeated ticker text and changed/removed metadata, then retrieve distinct
stable identities and retained inactive history.

### Failing tests for User Story 1

- [x] T010 [US1] Add the required baseline-upgrade integration test for two equal tickers on different MICs plus identity/foreign-key constraints in `server/internal/instruments/repository_integration_test.go`; run against migration `0001` and record the expected missing-domain red result
- [x] T011 [P] [US1] Add repository/service tests for idempotent synchronization, allowed metadata updates, stable UUID preservation, provider mappings, inactive retention, conflicting identity rejection, and an immutable sanitized `universe_sync` run record in `server/internal/instruments/service_test.go`; run and record the expected red behavior
- [x] T012 [P] [US1] Add a migration seed audit proving exactly 100 valid common-equity memberships, 25 per selected Nordic primary exchange, with traceable curation metadata in `server/internal/db/nordic_universe_test.go`; run and record the expected red behavior

### Implementation for User Story 1

- [x] T013 [US1] Add ordered exchange, instrument, provider-instrument, research-universe, and membership schema with search/identity constraints in `server/internal/db/migrations/0002_instruments.sql`
- [x] T014 [US1] Implement explicit-SQL instrument persistence, search primitives, and transactional synchronization in `server/internal/instruments/repository.go`
- [x] T015 [US1] Implement provider-neutral universe synchronization, conflict/inactivation rules, and immutable sanitized run accounting in `server/internal/instruments/service.go`
- [x] T016 [US1] Add the reviewed four-exchange, exactly-100-listing reference universe as a forward migration with selection provenance in `server/internal/db/migrations/0003_nordic_universe.sql`
- [x] T017 [US1] Run the clean-database and `0001` upgrade paths plus the US1 suites, and record green identity, synchronization, and seed evidence in `specs/002-instruments-market-data/quickstart.md`

**Checkpoint**: The instrument universe is independently trustworthy and queryable.

---

## Phase 4: User Story 2 - Build Reproducible Daily Price History (Priority: P1)

**Goal**: Retrieve and persist daily history idempotently, preserve corrections and
adjustment context, and support backfill/incremental operation through shared logic.

**Independent Test**: Import the deterministic fixture twice, apply its correction and
split context, and verify unchanged current history, one immutable correction revision,
calendar-correct sessions, source provenance, and deterministic counts.

### Failing tests for User Story 2

- [x] T018 [P] [US2] Add migration upgrade/constraint tests for sessions, bars, revisions, actions, import runs/items, findings, daily uniqueness, numeric ranges, and referential integrity in `server/internal/db/market_data_migrations_test.go`; run and record the expected red behavior
- [x] T019 [P] [US2] Add a calendar migration audit for official-source traceability, the ten-year window plus next complete year, time-zone correctness, closure/non-session handling, and all four MICs in `server/internal/db/nordic_calendars_test.go`; run and record the expected red behavior
- [x] T020 [P] [US2] Add provider contract tests for instrument resolution, ordered daily bars/actions, pagination overlap, cancellation, rate limiting, retries, and secret-safe failures in `server/internal/marketdata/provider_test.go`; run and record the expected red behavior
- [x] T021 [P] [US2] Add EODHD request/mapping tests using only a local HTTP test server for Nordic symbol mapping, decimals, exchange-local dates, adjusted close, actions, timeouts, and sanitized errors in `server/internal/marketdata/eodhd/client_test.go`; run and record the expected red behavior
- [x] T022 [P] [US2] Add valid-bar/session/action validation unit tests in `server/internal/marketdata/validate_test.go`; run and record the expected red behavior
- [x] T023 [US2] Add import integration tests for first write, identical replay, overlapping pages, corrected bars with immutable revisions, adjustment/action provenance, transactional per-instrument scopes, and advisory-lock conflicts in `server/internal/marketdata/service_integration_test.go`; run and record the expected red behavior
- [x] T024 [P] [US2] Add command tests proving `marketdata backfill` and `marketdata update` parse bounded scopes, use the shared service, respect cancellation, and emit only run IDs/safe totals in `server/cmd/market-lens/main_test.go`; run and record the expected red behavior
- [x] T025 [P] [US2] Add scheduler tests for disabled mode, configured exchange-zone time, one execution per session, shared-service delegation, and graceful shutdown in `server/internal/scheduler/marketdata_test.go`; run and record the expected red behavior

### Implementation for User Story 2

- [x] T026 [US2] Add ordered market-data schema for exchange sessions, runs/items, current daily bars, immutable revisions, corporate actions, and quality findings in `server/internal/db/migrations/0004_market_data.sql`
- [x] T027 [US2] Add application-owned official Nordic exchange sessions for the historical window and next complete year as a forward migration in `server/internal/db/migrations/0005_nordic_calendars.sql`
- [x] T028 [US2] Define the replaceable provider contract and normalized provider records in `server/internal/marketdata/provider.go`
- [x] T029 [US2] Implement the standard-library EODHD HTTP adapter with bounded retry/backoff, cancellation, pagination normalization, and secret sanitization in `server/internal/marketdata/eodhd/client.go`
- [x] T030 [US2] Implement exact OHLCV, session, and source-action validation for accepted records in `server/internal/marketdata/validate.go`
- [x] T031 [US2] Implement explicit-SQL import/run/bar/revision/action repositories, source hashing, transactional upserts, and PostgreSQL advisory locking in `server/internal/marketdata/repository.go`
- [x] T032 [US2] Implement bounded per-instrument backfill/incremental orchestration, idempotency, correction history, partial-scope accounting, and cancellation in `server/internal/marketdata/service.go`
- [x] T033 [US2] Implement `marketdata backfill` and `marketdata update` host subcommands through the shared import service in `server/cmd/market-lens/main.go`
- [x] T034 [US2] Implement the context-bound in-process daily update schedule through the shared import service in `server/internal/scheduler/marketdata.go` and wire it at application startup in `server/cmd/market-lens/main.go`
- [x] T035 [US2] Run US2 provider, migration, import, command, and scheduler suites twice against identical fixtures and record deterministic green evidence in `specs/002-instruments-market-data/quickstart.md`

**Checkpoint**: Daily market history is reproducible without UI or strategy behavior.

---

## Phase 5: User Story 3 - Diagnose Market-Data Health (Priority: P2)

**Goal**: Reject impossible values, flag suspicious/missing data non-destructively, and
make complete, partial, and failed import outcomes safely inspectable and retryable.

**Independent Test**: Import the required malformed/suspicious fixture and verify every
invalid record is rejected, every required suspicious condition is flagged, the last
valid bars survive, and failed scopes expose a sanitized host-side retry command.

### Failing tests for User Story 3

- [x] T036 [P] [US3] Extend validation tests with impossible OHLC, non-positive prices, negative/zero volume, duplicates, ordering, expected-session gaps, suspicious jumps, and action discontinuities in `server/internal/marketdata/validate_test.go`; run and record the expected red behavior
- [x] T037 [US3] Add integration tests for succeeded/partial/failed run transitions, atomic item counts, sanitized failures, unresolved findings, last-valid-value retention, scoped retry lineage, and transaction-coupled durable client events in `server/internal/marketdata/health_integration_test.go`; run and record the expected red behavior
- [x] T038 [P] [US3] Add read-only HTTP contract tests for `/api/v1/market-data/imports`, run detail, quality-finding filters/error envelopes, and `/api/v1/events` SSE IDs/version/shared scope/heartbeat/Last-Event-ID replay/cancellation/slow-consumer behavior in `server/internal/api/marketdata_test.go` and `server/internal/api/events_test.go`; run and record the expected red behavior
- [x] T039 [P] [US3] Add CLI tests proving `marketdata retry --run` reconstructs only failed scopes and never prints credentials/raw provider errors in `server/cmd/market-lens/main_test.go`; run and record the expected red behavior
- [x] T040 [P] [US3] Add frontend service/component tests for recent-run snapshots, duplicate-safe SSE refresh within 10 seconds, Last-Event-ID reconnect, connected/reconnecting/stale/offline state, counts, safe errors, retry command copying, warnings/errors with text plus color, loading, and failure states in `src/services/marketData.test.ts` and `src/components/finance/MarketDataStatus.test.ts`; run and record the expected red behavior
- [x] T041 [US3] Add Playwright failed/partial-import live-update and reconnect journeys at 360x800, 768x1024, and 1440x900 plus keyboard/touch, themes, state retention, no healthy-stream polling, and 320x800 overflow checks in `e2e/market-data.spec.ts`; run and record the expected missing-UI red behavior

### Implementation for User Story 3

- [x] T042 [US3] Implement non-destructive rejection/finding rules and calendar-aware gap/discontinuity classification in `server/internal/marketdata/validate.go`
- [x] T043 [US3] Add `0006_client_events.sql` and implement run/item transition accounting, safe error normalization, finding persistence/filtering, failed-scope reconstruction, and transaction-coupled shared event outbox writes in `server/internal/marketdata/repository.go`, `server/internal/marketdata/service.go`, and `server/internal/events/repository.go`
- [x] T044 [US3] Implement `marketdata retry` through the shared service in `server/cmd/market-lens/main.go`
- [x] T045 [US3] Implement thin import-run/detail and quality-finding handlers plus authorized-scope resumable SSE in `server/internal/api/marketdata.go` and `server/internal/api/events.go`, register them in `server/internal/api/router.go`, and keep generic transport behavior in `server/internal/httpx/`
- [x] T046 [US3] Implement typed read-only snapshots, duplicate-safe EventSource/reconnect handling, and accessible status/quality/live-connection presentation in `src/types/marketData.ts`, `src/services/marketData.ts`, `src/components/finance/MarketDataStatus.vue`, and `src/components/finance/QualityBadge.vue`
- [x] T047 [US3] Integrate the responsive market-data status section, SSE-driven refresh that exposes final outcomes within 10 seconds without primary polling, connected/reconnecting/stale/offline state, and explicit safe host retry instructions in `src/views/MarketsView.vue` and `src/router/index.ts`
- [x] T048 [US3] Make the US3 Go, Vitest, and Playwright suites green and record rejection/flag/retry evidence in `specs/002-instruments-market-data/quickstart.md`

**Checkpoint**: Bad or incomplete data cannot silently masquerade as trustworthy data.

---

## Phase 6: User Story 4 - Inspect Available Market History (Priority: P3)

**Goal**: Search the exchange-qualified universe and inspect latest-known daily values,
coverage, freshness, provenance, warnings, and empty states without implying live data.

**Independent Test**: Search by ticker, name, and ISIN; filter by identity fields; open
an instrument with and without history; verify the session-labelled latest-known value,
coverage, warnings, state retention, and responsive accessibility.

### Failing tests for User Story 4

- [x] T049 [P] [US4] Add repository/service tests for case-insensitive ticker/name/ISIN search, MIC/country/currency/active filters, cursor stability, latest bar, coverage, freshness, warning counts, and empty history in `server/internal/instruments/query_test.go`; run and record the expected red behavior
- [x] T050 [P] [US4] Add read-only HTTP contract tests for `/api/v1/instruments`, instrument detail, and daily prices including validation, pagination, not-found, and decimal JSON behavior in `server/internal/api/instruments_test.go`; run and record the expected red behavior
- [x] T051 [P] [US4] Add frontend API/service tests for typed instrument search, detail, history, cancellation, stale-response suppression, and empty/error results in `src/services/marketData.test.ts`; run and record the expected red behavior
- [x] T052 [P] [US4] Add component tests for exchange-qualified identity, latest-known session wording, native-currency decimals, coverage/freshness, accessible filters, warning disclosure, and empty/loading/error states in `src/components/finance/InstrumentIdentity.test.ts` and `src/views/MarketsView.test.ts`; run and record the expected red behavior
- [x] T053 [US4] Extend Playwright with the complete search/select/back journey, URL/state retention, orientation change, all three themes, keyboard/touch operation, required viewports, and 320x800 overflow checks in `e2e/market-data.spec.ts`; run and record the expected red behavior

### Implementation for User Story 4

- [x] T054 [US4] Implement explicit-SQL search/filter/cursor, latest-bar, coverage, freshness, and quality-summary queries in `server/internal/instruments/repository.go` and `server/internal/marketdata/repository.go`
- [x] T055 [US4] Implement query validation and inspection composition in `server/internal/instruments/service.go` without describing daily bars as real-time quotes
- [x] T056 [US4] Implement thin instrument list/detail/history handlers in `server/internal/api/instruments.go` and register the OpenAPI routes in `server/internal/api/router.go`
- [x] T057 [US4] Complete frontend response types and abortable read-only API access in `src/types/marketData.ts` and `src/services/marketData.ts`
- [x] T058 [US4] Implement reusable accessible `InstrumentIdentity` and latest-known/coverage presentation in `src/components/finance/InstrumentIdentity.vue`
- [x] T059 [US4] Implement mobile-first search/filter/results/detail/empty/error flows in `src/views/MarketsView.vue` and `src/views/InstrumentMarketDataView.vue`, preserving state through `src/router/index.ts`
- [x] T060 [US4] Make the US4 Go, Vitest, and Playwright suites green and verify SC-006/SC-008 language and timing in `specs/002-instruments-market-data/quickstart.md`

**Checkpoint**: The stored universe and history are inspectable end to end at every
required viewport.

---

## Phase 7: Polish, Acceptance, and Protected Delivery

**Purpose**: Prove cross-story correctness, operational safety, and deployability.

- [ ] T061 [P] Add a provider-neutral fixture import acceptance test covering 100-instrument scale, deterministic repeat, correction, action, quality summaries, durable shared events, and resumed duplicate-safe SSE delivery in `server/internal/marketdata/acceptance_integration_test.go`
- [ ] T062 [P] Add secret-regression tests covering configuration errors, provider failures, logs, CLI output, API payloads, and frontend state in `server/internal/marketdata/secrets_test.go`
- [ ] T063 Using only host-provided credentials, run a controlled live EODHD backfill audit for the curated universe, verify at least 95% have the source-provided ten-year/full-available daily history or a recorded limitation, and record only safe aggregate evidence in `specs/002-instruments-market-data/quickstart.md`
- [ ] T064 Reconcile `specs/002-instruments-market-data/contracts/openapi.yaml`, operational configuration, host commands, annual calendar extension rules, provider limitations, and screenshots-free acceptance steps in `README.md`, `server/.env.example`, `docs/`, and `specs/002-instruments-market-data/quickstart.md`
- [ ] T065 Run `make verify`, the full Playwright viewport/theme suite, a versioned production Docker build, `docker compose config --quiet`, `deploy/k8s/test.sh`, migration upgrades, and the fixture-provider import twice; record outcomes in `specs/002-instruments-market-data/quickstart.md`
- [ ] T066 Update feature 002 to in-review in `specs/002-instruments-market-data/spec.md`, `ROADMAP.md`, and `specs/README.md`; run `git diff --check` and confirm no credentials, generated builds, browser output, coverage, or database data are tracked
- [ ] T067 Push `002-instruments-market-data`, open a conventionally titled pull request, wait for `Required checks`, and squash merge only after every acceptance criterion is evidenced
- [ ] T068 Verify the automatic immutable release and all GHCR aliases, then mark feature 002 shipped through the next protected pull request if post-merge evidence requires a documentation update

---

## Dependencies and Execution Order

### Phase Dependencies

- **Setup (Phase 1)** starts immediately. T001 blocks the EODHD adapter task T029 but
  does not block provider-neutral fixtures, schema, or domain work.
- **Foundational (Phase 2)** depends on T002–T003 and blocks every user story.
- **US1 (Phase 3)** begins after Foundation and is the MVP. US2 needs its stable
  instrument/provider identities.
- **US2 (Phase 4)** depends on US1. It establishes the history/import core required by
  US3 and the history portion of US4.
- **US3 (Phase 5)** depends on US2 and may proceed alongside US4 backend query work once
  the import schema/service is green.
- **US4 (Phase 6)** depends on US1 for identity and US2 for history; it does not require
  the US3 UI to prove search/detail independently.
- **Acceptance (Phase 7)** depends on all selected user stories.

### Within Every Behavior Slice

- Run the listed test task first and verify a meaningful behavioral red result.
- Implement only the minimum code/migration required for green.
- Run the focused suite before refactoring and keep it green throughout.
- Never edit an applied migration; corrections and calendar extensions are new forward
  migrations.

### Parallel Opportunities

- T002 and T003 are independent; T005 and T006 can be prepared in parallel.
- US1 service behavior and seed audit tests (T011–T012) touch separate files.
- US2 migration, calendar, provider, adapter, validator, CLI, and scheduler red tests
  (T018–T025) are separable, though implementations retain the listed dependencies.
- US3 transport, CLI, component, and validation red tests can be prepared independently.
- US4 repository, HTTP contract, frontend service, and component red tests can be
  prepared independently before their corresponding implementations.
- Cross-cutting acceptance and secret tests (T061–T062) are independent.

## Parallel Examples

### User Story 1

```text
T011: instrument synchronization behavior tests
T012: exactly-100 reference migration audit
```

### User Story 2

```text
T018: market-data schema migration tests
T019: historical calendar audit
T020: provider-neutral contract tests
T021: EODHD HTTP mapping tests (after T001 evidence)
T022: valid record validation tests
T024: backfill/update command tests
T025: scheduler tests
```

### User Stories 3 and 4

```text
T038: import/quality API contract tests
T040: status frontend tests
T049: instrument query tests
T050: instrument HTTP contract tests
T051–T052: inspection frontend tests
```

## Implementation Strategy

### MVP First

1. Complete Setup and Foundational phases.
2. Complete US1 and independently prove stable exchange-aware identity and retention.
3. Stop for review before the significantly larger provider/import slice.

### Incremental Delivery

1. Identity/universe MVP.
2. Deterministic daily history and operational commands.
3. Observable validation, failures, and retries.
4. Responsive read-only inspection.
5. Cross-story acceptance and protected squash delivery.

The tasks deliberately exclude charts, feature calculation, recommendations, backtests,
portfolios, browser mutations, authentication, hourly/live quotes, and broker execution.
