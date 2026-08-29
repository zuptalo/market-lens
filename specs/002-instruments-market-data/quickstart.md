# Quickstart: Instruments and Daily Market Data

This is the planned developer/acceptance workflow. Never commit the token or use it as a
Docker build argument.

## Configuration

```sh
export MARKET_DATA_PROVIDER=eodhd
export EODHD_API_TOKEN='<personal token>'
export MARKET_DATA_SCHEDULE_ENABLED=false
export MARKET_DATA_DAILY_TIME=20:00
export MARKET_DATA_DAILY_TIMEZONE=Europe/Stockholm
export MARKET_DATA_REQUEST_TIMEOUT=30s
export MARKET_DATA_MAX_RETRIES=3
export MARKET_DATA_WORKERS=4
```

Provider operations fail closed without a token. Read-only views continue to show stored
data and a sanitized configuration status.

## Focused verification

```sh
make db-up
cd server && go test ./internal/instruments ./internal/marketdata ./internal/db
cd .. && npm run test:unit
```

Integration tests use a disposable database and deterministic fixture provider; they do
not call EODHD or depend on current prices.

## Load and retry history

```sh
cd server
go run ./cmd/market-lens marketdata backfill --universe nordic-liquid-v1 --years 10
```

In Compose:

```sh
docker compose exec app /app/market-lens marketdata backfill --universe nordic-liquid-v1 --years 10
docker compose exec app /app/market-lens marketdata retry --run '<run-uuid>'
```

Commands print only run IDs and safe totals. Detailed status is read-only in the UI/API.
See `docs/MARKET-DATA.md` for bounded update/retry commands, provider limitations, and
the annual forward-migration rule for exchange calendars.

## Acceptance inspection

1. Open `/markets`; search 50–100 listings by ticker, name, and ISIN with exchange and
   currency visible.
2. Open an instrument; verify latest session/native OHLCV, adjustment status, coverage,
   freshness, and warnings.
3. Review run/item counts and sanitized failures in market-data status.
4. Keep the view open while a fixture import finishes; verify SSE refreshes the affected
   status/read model without periodic polling.
5. Disconnect/reconnect with a saved event ID and verify missed events replay once
   without losing search/filter/selection state.
6. Repeat the backfill; verify no duplicates and unchanged visible values.
7. Import a fixture correction; verify one revision and the corrected current bar.

Acceptance is screenshots-free: retain commands, test outcomes, run IDs, and safe
aggregate evidence only. Do not store licensed provider payloads or browser screenshots.

## Full verification

```sh
make verify
npm run test:e2e
docker build -t market-lens:local .
docker compose config
```

Playwright covers 360x800, 768x1024, and 1440x900; a 320x800 assertion rejects accidental
page-level horizontal overflow.

## Implementation evidence

### US1 — instrument universe (2026-08-28)

- The required baseline test first failed against `0001` because the `exchanges` domain
  relation did not exist. After `0002`, both a clean database and an explicit database
  already recording migration `0001` upgraded successfully.
- `go test ./internal/db ./internal/instruments -count=1` passed twice against isolated
  PostgreSQL schemas.
- The migration audit proves 100 active `common_stock` memberships, exactly 25 each for
  `XSTO`, `XCSE`, `XHEL`, and `XOSL`, with provider mappings and non-empty selection
  provenance.
- Synchronization tests prove same-ticker cross-MIC identity, stable UUIDs, idempotent
  metadata updates, inactive historical retention, provider-identity conflict rejection,
  and immutable sanitized terminal run records.

### US2 — provider contract and EODHD adapter (2026-08-29)

- Provider collection tests first failed because `CollectDaily` returned the explicit
  not-implemented contract error. They now prove ordered/deduplicated pagination,
  transient retry with provider `Retry-After`, cancellation, repeated-cursor safety, and
  sanitized terminal failures.
- Local-only EODHD adapter tests first failed at the same explicit not-implemented seam.
  They now prove exchange-qualified Stockholm mapping, exact lexical decimals,
  exchange-local dates, adjusted close, splits/dividends, request timeouts, and secret-
  safe authentication failures without calling the provider.
