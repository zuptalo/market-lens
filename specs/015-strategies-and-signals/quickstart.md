# Quickstart: Deterministic Strategies and Signals

What the strategy layer is, how to run it, and how to check it is telling the truth.

---

## What a signal is, and is not

A signal is a record that one **version** of one strategy scored one instrument, as of one
session, from features that existed then — with the reasons. It is not advice, not a prediction,
and not a claim that the view is correct. The strategy exists to prove the platform can hold a
view reproducibly; whether the view is any good is a question backtesting answers, and
backtesting is the next milestone.

Every surface that shows a signal carries the strategy's own caveat, which is stored on the
version rather than written into a template, so no screen can display a score without it.

---

## Compute signals

```bash
# Everything, from empty. Budget: 10 minutes on the deployment's hardware.
market-lens signals compute --universe nordic-liquid-v1

# Only the sessions a given feature run changed. Budget: 30 seconds.
market-lens signals compute --since-feature-run <feature-run-id>

# One strategy version across the full history, after publishing a new version.
market-lens signals compute --strategy momentum_trend --version 2
```

An import triggers a feature computation, and a successful feature computation triggers signals.
The command line is for a rebuild, for a newly published version, or for recovering from a failed
run. Nothing over HTTP ever starts a computation.

```bash
POD=$(kubectl --context k3s -n market-lens get pods -o name | grep -v postgres | head -1)
kubectl --context k3s -n market-lens exec ${POD#pod/} -- \
  /app/market-lens signals compute --universe nordic-liquid-v1
```

---

## Read them

```bash
# What the current strategy says about one instrument, as of its latest session.
curl -s .../api/v1/instruments/<id>/signal | jq '{score, action, confidence, absence_reason}'

# Why it says it.
curl -s .../api/v1/instruments/<id>/signal | jq '.contributions[]
  | {factor, feature, feature_value, factor_score, weight, contribution}'

# The universe in the strategy's order for a session.
curl -s '.../api/v1/signals?as_of=2026-09-01' | jq '{scored, unscored,
  top: [.items[:5][] | {ticker, score, action}]}'
```

---

## Publish a new strategy version

A version is never edited. Changing a weight, a factor, a band or a liquidity rule means
publishing a new version by ordered migration, which supersedes the previous one and leaves its
signals readable and attributed to it.

```bash
# after the migration ships
market-lens signals compute --strategy momentum_trend --version 2
```

The previous version's signals stay exactly as they were. That is the point: a record of what was
said cannot be improved retroactively.

---

## Verify it

**Reproducible (SC-001)** — recompute and diff. Zero rows.

```sql
-- with the previous computation kept in a temporary table
SELECT count(*) FROM signals s JOIN previous_signals p
  USING (instrument_id, session_date, strategy_id)
WHERE s.score IS DISTINCT FROM p.score
   OR s.action IS DISTINCT FROM p.action
   OR s.confidence IS DISTINCT FROM p.confidence
   OR s.absence_reason IS DISTINCT FROM p.absence_reason
   OR s.contributions::text IS DISTINCT FROM p.contributions::text;
-- expect 0
```

**The reasons account for the score (SC-002)**

```sql
-- every scored signal's contributions, summed and divided by its divisor, equal its score
SELECT count(*) FROM (
  SELECT s.score, s.divisor,
         (SELECT sum((c->>'contribution')::numeric)
          FROM jsonb_array_elements(s.contributions) c
          WHERE c->>'contribution' IS NOT NULL) AS summed
  FROM signals s WHERE s.score IS NOT NULL
) t
WHERE round(t.summed / t.divisor, 12) IS DISTINCT FROM t.score;
-- expect 0
```

**No absence dressed as a view (SC-004)**

```sql
SELECT count(*) FROM signals
WHERE (score IS NULL) <> (absence_reason IS NOT NULL);
-- expect 0 — and the table's check constraint means it can only ever be 0

SELECT absence_reason, count(*) FROM signals
WHERE absence_reason IS NOT NULL GROUP BY absence_reason ORDER BY 2 DESC;
-- expect: every absence carries a reason a person can act on
```

**Every instrument, every session (FR-008a)**

```sql
SELECT count(*) FROM (
  SELECT b.instrument_id, b.session_date FROM daily_price_bars b
  EXCEPT
  SELECT s.instrument_id, s.session_date FROM signals s
  WHERE s.strategy_id = (SELECT id FROM strategies WHERE superseded_at IS NULL LIMIT 1)
) missing;
-- expect 0
```

**No lookahead (SC-005)** — every contribution's feature session is on or before the signal's.

```sql
SELECT count(*) FROM signals s, jsonb_array_elements(s.contributions) c
WHERE (c->>'feature_session')::date > s.session_date;
-- expect 0
```

---

## Recorded evidence

The first production computation, `v0.12.0`, 2026-09-02, on the k3s deployment.

```
run_id=bad5e559-8b6c-419e-bd8c-ee3a1a147bc8 status=succeeded instruments=100 signals=243005 failed=0
```

It took 89 seconds against a stated bound of 10 minutes, over sessions from 2016-08-31 to
2026-09-02, and wrote one run item per instrument and 1,940 `signals.changed.v1` events.

| Check | Expected | Result |
|---|---|---|
| Signals stored | — | 243,005 (222,853 scored, 20,152 absent) |
| Neither a view nor an absence | 0 | 0 |
| Both a view and an absence | 0 | 0 |
| A contribution read from a later session | 0 | 0 |
| An instrument-session with data and no signal | 0 | 0 |
| A scored signal its contributions do not account for | 0 | 0 |
| A signal naming no run | 0 | 0 |

Every scored signal records all seven contributions, available or not. The two absence reasons
that occur are the ones the design predicts:

| Reason | Count | Why |
|---|---|---|
| `insufficient_history` | 20,000 | Exactly 100 instruments × the version's 200-session minimum: the opening of every instrument's history, before the longest factor window can be reached. |
| `feature_unavailable` | 152 | Sessions where no factor had a usable value. |

The views the first version formed, across the whole history:

| Action | Count |
|---|---|
| HOLD | 65,788 |
| WATCH | 50,667 |
| REDUCE | 41,387 |
| BUY | 37,336 |
| SELL | 27,675 |

The most recent session, 2026-09-02, holds 84 signals rather than 100 — that is not a gap. Only
84 instruments have stored values for it yet, and the invariant is that every instrument-session
the engine computed has a signal, which holds at 0 missing.

---

## When something is wrong

| Symptom | Where to look |
|---|---|
| An instrument shows no signal at all | `strategy_run_items` for its last run — a failed instrument keeps its previous signals and records why |
| Every instrument is unscored for a session | The universe composite is probably undefined there (fewer than ten contributors), which removes every cross-sectional factor |
| A score changed without an import | `signals.run_id` names the run that wrote it; compare with `strategy_runs` and its trigger |
| Confidence looks high on a thin signal | Check `divisor` against the sum of all weights — coverage should have pulled it down; if it did not, that is a defect in the confidence calculation, not a judgement call |
| The ranking looks arbitrary | Ties resolve by feature value then instrument identifier; two runs producing different orders is a defect |

---

## What this feature does not do

- It does not size a position, apply a risk limit, or produce an order intent. Signals pass
  through a risk engine in a later milestone; here they stop at a stated view.
- It does not backtest. Whether the strategy's views were any good is Milestone 5, which reads
  these signals.
- It does not let anyone tune a parameter through the interface. A version is a reviewed
  migration, which is what keeps "what produced this signal" answerable.
- It does not claim the view is correct. Confidence measures agreement between factors, nothing
  more.
