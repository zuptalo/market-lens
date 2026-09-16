# Feature Specification: A Finding Re-observation Cannot Settle

**Feature Branch**: `017-unsettleable-findings`

**Created**: 2026-09-08

**Status**: shipped
<!-- Market Lens spec lifecycle: planned → in-progress → in-review → shipped. -->

**Input**: User description: "A data quality finding that re-observation cannot settle. The
nightly pass reaches back to re-examine an open finding, the source reports the same condition
again, nothing resolves, and tomorrow it repeats. The product never learns that it already asked."

## User Scenarios & Testing *(mandatory)*

A finding is resolved when an import covers its session and re-validates it without raising that
rule again. That works for a condition the source later corrects. It does not terminate for a
condition the source will keep reporting — an absent session stays absent however many times it
is requested — and since the scheduled pass began reaching back to re-examine open findings,
that non-termination has become a nightly loop.

Four nights of production evidence, after the change shipped:

| Night | Observations | Corrections | Status |
|---|---|---|---|
| Before | 84 | 0 | succeeded |
| First | 33,494 | 74 | partial |
| Second | 23,217 | 29 | partial |
| Third | 23,231 | 7 | partial |
| Fourth | 23,231 | 0 | partial |

The corrections column is the re-observation working: the source genuinely restates closes, the
backlog drained, and it now sits quiet. The other two columns are the loop. Eight instruments
carry a finding on a session the source has no data for, so every night their requests reach back
years, the same session is rejected again, the same finding re-raises, and the run ends partial.

### User Story 1 - The nightly run tells the truth again (Priority: P1)

An operator opens the operational screen to learn whether last night went well. Since the change,
every night says *partial*. A status that always reports trouble reports nothing, and the next run
that genuinely goes wrong will look exactly like the four that did not.

**Why this priority**: It is a live regression, and the harm is not the amber badge — it is that
the badge has stopped meaning anything, so a real failure now passes unnoticed.

**Independent test**: Let a scheduled pass run against a source with a standing known condition
and nothing else wrong; the run reports success, and the observation count is the ordinary
window's rather than years of history.

**Acceptance Scenarios**:

1. **Given** an instrument whose only rejected row is a session already known and awaiting a
   decision, **When** the scheduled pass runs, **Then** its item and the run report success.
2. **Given** an instrument with a rejection the product has not seen before, **When** the
   scheduled pass runs, **Then** the item and the run report partial exactly as they do today.
3. **Given** a finding the source keeps reporting, **When** several scheduled passes run,
   **Then** the second and later passes do not reach back to its session, and the observation
   count returns to the ordinary window's size.
4. **Given** any configuration, **When** the scheduled pass builds its requests, **Then** none
   reaches further back than the stated maximum, whatever the findings say.

---

### User Story 2 - A finding that cannot be settled says so (Priority: P2)

The product asked the source again and got the same answer. That is information, and there is
nowhere to record it: a finding is either open or resolved, so a condition that holds has to stay
open, indistinguishable from one nobody has checked.

**Why this priority**: Without it the loop can only be stopped by forgetting, and forgetting is
what made these findings unreachable in the first place.

**Independent test**: Cause a finding, let a pass re-examine it and raise the same rule again, and
confirm the finding now records that re-observation examined it and the condition held.

**Acceptance Scenarios**:

1. **Given** an open finding whose session an import covers, **When** the import re-validates that
   session and raises the same rule again, **Then** the finding records that it was re-examined
   and the condition still holds, and it stops driving the automatic reach-back.
2. **Given** the same finding, **When** a later import covers its session and does *not* raise the
   rule, **Then** the finding resolves exactly as it does today — being re-examined does not make
   a finding permanent.
3. **Given** a finding awaiting a decision, **When** any number of further scheduled passes run,
   **Then** the product does not decide on its behalf, and its state does not change on its own.
4. **Given** a finding with no session recorded, **When** imports run, **Then** it is unaffected:
   a session-scoped import cannot speak for it, which is already true today.

---

### User Story 3 - An operator can settle what the product cannot (Priority: P3)

Some conditions are simply true of the data. A session the source never had is not a defect to be
fixed; it is a limitation to be acknowledged. The product must not make that judgement — it is a
statement about somebody's own data — but it must let them make it, and act on it.

**Why this priority**: Without an action, a finding awaiting a decision is a quieter version of
the dead end this feature exists to remove.