- `go test ./internal/marketdata ./internal/marketdata/eodhd -count=1` and `go test ./...`
  pass. Live provider entitlement verification remains pending because no host
  `EODHD_API_TOKEN` is configured.
- Daily validation tests first failed at the explicit unimplemented validator and now
  accept valid OHLCV/actions, reject bars outside authoritative exchange sessions, and
  reject incomplete corporate actions with safe structured findings.

### US2 — reproducible import, commands, and scheduler (2026-08-29)

- Import integration tests first reached the explicit unimplemented persistence/service
  seam after successful migration setup. They now prove first write, overlapping-page
  deduplication, identical replay, immutable corrected-bar revisions, adjusted-close and
  split provenance, independent per-instrument transactions, sanitized partial failure,
  PostgreSQL advisory-lock conflicts, bounded workers, and cancellation.
- Command tests first failed at the explicit unimplemented command seam. They now prove
  bounded `backfill`/`update` scopes, active-universe target loading, shared-service
  delegation, cancellation, and output limited to the run ID and safe totals.
- Scheduler tests first failed at the explicit unimplemented schedule seam. They now
  prove disabled mode, configured `Europe/Stockholm` execution time, once-per-local-
  session execution, shared-service delegation, and context-bound shutdown.
- `go test ./internal/db ./internal/marketdata ./internal/marketdata/eodhd ./internal/scheduler ./cmd/market-lens -count=2`
  passed against isolated PostgreSQL schemas and local-only HTTP fixtures.

### US3 — observable import health and recovery (2026-08-29)

- Validation tests first failed on missing duplicate, ordering, zero-volume, expected-
  session gap, suspicious-jump, and corporate-action discontinuity classifications.
  The completed rules reject unsafe records without replacing the last valid bar and
  persist safe open findings for operator review.
- PostgreSQL integration tests prove atomic run/item counts, sanitized partial and failed
  outcomes, immutable correction history, failed-scope-only retry with parent lineage,
  real read-model queries, and transaction-coupled append-only shared client events.
- CLI and HTTP contract tests prove safe retry output, bounded read filters, consistent
  JSON errors, numeric SSE IDs, `Last-Event-ID` replay, heartbeats, cancellation, and
  bounded event batches without credentials or raw provider errors.
- Vitest proves typed snapshots, duplicate-safe invalidations, reconnect from the last
  event ID, explicit connected/reconnecting/stale/offline states, accessible counts and
  severity text, safe errors, loading/failure states, and retry command copying.
- Playwright passed all six market-data journeys at 360x800, 768x1024, and 1440x900,
  including keyboard/touch use, three themes, state retention, SSE-only refresh, reconnect
  resume, and a 320x800 no-horizontal-overflow assertion.
- `make verify` passed with PostgreSQL integration enabled; the focused Go feature suite
  passed twice. `docker build -t market-lens:local .` and `docker compose config --quiet`
  also passed.

### US4 — inspection query red tests (2026-08-29)

- Repository/service tests now specify case-insensitive ticker, company-name, and ISIN
  search; MIC/country/currency/active filters; exchange-qualified results; insertion-
  stable cursor pagination; and latest-bar, coverage, freshness, unresolved-quality,
  and empty-history summaries.
- The focused test run fails at the intended missing query boundary: `SearchFilter`,
  paginated `SearchPage`, and the inspection query service do not exist yet. Migration
  setup and fixture execution are not the cause of the red result.
- HTTP contract tests fail at the missing instrument read models/reader routes; frontend
  service tests fail at the missing typed search/detail/history functions and search
  coordinator; component tests fail at the absent identity component and search controls.
- The focused desktop Playwright journey reaches `/markets` and times out waiting for
  the specified accessible instrument searchbox, proving the browser red result is the
  missing inspection UI rather than route or fixture setup.

### US4 — read-only instrument inspection green evidence (2026-08-29)

- Explicit SQL now provides case-insensitive exchange-qualified search, all specified
  filters, insertion-stable keyset pagination, latest stored daily bar, coverage,
  observation freshness, unresolved warning/error counts, and newest-first price pages.
