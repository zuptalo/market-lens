# Feature Specification: Reusable Feature Engine

**Feature Branch**: `013-feature-engine`

**Created**: 2026-08-31

**Status**: shipped
<!-- Market Lens spec lifecycle: planned → in-progress → in-review → shipped. -->

**Input**: User description: "Reusable feature engine. Deterministic, versioned, timestamped
computation of quantitative features from the stored daily history feature 002 owns: returns
over stated session windows, trend, momentum, relative strength against a named comparison
series, realised volatility, ATR, RSI, MACD, drawdown, volume features, and a regime
classification. Every feature is computed from stored exchange sessions only, is reproducible
bit-for-bit for identical inputs, carries the definition version that produced it, and is
proven free of lookahead by automated leakage tests. Missing sessions, insufficient history,
corporate actions and half days must each have defined behavior rather than a silently imputed
value. This engine owns the authoritative definitions of the descriptive statistics feature
005 currently computes for display; the spec must state whether those definitions change and
what happens to the Markets table if they do. No strategies, no signals, no scores, no
backtesting, and no user-owned data are in scope."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Compute the feature set over the stored universe (Priority: P1)

An owner runs the feature computation over the curated universe. For every instrument and
every stored session, the engine derives the defined features from the stored daily bars and
records each value together with the session it describes, the definition version that
produced it, and the moment it was computed. Where a feature cannot be computed — too few
prior sessions, a gap in the window — the engine records that it is undefined rather than
inventing a number.

**Why this priority**: Nothing else in this feature exists without it, and nothing in the
milestones after it can begin. Strategies read features, backtests replay them, and both are
worthless if the values are not reproducible. It is independently valuable on its own: a
stored, versioned feature history is a research artifact even before anything consumes it.

**Independent Test**: Run the computation over the fixture universe, then read back the
feature values for a known instrument and session and confirm each defined feature carries a
value or an explicit absence, its definition version, and its session date.

**Acceptance Scenarios**:

1. **Given** an instrument with sufficient stored history, **When** the feature set is
   computed, **Then** every defined feature has a value for every session where its window is
   satisfied, each carrying the definition version that produced it.
2. **Given** a session whose lookback window extends past the instrument's first stored
   session, **When** the feature set is computed, **Then** the affected features are recorded
   as undefined for that session, and the reason is available.
3. **Given** a lookback window that spans a session the exchange was open for with no stored
   bar, **When** the feature set is computed, **Then** the affected features are undefined for
   that session rather than computed over a shortened window.
4. **Given** a session the exchange was closed, **When** the feature set is computed, **Then**
   no feature value is produced for that date at all, because there was no session.
5. **Given** a half day, **When** the feature set is computed, **Then** it is treated as a
   full session for window counting, and any feature whose definition depends on session
   length states that it does.
6. **Given** an instrument with no stored history, **When** the feature set is computed,
   **Then** it produces no values and no error.

---

### User Story 2 - Read one instrument's features as of a session (Priority: P1)

A signed-in person, or a later part of the system, asks for the feature values for one
instrument as of a stated session. They receive exactly the values that were computable from
data stored up to and including that session, each labelled with its definition version and
its window length in sessions. Asking again for the same instrument and session returns the
same answer.

**Why this priority**: A feature history that cannot be read as of a point in time is not
usable by a backtest, which is the whole reason to store it rather than recompute it. Equal
in priority to computing them because computation without readback proves nothing.

**Independent Test**: Read one instrument's features as of a session, read them again, and
confirm the two answers are identical; then read the same instrument as of an earlier session
and confirm the values differ and reflect only data up to that date.

**Acceptance Scenarios**:

1. **Given** stored feature values, **When** the same instrument and session are requested
   twice, **Then** the two responses are identical in every value and version.
2. **Given** a request for a session before the instrument's first stored session, **When**
   the features are read, **Then** the response states there is no history rather than
   returning empty values that could be mistaken for zeros.
3. **Given** a request naming a feature the engine does not define, **When** it is read,
   **Then** the request is refused with a message naming the features that do exist.
4. **Given** two instruments listed in different currencies, **When** their features are read,
   **Then** every currency-denominated feature states its currency and no cross-currency
   comparison is implied or computed.

---

