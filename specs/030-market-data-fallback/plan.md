# Implementation Plan: Market-data fallback provider

**Spec**: [spec.md](spec.md) · **Research**: [research.md](research.md) · **Branch**: `030-market-data-fallback`

## Summary

A second `marketdata.Provider` (`marketdata/yahoo`), used only as a child `fallback` run for the
instruments whose primary item failed authentication, and only when the owner has switched it on.
Provenance already lives on every bar; this feature adds the guard that keeps a fallback write off
a primary bar, a derived and once-recorded fallback state with its event, notice and banner, and
the two downstream exclusions (paper fills, backtests).

## Constitution check

- I Spec-driven: this spec authorizes exactly one provider, fallback role only. ✅
- II Monolith: a package inside the existing binary; no service, no queue. ✅
- III Migrations: schema, mapping and kind changes in `0036`. ✅
- IV Contracts: `GET /api/v1/market-data/fallback` snapshot plus `market_data_fallback.changed.v1`. ✅
- V Reproducibility: backtests read primary bars only. ✅
- VI TDD: every task below starts from a red test. ✅
- IX Ownership: shared read, owner-only commands. ✅  X Notifications: opt-in kind, off by default. ✅

## Design

### Data (`0036_market_data_fallback.sql`)

- `market_data_fallback_settings` — singleton: `enabled boolean NOT NULL DEFAULT false`,
  `updated_at`.
- `market_data_fallback_state` — singleton: `active`, `since` (earliest fallback session),
  `instruments` (count whose bars include a fallback bar), `pending_reconciliation`, `changed_at`.
  Written only by `Observe`, which is what makes "told once per change" a state machine.
- `import_runs.kind` gains `fallback`.
- `notification_preferences.kind` / `notifications.kind` gain `market_data_fallback`.
- `provider_instruments` rows for `yahoo`, keyed by ISIN + exchange MIC (research R3).

### Server

- `marketdata/yahoo` — `Client{Name, Resolve, Daily}`. `Daily` returns bars only, fails
  `fallback_action_in_window` when the window holds a split or dividend, rounds to `priceHint`,
  sets adjusted close = close.
- `marketdata`:
  - `FallbackProvider = "yahoo"`; `ImportFallback` kind requiring a parent run.
  - `upsertBar` refuses to replace a non-fallback bar with a fallback one (`barKept`).
  - `BeginImportScope` lock key drops the provider.
  - `Fallback` service: `Enabled/SetEnabled`, `Cover(ctx, primaryRun)` (targets = auth-failed items
    with a yahoo mapping, window after the newest primary bar), `Observe(ctx)` (derive, compare,
    record, emit, raise in one transaction), `State(ctx)` for the snapshot, `ReconcileTargets`.
  - `ClassifyFallbackMapping` — the audit's pure rule.
- `scheduler.MarketData`: after the primary run, tell the owner if it ended failed (FR-004a); run
  `Fallback.Cover`, compute features since the fallback run; `Observe` after every run.
- `paper.nextOpen`: a fallback bar is "no price yet, keep waiting" and is never given up on.
- `backtest.loadBars` and its freshness read: `provider <> 'yahoo'`.
- `notify`: kind `market_data_fallback`, owner-only, detail `{state}` with count; wording.
- `api`: `GET /api/v1/market-data/fallback` (any signed-in person).
- CLI: `marketdata fallback status|enable|disable|audit|reconcile`.

### Client

- `FallbackBanner.vue` in `AppShell` between header and content: PrimeVue `Message`, snapshot plus
  the named event, text states since-when, how many instruments, that paper fills are paused, and
  "reconciliation pending" when the primary is back.
- Notification settings copy for the new kind.

## Out of scope

Staleness trigger, per-figure markers, a web UI for the owner commands, backfilling from the
fallback, corporate actions from the fallback.
