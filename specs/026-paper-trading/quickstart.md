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