**Independent test**: With findings awaiting a decision, open the operational screen, read what
each one says, accept one as a limitation, and confirm it leaves the list and does not return.

**Acceptance Scenarios**:

1. **Given** findings awaiting a decision, **When** an operator opens the operational screen,
   **Then** each is listed with its instrument, session, rule and detail, in a way that says what
   is being asked of them.
2. **Given** such a finding, **When** an operator accepts it as a limitation, **Then** it is
   recorded as accepted, by whom and when, and no longer awaits a decision.
3. **Given** no findings await a decision, **When** the screen is read, **Then** it says so rather
   than showing an empty table.
4. **Given** a caller who is not the owner, **When** they attempt to accept a finding, **Then** it
   is refused.

---

### Edge Cases

- **A source that fixes an old session later.** A finding awaiting a decision is no longer
  re-examined automatically, so the fix is not noticed on its own. That is deliberate: noticing it
  costs a years-wide request every night forever, against an event that has never been observed.
  An operator who believes it has changed runs an explicit backfill, which re-validates the
  session and resolves the finding through the path that already exists.
- **A finding accepted as a limitation, then genuinely fixed.** An explicit backfill covering its
  session re-validates it. What an accepted finding does when its condition disappears is stated
  by FR-013 rather than left to chance.
- **Several findings on one instrument at different sessions.** The reach-back is driven by the
  oldest that still qualifies; once none qualifies, the instrument returns to the ordinary window.
- **A finding raised for the first time on an old session** by a backfill. It is new information
  and has not been re-examined, so it drives the reach-back once, within the stated bound.
- **The bound is shorter than a finding's age.** The scheduled pass does not reach it; the finding
  stays as it is and is only re-examined by an explicit backfill. The bound always wins.
- **The same rule raised at a session by two different imports** on the same day. Re-examination
  is a property of the finding and its session, not a counter of imports, so the state does not
  depend on how many passes happened to run.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: When an import covers an open finding's session and raises the same rule for that
  session again, the finding MUST record that re-observation examined it and the condition still
  holds.
- **FR-002**: A finding in that state MUST NOT cause the scheduled pass to reach back to its
  session.
- **FR-003**: A finding in that state MUST still resolve if a later import covers its session and
  does not raise the rule.
- **FR-004**: The product MUST NOT move a finding to accepted on its own. Accepting a data quality
  condition is a person's judgement about their own data.
- **FR-005**: The scheduled pass MUST NOT reach further back than a stated maximum, whatever any
  finding says, and that maximum MUST be a single value a deployment can change without a code
  change.
- **FR-006**: A configured maximum outside its permitted range MUST be refused at startup with a
  message naming the setting, rather than silently clamped.
- **FR-007**: A rejected observation that corresponds to a finding already awaiting a decision for
  that instrument and session MUST NOT degrade the import item's status or the run's.
- **FR-008**: Any other rejected observation MUST degrade them exactly as it does today.
- **FR-009**: A run's reported counts MUST continue to state what was processed, accepted,
  rejected, flagged and corrected, so that suppressing a status does not suppress the numbers.
- **FR-010**: An owner MUST be able to read the findings awaiting a decision, each with its
  instrument, session, rule and detail.
- **FR-011**: An owner MUST be able to accept such a finding as a limitation, and the product MUST
  record who accepted it and when.
- **FR-012**: Accepting a finding MUST be refused to a caller who is not the owner, and to an
  unauthenticated or deactivated one.
- **FR-013**: A finding accepted as a limitation MUST NOT be reopened automatically; if a later
  import re-validates its session without raising the rule, it MUST resolve, because the condition
  it describes has ended.
- **FR-014**: Every state change MUST be attributable — to the run that caused it, or to the
  person who made it — and MUST be visible to a client through the existing change contract.

### Test-First Proof *(mandatory)*

- **Initial failing test**: `TestAReExaminedFindingStopsDrivingTheReachBack` — an import
  re-validates a finding's session and raises the same rule again; the next scheduled pass is
  asserted not to reach back to that session.
- **Expected red reason**: The pass still requests from the finding's session, because nothing
  records that it was already examined. A value failure on the requested range, not a compilation
  or setup failure.
- **Green evidence**: The market-data and scheduler suites, including the existing resolution,
  re-observation and widening tests, which must continue to pass — the resolution path is reused,
  not replaced.
