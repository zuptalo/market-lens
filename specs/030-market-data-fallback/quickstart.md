# Quickstart: Market-data fallback provider

Every command runs where the others do — in production, `kubectl exec` into the pod:

```sh
market-lens marketdata fallback status      # enabled, active, since, instruments, pending
market-lens marketdata fallback audit       # re-verify every symbol against the evidence; read-only
market-lens marketdata fallback enable      # the switch, stored in the database (off by default)
market-lens marketdata fallback disable
market-lens marketdata fallback reconcile   # ask the primary for every fallback-covered session
```

## What happens on a lapse

1. The nightly import runs against the primary. Every item fails `provider_authentication`, the run
   ends `failed`, and the owner is told (pipeline-failure notice) — whether or not the fallback is on.
2. If the fallback is enabled, a child run of kind `fallback` asks Yahoo for each refused instrument,
   from the session after its newest primary price to tonight. An instrument with a split or dividend
   in that window is held back (`fallback_held_back`).
3. Fallback bars never replace primary bars. Features and signals recompute from them.
4. The state is observed: the banner appears on every screen, `market_data_fallback.changed.v1` is
   published, and the owner is told once if they opted in.
5. Paper orders whose next bar is a fallback bar wait; backtests never read fallback bars.

## When the subscription is renewed

The next nightly primary import re-observes its trailing window and replaces fallback bars inside
it, keeping each as a revision. If fallback bars remain older than that window, the banner says
reconciliation is pending; run `marketdata fallback reconcile`. It prints each replaced session with
both closes and flags any difference over 0.5%.
