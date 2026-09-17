# Implementation Plan: An Overview That Says What Needs You

**Branch**: `024-overview` | **Date**: 2026-09-17 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/024-overview/spec.md`

## Summary

Replace a foundation-stage stub that now misstates the product's own state with a screen answering
two questions nothing else does: has anything changed, and does anything need me.

The whole implementation is client-side. Every figure the screen shows already has an authorized
read, each enforcing its own ownership boundary, so the Overview composes rather than computes. That
is the feature's defining property and the source of its main constraint: nothing on it is a value
or a percentage, because every such figure would be a second copy of something another screen owns.

## Technical Context

**Language/Version**: TypeScript 5 with Vue 3. No Go changes at all.

**Primary Dependencies**: PrimeVue 4. No new dependency.

**Storage**: None. No migration, no table, no stored state — asserted by FR-012 and by the existing
migration-count test, which would fail if one were added.

**Testing**: Vitest for the sections and their empty, populated and unreadable states; Playwright
across the three viewport projects. No Go test changes beyond confirming existing suites are
untouched.

**Responsive UI Verification**: 360x800 asserts every waiting item and its link are reachable with
no horizontal page scrolling; 768x1024 and 1440x900 assert the same at the shared content width;
the 320-pixel floor asserts nothing clips. Counts are text, never colour-only badges.

**Live Delivery**: no new event type. The screen subscribes to the types its sources already publish
and re-reads on any of them, because every item it shows derives from something one of those already
reports.

**Identity and Ownership**: composes two private reads (portfolio, limits) and four shared ones. It
introduces no record and no boundary; each read enforces its own, which is the argument for
composing rather than adding an aggregating endpoint.

**PWA and Notifications**: N/A.

**Red-Green-Refactor Proof**: the designated first red is `TestTheOverviewSaysWhatIsWaiting`: with a
finding awaiting a decision and a limit exceeded, the screen names both with counts and links to the
screens that own them. It fails because the stub renders foundation-stage prose — a value failure on
rendered content, not a compilation or setup failure.

**Database Evolution**: N/A, and asserted: the migration bookkeeping test pins the count, so adding
one would fail a test that already exists.

**Target Platform**: Linux container serving the built SPA from the Go process.

**Performance Goals**: six parallel reads already measured in their own features. A budget test here
would sum six things already tested; the omission is recorded in research R-008 rather than left
implicit.

**Constraints**: no value, percentage or return anywhere on the screen; every item links to the
screen that owns it; an unreadable section says so rather than reporting zero; an empty section is
absent; no claim about the product's state that is not true of the running deployment.

**Scale/Scope**: one screen, three sections, six reads.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Assessment |
|---|---|
| **I. Specification-driven** | PASS. A reviewed specification that records where it departs from the product vision and why, rather than quietly shipping a shorter version of it. |
| **II. Modular monolith** | PASS. No new package and no backend change; one view and three components. |
| **III. Migration-only evolution** | PASS by absence, and testable: FR-012 forbids a migration and the existing count test enforces it. |
| **IV. Versioned contracts** | PASS. No new operation, so no contract change. The existing contract test continues to reconcile the router against the reviewed contracts. |
| **V. Correctness and reproducibility** | PASS. The screen computes nothing; every figure is a count or a date read from a source that owns it, and an unreadable source is stated rather than rendered as zero. |
| **VI. Test-driven development** | PASS. The first red is behavioural and asserts on rendered content. The three states of each section — populated, empty, unreadable — each get a test, because the failure that matters is an unreadable section looking like an all-clear. |
| **VII. PrimeVue-first, accessible, responsive** | PASS. Existing components, landmarks with headings, counts as text rather than colour-only badges, and no hover-dependent interaction. |
| **VIII. Operational simplicity** | PASS. Nothing to operate; nothing stored. |
| **IX. Identity, ownership, isolation** | PASS. No new record. Private reads stay private because they are the same reads features 022 and 023 already prove isolated, used unchanged. |
| **X. Live updates and consented notifications** | PASS. No new event type; the screen re-reads on the ones its sources already publish. No notification, which is Notifications-A's subject with its own consent requirements. |

**Post-design re-check**: PASS. Phase 1 adds one view, three components and their tests. Nothing
introduces a migration, an endpoint, an event, a stored figure, or a computed value.

## Project Structure

### Documentation (this feature)

```text
specs/024-overview/
├── plan.md              # This file
├── spec.md              # Reviewed specification, no open decisions
├── research.md          # Phase 0: R-001..R-008
├── quickstart.md        # Phase 1: what the screen says, and how to check it is honest
├── checklists/
│   └── requirements.md  # Specification quality checklist, including where this departs from the vision
└── tasks.md             # Phase 2 output
```

There is no `data-model.md` and no `contracts/` directory. The feature stores nothing and exposes
nothing, and empty documents asserting that would be worse than their absence explained here.

### Source Code (repository root)

```text
src/
├── views/DashboardView.vue                       # REPLACED: the foundation-stage stub
├── components/finance/
│   ├── WaitingList.vue                           # NEW: what needs a person, and where to settle it
│   ├── ChangeList.vue                            # NEW: what moved, and when
│   └── UnknownList.vue                           # NEW: what the product could not determine
└── services/marketData.ts                        # unchanged: every read already exists

e2e/
└── overview.spec.ts                              # NEW: waiting, changed, unknown, and the all-clear
```

**Structure Decision**: three small components rather than one view, because each has the same three
states — populated, empty, unreadable — and testing that shape once per section is what stops the
unreadable case being handled in two of them and forgotten in the third. The view owns the reads and
the composition; the components own nothing but their rendering.

## Complexity Tracking

No constitution violations require justification. Three choices are less obvious than the
alternative, recorded so a reviewer can challenge them:

| Choice | Why | Simpler alternative rejected because |
|---|---|---|
| Six client reads rather than one aggregating endpoint | Each existing read enforces its own ownership boundary; an aggregate would re-derive two private and four shared distinctions in a second place (R-001) | One request is fewer requests, and the first bug in that second place is a private figure in a shared response |
| No value or percentage anywhere | Every such figure is a second copy of something another screen owns, and a dashboard is where that discipline is most likely to be quietly abandoned (R-003) | A portfolio value on the Overview is the single most expected thing on it, and would be wrong the first time it disagreed with the portfolio screen |
| An unreadable section says so rather than showing zero | The screen's value rests on "nothing needs you" being trustworthy, and a failed read rendering as zero fails in the direction that makes somebody stop looking (R-004) | Treating a failure as zero is less code and hides exactly the case the screen exists to surface |
