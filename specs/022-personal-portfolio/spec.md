# Feature Specification: Personal Portfolio and Holdings

**Feature Branch**: `022-personal-portfolio`

**Created**: 2026-09-17

**Status**: planned
<!-- Market Lens spec lifecycle: planned → in-progress → in-review → shipped. -->

**Input**: User description: "Personal portfolio and holdings — the first feature of Milestone 6.
Every record this product has stored so far is shared reference data. This feature introduces the
first user-owned records, so the ownership boundary has to be built correctly here rather than
retrofitted when risk limits and order intents arrive behind it."

## User Scenarios & Testing *(mandatory)*

Everything this product has stored so far is the same for every user. Instruments, bars, features,
signals, backtests — all shared reference data, which is why cross-user isolation has never been
tested on anything but identity itself. This feature introduces the first record that belongs to a
person, and the rest of Milestone 6 is built on top of it: risk limits constrain a portfolio, order
intents are computed against one. The ownership boundary is cheap to get right now and expensive
to retrofit later.

It is also the first time the product holds something a person could be harmed by getting wrong.
A backtest that is subtly mis-stated is an embarrassment. A portfolio that silently disagrees with
a broker statement is a person's own money, misreported to them.

### User Story 1 - A person records what they own, and sees what it is worth (Priority: P1)

Somebody holding shares has no way to see them in this product. They can browse the universe, read
a chart and read a strategy's view, and none of it knows what they actually hold.

They record a purchase — which instrument, how many shares, what they paid, on what date, and what
the trade cost them. The product shows the position, what it is worth at the latest stored price,
and states plainly when it cannot value something rather than guessing.

**Why this priority**: It is the whole foundation. Nothing else in Milestone 6 exists without a
record of what a person holds, and the ownership boundary this establishes is what every later
feature inherits.

**Independent test**: Record two purchases as one user and a third as another; confirm each sees
only their own holdings, with quantities and values derived from what they entered.

**Acceptance Scenarios**:

1. **Given** an authenticated person, **When** they record a purchase of an instrument in the
   universe, **Then** the holding appears with the quantity they entered and a value computed from
   the latest stored close.
2. **Given** two people each with holdings, **When** either reads their portfolio, **Then** they
   see only their own — in the listing, in any total, in the event stream, and in any export.
3. **Given** a holding whose instrument has no stored price for the valuation session, **When** the
   portfolio is read, **Then** the holding is shown as unvalued with the reason, and the portfolio
   total states that it is incomplete rather than omitting the holding.
4. **Given** a second purchase of an instrument already held, **When** it is recorded, **Then** the
   position is the sum of the two, and both trades remain individually visible.
5. **Given** a sale of part of a holding, **When** it is recorded, **Then** the remaining quantity
   is reduced and the sale is visible as its own entry.

---

### User Story 2 - What it cost, and what it has made or lost (Priority: P1)

A quantity and a market value answer half the question. The half that matters is whether the
position is up or down, by how much, and how much of that has actually been taken.

**Why this priority**: A portfolio that shows value without cost is a number nobody can act on, and
the two are entered together anyway — the cost comes in with the trade. The two P1 stories ship as
one for the same reason features 015 and 021 did: either alone looks finished and is worse than
neither.

**Independent test**: Record a purchase and a partial sale at a different price; confirm the
realised profit on the shares sold, the unrealised profit on those still held, and that the two
reconcile with the trades under the stated cost basis.

**Acceptance Scenarios**:

1. **Given** a holding bought at one price and currently priced at another, **When** the portfolio
   is read, **Then** the unrealised profit or loss is reported with the cost it is measured against.
2. **Given** a partial sale, **When** it is recorded, **Then** the realised profit or loss on the
   shares sold is reported, computed under the stated cost basis, and the remaining cost is reduced
   consistently with it.
3. **Given** any reported profit, **When** it is read, **Then** the costs of the trades that
   produced it are included, and the figure states whether it is realised or unrealised.
4. **Given** a position sold in full, **When** the portfolio is read, **Then** it is no longer an
   open holding but its realised result remains readable.
5. **Given** a holding in a currency other than the accounting currency, **When** it is valued,
   **Then** the rate used and its session are stated, and a session with no stored rate produces a
   stated absence rather than a carried-forward rate.

