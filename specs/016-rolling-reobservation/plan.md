# Implementation Plan: Rolling Re-observation of Recent Sessions

**Branch**: `016-rolling-reobservation` | **Date**: 2026-09-03 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/016-rolling-reobservation/spec.md`

## Summary

The scheduled daily pass asks the source about the last five trading sessions rather than only
the session that just closed, so a restatement published after the fact is corrected through the
path feature 002 already built and feature 013 and 015 already extended. A run reports how many
sessions it corrected, distinctly from how many it stored.

The feature is small because two properties of the existing code make it small, both re-confirmed
in Phase 0 rather than assumed. A source range costs one request per instrument whatever its
width (R-001), so the window is free. And an unchanged re-observation writes nothing, leaving the
bar's `import_run_id` pointing at the run that first stored it (R-002) — and the incremental
feature scope is derived from that column, so a night with no restatement triggers no
recomputation at all. The cascade fires only on a genuine correction.

Almost all of the work is therefore in *asking*: computing a per-exchange window start from the
stored calendar, clamping it per instrument, and counting what came back as corrected rather than
merely stored.

## Technical Context

**Language/Version**: Go 1.26 (backend), TypeScript 5 with Vue 3 (frontend)

**Primary Dependencies**: standard library `net/http`, pgx/PostgreSQL 18, PrimeVue 4. No new
dependency.

**Storage**: PostgreSQL. One ordered migration adding `revised_count` to `import_runs` and
`import_run_items`, with the same shape of constraint the tables already carry. No new table.

**Testing**: Go tests including a migration test, a scheduler window test, and an end-to-end
restatement test that drives a correction through to recomputed features and rescored signals;
Vitest; Playwright across the three viewport projects.

**Responsive UI Verification**: the operational screen's run report gains one labelled count.
360x800 asserts it reads in the stacked layout without horizontal page scrolling and that a run
which corrected nothing does not imply otherwise; 768x1024 and 1440x900 assert the same content
in the tabular layout; the 320-pixel floor asserts nothing clips.

**Live Delivery**: no new event type. The corrected count travels on the run in the existing
`import_run.changed.v1` event and its REST snapshot. The corrections themselves publish the
events they already would — the bar change, the feature recomputation, the signal rescoring.

**Identity and Ownership**: no user-owned data. Import runs are shared operational reference
data; the reads stay behind the authenticated boundary and refused to a deactivated account,
which existing tests assert.

**PWA and Notifications**: N/A.

**Red-Green-Refactor Proof**: the designated first red is
`TestARestatedCloseIsCorrectedByTheNextRoutinePass`: restate one instrument's close three
sessions back in the source, run the scheduled daily pass, and assert the stored bar carries the
restated close and the previous values are archived. It fails on stored data — the bar still
holds the original close and no revision exists — because the pass asked only about the session
that just closed. A value failure, not a compilation or setup failure.

**Database Evolution**: one ordered migration, `0022_revised_session_count.sql`. Its test proves
a clean install and an upgrade both arrive with the columns present, defaulted to zero for runs
that predate the feature, and constrained so a corrected count cannot exceed the processed count.

**Target Platform**: Linux container serving the built SPA from the Go process; PostgreSQL 18.

**Project Type**: Web application — Go modular monolith with a Vue single-page client.

**Performance Goals**: the daily pass keeps its existing budget (R-008). The same number of
provider requests, the same writes on a quiet night, and a few hundred extra rows validated.

**Constraints**: re-observing an unchanged session must write nothing and trigger nothing. The
window must be counted in trading sessions per exchange. A configured window outside its bounds
must be refused at startup, not clamped.

**Scale/Scope**: 100 instruments × 5 sessions per nightly pass, against a store of ~243,000 bars.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Assessment |
|---|---|
| **I. Specification-driven** | PASS. Implements a reviewed specification whose four open decisions were resolved before planning. Scope is stated by exclusion — no intraday, no new source, no change to what a bar is — and R-009 records one adjacent gap it deliberately does not close. |
| **II. Modular monolith** | PASS. Changes live in the existing market-data and scheduler packages. No new package, service or infrastructure. |
| **III. Migration-only evolution** | PASS. Two columns arrive as migration `0022`, exercised by a migration test. Existing runs default to zero, which is the truthful value rather than an unknown. |
| **IV. Versioned contracts** | PASS. One added field on an existing read, specified in `contracts/openapi.yaml`. No new operation, no new event type, no breaking change: a client that ignores the field behaves exactly as before. |
| **V. Correctness and reproducibility** | PASS. This *increases* fidelity to the source: it is the first thing that makes "the stored history matches what the source says today" a claim the product can support. The correction path's determinism is already proven and is reused unchanged. |
| **VI. Test-driven development** | PASS. The first red is behavioural and named, and asserts on stored data. Each story carries its own reds before its implementation. The quiet-night property (R-002) gets a test of its own rather than remaining incidental. |
| **VII. PrimeVue-first, accessible, responsive** | PASS. One labelled count in an existing component, as text, at every viewport including the 320-pixel floor. |
| **VIII. Operational simplicity** | PASS. One environment value with validated bounds, following the pattern `MARKET_DATA_WORKERS` and `MARKET_DATA_MAX_RETRIES` already set. No new process, schedule or moving part. |
| **IX. Identity, ownership, isolation** | PASS. No user-owned data introduced; reads unchanged behind the existing boundary. |
| **X. Live updates and consented notifications** | PASS. No new stream. The added field rides the existing durable, versioned, authorization-scoped, resumable event. |

**Post-design re-check**: PASS. Phase 1 adds two columns, one configuration value, one displayed
count and no new surface. Nothing introduces a service, a user-owned record, a hover-dependent
interaction, or a claim the data cannot support.

## Project Structure

### Documentation (this feature)

```text
specs/016-rolling-reobservation/
├── plan.md              # This file
├── spec.md              # Reviewed specification, four decisions resolved
├── research.md          # Phase 0: R-001..R-009
├── data-model.md        # Phase 1: one added count, and what does not change
├── quickstart.md        # Phase 1: configure, verify, and decide whether five is right
├── contracts/
│   └── openapi.yaml     # Phase 1: the added field on the existing read
├── checklists/
│   └── requirements.md  # Specification quality checklist
└── tasks.md             # Phase 2 output (/speckit-tasks — not created here)
```

### Source Code (repository root)

```text
server/
├── internal/
│   ├── config/
│   │   └── config.go                          # MARKET_DATA_REOBSERVE_SESSIONS, bounded 1..60
│   ├── db/migrations/
│   │   └── 0022_revised_session_count.sql     # NEW: two counts and their constraints
│   ├── marketdata/
│   │   ├── repository.go                      # window start per exchange; first stored session
│   │   │                                      # on the target; upsertBar reports its outcome
│   │   ├── service.go                         # ImportCounts gains Revised
│   │   └── *_test.go                          # the restatement test lives here
│   ├── scheduler/
│   │   └── marketdata.go                      # ask for the window, not one date
│   └── api/
│       └── marketdata.go                      # the count on the run response
└── cmd/market-lens/
    └── main.go                                # pass the configured window to the scheduler

