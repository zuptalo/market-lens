# Feature Specification: Order Intents

**Feature Branch**: `025-order-intents`

**Created**: 2026-09-18

**Status**: planned
<!-- Market Lens spec lifecycle: planned → in-progress → in-review → shipped. -->

**Input**: User description: "Order intents — the last piece of Milestone 6, and the point at which
the risk engine finally has something to evaluate."

## User Scenarios & Testing *(mandatory)*

This is the closest this product comes to telling somebody what to do, and it is therefore the
feature where its position has to be stated most carefully.

The vision says signals pass through a risk engine which "may reject or modify a recommendation".
Read literally, that is a product that generates a list of trades from a strategy. This product will
not do that, for a reason of its own making: feature 021 measured that strategy over ten years and
found it lost to two of its three benchmarks, with trading costs of about 3.1% of equity a year. A
product that refuses to say what would close a limit gap, and then hands somebody a list of five
things to buy from a method its own evidence says underperforms, would be contradicting itself in
the one place where the contradiction costs money.

The difference between advice and an intent is **who formed it**. This feature takes the second
reading: a person writes down what they are considering, and the product tells them what it would
do — to their position, to their concentration, and to the limits they set themselves. That is the
same shape as feature 023, where the product does not set your limits and does not advise on them,
it holds you to the ones you wrote.

### User Story 1 - A person writes down what they are considering (Priority: P1)

Somebody is thinking about buying more of something they already hold. They can see what they hold,
what it is worth, and which limits they are inside. What they cannot see is what this particular
trade would do to any of it, short of working it out on paper.

They record an intent: the instrument, whether they would buy or sell, how many shares, and the
price they expect to pay. The product stores it as something they are considering — not as
something that happened, and not as something it suggested.

**Why this priority**: Nothing else here exists without it, and stating it first is what keeps the
authorship straight: every intent in this product was written by the person whose portfolio it
would change.

**Independent test**: Record an intent as one person and a different one as another; confirm each
sees only their own, stored exactly as entered and marked as under consideration.

**Acceptance Scenarios**:

1. **Given** an authenticated person, **When** they record an intent, **Then** it is stored against
   them alone and shown back as something they are considering.
2. **Given** an intent, **When** it is read, **Then** nothing about it reads as a suggestion — the
   product records that the person is considering it and claims nothing about whether they should.
3. **Given** two people, **When** either reads or changes an intent, **Then** the other's are
   invisible and unaffected in reads, writes and events.
4. **Given** an instrument this product does not carry, **When** an intent names it, **Then** it is
   refused with the reason, as recording a trade in it would be.
5. **Given** a sale larger than the position, **When** an intent proposes it, **Then** it is
   accepted and reported as something that would leave a negative position, because an intent is a
   thought rather than a fact — unlike a recorded trade, which is refused.

---

### User Story 2 - The product says what it would do (Priority: P1)

An intent nobody evaluates is a note. The point is the consequence, and the consequence is worth
nothing if it cannot be checked.

**Why this priority**: It is the half that does the work, and it ships with User Story 1 for the
reason every P1 pair in this product has shipped together — a feature that stores intentions and
never evaluates them looks finished and is useless.

**Independent test**: Hold a position, state a concentration limit, and record an intent that would
breach it; confirm the resulting position, the resulting share, and the limit it would put the
person outside, with the arithmetic behind each.

**Acceptance Scenarios**:

1. **Given** an intent, **When** it is read, **Then** the resulting position and the resulting share
   of the portfolio are reported, with the values they were computed from.
2. **Given** a stated limit the intent would put the person outside, **When** it is read, **Then**
   that limit is named with the share it would reach and the threshold it would pass.
3. **Given** a stated limit the intent would leave satisfied, **When** it is read, **Then** it is
   reported as still within, rather than omitted.
4. **Given** a portfolio with a holding that cannot be valued, **When** an intent is evaluated,
   **Then** every consequence measured against the portfolio total is reported unevaluable with the
   reason, exactly as a limit is.
5. **Given** any evaluated intent, **When** it is read, **Then** the product states what would
   happen and does not state whether to do it.

---

### User Story 3 - A record of what was considered (Priority: P2)

Somebody who has been running a portfolio for a year has made decisions they can no longer
reconstruct — the things they nearly did, and why they did not.

**Why this priority**: It is what makes the feature worth more than a calculator. The product cannot
say whether a decision was good, but it can keep an honest record of what was considered and what
became of it, which is something no other screen holds.

**Independent test**: Record an intent, withdraw it, record another and mark it acted on; confirm
all three states are readable afterwards with their dates.

**Acceptance Scenarios**:

1. **Given** an intent under consideration, **When** its owner withdraws it, **Then** it stops being
   evaluated and remains readable as withdrawn, with when that happened.
