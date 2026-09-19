# Specification Quality Checklist: The address a session was last seen from

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-09-19
**Feature**: [spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs)
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain — D1 and D2 were put to the owner and resolved
- [x] Requirements are testable and unambiguous
- [x] Success criteria are measurable
- [x] Success criteria are technology-agnostic (no implementation details)
- [x] All acceptance scenarios are defined
- [x] Edge cases are identified — unresolvable address (FR-003), pre-existing sessions (FR-004),
      forged forwarded header (FR-005), malformed chain (FR-006), IPv6 form (FR-002)
- [x] Scope is clearly bounded — five exclusions, FR-018 to FR-022
- [x] Dependencies and assumptions identified

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification

## Notes

- Two named deployment facts appear in the spec (Traefik, the forwarded header) because the
  decision in D1 cannot be stated without them: which address is *correct* is a property of how
  this product is actually served, not an implementation choice left to planning.
- The retention decision (D2) was taken against the drafted default. The cost it accepts — one
  stored plaintext address per sign-in, kept indefinitely — is written into the spec rather than
  left to be discovered, and a later retention rule is noted as its own schema change.