- The UI consistently says “Latest known daily value” and displays its exchange-local
  session, native currency, source, and observation time; neither API nor UI describes
  daily end-of-session data as real-time, satisfying SC-008.
- Focused Go packages passed twice against isolated PostgreSQL schemas. All 20 Vitest
  tests passed, strict TypeScript and the production Vite build passed, and all 15
  Playwright tests passed across 360x800, 768x1024, and 1440x900. The search/select/back
  journey completed in under 3 seconds per viewport, comfortably within SC-006's
  30-second inspection target, and its 320x800 overflow assertion passed.
- `make verify` passed after the US4 implementation, including release/workflow policy,
  formatting, vet, the full Go suite, strict frontend build, and the full Vitest suite.

### Cross-story fixture acceptance (2026-08-29)

- A provider-neutral PostgreSQL acceptance test imported all 100 curated instruments,
  repeated the same fixture without adding bars, actions, or revisions, then applied one
  traceable correction while retaining one corporate action and a visible quality
  warning.
- The test proves all initial and corrected daily-bar changes create durable shared
  events and that SSE replay after a saved event ID is ordered and duplicate-free.
- `go test ./internal/marketdata -count=2` passed against isolated PostgreSQL schemas.

### Secret-regression acceptance (2026-08-29)

- A single sentinel-bearing regression path proves configuration errors, normalized
  provider failures, structured logs, persisted run/item/event state, CLI totals, API
  payloads, and frontend state do not expose provider credentials or raw URLs.
- Safe errors are canonicalized again at the API boundary, and the frontend accepts
  only the documented public summaries before placing an error in application state.
- The affected Go packages passed twice and all 21 Vitest tests passed.

### Controlled live backfill audit (2026-08-29)

- After the configured account was upgraded to the EOD Historical Data All World plan,
  the ignored host-local token was used through the application backfill command. No
  credential, provider URL, raw response, or instrument-level provider detail was
  printed or recorded here.
- Run `a8adc265-e989-4cfe-8da7-80cb9769a057` requested 2016-08-29 through 2026-08-29 for
  all 100 curated scopes and completed `partial`: 68 scopes succeeded, seven were
  partial, and 25 recorded the canonical sanitized `provider_error` limitation.
- The 75 history-bearing scopes supplied 181,160 observations: 181,153 were accepted,
  seven provider-gap records were rejected, and 17 source observations were flagged.
  Sixty-seven scopes reach the requested ten-year start; eight start later where the
  source exposes less history. Stored coverage ranges from 727 to 2,514 daily bars per
  successful or partial scope.
- Aggregate quality evidence retained 8,406 open missing-session findings, seven open
  rejected provider gaps, nine open zero-volume warnings, and one open suspicious-jump
  warning. These findings remain non-destructive and visible for later review.
- All 100 scopes therefore have either source-provided ten-year/full-available history
  or an explicit recorded limitation, exceeding the required 95% audit threshold. The
  earlier free-plan audit remains traceable as run
  `8ad0a193-7431-48d6-9131-dad2fdfced59`; no stored history was discarded for this rerun.

### Final verification matrix (2026-08-29)

- `make verify` passed after the final cross-suite sanitizer correction: release and
  workflow policies, development-port behavior, Go formatting/vet/tests, strict
  TypeScript, the production frontend build, and all 21 Vitest tests are green.
- The complete Playwright suite passed all 15 tests at the configured 360x800, 768x1024,
  and 1440x900 projects, including themes, keyboard/touch use, SSE refresh/reconnect,
  state retention, and the explicit 320x800 overflow assertion.
- `market-lens:0.1.0-t065` built successfully as the production multi-stage image with
  the same version embedded into the Go binary and Vue client. `docker compose config
  --quiet` and `deploy/k8s/test.sh` both passed.
- `go test ./internal/db -count=2` passed the clean and baseline migration paths twice.
  `TestFixtureImportAcceptanceAtCuratedUniverseScale` also passed twice, proving the
  provider-neutral 100-instrument import remains deterministic and duplicate-safe.