### User Story 3 - Trust that no feature saw the future (Priority: P2)

Someone evaluating the engine needs to be certain a feature value for a given session used
only observations available at that session. The engine proves this rather than asserting it:
recomputing any historical session with later data present produces the value it produced
before that data existed.

**Why this priority**: Lookahead is the defect that makes a research platform actively
harmful, because it produces confident, attractive, wrong results. It follows the computation
itself only because there has to be something to test.

**Independent Test**: Compute features over a truncated history, store the results, then
extend the history with later sessions, recompute, and confirm every value for the original
sessions is unchanged.

**Acceptance Scenarios**:

1. **Given** features computed over history up to session N, **When** sessions N+1 onward are
   added and the features are recomputed, **Then** every value for sessions up to N is
   byte-for-byte identical to what was computed before.
2. **Given** any feature definition, **When** its computation for a session is examined,
   **Then** it references no bar with a session date later than that session.
3. **Given** a corporate action recorded after a historical session, **When** the features for
   that session are recomputed, **Then** the result reflects the action only if the action's
   ex-date is on or before that session.

---

### User Story 4 - Recompute when a definition or the data changes (Priority: P2)

When a feature definition is corrected, or a stored bar is revised by a later import, the
affected feature values are recomputed. Values produced by an older definition version remain
distinguishable from values produced by the new one, so a reader can always tell which
definition a number came from.

**Why this priority**: Definitions will be corrected, and market data is revised — feature 002
already stores bar revisions. Without this the feature history silently becomes a mixture of
definitions that nobody can tell apart, which destroys the reproducibility the rest of the
feature is for.

**Independent Test**: Revise a stored bar, recompute, and confirm only the sessions whose
windows include that bar changed; then publish a new definition version and confirm values
carry the new version while the previous ones remain identifiable.

**Acceptance Scenarios**:

1. **Given** a stored bar is revised, **When** features are recomputed, **Then** exactly the
   sessions whose lookback windows include that bar are recomputed, and no other value
   changes.
2. **Given** a feature definition version is superseded, **When** values are computed
   afterwards, **Then** they carry the new version, and values carrying the previous version
   remain readable and clearly labelled.
3. **Given** a recomputation, **When** it fails partway, **Then** no partially recomputed
   session is left readable, and the previous values remain in force.

---

### User Story 5 - The Markets table shows the engine's numbers (Priority: P3)

The 20-session return, 90-session return and volatility already shown on the Markets list
become the engine's values rather than statistics the listing query computes for itself. A
person browsing sees the same kind of number as before, and every consumer of those three
statistics now agrees on their definition.

**Why this priority**: It closes a divergence feature 005 deliberately left open, and it is
the least urgent part of this feature because the numbers are already correct for display.
It is last because it depends on the engine existing and being trusted.

**Independent Test**: Open Markets and confirm the three statistics are present and consistent
with the engine's stored values for the same instrument and session.

**Acceptance Scenarios**:

1. **Given** an instrument with engine features computed, **When** its row is listed, **Then**
   the 20-session return, 90-session return and volatility shown are the engine's values.
2. **Given** an instrument whose features are not yet computed, **When** its row is listed,
   **Then** those three statistics are absent rather than zero, exactly as they are today when
   too few sessions exist.
3. **Given** the engine defines a statistic differently from the listing query, **When** the
   Markets table adopts it, **Then** the change is recorded in the feature's documentation so
   a reader can tell why a number moved.

---

### Edge Cases

- An instrument's stored history contains a run of missing sessions longer than a feature's
  window: every session whose window falls entirely inside the gap is undefined, and the
  feature resumes only once a complete window of stored sessions exists again.
- A feature window is longer than the instrument's entire stored history: the feature is
  undefined for every session, and this is distinguishable from "not yet computed".
- A stored bar has zero volume: volume features treat it as the observation it is, while a
  session with no stored bar remains absent — the two must not be conflated.
- An instrument is delisted mid-history: features continue to the last stored session and stop
  there, with no values produced for dates after it.
- A split's ex-date falls inside a lookback window: the feature's definition states whether it
  reads raw or adjusted closes, and the same choice applies to every session.
- Two recomputations run at once for the same instrument: one completes and the other does not
  corrupt it or produce a mixture of versions.
