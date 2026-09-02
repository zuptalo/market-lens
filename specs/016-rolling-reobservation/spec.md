# Feature Specification: Rolling Re-observation of Recent Sessions

**Feature Branch**: `016-rolling-reobservation`

**Created**: 2026-09-02

**Status**: planned
<!-- Market Lens spec lifecycle: planned → in-progress → in-review → shipped. -->

**Input**: User description: "Rolling re-observation so a restated bar is noticed. The nightly
import asks the provider for exactly one session. Everything downstream is built to handle a
corrected bar and all of it is tested. Nothing ever asks for it."

## User Scenarios & Testing *(mandatory)*

The product already knows how to be told that a stored fact was wrong. It archives the previous
bar, replaces it, recomputes exactly the features that read it, and rescores the strategies that
read those. Every step is specified and tested.

None of it ever runs, because nothing asks the source whether it changed its mind.

### User Story 1 - A restated close is corrected without anyone asking (Priority: P1)

A provider publishes a close, and a day later restates it — a late correction, a re-run of their
own pipeline, an adjustment applied after the fact. Today the product never asks again, so its
copy of that session stays wrong permanently, and every statistic and every strategy view derived
from that session stays wrong with it: consistently, reproducibly, and with no symptom anybody
could notice.

After this feature, the routine daily pass re-asks the source about the handful of most recent
sessions as well as the one that just closed. A restatement inside that window is noticed on the
next run and flows through the correction path that already exists.

**Why this priority**: It is the whole feature. Without it, the revision machinery built by
feature 002 is unreachable code, and "the stored history matches the source" is a claim the
product cannot make.

**Independent Test**: Restate one instrument's close three sessions back in the source, let the
scheduled daily pass run, and confirm the stored bar now matches the source, the previous values
are archived as a revision, and the features and signals for that session were recomputed.

**Acceptance Scenarios**:

1. **Given** a stored session inside the observation window whose source values have since
   changed, **When** the routine daily pass runs, **Then** the stored bar is replaced with the
   source values, the previous values are archived as a traceable revision, and no duplicate
   instrument-session record exists.
2. **Given** a restated session, **When** the correction is stored, **Then** every stored feature
   value that reads that session is recomputed, and every strategy signal for the affected
   sessions is rescored — for the whole universe, not only the corrected instrument.
3. **Given** a session inside the window whose source values are unchanged, **When** the routine
   daily pass runs, **Then** nothing is written for it, no revision is archived, and no
   recomputation is triggered.
4. **Given** an instrument first listed fewer sessions ago than the window reaches, **When** the
   pass runs, **Then** the window starts at that instrument's first stored session rather than
   requesting sessions that never existed.
5. **Given** an exchange that was closed for several consecutive days, **When** the window is
   computed, **Then** it counts trading sessions rather than calendar days, so the window reaches
   the same number of sessions back on every exchange regardless of their differing holidays.

---

### User Story 2 - An operator can see that history was corrected (Priority: P2)

A run that corrects a session and a run that merely adds one look identical today. The second is
routine. The first means every derived value for that session changed underneath, which is the
kind of thing somebody should be able to notice without querying the database.

**Why this priority**: A correction that nothing reports is only marginally better than a
correction that never happens — it becomes visible only when somebody already suspects it.

**Independent Test**: Cause a restatement, open the operations screen, and confirm the run states
how many sessions it corrected, distinctly from how many it processed and accepted.

**Acceptance Scenarios**:

1. **Given** a routine daily pass that restated some sessions, **When** an operator opens the
   operational screen, **Then** the run states how many sessions were corrected, separately from
   the counts it already reports.
2. **Given** a routine daily pass in which nothing was restated, **When** an operator opens the
   operational screen, **Then** the run does not imply a correction occurred.
3. **Given** a run that corrected sessions, **When** it is read, **Then** the count of corrected
   sessions is never larger than the count of sessions processed.

---

### User Story 3 - The routine pass stays cheap when nothing changed (Priority: P3)

Almost every night, nothing has been restated. The widened window must cost effectively nothing
on those nights, or the deployment pays every day for an event that happens rarely — and the
first response to that cost would be to narrow the window until the feature stops working.

**Why this priority**: It is what makes the widened window safe to leave on permanently, which is
the only way a correction gets noticed without somebody deciding to look.

**Independent Test**: Run the routine pass twice with an unchanged source and confirm the second
run requests no more of the provider than the first did, writes nothing, and triggers no
recomputation.

**Acceptance Scenarios**:

1. **Given** an unchanged source, **When** the routine daily pass runs with the widened window,
   **Then** it makes no more requests of the provider than a single-session pass would.
