# Phase 1 Data Model: Paper Trading

One migration, `0029_paper_trading.sql`, three tables, and a list of columns that are deliberately
absent.

## `paper_accounts`

One per person, and its terms cannot change after it is opened.

| Column | Type | Notes |
|---|---|---|
| `id` | uuid pk | |
| `user_id` | uuid not null, **unique** | one account per person, enforced by the index, not by a service |
| `starting_cash` | numeric(24,12) not null | `CHECK (starting_cash > 0)` |
| `accounting_currency` | text not null | |
| `brokerage_bps` | numeric(24,12) not null | feature 021's defaults |
| `brokerage_minimum` | numeric(24,12) not null | |
| `slippage_bps` | numeric(24,12) not null | |
| `currency_spread_bps` | numeric(24,12) not null | |
| `opened_at` | timestamptz not null default now() | |

A trigger refuses any update to `starting_cash`, `accounting_currency` or the four rates, the same
way feature 021's `backtest_configuration_is_immutable()` does. The point of the record is that it
cannot be tuned after the result is known.

## `paper_orders`

| Column | Type | Notes |
|---|---|---|
| `id` | uuid pk | |
| `account_id` | uuid not null | |
| `user_id` | uuid not null | denormalised, held equal by `(account_id, user_id)` composite FK |
| `intent_id` | uuid not null **unique** | an intent becomes at most one order |
| `instrument_id` | uuid not null | |
| `direction` | text not null | `CHECK (direction IN ('buy','sell'))` |
| `quantity` | numeric(24,12) not null | `CHECK (quantity > 0)` |
| `expected_price` | numeric(24,12) not null | `CHECK (expected_price > 0)` — what the person thought |
| `placed_session` | date not null | the session it was placed in |
| `state` | text not null | `CHECK (state IN ('pending','filled','cancelled','unfillable'))` |
| `absence_reason` | text | `CHECK ((state = 'unfillable') = (absence_reason IS NOT NULL))` |
| `placed_at` | timestamptz not null default now() | |
| `settled_at` | timestamptz | `CHECK ((state = 'pending') = (settled_at IS NULL))` |

`user_id` is denormalised and held equal to the account's owner by a composite foreign key, exactly
as feature 022 does, so every ownership predicate reads the row's own column and a forgotten join
cannot widen access.

## `paper_fills`

| Column | Type | Notes |
|---|---|---|
| `id` | uuid pk | |
| `order_id` | uuid not null **unique** | an order fills once or not at all; no partial fills |
| `user_id` | uuid not null | denormalised, composite FK to the order |
| `fill_session` | date not null | |
| `open_price` | numeric(24,12) not null | the stored open, unmodified |
| `quantity` | numeric(24,12) not null | |
| `costs` | numeric(24,12) not null | `CHECK (costs >= 0)` |
| `cash_effect` | numeric(24,12) not null | negative for a buy, positive for a sale |
| `conversion_rate` | numeric(24,12) not null | 1 when the currencies match |
| `bar_observed_at` | timestamptz not null | what the bar said *when it was read* (R-005) |
| `filled_at` | timestamptz not null default now() | |

A foreign key to `(instrument_id, fill_session)` in `daily_price_bars` ties the fill to the bar it
read. And, as feature 021 does for the same reason:

```sql
FOREIGN KEY (order_id) REFERENCES paper_orders (id),
CHECK (fill_session > (SELECT placed_session ...))  -- expressed as a trigger, see below
```

A check constraint cannot subquery, so **a trigger enforces `fill_session > placed_session`**. It
is the constraint that makes a lookahead bug impossible rather than merely unlikely, so it lives in
the database and not in a service.

## Deliberately absent

A migration test asserts these columns do **not** exist on `paper_orders` or `paper_fills`:

`venue`, `order_type`, `time_in_force`, `destination`, `expires_at`, `broker_reference`,
`external_id`, `limit_price`, `stop_price`, `leverage`, `margin`, `short`.

The first nine are feature 025's list, for the same reason: a field that exists will eventually be
filled in and then sent. The last three are this feature's own, because FR-025 excludes them and an
exclusion nobody can test is a comment.

## Derived, never stored

Position, cost, realised and unrealised result, cash balance, account value and total return are all
folds over the recorded fills. Nothing derived is stored, so nothing can drift from the fills behind
it — feature 022's rule, and what makes the figures reconcilable at twelve decimal places.

## Event

`paper_account.changed.v1`, **scope `user`** with `subject_user_id`, published in the transaction
that promotes, cancels or fills an order. It carries the account's identity, never its figures:
those are computed on read, through the authorized path.