2. **Given** an intent under consideration, **When** its owner marks it acted on, **Then** it
   remains readable as acted on, with when — and the product does not record a trade on their
   behalf.
3. **Given** an intent that was acted on, **When** the portfolio is read, **Then** it is unchanged
   until the person records the trade themselves, because what they actually paid is a fact only
   they can assert.
4. **Given** any intent, **When** its history is read, **Then** the figures it was recorded with are
   what it shows, not today's.

---

### User Story 4 - Nothing here is an order (Priority: P2)

An order intent is the entity in this product most likely to be mistaken for an instruction, by a
reader or by a later feature.

**Why this priority**: The constraint is cheap to state now and expensive to reintroduce once
something downstream has started treating an intent as an instruction.

**Independent test**: Read an intent through every surface and confirm nothing in it could be acted
on by a broker, and that no field names a venue, an order type, or a time in force.

**Acceptance Scenarios**:

1. **Given** any intent, **When** it is read, **Then** it carries no venue, no order type, no time
   in force and no destination — nothing a broker could act on.
2. **Given** an intent the product's own strategy disagrees with, **When** it is read, **Then** it
   is evaluated and reported exactly as any other.
3. **Given** the product's strategy, **When** anybody looks for a way to have it propose intents,
   **Then** there is none, and the screen says the product does not propose trades.
4. **Given** any surface showing an intent, **When** it is read, **Then** it states that the product
   records what a person is considering and offers no advice.

---

### Edge Cases

- **An intent in an instrument the person does not hold.** Evaluated normally; the resulting
  position is the quantity proposed.
- **A sale of more than is held.** Accepted and reported as producing a negative position, which the
  person can see is impossible. An intent is a thought, and refusing to let somebody write down a
  thought they are having would be a strange thing for a notebook to do.
- **An intent whose instrument stops being priceable.** The consequence measured against the
  portfolio total becomes unevaluable with the reason, and the intent stays readable.
- **Several intents at once.** Each is evaluated against the portfolio as it stands, not against
  each other. Two intents that would each be fine alone may both report satisfied while together
  they would not — and the screen says so, rather than inventing an order of application.
- **An intent recorded before a limit existed.** Evaluated against the limits in force now, because
  the decision is being made now.
- **An intent left under consideration for months.** Still shown, still evaluated against today's
  prices. The product does not expire somebody's thinking.

## Requirements *(mandatory)*

### Functional Requirements

**Whose intents these are**

- **FR-001**: Every intent MUST carry explicit ownership, enforced in backend services and
  persistence queries, and MUST be invisible to every other person in reads, writes and events.
- **FR-002**: The product MUST NOT create an intent. Every intent is written by the person whose
  portfolio it would change.
- **FR-003**: A person MUST be able to record, withdraw, and mark acted on their own intents.

**What an intent is**

- **FR-004**: An intent MUST record the instrument, the direction, the quantity, the price the
  person expects, and any costs they expect, with the date it was recorded.
- **FR-005**: The product MUST accept only instruments it carries.
- **FR-006**: An intent MUST NOT carry a venue, an order type, a time in force, or any other field a
  broker could act on.
- **FR-007**: An intent MUST NOT change a portfolio. Marking one acted on records that the person
  acted; it does not record a trade, because what they actually paid is a fact only they can assert.

**What the product says about it**

- **FR-008**: The product MUST report the position that would result and the share of the portfolio
  it would represent, with the values those were computed from.
- **FR-009**: The product MUST report, for each limit the person has stated, whether the intent
  would leave them within it or outside it, with the share it would reach and the threshold.
- **FR-010**: A consequence that cannot be computed MUST be reported unevaluable with the reason,
  never as satisfied — the same three states feature 023 established, for the same reason.
- **FR-011**: Each intent MUST be evaluated against the portfolio as it stands, and the product MUST
  say plainly that intents are not evaluated against each other.
- **FR-012**: The product MUST NOT state whether to act on an intent, MUST NOT rank intents, and
  MUST NOT modify one.

**What this feature must not do**

- **FR-013**: The product MUST NOT derive an intent from a signal, a strategy, or a ranking, and
  MUST offer no way to ask it to.
- **FR-014**: The feature MUST NOT block, reject or refuse an intent on risk grounds. It reports the
  consequence; the decision belongs to the person.
- **FR-015**: The feature MUST NOT produce an order, contact a broker, or generate anything
  transmissible.
- **FR-016**: The feature MUST NOT send a notification.
- **FR-017**: Every surface showing an intent MUST state that the product records what a person is
  considering and offers no advice.

### Test-First Proof *(mandatory)*