2. **Given** an unchanged source, **When** the routine daily pass completes, **Then** no feature
   computation and no strategy computation is triggered by it.
3. **Given** the widened window, **When** the routine pass runs, **Then** it completes within the
   time budget the single-session pass already meets.

---

### Edge Cases

- **A restatement older than the window.** It is not caught, and the specification says so
  plainly rather than implying completeness. The remedy is the explicit backfill that already
  exists, and the operator has to decide to run it. Widening the window indefinitely would make
  the nightly pass re-ask for a decade every night to catch an event that is rare and, past a
  few days, effectively never happens.
- **A long outage.** A first run after the product has been down for a week does not widen its
  window to cover the gap. Recovering missed history is an explicit backfill, an operator
  decision about scope, consistent with the rule the product already follows: automatic when data
  arrives, deliberate when history is rebuilt.
- **The same session restated twice on different days.** Each correction archives its own
  revision, numbered in sequence, and the current bar is the most recent source values.
- **A restatement arriving while a computation is running.** The existing per-instrument scope
  and its lock decide the order; the losing writer waits and then recomputes from what committed.
- **A provider returning fewer sessions than the window asked for.** Unchanged from today: an
  absent session is interpreted against the exchange calendar as an expected closure or a
  potential gap, and neither is a restatement.
- **A re-observed bar identical to the stored one.** Nothing is written, nothing is archived,
  nothing is counted, and nothing downstream is triggered.
- **An instrument that left the universe** inside the window. It is not re-observed; the window
  applies to the instruments the routine pass already targets.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The routine daily pass MUST request a trailing window of recent sessions from the
  source for each active instrument, rather than only the session that most recently closed.
- **FR-002**: The window MUST be counted in stored trading sessions for the instrument's own
  exchange, not in calendar days, so that differing holiday calendars do not make the window
  reach different distances into different exchanges' history.
- **FR-003**: The window MUST span five trading sessions, ending with the session that most
  recently closed.
- **FR-004**: When an instrument has fewer stored sessions than the window spans, the window MUST
  begin at that instrument's first stored session.
- **FR-005**: A re-observed session whose source values are unchanged MUST NOT be written,
  archived, counted as a correction, or cause any recomputation.
- **FR-006**: A re-observed session whose source values differ MUST be corrected through the
  existing revision path: the previous values archived traceably, the current bar replaced, and
  no duplicate instrument-session record created.
- **FR-007**: A correction MUST cause every stored feature value that reads the corrected session
  to be recomputed, and every strategy signal for the affected sessions to be rescored across the
  whole universe rather than only for the corrected instrument.
- **FR-008**: The routine pass MUST remain one kind of run. Widening what it observes does not
  change what it is for, and splitting it would fragment the operational report for no gain.
- **FR-009**: A run MUST record how many sessions it corrected, distinctly from how many it
  processed, accepted, rejected and flagged.
- **FR-010**: The count of corrected sessions MUST never exceed the count of sessions processed.
- **FR-011**: The operational screen MUST state how many sessions a run corrected, and MUST NOT
  imply a correction occurred when none did.
- **FR-012**: The window size MUST be a stated, reviewable value rather than an incidental
  constant scattered through the code, so that changing it is a decision somebody makes rather
  than a detail somebody edits.
- **FR-013**: A run following an extended outage MUST NOT widen its window to cover the missed
  period; recovering missed history remains an explicit operator action.
- **FR-014**: Re-observation MUST NOT increase the number of source requests a routine pass
  makes, because a range is requested once per instrument regardless of its width.

### Test-First Proof *(mandatory)*

- **Initial failing test**: `TestARestatedCloseIsCorrectedByTheNextRoutinePass` — restate one
  instrument's close three sessions back in the source, run the routine daily pass, and assert
  the stored bar now carries the restated close and the previous values are archived as a
  revision.
- **Expected red reason**: The stored bar still holds the original close and no revision exists,
  because the pass asked the source only about the session that just closed. This is a value
  failure on stored data, not a setup or compilation failure.
- **Green evidence**: The market-data, scheduler, feature-engine and strategy suites, including
  the existing revision, incremental-recompute and cross-sectional rescoring tests, which must
  continue to pass unchanged — the correction path is reused, not rebuilt.
- **Database migration proof**: The corrected-session count is a new stored fact on a run, so it
  arrives as an ordered migration. A migration test must prove that a clean database and an
  upgrade from the current schema both arrive with the column present, defaulted for runs that
  predate it, and constrained so it cannot exceed the processed count — with no manual step.

