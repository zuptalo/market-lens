# Specification Quality Checklist: Rolling Re-observation of Recent Sessions

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

Three decisions the description raised were resolved here rather than deferred, each recorded
under Assumptions with the reasoning that settles it:

- **Window size and unit** — five trading sessions on the instrument's own exchange (FR-002,
  FR-003). Sessions rather than days because the four exchanges keep different holiday calendars,
  and a fixed number of days would reach different distances into each.
- **Run kind** — unchanged (FR-008). Widening what the routine pass observes does not change what
  it is for; a new kind would split the operational report for no reader's benefit.
- **Behaviour after an outage** — the window does not widen (FR-013). Recovering missed history
  stays an explicit operator action, consistent with the rule the product already follows.

Two claims in the description are stated as assumptions rather than requirements, because they
are properties of the current implementation that the plan must re-confirm rather than facts the
specification can assert: that a source range costs one request per instrument regardless of
width, and that an unchanged re-observation performs no write. SC-002 and SC-003 make both
verifiable, so if either has changed the plan will find out before any code is written.

One limitation is stated rather than engineered away: a restatement older than five sessions is
not detected automatically (SC-007). Extending the window to cover it would mean re-asking for a
decade every night to catch something that, past a few days, effectively never happens.
