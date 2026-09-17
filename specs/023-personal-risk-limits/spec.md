# Feature Specification: Personal Risk Limits

**Feature Branch**: `023-personal-risk-limits`

**Created**: 2026-09-17

**Status**: shipped
<!-- Market Lens spec lifecycle: planned → in-progress → in-review → shipped. -->

**Input**: User description: "Risk limits — the second feature of Milestone 6. A person states the
limits they want to be held to, and the product tells them where they stand against those limits,
with the arithmetic. It never invents a limit, never blocks a recording, and never softens one."

## User Scenarios & Testing *(mandatory)*

The product vision puts a risk engine between a signal and an order intent: signals pass through it,
it may reject or modify a recommendation, and it must explain why. That engine cannot be built yet,
because there are no recommendations to reject — order intents are the next feature and paper
trading the one after.

What can be built now is the half that needs nothing proposed: **a person's own limits, evaluated
against what they actually hold**. Feature 022 records holdings and derives what they are worth, by
instrument, by sector and by market. A limit is a sentence about those numbers that its owner wrote
down, and this feature's whole job is to hold them to it and show the arithmetic.

The line this feature must not cross is the one that separates a limit from advice. "You should hold
no more than 25% in one company" is advice, and this product does not give advice. "You said 25%,
you are at 41%, here is the sum" is not advice — it is somebody's own rule, applied. Everything
below follows from that distinction, including the decision to publish no default limits at all.

### User Story 1 - A person writes down the rules they want to be held to (Priority: P1)

Somebody with holdings has no way to say what they consider too much. They may well have a rule in
their head — never more than a quarter in one company, never more than half in one sector — and the
product neither knows it nor could tell them if they broke it.

They state a limit: what it is about, and the threshold. The product stores it, shows it back, and
does not argue with it.

**Why this priority**: Nothing else in this feature exists without it, and it is the whole reason
the product may say anything about risk at all — every figure it reports afterwards is a
consequence of a sentence its owner wrote.

**Independent test**: State a limit as one person and a different one as another; confirm each sees
only their own, stored exactly as entered.

**Acceptance Scenarios**:

1. **Given** an authenticated person, **When** they state a limit, **Then** it is stored against
   them alone and shown back with the threshold they entered.
2. **Given** a person who has stated no limits, **When** they read the screen, **Then** it says so
   plainly and suggests none — not a default, not a recommendation, not a "typical" value.
3. **Given** a stated limit, **When** its owner changes or removes it, **Then** every reported
   figure follows immediately, because nothing about a breach is stored.
4. **Given** two people, **When** either reads or changes a limit, **Then** the other's limits are
   invisible and unaffected in reads, writes and events.
5. **Given** a threshold outside what a limit can mean, **When** it is entered, **Then** it is
   refused with the reason — a concentration above 100% is not a rule, it is a typo.

---

### User Story 2 - The product says where they stand, and shows its working (Priority: P1)

A limit nobody is measured against is a note in a drawer. The point is the comparison, and the
comparison is worth nothing if it cannot be checked.

**Why this priority**: It is the half that does the work, and it ships with User Story 1 for the
reason features 015, 021 and 022 all shipped their pairs together — a product that stores limits
and never evaluates them looks finished and is useless.

**Independent test**: Hold two instruments in known proportion, state a concentration limit one of
them exceeds, and confirm the breach is reported with the holding's value, the portfolio's total,
the percentage they produce, and the threshold it passed.

**Acceptance Scenarios**:

1. **Given** a stated limit and holdings that satisfy it, **When** the person reads it, **Then** it
   is reported as within, with the measured figure beside the threshold.
2. **Given** holdings that exceed a limit, **When** the person reads it, **Then** it is reported as
   exceeded, with the measured figure, the threshold, and the holdings that produced the figure.
3. **Given** any reported figure, **When** a reader checks it, **Then** the values it was computed
   from are on the same screen — a percentage with no numerator and denominator is an assertion.
4. **Given** a breach, **When** it is reported, **Then** the product states the gap and does not
   tell the person what to do about it.
5. **Given** a portfolio with no holdings, **When** limits are evaluated, **Then** each reports that
   there is nothing to measure rather than reporting compliance.

---

### User Story 3 - Missing data cannot make a limit look satisfied (Priority: P2)

A limit is measured against a portfolio's value, and feature 022 already states plainly that a
holding which cannot be priced leaves the total incomplete. A limit evaluated against an incomplete
total would be arithmetic on a number the product itself refuses to state.

