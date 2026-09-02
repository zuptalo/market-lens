# Research: Deterministic Strategies and Signals

Phase 0 decisions for `specs/015-strategies-and-signals/spec.md`. Each records what was chosen,
why, and what was rejected.

---

## R-001: What a factor is, and how a score is assembled

**Decision**: A strategy version declares an ordered list of **factors**. Each factor names one
published feature definition, a transform from that feature's value to a **normalised score in
[-1, +1]**, and a weight. The instrument's score is the weighted sum of its factor scores,
divided by the sum of the weights of the factors that were *available*. Contributions are
recorded as `weight × factorScore` before that division, plus the divisor, so they add up to the
score exactly (FR-011).

**Rationale**: The requirement that contributions account for the score with no remainder is the
whole explanation mechanism, and it is far easier to guarantee by construction than to check
afterwards. A weighted mean over available factors also gives the absence rules somewhere sane to
land: a missing feature removes its factor from both the numerator and the denominator rather
than counting as zero, which would silently drag every score toward the middle.

**Alternatives considered**:

- *Raw feature values summed with weights* — rejected: a 250-session return and an RSI are not on
  the same scale, so weights would be doing two jobs at once (importance and unit conversion) and
  nobody could read them.
- *Rank-based scoring only* (score = position in the universe) — rejected as the primary
  mechanism: it destroys magnitude, so "everything is falling" and "everything is rising" produce
  identical rankings and identical actions. Cross-sectional comparison enters through the
  normalisation of specific factors instead (R-002).
- *Counting a missing feature as zero* — rejected: it is the "absence as a neutral value" error
  the whole product avoids, and it would make an instrument with no data look moderate.

---

## R-002: Cross-sectional versus per-instrument normalisation

**Decision**: Each factor declares which of the two it uses. **Cross-sectional** factors normalise
against the distribution of that feature across the universe for the same session, by percentile
rank. **Absolute** factors map a feature value to [-1, +1] by a stated bounded transform, without
reference to other instruments.

**Rationale**: "Momentum ranking" is inherently comparative — a 6% ninety-session return means
something different in a falling market than a rising one, and the universe composite already
exists for exactly this reason. But some factors are not comparative: a regime label and a
volatility penalty mean what they mean regardless of what everything else is doing. Forcing
either style on all factors would misrepresent one group.

This makes the computation ordered the way the feature engine's composite already is: the
universe's feature values for a session must be gathered before any instrument's cross-sectional
factor score exists. Instrument-level parallelism is safe within a session; across the
distribution boundary it is not.

**Alternatives considered**:

- *Z-scores instead of percentile ranks* — rejected: unbounded, and one extreme instrument
  distorts everything else's score. Percentile rank is bounded by construction, which the [-1, +1]
  contract needs.
- *All factors absolute* — rejected: it cannot express "strongest of the universe", which is what
  a ranking strategy is for.
- *All factors cross-sectional* — rejected: percentile-ranking a regime label is meaningless.

**Ties**: equal feature values must receive equal percentile ranks, and the ordering used to
resolve them must be total and stable (feature value, then instrument identifier), so that the
same input can never produce two different rankings (an edge case the spec names).

---

## R-003: How confidence is computed

**Decision**: Confidence is the **share of available contribution weight that agrees in sign with
the score**, scaled by how much of the strategy's total weight was available:

```
agreement = |Σ contributions agreeing with the score's sign| ÷ Σ |contributions|
coverage  = Σ weights of available factors ÷ Σ weights of all factors
confidence = agreement × coverage
```

**Rationale**: FR-013 fixes the meaning as agreement between factors; FR-013a requires that a
signal resting on one available factor cannot report the confidence of one where seven agree.
Agreement alone would give a lone factor a perfect score — it trivially agrees with itself — so
the coverage term is what makes FR-013a true by construction rather than by a special case.

Both terms are derived from contributions already recorded, so confidence adds no new input and
cannot disagree with the explanation shown beside it.

**Alternatives considered**:

- *Agreement without coverage* — rejected: violates FR-013a.
- *Counting factors rather than weight* — rejected: it would let three small factors outvote one
  large one in the confidence number while the score says the opposite.
- *Any probability-like calibration* — rejected, and forbidden by FR-013: nothing in this feature
  provides evidence that a view is correct, and a number between 0 and 1 next to the word
  confidence will be read as one unless the surface says otherwise.

---

## R-004: Where actions come from

**Decision**: The strategy version declares ordered **action bands** over the score — a lower
bound, an upper bound and an action. Bands are contiguous and cover [-1, +1] exactly, and a score
on a boundary belongs to the upper band. The version's bands are stored with it, so a superseded
version's signals remain explicable.