---

### User Story 3 - A mistake can be corrected, and the correction is visible (Priority: P2)

A trade in this product is a fact a person asserted, not a fact the product observed. People mistype
quantities and dates. The product must let them fix it, and must not let the fix be invisible.

**Why this priority**: Without it the first typo makes a portfolio permanently wrong and the person
starts keeping the real numbers in a spreadsheet. With a silent correction, the portfolio cannot be
reconciled against a broker statement, because last week's figure is no longer reproducible.

**Independent test**: Record a trade, correct its quantity, and confirm both that the position
reflects the correction and that the original entry remains visible as superseded.

**Acceptance Scenarios**:

1. **Given** a recorded trade, **When** its owner corrects a field, **Then** every derived figure is
   recomputed and the previous version remains readable as superseded, with when it was changed.
2. **Given** a recorded trade, **When** its owner withdraws it, **Then** it stops affecting any
   figure and remains readable as withdrawn rather than disappearing.
3. **Given** a trade belonging to another person, **When** anybody else attempts to correct or
   withdraw it, **Then** the attempt is refused by the service, not merely hidden by the interface.
4. **Given** a correction that would make a position negative, **When** it is attempted, **Then** it
   is refused with a reason naming the sale it would invalidate.

---

### User Story 4 - What the market did over the same window, and what a strategy thinks (Priority: P2)

A profit means little on its own. A holding that gained 8% while its market gained 20% lost money in
every sense that matters, and somebody looking at a green number has no way to know that.

Because a portfolio does not track cash, the product cannot state a portfolio *return* — it does not
know what was paid in (FR-024). What it can state honestly is narrower and, for somebody holding a
handful of names, arguably more useful: **each holding against its own market over its own holding
period**. Bought in March, still held in September — here is what the instrument did, and here is
what the Stockholm benchmark did between exactly those two dates.

**Why this priority**: The comparison is what stops a profit reading as a success. The series it
needs are already stored by feature 021. The strategy view rides in the same story because a holding
sitting beside a live opinion about it is the point at which this product is most likely to start
sounding like advice.

**Independent test**: Record a purchase, read the holding, and confirm the benchmark return between
the purchase date and the valuation session is reported beside the holding's own; confirm an
uncovered period says so; confirm the absent portfolio return says why; confirm the strategy view
carries its caveat.

**Acceptance Scenarios**:

1. **Given** a holding, **When** it is read, **Then** the return of its market's benchmark between
   the holding's first purchase and the valuation session is reported beside the holding's own
   return over those same two dates.
2. **Given** a holding whose period the benchmark series does not cover, **When** the comparison is
   read, **Then** it is stated as unavailable with the reason, never truncated or back-filled.
3. **Given** a portfolio, **When** somebody looks for its overall return, **Then** the product
   states that it cannot report one because it does not know what was paid in — rather than
   omitting the figure silently or computing one from an unknown denominator.
4. **Given** a held instrument with a current signal, **When** it is shown beside the holding,
   **Then** it carries the strategy's stored caveat and is not phrased as a suggestion to buy, sell
   or hold anything.
5. **Given** any surface showing a portfolio, **When** it is read, **Then** it states that the
   product records what a person entered and offers no advice.

---

### Edge Cases

- **A sale of more shares than are held.** Refused, naming the quantity actually held. A negative
  position is not a short — this feature has no shorting — it is a data-entry error.
- **A trade in an instrument the product does not carry.** The universe is 100 curated Nordic
  listings. Somebody holding anything else cannot record it, and the refusal says so plainly rather
  than failing to find something.
- **A trade dated before the instrument's stored history begins.** Accepted — the person did buy it
  — but the portfolio cannot value or compare the period it has no prices for, and says so.
- **A trade dated on a session the exchange was closed.** Accepted, because a broker statement is
  the authority on when a person's trade happened and this product's calendar is not. Valuation
  still uses stored sessions only.
- **A trade dated in the future.** Refused. A holding somebody does not have yet is not a holding.
- **An instrument that stops trading while held.** The position is unvalued with a reason from the
  session prices stop, and the portfolio total states that it is incomplete, exactly as a backtest
  does.
- **Two trades in the same instrument on the same date.** Both recorded and both visible; order
  within a date follows the order they were entered, which is stated because cost basis depends on
  it.