- The comparison series used for relative strength has no observation for a session an
  instrument traded: relative strength is undefined for that session rather than carried
  forward from the previous one.
- A definition version is published with no change in its computed values: values still carry
  the new version, so version equality never implies value equality by accident.

## Requirements *(mandatory)*

### Functional Requirements

**Definitions and determinism**

- **FR-001**: The system MUST define each feature explicitly, including its inputs, its
  lookback window expressed in stored exchange sessions, its treatment of raw versus adjusted
  prices, and the conditions under which it is undefined.
- **FR-002**: The system MUST produce identical values for identical inputs on every
  computation, with no dependence on wall-clock time, iteration order, machine, or the order
  in which instruments are processed.
- **FR-003**: The system MUST record, with every computed value, the session it describes, the
  definition version that produced it, and when it was computed.
- **FR-004**: The system MUST express every lookback window in stored exchange sessions, never
  in calendar days.
- **FR-005**: The system MUST treat a half day as one full session for window counting, and
  any feature whose meaning depends on session length MUST state so in its definition.

**The feature set**

- **FR-006**: The system MUST compute returns over stated session windows.
- **FR-007**: The system MUST compute a trend measure and a momentum measure, each with its
  window stated.
- **FR-008**: The system MUST compute relative strength against an equal-weighted composite of
  the curated universe, computed by this engine from stored sessions only.
- **FR-008a**: The composite for a session MUST be the equal-weighted mean of the
  session-over-session returns of every active instrument with a stored bar for both that
  session and the one before it. An instrument missing either bar MUST be excluded from that
  session's composite rather than carried forward.
- **FR-008b**: The composite MUST record, for every session, how many instruments contributed
  to it, and MUST be undefined for a session where fewer than a stated minimum contributed.
- **FR-008c**: Every relative-strength value MUST state that it is relative to the universe
  composite and MUST name the composite's version. The composite MUST NOT be presented as an
  index or a benchmark anywhere in the product, because it is neither.
- **FR-009**: The system MUST compute realised volatility and average true range.
- **FR-010**: The system MUST compute the relative strength index and the moving average
  convergence divergence, including its signal and histogram components.
- **FR-011**: The system MUST compute drawdown from a running peak within a stated window.
- **FR-012**: The system MUST compute volume features, including a volume average over a
  stated window and a measure of the current session's volume relative to it.
- **FR-013**: The system MUST compute a regime classification from the features above, with
  each regime defined by explicit, testable boundaries.

**Absence rather than invention**

- **FR-014**: The system MUST record a feature as undefined, never as zero or as an
  interpolated value, when its window is not satisfied by stored sessions.
- **FR-015**: The system MUST treat a session the exchange was open for with no stored bar as
  a break in the window, so any feature whose window spans it is undefined for that session.
- **FR-016**: The system MUST produce no value at all for a date the exchange was closed.
- **FR-017**: The system MUST make the reason a value is undefined available alongside the
  absence, distinguishing insufficient history from a gap in the window.

**Point-in-time correctness**

- **FR-018**: A feature value for a session MUST be derived only from observations whose
  session date is on or before that session.
- **FR-019**: A corporate action MUST affect a session's features only when its ex-date is on
  or before that session.
- **FR-020**: The system MUST prove FR-018 and FR-019 by automated test, by recomputing a
  historical range after later data exists and asserting the values are unchanged.

**Lifecycle**

- **FR-021**: The system MUST recompute exactly the sessions affected when a stored bar is
  revised, leaving all other values unchanged.
- **FR-022**: The system MUST keep values produced by different definition versions
  distinguishable, and MUST NOT overwrite a value with one produced by a different version
  without recording that the version changed.
- **FR-023**: A recomputation that fails partway MUST leave no partially updated session
  readable.

**Access**

- **FR-024**: Every read of feature values MUST require an authenticated, active session, and
  MUST answer an unknown and an unauthorized instrument identifier identically.
- **FR-025**: Users MUST be able to read the feature values for one instrument as of a stated
  session, and to read the definition of every feature including its version and window.
- **FR-026**: The Markets list's 20-session return, 90-session return and volatility MUST be
  the engine's values once computed, and MUST remain absent rather than zero when they are
  not.

