# Quickstart: Paper Trading

## Opening the account

Choose what it starts with and what currency it reports in. Both are fixed from that moment, and so
are the trading costs it charges — a record that can be retuned once the result is known is not a
record of anything. There is one account per person, and no way to reset it.

## Getting an order into it

Write down an intent on the **Intents** screen, then promote it here. That is the only way an order
comes into existence: no strategy places one, no schedule places one, and neither does the product.

The order sits **waiting**. It fills at the **open of the next session** that instrument trades —
a price nobody had seen when you promoted it. That is the whole point: filling at anything you could
already see is the easiest way to make a simulated account look good, and it is invisible in the
resulting figures.

Fills happen on the server after each market-data import, whether or not anybody is looking.

## Reading it honestly

- **What you expected sits beside what it actually paid.** The gap between them is the part of a
  simulation people forget exists.
- **The costs travel separately from the price**, so the price can be checked against the bar.
- **An order that could not fill says why**: not enough cash, more than the account held, or no
  price ever arrived. It is never partially filled — a size nobody asked for would be the product
  making a decision.
- **A corrected price is reported, never applied.** If the bar behind a fill is later re-observed
  and changed, the fill stands and the divergence is shown. Re-pricing it would make last week's
  figure differ from this week's with nothing to explain it.
- **This is the one screen in the product that reports a return**, and it says why it may: the
  account started at a stated balance and this product recorded every movement since. Your real
  portfolio cannot, because it never saw what you paid in or took out.

## What it deliberately cannot do

No broker connection. No short selling, leverage or margin. No intraday fills — the product stores
daily bars, and a fill at anything but a session open would be invented. No second account, no
reset, and no way to delete a filled order.

And **no strategy-driven account**. Everything needed for one now exists — orders, fills, a
scheduled pass, somewhere to keep the result — which is exactly why its absence is written down as
a requirement rather than left unmentioned. Feature 021 measured `momentum_trend` over ten years
and found it lost to two of its three benchmarks. Turning that into an equity curve that looks like
a track record would need a specification that argues for it, not a scheduler change.

---

## Recorded evidence

`v0.22.0` on k3s, 2026-09-18. Nothing was seeded; every figure below is the deployment's own state.

**The tables arrived with the release and are empty**, which is what a feature nobody has used yet
should look like:

```text
migration: 29 at 2026-09-18 09:51:46+00
accounts: 0   orders: 0   fills: 0
```

**Nothing in them could be transmitted, shorted or levered.** Asked directly of the production
schema, across both `paper_orders` and `paper_fills`:

```text
forbidden columns: 0
  (venue, order_type, time_in_force, destination, expires_at, broker_reference,
   external_id, limit_price, stop_price, leverage, margin, short)
```

**Both triggers are installed:**

```text
paper_accounts_immutable, paper_fills_follow_their_order
```

**Every constraint refuses what it should, and accepts what it should.** Run against production
inside a transaction that was rolled back, using a real owner and a real instrument so nothing but
the constraint under test could be responsible:

```text
refused: a second account for the same person       -> paper_accounts_user_id_key
refused: changing the starting balance after opening -> a paper account's terms are fixed when it is opened
refused: a fill in the session the order was placed  -> a paper order placed on 2026-06-10 cannot fill
                                                        at 2026-06-10: that price already existed
                                                        when it was placed
refused: a fill before the order was placed          -> (the same trigger, from the other side)
refused: a short sale                                -> paper_orders_direction_check
refused: a negative starting balance                 -> paper_accounts_starting_cash_check
refused: an order with no intent behind it           -> paper_orders_intent_id_fkey
accepted: a fill at a later session
refused 7 of 7
```

The last two are the ones worth reading together. **An order cannot exist without an intent behind
it** — that is FR-001 enforced by a foreign key rather than by a service, so no future code path can
create one. And **a fill at a later session is accepted while one at the placement session is
refused**, which is the whole feature in two lines.

After the rollback: `accounts: 0, orders: 0, fills: 0`, and the intents table untouched.

**Every route requires a session:**

```text
GET    /api/v1/paper-account              -> 401
GET    /api/v1/paper-account/orders       -> 401
POST   /api/v1/paper-account              -> 401
DELETE /api/v1/paper-account/orders/{id}  -> 401
```

**What could not be checked from outside, and why.** This deployment answers 401 for every path,
including ones that do not exist, because authentication runs ahead of routing. So those 401s prove
the routes are guarded, not that they exist — the passing contract test and the diff are the
evidence for that, as they were for features 024 and 025.

**What to check when you next open it.** Open an account, promote something you are considering, and
confirm it reads *Waiting* rather than filling immediately. After the next overnight import it
should read *Filled*, at the open of a session **after** the one you promoted it in — compare the
price it paid against the price you expected, which is the part of a simulation that is easiest to
forget exists.
