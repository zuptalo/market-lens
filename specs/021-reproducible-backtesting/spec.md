# Feature Specification: Reproducible Backtesting

**Feature Branch**: `021-reproducible-backtesting`

**Created**: 2026-09-16

**Status**: planned
<!-- Market Lens spec lifecycle: planned → in-progress → in-review → shipped. -->

**Input**: User description: "Reproducible backtesting (Milestone 5). Feature 015's caveat says
its weights are stated rather than fitted and that whether its views are any good is a question
backtesting answers. This is that question."

## User Scenarios & Testing *(mandatory)*

Feature 015 records what a strategy said and refuses to say whether it was right. Its stored
caveat is explicit: the weights are stated rather than fitted, nothing has been tested against
historical outcomes, and whether the views are any good is a question backtesting answers.

A backtest replays those stored signals over stored sessions under a stated set of rules and
records what would have happened — trades, cash, positions, an equity curve, and the measures a
person uses to judge a method. It is the first thing in this product that produces a number
somebody might act on, so the bar for honesty is higher here, not lower.

What the provider actually serves, probed through the product's own commands rather than assumed:

| Series | Sessions | From | Covers stored history (2016-08-31)? |
|---|---|---|---|
| `OMXS30.INDX` (SEK) | 3,766 | 2011-09-16 | Yes |
| `OMXH25.INDX` (EUR) | 3,705 | 2011-09-16 | Yes |
| `OBX.INDX` (NOK) | 3,764 | 2011-09-16 | Yes |
| `OMXC25.INDX` (DKK) | 2,434 | **2016-12-19** | **No — 110 days short** |
| `EURSEK`, `EURNOK`, `EURDKK` | ~3,985 each | 2011-09-16 | Yes |

### User Story 1 - A strategy is measured against what happened (Priority: P1)

Somebody reading a signal has no way to tell whether the method behind it has ever been worth
following. They can see that it scored an instrument 0.83 and why, and nothing else.

A backtest replays one strategy version over a stated session range under stated rules and records
the trades it would have made, the cash and positions at every session, and the equity that
resulted. Recomputing it produces the identical result.

**Why this priority**: It is the question feature 015 explicitly deferred, and every later
milestone — portfolio, paper trading — consumes backtested strategies rather than untested ones.

**Independent test**: Run a backtest over the fixture, confirm the trades, the cash and the final
equity; run it again and diff every field.

**Acceptance Scenarios**:

1. **Given** a stated configuration and stored signals, **When** the backtest runs, **Then** it
   records every trade with the session it executed on, the price it paid, the cost it bore, and
   the signal that caused it.
2. **Given** the same configuration and the same stored data, **When** the backtest is recomputed,
   **Then** every stored field is identical — trades, cash, positions, curve and measures.
3. **Given** a signal as of a session, **When** the simulation acts on it, **Then** it executes no
   earlier than the next session the instrument actually traded, at a price from that session.
4. **Given** an instrument that does not trade again within a stated window, **When** the
   simulation tries to act, **Then** it records that the trade did not happen and why, rather than
   filling at a price nobody could have paid.
5. **Given** a configuration with costs, **When** a trade executes, **Then** the brokerage and
   slippage it bore are recorded on that trade and included in the cash that results.
6. **Given** a signal that produced no trade, **When** a reader asks why, **Then** a reason is
   recorded — no cash, already held, not selected, or not executable.

---

### User Story 2 - The result is compared with doing nothing clever (Priority: P1)

A curve that rises means nothing on its own. The question is whether it rose more than the market
it was picking from, after costs.

**Why this priority**: A backtest without a benchmark is the most misleading artefact this product
could produce, and it would be produced by default. The two P1 stories ship together for the same
reason feature 015's did: either alone looks finished and is worse than neither.

**Independent test**: Run a backtest over a window, read the benchmark's return over the identical
window, and confirm both are reported side by side with the same start and end sessions.

**Acceptance Scenarios**:

1. **Given** a completed backtest, **When** its result is read, **Then** the benchmark's return
   over the identical session range is reported beside the strategy's.
2. **Given** a backtest whose range begins before its market's benchmark series does, **When** the
   result is read, **Then** the comparison is stated as unavailable for that window with the
   reason, and is never silently truncated or back-filled.