- **A portfolio with no trades at all.** Reads as empty and says what to do about it, rather than
  showing zeroes that look like a loss.
- **A holding whose whole position is sold and then bought again.** The realised result of the first
  holding period stays readable and is not merged into the new position's cost.
- **A fractional quantity.** Some brokers fill fractional shares; the product accepts a fractional
  quantity because refusing it would make a real holding unrecordable.

## Requirements *(mandatory)*

### Functional Requirements

**Ownership**

- **FR-001**: Every portfolio, trade and derived figure MUST carry explicit ownership, enforced in
  backend services and persistence queries, never by client-side filtering alone.
- **FR-002**: A person MUST NOT be able to read, correct, withdraw or otherwise observe another
  person's portfolio, trades or figures — in reads, in writes, in live events, or in any export.
- **FR-003**: Ownership MUST be read from the persisted session and account record, so a
  deactivated account loses access without waiting for a session to expire.

**Recording what is held**

- **FR-004**: A person MUST be able to record a purchase or sale naming the instrument, the
  quantity, the price paid or received, the date, and the costs the trade bore.
- **FR-005**: The product MUST accept only instruments it carries, and MUST say so plainly when
  asked to record anything else.
- **FR-006**: A sale MUST NOT be accepted where it would make a position negative, and the refusal
  MUST name the quantity actually held.
- **FR-007**: A trade dated in the future MUST be refused.

**Correction**

- **FR-008**: A trade MUST be correctable and withdrawable by its owner, and both MUST recompute
  every derived figure.
- **FR-009**: A corrected or withdrawn trade MUST remain readable as superseded or withdrawn, with
  when the change was made. History MUST NOT be silently rewritten.

**Derivation**

- **FR-010**: Positions, cost, and profit MUST be derived from the recorded trades rather than
  stored as independently editable figures, so they cannot drift from the trades that produced them.
- **FR-011**: Valuation MUST use stored prices only. A holding that cannot be priced MUST be
  recorded as unvalued with a reason, and any total that includes it MUST state that it is
  incomplete rather than omitting the holding or assuming a price.
- **FR-012**: Realised profit MUST be computed under a stated cost basis, and the basis MUST be
  visible wherever a realised figure is shown.
- **FR-013**: Reported profit MUST include the costs of the trades that produced it, and MUST state
  whether it is realised or unrealised.
- **FR-014**: Where an instrument's currency differs from the accounting currency, conversion MUST
  use a stored rate as of the valuation session, and that rate and its session MUST be stated. A
  session with no stored rate MUST produce a stated absence rather than a carried-forward rate.

**Comparison and honesty**

- **FR-015**: Each holding's own return MUST be reported beside the return of its market's benchmark
  between the same two dates — the holding's first purchase and the valuation session.
- **FR-016**: Where the benchmark series does not cover that period, the comparison MUST be stated
  as unavailable with the reason, never truncated or back-filled.
- **FR-016a**: The product MUST NOT report a portfolio-level return, and MUST state that it cannot
  because it does not track what was paid in. An absent figure with a stated reason is the
  requirement; a silently missing one is not.
- **FR-017**: A strategy's view of a held instrument MAY be shown and MUST carry the strategy's
  stored caveat. It MUST NOT be phrased as a suggestion to act.
- **FR-018**: Every surface showing a portfolio MUST state that the product records what a person
  entered and offers no advice.

**Out of scope, stated as requirements so they are testable**

- **FR-019**: The feature MUST NOT connect to a broker, place an order, or produce an order intent.
- **FR-020**: The feature MUST NOT apply a risk limit, reject a trade on risk grounds, or modify
  what a person entered for any reason other than their own correction.
- **FR-021**: The feature MUST NOT compute tax, produce a tax report, or claim that any figure is
  suitable for a tax return.
- **FR-022**: The feature MUST NOT adjust a person's cost basis for a dividend or a corporate
  action. Where a stored corporate action falls inside a holding period, the product MUST say so
  and leave the adjustment to the person.

**The three decisions, resolved 2026-09-17**

