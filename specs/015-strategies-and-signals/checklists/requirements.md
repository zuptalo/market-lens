# Specification Quality Checklist: Deterministic Strategies and Signals

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-09-02
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

Both open questions were resolved on 2026-09-02 and the reasoning is kept in *Resolved
decisions* rather than discarded:

- **Confidence** means agreement between factors (Option A). Choosing it added FR-013a — a
  signal resting on one available factor may not report the confidence of one where seven agree,
  because unanimity among one factor is not agreement — and a constraint that confidence is
  never described as a probability that the view is correct. The word invites that reading and
  nothing in this feature supports it.
- **Storage grain** is one signal per instrument, session and strategy version (Option A), which
  added FR-008a: a point-in-time question is one lookup, not a search backwards. Roughly 250,000
  rows per version against a feature store already holding 5.8 million.

Two things were tightened during validation:

- An earlier draft let `HOLD` stand for both "the strategy has a view and it is hold" and "no
  view could be formed". FR-009 and FR-012 now separate them, because a HOLD that means "no
  data" is exactly the class of quiet dishonesty feature 013 was built to end.
- The responsive section originally said contributions would be shown "with a bar indicating
  magnitude". It now requires direction and magnitude to be available as text, since a bar's
  length is invisible to a screen reader and unreliable for anyone who cannot compare colours.

Items marked incomplete require spec updates before `/speckit-clarify` or `/speckit-plan`.