3. **Given** a multi-market backtest, **When** a benchmark is reported, **Then** the specification
   states which series it is and why that one.
4. **Given** any reported measure, **When** it is read, **Then** total return, annualised return,
   volatility, maximum drawdown, trade count and total costs are all present — a result may not
   report only the flattering ones.

---

### User Story 3 - A portfolio can hold more than one currency (Priority: P2)

The universe spans SEK, DKK, NOK and EUR, and this product has never converted between currencies:
every price is stated in its listing currency and nothing is compared across them. A portfolio
holding instruments from four markets cannot avoid the question.

**Why this priority**: Without it a backtest is confined to one market at a time, which is useful
and honest but not what the universe is. With it, conversion becomes new behaviour that has to be
stated rather than assumed.

**Independent test**: Run a backtest spanning two markets in a stated accounting currency and
confirm every position, every trade and the curve are expressed in it, at rates from the session
concerned.

**Acceptance Scenarios**:

1. **Given** a configuration naming an accounting currency, **When** an instrument in another
   currency is traded, **Then** the conversion uses a rate as of the execution session and that
   rate is recorded on the trade.
2. **Given** a session with no stored rate for a currency, **When** the simulation needs one,
   **Then** it records that it could not value the position and why, rather than carrying the
   previous rate forward silently.
3. **Given** a stated currency spread, **When** a conversion happens, **Then** its cost is recorded
   and included in the result.
4. **Given** a single-currency backtest, **When** it runs, **Then** no conversion happens and no
   rate is required.

---

### User Story 4 - A person can read the result, and argue with it (Priority: P2)

A curve in a canvas is not an explanation. Somebody has to be able to see what was bought, when,
why, and what it cost — and to reach the strategy's own reasons from any trade.

**Independent test**: Open a completed backtest, read its measures and its trades, and move from
one trade to the signal that caused it and the contributions behind that.

**Acceptance Scenarios**:

1. **Given** a completed backtest, **When** it is opened, **Then** its configuration is stated in
   full — strategy version, range, capital, currency, sizing, schedule and costs.
2. **Given** an equity curve, **When** it is read by somebody who cannot see the chart, **Then**
   the same information is available as text.
3. **Given** a trade, **When** a reader follows it, **Then** they reach the signal that caused it
   and the per-factor contributions behind that signal.
4. **Given** any surface showing a backtest, **When** it is read, **Then** it states that the
   result is a simulation over past data and not a prediction.

---

### Edge Cases

- **A signal on the last stored session.** There is no next session to execute on, so no trade
  happens and the reason is recorded. The backtest does not extend past its data.
- **An instrument that stops trading mid-range.** A held position that can no longer be priced is
  recorded as unvalued with a reason from the session it stops, and the equity curve states that
  it is incomplete for those sessions rather than carrying the last price forward as though it
  were still true.
- **A signal to buy with no cash.** No trade, reason recorded.
- **A signal to buy something already held**, under a rule that does not add to positions. No
  trade, reason recorded.
- **A rebalance session that is not a trading session** on some market in the universe. Each
  instrument executes on the next session *it* traded, so markets with different holidays do not
  force each other to trade on days they were closed.
- **A corrected bar inside a completed backtest's range.** The stored result was correct for the
  data it read and is not silently rewritten; it records the data it was computed from, so a
  reader can tell that it predates a correction.
- **Fewer instruments with signals than the sizing rule wants to hold.** The portfolio holds what
  there is, and the shortfall is recorded rather than concentrated into the remainder.
- **The whole range produces no trade at all.** A result of "this did nothing" is a result, and is
  reported as one rather than as a failure.

## Requirements *(mandatory)*

### Functional Requirements

**The configuration**

- **FR-001**: A backtest MUST be defined by a stated configuration: the strategy version it
  replays, the universe, the session range, the starting capital, the accounting currency, the
  sizing rule, the rebalance schedule, and the costs.
- **FR-002**: A configuration MUST be immutable once a result has been recorded against it.
  Changing any part produces a new configuration, so a result recorded months ago stays
  reproducible from the rules that produced it.
- **FR-003**: Every result MUST name the configuration that produced it and the strategy version
  it replayed.

**Execution**

