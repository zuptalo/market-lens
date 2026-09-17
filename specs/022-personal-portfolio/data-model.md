# Phase 1 Data Model: Personal Portfolio and Holdings

Two new tables, and nothing derived is stored. That second half is the design.

---

## `portfolios`

One per person, created on first use rather than by a setup step nobody would understand the
purpose of.

| Column | Rule |
|---|---|
| `user_id` | The owner. `NOT NULL`, unique — one portfolio per person (spec assumption) |
| `accounting_currency` | Three-letter code, chosen by the owner from the currencies the universe trades in |
| `created_at`, `updated_at` | |

The cost basis is not a column. It is first-in-first-out for every portfolio, stated on every
realised figure, and a column would imply a choice the product does not offer.

---

## `portfolio_trades`

What a person asserted. The only table in this feature that holds a fact.

| Column | Rule |
|---|---|
| `id` | |
| `portfolio_id` | `NOT NULL REFERENCES portfolios` |
| `user_id` | `NOT NULL`. Denormalised from the portfolio **on purpose** — every ownership predicate reads it directly, so a query cannot accidentally scope by portfolio alone and a missing join cannot widen access. A constraint keeps it equal to the portfolio's owner |
| `instrument_id` | `NOT NULL REFERENCES instruments` — the universe is the vocabulary (R-008) |
| `direction` | `buy` or `sell`. No short, no leverage |
| `quantity` | `numeric(24,12) > 0`. Fractional allowed: brokers fill fractions |
| `price` | `numeric(24,12) > 0`, in the instrument's listing currency |
| `costs` | `numeric(24,12) >= 0`, what the trade bore, in the listing currency |
| `trade_date` | `date NOT NULL`, not in the future |
| `sequence` | Per-portfolio monotonic. With `trade_date` it totally orders the FIFO walk (R-003) |
| `superseded_by` | The version that replaced this one, or null |
| `withdrawn_at` | Set when withdrawn; the row stays readable |
| `recorded_at` | |

**Constraints worth stating in the schema**

- `user_id` equals the owning portfolio's `user_id`, enforced by a composite foreign key rather
  than by a trigger, so the two can never disagree.
- `trade_date <= current_date`, so a holding nobody has yet cannot be recorded.
- A row is superseded or withdrawn, never both.
- `(portfolio_id, sequence)` is unique.

**What is not here.** No position table, no cost column, no realised-profit column, no valuation
row. FR-010 requires those to be derived, and a stored copy is a second source of truth that every
correction would have to repair.

---

## What is derived, and from what

| Figure | Derivation |
|---|---|
| Position | Sum of current, non-withdrawn buys minus sells for one instrument |
| Cost of what is still held | FIFO fold over the purchases in `(trade_date, sequence)` order, including each purchase's costs |
| Realised profit | Per sale: proceeds less the FIFO cost of the shares it consumed, less both trades' costs |
| Value | Quantity × latest stored close for that instrument, converted (R-004), or a stated absence |
| Holding return | Value against current cost, over the window R-007 defines |
| Benchmark comparison | The market's benchmark between the same two sessions, or a stated absence |

Every one of these reads only stored prices, stored rates and the person's own trades.

---

## Absence, everywhere it can occur

The same discipline as feature 021: a figure that cannot be computed is stated as absent with a
reason, never as a zero and never as a stale value carried forward.

| Absence | When |
|---|---|
| `no_price` | The instrument has no stored close at or before the valuation session |
| `no_rate` | Either leg of the euro cross is missing for the session |
| `benchmark_uncovered` | The series does not span the holding's window |
| `no_portfolio_return` | Always, at portfolio level — cash is not tracked (FR-016a) |

The last one is not an error state. It is the product declining to divide by a number it does not
have, and saying so.

---

## Migration

`0026_personal_portfolios.sql` — both tables, their constraints, and no seed data. A person's
portfolio is created when they first record a trade; there is nothing to publish by migration.

Its test proves a clean install and an upgrade both arrive with the tables present, `user_id` not
nullable on either, the owner-matching constraint refusing a mismatched pair, a future-dated trade
refused, and no manual step.
