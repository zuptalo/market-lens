# Research: Market-data fallback provider

## R1 — Production is lapsed now

The scheduled `daily_update` run at 2026-09-29 18:00 UTC ended `failed` with
`provider_authentication` on every item. The previous two nights succeeded. No notice reached the
owner: `scheduler.MarketData.RunDue` called `tellTheOwner` only when `Import` returned an error, and
`Import` returns nil for a run whose items failed. That is FR-004a.

## R2 — The fallback source

Yahoo Finance's chart endpoint, `GET /v8/finance/chart/{symbol}?period1&period2&interval=1d&events=div|split`,
one request per instrument that returns bars, currency, exchange time zone and actions together.

- **Reachability from production.** From the production pod, the request answered `429` without a
  browser-like `User-Agent` and `200` with one. The client therefore sends one.
- **Numbers are binary floats** (`166.60000610351562`). Each value is rounded to the response's
  `priceHint` decimals (bounded to 2–6), which reproduced the stored primary closes exactly.
- **Timestamps** are the session's opening instant; the session date is that instant in
  `exchangeTimezoneName`.
- **No credential**, so nothing is added to the credential store (FR-033).
- Terms of service restrict automated use; accepted by the owner for personal self-hosted use.

## R3 — Mapping evidence (2026-09-29, against production)

For each of the 100 active instruments the Yahoo symbol with the same exchange suffix as the stored
primary symbol (`.CO`, `.HE`, `.OL`, `.ST`) was fetched, and its currency and close on the
instrument's newest stored session compared with the stored bar:

| Result | Count |
|---|---|
| Currency equal and close within 0.5% (all were exact to the stored decimals) | **100** |
| Yahoo's ISIN search returned the same symbol | 92 |

The eight ISIN-search misses are all explained and none is a wrong company: ISIN search returns a
dual-listed company's home listing (Nordea → `NDA-SE.ST`, ABB → `ABBN.SW`, AstraZeneca → `AZN.L`,
Frontline → `FRO`) or nothing (Kesko B, BW LPG, Vår Energi). The close agreement is the stronger
evidence, and it is the audit's rule (FR-011). The migration keys each mapping on ISIN **and**
exchange, because Nordea's ISIN is listed on three of the four exchanges.

Aside: nine Helsinki instruments' newest stored session was 2026-09-25 while the rest had
2026-09-28, although the 09-28 run succeeded. Yahoo has 09-28 for all nine. Not this feature; it
is reported to the owner.

## R4 — What the codebase already gives

- `daily_price_bars.provider` exists and is written on every bar; `upsertBar` archives a replaced
  bar to `price_bar_revisions`. It does **not** compare providers, so a fallback write would
  silently replace a primary bar — the guard FR-006 needs.
- `BeginImportScope` keys its advisory lock on `provider|instrument|interval`, so two providers
  would not exclude each other (FR-010a): the key drops the provider.
- The nightly run re-requests a trailing window (feature 016), so a recovered primary replaces
  fallback bars in that window unaided.
- `paper.nextOpen` reads the first bar after the order whatever its source (FR-026).
- `backtest.loadBars` reads every bar (FR-027).
- `import_runs.kind` is a check constraint; the fallback run is a new kind, `fallback`, whose
  `parent_run_id` is the primary run it covered, so every run's `provider` column stays true.
- A notification kind lives in check constraints on two tables, so adding one is a migration.