- **FR-004**: A signal as of a session MUST NOT execute on that session. It executes no earlier
  than the next session the instrument actually traded.
- **FR-005**: The execution price MUST be a stated price from the execution session, and the
  specification MUST say which one.
- **FR-006**: When an instrument does not trade again within a stated number of sessions, the
  simulation MUST record that the trade did not happen and why.
- **FR-007**: No value used by a backtest MAY come from a session later than the one being
  simulated.

**Costs**

- **FR-008**: Brokerage, slippage and any currency spread MUST be part of the configuration and
  MUST be recorded on the trades that bore them.
- **FR-009**: A result MUST report total costs alongside total return, so the two are never
  separated.

**Accounting**

- **FR-010**: The simulation MUST record cash and every position at every session in the range.
- **FR-011**: A position that cannot be valued at a session MUST be recorded as unvalued with a
  reason, and the equity for that session MUST state that it is incomplete rather than assume a
  price.
- **FR-012**: When an instrument's currency differs from the accounting currency, conversion MUST
  use a stored rate as of the execution or valuation session, and that rate MUST be recorded.
- **FR-013**: A single-currency backtest MUST require no rate and perform no conversion.

**Traceability**

- **FR-014**: Every trade MUST name the signal that caused it, so a reader can reach the strategy's
  own contributions from any trade.
- **FR-015**: Every signal that produced no trade MUST record a reason from a stated vocabulary.

**Reproducibility**

- **FR-016**: Recomputing a backtest over unchanged data MUST produce an identical result in every
  stored field.
- **FR-017**: A completed result MUST NOT be silently rewritten when underlying data is later
  corrected. It MUST record what it was computed from, so a reader can tell it predates a change.

**Measures and comparison**

- **FR-018**: A result MUST report total return, annualised return, volatility, maximum drawdown,
  trade count and total costs. Reporting a subset is not permitted.
- **FR-019**: A result MUST report the benchmark's return over the identical session range.
- **FR-020**: When the benchmark series does not cover the range, the comparison MUST be stated as
  unavailable with the reason, never truncated silently or back-filled.
- **FR-021**: Every surface showing a backtest MUST state that it is a simulation over past data
  and not a prediction.

**Out of scope, stated as requirements so they are testable**

- **FR-022**: The feature MUST NOT short, borrow, or apply leverage.
- **FR-023**: The feature MUST NOT fit, tune or search any parameter. A configuration is written
  down by a person and reviewed, exactly as a strategy version is.
- **FR-024**: The feature MUST NOT produce an order, an order intent, or anything a broker could
  act on.

### Test-First Proof *(mandatory)*

- **Initial failing test**: `TestTheSameConfigurationProducesTheIdenticalResult` — run a backtest
  over the fixture, record every trade, cash position and measure, recompute, and assert every
  stored field is identical.
- **Expected red reason**: No result is produced. A value failure on stored data, not a
  compilation or setup failure.
- **Green evidence**: The backtest suite, plus the existing strategy, feature-engine and
  market-data suites unchanged — signals are read, not recomputed, and nothing this feature does
  may alter them.
- **Database migration proof**: Configurations, runs, trades, positions, curve points and measures
  are new stored facts, and benchmark and rate series are new stored data. They arrive as ordered
  migrations, with a test proving a clean install and an upgrade both arrive with the tables
  present, the first configuration published, and no manual step.

### Responsive UI Behavior *(mandatory for user-facing features; otherwise state N/A)*

- **Mobile (320-767 CSS px)**: A result reads as a stacked record — configuration, measures beside
  the benchmark's, then trades as cards — without horizontal page scrolling. The equity curve
  collapses to its stated figures rather than a squeezed chart. The 360x800 scenario asserts the
  measures, the benchmark comparison and the not-a-prediction statement are all reachable.
- **Tablet (768-1023 CSS px)**: The 768x1024 scenario asserts the same content with the trades in
  the tabular layout the other screens adopt at that width.
- **Desktop (1024+ CSS px)**: The 1440x900 scenario asserts the same at the shared content width,
  with the curve drawn and its figures still present as text.
- **Input and accessibility**: Every figure the curve conveys is available as text; direction and
  magnitude are never carried by colour alone; the path from a trade to its signal is keyboard
  reachable. At the 320-pixel floor nothing clips.

