# Specification Quality Checklist: Personal Portfolio and Holdings

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

Three decisions were put to the owner rather than guessed, because each changes what the product may
honestly claim and none had a defensible default. All three were resolved on 2026-09-17:

- **FR-023, cost basis → first-in-first-out.** What the Nordic tax authorities expect by default, so
  a person's figure has a chance of matching their own filing, and it keeps each holding period's
  result separable — which FR-010 and the re-purchase edge case already required. The cost is that
  every sale must walk the purchase history, and that two purchases on one date need a stored order.
- **FR-024, cash → not tracked.** The product reports profit per position and across positions, and
  reports no portfolio return at all, because it does not know what was paid in. FR-016a makes the
  absence itself a requirement: stated with its reason, never silently missing. This is what
  narrowed User Story 4 from a portfolio-level comparison to a per-holding one, which is both
  honest and, for a handful of names, more informative.
- **FR-025, accounting currency → chosen by the person.** Requires cross rates through the euro,
  which is new arithmetic and two conversions where feature 021 has one. Accepted because a personal
  tracker that reports a Stockholm holder's money in euro is answering a question nobody asked.

The resulting spec has no open markers. The one thing a reviewer should push back on if they
disagree: FR-024 makes this feature deliberately unable to answer "how am I doing overall", which is
the question most people would ask first.

Scope is bounded by exclusion in FR-019 to FR-022, each written so its absence is testable rather
than merely intended. Risk limits and order intents are the rest of Milestone 6 and get their own
specifications; paper trading is Milestone 7.
