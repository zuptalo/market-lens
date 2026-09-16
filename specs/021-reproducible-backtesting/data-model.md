# Phase 1 Data Model: Reproducible Backtesting

Two new kinds of stored series, and the record of a simulation. Nothing existing changes.

---

## Stored series

### `benchmark_series` / `benchmark_points`

An index the product compares against. Not an instrument (R-002): it has no exchange membership,
no sector, no purchasability, never enters a universe, and is never scored.

| Column | Rule |
|---|---|
| `code` | The provider's symbol, e.g. `OMXS30.INDX` |
| `name`, `currency` | As the provider states them |
| `mic` | The market this series is the benchmark *for*, so a result knows which to compare with |

Points carry `session_date` and `close`, unique per series and session.

### `fx_rates`

| Column | Rule |
|---|---|
| `base`, `quote` | Three-letter codes, e.g. `EUR` / `SEK` |
| `session_date`, `rate` | `numeric(24,12)`, unique per pair and session |

A rate is stored as the provider quotes it. Conversion in the other direction divides rather than
storing a second row, so the two directions can never disagree.

---

## The configuration

### `backtest_configurations`

Immutable once a result exists against it, published by migration, superseded rather than edited
(R-004): strategy version, universe, session range, starting capital, accounting currency, sizing
rule and its N, rebalance schedule, and the costs — brokerage basis points and minimum, slippage
basis points, currency spread basis points.

Held as one document for the same reason a strategy version is: it *is* the configuration, and
splitting it into columns would let one part change without publishing a new version.

---

## The result

### `backtest_runs`

One execution: the configuration, when it ran, its outcome, and what it read — the strategy run
whose signals it replayed, so a reader can tell a result predates a later correction (FR-017).

### `backtest_trades`

One execution each: instrument, the session whose signal caused it, the session it executed on,
direction, quantity, price, brokerage, slippage, conversion rate, and the signal identifier.

The signal identifier is what makes FR-014 true by construction rather than by convention: a trade
cannot exist without naming the reason it happened.

### `backtest_positions`

What was held at each session, and at what price it was valued — or the stated reason it could not
be (FR-011).

### `backtest_equity`

One point per session: cash, position value, total, in the accounting currency — or an absence
reason. A session that could not be valued says so rather than repeating yesterday's number.

### `backtest_measures`

The six figures for the strategy and, separately, for each benchmark over the identical range —
with an absence reason where a series does not cover the window, which is the Danish case.

### `backtest_skips`

Every signal that produced no trade, with a reason from a stated vocabulary: no cash, already
held, not selected, not executable, no price. FR-015 is enforced by there being a row.

---

## Constraints worth stating in the schema

- A trade's execution session is strictly later than its signal session (FR-004).
- An equity point carries a total or an absence reason, never both and never neither — the same
  shape as a signal, for the same reason.
- A measure set carries a value or an absence reason, never both.
- Quantities and money are `numeric(24,12)`, matching the precision the feature and strategy layers
  already use.

---

## Migrations

- `0024_market_series.sql` — benchmark and rate series.
- `0025_backtests.sql` — configurations, runs, trades, positions, equity, measures, skips, and the
  first published configuration.

Each with a test proving a clean install and an upgrade both arrive with the tables present, the
constraints in force, and no manual step.