### Responsive UI Behavior *(mandatory for user-facing features; otherwise state N/A)*

The only surface that changes is the operational screen's report of recent imports, which gains
one stated fact per run.

- **Mobile (320-767 CSS px)**: The corrected-session count reads as a labelled value in the run's
  stacked layout, in the same list as the counts already shown, without horizontal page
  scrolling. The 360x800 scenario asserts that a run which corrected sessions states so, and that
  a run which corrected none does not.
- **Tablet (768-1023 CSS px)**: The 768x1024 scenario asserts the same content in the tabular
  layout the screen adopts at that width.
- **Desktop (1024+ CSS px)**: The 1440x900 scenario asserts the same, and that the added value
  does not push the existing counts out of view.
- **Input and accessibility**: The count is text with its own label, legible to a screen reader
  in the same reading order as the counts beside it. Nothing about the correction is conveyed by
  colour alone. At the 320-pixel floor nothing clips or overlaps.

### Live Update Behavior *(mandatory for client-visible data; otherwise state N/A)*

- **Snapshot and events**: No new event type. A run's corrected-session count travels with the
  run in the existing import-run change event and its REST snapshot. The corrections themselves
  already publish the events they always would: the bar change, the feature recomputation, and
  the signal rescoring.
- **Reliability**: Unchanged. The existing authorization scope, event identifiers, ordering,
  resumption from the last delivered event, duplicate-safe consumption and bounded buffering
  apply, because no new stream is introduced.
- **Test evidence**: An operator watching the operational screen when a routine pass corrects a
  session sees the corrected count arrive without reloading, and a reconnecting client replays it
  exactly once.

### Identity, Ownership, and Permissions *(mandatory for user/account data; otherwise state N/A)*

N/A for new data: imports, bars, features and signals are shared reference data, and this feature
introduces no user-owned record. The reads it touches remain behind the authenticated boundary
and refused to a deactivated account, which the existing tests already assert.

### PWA and Notification Behavior *(mandatory when applicable; otherwise state N/A)*

N/A.

### Key Entities *(include if feature involves data)*

- **Observation window**: How far back a routine pass re-asks the source, counted in trading
  sessions on the instrument's own exchange. A stated, reviewable value.
- **Import run**: Gains one fact — how many sessions it corrected — alongside the counts it
  already reports.
- **Price bar revision**: Unchanged. The archive of previous values that this feature finally
  causes to be written in normal operation rather than only when an operator intervenes.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A close restated by the source within the last five trading sessions is reflected
  in the stored history after the next routine pass, with no operator action.
- **SC-002**: A routine pass with the widened window makes the same number of source requests as
  a single-session pass, so the change consumes no additional source quota.
- **SC-003**: A routine pass in which nothing was restated writes no bar, archives no revision,
  and triggers no feature or strategy computation.
- **SC-004**: When a session is restated, every stored feature value derived from it and every
  strategy signal for the affected sessions is recomputed, verifiable by comparing stored values
  against a full recomputation and finding no difference.
- **SC-005**: A routine pass with the widened window completes within the time budget the
  single-session pass already meets.
- **SC-006**: An operator reading the operational screen can tell, without querying the database,
  whether a run corrected history or only extended it.
- **SC-007**: A restatement older than the window is not silently absorbed: the product's
  documented recovery is an explicit backfill, and the limit of automatic detection is stated
  where an operator will read it.

## Assumptions

- **Five sessions is a starting value, not a derived one.** No measurement of this provider's
  restatement latency exists. Five covers a correction published within a normal working week
  while keeping the window short enough that its cost stays negligible. FR-012 requires it to be
  a stated value precisely so that evidence can change it later without archaeology.
- **The routine pass keeps its existing kind.** Its purpose — the scheduled pass that keeps
  stored history current — is unchanged; only how far back it looks has widened. A new kind would
  split the operational report in two for no reader's benefit.
- **The correction path is reused entirely.** Source-value comparison, revision archiving,
  incremental feature recomputation and universe-wide signal rescoring all exist, are specified,
  and are tested. This feature causes them to run; it does not change them.
- **No new source, plan, or data grain.** This re-asks for daily sessions the product already
  stores, through the import path that already exists. Intraday data, a different source, and any
  change to what a bar means are explicitly out of scope and would each be their own
  specification.
- **The measurements behind "this is cheap" are properties of the current implementation**, and
  the plan must confirm both still hold: a source range is requested once per instrument
  regardless of width, and an unchanged re-observation performs no write, which is what keeps a
  quiet night from triggering a recomputation cascade.
