# Phase 0 Research: Personal Portfolio and Holdings

What had to be settled before planning, and what the codebase already answers.

---

## R-001: The ownership machinery already exists — this is its first domain use

**Finding.** Feature 004 built the whole boundary and it is already enforced:

- `authorization.ScopeUser` and `PrivateScopeFor(userID)` describe one person's private data, and
  `authorization.Require` decides access from the persisted principal.
- `client_events` carries `scope` and `subject_user_id` with a CHECK constraint pairing them, and
  the replay query filters `scope='user' AND subject_user_id=$1`. A deactivated account is refused
  replay even with a cursor it held while active.
- Identity already publishes user-scoped events — `session.created.v1`, `account.changed.v1`,
  `credential.changed.v1`.

**Decision.** Inherit all of it. This feature invents no new scoping concept; it is the first
*domain* record to be private, which is a different claim from the one the specification originally
made and has been corrected there.

**Consequence.** The isolation tests are about this feature's queries, not about the mechanism. The
mechanism is tested; what is untested is whether a portfolio query remembers to use it. Every read
and write path gets its own cross-user test for exactly that reason.

---

## R-002: FIFO is derived on read, not materialised

**Options.** Store lots as rows and consume them on each sale; or derive the whole FIFO walk from
the trades every time a figure is read.

**Decision.** Derive. A position, its cost and its realised result are a fold over that
instrument's trades in entry order, and FR-010 requires exactly that — figures derived from trades
rather than stored as independently editable numbers.

**Rationale.** Materialised lots are a second source of truth that a correction has to repair. The
correction story (US3) is a first-class requirement here, and every correction would have to unwind
and replay consumed lots. Deriving makes a correction a re-read rather than a repair. The scale
makes it free: SC-008 states a thousand trades across a hundred holdings, which is a fold over ten
trades per instrument.

**Rejected.** Caching the fold behind a materialised view. Same objection, and nothing measured
suggests it is needed.

---

## R-003: Entry order is stored, because FIFO depends on it

**Finding.** Two purchases dated the same day have no inherent order, and FIFO's answer differs
depending on which is consumed first.

**Decision.** Store a per-portfolio monotonic sequence assigned when a trade is recorded, and order
by `(trade_date, sequence)`. The sequence is shown wherever the order matters.

**Rationale.** Ordering by identifier would make the basis depend on a UUID, and ordering by
insertion timestamp would make it depend on something the interface never shows. A correction does
not change the sequence: the person is fixing what a trade said, not when they told us about it.

---

## R-004: Conversion goes through the euro, in one session

**Finding.** Every stored rate has the euro as its base (`fx_rates.base='EUR'`), because feature 021
chose the euro as its accounting currency precisely to avoid cross rates.

**Decision.** Converting an amount in currency A into accounting currency B: divide by the euro rate
of A, then multiply by the euro rate of B. Either leg is one when its currency is the euro. Both
legs must come from the same session; a session missing either produces a stated absence.

**Rationale.** Storing a direct A→B rate, or an inverse, would let two stored numbers disagree with
no way to say which is right — the rule feature 021 established and this must not fork. The cost is
one extra rounding step, which is why the intermediate is rounded to stored precision before the
second leg reads it, exactly as the backtest does.

---

## R-005: Valuation is per instrument, at its own latest stored session

**Options.** One portfolio-wide valuation session (the latest session any holding traded), or each
holding at its own latest stored close.

**Decision.** Each holding at its own latest stored close, with that session stated on the holding.

**Rationale.** A portfolio-wide session forces Helsinki holdings to be unvalued whenever Stockholm
traded more recently, which is the union-calendar problem feature 021 hit and solved the same way.
Stating the session per holding is what makes "this figure is two days old" visible rather than
hidden inside a total.

**Consequence.** A total sums holdings valued at slightly different sessions. That is stated on the
total rather than papered over, and it is the honest answer: the alternative is refusing to total a
multi-market portfolio at all.

---

## R-006: A correction supersedes; it does not overwrite

**Decision.** A recorded trade is immutable. A correction writes a new version that supersedes the
previous one, and a withdrawal marks it withdrawn. Derived figures read only the current,
non-withdrawn versions.

**Rationale.** FR-009 requires history not to be silently rewritten, and the reason is
reconciliation: somebody checking last month's figure against a broker statement needs the version
that produced it. This is the same discipline strategy versions and backtest configurations already
follow, and it makes a correction auditable without a separate audit table.

---

## R-007: Holding-period return, when a position was built from several purchases

**Finding.** FR-015 compares a holding's return with its benchmark's "between the holding's first
purchase and the valuation session". A position bought in three instalments has one first purchase
but three different cost points.

**Decision.** The holding's return is its current value against its **current cost** — what the
shares still held actually cost under FIFO — and the benchmark's return is measured from the first
purchase still contributing to that cost, not from the first purchase ever made.

**Rationale.** Measuring the benchmark from a purchase whose shares have already been sold would
compare a period the holding no longer represents. Naming the session the comparison starts from is
what makes it checkable, so it is stated on the comparison rather than implied.

**Accepted limitation.** This is not a money-weighted return and does not claim to be. A position
added to over time has no single honest return figure without cash flows, which the product does
not track (FR-024). The comparison is stated as "value against cost, over this window", which is
what it is.

---

## R-008: Instruments outside the universe

**Finding.** The product carries 100 curated Nordic listings. A person may well hold something else.

**Decision.** Refuse, naming the limitation plainly: this product tracks a curated universe and
does not carry that instrument. No placeholder instrument, no free-text holding.

**Rationale.** A holding with no stored prices can never be valued, compared or explained, so it
would be a permanently broken row that makes every total incomplete. Saying "we do not carry this"
is a better answer than accepting something the product cannot ever say anything about.

---

## R-009: Budget

**Scale.** SC-008 states a hundred holdings and a thousand trades. The fold is ten trades per
instrument; the valuation is one latest-close lookup per instrument; the comparison is two benchmark
point lookups per holding. Nothing here approaches the feature engine's or the backtest's scale, and
the budget is stated to catch an accidental per-trade query rather than to police milliseconds.
