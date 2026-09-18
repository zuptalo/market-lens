# Phase 0 Research: Paper Trading

## R-001 — Where a fill price comes from

**Decision**: the **open** of the first stored session *after* the session the order was placed in.

**Rationale**: it is the only price in the stored data that provably did not exist when the order
was placed. Feature 021 reached the same conclusion for the same reason and encoded it as a check
constraint (`execution_session > signal_session`); repeating the rule here keeps a backtest and a
paper run comparable, and repeating the *constraint* keeps a bug from producing a flattering result
that looks plausible.

**Alternatives rejected**: the same session's close — an order can be entered after the close is
known, which is the lookahead the backtester exists to avoid. The next session's close — no worse
in principle, but an order placed in the morning would then sit for a day and a half, and the open
is what somebody acting on a written-down intent would realistically have got.

## R-002 — When the account moves

**Decision**: a pass over pending orders after each market-data import completes, in process.

**Rationale**: a paper track record is only worth something if it accrues whether or not anybody
visits. Tying it to the import means it runs exactly when new bars exist and never otherwise, so
there is no schedule to keep in sync with the data. The product already runs the feature engine and
the strategy pass this way.

**Alternatives rejected**: deriving fills on read, as the risk limits do — a limit is a statement
about *now* and can be recomputed freely, but a fill is a statement about a moment, and re-deriving
it means a corrected bar silently rewrites the past. A manual button — a record with gaps where
nobody pressed it is not a record.

## R-003 — Where orders come from

**Decision**: promoted from an order intent, and from nothing else.

**Rationale**: feature 025 reversed the vision's authorship deliberately, after feature 021 measured
the strategy and found it lost to two of its three benchmarks over ten years. A paper account that
the strategy fed would restore exactly the arrangement that was rejected, in the place where a
convincing-looking equity curve does the most damage.

**Alternatives rejected**: strategy-driven paper orders — the honest way to measure a strategy
forward, and genuinely worth doing, but it is the product generating trades and needs its own
review. It is excluded by FR-024 rather than left unmentioned, because after this feature lands it
is one scheduler change away.

## R-004 — Whether cash is tracked

**Decision**: yes, and this is a deliberate divergence from feature 022.

**Rationale**: feature 022 does not track cash because a person's real deposits and withdrawals are
facts the product never observed, so any total return it stated would be invented. A paper account
has no such gap: it starts with a stated balance and the only thing that moves it is a fill this
product recorded. The return is therefore exactly computable, and FR-015 makes this the one place
in the product that reports one.

**Consequence worth stating**: it also makes `insufficient_cash` a real outcome. An account that
cannot run out of money measures nothing, because every order fills and position sizing stops
mattering.

## R-005 — A corrected bar under a recorded fill

**Decision**: record the bar's identity and observation time with the fill; report a later
divergence rather than re-filling.

**Rationale**: feature 016 re-observes bars and corrects them, and feature 017 keeps findings a
person must settle. A fill that silently moved when its bar was corrected would make the account's
history unreconcilable — the figure a person read last week would differ from the same figure this
week with nothing to explain it. Reporting the divergence keeps both facts: what was filled, and
that the underlying price has since changed.

**Alternative rejected**: re-filling at the corrected price. Tidier arithmetic, and it destroys the
property that makes a track record worth keeping.

## R-006 — One account, immutable terms

**Decision**: one paper account per person, opened explicitly, with starting cash, accounting
currency and cost rates fixed at opening.

**Rationale**: the value of the record is that it cannot be tuned after the fact. Being able to
reset it, open a second one, or change the starting balance turns a track record into a collection
of attempts, and the best attempt is the one that gets shown.

**Alternative rejected**: multiple named accounts. Genuinely useful for comparing approaches, and
the obvious way to make the result flattering. If it is wanted later it needs a specification that
argues for how comparisons stay honest.

## R-007 — Reusing feature 021's arithmetic

**Decision**: reuse `internal/decimal`, feature 021's cost model, and feature 022's FIFO fold.
Reimplement none of them.

**Rationale**: feature 022's property test found that spreading costs across shares and multiplying
back loses fractions, and the fix — lots carrying their remaining total cost — is structural. A
second implementation here would eventually disagree with the first about one trade, and the
disagreement would surface as a portfolio that does not reconcile.

## R-008 — Risk limits against a paper account

**Decision**: a person's stated limits (feature 023) are evaluated against the paper account with
the same evaluator, through `risk.EvaluateAgainst`.

**Rationale**: feature 025 already extracted that function so a hypothetical portfolio could be
measured by the code that measures the real one. A paper account is a portfolio; measuring it with
a second copy would let two screens disagree about whether the same concentration is a breach.

## R-009 — Proving nothing proposes an order

**Decision**: a test that gives the system a strategy, current signals, a scheduler pass and a
populated intent list, runs the pass, and asserts that no order exists that no person promoted.

**Rationale**: FR-001 is the requirement this feature most needs to still be true in a year, and
reading the code is not proof. Feature 025 has the same test shape for intents and it is the
cheapest guard in the codebase.
