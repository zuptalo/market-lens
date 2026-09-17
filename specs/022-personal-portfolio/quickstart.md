# Quickstart: Personal Portfolio and Holdings

How to record what you hold, what the product will and will not tell you about it, and how to check
it is not lying.

---

## What this is, and is not

It is a record of what **you entered**. Nothing is imported from a broker — this product has no
broker connection and none is planned — so every figure traces back to something you typed, and
anything you typed you can correct.

It offers no advice. It will show you what the product's own strategy currently thinks of something
you hold, with that strategy's caveat attached, because hiding it would be worse. It will never
suggest you act.

---

## Set your currency, once

```bash
curl -X PUT .../api/v1/portfolio -d '{"accounting_currency":"SEK"}'
```

Your holdings are converted into this. A Danish holding in a Swedish-accounting portfolio goes
DKK → EUR → SEK, because every stored rate has the euro as its base and storing a direct cross
would let two numbers disagree. Both legs come from the same session; a session missing either
reports the holding as unvalued rather than guessing.

---

## Record what you hold

```bash
curl -X POST .../api/v1/portfolio/trades \
  -H 'X-CSRF-Token: <token>' -H 'Content-Type: application/json' -d '{
  "instrument_id": "...", "direction": "buy",
  "quantity": "100", "price": "245.80", "costs": "39", "trade_date": "2026-03-16"
}'
```

Every write carries the CSRF token, the same as every other state change in this product. A
portfolio nobody else can read is still a portfolio somebody else's page could write to.

Three things it will refuse, each naming what to do about it:

- **An instrument this product does not carry.** The universe is 100 curated Nordic listings. A
  holding with no stored prices could never be valued or compared, so it is refused rather than
  accepted as a permanently broken row.
- **A sale larger than the position**, naming what you actually hold. There is no shorting here, so
  a negative position is a typo rather than a trade.
- **A date in the future.** A holding you do not have yet is not a holding.

---

## Read it

```bash
curl -s .../api/v1/portfolio | jq '{
  currency: .accounting_currency,
  holdings: [.holdings[] | {ticker, quantity, cost, value: .valuation.value, unrealised}],
  total
}'
```

Each holding states the session its price came from. A multi-market portfolio is valued at slightly
different sessions — Stockholm traded today, Copenhagen was shut — and saying so per holding is
better than hiding it inside one number.

**The total will not tell you your return.** It reports value, cost, unrealised and realised, and
then states why there is no return figure: the product does not know what you paid in, and a return
computed without that divides by a number it would have to invent. If that figure matters to you,
the fix is deposits and withdrawals, which is a deliberate future decision rather than something to
approximate now.

---

## Correct a mistake

```bash
curl -X PATCH .../api/v1/portfolio/trades/<id> -d '{"quantity":"120", ...}'
curl -X DELETE .../api/v1/portfolio/trades/<id>
```

A correction supersedes; it does not overwrite. The previous version stays readable, because
somebody checking last month's figure against a broker statement needs the version that produced
it. A withdrawn trade stops affecting every figure and remains visible as withdrawn.

---

## Check it is telling the truth

**Nobody can see anybody else's holdings (SC-001)**

```sql
-- Every row in both tables carries its owner, and the owner matches the portfolio's.
SELECT count(*) FROM portfolio_trades t JOIN portfolios p ON p.id = t.portfolio_id
WHERE t.user_id <> p.user_id;
-- expect 0 — and the composite foreign key means it can only ever be 0
```

**Every figure reconciles with the trades behind it (SC-002)**

Nothing derived is stored, so there is no second copy to disagree. Read the portfolio twice and
diff it; then read it again after correcting a trade and confirm the figures moved.

**No holding is valued at a price its instrument never quoted (SC-003)**

```sql
SELECT count(*) FROM portfolio_trades t
WHERE t.withdrawn_at IS NULL AND t.superseded_by IS NULL
  AND NOT EXISTS (SELECT 1 FROM daily_price_bars b WHERE b.instrument_id = t.instrument_id);
-- expect 0 — an instrument with no stored prices should never have been accepted
```

Every holding's `valuation.session` is a session that instrument actually traded, or the valuation
is absent with `no_price` or `no_rate`.

**Every realised figure says what basis produced it (SC-004)**

```bash
curl -s .../api/v1/portfolio | jq '.realised[] | {ticker, realised, cost_basis}'
# cost_basis is "fifo" on every row. A realised number without its basis is not checkable.
```

**History survives a correction (SC-005)**

```bash
curl -s '.../api/v1/portfolio/trades?include_withdrawn=true' | jq '.items[] | {id, status, supersedes}'
# a corrected trade appears twice: the current version, and the superseded one it replaced
```

---

## Reading it honestly

- **FIFO decides which shares you sold**, oldest first. It is what the Nordic tax authorities expect
  by default, which is why it was chosen — but this product computes no tax and makes no claim that
  any figure is suitable for a return.
- **Two purchases on the same day are consumed in the order you entered them**, and that order is
  shown on each trade. It has to be stored, because otherwise the basis would depend on something
  nobody can see.
- **A corporate action inside a holding period is your problem, not the product's.** Where a stored
  split or dividend falls inside a period you held something, the product says so and leaves your
  cost basis alone rather than adjusting it on your behalf.
- **A holding's comparison is value against cost over one window**, not a money-weighted return. A
  position added to over time has no single honest return figure without cash flows, and the product
  does not have them.

---

## What this does not do

- No broker connection, no orders, no order intents.
- No risk limits — nothing here rejects or modifies a trade for any reason but your own correction.
- No tax calculation and no tax report.
- No dividend or corporate-action adjustment to your cost basis.
- No advice.
