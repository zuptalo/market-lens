# Research: Instruments and Daily Market Data

## Decision 1: EODHD behind a provider contract

**Decision**: Implement a small EODHD HTTP adapter for daily history, exchange symbols,
splits, dividends, and symbol changes. Require a paid personal plan whose actual
entitlements are confirmed before implementation; do not rely on the free tier.

**Rationale**: EODHD officially lists Copenhagen (`CO`/`XCSE`), Stockholm (`ST`/`XSTO`),
Oslo (`OL`/`XOSL`), and Helsinki (`HE`/`XHEL`). Its EOD endpoint returns daily OHLC,
adjusted close, and volume with date filtering, and its APIs cover actions and exchange
metadata. The free tier exposes only one year and 20 calls/day; the paid All World EOD
offering advertises longer history. Personal terms fit private self-hosted research, but
redistribution is not assumed.

Primary sources: [supported exchanges](https://eodhd.com/list-of-stock-markets),
[EOD API](https://eodhd.com/financial-apis/api-for-historical-data-and-volumes),
[official OpenAPI](https://github.com/EodHistoricalData/eodhd-openapi),
[pricing](https://eodhd.com/pricing), and
[personal-use terms](https://eodhd.com/financial-apis/terms-conditions).

**Alternatives considered**: Free-provider limits are too restrictive for the target
backfill; unofficial scraping lacks a stable contract/provenance; direct exchange feeds
increase cost and integration complexity. A CSV provider remains test-only.

## Decision 2: Backfill by instrument

**Decision**: Use one date-ranged EOD request per instrument for initial backfill and
bounded per-instrument daily updates. Do not use whole-exchange bulk calls unless measured
quota/cost later favors them.

**Rationale**: The provider documents full symbol history as one call, while its bulk
endpoint costs 100 calls per exchange/day and targets whole exchanges. Symbol requests
are simpler and precisely retryable for 50–100 names. See the
[bulk API](https://eodhd.com/financial-apis/bulk-api-eod-splits-dividends).

**Alternatives considered**: Whole-exchange downloads ingest unwanted records; one call
per historical session is inefficient.

## Decision 3: Application-owned historical exchange sessions

**Decision**: Store exchange-local session rows covering the backfill window and next
complete year, sourced from official calendars and delivered by ordered migrations. Use
`date` as the financial key and store UTC open/close instants separately.

**Rationale**: Provider holiday documentation describes a short rolling historical
window, insufficient for ten-year gap classification. Official schedules include half
days and venue-specific exceptions. IANA zones prevent DST from shifting a session.

Primary sources: [Nasdaq Nordic Market Model](https://www.nasdaq.com/docs/2026/03/13/Nasdaq-Nordic-Market-Model-2026-02.pdf),
[Euronext Oslo calendar](https://live.euronext.com/en/media/1901/download), and
[provider holiday-window documentation](https://eodhd.com/financial-apis-blog/exchange-market-holidays-api).

**Alternatives considered**: Generic weekday rules misclassify closures/half days;
provider-only calendars cannot validate older history.

## Decision 4: Preserve raw values and explicit adjustments

**Decision**: Store raw OHLCV unchanged, provider adjusted close when supplied, and
discrete source-provided actions. Do not derive adjusted OHLC. On a valid correction,
archive the prior row as an immutable revision with its importing run and source hash.

**Rationale**: EODHD documents raw OHLC and a close adjusted for splits/dividends.
Separating them prevents split discontinuities from silently becoming returns while
keeping methodology visible. See [EOD fields](https://eodhd.com/financial-apis/api-for-historical-data-and-volumes)
and [bulk fields](https://eodhd.com/financial-apis/bulk-api-eod-splits-dividends).

**Alternatives considered**: Adjusted-only storage destroys auditability; synthesizing
adjusted OHLC invents observations.

## Decision 5: Exact decimal persistence

**Decision**: Persist prices/ratios as `numeric(20,8)`, parse decimal strings without a
binary floating-point round trip, and expose canonical decimal strings in JSON.

**Rationale**: This prevents avoidable rounding drift and preserves deterministic
re-import comparison without a new decimal dependency.

**Alternatives considered**: Floating point undermines exact comparison; integer minor
units cannot represent varying market precision.

## Decision 6: Internal scheduler and host commands

**Decision**: Use a `time.Timer` that calculates the next configured post-close run and
stops with application context. Provide backfill/update/retry subcommands through the
same service. Default to 20:00 `Europe/Stockholm`, with bounded transient retries.

**Rationale**: One daily job does not justify another dependency. Host commands are safe
with the current unauthenticated public deployment and keep credentials server-side.

**Alternatives considered**: Browser mutation requires authentication; system cron splits
operations from the app lifecycle; a queue violates the modular-monolith principle.

## Decision 7: PostgreSQL advisory locks

**Decision**: Lock by provider/instrument/interval before mutating a scope. Explicitly
report conflicts while a bounded pool processes different instruments concurrently.

**Rationale**: Advisory locks give multi-process safety without new infrastructure.

**Alternatives considered**: In-memory locks fail across replicas/commands; serializing
the universe needlessly slows independent work.

## Decision 8: Read-only REST and responsive inspection

**Decision**: Expose paginated instruments, instrument history, import runs, and quality
findings under `/api/v1`, all GET-only. Use PrimeVue DataTable at larger widths and
stacked result/detail treatments on mobile.

**Rationale**: This proves data usability without exposing provider quota or mutation on
a public unauthenticated deployment.

**Alternatives considered**: Full charting/screening belongs to later features;
browser-side provider calls leak the API token.
