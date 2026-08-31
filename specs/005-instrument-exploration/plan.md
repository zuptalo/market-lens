# Implementation Plan: Instrument Exploration and Financial Charts

**Branch**: `005-instrument-exploration` | **Date**: 2026-08-31 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/005-instrument-exploration/spec.md`

## Summary

Make the curated Nordic universe browsable and one instrument's stored daily history
readable. A Markets list adds price, change, descriptive statistics, and freshness to the
identity search feature 002 already exposes, sorted and paginated in the database. An
Instrument Detail view draws a candlestick and volume chart over the stored sessions, with
moving-average overlays, and surfaces the corporate actions, quality findings, and calendar
gaps that would otherwise make a price series quietly misleading.

Everything read here is shared market data feature 002 already stores, behind the access
boundary feature 004 already enforces. The feature adds no provider integration, no import
control, and no server-side private data. The one write-path change is a versioned event
for corporate actions, which are currently recorded without publishing one.

## Technical Context

**Language/Version**: Go 1.26 (server), TypeScript with Vue 3 and Vite (client).

**Primary Dependencies**: existing — pgx/PostgreSQL, standard-library HTTP, PrimeVue 4,
Vue Router. New — one pinned financial charting library (`lightweight-charts`, see
[research.md](./research.md) R1), wrapped behind a single project component so nothing
else in the client depends on it.

**Storage**: PostgreSQL, reading `instruments`, `exchanges`, `daily_price_bars`,
`exchange_sessions`, `corporate_actions`, and `data_quality_findings`. No new table.

**Testing**: Go tests including PostgreSQL-backed integration tests, Vitest, Playwright.

**Responsive UI Verification**: Playwright journeys across the existing mobile
(360x800), tablet (768x1024), and desktop (1440x900) projects, plus explicit 320x800
checks for the list and the chart. Coverage includes browsing, filtering, sorting, opening
an instrument, changing range, toggling an overlay, pinch and drag zoom on touch, the
keyboard equivalents, orientation change with range and zoom preserved, and system, light,
and dark themes. The accessibility suite added in feature 004 is extended to cover the new
views, so contrast, accessible names, focus visibility, touch-target size, and the absence
of hover-only interaction are measured rather than asserted.

**Live Delivery**: REST loads the initial list and history window. Subsequent committed
changes arrive on the existing authorized SSE stream as the shared-scope
`daily_bar.changed.v1`, `quality_finding.changed.v1`, `import_run.changed.v1`, and
`import_item.changed.v1` events, plus a new `corporate_action.changed.v1` written in the
same transaction as the action it reports (research R5). The client already deduplicates by
event identifier, resumes with `Last-Event-ID`, and reports connected, reconnecting, stale,
and offline. This feature adds targeted invalidation: a bar event names its instrument and
session in the payload, so only the affected row or chart window is refreshed and the
person's filters, sort, range, zoom, overlays, and scroll position survive.

**Identity and Ownership**: No new identity behavior. Every read requires an active session
through the existing protected-by-default boundary, and an unknown instrument identifier is
answered identically to an unauthorized one. All data is shared reference data. The only
per-person state is the optional-column selection, which lives in browser storage on the
device rather than on the server (research R4), so this feature introduces no private
record, no private query scope, and no private event.

**PWA and Notifications**: N/A. Neither is required or changed.

**Red-Green-Refactor Proof**: The first failing test is a PostgreSQL-backed query test
asserting that listing the universe returns each instrument's latest close, prior-session
change, and coverage freshness in a requested sort order. It fails behaviorally because the
existing search returns identity only — no price, no change, no freshness, and no ordering
— rather than failing to compile. Green is proven by the instruments query, API, and view
suites, the chart component suites, and the responsive and live-update journeys.

**Database Evolution**: N/A for this feature's own storage. The derived statistics are
computed at query time (research R2), gaps come from the existing `exchange_sessions`
calendar (research R3), and the column preference is device-local (research R4). The
`corporate_action.changed.v1` event needs no schema change because `client_events` already
accepts any versioned shared event. If implementation discovers a schema need, it must be
introduced as an ordered migration with a clean-install and upgrade test; no manual step is
permitted.

**Target Platform**: Linux container serving the built client from the Go process; Chrome
and Edge on mobile, tablet, and desktop.

**Project Type**: Web application — Go modular monolith with an embedded Vue client.

**Performance Goals**: First page of the universe within two seconds under any supported
sort (SC-002). A full stored history renders and stays interactive under zoom and pan
without visible stalling (SC-003). A committed change reaches an open view within five
seconds (SC-009).

**Constraints**: Nothing may be drawn that is not in stored data — no interpolation, no
forward-fill, no invented session (FR-013, SC-005). No cross-currency comparison, because
the FX history it needs does not exist. Ranges are counted in exchange sessions, never
calendar days (research R7).

**Scale/Scope**: 100 curated instruments with roughly ten years of daily sessions each
(~2,500 bars per instrument, ~250,000 bars total), designed to remain correct as the
universe grows.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Assessment |
| --- | --- |
| I. Specification-driven | Reviewed spec exists and precedes this plan. Every decision here traces to a numbered requirement or a research entry. **Pass** |
| II. Modular monolith | Extends the existing `instruments` and `marketdata` packages and the Vue client. No new service, broker, or infrastructure. **Pass** |
| III. Migration-only evolution | No schema change planned, and the plan forbids resolving any discovered need by hand. **Pass** |
| IV. Explicit versioned contracts | The list and history endpoints are specified in `contracts/openapi.yaml` and reconciled against the router by the contract test feature 004 introduced. The new event is versioned `.v1`. **Pass** |
| V. Correctness and reproducibility | Statistics are computed from stored sessions with stated definitions; gaps come from the authoritative exchange calendar rather than a heuristic; nothing is interpolated. **Pass** |
| VI. Test-driven development | The first red test is named above with its behavioral failure reason. Every production task in `tasks.md` will be preceded by an observed red. **Pass** |
| VII. PrimeVue-first, accessible, responsive | PrimeVue primitives for the table, filters, and controls; a dedicated library only for the chart, which is not a PrimeVue primitive. Mobile-first behavior is specified per breakpoint, with 320-pixel tolerance, touch and keyboard parity, and no hover-only interaction, all measured by the accessibility suite. **Pass** |
| VIII. Self-hosted simplicity | One image plus PostgreSQL, unchanged. One new client dependency, pinned and wrapped. **Pass** |
| IX. Secure identity and isolation | Every read requires an active session; unknown and unauthorized identifiers are indistinguishable; no private server-side data is introduced. **Pass** |
| X. Live updates | Every client-visible committed change publishes a versioned, authorized, transactionally coupled event, including the corporate-action event this plan adds precisely because it was missing. Polling is not the update path. **Pass** |

**Post-Phase-1 re-check**: unchanged. The design added no schema, no private data, and no
new infrastructure, and it removed rather than added a private-data surface (research R4).

## Project Structure

### Documentation (this feature)

```text
specs/005-instrument-exploration/
├── plan.md              # This file
├── research.md          # Phase 0 decisions
├── data-model.md        # Phase 1 read model
├── quickstart.md        # Phase 1 verification and evidence
├── contracts/
│   └── openapi.yaml     # Phase 1 endpoint and event contract
├── checklists/
│   └── requirements.md  # Specification quality checklist
└── tasks.md             # Phase 2 output, created by /speckit-tasks
```

### Source Code (repository root)

```text
server/internal/
├── instruments/
│   ├── model.go          # listing row, history window, gap, annotation types
│   ├── repository.go     # sorted listing with derived statistics; history window with gaps
│   ├── service.go        # bounds, defaults, and range validation
│   └── *_test.go         # query and integration tests, including the first red
├── marketdata/
│   └── repository.go     # corporate_action.changed.v1 emitted in the import transaction
└── api/
    ├── instruments.go    # thin handlers for the listing and history endpoints
    └── *_test.go         # HTTP contract, authorization, and contract-reconciliation tests

