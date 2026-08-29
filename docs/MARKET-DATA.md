# Market-data operations

Market Lens keeps provider credentials and import mutations on the host. The browser
uses read-only REST snapshots under `/api/v1` and the authorized, resumable
`/api/v1/events` SSE stream for committed shared-data changes.

## Configuration

Copy `server/.env.example` to the ignored `server/.env` for local serving, or supply the
same variables from the host environment. `EODHD_API_TOKEN` requires an EOD Historical
Data plan with the intended Nordic history entitlement. Never put the token in a Docker
build argument, repository file, command output, URL, log, or browser configuration.

The market-data controls are:

- `MARKET_DATA_PROVIDER` (`eodhd`)
- `EODHD_API_TOKEN` (required for provider operations)
- `MARKET_DATA_SCHEDULE_ENABLED` (`false` by default)
- `MARKET_DATA_DAILY_TIME` (`20:00` by default)
- `MARKET_DATA_DAILY_TIMEZONE` (`Europe/Stockholm` by default)
- `MARKET_DATA_REQUEST_TIMEOUT` (`30s` by default)
- `MARKET_DATA_MAX_RETRIES` (`3`, allowed 0–10)
- `MARKET_DATA_WORKERS` (`4`, allowed 1–16)

The optional schedule runs inside the application process, uses the same importer as
the host commands, and stops with application context cancellation. Keep it disabled
unless the deployment is intended to perform automatic post-close updates.

## Host commands

Run from `server/` with `DATABASE_URL` and the provider variables already exported:

```sh
go run ./cmd/market-lens marketdata backfill --universe nordic-liquid-v1 --years 10
go run ./cmd/market-lens marketdata update --universe nordic-liquid-v1 --days 7
go run ./cmd/market-lens marketdata retry --run '<run-uuid>'
```

Backfill accepts 1–30 years; update accepts 1–31 calendar days. Retry reconstructs only
failed scopes from the named parent run. Each command persists an immutable run and
prints only its run ID, final status, and safe totals. Inspect details through the
read-only market-data status UI or API.

## Provider and data limitations

EODHD is isolated behind a provider-neutral contract. A successful ranged response is
stored exactly as supplied; Market Lens does not invent bars before an IPO, after a
delisting, during a suspension, or where the provider has no history. Missing expected
sessions, provider gaps, zero volume, suspicious jumps, and possible corporate-action
discontinuities remain visible quality findings. Invalid records are rejected without
replacing the last valid stored bar.

The curated provider mapping can outlive provider symbol availability. Such a scope is
recorded with a canonical safe error and can be retried after the mapping or entitlement
is reviewed. Raw provider errors and instrument-level live-audit details are not copied
into public documentation.

## Exchange-calendar maintenance

Gap classification uses application-owned exchange sessions, not a generic weekday
rule. Migration `0005_nordic_calendars.sql` covers XSTO, XCSE, XHEL, and XOSL from the
ten-year historical boundary through 2027-12-31, including venue closures and known
half days with source references and exchange-local timestamps.

Before the final covered year starts, review the official Nasdaq Nordic and Euronext
Oslo calendars and add the next complete year in a new ordered forward migration. Add
or correct exceptional closures with another forward migration; never edit an applied
migration or mutate calendar rows manually. Extend the migration audit to prove all
four MICs, official-source traceability, local-date correctness, closures, and the new
coverage boundary.

## Screenshots-free acceptance

Acceptance evidence is behavioral and textual; screenshots are neither required nor
stored. Verify `/markets` search and instrument inspection at 360x800, 768x1024, and
1440x900, plus the 320x800 overflow check, keyboard/touch operation, and system/light/
dark themes. Confirm REST snapshots load first, SSE refreshes committed changes without
primary polling, `Last-Event-ID` resumes missed events without duplicate refreshes, and
offline/reconnecting/stale states preserve current filters and selection.

For operational acceptance, repeat the fixture import to prove idempotency, apply the
fixture correction/action case, inspect safe partial/failure detail and retry commands,
then run the controlled live audit using only host-provided credentials. Record only
run IDs and aggregate counts in the feature quickstart.
