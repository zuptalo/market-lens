# Quickstart: Reproducible Backtesting

What a backtest is, how to run one, and how to check it is not flattering itself.

---

## What a backtest is, and is not

It replays stored signals over stored sessions under a stated, immutable configuration, and
records what would have happened. It is a simulation over past data. It is not a prediction, not
advice, and not evidence that the same rules will work again — every surface showing one says so.

It reads stored data only. Nothing it does calls the provider, which is what lets a result be
reproduced exactly and what lets a backtest run with no credential and no network.

---

## Bring in the series it compares against

Benchmarks and rates are stored deliberately, not nightly.

```bash
market-lens series import --benchmark OMXS30.INDX --mic XSTO
market-lens series import --benchmark OMXH25.INDX --mic XHEL
market-lens series import --benchmark OBX.INDX    --mic XOSL
market-lens series import --benchmark OMXC25.INDX --mic XCSE

market-lens series import --rate EURSEK
market-lens series import --rate EURNOK
market-lens series import --rate EURDKK
```

`OMXC25.INDX` begins 2016-12-19, 110 days after this product's stored history. That is not a
defect to work around: a backtest covering the opening window reports the Danish comparison as
unavailable for it, and says why. Splicing in `OMXC20`, the index it replaced, would join two
different things and present the result as one series.

---

## Run one

```bash
market-lens backtest run --configuration momentum_trend_equal_weight_v1
```

An owner action at the command line. No request over HTTP can make the product compute a result.

---

## Read it

```bash
curl -s .../api/v1/backtests | jq '.items[0] | {id, status, configuration: .configuration.name}'
curl -s .../api/v1/backtests/<id> | jq '{measures, benchmarks}'
curl -s .../api/v1/backtests/<id>/trades | jq '.items[:3]'
```

Or open `/backtests` in the application, where each trade leads to the signal that caused it and
the per-factor contributions behind that.

---

## Check it is telling the truth

**It reproduces (SC-001)**

```sql
-- with a previous run's trades kept in a temporary table
SELECT count(*) FROM backtest_trades t JOIN previous_trades p
  USING (run_configuration_id, instrument_id, signal_session)
WHERE t.execution_session IS DISTINCT FROM p.execution_session
   OR t.quantity IS DISTINCT FROM p.quantity
   OR t.price IS DISTINCT FROM p.price
   OR t.brokerage IS DISTINCT FROM p.brokerage;
-- expect 0
```

**Nothing executed on the session that produced its signal (SC-002)**

```sql
SELECT count(*) FROM backtest_trades WHERE execution_session <= signal_session;
-- expect 0 — and the table's check constraint means it can only ever be 0
```

**Nothing was priced on a day it did not trade (SC-009)**

```sql
SELECT count(*) FROM backtest_trades t
WHERE NOT EXISTS (
  SELECT 1 FROM daily_price_bars b
  WHERE b.instrument_id = t.instrument_id AND b.session_date = t.execution_session);
-- expect 0
```

**Every trade names its reason (SC-003)**

```sql
SELECT count(*) FROM backtest_trades WHERE signal_id IS NULL;
-- expect 0
```

**Every signal that did nothing says why (SC-006)**

```sql
SELECT reason, count(*) FROM backtest_skips GROUP BY reason ORDER BY 2 DESC;
-- expect: every absence carries a reason a person can argue with
```

**The costs are there (SC-005)**

```sql
SELECT total_costs, total_return FROM backtest_measures WHERE benchmark_series IS NULL;
-- a return reported without its costs beside it is a sales pitch
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

---

## What this does not do

- No shorting, no leverage, no dividends or total-return accounting.
- No position sizing beyond equal weight across the top N by score — anything cleverer is
  Milestone 6, which has its own specification.
- No fitting, tuning or search of any parameter.
- No order, order intent, or anything a broker could act on.
