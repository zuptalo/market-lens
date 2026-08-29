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