### Test-First Proof *(mandatory)*

- **Initial failing test**: A query-layer test asserting that reading the feature set for an
  instrument as of a stored session returns a value or an explicit absence for every defined
  feature, each carrying its definition version. It must fail because no feature values are
  computed or stored at all, so the read returns nothing.
- **Expected red reason**: The response contains no features and no versions — a behavioral
  assertion on returned values, not a compilation or fixture failure.
- **Green evidence**: The feature-definition unit suites, the computation and readback
  integration suites, the leakage suite, the recomputation suite, and the Markets listing
  suite once it adopts the engine's values.
- **Database migration proof**: **Required.** This feature stores computed values, which is
  its first schema change since feature 002. An ordered migration test must prove a clean
  install and an upgrade from the current schema without manual steps, and must prove that an
  upgrade leaves the existing Markets statistics readable until the engine's values exist.

### Responsive UI Behavior *(mandatory for user-facing features; otherwise state N/A)*

This feature introduces no new page, and no new layout. Its only user-facing change is the
source of three columns the Markets table already renders, whose responsive behavior feature
005 already defines and covers at 360x800, 768x1024, 1440x900 and the 320-pixel floor.

- **Mobile, tablet and desktop**: unchanged from feature 005. The three statistics keep their
  existing treatment, including their behavior when absent.
- **Input and accessibility**: unchanged. The absence marker and its explanation must continue
  to be readable as text, not conveyed by colour or position alone.
- **Regression evidence**: the existing Markets responsive and accessibility journeys must
  continue to pass unchanged once the values come from the engine, which is the proof that
  this feature altered where a number comes from and nothing else.

### Live Update Behavior *(mandatory for client-visible data; otherwise state N/A)*

- **Snapshot and events**: Feature values are client-visible through the Markets list, so a
  committed change to them is a client-visible committed change. Initial state loads over
  request/response. A completed computation or recomputation MUST publish a versioned,
  shared-scope event written in the same transaction as the values it reports, naming the
  instrument and the range of sessions affected.
- **Reliability**: Delivery is on the existing authorized stream, ordered by event identifier,
  resumable from the last identifier applied, duplicate-safe, and reporting connected,
  reconnecting, stale and offline states — the behavior features 004 and 005 already
  established. The client must apply a feature change to the affected row only, without
  disturbing filters, sort, page or scroll position.
- **Test evidence**: Automated scenarios for a completed computation reaching an open Markets
  view, a duplicate event applied once, a reconnection replaying only what was missed, an
  event for an instrument outside the current filters leaving the view undisturbed, and an
  unauthenticated caller receiving no event.

### Identity, Ownership, and Permissions *(mandatory for user/account data; otherwise state N/A)*

- **Bootstrap and invitations**: N/A. This feature creates no accounts and changes no
  membership.
- **Ownership and authorization**: All feature values are derived from shared reference data
  and are themselves shared reference data, readable by every authenticated, active user. This
  feature stores no user-owned record, introduces no private scope, and adds no per-user
  event. Triggering a computation is an owner action, because it consumes resources and
  changes what every user sees.
- **Security evidence**: Automated proof that every read requires an active session, that an
  unknown and an unauthorized instrument identifier are indistinguishable, that a
  non-owner cannot trigger a computation, and that a deactivated or revoked session loses
  access immediately.

### PWA and Notification Behavior *(mandatory when applicable; otherwise state N/A)*

N/A. This feature neither requires nor changes installability or notifications.

### Key Entities *(include if feature involves data)*

- **Feature definition**: One named quantitative feature — its inputs, its window in sessions,
  its price basis, the conditions under which it is undefined, and its version. Definitions
  are additive and versioned; a definition is never edited in place.
- **Feature value**: One feature's value for one instrument on one session, or an explicit
  absence with its reason, carrying the definition version that produced it and when it was
  computed.
- **Computation run**: One execution of the engine over a set of instruments and sessions, its
  outcome, and what it covered — so a value can always be traced to the run that produced it.
- **Universe composite**: The equal-weighted series relative strength is measured against,
  derived by this engine from the curated universe's stored sessions, versioned like any other
  definition, and carrying the number of contributing instruments for each session. It is a
  composite, never an index or a benchmark.