**Why this priority**: It is the failure that matters most and shows least. A breach reported as
compliance because one holding had no price is worse than no limit at all: the person believes they
were checked.

**Independent test**: Hold two instruments, remove the stored prices for one, and confirm every
limit whose measurement depends on the portfolio's total reports itself unevaluable with the reason
rather than reporting a figure.

**Acceptance Scenarios**:

1. **Given** a holding that cannot be valued, **When** a limit measured against the portfolio's
   total is evaluated, **Then** it reports that it could not be evaluated, naming the holding and
   the reason.
2. **Given** a limit that does not depend on the missing value, **When** it is evaluated, **Then**
   it still reports normally — one unpriceable holding does not silence every limit.
3. **Given** a limit reported unevaluable, **When** the reader looks at it, **Then** it is visibly
   distinct from both "within" and "exceeded", and never counted as either.
4. **Given** a sector limit and a holding whose sector is `unclassified`, **When** it is evaluated,
   **Then** the unclassified holding is stated rather than silently excluded from every sector.

---

### User Story 4 - Nothing the product does softens a limit (Priority: P2)

A limit belongs to the person who wrote it. Nothing the product thinks should be able to move it —
not a strategy that likes the position, not a backtest that says the method works, and not the
product's own opinion of whether the limit is sensible.

**Why this priority**: This is where a risk feature rots. The pressure to add "but the signal is
strong" or "this is within the usual range" is exactly the pressure this product exists to resist,
and the constraint is cheap to state now and expensive to reintroduce later.

**Independent test**: Hold a position that breaches a limit and carries a BUY signal; confirm the
breach is reported identically to one carrying a SELL, and that no surface qualifies it.

**Acceptance Scenarios**:

1. **Given** a breached limit on an instrument the strategy rates highly, **When** it is reported,
   **Then** it reads exactly as it would for any other instrument.
2. **Given** a recorded trade that puts a portfolio over a limit, **When** it is recorded, **Then**
   it is accepted — a recorded trade is a fact that already happened, and the breach is reported
   afterwards.
3. **Given** any surface showing a limit, **When** it is read, **Then** it states that the limits
   are the person's own and that the product offers no advice.
4. **Given** a completed backtest, **When** a person's limits change, **Then** no stored result
   changes — limits are not applied retrospectively to a simulation that ran without them.

---

### Edge Cases

- **A limit whose threshold the portfolio can never reach.** A 100% concentration limit can only be
  breached by rounding; it is stored and evaluated like any other, because it is not the product's
  place to decide a limit is pointless.
- **A limit stated before any holding exists.** Evaluated as "nothing to measure", not as satisfied.
- **A holding in a sector the product classifies as `unclassified`.** Named in the sector report
  rather than dropped, because a sector limit that quietly ignored a tenth of a portfolio would be
  worse than no sector limit.
- **Every holding unpriceable.** Every total-dependent limit is unevaluable and says so; the count
  of holdings is still measurable, because counting needs no price.
- **A limit removed while it is breached.** The breach disappears, because nothing about it was
  stored. That is the consequence of limits being current state, and the screen says so rather than
  leaving somebody to wonder where the warning went.
- **Two limits of the same kind.** One limit per kind per person: a second threshold for the same
  thing is a contradiction, not a refinement.
- **A position held in an instrument that has left the curated universe.** Its value still counts
  toward the total if it can be priced, because the person still holds it.

## Requirements *(mandatory)*

### Functional Requirements

**Whose limits these are**

- **FR-001**: Every limit MUST carry explicit ownership, enforced in backend services and
  persistence queries, and MUST be invisible to every other person in reads, writes and events.
- **FR-002**: The product MUST publish no default limits and MUST NOT suggest a threshold, a
  "typical" value, or a range. A person with no limits has none.
- **FR-003**: A person MUST be able to state, change and remove their own limits, and every reported
  figure MUST follow immediately.
- **FR-004**: At most one limit of each kind MAY exist per person.

**What a limit can be about**

- **FR-005**: The product MUST support exactly four kinds of limit: the share of a portfolio held in
  one instrument, the share held in one sector, the share held in one market, and the number of
  holdings. No other kind is offered in this version.
- **FR-006**: A threshold MUST be refused when it cannot mean anything for its kind — a share above
  100%, a negative share, or a holding count below zero.
- **FR-007**: The product MUST NOT offer a limit it cannot evaluate from stored data.

**Evaluation**