src/
├── types/marketData.ts                        # revised on ImportRunSummary counts
├── services/marketData.ts                     # map it at the boundary
└── components/finance/MarketDataStatus.vue    # state it beside the existing counts

e2e/
└── operations.spec.ts                         # a corrected run says so, at three viewports
```

**Structure Decision**: no new package. The change is a widening of what an existing pass asks
for, plus one number it reports — and the whole reason it is cheap is that everything downstream
of the ask already exists and is tested. Introducing a component would obscure that.

## Complexity Tracking

No constitution violations require justification. Two choices are more elaborate than the
obvious alternative, recorded so a reviewer can challenge them:

| Choice | Why | Simpler alternative rejected because |
|---|---|---|
| `upsertBar` reports inserted/revised/unchanged instead of a `changed` boolean | The corrected count must be persisted with the run that produced it and constrained against its processed count (R-004, FR-010) | Counting `price_bar_revisions` by superseding run afterwards makes a reported figure depend on a second query that can disagree with the run it describes, and cannot be constrained in the schema |
| The window start is computed per exchange from the stored calendar and clamped per instrument | Four exchanges, four holiday calendars; and an instrument listed last week should not be asked about sessions that predate it (R-003, FR-002, FR-004) | Five calendar days reaches a different distance into each exchange and lands differently every holiday week |
