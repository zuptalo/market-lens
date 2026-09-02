# Implementation Plan: Market Data Navigation, Sector Data, and Continuous Listing

**Branch**: `014-market-data-navigation` | **Date**: 2026-09-02 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/014-market-data-navigation/spec.md`

## Summary

Three changes to one screen. Operational reporting — import runs, their outcomes, quality
findings, and the feature engine's runs, which have no interface at all today — moves to a
screen of its own, leaving Market Data as a research tool with a one-line freshness statement
that links onward. The listing loads the next page as the reader scrolls, states how many
instruments are shown and how many match the filter, and says when it has reached the end,
while keeping a focusable control so keyboard and screen-reader users are not stranded in a
list with no end. Sector becomes real data: a vocabulary table, a NOT NULL classification on
every instrument with `unclassified` as a stated value rather than an empty cell, and the
provenance and review date that make a stale classification visible as stale.

The technical shape follows from two constraints. Keyset pagination is retained, so the total is
counted once per filter change rather than once per page (R-001). And curated classification is
carried by migration rather than fetched, because this deployment's market-data plan excludes
fundamentals — which makes a database constraint, not a convention, the thing that stops an
instrument entering the universe unclassified.

## Technical Context

**Language/Version**: Go 1.26 (backend), TypeScript 5 with Vue 3 (frontend)

**Primary Dependencies**: standard library `net/http`, pgx/PostgreSQL 18, Vite, Vue Router,
PrimeVue 4. No new dependency. `IntersectionObserver` is a platform API, not a library.

**Storage**: PostgreSQL. One new reference table (`sectors`), three columns changed or added on
`instruments`. Everything else in this feature is derived at read time or read-only.

**Testing**: Go tests including migration and listing integration tests, Vitest, Playwright
across the mobile, tablet and desktop projects.

**Responsive UI Verification**: 360x800 asserts that scrolling the stacked cards loads a further
page without a tap, that the stated position advances, and that no horizontal page scrolling
appears. 768x1024 asserts continuous loading in the table presentation and that the table's own
horizontal scrolling stays contained. 1440x900 asserts continuous loading across at least three
pages with no row repeated or skipped, and the stated end on arrival. 320 CSS pixels asserts the
position statement neither clips nor wraps into nonsense, and that the focusable next-page
control remains reachable. Keyboard-only and screen-reader paths are asserted through the
focusable control and the polite live region, at every viewport.

**Live Delivery**: No new event type. The client already subscribes to `daily_bar.changed.v1`,
`import_run.changed.v1`, `import_item.changed.v1`, `quality_finding.changed.v1`,
`corporate_action.changed.v1` and `feature_values.changed.v1`. This feature defines behaviour
over multiple loaded pages: a change to a loaded row updates it in place without moving it; a
change that alters membership corrects the stated total and offers a refresh rather than
reordering (R-003). Reconnection, `Last-Event-ID` resumption, duplicate suppression and bounded
coalescing are unchanged and must be proven to still hold with several pages loaded.

**Identity and Ownership**: No user-owned data. The operational screen shows deployment-wide
facts already visible to every authenticated user on Market Data today; moving them must not
widen that audience. Tests assert the new reads are refused to an unauthenticated request and to
a deactivated account, and expose no credential, token or raw provider error.

**PWA and Notifications**: N/A. No installability or notification behaviour changes.

**Red-Green-Refactor Proof**: The designated first red is
`TestTheMarketsListingReportsItsTotalAndPosition` in `server/internal/instruments`: a filter
matching more rows than one page must report the count of matching instruments alongside the
page. It fails on the value — the listing reports rows and a cursor and no total — and must be
written so it compiles against the new field before the query populates it, so the red is
behavioural rather than a compilation error.

**Database Evolution**: One ordered migration, `0020_sector_classification.sql`: create
`sectors`, seed the vocabulary including `unclassified`, classify every instrument in the
curated universe, add `sector_source` and `sector_reviewed_on`, then constrain
`instruments.sector` to NOT NULL with a foreign key and a default of `unclassified`. A migration
test proves a clean install and an upgrade from the current schema both end with every curated
instrument classified and the constraint in force, with no manual step.

**Target Platform**: Linux container serving the built SPA from the Go process; PostgreSQL 18.

**Project Type**: Web application — Go modular monolith with a Vue single-page client.

**Performance Goals**: First page including its total within the two-second bound the listing
already meets; each subsequent page within the same bound; the sector vocabulary read is a
single small table scan.

**Constraints**: Keyset pagination is retained — no numbered pages, no offsets. Loaded rows are
never reordered or removed by a live event. No interaction may depend on hover. The layout
tolerates 320 CSS pixels.

**Scale/Scope**: 100 curated instruments today, one exchange group, four exchanges. The listing
query is written for a universe considerably larger, which is why the total is counted once per
filter change rather than folded into every page.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Assessment |
|---|---|
| **I. Specification-driven** | PASS. This plan implements a reviewed specification whose one open decision was resolved and recorded before planning began. No behaviour here precedes its specification. |
| **II. Modular monolith** | PASS. No new service, no new infrastructure. One Go package gains a read, one gains a column, the client gains a route. |
| **III. Migration-only evolution** | PASS. Sector classification — reference data — arrives as ordered migration `0020`, exercised by a migration test. The plan explicitly forbids populating it by hand, and the NOT NULL constraint makes the unclassified state explicit rather than absent. Reclassification is a forward migration. |
| **IV. Versioned contracts** | PASS. Three REST additions under `/api/v1`, specified in `contracts/openapi.yaml`. No new SSE event type is needed because no new domain state is created: the feature changes how a client applies events it already receives. |
| **V. Correctness and reproducibility** | PASS. The stated total must equal what the same filter returns when counted directly (SC-004), and a full scroll must neither repeat nor omit a row (SC-005). Both are assertions about identity, not about appearance. |
| **VI. Test-driven development** | PASS. The first red is named above and is behavioural. Each story carries its own reds before its implementation. |
| **VII. PrimeVue-first, accessible, responsive** | PASS. The listing keeps its existing PrimeVue presentation; the additions are a live region, a sentinel and a control that already exists. Every viewport has a stated behaviour and an automated scenario, including the 320-pixel floor. The keyboard and screen-reader path is a requirement (FR-008, FR-016) rather than an afterthought, and the "never move loaded rows" rule (FR-018, R-003) exists because moving content under a screen-reader user is disabling. |
| **VIII. Operational simplicity** | PASS. No new infrastructure. The operational screen surfaces sanitized errors only, and asserts it exposes no credential or raw provider response. |
| **IX. Identity, ownership, isolation** | PASS. No user-owned data is introduced. The operational reads are shared, authenticated, and refused to a deactivated account, with tests. |
| **X. Live updates and consented notifications** | PASS. No new event; existing events remain durable, versioned, authorization-scoped and resumable. Tests must prove resumption and duplicate suppression still hold with several pages loaded, which is the new condition this feature creates. |

**Post-design re-check**: PASS. The Phase 1 design adds one reference table, three REST reads
and one client route. Nothing in it introduces a service, an event type, a user-owned record, or
a hover-dependent interaction.

## Project Structure

### Documentation (this feature)

```text
specs/014-market-data-navigation/
├── plan.md              # This file
├── spec.md              # Reviewed specification, decision resolved
├── research.md          # Phase 0: R-001..R-009
├── data-model.md        # Phase 1: sectors, instruments changes, derived total
├── quickstart.md        # Phase 1: what moves, how to verify it
├── contracts/
│   └── openapi.yaml     # Phase 1: listing total, sector vocabulary, feature runs
├── checklists/
│   └── requirements.md  # Specification quality checklist
└── tasks.md             # Phase 2 output (/speckit-tasks — not created here)
```

### Source Code (repository root)

```text
server/
├── cmd/market-lens/
│   └── main.go                          # wire the new reads into the router
└── internal/
    ├── db/migrations/
    │   └── 0020_sector_classification.sql   # NEW: vocabulary, classification, constraint
    ├── instruments/
    │   ├── listing.go                   # count for cursor-less requests; sector join
    │   ├── model.go                     # ListingPage.Total; ListingRow sector code + name
    │   ├── repository.go                # sector vocabulary read
    │   └── listing_test.go              # the first red lives here
    ├── features/
    │   └── repository.go                # ListRuns for the operational screen
    └── api/
        ├── instruments.go               # total in the listing response; /instruments/sectors
        └── features.go                  # /feature-runs