- **FR-008**: Limits MUST be evaluated against what the person currently holds, valued as feature
  022 values it, and MUST NOT be evaluated against a simulated portfolio.
- **FR-009**: Every evaluation MUST report one of exactly three states: within, exceeded, or
  unevaluable. A limit MUST NOT be reported as within by default.
- **FR-010**: Every evaluation MUST carry the measured figure, the threshold, and the values the
  figure was computed from.
- **FR-011**: A limit whose measurement depends on a value the product could not compute MUST be
  reported unevaluable with the reason, naming what was missing.
- **FR-012**: A limit whose measurement does not depend on a missing value MUST still be evaluated.
- **FR-013**: A holding whose sector is `unclassified` MUST be stated in a sector evaluation rather
  than excluded from it.

**What this feature must not do**

- **FR-014**: The feature MUST NOT block, reject or modify a recorded trade. Feature 022's FR-020
  stands: a recorded trade is a fact its owner asserted, and a breach is reported after it.
- **FR-015**: The feature MUST NOT tell a person what to do about a breach — no quantity to sell, no
  suggested reallocation, no ranked list of what to reduce, and no statement of the value that would
  have to move. It reports the measured figure, the threshold, and the distance between them.
- **FR-016**: A strategy's view of an instrument MUST NOT change how a limit is evaluated or
  reported.
- **FR-017**: The feature MUST NOT change any stored backtest result, and MUST NOT apply a person's
  limits to a simulation that ran without them.
- **FR-018**: Every surface showing a limit MUST state that the limits are the person's own and that
  the product offers no advice.

**Out of scope, stated as requirements so they are testable**

- **FR-019**: The feature MUST NOT produce an order, an order intent, or anything a broker could act
  on. Order intents are the next feature.
- **FR-020**: The feature MUST NOT send a notification. Consented email and Web Push delivery is its
  own backlog item with its own consent requirements.
- **FR-021**: The feature MUST NOT evaluate a limit that needs a portfolio value history — drawdown,
  volatility of the portfolio, or anything else measured over time — because feature 022 values
  holdings only at their latest stored session and no such history exists.

**The three decisions, resolved 2026-09-17**

- **FR-022**: The first version offers exactly four kinds — the share of a portfolio held in one
  instrument, in one sector, in one market, and the number of holdings. All four are measurable from
  what feature 022 already derives, all four are things people have rules about, and each has a
  denominator a reader can check. A per-holding volatility gate was considered and left out: it is
  measurable, but it is a limit on an instrument rather than on a portfolio, and putting a different
  shape of rule on the same screen would make both harder to read.
- **FR-023**: A breach report states the gap and stops — the measured figure, the threshold, and the
  distance between them. It does not state the value above the threshold and does not state what
  would bring the portfolio back within. Naming an amount is one step from naming a trade, and
  naming a trade is the order-intent feature's job, with the review that implies.
- **FR-024**: An evaluation is computed on every read and nothing about it is stored. A limit's
  state is the person's holdings now against their limits now, so changing a limit changes every
  figure immediately and no second copy can drift — the discipline feature 022 applies to positions
  and profit, applied here for the same reason. The accepted cost is that the product cannot say
  when a breach began, or that one happened at all once it has resolved.

### Test-First Proof *(mandatory)*

- **Initial failing test**: `TestABreachIsReportedWithTheArithmeticBehindIt` — a person holds two
  instruments in a known proportion and states a concentration limit one of them exceeds; the
  evaluation reports it exceeded, with the holding's value, the portfolio total, the percentage and
  the threshold.
- **Expected red reason**: No limit can be stated, so nothing is evaluated and the reported state is
  empty. A value failure on stored data, not a compilation or setup failure.
- **Green evidence**: The risk suite, plus the existing portfolio, strategy, backtest and
  market-data suites unchanged — this feature reads holdings and reference data and must alter
  neither.
- **Database migration proof**: Limits are new stored facts and user-owned. They arrive as an
  ordered migration, with a test proving a clean install and an upgrade both arrive with the table
  present, ownership not nullable, one limit per kind per person enforced, an impossible threshold
  refused, and no manual step.

### Responsive UI Behavior *(mandatory for user-facing features; otherwise state N/A)*

- **Mobile (320-767 CSS px)**: Each limit reads as a stacked card — what it is about, the threshold,
  the measured figure, the state, and the values behind the figure — without horizontal page
  scrolling. Stating a limit is a full-width form reachable in one action. The 360x800 scenario
  asserts the state, the arithmetic and the no-advice statement are all reachable.
