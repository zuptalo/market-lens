# Specification Quality Checklist: Reproducible Backtesting

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-09-16
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

Every number in this specification was probed through the product's own commands before being
written down — the benchmark series, their coverage, and the currency pairs. The one that matters
is the one that would otherwise have become a silent assumption: `OMXC25.INDX` begins 2016-12-19,
110 days after stored history, so a Danish comparison is unavailable for the opening window of any
full-history backtest (FR-020).

Seven decisions were resolved rather than deferred, each recorded under Assumptions:

- **Execution at the next session's open**, because a signal computed from a close cannot honestly
  be acted on at that close.
- **A benchmark per market**, because comparing a Swedish holding with a Norwegian index is a claim
  about a relationship that does not exist.
- **The Danish gap stated, not back-filled** with the index OMXC25 replaced — splicing two series
  and calling the result one is the kind of quiet lie this product exists not to tell.
- **Equal-weight the top N by score**, the simplest rule that uses the strategy's output and
  nothing else; anything cleverer is Milestone 6's position sizing.
- **Non-zero default costs**, because a zero default makes the first result anybody runs the most
  flattering one, and defaults are what people keep.
- **Stored series only**, never fetched during simulation, which is what makes recomputation
  reproducible.
- **Conversion confined to backtesting**; the Markets screens still state every price in its
  listing currency.

Four requirements exist to make the *absence* of behaviour testable — no shorting or leverage
(FR-022), no fitting or parameter search (FR-023), and no order or order intent (FR-024). They are
written as requirements rather than left to the scope paragraph because this is the first feature
in the product whose output somebody might act on, and "we didn't build that" is easier to verify
when a test says so.

The two P1 stories ship together. A backtest without a benchmark is the most misleading artefact
this product could produce, and it would be the default one; either story alone would look
finished and be worse than neither.
