# Phase 0 Research: Order Intents

What had to be settled before planning.

---

## R-001: Authorship decides whether this is advice

**Finding.** The vision's risk engine filters recommendations the product generated. Feature 021
then measured the only published strategy and found it lost to two of its three benchmarks.

**Decision.** The person authors every intent. The product evaluates.

**Rationale.** The difference between advice and a consequence analysis is who formed the proposal.
It is the same shape feature 023 used for limits: the product does not set them and does not advise
on them; it holds you to the ones you wrote. Generating trades from a method the product's own
evidence calls unpromising would contradict every other refusal in the codebase, in the one place
the contradiction is expensive.

**Consequence.** There is no code path that creates an intent, and FR-002 makes that testable rather
than intentional.

---

## R-002: The consequence is computed, the record is stored

**Decision.** An intent stores what was proposed and when. What it would do is computed on every
read from the portfolio as it stands now.

**Rationale.** The record answers "what was I thinking in March"; the consequence answers "what
would acting today do". Freezing the consequence at proposal time answers a question nobody is
asking any more — prices have moved, limits may have changed, and the position may be different.
This is feature 023's rule (nothing derived is stored) applied to a record that does persist.

---

## R-003: Evaluation reuses the risk evaluator rather than reimplementing it

**Finding.** Feature 023 already measures a portfolio against a person's stated limits, producing
three states and the contributions behind each figure.

**Decision.** An intent's consequence is computed by applying the proposed change to the portfolio's
holdings and running the *existing* evaluator over the result.

**Rationale.** A second implementation of "what share would this be" would eventually disagree with
the limits screen about the same portfolio. Reusing it also means the unevaluable rule — a holding
that cannot be priced makes a share limit unevaluable — arrives already correct.

**Consequence.** The risk package gains a way to evaluate a *hypothetical* portfolio, which the
limits screen calls with no change applied. One code path, two callers.

---

## R-004: An impossible intent is recorded; an impossible trade is not

**Decision.** An intent to sell more than is held is accepted and reported as producing a negative
position. Feature 022 refuses the same thing as a recorded trade.

**Rationale.** A recorded trade is a claim about what happened, and a claim that cannot be true is an
error worth refusing. An intent is a thought, and a notebook that refused to let somebody write down
a thought they were having would be a strange notebook. The consequence reports the negative
position plainly, which is more useful than a refusal.

---

## R-005: Intents are not combined

**Decision.** Each intent is evaluated against the portfolio as it stands. Two intents are never
evaluated together.

**Rationale.** Combining them needs an order of application nobody stated, and the product would be
reporting the consequence of a sequence the person never proposed. Saying so on the screen is the
honest alternative; leaving it unsaid would let somebody assume the figures compose.

---

## R-006: Nothing a broker could act on

**Decision.** No venue, no order type, no time in force, no destination, no expiry.

**Rationale.** The absence is the safeguard. An intent that carried an order type would be one field
away from being transmissible, and the next feature that touched it would have to decide not to
send it. FR-006 makes the absence testable by naming the fields that must not exist.

---

## R-007: Marking one acted on records nothing else

**Decision.** Acting on an intent sets its status and a date. It does not create a portfolio trade.

**Rationale.** What somebody actually paid is a fact only they can assert — feature 022's whole
position. An intent says they expected 245.80; the fill was something else. Creating a trade from
the expectation would put a number in the portfolio that nobody observed.

---

## R-008: Budget

One intent's evaluation is one portfolio read plus one pass of the existing limit evaluator over a
holdings list with one element changed. At the scale features 022 and 023 measured — 300 trades in
122 ms, four limits in 26 ms — there is nothing here to optimise, and a budget test would measure
two things already tested.