- **Tablet (768-1023 CSS px)**: The 768x1024 scenario asserts the same content in the tabular layout
  the other screens adopt at that width.
- **Desktop (1024+ CSS px)**: The 1440x900 scenario asserts the same at the shared content width.
- **Input and accessibility**: The three states are distinguished by a word, never by colour alone —
  a person who cannot tell red from green must not have to guess whether they are inside their own
  rule. Every figure is available as text. At the 320-pixel floor nothing clips.

### Live Update Behavior *(mandatory for client-visible data; otherwise state N/A)*

- **Snapshot and events**: A stated, changed or removed limit is a committed domain change and
  publishes a versioned, resumable event scoped to its owner alone, as feature 022's trades do. A
  recorded trade already publishes its own event, and a new stored price publishes a shared one;
  both change where a person stands, and the screen re-reads on either rather than publishing a
  third event that says the same thing.
- **Reliability**: Ordering, event identifiers, resumption from the last delivered event,
  duplicate-safe consumption and bounded coalescing follow the existing contracts exactly.
- **Test evidence**: A person watching sees their own limit change arrive without reloading; a
  second person connected throughout receives nothing about it; a reconnecting client replays its
  own events exactly once.

### Identity, Ownership, and Permissions *(mandatory for user/account data; otherwise state N/A)*

- **Bootstrap and invitations**: Unchanged.
- **Ownership and authorization**: Limits are private to one person, inheriting the boundary feature
  022 established — ownership on the row, enforced in services and queries, with the owner role
  granting no access to anybody else's. Instruments, sectors, prices and signals remain shared
  reference data.
- **Security evidence**: Tests prove reads, writes, changes, removals, evaluations and events are
  all refused or empty across users, and refused to an unauthenticated and to a deactivated caller.

### Key Entities *(include if feature involves data)*

- **Risk limit**: One rule a person wrote down — its kind, its threshold, and whose it is.
- **Evaluation**: What a limit says about the portfolio right now: within, exceeded or unevaluable,
  with the measured figure and the values behind it.
- **Contribution**: The holdings that produced a measured figure, so a percentage can be checked.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: No person can observe any part of another person's limits or evaluations through any
  read, write or live event.
- **SC-002**: The product reports no limit that its owner did not state, and no surface suggests a
  threshold.
- **SC-003**: Every evaluation reports exactly one of within, exceeded or unevaluable, and a limit
  is never reported within because something could not be measured.
- **SC-004**: Every measured figure can be reproduced from values shown beside it.
- **SC-005**: No surface tells a person what to do about a breach.
- **SC-006**: Recording a trade that breaches a limit succeeds, and the breach is visible afterwards.
- **SC-007**: No stored backtest result changes when a person's limits change.
- **SC-008**: Every surface showing a limit states that the limits are the person's own and that the
  product offers no advice.
- **SC-009**: A person can state their first limit and see where they stand without reading
  documentation.

## Assumptions

- **Limits are about what is held, not about what is proposed.** The vision's risk engine rejects or
  modifies a *recommendation*, which needs order intents to exist first. The controls it lists split
  cleanly: allocation and count limits are measurable against a held portfolio today, while minimum
  trade size, cash reserve and no-averaging-down are only meaningful against a proposed action and
  arrive with the feature that proposes one.
- **No portfolio drawdown, and the reason is structural.** Feature 022 values holdings at their
  latest stored session and keeps no equity history, and it deliberately tracks no cash. A drawdown
  limit would need both. Offering one computed from what exists would be inventing a series.
- **Sector comes from feature 014's curated classification**, including `unclassified`, which is
  stated rather than hidden. A sector limit that quietly ignored the unclassified holdings would
  understate every other sector's share.
- **Market means the exchange an instrument is listed on**, which is how feature 021 already chooses
  a benchmark. Currency is not a separate limit kind: for this universe the two coincide except for
  Helsinki, and two kinds that almost always agree would invite a person to set both and be
  confused by the difference.
- **Evaluation reads the portfolio's own valuation**, so a limit inherits feature 022's honesty
  rules for free: the same unvaluable holding that makes a total incomplete makes a limit
  unevaluable, and for the same stated reason.
- **No breach history, and the screen says so.** Because nothing is stored, a breach that resolves
  leaves no trace and one that began in March looks identical to one that began today. That is a
  real loss, and the alternative — stored derived state, plus a job to notice when it changes — is
  the thing feature 022 deliberately avoided. Somebody who needs the history has the trade record it
  would be reconstructed from.
