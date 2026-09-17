# Quickstart: Personal Risk Limits

How to write down the rules you want to be held to, what the product will and will not say about
them, and how to check the arithmetic.

---

## What a limit is here, and what it is not

It is **your** rule. The product publishes no defaults, suggests no thresholds, and has no opinion
about whether a quarter in one company is too much. It stores what you wrote down and tells you
where you stand against it.

That is the whole line this feature is built on. *"You should hold no more than 25% in one
company"* is advice, and this product does not give advice. *"You said 25%, you are at 41%"* is your
own rule, applied.

---

## State a limit

Four kinds, all measured against what you actually hold:

```bash
# No more than a quarter of the portfolio in any one company
curl -X PUT .../api/v1/risk-limits/instrument_share \
  -H 'X-CSRF-Token: <token>' -d '{"threshold":"0.25"}'

# No more than half in any one sector
curl -X PUT .../api/v1/risk-limits/sector_share  -d '{"threshold":"0.50"}'

# No more than 70% in any one market
curl -X PUT .../api/v1/risk-limits/market_share  -d '{"threshold":"0.70"}'

# At most fifteen holdings
curl -X PUT .../api/v1/risk-limits/holding_count -d '{"threshold":"15"}'
```

One limit of each kind. Stating one that exists replaces its threshold — two thresholds for the same
thing is a contradiction, not a refinement.

A threshold that cannot mean anything is refused with what it can be: a share above 100%, a share of
zero, or a fractional number of holdings.

---

## Read where you stand

```bash
curl -s .../api/v1/risk-limits | jq '.limits[] | {kind, state, measured, threshold}'
```

```json
{ "kind": "instrument_share", "state": "exceeded", "measured": "0.412300000000", "threshold": "0.250000000000" }
{ "kind": "sector_share",     "state": "within",   "measured": "0.388000000000", "threshold": "0.500000000000" }
{ "kind": "holding_count",    "state": "within",   "measured": "7",              "threshold": "15" }
```

Every share comes with the total it was measured against and the full breakdown that produced it, so
the percentage can be checked rather than believed:

```bash
curl -s .../api/v1/risk-limits | jq '.limits[] | select(.kind=="sector_share") | {denominator, contributions}'
```

**Three states, and only three.** `within`, `exceeded`, `unevaluable`. A limit is never reported
within because something could not be measured — that failure is the one this feature exists to
avoid, and it is the one that shows least.

---

## What it will not tell you

It states the gap — the figure, the threshold, and the distance between them — and stops. It does
not say how much value is above the line, and it does not say what selling would bring you back
under. Naming an amount is one step from naming a trade, and a trade is the order-intent feature's
job, with the review that implies.

It also does not stop you. Recording a trade that puts you over a limit succeeds: a recorded trade
is a fact you asserted, and refusing to record reality would be a strange thing for a portfolio to
do. The breach is reported afterwards.

---

## Check it is telling the truth

**Nobody can see anybody else's limits (SC-001)**

```sql
SELECT count(*) FROM risk_limits WHERE user_id IS NULL;
-- expect 0 — and the column is NOT NULL, so it can only ever be 0
```

**One limit of each kind (R-008)**

```sql
SELECT user_id, kind, count(*) FROM risk_limits GROUP BY 1, 2 HAVING count(*) > 1;
-- expect no rows — and the unique constraint means there can be none
```

**No limit exists that nobody stated (SC-002)**

```sql
SELECT count(*) FROM risk_limits;
-- on a fresh installation: 0. The migration seeds nothing, because a default threshold is the
-- product telling you what is prudent.
```

**The arithmetic reproduces (SC-004)**

Take any share limit's response and add up the `contributions`: each `value` divided by
`denominator` is its `share`, the shares sum to 1, and the largest is the `measured` figure.

**A missing price does not become compliance (SC-003)**

```bash
# With one holding the product cannot price:
curl -s .../api/v1/risk-limits | jq '.limits[] | {kind, state, absence_reason}'
# every share limit: "unevaluable" / "portfolio_incomplete"
# holding_count:     "within" — counting needs no price
```

That split is the point. Silencing every limit because one holding lost its price would be its own
dishonesty; your holding count is perfectly well known.

---

## Reading it honestly

- **An empty portfolio is `unevaluable`, not `within`.** Nothing to measure is not compliance.
- **`unclassified` is a sector.** Feature 014 made sector curated reference data with an explicit
  unclassified value, and a sector limit that quietly dropped those holdings would understate every
  other sector's share.
- **There is no breach history.** State is computed on every read, so a breach that resolves leaves
  no trace and one that began in March looks like one that began today. The trade record is what a
  history would have been reconstructed from.
- **There is no drawdown limit**, and the reason is structural rather than an omission: feature 022
  values holdings only at their latest stored session and tracks no cash, so a portfolio value over
  time does not exist to measure against. Offering one computed from what exists would be inventing
  a series.

---

## What this does not do

- No default limits and no suggested thresholds.
- No statement of what would bring a breach back within.
- No blocking, rejecting or modifying a recorded trade.
- No notification — consented email and Web Push is its own feature with its own consent rules.
- No order, no order intent, and no effect on any stored backtest.