- **FR-023**: Realised profit MUST be computed first-in-first-out: the shares sold are the oldest
  ones held. Chosen because it is what the Nordic tax authorities expect by default, so a person's
  figure has a chance of matching their own filing, and because it keeps each holding period's
  result separable — which FR-010 and the re-purchase edge case already require. Where two
  purchases share a date, the order they were entered decides, and that order is stored rather than
  inferred.
- **FR-024**: A portfolio MUST NOT track cash. Profit is reported per position and in total across
  positions; a portfolio *return* is not reported at all, because the product does not know what was
  deposited and a figure computed without that would be arithmetic on an unknown denominator. The
  product MUST say why the figure is absent rather than omitting it silently.
- **FR-025**: A portfolio MUST state its own accounting currency, chosen by its owner from the
  currencies the universe trades in. Conversion between two non-euro currencies goes through the
  euro — divide by the euro rate of the holding's currency, multiply by the euro rate of the
  accounting currency — because every stored rate has the euro as its base and storing an inverse
  or a direct cross would let two stored numbers disagree. Both legs MUST come from the same
  session, and a session missing either leg MUST produce a stated absence.

### Test-First Proof *(mandatory)*

- **Initial failing test**: `TestAPersonSeesOnlyTheirOwnHoldings` — two people each record a trade;
  each reads the portfolio listing, the totals, and the event stream, and observes only their own.
- **Expected red reason**: No portfolio exists, so the read returns nothing for a person who has
  just recorded a trade. A value failure on stored data, not a compilation or setup failure.
- **Green evidence**: The portfolio suite, plus the existing identity, strategy, backtest and
  market-data suites unchanged — this feature reads shared reference data and must not alter it.
- **Database migration proof**: Portfolios and trades are new stored facts and the first user-owned
  ones. They arrive as an ordered migration, with a test proving a clean install and an upgrade both
  arrive with the tables present, ownership not nullable, and no manual step.

### Responsive UI Behavior *(mandatory for user-facing features; otherwise state N/A)*

- **Mobile (320-767 CSS px)**: A portfolio reads as a stacked record — total, comparison, then each
  holding as a card carrying quantity, value, cost and profit — without horizontal page scrolling.
  Recording a trade is a full-width form reachable in one action from the portfolio. The 360x800
  scenario asserts the total, the comparison, the no-advice statement and the action to record a
  trade are all reachable.
- **Tablet (768-1023 CSS px)**: The 768x1024 scenario asserts the same content with holdings in the
  tabular layout the other screens adopt at that width.
- **Desktop (1024+ CSS px)**: The 1440x900 scenario asserts the same at the shared content width.
- **Input and accessibility**: Every figure is available as text; profit and loss are never carried
  by colour alone, since the difference between a gain and a loss is exactly what colour-blind
  readers lose; the path from a holding to the instrument and to the strategy's view is keyboard
  reachable. At the 320-pixel floor nothing clips.

### Live Update Behavior *(mandatory for client-visible data; otherwise state N/A)*

- **Snapshot and events**: A recorded, corrected or withdrawn trade is a committed domain change and
  publishes a versioned, resumable event **scoped to its owner alone**. The mechanism already
  exists and is already enforced: feature 004 built user-scoped events for sessions and account
  changes, and the replay query filters them by subject, refusing a deactivated account even with
  a cursor it held while active. What is new is that this is the first *domain* record to use it,
  so the scoping is inherited rather than invented. A new stored price changes what a portfolio is
  worth, so the existing market-data event is what prompts a re-read; the product does not publish
  a private event for a change every user can already see.
- **Reliability**: Ordering, event identifiers, resumption from the last delivered event,
  duplicate-safe consumption and bounded coalescing follow the existing contracts exactly.
- **Test evidence**: A reader watching their own portfolio sees their own change arrive without
  reloading; a second person connected to the stream throughout receives nothing about it; a
  reconnecting client replays its own events exactly once and none of anybody else's.

### Identity, Ownership, and Permissions *(mandatory for user/account data; otherwise state N/A)*

- **Bootstrap and invitations**: Unchanged. This feature adds no route to an account and no change
  to how one is created.
- **Ownership and authorization**: Portfolios and trades are private to one person. Instruments,
  prices, signals, benchmarks and rates remain shared reference data that every authenticated user
  reads. The distinction is explicit in contracts, events, logs and tests; the owner role grants no
  access to another person's portfolio, because ownership here is not administrative.
