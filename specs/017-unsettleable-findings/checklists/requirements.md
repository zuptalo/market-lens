# Specification Quality Checklist: A Finding Re-observation Cannot Settle

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-09-08
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

Five decisions the description raised were resolved rather than deferred, each recorded under
Assumptions with the reasoning that settles it:

- **How many re-examinations** — one. An identical request producing an identical answer is
  complete information. A threshold of several only delays the same conclusion while paying the
  same nightly cost.
- **Whether the rule varies by finding type** — it does not. The question asked of an absent
  session and of a suspicious value is the same: did asking again change the answer? What differs
  is the operator's judgement afterwards.
- **Whether a known rejection still degrades a run** — it does not (FR-007), because a standing
  known condition is not news about tonight. Every other rejection still does (FR-008), and the
  counts are unchanged either way (FR-009), so suppressing a status never suppresses a number.
- **What bounds the pass** — a stated maximum reach, refused rather than clamped when misconfigured
  (FR-005, FR-006), following the pattern feature 016 established for its window.
- **Where the operator acts** — in the interface, because accepting is a judgement about data
  rather than a computation over it, and the operator is already reading the finding when they
  form it. Computation stays at the command line as the constitution requires.

One limitation is stated rather than engineered away: a source that corrects a years-old session
is not detected automatically, because detecting it costs a years-wide request every night against
an event never observed in this deployment. The explicit backfill remains the way to look, and the
specification says so where an operator will read it.

The central constraint is FR-004: the product may record that re-observation cannot settle a
finding, and must then ask rather than conclude. A machine deciding on its own that a data quality
problem is acceptable would be the worst possible outcome of a feature meant to stop the product
from quietly ignoring one.
