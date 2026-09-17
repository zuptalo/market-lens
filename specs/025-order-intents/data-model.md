# Phase 1 Data Model: Order Intents

One new table. The consequence is computed on every read.

---

## `order_intents`

| Column | Rule |
|---|---|
| `id` | |
| `user_id` | `NOT NULL REFERENCES users`, read directly by every predicate — feature 022's discipline |
| `instrument_id` | `NOT NULL REFERENCES instruments`; the universe is the vocabulary |
| `direction` | `buy` or `sell` |
| `quantity` | `numeric(24,12) > 0` |
| `price` | `numeric(24,12) > 0`, what the person expects to pay, in the listing currency |
| `costs` | `numeric(24,12) >= 0`, what they expect it to cost |
| `status` | `considering`, `withdrawn`, `acted_on` |
| `recorded_at`, `settled_at` | When it was written down, and when it stopped being under consideration |

**Constraints in the schema**

- `settled_at` is set exactly when the status is not `considering` — a withdrawn intent with no date
  is a state nobody can read.
- Quantity and price are positive. A sale larger than the position is *not* refused here (R-004):
  that is a consequence to report, not a constraint to enforce.

**What is deliberately not a column**

No venue, no order type, no time in force, no destination, no expiry, no broker reference, no
external identifier. The absence is the safeguard, and the migration test names these so a later
addition has to argue with a failing test rather than a comment.

**What is not stored**

The consequence. The resulting position, the resulting share, and each limit's verdict are computed
on every read from the portfolio as it stands (R-002). An intent's *record* is what somebody was
thinking; its *consequence* is what acting today would do.

---

## What a consequence is

Computed per read by applying the proposed change to the portfolio's holdings and running feature
023's evaluator over the result:

| Field | Meaning |
|---|---|
| `resulting_quantity` | What would be held in that instrument afterwards; may be negative, and says so |
| `resulting_value`, `resulting_share` | What it would be worth and what share of the portfolio, or absent with a reason |
| `limits` | One entry per stated limit: the state it would be in, the figure it would reach, the threshold |

Every figure carries the values behind it, so a share can be reproduced rather than believed.

---

## Migration

`0028_order_intents.sql` — one table, its constraints, and no seed data.

Its test proves a clean install and an upgrade both arrive with the table present, `user_id` not
nullable, a status outside the vocabulary refused, a settled state without a date refused, a
non-positive quantity refused, **none of the broker-actionable columns present**, and no manual step.
