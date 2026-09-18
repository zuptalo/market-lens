# Specification Quality Checklist: Paper Trading

**Purpose**: Validate specification completeness before planning
**Created**: 2026-09-18
**Feature**: [spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs)
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain — three were raised and all three were resolved by
      the owner before planning: the fill price, the execution trigger, and the source of orders
- [x] Requirements are testable and unambiguous
- [x] Success criteria are measurable
- [x] Success criteria are technology-agnostic
- [x] All acceptance scenarios are defined
- [x] Edge cases are identified — no next bar, insufficient cash, oversized sale, unpriceable
      holding, and a corrected bar under a recorded fill
- [x] Scope is clearly bounded — six exclusions, each written so its absence is testable
- [x] Dependencies and assumptions identified

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification

## Notes

**The exclusion that matters most is FR-024**, no strategy-driven paper account. This feature builds
every part needed for one — orders, fills, a scheduler pass, an account to hold the result — so
after it lands, generating trades from `momentum_trend` is one scheduler change away. Feature 021
measured that strategy and found it lost to two of its three benchmarks over ten years. Stating the
exclusion here means the change cannot be made by accident, only by a reviewed specification that
has to argue for it.

**FR-015 is the one place the product reports a total return.** Feature 022 declines to, because it
tracks no cash. Here cash is tracked and the figure is honest. A reviewer should check that the two
screens never present their figures as comparable — one is a real portfolio with an unknown cash
history, the other is a closed simulation.

**FR-009 is a deliberate divergence from feature 022.** That feature does not track cash and says
so; this one must, because an account that cannot run out of money measures nothing.
