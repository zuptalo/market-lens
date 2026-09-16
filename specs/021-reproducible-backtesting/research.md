# Phase 0 Research: Reproducible Backtesting

Nine decisions, each checked against the data or the code it depends on.

---

## R-001: What price does a trade execute at?

**Decision**: the open of the next session the instrument actually traded.

A signal as of a session is computed from that session's close. Filling at that close assumes
somebody acted on a price they only learned when the session ended — a lookahead small enough to
be invisible and large enough to flatter every result this product will ever produce.

The open of the next traded session is the earliest price a person could actually have paid.
`daily_price_bars` already stores `open numeric(20,8) NOT NULL`, so this needs no new data.

**Alternatives rejected**: the signal session's close, which is lookahead; the next session's
close, which is defensible but discards a whole session of information for no gain in honesty.

---

## R-002: Where do benchmark and rate series live?

**Decision**: their own tables, not `instruments`.

An instrument in this product carries an exchange, an ISIN, a sector, a purchasability status and
a membership in a curated universe. It is imported by the daily pass, computed over by the feature
engine, scored by strategies and listed on Markets. An index is none of those things, and a
currency pair is further from it still.

Storing them as instruments would put OMXS30 in the Markets table, feed it to the feature engine,
and let a strategy score the index it is being compared against. Each of those is a separate
absurdity and all three arrive together.

**Alternatives rejected**: instruments on a synthetic exchange, which requires excluding them from
every read that already exists — the exclusions are the tell.

---

## R-003: How do those series arrive?

**Decision**: an owner command, reading through the provider client that already exists.

The client's `Daily` call takes a provider symbol and a range and does not consult the exchange
whitelist, so it can already fetch `OMXS30.INDX` and `EURSEK.FOREX`. What is missing is a command
to store what comes back, and storage keyed by series rather than by instrument.

Series are imported deliberately, not nightly: an index moves daily, but a backtest reads stored
data only (R-007), so a series that is a week stale changes nothing until somebody runs a backtest
over that week.

---

## R-004: How is a configuration stated?

**Decision**: published by ordered migration and superseded rather than edited, exactly as a
strategy version is.

A result recorded months ago must stay reproducible from the rules that produced it. The same
argument that made a strategy version immutable applies with more force here, because a
configuration is where somebody would be tempted to change a number until the curve improved —
which is parameter fitting wearing a different hat, and FR-023 forbids it.

---

## R-005: What makes a recomputation identical?

**Decision**: the same three rules the strategy layer uses, for the same reasons.

A stated evaluation order over sessions and instruments, decimals stored as strings at twelve
places, and no map iteration reaching a stored value. Ranking ties are already resolved by
instrument identifier in the signal layer, which the simulation reads rather than recomputes.

Money is the new case: cash, costs and quantities compound across thousands of sessions, so a
rounding rule applied inconsistently would drift invisibly. Every monetary value is rounded once,
at the stored precision, at the point it is recorded — and the accounting identity (cash plus
positions equals equity) is asserted at every session rather than at the end.

---

## R-006: What does the simulation hold?

**Decision**: equal weight across the top N by score, rebalanced on a stated schedule, long only.

The simplest rule that uses the strategy's output and nothing else. Anything that varies size by
conviction, volatility or correlation is position sizing, which is Milestone 6's subject and needs
its own specification — and inventing it here would make this feature's results depend on an
unreviewed rule nobody agreed to.

A shortfall — fewer instruments with usable signals than N — is recorded and left as cash, not
concentrated into the remainder. Concentrating would quietly change the rule under load.

---

## R-007: Does the simulation ever call the provider?

**Decision**: never. It reads stored bars, stored signals, stored series.

This is what makes recomputation reproducible at all: a simulation that fetched would depend on
what a vendor served at the moment it ran. It also keeps a backtest runnable with no credential
and no network.

---

## R-008: How are the measures defined?

**Decision**: stated in the specification rather than left to a library, because each one has
several defensible definitions and the difference between them is exactly where a flattering
result hides.

| Measure | Definition |
|---|---|
| Total return | Final equity over starting capital, minus one |
| Annualised return | Geometric, over the sessions actually simulated, at 252 sessions a year |
| Volatility | Standard deviation of session returns, annualised at √252 |
| Maximum drawdown | The largest peak-to-trough fall in the equity curve |
| Trade count | Every execution, including the ones that closed a position |
| Total costs | Brokerage plus slippage plus any currency spread, in the accounting currency |

A session on which equity could not be valued (FR-011) is excluded from the return series and
counted, rather than treated as a flat day — a flat day is a claim, and an unvalued one is not.

---

## R-009: What does it cost to run?

**Decision**: the budget scales from the strategy layer's, measured the same way.

The simulation reads roughly what a strategy run writes — a hundred instruments over ~2,500
sessions — and does arithmetic per session rather than per factor. The stored result is small:
one equity point per session, positions only for what is held, and trades in the hundreds.

Unlike the feature engine, this is not incremental. A backtest is a whole-range computation by
nature, so its budget is stated as one full pass rather than as a nightly increment.
