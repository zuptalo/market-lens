# Quickstart: Order Intents

## Writing one down

Open **Intents**, choose an instrument, say buy or sell, how many and at what price. That is the
whole entry. Nothing is sent anywhere — there is no broker connection in this product — and nothing
happens to your holdings.

What comes back is the consequence, computed now:

- the position it would leave you holding,
- what that would be worth in your accounting currency,
- its share of your portfolio, with the total it was divided by,
- and what each limit **you** set would then say: within, over, or not measured.

## Settling one

**Withdraw** when you decide against it. **I acted on this** when you went ahead. Acted on records
that you acted; it does not record a trade. What you actually paid is a fact only you can assert,
and you assert it in Portfolio, where a trade belongs.

A settled intent stops being evaluated and stops carrying a consequence.

## Checking it is not lying to you

- **The arithmetic travels with the figure.** Every share shows the total it was measured against,
  so you can divide it yourself.
- **Two intents in the same instrument each report the same resulting position.** They are measured
  against your portfolio as it stands, never against one another — combining them would need an
  order of application you never stated.
- **Selling more than you hold comes back negative, in words.** It is reported, not refused: an
  intent is a thought. Record that same sale as a trade and the portfolio refuses it, because that
  is a claim about the past.
- **Nothing tells you what to do.** No recommendation, no suggested size, no "you should". If you
  ever see one, it is a bug, and three tests exist to stop it.

## What it deliberately has no room for

No venue. No order type. No time in force. No destination. No expiry, broker reference or external
id. No limit or stop price. The absence is the safeguard, and it is asserted rather than reviewed —
against the table, against the encoded intent, and against the HTTP response — because a field that
exists will eventually be filled in and then sent.

---

## Recorded evidence

`v0.21.0` on k3s, 2026-09-18. Nothing was seeded; every figure below is the deployment's own state.

**The table arrived with the release and is empty**, which is what a feature nobody has used yet
should look like:

```text
migration: 28 at 2026-09-18 00:45:37+00
rows: 0
```

**Nothing in it could be transmitted.** Asked directly of the production schema:

```text
forbidden columns present: 0
  (venue, order_type, time_in_force, destination, expires_at,
   broker_reference, external_id, limit_price, stop_price)
```

**Every constraint refuses what it should.** Run against production inside a transaction that was
rolled back, using a real owner and a real instrument so nothing but the constraint under test could
be responsible:

```text
refused: short direction              -> order_intents_direction_check
refused: zero quantity                -> order_intents_quantity_check
refused: zero price                   -> order_intents_price_check
refused: settled with no settled_at   -> order_intents_check
refused: considering with a settled_at-> order_intents_check
refused: an owner who does not exist  -> order_intents_user_id_fkey
refused 6 of 6
rows_remaining: 0
```

The fourth and fifth are the same constraint read from both sides:
`(status = 'considering') = (settled_at IS NULL)`. A settled intent cannot lack the moment it was
settled, and an unsettled one cannot carry one.

**Every route requires a session**, including the settle path:

```text
GET   /api/v1/order-intents        -> 401
GET   /api/v1/order-intents/{id}   -> 401
POST  /api/v1/order-intents        -> 401
PATCH /api/v1/order-intents/{id}   -> 401
```

**What could not be checked from outside, and why.** This deployment answers 401 for every path,
including ones that do not exist and including the SPA's own routes, because authentication runs
ahead of routing. So the 401s above prove the routes are guarded, not that they exist — the passing
contract test and the diff are the evidence for that, as they were for feature 024.

**What to check when you next open it.** With holdings and at least one limit of your own: write
down an intent large enough to breach it and confirm the limit reads *Over* afterwards while your
actual portfolio is unchanged; write down two in the same instrument and confirm both report the
same resulting position; then mark one acted on and confirm Portfolio still shows exactly the trades
you entered yourself.
