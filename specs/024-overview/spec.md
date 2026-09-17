# Feature Specification: An Overview That Says What Needs You

**Feature Branch**: `024-overview`

**Created**: 2026-09-17

**Status**: planned
<!-- Market Lens spec lifecycle: planned → in-progress → in-review → shipped. -->

**Input**: User description: "Replace the foundation-stage Overview stub with a screen that answers
the two questions a person actually has when they open this: has anything changed, and does anything
need me."

## User Scenarios & Testing *(mandatory)*

The Overview is a thirty-one line stub from the foundation stage, and it is now wrong. It tells a
signed-in owner that "market data, signals, backtesting, portfolios, and paper trading will be
implemented from future specifications". All but the last shipped, several of them this week. A
landing page that lies about the product's own state is worse than no landing page.

The product vision planned an Overview of portfolio value, daily and total change, cash and invested
amounts, drawdown, allocation and performance. Four of those cannot be reported honestly any more,
and not by oversight: feature 022 deliberately tracks no cash, so there is no denominator for a
return and no invested figure; feature 023 could offer no drawdown limit for the same reason, since
holdings are valued only at their latest stored session and no equity history exists. An Overview
built to that list would have to invent numbers, which is the one thing this product does not do.

What nothing currently answers is the pair of questions somebody actually has when they open this
once a day or once a week: **has anything changed, and does anything need me.** The second matters
most, because this product has deliberately accumulated decisions only a person can make — findings
it refuses to settle, limits it refuses to advise on — and scattered them across three screens.

### User Story 1 - A person sees what is waiting for them (Priority: P1)

Somebody opens the product after a few days. Findings the nightly pass cannot settle are on the
operations screen, limits they are outside are on the limits screen, and a holding that stopped
being priceable is on the portfolio screen. Nothing tells them any of it exists, so the honest
answer to "is anything wrong" is: go and look in three places.

The Overview says what is waiting, counts it, and links to the screen that owns it.

**Why this priority**: It is the reason to have the screen at all. Everything else on it is
interesting; this part is the part that is acted on.

**Independent test**: With a finding awaiting a decision and a limit exceeded, open the Overview and
confirm both are named with their counts and both link to the screen that owns them.

**Acceptance Scenarios**:

1. **Given** findings awaiting a decision, **When** the Overview is read, **Then** it names how many
   and links to where they are settled.
2. **Given** limits the person is outside, **When** the Overview is read, **Then** it names how many
   and links to where they are shown — without repeating the figures.
3. **Given** nothing waiting, **When** the Overview is read, **Then** it says so plainly. "Nothing
   needs you" is the most useful thing a daily check-in can report, and it is not an empty state to
   apologise for.
4. **Given** an item that needs a person, **When** it is shown, **Then** the product does not say
   what to do about it — the screen that owns it does not either, and the Overview may not be the
   place that starts.
5. **Given** a person who holds nothing and has stated no limits, **When** the Overview is read,
   **Then** it does not manufacture work by reporting the absence of holdings as something needing
   attention.

---

### User Story 2 - A person sees what changed (Priority: P1)

A portfolio's value moves overnight because a price arrived. A signal changes because a bar was
restated three weeks ago and everything derived from it moved. Neither is visible anywhere.

**Why this priority**: It ships with User Story 1 because a screen that only ever says "nothing
needs you" gives somebody no reason to open it, and then they do not see the day it says otherwise.

**Independent test**: After a nightly pass that corrected sessions, open the Overview and confirm it
names when the last import ran, how many sessions it corrected, and when the derived values were
last recomputed.

**Acceptance Scenarios**:

1. **Given** a completed import, **When** the Overview is read, **Then** it says when it ran and
   whether it succeeded.
2. **Given** an import that corrected stored sessions, **When** the Overview is read, **Then** it
   says how many — a correction means every value derived from those sessions moved, which is the
   one import statistic worth surfacing above the others.
3. **Given** completed feature and strategy computations, **When** the Overview is read, **Then** it
   says when each last ran, so a person can see whether what they are looking at is current.
4. **Given** a run that failed or ended partial, **When** the Overview is read, **Then** it is
   reported as something waiting rather than buried among the things that merely happened.

---

### User Story 3 - A person sees what the product could not tell them (Priority: P2)

This product knows a great deal about what it does not know: holdings it could not value, limits it
could not evaluate, comparisons it could not make. Every one of those is stated honestly on its own
screen, and nowhere are they collected.

