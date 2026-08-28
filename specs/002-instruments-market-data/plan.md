# Implementation Plan: Instruments and Daily Market Data

**Branch**: `002-instruments-market-data` | **Date**: 2026-08-28 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/002-instruments-market-data/spec.md`

## Summary

Add the first financial-domain vertical slice to the existing Go/Vue modular monolith:
a migration-seeded Nordic equity universe, exchange-aware identities and calendars, a
provider-neutral daily-market-data importer with EODHD as the first adapter, immutable
import and correction provenance, read-only REST inspection APIs, an accessible
responsive PrimeVue market-data view, an in-process daily update job, and host-side
backfill/retry commands. Feature calculation, signals, charts, backtesting, FX,
portfolios, authentication, and browser-accessible administrative mutations remain out
of scope.

## Technical Context

**Language/Version**: Go 1.26; Vue 3.5 with strict TypeScript 5.6

**Primary Dependencies**: Go standard library (`net/http`, `time`, `log/slog`) and pgx
5.9; Vue Router and PrimeVue 4.5. Add no router, ORM, scheduler, or provider SDK. Use a
small HTTP adapter against EODHD's documented JSON API.

**Storage**: PostgreSQL 18 using ordered embedded SQL migrations; `numeric(20,8)` for
prices and adjustment values, `bigint` for volume, `date` for exchange-local sessions,
and `timestamptz` for events.

**Testing**: Go unit, HTTP contract, provider contract, and PostgreSQL integration tests;
Vitest component/service tests; Playwright inspection journeys; production build,
Docker build, and Compose validation.

**Responsive UI Verification**: Playwright covers search, selection, warning display,
empty/error states, and import status at 360x800, 768x1024, and 1440x900. A dedicated
320x800 test asserts no page-level overflow or clipped controls. Keyboard, touch,
non-hover interaction, orientation/state retention, and all three themes are covered by
component or browser tests.

**Red-Green-Refactor Proof**: Start with a PostgreSQL integration test proving that two
listings sharing ticker text on different MICs persist and resolve independently. Run it
against migration `0001` and capture the expected missing-domain behavior, then add
`0002` and the minimum repository code. Every later slice starts with its own focused
red test: provider mapping, idempotent import, correction history, validation, API, job,
CLI, then UI.

**Database Evolution**:

- `0002_instruments.sql`: exchanges, instruments, provider mappings, research universe,
  memberships, constraints, and search indexes.
- `0003_nordic_universe.sql`: reviewed initial exchanges and exactly 100 instrument/
  reference rows. Subsequent corrections use new forward migrations.
- `0004_market_data.sql`: exchange sessions, import runs/items, current daily bars,
  immutable bar revisions, corporate actions, and quality findings.
- `0005_nordic_calendars.sql`: documented exchange sessions for the ten-year backfill
  window plus the next complete year. Annual extensions and corrections use forward
  migrations.

Migration integration tests exercise both a clean database and an upgrade from `0001`,
including identity, uniqueness, referential, and range constraints.

**Target Platform**: The current Linux container and local macOS/Linux development
workflow; one application process plus PostgreSQL.

**Project Type**: Self-hosted web application with a Go REST/JSON backend, embedded Vue
SPA, in-process background worker, and operational subcommands in the existing binary.

**Performance Goals**: Search/filter 100 instruments within 1 second at the browser;
return a 10-year daily series for one instrument within 2 seconds on the target
self-hosted deployment; ingest 100 instruments × 10 years within 30 minutes subject to
provider throttling; show final run status within 10 seconds of completion.

**Constraints**: Deterministic/idempotent imports; no ticker-only identity; no future
data inference; no invented bars; API token never reaches logs/browser/database; bounded
provider concurrency and retries; graceful worker shutdown; read-only browser API until
authentication is separately specified; 320 CSS-pixel tolerance.

**Scale/Scope**: One user, four Nordic primary exchanges, 100 active equities, about
250,000 daily bars for ten years, one provider adapter, one curated universe, and one
market-data/status frontend view plus instrument summary route.

## Constitution Check

*GATE: Passed before research and re-checked after design.*

| Principle | Design evidence | Result |
|---|---|---|
| Specification-driven | `spec.md` bounds this financial domain slice with independently testable scenarios. | PASS |
| Modular monolith | New packages remain inside the existing Go process; no services or queues are added. | PASS |
| Migration-only evolution | All schema, universe, exchange, and calendar reference data are ordered migrations. Runtime imports use reviewed application code. | PASS |
| Versioned contracts | Read-only routes remain under `/api/v1`; OpenAPI contract and tests are planned. | PASS |
| Correctness/reproducibility | Session dates, calendars, numeric representation, source hashes, revisions, correction rules, and missing-data rules are explicit. | PASS |
| Test-driven development | Each implementation slice has a named behavioral red test and green suite. | PASS |
| Responsive accessible UI | Mobile/tablet/desktop/320 behavior, touch, keyboard, themes, and non-color status semantics are specified. | PASS |
| Operational simplicity | One image plus PostgreSQL; standard timers and host commands; secrets only from environment. | PASS |

Post-design re-check: contracts are read-only; data model keeps all persistent changes
migration-driven; the EODHD adapter is isolated behind a narrow interface; the worker
shares the import service and stops with application context; no gate exception exists.

## Project Structure

### Documentation (this feature)

```text
specs/002-instruments-market-data/
├── spec.md
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   └── openapi.yaml
└── checklists/
    └── requirements.md
