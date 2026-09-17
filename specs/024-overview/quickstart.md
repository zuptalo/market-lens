# Quickstart: An Overview That Says What Needs You

What the screen says, what it refuses to say, and how to check it is honest.

---

## What it is for

Two questions, asked by somebody who opens this once a day or once a week:

**Does anything need me?** This product has deliberately accumulated decisions only a person can
make — findings the nightly pass refuses to settle, limits it refuses to advise on — and scattered
them across three screens. This is the one place that says they exist.

**Has anything changed?** A price arrived and every value moved. A bar was restated three weeks ago
and everything derived from it moved with it. Neither was visible anywhere.

---

## What it says

**Waiting for you** — findings awaiting a decision, limits you are outside, runs that ended partial
or failed. Each counted, each linking to the screen that settles it.

**What changed** — when market data last arrived and **how many stored sessions it corrected**, when
the statistics were last computed, when signals were last computed.

**What this could not tell you** — holdings that could not be valued, limits that could not be
evaluated. Absent entirely when nothing is missing.

---

## What it refuses to say

**No value, no percentage, no return.** Every figure on a dashboard is a second copy of something
another screen owns, and this is where that discipline is most easily abandoned. Counts and dates
only; the figures live where they are derived.

**Nothing about what to do.** The screens that own these items do not tell anybody what to do about
them, and the Overview is not the place that starts.

**Never "nothing needs you" while something is unknown.** A source that failed to load says so. The
whole value of this screen is that all-clear being trustworthy, and a failed read rendering as zero
fails in the direction that makes somebody stop looking.

---

## Check it is telling the truth

**It states no values**

```bash
# Open /, then:
#   no % anywhere in the main region
#   no SEK, EUR, DKK or NOK
#   every number is a count or a date
```

Asserted by `TestTheOverviewNeverStatesAValue` in `src/views/DashboardView.test.ts` and again in the
end-to-end suite at every viewport, because this is the constraint most likely to erode.

**A failed source is not an all-clear**

Block one of its reads and confirm the screen says that source could not be read, and that the
"nothing needs you" message does not appear.

**Every item goes somewhere**

Each links to `/operations`, `/risk`, `/portfolio` or `/signals` — and to nowhere else. The test
enumerates the destinations rather than counting them, so a link to a route that does not exist
fails.

**It adds nothing to the backend**

```bash
# The migration count is pinned by an existing test, so a migration would fail it.
grep -c "0027_risk_limits.sql" server/internal/db/migrate_test.go
# And the contract test reconciles routes against the reviewed contracts: no new operation appears.
```

The feature composes six reads that already existed. If a Go file changed, it grew an endpoint it
was told not to have.

---

## Reading it honestly

- **"Nothing needs you" is the answer most days**, and that is the point. A screen that only ever
  reported problems would be one nobody opens, and then the day it has one they do not see it.
- **The correction count is the import statistic shown**, not sessions stored. A correction means
  every value derived from those sessions moved underneath; sessions stored is nearly always the
  same number.
- **This screen departs from the product vision, deliberately.** The vision planned a digest of
  portfolio value, change, cash, drawdown and performance. Four of those cannot be reported honestly
  now — feature 022 tracks no cash and keeps no equity history — and the rest are better where they
  are derived. The departure is recorded in the specification's checklist.
