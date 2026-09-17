# Specification Quality Checklist: Order Intents

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-09-18
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

**This specification departs from the product vision in two places, and both are deliberate.**

**The product does not propose intents (FR-013).** The vision describes signals passing through a
risk engine that "may reject or modify a recommendation", which reads as strategy-generated trade
proposals. Feature 021 measured that strategy over ten years: it returned 8.17% annualised against
OMXS30's 8.61% and OBX's 13.96%, with a deeper drawdown and costs of about 3.1% of equity a year.
Handing somebody a list of trades derived from it, in a product that will not say what would close a
limit gap, is the contradiction that would cost them money. An intent here is authored by the person
whose portfolio it would change.

**Risk states rather than rejects (FR-014).** Rejection requires execution to reject into. This
product has no broker and cannot prevent anybody trading; refusing to record what somebody is
considering would only be refusing to let them write it down. In Milestone 7, where the product
controls paper execution, a refusal will mean something.

**Modification is refused outright (FR-012)**, on the same reasoning feature 023 used when it
declined to say what would close a gap: naming the quantity makes the decision.

**What a reviewer should push back on if they disagree:**

- **The whole authorship inversion.** If the intended product really is one that generates trade
  proposals from a strategy, this specification does not build it, and the argument to answer is the
  backtest evidence rather than the design.
- **Sales larger than the position are accepted as intents** while being refused as recorded trades.
  The distinction is that an intent is a thought and a trade is a claim about the past, but somebody
  could reasonably argue an impossible intent is just an error.
- **Intents are not combined.** Two intents that each look fine may together breach a limit. The
  screen says so rather than inventing an order of application, which is honest but leaves a person
  to do that arithmetic themselves.

Scope is bounded by exclusion in FR-013 to FR-017, each written so its absence is testable: no
derived intents, no rejection, no order, no notification.