```

### Source Code (repository root)

```text
server/
├── cmd/market-lens/main.go             # serve, backfill, and retry entry points
└── internal/
    ├── api/
    │   ├── router.go
    │   ├── instruments.go
    │   └── marketdata.go
    ├── config/config.go
    ├── db/
    │   └── migrations/0002_...0005_*.sql
    ├── instruments/
    │   ├── model.go
    │   ├── repository.go
    │   └── service.go
    ├── marketdata/
    │   ├── model.go
    │   ├── provider.go
    │   ├── repository.go
    │   ├── service.go
    │   ├── validate.go
    │   └── eodhd/client.go
    └── scheduler/marketdata.go

src/
├── components/finance/
│   ├── InstrumentIdentity.vue
│   ├── MarketDataStatus.vue
│   └── QualityBadge.vue
├── services/marketData.ts
├── types/marketData.ts
└── views/
    ├── MarketsView.vue
    └── InstrumentMarketDataView.vue

e2e/market-data.spec.ts
```

Tests live beside Go packages and Vue modules, with PostgreSQL integration helpers under
`server/internal/testdb/`. Provider fixtures contain no licensed production dataset.

**Structure Decision**: Preserve the current frontend-at-root/backend-under-`server`
monorepo. Domain services own behavior, repositories own explicit SQL, API handlers only
decode/validate/respond, and generic response helpers remain in `server/internal/httpx`.

## Implementation Sequence

1. **Identity and migrations**: Red integration test; add `0002`; implement exchange,
   instrument, provider mapping, universe repositories; seed only after validation.
2. **Calendar and market schema**: Red migration/constraint tests; add `0003`–`0005`;
   verify clean and baseline upgrades and official-source traceability.
3. **Provider and validation**: Define the provider contract and deterministic fixtures;
   implement EODHD mapping, rate-limit handling, secret-safe errors, bar/action
   validation, session-gap classification, and cancellation.
4. **Import orchestration**: Implement transactional per-instrument/date scopes,
   idempotent source hashing, immutable correction revisions, partial-run reporting, and
   advisory-lock protection for overlapping imports.
5. **Operations**: Add `marketdata backfill`, `marketdata update`, and `marketdata retry`
   subcommands plus the context-bound daily scheduler. The CLI and scheduler call the
   same service; neither executes SQL directly.
6. **Read-only API and UI**: Add paginated instruments, instrument history, import status,
   and quality endpoints; then implement mobile-first PrimeVue views and states.
7. **Verification**: Relevant Go/Vitest/Playwright suites, `make verify`, production
   build, Docker build, Compose configuration, and a fixture-provider end-to-end import.

## Decisions Required Before Implementation

| Decision | Resolution |
|---|---|
| First provider | EODHD personal All World EOD plan; verify the account's current Nordic entitlements before coding the adapter. |
| Identity | Internal UUID plus exchange/MIC-qualified listing; ticker is searchable but never globally unique; ISIN is not assumed unique across listings. |
| Calendar authority | Versioned application-owned session rows derived from official Nasdaq Nordic and Euronext Oslo calendars. Provider holiday data may assist future updates but is not the historical authority. |
| Adjustment model | Store raw OHLCV, provider adjusted close, discrete split/dividend/symbol-change records, source hash, and immutable prior revisions. Never synthesize adjusted OHLC. |
| Import control | Host-side subcommands and internal scheduling only; browser/API mutation waits for an authentication spec. |
| Concurrency | One advisory lock per provider/instrument/interval; different instruments may import with a small bounded worker pool. |
| Initial universe | Exactly 100 user-reviewed common-equity listings: 25 current primary large-cap benchmark constituents from each of Stockholm, Copenhagen, Helsinki, and Oslo. The migration records its selection date/source; “Danske-purchasable” remains user-curated metadata, not an automated broker claim. |

## Safely Deferred Decisions

- Financial chart library and rich instrument detail visualization.
- Hourly/intraday bars and real-time quotes.
- Additional providers, automatic failover, and source precedence across providers.
- Comprehensive total-return reconstruction if provider action coverage proves
  insufficient.
- FX rates and SEK-normalized valuation.
- Feature storage, strategies, immutable signals, benchmarks, and backtesting.
- Browser administration after single-user authentication is specified.
- Historical point-in-time universe membership and delisted-security coverage beyond
  what the initial source and curated seed can substantiate.

## Complexity Tracking

No constitution violations require justification.