**Why this priority**: Scattered absences are individually honest and collectively invisible. A
person can read three screens that each say "this one figure is missing" without ever forming the
thought that their picture has holes in it.

**Independent test**: With one holding that cannot be priced, open the Overview and confirm it says
the portfolio total is incomplete and why, linking to the holding.

**Acceptance Scenarios**:

1. **Given** a holding that could not be valued, **When** the Overview is read, **Then** it says the
   portfolio total is incomplete and how many holdings are affected.
2. **Given** a limit that could not be evaluated, **When** the Overview is read, **Then** it says so
   with the reason, and does not count it as satisfied.
3. **Given** nothing missing, **When** the Overview is read, **Then** the section is absent rather
   than present and empty — a heading that always says "nothing missing" trains a reader to skip it.

---

### User Story 4 - The screen states nothing it cannot source (Priority: P2)

The Overview is the screen most likely to grow a figure nobody can check, because a dashboard is
where people expect a summary and a summary is where duplicated numbers hide.

**Why this priority**: Every figure on this screen is a second copy of something another screen
owns. The constraint that keeps it honest has to be stated before it is built, not discovered after
somebody adds a portfolio value to it.

**Independent test**: Read the Overview and confirm it reports no money, no percentage, and no
figure that is not a count or a date.

**Acceptance Scenarios**:

1. **Given** any item on the Overview, **When** it is read, **Then** it is a count, a date, or a
   status — never a value, a percentage, or a return.
2. **Given** any item, **When** a reader wants the detail, **Then** a link takes them to the screen
   that owns it, which is the only place the figures appear.
3. **Given** a person who is not the owner, **When** they read the Overview, **Then** they see their
   own private items and the shared operational ones, and nothing belonging to anybody else.
4. **Given** any surface on the screen, **When** it is read, **Then** it offers no advice and
   suggests no action beyond going to look at something.

---

### Edge Cases

- **A brand-new installation with no data at all.** Says what is not there yet — no imports, no
  holdings — without dressing it as a problem, because it is not one.
- **A screen whose data cannot be loaded.** That section says it could not be read; it does not
  report zero, which would be indistinguishable from nothing waiting. This matters more here than
  anywhere else in the product, because the whole value of the screen is the claim "nothing needs
  you" being trustworthy.
- **A person with no portfolio and no limits.** The private sections are absent rather than empty.
- **Every section empty at once.** The page is short and says so. It does not pad itself.
- **A nightly run still in progress.** Reported as running, not as failed and not as absent.

## Requirements *(mandatory)*

### Functional Requirements

**What it says**

- **FR-001**: The Overview MUST report what is waiting for a person: findings awaiting a decision,
  limits exceeded, and runs that failed or ended partial.
- **FR-002**: The Overview MUST report what changed: when the last import ran, how many sessions it
  corrected, and when features and signals were last computed.
- **FR-003**: The Overview MUST report what the product could not determine: holdings that could not
  be valued, and limits that could not be evaluated.
- **FR-004**: When nothing is waiting, the Overview MUST say so plainly rather than showing an empty
  region.
- **FR-005**: A section with nothing in it MUST be absent rather than present and empty.

**What it may not say**

- **FR-006**: The Overview MUST NOT report a monetary value, a percentage, or a return. Every figure
  on it is a count, a date, or a status.
- **FR-007**: Every item MUST link to the screen that owns it, and that screen MUST remain the only
  place its figures appear.
- **FR-008**: The Overview MUST NOT offer advice or suggest an action beyond looking at something.
- **FR-009**: The Overview MUST NOT claim the product's state is other than it is. The stub it
  replaces asserted that shipped features were unimplemented, which is the specific defect this
  feature exists to remove.

**Honesty about its own reads**

- **FR-010**: A section whose data could not be read MUST say so, and MUST NOT report zero. Zero and
  unknown are different answers, and on this screen the difference is the whole point.
- **FR-011**: Private items MUST be scoped to the person reading, using the boundaries features 022
  and 023 established. Shared operational items remain visible to every authenticated user.

**Out of scope, stated as requirements so they are testable**

- **FR-012**: The feature MUST NOT introduce a stored table, a migration, or a new event type.
- **FR-013**: The feature MUST NOT introduce a new API operation. Every figure it shows already has
  an authorized read.
