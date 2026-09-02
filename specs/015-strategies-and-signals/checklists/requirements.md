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

- [ ] No [NEEDS CLARIFICATION] markers remain
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

Two questions remain open, both deliberately. They are the decisions where a reasonable default
would have been a guess dressed as a requirement:

- **Confidence** (FR-013). The product vision persists a confidence field but never says what it
  means. Any definition is defensible and they measure different things — factor agreement,
  distance from an action boundary, or input completeness — so choosing one silently would put a
  number on every signal that nobody could interpret. Question 1 sets out the three with their
  consequences, including dropping the field until backtesting can give the word evidence.
- **Signal storage grain** (the assumption behind US2 and SC-001). One row per instrument per
  session per version is about 250,000 rows per version over today's history; storing only
  changes is far smaller but makes every point-in-time query "the most recent on or before this
  session", which is easy to get subtly wrong. This is a scope and correctness decision, not a
  storage optimisation, so Question 2 puts it to the reader.

Everything else in the specification stands independently of both answers: the versioning rules,
the no-lookahead requirement, the explanation requirement, the absence rules and the not-advice
requirement do not change whichever way they go.

Two things were tightened during validation:

- An earlier draft let `HOLD` stand for both "the strategy has a view and it is hold" and "no
  view could be formed". FR-009 and FR-012 now separate them, because a HOLD that means "no
  data" is exactly the class of quiet dishonesty feature 013 was built to end.
- The responsive section originally said contributions would be shown "with a bar indicating
  magnitude". It now requires direction and magnitude to be available as text, since a bar's
  length is invisible to a screen reader and unreliable for anyone who cannot compare colours.

Items marked incomplete require spec updates before `/speckit-clarify` or `/speckit-plan`.