**Rationale**: The spec requires one score to map to exactly one action, always (FR-010), and
names "a score exactly on a threshold" as an edge case. Declaring the bands as data with a stated
tie rule settles it once, rather than leaving it to whichever comparison an implementation
happens to use.

**Alternatives considered**:

- *Thresholds in code* — rejected: a strategy version must be reproducible from its stored
  definition, and a threshold in a binary is not part of the version.
- *Bands over percentile rank rather than score* — rejected: it would force a fixed proportion of
  the universe into BUY every session regardless of whether anything looked good, which is a claim
  the data does not support.

---

## R-005: Storage shape for contributions

**Decision**: One row per signal, carrying its contributions as a stored ordered structure on the
row rather than as a separate table of contribution rows.

**Rationale**: Contributions are always read with their signal and never queried across signals —
the surfaces ask "why this score", never "every instrument where momentum contributed more than
0.3". One row per signal keeps the point-in-time lookup (FR-008a) a single read, and avoids a
second table roughly seven times larger than the signal table itself: about 1.8 million rows per
strategy version against 250,000.

The trade is that contributions cannot be filtered in the database. That is acceptable while
nothing needs to; the day something does, it is a derived index over data that is already stored,
not a migration of the truth.

**Alternatives considered**:

- *A `signal_contributions` table* — rejected for now on the volume argument above. Revisit if a
  surface ever needs to query across contributions.
- *Recomputing contributions on read* — rejected outright: it would make the explanation a
  present-tense derivation rather than a record of what was said, and FR-016 forbids a stored
  signal changing except by attributable recomputation.

---

## R-006: What triggers a signal computation

**Decision**: A signal run follows a **feature run**, in the same way a feature run follows an
import: the feature service, having finished successfully, asks the strategy service to compute
signals for the sessions whose feature values it wrote. A failure to compute signals is logged
and swallowed — it never marks the feature run failed. An owner can also run it from the command
line for the full history, one strategy version, or what a given import changed.

**Rationale**: This is the shape feature 013 already established for imports and it is proven in
production. It also gives the right answer to the failure question: features are the more
valuable artefact, and a strategy defect must not make a good feature pass look broken.

**Alternatives considered**:

- *A scheduler on its own timer* — rejected: it would compute signals from features that may be
  mid-recomputation, and the ordering guarantee is the whole point.
- *Computing signals lazily on read* — rejected: FR-021 forbids a read triggering a computation,
  and a signal must be a record, not a rendering.

---

## R-007: Incremental scope

**Decision**: When feature values change for instrument *I* at sessions *[S, T]*, signals must be
recomputed for **every instrument in the universe over [S, T]**, not only for *I*.

**Rationale**: Cross-sectional factors (R-002) make an instrument's score depend on the
distribution across the universe for that session. Changing one instrument's momentum changes
every other instrument's percentile rank for that session. Recomputing only the touched
instrument would leave the rest quietly wrong — the same reasoning that makes the feature engine
recompute every member's relative strength when the composite moves.

The session range is therefore the unit of incremental work, and it is bounded by what the
feature pass reported changing, not by a window.

**Alternatives considered**:

- *Recompute only the touched instrument* — rejected as wrong for cross-sectional factors.
- *Recompute the whole history on any change* — rejected as needlessly expensive; the affected
  sessions are known.

---

## R-008: Budgets

**Decision**: A full computation over the fixture universe stays inside a budget scaled from
production the way feature 013's is: the stated production bound is **10 minutes** for a full
signal history and **30 seconds** for an incremental pass, and the fixture asserts the plain
linear scaling of those, not the quiet-machine measurement.

**Rationale**: Feature 013 learned this the hard way — `go test ./...` runs the database-heavy
packages concurrently, so a budget pinned to a quiet machine fails on contention rather than on a
defect. The signal computation reads ~7 features per instrument-session and writes one row, so it
is a fraction of the engine's work per session; the budget exists to catch an algorithm that
stops being linear, not to measure hardware.

---

## R-009: Where signals are read

**Decision**: Two surfaces. The instrument view gains its signal with the full contribution
breakdown. A new ranked view lists the universe in score order for a session, stating the strategy
version and separating scored from unscored instruments.

**Rationale**: US1 and US3 respectively. The Markets listing is deliberately left alone: it is a
market view, and hanging a strategy's opinion on every row would make the product's own view
inseparable from the data, which is the distinction this whole architecture keeps.

**Alternatives considered**:

- *A score column on the Markets table* — rejected for the reason above, and because feature 014
  has just finished making that screen a research tool rather than a mixture.
- *Signals only on the instrument page* — rejected: US3 exists because one instrument at a time
  cannot answer "what does it think".
