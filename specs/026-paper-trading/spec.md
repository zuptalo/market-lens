# Feature Specification: Paper Trading

**Feature**: 026 | **Branch**: `026-paper-trading` | **Milestone**: 7
**Status**: Reviewed — three decisions resolved by the owner before planning

## Summary

A person promotes something they were considering into a **paper account**: an order that the
product fills from stored prices at the next session's open, records permanently, and keeps score
on. It answers the one question the product cannot answer today — *would the decisions I actually
made have worked?* — and it answers it forward, on prices nobody had seen when the order was placed.

It is not a second portfolio. Feature 022 records what a person *did*; this records what they
*would have done*, and the two must never be mistaken for one another on screen or in a figure.

## Why this is safe to build here

Every refusal this codebase has accumulated points at the same risk: a product that quietly starts
telling people what to buy. Paper trading is where that risk is highest, because a simulated
account that grows looks like a recommendation to do the same with real money.

Three things hold the line, and each is a requirement below rather than an intention:

1. **Nothing here proposes an order.** Orders come only from intents a person wrote down
   themselves (FR-001). The strategy does not place paper orders; the product does not either.
2. **A fill can never be chosen after the price is known.** An order placed during or after a
   session fills at the *next* session's open, which did not exist when the order was placed
   (FR-006). This is feature 021's rule, kept rather than re-argued.
3. **A paper result is reported beside what it is not.** Every figure carries that it is a
   simulation over stored prices with stated costs, and no figure is ever combined with a real
   holding (FR-019, FR-021).

## Decisions resolved by the owner

| Question | Decision | Consequence |
|---|---|---|
| Where does a fill price come from? | **The next session's open**, from stored bars | Matches feature 021, so a backtest and a paper run are comparable. An order can never be placed after seeing the price it fills at. |
| When does the account move? | **A scheduled pass after each market-data import**, in process | A track record accrues whether or not anybody visits. Recorded, not re-derived: what was true at the time stays true. |
| Where do orders come from? | **Promoted from order intents only** | Keeps feature 025's authorship reversal. Gives intents somewhere to go besides being withdrawn. |

## User scenarios

### US1 — Promote an intent and watch it fill (P1)

A person has written down that they are considering buying 120 ABB at 681.40. They promote it into
their paper account. Nothing happens immediately: the order sits **pending**, and the screen says
it will fill at the next session's open. After the next import lands, it is **filled** at that
open — which may differ from the price they expected, and the difference is shown.

**Independently testable**: promote an intent, run the pass with a known next bar, and read the
fill price, the costs, and the resulting paper position.

### US2 — Keep score honestly (P1)

The paper account reports what it is worth, what it cost, what has been made or lost, and how that
compares with the markets its holdings trade in — using the same accounting and the same benchmark
rule as feature 021 and 022, so three screens cannot disagree about one instrument.

**Independently testable**: fill several orders across sessions, then read the account's value,
cost, unrealised and realised results, and the per-holding comparison.

### US3 — An order that cannot fill says so (P1)

An instrument with no next bar, a sale larger than the paper position, or a buy the paper cash
cannot cover: each is reported with a reason and the order is **not** filled. Nothing is silently
dropped and nothing is silently partially filled.

**Independently testable**: construct each case and read the order's state and its reason.

### US4 — It is mine, and it is not real (P2)

Two people's paper accounts are invisible to one another in reads, writes and events. Every screen
that shows a paper figure says it is a simulation, and no screen adds a paper figure to a real one.

**Independently testable**: two accounts, cross-read attempts on every path, and a scan of every
rendered surface for a combined total.

## Functional requirements

### Orders

- **FR-001** A paper order is created **only** by promoting an order intent the person wrote down.
  There is no endpoint, command, scheduler or strategy path that creates one.
- **FR-002** Promoting an intent settles it as acted on and records the paper order it became, so
  the intent's own history stays truthful.
- **FR-003** An order records the instrument, direction, quantity, the price the person expected,
  the intent it came from, and when it was placed. It records **no** venue, order type, time in
  force, destination or broker reference — the same absences feature 025 asserts.
- **FR-004** An order is in exactly one state: `pending`, `filled`, `cancelled`, or `unfillable`.
- **FR-005** A person may cancel a pending order. A filled order is permanent: a paper track record
  that can be edited afterwards is worthless.

### Fills

- **FR-006** A pending order fills at the **open of the first stored session after the session it
  was placed in**. It never fills at a session on or before its placement session.
- **FR-007** A fill applies the same costs feature 021 applies: brokerage in basis points with a
  minimum, slippage in basis points, and a currency spread when the instrument's currency differs
  from the account's. The rates are the account's own, stated when it is opened.
