# Phase 0 Research: Personal Risk Limits

What had to be settled before planning, and what the codebase already answers.

---

## R-001: Risk reads the portfolio's valuation; it does not re-derive one

**Finding.** `portfolio.Service.View` already produces everything a limit needs to be measured
against: each holding's quantity, cost, value in the accounting currency, the session that priced
it, and the portfolio total — with the total stated only when every holding could be valued.

**Decision.** The risk evaluator calls `View` and measures against what comes back. It performs no
valuation of its own and reads no price directly.

**Rationale.** A second valuation would eventually disagree with the first, and the disagreement
would surface as a portfolio screen and a risk screen reporting different percentages of the same
money. It also means every honesty rule feature 022 established — a holding that cannot be priced,
a rate that is missing, a total that says it is incomplete — applies to limits for free rather than
being reimplemented and subtly weakened.

**Consequence.** `View` is called per evaluation, which is a fold over the person's own trades plus
one price lookup per instrument. At the scale feature 022 measured (300 trades, 122 ms) that is not
worth optimising.

---

## R-002: `Holding` gains its sector and its market

**Finding.** A sector limit and a market limit need each holding's sector and listing exchange.
`portfolio.Holding` carries neither: it has the instrument, ticker, name and currency.

**Decision.** Extend `portfolio.Holding` with `Sector`, `SectorName` and `MIC`, read from the same
join that already fetches the ticker and name. The portfolio API's `Holding` schema gains the same
three fields.

**Rationale.** The alternative is a second query from the risk package mapping instruments to
sectors, which is the same data fetched twice and a second place to get the `unclassified` handling
wrong. The fields are useful on the portfolio screen in their own right — somebody looking at what
they hold reasonably wants to know what sector it is in — so this is not scope bought for the risk
feature alone.

**Compatibility.** Additive to a shipped contract: three new fields on a response object, no field
removed and none changed.

---

## R-003: Nothing about an evaluation is stored

**Decision (the owner's, 2026-09-17).** A limit's state is computed on every read. No breach row, no
evaluation history, no scheduled job noticing a change.

**Rationale.** It is the discipline feature 022 already applies to positions, cost and realised
results, and for the same reason: a stored derived figure is a second source of truth that every
change has to repair. Here the changes are frequent — a new stored price moves every percentage
overnight — so a stored evaluation would need a job watching prices, which is a moving part this
feature otherwise does not have.

**Accepted cost, stated in the specification.** The product cannot say when a breach began, and a
breach that resolves leaves no trace. Somebody who needs that has the trade record it would be
reconstructed from.

---

## R-004: Three states, and "within" is never the default

**Decision.** Every evaluation reports exactly one of `within`, `exceeded`, `unevaluable`. There is
no fourth state and no absent state, and `unevaluable` carries a reason.

**Rationale.** The failure this feature must not have is reporting compliance because something
could not be measured. Making the state an enumeration with no zero value, computed from an explicit
branch rather than from a boolean that defaults to false, is what makes that a compile-time shape
rather than a discipline. It is the same reasoning behind feature 015's constraint that a signal is
a view or a stated absence and never a HOLD standing in for missing data.

---

## R-005: An allocation limit needs a complete total; a count limit does not

**Finding.** Every share-of-portfolio limit divides by the portfolio's total value, and feature 022
states that total only when every holding could be valued. A limit on the *number* of holdings
divides by nothing.

**Decision.** When the total is incomplete, every allocation limit reports `unevaluable` naming the
holding that could not be priced and why; the holding-count limit still reports normally.

**Rationale.** This is FR-011 and FR-012 together, and the split falls out of the arithmetic rather
than being a judgement. Silencing every limit because one holding lost its price would be its own
kind of dishonesty — the person's holding count is perfectly well known.

---

## R-006: `unclassified` is a sector, and it is stated

**Finding.** Feature 014 made sector curated reference data with a NOT NULL column against a
vocabulary that includes `unclassified`, precisely so an instrument cannot enter the universe with
no classification state.

**Decision.** A sector evaluation treats `unclassified` as a sector like any other and names it. It
is not excluded from the denominator and not merged into anything.

**Rationale.** Excluding it would understate every other sector's share — the denominator would
shrink while the numerators stayed the same — and a sector limit that quietly ignored a tenth of a
portfolio is worse than no sector limit. Naming it also makes the gap visible to somebody who could
go and classify it.

---

## R-007: Market means the listing exchange, and there is no separate currency limit

**Decision.** A market limit is measured by the exchange an instrument is listed on, which is how
feature 021 already chooses which benchmark to compare against.

**Rationale.** For this universe market and currency coincide everywhere except Helsinki, which
trades in euro alongside the accounting currency of a euro portfolio. Offering both kinds would
invite a person to set both and then be unable to explain why the two figures differ by one
holding's worth.

---

## R-008: One limit of each kind, enforced by the schema

**Decision.** A unique constraint on `(user_id, kind)`.

**Rationale.** Two thresholds for the same thing is a contradiction, not a refinement, and the
question "which one applies" has no good answer. A person who wants to change their limit changes
it.

**Consequence.** Stating a limit is an upsert from the interface's point of view, which also makes
the write idempotent.

---

## R-009: Where a limit appears

**Decision.** Limits are stated and read on their own screen, and the portfolio screen shows a
summary line naming how many are exceeded and linking to it.

**Rationale.** The portfolio screen is already dense — totals, holdings, an entry form and the
history — and limits are a different question from "what do I hold". But a breach nobody sees is a
breach nobody acts on, so the portfolio screen has to say that one exists. Counting them is enough
to justify a look; repeating them there would put the same figures in two places with two chances to
disagree.

---

## R-010: Budget

**Scale.** One person, at most four limits, up to a hundred holdings. The evaluation is one `View`
call plus a group-by over its holdings. Nothing here approaches any existing budget, and the test
exists to catch a per-holding query rather than to police milliseconds.
