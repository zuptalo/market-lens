# Specification Quality Checklist: Consented Email and Web Push Alerts

**Purpose**: Validate specification completeness before planning
**Created**: 2026-09-18
**Feature**: [spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs)
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain — three were raised and all three resolved by the
      owner before planning: which kinds, the timing, and the granularity of consent
- [x] Requirements are testable and unambiguous
- [x] Success criteria are measurable
- [x] Success criteria are technology-agnostic
- [x] All acceptance scenarios are defined
- [x] Edge cases identified — quiet hours spanning midnight, a gone endpoint, a provider outage, a
      deactivated account with pending notifications, and a shared change with several consenters
- [x] Scope is clearly bounded — five exclusions, each written so its absence is testable
- [x] Dependencies and assumptions identified

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification

## Notes

**FR-021 and FR-022 are the requirements a reviewer should read first.** The owner chose to include
signal-change alerts knowing they edge toward advice. A push saying "NOKIA is now a buy" reads as a
recommendation however it is worded, and feature 021 measured this strategy over ten years and found
it lost to two of its three benchmarks. The mitigation is not care but constraint: the message
states the two views and the strategy's caveat, and the advice-vocabulary guard that already scans
`src/` extends to every template. If that guard is ever narrowed, this is the feature that breaks.

**FR-015 and FR-016 are the other pair worth checking.** A push payload is stored on a third party's
server until the browser collects it, so it carries a kind, a count and a path — nothing about what
somebody owns. An email is more private but still leaves the building, so it may name an instrument
and may not carry a holding or a figure. Both are asserted across every template rather than
reviewed per message.

**FR-006 follows the instance signing key exactly** (migration 0011): self-provisioned,
database-resident, one forever, converging under simultaneous starts via
`INSERT ... ON CONFLICT DO NOTHING` plus an unconditional `SELECT`. The VAPID private key is
unlike `EXTERNAL_CREDENTIAL_KEY` — it encrypts nothing in this database, so keeping it here creates
no circularity, and it means a restored backup keeps working instead of silently invalidating every
subscription.

**The SMTP section already exists** in Account settings and is not rebuilt. This feature adds a test
send to it, because a mail path exercised only by a real alert is one nobody discovers is broken
until it matters.