- **Database migration proof**: The state and its attribution are new stored facts, so they arrive
  as an ordered migration. A migration test must prove that a clean install and an upgrade from
  the current schema both arrive with the new state permitted, existing findings untouched and
  still open, and no manual step.

### Responsive UI Behavior *(mandatory for user-facing features; otherwise state N/A)*

The operational screen gains a report of findings awaiting a decision, with an action on each.

- **Mobile (320-767 CSS px)**: Each finding reads as a stacked record — instrument, session, rule,
  detail — with its action full width beneath, without horizontal page scrolling. The 360x800
  scenario asserts a finding is readable and its action reachable, and that an account with none
  says so rather than showing an empty table.
- **Tablet (768-1023 CSS px)**: The 768x1024 scenario asserts the same content in the tabular
  layout the screen adopts at that width.
- **Desktop (1024+ CSS px)**: The 1440x900 scenario asserts the same, at the shared content width
  every other report on the screen uses.
- **Input and accessibility**: The action is keyboard reachable and named for the finding it acts
  on, so a screen reader hears which one is being accepted. Nothing about a finding's state is
  conveyed by colour alone. At the 320-pixel floor nothing clips or overlaps.

### Live Update Behavior *(mandatory for client-visible data; otherwise state N/A)*

- **Snapshot and events**: No new event type. A finding's change of state publishes the same
  quality-finding change event it already publishes when resolved, in the same transaction as the
  row it reports, and an open screen re-reads through the authorized path.
- **Reliability**: Unchanged. Existing authorization scope, ordering, resumption from the last
  delivered event, duplicate-safe consumption and bounded coalescing apply.
- **Test evidence**: An operator watching the screen when a pass re-examines a finding, or when
  they accept one, sees the change arrive without reloading, and a reconnecting client replays it
  exactly once.

### Identity, Ownership, and Permissions *(mandatory for user/account data; otherwise state N/A)*

- **Bootstrap and invitations**: Unchanged; this feature introduces no account or invitation
  behaviour.
- **Ownership and authorization**: Findings are shared operational reference data that every
  authenticated user may read. Accepting one is an owner action, refused to a member in the
  backend service rather than only hidden in the interface.
- **Security evidence**: Tests prove the read is refused to an unauthenticated and to a
  deactivated caller, and that the accept is refused to a member and to a deactivated owner.

### Key Entities *(include if feature involves data)*

- **Data quality finding**: Gains a state for "re-observation examined this again and the
  condition still holds", the run that established it, and — when a person accepts it — who did so
  and when.
- **Re-observation reach**: How far back the scheduled pass may look on account of a finding,
  bounded by a stated maximum independent of any finding's age.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Within two scheduled passes of this shipping, the nightly observation count returns
  to the ordinary re-observation window's size rather than years of history.
- **SC-002**: A scheduled pass reports success when nothing about that night went wrong, so a
  reported failure means a failure that night.
- **SC-003**: No finding causes the scheduled pass to reach back to its session more than once
  without new information having arrived.
- **SC-004**: No finding is ever recorded as accepted without a person having accepted it.
- **SC-005**: No request the scheduled pass makes reaches further back than the stated maximum.
- **SC-006**: An operator can find every finding awaiting a decision, understand what each says,
  and act on one, without querying the database.
- **SC-007**: The counts a run reports are unchanged in meaning: suppressing a status never
  suppresses the number of rejections behind it.

## Assumptions

- **One re-examination is enough.** An identical request producing an identical answer is complete
  information; asking a third time cannot tell the product anything the second did not. The
  alternative — a threshold of several attempts — only delays the same conclusion while paying the
  same nightly cost, and there is no evidence a source that reports a condition twice stops on the
  third try.
- **The rule does not vary by finding type.** An absent session and a suspicious value are
  different conditions, but the question asked of them is the same one: did asking again change
  the answer? What differs is what an operator decides afterwards, which is theirs to judge.
- **A source correcting a years-old session is not worth a nightly search.** It has never been
  observed in this deployment, and detecting it automatically costs a years-wide request every
  night in perpetuity. The explicit backfill remains the way to look, and is stated where an
  operator will read it rather than left as an omission.
- **Accepting is an interface action, not a command.** It is a judgement about data rather than a
  computation over it, and the operator is already reading the finding on the screen when they
  form it. Computation stays at the command line as the constitution requires.
- **The existing resolution rule is correct and is reused unchanged.** This feature adds a state
  reached when that rule declines to fire; it does not alter when a finding resolves.
