# Specification Quality Checklist: Personal Risk Limits

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-09-17
**Feature**: [spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs)
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain — **three resolved by the owner, 2026-09-17**
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

**The line this specification is built on.** "You should hold no more than 25% in one company" is
advice. "You said 25%, you are at 41%" is somebody's own rule applied. Every decision below follows
from keeping the product on the second side of that line: no default limits (FR-002), no suggested
thresholds, no statement of what would fix a breach (FR-015), and no softening because a strategy
likes the position (FR-016).

Three decisions were put to the owner rather than guessed, and all three were resolved on
2026-09-17:

- **FR-022, limit set → four kinds.** Instrument, sector and market concentration, plus the number
  of holdings. A per-holding volatility gate was measurable and was left out: it is a limit on an
  instrument rather than on a portfolio, and two shapes of rule on one screen makes both harder to
  read.
- **FR-023, breach report → the gap only.** The measured figure, the threshold, and the distance
  between them. Not the value above the threshold, and not what would bring it back within — naming
  an amount is one step from naming a trade, and that belongs to the order-intent feature.
- **FR-024, no breach history.** Evaluation is computed on every read and nothing is stored, so
  changing a limit changes every figure at once and no second copy can drift. The accepted cost is
  stated in the assumptions: a resolved breach leaves no trace.

**What a reviewer should push back on if they disagree.** FR-021 rules out every limit that needs a
portfolio value over time — drawdown above all — because feature 022 values holdings only at their
latest session and tracks no cash. That is the single most conventional risk control, and this
feature cannot offer it honestly. If drawdown matters more than the reasons feature 022 gave for not
tracking cash, that is the decision to revisit, not this specification.

Scope is bounded by exclusion in FR-014 to FR-021, each written so its absence is testable. Order
intents are the next feature; notifications are their own backlog item with their own consent
requirements.
