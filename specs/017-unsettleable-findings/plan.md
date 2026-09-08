# Implementation Plan: A Finding Re-observation Cannot Settle

**Branch**: `017-unsettleable-findings` | **Date**: 2026-09-08 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/017-unsettleable-findings/spec.md`

## Summary

A finding whose condition the source keeps reporting can never satisfy the resolution rule, so
once the scheduled pass began reaching back to re-examine open findings, it began doing so every
night for ever — leaving the run permanently partial and its status meaningless.

The fix is a narrowing, not a mechanism. The reach-back is driven by one predicate,
`status='open'`, in the query that carries each target's oldest unsettled session. A finding that
has been re-examined and re-raised records that fact and stops matching it. It stays `open`,
because that is also the predicate the *resolution* rule uses and the condition must still be able
to end (R-002).

Two smaller pieces make the result honest rather than merely quiet. A rejection matching a finding
already awaiting a decision stops deciding an item's status while still being counted, so a
suppressed badge never suppresses a number. And the pass is bounded regardless of any finding, so
its cost stays predictable when a backfill raises a hundred new ones.

## Technical Context

**Language/Version**: Go 1.26 (backend), TypeScript 5 with Vue 3 (frontend)

**Primary Dependencies**: standard library `net/http`, pgx/PostgreSQL 18, PrimeVue 4. No new
dependency.

**Storage**: PostgreSQL. One ordered migration adding four columns to `data_quality_findings`
with their paired constraints. No new table and, deliberately, no new status value.

**Testing**: Go tests including a migration test, a termination test over successive scheduled
passes, and an authorization test on the new mutation; Vitest; Playwright across the three
viewport projects.

**Responsive UI Verification**: the operational screen gains a report of findings awaiting a
decision with an action on each. 360x800 asserts a finding is readable and its action reachable
without horizontal page scrolling and that an empty list says so; 768x1024 and 1440x900 assert the
same content at the shared width every other report on that screen uses; the 320-pixel floor
asserts nothing clips.

**Live Delivery**: no new event type. A finding's re-examination and its acceptance both publish
the `quality_finding.changed.v1` event that already exists, in the same transaction as the row.

**Identity and Ownership**: findings are shared operational data every authenticated user may
read. Accepting one is an owner action, refused to a member in the service rather than only hidden
in the interface, and attributed to the person who made it.

**PWA and Notifications**: N/A.

**Red-Green-Refactor Proof**: the designated first red is
`TestAReExaminedFindingStopsDrivingTheReachBack`: an import re-validates a finding's session and
raises the same rule again, and the next scheduled pass is asserted not to reach back to that
session. It fails on the requested range — the pass still asks from the finding's session, because
nothing records that it was already examined. A value failure, not a compilation or setup failure.

**Database Evolution**: one ordered migration, `0023_finding_reexamination.sql`. Its test proves a
clean install and an upgrade both arrive with the columns present and null, every existing finding
still open and untouched, the paired constraints in force, and acceptance refused while a finding
is unexamined.

**Target Platform**: Linux container serving the built SPA from the Go process; PostgreSQL 18.

**Performance Goals**: the scheduled pass returns to the observation count of feature 016's
ordinary window within two nights, and never exceeds the stated maximum reach.

**Constraints**: a re-examined finding must still resolve. The product must never record a finding
as accepted without a person. A suppressed status must never suppress a count.

**Scale/Scope**: 27 findings currently open across 26 instruments, the oldest from 2016-11-11.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Assessment |
|---|---|
| **I. Specification-driven** | PASS. Implements a reviewed specification whose five open decisions were resolved before planning, and states its one accepted limitation rather than engineering around it. |
| **II. Modular monolith** | PASS. Changes live in the existing market-data and API packages. No new package, service or infrastructure. |
| **III. Migration-only evolution** | PASS. Four columns arrive as migration `0023`, exercised by a migration test. Existing findings are deliberately not migrated into the new state, because nothing has examined them twice yet. |
| **IV. Versioned contracts** | PASS. One added field set on an existing read and one new owner-only operation, both specified in `contracts/openapi.yaml`. No new event type. |
| **V. Correctness and reproducibility** | PASS. The resolution rule is reused unchanged; this adds a state reached when it declines to fire. The feature *increases* the truthfulness of the operational report, which is the defect it exists to fix. |
| **VI. Test-driven development** | PASS. The first red is behavioural and asserts on a requested range. Each story carries its own reds. The termination property gets a test over successive passes rather than a single one. |
| **VII. PrimeVue-first, accessible, responsive** | PASS. One report and one action in existing components, at every viewport including the 320-pixel floor, with state never carried by colour alone. |
| **VIII. Operational simplicity** | PASS. One environment value with validated bounds, following the pattern `MARKET_DATA_REOBSERVE_SESSIONS` set. No new process or moving part. |
| **IX. Identity, ownership, isolation** | PASS. The mutation is owner-only, enforced in the service and proven by test for a member, an unauthenticated caller and a deactivated one. Attribution is stored. |
| **X. Live updates and consented notifications** | PASS. Both state changes ride the existing durable, versioned, authorization-scoped, resumable event. |

**Post-design re-check**: PASS. Phase 1 adds four columns, one configuration value, one read
filter, one owner-only mutation and one report. Nothing introduces a service, a user-owned record,
a hover-dependent interaction, or a judgement the product makes on a person's behalf.

## Project Structure

### Documentation (this feature)

```text
specs/017-unsettleable-findings/
├── plan.md              # This file
├── spec.md              # Reviewed specification, five decisions resolved
├── research.md          # Phase 0: R-001..R-006
├── data-model.md        # Phase 1: four columns, and the state a reader derives
├── quickstart.md        # Phase 1: the first morning, the bound, and how to check
├── contracts/
│   └── openapi.yaml     # Phase 1: the added fields and the accept operation
├── checklists/
│   └── requirements.md  # Specification quality checklist
└── tasks.md             # Phase 2 output (/speckit-tasks — not created here)
```

### Source Code (repository root)

```text
server/
├── internal/
│   ├── config/
│   │   └── config.go                              # MARKET_DATA_MAX_REACH_SESSIONS, bounded 1..2600
│   ├── db/migrations/
│   │   └── 0023_finding_reexamination.sql         # NEW: four columns and their constraints
│   ├── marketdata/
│   │   ├── repository.go                          # narrow the reach-back predicate; record a
│   │   │                                          # re-examination; accept a finding; status by
│   │   │                                          # rejections not already known
│   │   ├── service.go                             # the accept path and its authorization
│   │   └── *_test.go                              # the first red lives here
│   ├── scheduler/
│   │   └── marketdata.go                          # apply the maximum reach
│   └── api/
│       ├── marketdata.go                          # the added fields and the accept handler
│       └── router.go                              # register the owner-only route
└── cmd/market-lens/
    └── main.go                                    # pass the configured maximum to the scheduler

