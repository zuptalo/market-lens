# Phase 0 Research: An Overview That Says What Needs You

What the screen can honestly source, and what it must not.

---

## R-001: Every figure the screen needs already has an authorized read

**Finding.** The client already exposes, and the API already serves, every source this feature
requires:

| What the Overview says | Existing read | Scope |
|---|---|---|
| Findings awaiting a decision | `fetchFindingsAwaitingDecision` | shared |
| Limits exceeded, limits unevaluable | `fetchRiskLimits` | private |
| Holdings that could not be valued | `fetchPortfolio` | private |
| Last import, sessions corrected, status | `fetchRecentImports` | shared |
| Last feature computation | `fetchFeatureRuns` | shared |
| Last signal computation | `fetchStrategyRuns` | shared |

**Decision.** The Overview composes these in the client. No new endpoint, no aggregate, no backend
change of any kind.

**Rationale.** Each existing read enforces its own ownership boundary — two of them private, four
shared — and an aggregating endpoint would have to re-derive those distinctions in a second place.
The first bug in that second place puts a private figure into a shared response. Six requests on one
screen is a cost worth paying to keep the boundary in one implementation each.

**Consequence.** The feature adds no migration and no event type, which FR-012 and FR-013 make
testable rather than incidental.

---

## R-002: Half the vision's Overview is now unreportable

**Finding.** The vision lists portfolio value, daily and total change, cash and invested amounts,
drawdown, positions, latest signals, performance, benchmark comparison, allocation and system
status.

Of those, **change**, **performance**, **cash/invested** and **drawdown** cannot be produced without
inventing numbers: feature 022 deliberately tracks no cash, so a return has no denominator, and it
values holdings only at their latest stored session, so no equity history exists. Feature 023
declined a drawdown limit for exactly that reason. **Allocation** is already reported, as limit
contributions on the limits screen.

**Decision.** Build what is left that nothing else answers — what changed, and what needs a person —
and record the departure in the specification rather than silently shipping a shorter version of the
vision's list.

---

## R-003: Counts and dates, never values

**Decision.** Nothing on the screen is a monetary value, a percentage or a return.

**Rationale.** A dashboard is where duplicated figures hide, because a summary is exactly what people
expect there. This codebase has refused second copies consistently: no stored derived figures, one
implementation of what a holding is worth, and a limits screen that links to the portfolio rather
than restating its totals. The Overview is where that discipline is most likely to be abandoned by
somebody adding "just the portfolio value", so the constraint is written down first and asserted by
test.

---

## R-004: Unreadable is not zero

**Decision.** A section whose source fails to load says it could not be read, and does not report
zero.

**Rationale.** The value of this screen rests entirely on "nothing needs you" being trustworthy. A
failed read that renders as zero is indistinguishable from a genuine all-clear, and it fails in the
direction that makes somebody stop looking. This is the same rule the rest of the product already
follows for an unvaluable holding or an unevaluable limit, applied to a network call.

---

## R-005: An empty section is absent, not empty

**Decision.** A section with nothing in it is not rendered. The exception is the waiting section,
which when empty says so in one sentence.

**Rationale.** A heading that always reads "nothing missing" trains a reader to skip the region, and
the day it says something else they skip it too. The waiting section is the exception because its
emptiness is the answer somebody came for.

---

## R-006: Which import statistic to show

**Decision.** Sessions **corrected**, not sessions stored.

**Rationale.** Feature 016 made the correction count first-class precisely because a correction means
every value derived from those sessions moved underneath — features, signals, and anything a person
was looking at yesterday. Sessions stored is a number that is almost always the same and tells
nobody anything.

---

## R-007: Live updates without a new event

**Decision.** The screen subscribes to the event types its sources already publish and re-reads on
any of them.

**Rationale.** Every item it shows is derived from something that already publishes a change:
imports, feature values, signals, findings, portfolios and limits. A new `overview.changed` event
would fire in exactly the same circumstances and carry nothing extra.

---

## R-008: Budget

Six parallel reads, each already measured in its own feature and none of them expensive. There is no
new query and nothing to optimise; a budget test would measure the sum of six things already tested.
Stated here so the omission is a decision rather than a gap.