- **Security evidence**: Tests prove that reads, writes, corrections, withdrawals, totals, exports
  and events are all refused or empty across users, and refused to an unauthenticated and to a
  deactivated caller.

### Key Entities *(include if feature involves data)*

- **Portfolio**: One person's record of what they hold, with its stated accounting currency. The
  cost basis is first-in-first-out for every portfolio and is stated on each realised figure rather
  than chosen per portfolio.
- **Recorded trade**: One purchase or sale a person asserted — instrument, quantity, price, date,
  costs — with its correction history.
- **Position**: What is currently held in one instrument, derived from the trades.
- **Valuation**: What a position is worth at a session, or the stated reason it could not be valued.
- **Realised result**: Profit or loss on shares that have been sold, under the stated basis.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: No person can observe any part of another person's portfolio through any read, write,
  total, export or live event.
- **SC-002**: Every position, cost and profit figure reconciles exactly with the trades recorded
  against it; recomputing from the trades produces no differing field.
- **SC-003**: No holding is ever valued at a price from a session its instrument did not trade, and
  no total silently omits a holding it could not value.
- **SC-004**: Every realised figure states the cost basis it was computed under, and recomputing it
  first-in-first-out from the recorded trades reproduces it exactly.
- **SC-004a**: No surface reports a portfolio-level return, and every surface a reader might look
  for one on says why there is none.
- **SC-005**: A corrected or withdrawn trade remains readable, and the figure it produced before the
  change can still be explained.
- **SC-006**: Every surface showing a portfolio states that the product records what a person
  entered and offers no advice, and no surface phrases a strategy's view as a suggestion to act.
- **SC-007**: A person can record their first trade and see the resulting holding without reading
  documentation.
- **SC-008**: A portfolio of a hundred holdings and a thousand recorded trades reads within a stated
  time budget on the deployment's own hardware.

## Assumptions

- **One portfolio per person, for now.** Several portfolios is a reasonable thing to want and a
  straightforward extension, but every screen and every figure would have to name which one, and
  nothing in Milestone 6 needs it. One portfolio keeps the ownership boundary as simple as it will
  ever be, which is the point of establishing it here.
- **Trades are entered by hand.** There is no broker integration in this product and none planned
  for V1, so every record is a person's own assertion. That is why correction is a first-class
  requirement rather than an administrative afterthought.
- **The accounting rules are feature 021's, reused rather than forked.** Conversion divides by a
  rate stored with the accounting currency as its base; every intermediate is rounded to the stored
  precision before the next step reads it; an unpriceable position is a stated absence. A holding is
  not a backtest position — those rows belong to one simulation run — but a second implementation of
  the same arithmetic would eventually disagree with the first.
- **First-in-first-out, and the entry order is stored.** Two purchases on the same date have to be
  ordered for FIFO to mean anything, and inferring that order from an identifier or an insertion
  timestamp would make the basis depend on something nobody can see. The order a person entered
  them in is recorded and shown.
- **No cash means no portfolio return, said out loud.** The product knows what was bought and what
  it is worth; it does not know what was deposited. A return computed without that would divide by
  a number the product invented. Saying "we cannot tell you this, and here is why" is the whole
  behaviour — and it is also the argument for adding deposits later, deliberately, rather than
  defaulting into them now.
- **A chosen accounting currency costs a second conversion.** Every stored rate has the euro as its
  base, so a Danish holding in a Swedish-accounting portfolio converts DKK→EUR→SEK. Feature 021
  avoided this by choosing the euro; a personal tracker cannot, because telling somebody in
  Stockholm their holdings are worth €48,000 is a strange thing for a product about their own money
  to do. Both legs come from one session, and either missing leg is a stated absence.
- **Valuation is end-of-day, from stored closes.** This product stores daily bars and nothing
  intraday, so a portfolio is worth what it was worth at the last stored close. Anything else would
  be inventing a price.
- **A strategy's view is shown because hiding it would be worse.** A person looking at a holding
  will want to know what the product's own method makes of it. Backtesting has since measured that
  method and found it lost to two of its three benchmarks over ten years, at about 3.1% of equity a
  year in trading costs — which is the strongest possible argument for showing the view with its
  caveat attached and never as a recommendation.