src/
├── types/marketData.ts                            # awaitingDecision, reexaminedAt, acceptedAt
├── services/marketData.ts                         # the read filter and the accept call
├── components/finance/QualityFindingList.vue      # NEW: what is waiting, and the action
└── views/OperationsView.vue                       # show it beside the run reports

e2e/
└── operations.spec.ts                             # a finding awaiting a decision, at three viewports
```

**Structure Decision**: no new package. The loop is one predicate in a query that already exists,
and the honest-status fix is one condition where an item's status is decided. Introducing a
component around either would hide how small the change is and how narrowly it is scoped.

## Complexity Tracking

No constitution violations require justification. Two choices are less obvious than the
alternative, recorded so a reviewer can challenge them:

| Choice | Why | Simpler alternative rejected because |
|---|---|---|
| Re-examination recorded as columns, not a fourth status | `status='open'` is also the resolution rule's predicate, so a new status satisfies FR-002 by breaking FR-003 — the finding could never resolve again (R-002) | A fourth status reads more naturally, and would have quietly made every re-examined finding permanent |
| A known rejection stops deciding the status but is still counted | An operator needs a nightly status that means what it says *and* a count that reports what happened (FR-007, FR-009) | Suppressing the rejection itself makes the report say something untrue to make a badge look right |