src/
├── router/index.ts                      # /operations route
├── views/
│   ├── MarketsView.vue                  # freshness summary; continuous listing; no operations
│   └── OperationsView.vue               # NEW: imports, items, findings, engine runs
├── components/finance/
│   ├── InstrumentTable.vue              # sentinel, live region, sector name
│   ├── InstrumentFilters.vue            # vocabulary from the server; delete the hardcoded list
│   ├── ListingProgress.vue              # NEW: "Showing 100 of 342", the end, the control
│   ├── MarketDataStatus.vue             # moves to the operations view
│   └── FeatureRunList.vue               # NEW: the engine's runs
├── services/
│   └── marketData.ts                    # total, sector vocabulary, listing state cache
└── types/marketData.ts                  # total, sector code and name, feature run

e2e/
├── instrument-exploration.spec.ts       # continuous scroll, position, end, no repeats
├── accessibility.spec.ts                # keyboard path, live region, 320px floor
└── operations.spec.ts                   # NEW: the operational screen at three viewports
```

**Structure Decision**: The existing web-application layout is unchanged. Backend work is
confined to `internal/instruments` (the listing and the vocabulary), `internal/features` (one
read), `internal/api` (three endpoints) and one migration. Frontend work adds one route, one
view, two components and deletes a hardcoded constant. No new package, module or service.

## Complexity Tracking

No constitution violations require justification. Two choices are deliberately *less* simple
than an obvious alternative, recorded here so a reviewer can challenge them:

| Choice | Why | Simpler alternative rejected because |
|---|---|---|
| Total counted per filter change, not folded into the page query | Keeps a page request's early termination (R-001) | `count(*) OVER ()` is one round trip but forces every page to materialise the whole filtered set — free at 100 rows, wrong at 100,000, and the listing query is already written for the larger case |
| A module-scoped listing cache to restore the reader's place | FR-017: returning from an instrument must not lose the reader's position (R-004) | `<KeepAlive>` preserves the whole component including its live connection and timers, which is more state and less predictable than one explicit, query-keyed cache |