- **FR-014**: The feature MUST NOT compute anything itself. It composes what other screens own.

### Test-First Proof *(mandatory)*

- **Initial failing test**: `TestTheOverviewSaysWhatIsWaiting` — with a finding awaiting a decision
  and a limit exceeded, the Overview names both with their counts and links to the screens that own
  them.
- **Expected red reason**: The Overview renders the foundation-stage stub, so neither appears. A
  value failure on rendered content, not a compilation or setup failure.
- **Green evidence**: The Overview's own tests, plus every existing suite unchanged — this feature
  adds no backend and must alter none.
- **Database migration proof**: N/A, and asserted rather than assumed: FR-012 requires that no
  migration is added, and the migration count test would fail if one were.

### Responsive UI Behavior *(mandatory for user-facing features; otherwise state N/A)*

- **Mobile (320-767 CSS px)**: Sections stack, each item a row with its count, its sentence and its
  link, without horizontal page scrolling. The 360x800 scenario asserts every waiting item and its
  link are reachable.
- **Tablet (768-1023 CSS px)**: The 768x1024 scenario asserts the same content at the shared width.
- **Desktop (1024+ CSS px)**: The 1440x900 scenario asserts the same; the sections may sit side by
  side but nothing is hidden behind a hover or a tab.
- **Input and accessibility**: Each section is a landmark with a heading a screen reader can
  navigate by. Counts are text, not badges that convey meaning by colour. Every link states where it
  goes. At the 320-pixel floor nothing clips.

### Live Update Behavior *(mandatory for client-visible data; otherwise state N/A)*

- **Snapshot and events**: No new event type. The Overview subscribes to the events its sources
  already publish — import, feature, signal, finding, portfolio and limit changes — and re-reads on
  any of them, because every item it shows is derived from something one of those already reports.
- **Reliability**: Ordering, resumption, duplicate-safe consumption and bounded coalescing are the
  existing client's, unchanged.
- **Test evidence**: A reader watching sees a new finding arrive without reloading; a second person
  receives nothing about another person's private items.

### Identity, Ownership, and Permissions *(mandatory for user/account data; otherwise state N/A)*

- **Ownership and authorization**: The Overview composes private reads (portfolio, limits) and
  shared reads (imports, runs, findings). It introduces no record and no new boundary; each read
  enforces its own, which is exactly why composing existing reads is safer than adding an
  aggregating endpoint that would have to re-derive the same distinctions.
- **Security evidence**: Tests prove one person's Overview shows nothing of another's private items.

### Key Entities *(include if feature involves data)*

None. The feature stores nothing.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Every item shown is a count, a date or a status; no monetary value, percentage or
  return appears anywhere on the screen.
- **SC-002**: Every item links to the screen that owns it.
- **SC-003**: When nothing is waiting, the screen says so in one sentence a reader can act on.
- **SC-004**: A section whose source could not be read says so and does not report zero.
- **SC-005**: No person sees any part of another person's private items.
- **SC-006**: The screen makes no claim about the product's state that is not true of the running
  deployment.
- **SC-007**: The feature adds no migration, no table, no event type and no API operation.
- **SC-008**: A person can tell whether anything needs them within one screen and without scrolling
  on a 1440x900 display.

## Assumptions

- **Two questions, not a summary.** The vision's Overview was a digest of portfolio figures. Half of
  that list is now knowingly unreportable, and the other half is better on the screens that own it.
  "What changed and what needs you" is what is left that nothing else answers, and it is what a
  person opening this weekly actually wants.
- **Counts and dates, never values.** Every number on a dashboard is a second copy. This codebase
  has refused second copies at every turn — no stored derived figures, one implementation of what a
  holding is worth, limits linking to the portfolio rather than restating it — and an Overview is
  where that discipline is most likely to be quietly abandoned.
- **No new endpoint, and that is a design decision rather than laziness.** Every figure already has
  an authorized read that enforces its own boundary. An aggregating endpoint would re-derive those
  distinctions in a second place, and the first bug in it would be a private figure in a shared
  response.
- **The correction count is the import statistic worth showing.** Feature 016 made it a first-class
  number precisely because a correction means every derived value moved underneath. Sessions stored
  is noise by comparison.
- **"Nothing needs you" is a feature.** It is the answer most days, it is what makes the screen
  worth opening, and it is only worth anything if the screen is trusted — which is why FR-010 makes
  an unreadable section say so rather than report zero.