- **Initial failing test**: `TestAnIntentReportsWhatItWouldDo` — a person holding one instrument
  records an intent to buy more and has a concentration limit it would breach; the evaluation
  reports the resulting position, the resulting share, the values behind it, and the limit it would
  put them outside.
- **Expected red reason**: No intent can be recorded, so nothing is evaluated and the report is
  empty. A value failure on stored data, not a compilation or setup failure.
- **Green evidence**: The intents suite, plus the existing portfolio, risk, strategy, backtest and
  market-data suites unchanged — this feature reads holdings and limits and must alter neither.
- **Database migration proof**: Intents are new stored facts and user-owned. They arrive as an
  ordered migration, with a test proving a clean install and an upgrade both arrive with the table
  present, ownership not nullable, a broker-actionable column absent, and no manual step.

### Responsive UI Behavior *(mandatory for user-facing features; otherwise state N/A)*

- **Mobile (320-767 CSS px)**: An intent reads as a stacked card — what is proposed, what would
  result, and which limits it would touch — without horizontal page scrolling. Recording one is a
  full-width form. The 360x800 scenario asserts the consequence and the no-advice statement are
  reachable.
- **Tablet (768-1023 CSS px)**: The 768x1024 scenario asserts the same content in the tabular layout
  the other screens adopt at that width.
- **Desktop (1024+ CSS px)**: The 1440x900 scenario asserts the same at the shared content width.
- **Input and accessibility**: Whether an intent would leave somebody inside or outside a limit is
  carried by a word, never by colour alone. Every figure is available as text. At the 320-pixel
  floor nothing clips.

### Live Update Behavior *(mandatory for client-visible data; otherwise state N/A)*

- **Snapshot and events**: A recorded, withdrawn or acted-on intent is a committed domain change and
  publishes a versioned, resumable event scoped to its owner alone, as features 022 and 023 do. A
  new stored price and a changed limit both change what an intent would do, and both already
  publish; the screen re-reads on either.
- **Reliability**: Ordering, event identifiers, resumption, duplicate-safe consumption and bounded
  coalescing follow the existing contracts exactly.
- **Test evidence**: A person watching sees their own intent arrive without reloading; a second
  person connected throughout receives nothing about it.

### Identity, Ownership, and Permissions *(mandatory for user/account data; otherwise state N/A)*

- **Ownership and authorization**: Intents are private to one person, inheriting the boundary
  feature 022 established — ownership on the row, read directly by every predicate, with the owner
  role granting no access to anybody else's.
- **Security evidence**: Tests prove reads, writes, withdrawals, evaluations and events are all
  refused or empty across users, and refused to an unauthenticated and to a deactivated caller.

### Key Entities *(include if feature involves data)*

- **Order intent**: Something a person is considering — instrument, direction, quantity, expected
  price and costs, and what became of it.
- **Consequence**: What the intent would do, computed now: the resulting position, the resulting
  share, and each stated limit's verdict.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: No person can observe any part of another person's intents through any read, write or
  live event.
- **SC-002**: No intent exists that the product created.
- **SC-003**: Every reported consequence can be reproduced from values shown beside it.
- **SC-004**: A consequence is never reported satisfied because something could not be measured.
- **SC-005**: No surface states whether to act on an intent, ranks intents, or modifies one.
- **SC-006**: Marking an intent acted on changes no portfolio figure.
- **SC-007**: No intent carries a field a broker could act on.
- **SC-008**: Every surface showing an intent states that the product records what a person is
  considering and offers no advice.

## Assumptions

- **The product evaluates intents; it does not propose them.** The vision reads as a risk engine
  filtering strategy-generated recommendations. Feature 021 measured that strategy and found it lost
  to two of its three benchmarks over ten years. Generating trades from it here — in a product that
  will not even say what would close a limit gap — would be the one contradiction that costs a
  person money. The departure is deliberate and recorded in the checklist.
- **Risk states rather than rejects, and the difference is execution.** The vision says risk "may
  reject or modify". Rejection needs something to reject *into*: this product has no broker and
  cannot stop anybody trading, so refusing to record what somebody is considering would only mean
  refusing to let them think. In paper trading, where the product does control execution, a refusal
  will mean something. Here it would be theatre.
- **Modification is refused outright.** "You asked for 100; 60 would keep you inside your limit" is
  the same act feature 023 declined when it would not say what would close a gap. Naming the
  quantity makes the decision.
- **Intents are evaluated against the portfolio, not against each other.** Combining them needs an
  order of application the person never stated, and inventing one would report a consequence nobody
  proposed. The screen says so rather than leaving it to be inferred.
- **An intent's stored figures are the ones it was recorded with; its consequence is computed now.**
  The record is what you were thinking; the consequence is what acting today would do. Freezing the
  consequence would answer a question nobody is asking any more.
