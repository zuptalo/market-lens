# Quickstart: Reproducible Backtesting

What a backtest is, how to run one, and how to check it is not flattering itself.

---

## What a backtest is, and is not

It replays stored signals over stored sessions under a stated, immutable configuration, and
records what would have happened. It is a simulation over past data. It is not a prediction, not
advice, and not evidence that the same rules will work again — every surface showing one says so.

It reads stored data only. Nothing it does calls the provider, which is what lets a result be
reproduced exactly and what lets a backtest run with no credential and no network. The absence is
structural rather than careful: the package cannot import a provider client, and a test fails if
one is ever added.

---

## Bring in the series it compares against

Benchmarks and rates are stored deliberately, not nightly. The four benchmarks arrive published by
migration, each attached to the market it is the benchmark *for*, so an import names a series that
already exists rather than inventing one.

```bash
market-lens series import --benchmark OMXS30.INDX
market-lens series import --benchmark OMXH25.INDX
market-lens series import --benchmark OBX.INDX
market-lens series import --benchmark OMXC25.INDX

market-lens series import --rate EURSEK
market-lens series import --rate EURNOK
market-lens series import --rate EURDKK
```

Each import prints what the series actually covers. That is the point of running it first: a
series that begins after the range a backtest asks about produces a stated absence rather than a
comparison, and this is where you find that out before running anything.

```text
series=OMXC25.INDX stored=2434 revised=0 unchanged=0 sessions=2434 first=2016-12-19 last=2026-09-15
```

`OMXC25.INDX` begins 2016-12-19, 110 days after this product's stored history. That is not a
defect to work around: a backtest covering the opening window reports the Danish comparison as
unavailable for it, and says why. Splicing in `OMXC20`, the index it replaced, would join two
different things and present the result as one series.

The rates are quoted with the euro as the base because the published configuration keeps its
accounting in euro — which is *why* it does: every rate this universe needs is served in that
direction, and choosing kronor would have meant deriving three cross rates through the euro
anyway, with three more places to be wrong. The inverse is never stored; converting into the
accounting currency divides, so the two directions cannot disagree.

---

## Run one

```bash
market-lens backtest run --configuration momentum_trend_equal_weight
```

An owner action at the command line. No request over HTTP can make the product compute a result,
and nothing on a schedule decides to.

```text
run_id=… status=succeeded from=2016-08-31 to=2026-09-15 trades=312 skipped=11840 rebalances=121
```

---

## Read it

```bash
curl -s .../api/v1/backtests | jq '.items[0] | {id, status, configuration: .configuration.name}'
curl -s .../api/v1/backtests/<id> | jq '{measures, benchmarks}'
curl -s .../api/v1/backtests/<id>/trades | jq '.items[:3]'
curl -s .../api/v1/backtests/<id>/equity | jq '.items | length'
```

Or open `/backtests` in the application, where each trade leads to the signal that caused it and
the per-factor contributions behind that.

---

## Check it is telling the truth

Run it twice, then put both run identifiers into the first query.

**It reproduces (SC-001)**

```sql
SELECT count(*) FROM backtest_trades a JOIN backtest_trades b
  USING (instrument_id, signal_session)
WHERE a.run_id = '<first>' AND b.run_id = '<second>'
  AND (a.execution_session, a.quantity, a.price, a.brokerage, a.slippage,
       a.spread_cost, a.cash_effect)
   IS DISTINCT FROM
      (b.execution_session, b.quantity, b.price, b.brokerage, b.slippage,
       b.spread_cost, b.cash_effect);
-- expect 0
```

**Nothing executed on the session that produced its signal (SC-002)**

```sql
SELECT count(*) FROM backtest_trades WHERE execution_session <= signal_session;
-- expect 0 — and the table's check constraint means it can only ever be 0
```

**Nothing was bought or sold on a day it did not trade (SC-009)**

```sql
SELECT count(*) FROM backtest_trades t
WHERE t.run_id = '<run>' AND NOT EXISTS (
  SELECT 1 FROM daily_price_bars b
  WHERE b.instrument_id = t.instrument_id AND b.session_date = t.execution_session);
-- expect 0
```

**Nothing was valued at a price its instrument never quoted (SC-009)**

```sql
SELECT count(*) FROM backtest_positions p
WHERE p.run_id = '<run>' AND p.price_session IS NOT NULL AND NOT EXISTS (
  SELECT 1 FROM daily_price_bars b
  WHERE b.instrument_id = p.instrument_id AND b.session_date = p.price_session);
-- expect 0
```

On a market holiday `price_session` is an earlier session the instrument did trade. Beyond the
give-up window it has stopped trading, the position is unvalued instead, and the equity for that
session says it is incomplete rather than repeating yesterday's number.

**Every trade names its reason (SC-003)**

```sql
SELECT count(*) FROM backtest_trades t WHERE NOT EXISTS (
  SELECT 1 FROM signals s WHERE s.instrument_id = t.instrument_id
    AND s.session_date = t.signal_session AND s.strategy_id = t.strategy_id);
-- expect 0 — a foreign key, so it can only ever be 0
```

**Every considered signal that did nothing says why (SC-006)**

```sql
SELECT reason, count(*) FROM backtest_skips WHERE run_id = '<run>'
GROUP BY reason ORDER BY 2 DESC;
-- expect: every absence carries a reason a person can argue with
```

A signal on a session the schedule does not trade on is not in this table. It was never
considered, and the run's `rebalance_count` accounts for it — writing a quarter of a million rows
saying "it was a Tuesday" would bury the twelve thousand that record an actual decision.

**The accounting closes at every session (FR-010)**

```sql
SELECT count(*) FROM backtest_equity e
WHERE e.run_id = '<run>' AND e.total IS NOT NULL
  AND e.total <> e.cash + coalesce((SELECT sum(p.value) FROM backtest_positions p
    WHERE p.run_id = e.run_id AND p.session_date = e.session_date), 0);
-- expect 0 at every session, not merely at the last one
```

**The costs are there (SC-005)**

```sql
SELECT total_return, total_costs, max_drawdown FROM backtest_measures
WHERE run_id = '<run>' AND subject = 'strategy';
-- a return reported without its costs and its drawdown beside it is a sales pitch
```

---

## Reading the result honestly

A few things worth remembering when the first curve appears:

- **One configuration is one experiment.** Running several and keeping the best is parameter
  fitting, and the product refuses to do it for you (FR-023) precisely because it is so easy to do
  by hand.
- **The benchmark is the point.** A curve that rose during a rising market has not demonstrated
  anything. The comparison over the identical window is what the measures exist for.
- **Costs move results more than most people expect** over thousands of sessions, which is why
  they default to non-zero and appear beside the return rather than in a footnote.
- **The measures include the unflattering ones by requirement.** Maximum drawdown is what the
  strategy would have put you through, and a result that omitted it would be a different kind of
  document.
- **A result is not rewritten when a bar is later corrected.** Each run records the data it read —
  `signals_computed_through` and `bars_observed_through` — so a reader can tell that a result
  predates a restatement rather than discovering it silently changed.

---

## What this does not do

- No shorting, no leverage, no dividends or total-return accounting.
- No position sizing beyond equal weight across the top N by score, in whole shares — anything
  cleverer is Milestone 6, which has its own specification.
- No fitting, tuning or search of any parameter.
- No order, order intent, or anything a broker could act on.