src/
├── components/finance/
│   ├── PriceChart.vue           # the only file that knows the charting library
│   ├── InstrumentTable.vue      # sortable, filterable, responsive universe list
│   ├── ChartRangeControls.vue   # range, zoom, and overlay controls with keyboard parity
│   └── *.test.ts
├── services/
│   └── marketData.ts     # typed clients for the listing and history endpoints
├── views/
│   ├── MarketsView.vue           # the universe list
│   └── InstrumentMarketDataView.vue  # identity, chart, actions, findings, coverage
└── stores/               # list query state and device-local column preference

e2e/
├── instrument-exploration.spec.ts  # browsing, charting, live update, responsive journeys
└── accessibility.spec.ts           # extended to cover the new views
```

**Structure Decision**: Extend the existing packages and views rather than introducing new
ones. `MarketsView.vue` and `InstrumentMarketDataView.vue` already exist from feature 002
and are grown in place, which keeps the routes, the SSE wiring, and the authorization
boundary that feature 004 established exactly as they are.

## Complexity Tracking

No constitutional violation requires justification. One decision is worth recording because
it trades away a capability deliberately:

| Decision | Why | Rejected alternative |
| --- | --- | --- |
| Column preference stored per device rather than per account | Avoids introducing this feature's only private table, migration, authorization scope, and private event for a display preference | A `user_view_preferences` table, rejected as disproportionate; the cost is that the choice does not follow a person between devices |
