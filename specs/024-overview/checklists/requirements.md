# Specification Quality Checklist: An Overview That Says What Needs You

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-09-17
**Feature**: [spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs)
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain
- [x] Requirements are testable and unambiguous
- [x] Success criteria are measurable
- [x] Success criteria are technology-agnostic (no implementation details)
- [x] All acceptance scenarios are defined
- [x] Edge cases are identified
- [x] Scope is clearly bounded
- [x] Dependencies and assumptions identified

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification

## Notes

**This specification departs from the product vision, deliberately.** The vision's Overview is a
digest of portfolio value, daily and total change, cash and invested amounts, drawdown, allocation
and performance. Four of those cannot be reported honestly any more — feature 022 tracks no cash and
keeps no equity history, which is the same reason feature 023 offers no drawdown limit — and the
rest are better on the screens that own them. The departure is recorded here rather than left for a
reviewer to notice.

**No open decisions were left for the owner.** The one that would have been worth asking —
"summary of state, or what changed and what needs you" — is settled by the facts above: half the
summary is unreportable. The rest follow from constraints this codebase has already established.

**What a reviewer should push back on if they disagree:**

- **FR-006 bans every value and percentage.** That is a strong constraint and it makes the screen
  less immediately satisfying than a dashboard with a portfolio value on it. The argument for it is
  that every such figure is a second copy of something another screen owns, and this codebase has
  refused second copies at every turn. If that trade is wrong, it is wrong here first.
- **FR-013 bans a new API operation.** Composing six existing reads means six requests where one
  aggregate would do. The argument is that each existing read already enforces its own ownership
  boundary, and an aggregating endpoint would re-derive those distinctions in a second place — where
  the first bug would put a private figure in a shared response.
- **The whole screen could be deleted instead.** If the waiting items were surfaced on the screens
  that own them, the tab would not need to exist. That remains a reasonable position; this
  specification takes the other one because three screens each holding one unread warning is how the
  warnings go unread.

Scope is bounded by exclusion in FR-012 to FR-014, each written so its absence is testable: no
migration, no event type, no endpoint, no computation of its own.