- **Regime**: A named classification of an instrument's state on a session, defined by
  explicit boundaries over other features rather than by judgement.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Recomputing the entire feature history for the curated universe produces values
  identical to the previous computation, with zero differences.
- **SC-002**: Extending the stored history with later sessions and recomputing changes no
  value for any earlier session — zero lookahead, verified across the full universe.
- **SC-003**: Every feature value in the store carries a definition version that resolves to a
  published definition; no value carries an unknown or absent version.
- **SC-003a**: Every relative-strength value names the composite version it was measured
  against, and no surface in the product describes the composite as an index or a benchmark.
- **SC-004**: No feature value exists for a date the exchange was closed, and no feature value
  is derived from a window containing a session the exchange was open for with no stored bar,
  verified across the full universe.
- **SC-005**: A statistic that cannot be computed is absent rather than zero in every surface
  that shows it, verified across the full universe.
- **SC-006**: The full feature history for the curated universe can be computed from empty
  within a stated time budget on the deployment's own hardware, and an incremental computation
  after a single import completes within a smaller stated budget.
- **SC-007**: Reading one instrument's features as of a session returns within the same bound
  the Markets list already meets.
- **SC-008**: A revision to one stored bar recomputes only the sessions whose windows include
  it, measured as the count of changed values.
- **SC-009**: An unauthenticated request for any feature data is refused, and a request naming
  an instrument identifier is indistinguishable from one naming an identifier that does not
  exist.
- **SC-010**: The Markets table's three adopted statistics match the engine's stored values
  for every instrument in the universe.

## Assumptions

- The stored daily history, the exchange calendar, corporate actions and quality findings are
  those feature 002 already delivers. This feature reads them and adds no provider
  integration and no new market data.
- Authentication, the protected-by-default boundary and the authorized event stream are those
  feature 004 delivers. This feature adds no authentication behavior.
- **The three statistics feature 005 displays keep their current definitions.** The engine
  adopts feature 005's existing definitions verbatim as version 1 of those three features —
  20- and 90-session returns over stored sessions, and volatility as the annualised standard
  deviation of twenty session-over-session logarithmic returns. Nothing a person currently
  sees on the Markets table changes value; only the source of the number changes, from the
  listing query to the engine. This is deliberate: adopting the engine and changing the
  numbers in the same release would make it impossible to tell a definition change from a
  computation defect. A later version may change a definition, and FR-022 requires it be
  distinguishable when it does.
- Features are stored rather than recomputed on read. Storage is what makes a value
  point-in-time readable by a later backtest, and what makes the version it was computed under
  a fact rather than a reconstruction.
- Regime classification is defined over this feature's own outputs — trend, volatility and
  drawdown — with explicit numeric boundaries, rather than over any judgement, external
  classification or model.
- The engine computes daily features only. Hourly data remains deferred.
- No strategy, signal, score, confidence, ranking, recommendation or backtest is in scope.
  Those belong to the milestones that follow and must not be pre-empted here.
- No user-owned data is in scope. The engine produces shared reference data only.
- Feature values are displayed and compared only within one instrument's own currency. No
  currency conversion happens here, because the FX history it would need does not exist.

## Resolved decisions

- **What relative strength is measured against** (FR-008, decided 2026-08-31). The product
  stores no index or benchmark data — feature 002 delivers individual Nordic listings only —
  so relative strength had nothing to be relative to.

  **Decision: an equal-weighted composite of the curated universe**, computed by this engine
  from stored sessions only. It needs no new data, no provider and no cost, it survives any
  single listing being delisted, and it is honest about what it measures: performance relative
  to the universe this product actually tracks.

  Two things follow, and both are requirements rather than notes. The composite is a derived
  series this feature owns and versions like any other definition (FR-008a, FR-008b). And it
  must never be labelled an index or a benchmark anywhere in the product (FR-008c) — it is a
  composite of one curated list, and calling it otherwise would invite a reader to compare it
  with a real index it has no relationship to.

  *Rejected*: a single named listing as a proxy, because one company's fate would distort
  every other instrument's value and a delisting would break the entire feature history.
  *Rejected*: deferring relative strength, because the strategy milestone hits the same gap and
  the regime classification loses a useful input.