### Live Update Behavior *(mandatory for client-visible data; otherwise state N/A)*

- **Snapshot and events**: A completed backtest is a committed domain change and publishes a
  versioned, authorization-scoped, resumable event carrying the run and its configuration, never
  the result itself. REST loads the result.
- **Reliability**: Ordering, event identifiers, resumption from the last delivered event,
  duplicate-safe consumption and bounded coalescing follow the existing contracts exactly.
- **Test evidence**: A reader watching while a backtest completes sees it arrive without
  reloading, and a reconnecting client replays it exactly once.

### Identity, Ownership, and Permissions *(mandatory for user/account data; otherwise state N/A)*

- **Ownership and authorization**: Backtest configurations and results are shared reference data
  that every authenticated user may read — this feature introduces no user-owned record, which is
  Milestone 6's subject. Running a backtest is an owner action at the command line, never an
  interface action, so no request can make the product compute a result.
- **Security evidence**: Tests prove the reads are refused to an unauthenticated and to a
  deactivated caller.

### Key Entities *(include if feature involves data)*

- **Backtest configuration**: The stated, immutable rules — strategy version, universe, range,
  capital, accounting currency, sizing, schedule, costs.
- **Backtest run**: One execution of a configuration, with its outcome and what it read.
- **Simulated trade**: One execution — instrument, session, direction, quantity, price, costs,
  conversion rate, and the signal that caused it.
- **Position and cash**: What was held and what remained, at every session.
- **Equity point**: The portfolio's value at a session in the accounting currency, or a stated
  reason it could not be valued.
- **Measure set**: The reported figures for the strategy and for the benchmark over the same range.
- **Benchmark series**: A stored index series, with its own coverage.
- **Rate series**: A stored currency pair, with its own coverage.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Recomputing a backtest over unchanged data produces zero differing fields.
- **SC-002**: No trade executes on the session whose signal caused it; every trade executes on a
  session where its instrument actually traded.
- **SC-003**: Every trade names the signal behind it, and a reader can reach that signal's
  contributions from the trade without knowing an identifier.
- **SC-004**: Every result reports all six measures and the benchmark's return over the identical
  range, or states plainly why the comparison is unavailable.
- **SC-005**: Total costs are reported on every result, and a result with costs configured to zero
  says so rather than omitting the figure.
- **SC-006**: Every signal in the range either produced a trade or recorded a reason it did not.
- **SC-007**: A backtest over the curated universe and its stored history completes within a
  stated time budget on the deployment's own hardware.
- **SC-008**: Every surface showing a result states that it is a simulation over past data and not
  a prediction.
- **SC-009**: No position is ever valued at a price from a session where its instrument did not
  trade.

## Assumptions

- **Execution at the next session's open.** A signal is computed from a session's close, so
  executing at that close assumes somebody acted on a price they only learned when the session
  ended. The open of the next session the instrument traded is the earliest price a person could
  actually have paid. Stored bars carry an open, so this needs no new data.
- **The benchmark is per market, not one for the universe.** Comparing a Swedish holding with a
  Norwegian index would be a claim about a relationship that does not exist. A multi-market result
  reports each market's own benchmark, and the universe composite the feature engine already
  computes remains the only universe-wide comparison this product makes.
- **The Danish gap is stated, not papered over.** `OMXC25.INDX` begins 2016-12-19, 110 days after
  stored history. A backtest covering that window reports the Danish comparison as unavailable for
  it. Back-filling with `OMXC20`, which the index replaced, would splice two different things and
  call the result one series.
- **Equal-weight the top N by score, rebalanced on a stated schedule.** The simplest rule that
  uses the strategy's output and nothing else. Anything cleverer is position sizing, which is
  Milestone 6's subject and needs its own specification.
- **Costs default to stated non-zero values.** A default of zero would make the first result
  anybody runs the most flattering one, and defaults are what people keep.
- **Rates and index series are stored, not fetched during simulation.** A backtest reads stored
  data only, which is what makes recomputation reproducible and keeps the provider out of the
  simulation path entirely.
- **Currency conversion is new behaviour and is confined to backtesting.** Nothing else in the
  product converts, and this does not change that: the Markets screens still state every price in
  its listing currency.