- **FR-008** A fill is recorded, not derived. The bar it used is recorded with it, so a later
  correction to that bar does not silently rewrite history — it is reported as a divergence.
- **FR-009** A buy that the account's cash cannot cover is `unfillable` with reason
  `insufficient_cash`. Cash is tracked here, unlike feature 022, because a simulated account that
  cannot run out of money measures nothing.
- **FR-010** A sale larger than the paper position is `unfillable` with reason
  `exceeds_position`. A paper account cannot go short.
- **FR-011** An instrument with no stored session after the placement session stays `pending` until
  one exists. After a stated number of sessions with no bar it becomes `unfillable` with reason
  `no_price`.

### The account

- **FR-012** A person has at most one paper account. It is opened explicitly, with a starting cash
  balance and an accounting currency they state, and both are immutable afterwards.
- **FR-013** Cash moves only through fills: a buy reduces it by value plus costs, a sale increases
  it by value minus costs. There is no deposit and no withdrawal.
- **FR-014** Position, cost, realised and unrealised results are derived from recorded fills using
  first-in-first-out, the same rule and the same arithmetic as feature 022.
- **FR-015** Because cash *is* tracked, the account reports a **total return** — the one figure
  feature 022 declines to state. It is the only place in the product where that figure is honest.
- **FR-016** Each holding is compared with the benchmark for its market over its own holding period,
  as feature 022 does.
- **FR-017** A holding that cannot be priced is reported unvalued with a reason, and the account
  total is reported incomplete rather than understated.

### Isolation and honesty

- **FR-018** Every order, fill and account row is owned by one person and enforced in services and
  queries. Two people's accounts are invisible to each other in reads, writes, events and exports.
- **FR-019** Every surface showing a paper figure states that it is a simulation over stored prices
  with stated costs, and that no order was placed anywhere.
- **FR-020** No screen, endpoint or export combines a paper figure with a real holding from feature
  022 into one number.
- **FR-021** No paper figure is ever framed as a reason to act. The account reports what happened;
  it says nothing about what to do next.
- **FR-022** A committed change to a paper account publishes a versioned, authorized, resumable SSE
  event scoped to its owner, in the same transaction.

## Scope by exclusion

Each of these is absent, and its absence is testable:

- **FR-023** No broker connection, and no order that could be transmitted anywhere.
- **FR-024** No strategy-driven paper account. Running a strategy forward is Milestone 8's question
  and needs its own review; this feature would make it one scheduler change away, which is exactly
  why it is stated as excluded rather than left unmentioned.
- **FR-025** No short selling, no leverage, no margin, no derivatives.
- **FR-026** No intraday fills. The product stores daily bars; a fill at anything but a session
  open would be invented.
- **FR-027** No tax treatment, no dividends, no corporate-action adjustment of paper cost basis.
- **FR-028** No second paper account, no account reset, and no deletion of a filled order.

## Success criteria

- **SC-001** An intent promoted before a session fills at that session's open, and the fill price
  equals the stored open exactly.
- **SC-002** Replaying the same orders against the same bars produces identical fills, costs and
  results, byte for byte.
- **SC-003** No order exists that no person created, asserted by a test that gives the system a
  strategy, signals and a scheduler pass and finds none.
- **SC-004** Two accounts are provably invisible to each other on every read, write and event path.
- **SC-005** Every rendered paper figure appears within a surface that states it is a simulation.
- **SC-006** The account's total return reconciles exactly with cash plus holdings minus starting
  cash, at twelve decimal places.
- **SC-007** Every screen behaves at 360x800, 768x1024 and 1440x900, tolerates 320 CSS pixels
  without page-level horizontal scrolling, and states every figure as text.

## Key entities

- **Paper account** — one per person: starting cash, accounting currency, cost rates, opened at.
- **Paper order** — promoted from an intent: instrument, direction, quantity, expected price,
  placement session, state, and reason when unfillable.
- **Paper fill** — what an order became: session, open price used, quantity, costs, cash effect,
  and the identity of the bar it read.
- **Paper position** — derived, never stored: quantity, cost, realised and unrealised result.

## Assumptions

- The person's own risk limits (feature 023) are reported against the paper account exactly as they
  are against the real one, because a limit is a rule about a portfolio and this is a portfolio.
- The cost rates default to feature 021's stated defaults, so a paper run and a backtest of the same
  strategy are comparable without anybody configuring anything.
- A paper account's accounting currency need not match the real portfolio's; conversion uses the
  same euro-based rates feature 021 stores.
