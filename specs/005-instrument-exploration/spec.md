# Feature Specification: Instrument Exploration and Financial Charts

**Feature Branch**: `005-instrument-exploration`

**Created**: 2026-08-31

**Status**: in-review
<!-- Market Lens spec lifecycle: planned → in-progress → in-review → shipped. -->

**Input**: User description: "Instrument exploration and financial charts. Authenticated
users browse and search the curated Nordic universe from a Markets view with sortable,
filterable, paginated, selectable columns covering exchange-qualified identity, native
price and change, 20/90-day return, volatility, sector, country, and data freshness.
Selecting an instrument opens an Instrument Detail view showing exchange-qualified
identity, native price and change, sector/country, a responsive candlestick and volume
chart over the stored daily history with zoom and pan, selectable ranges, moving-average
overlays, and clearly surfaced data quality, corporate actions, and gaps so nothing is
silently smoothed over. All data is the shared market data already stored by feature 002;
this feature adds no provider integration, no private user data, no watchlists, and no
signals or scores. Charts must be usable on mobile, tablet, and desktop with touch and
keyboard, must not depend on hover, and must degrade honestly when history is missing.
Live updates arrive over the existing authorized SSE stream rather than polling."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Find an instrument worth looking at (Priority: P1)

A signed-in person opens Markets and sees the curated Nordic universe as a table they can
read at a glance: which listing this is and on which exchange, what it last closed at in
its own currency, how much it moved, and how current that information is. They narrow the
list by exchange, country, or sector, type part of a name or ticker to search, sort by the
column they care about, and page through the result. Every row states plainly how fresh
its data is, so a stale listing is never mistaken for a current one.

**Why this priority**: Without this there is no way to reach an instrument at all. It is
the entry point for every later milestone, and it is independently valuable: the universe
becomes browsable the day it ships, even if the detail view never existed.

**Independent Test**: Sign in, open Markets, and confirm the curated universe is listed
with identity, price, change, and freshness; then filter, search, sort, and page and
confirm the result set changes correctly and the visible state survives a reload.

**Acceptance Scenarios**:

1. **Given** a signed-in person and a universe with stored daily history, **When** they
   open Markets, **Then** they see a paginated table of instruments with exchange-qualified
   identity, latest close in the listing currency, change from the prior session, and the
   session date that close belongs to.
2. **Given** the Markets table, **When** they sort by a column, **Then** the entire result
   set is ordered by that column rather than only the page in front of them, and the
   ordering is stable for equal values.
3. **Given** the Markets table, **When** they filter by exchange, country, or sector and
   search by name, ISIN, or ticker, **Then** only matching instruments are listed and the
   active filters remain visible and individually removable.
4. **Given** an instrument whose most recent stored session is older than the rest of the
   universe, **When** it appears in the table, **Then** its row states how stale it is
   rather than presenting the old close as current.
5. **Given** an instrument with no stored history at all, **When** it appears in the table,
   **Then** its price columns say so explicitly and the row remains selectable.
6. **Given** a filter or search that matches nothing, **When** the result is empty,
   **Then** an explanation and a way to clear the filters is shown instead of a blank table.

---

### User Story 2 - Read one instrument's price history (Priority: P1)

Selecting a row opens Instrument Detail. The person sees the same exchange-qualified
identity plus sector, country, and currency, and below it a candlestick chart of the
stored daily history with a volume series beneath it. They switch between ranges, zoom and
pan within the history, and turn moving-average overlays on and off. The chart shows the
history that exists and nothing more: missing sessions appear as gaps rather than being
bridged, and the range of stored coverage is stated.

**Why this priority**: Reading price history is the point of the product's research half.
It is equal in priority to browsing because a list that leads nowhere delivers little, and
it is independently testable against one instrument.

**Independent Test**: Open one instrument with stored history and confirm the candlestick
and volume series render the stored sessions, that range, zoom, pan, and overlays change
what is displayed, and that a gap in the stored history is visible as a gap.

**Acceptance Scenarios**:

1. **Given** an instrument with stored daily history, **When** its detail view opens,
   **Then** a candlestick series and an aligned volume series are shown over a default
   range, with the axes labelled in the listing currency and the exchange's session dates.
2. **Given** the chart, **When** the person selects a different range, **Then** the chart
   redraws over that range and states the first and last session actually available within
   it.
3. **Given** the chart, **When** the person zooms or pans, **Then** the visible window
   changes without losing the selected overlays, and the window cannot be moved outside
   the stored coverage.
4. **Given** a moving-average overlay, **When** it is enabled, **Then** it is drawn only
   where enough prior sessions exist to compute it, and it is not extended over a gap.
5. **Given** an instrument with sessions missing inside the stored range, **When** the
   chart is drawn, **Then** the missing sessions appear as an interruption and the count
   of missing sessions in view is stated.
6. **Given** an instrument with fewer stored sessions than the selected range, **When** the
   chart is drawn, **Then** it shows what exists and says how much of the requested range
   is covered rather than padding it.

---

### User Story 3 - Trust what the chart is showing (Priority: P2)

The same detail view surfaces what would otherwise silently distort a price series:
recorded corporate actions on the dates they occurred, open data-quality findings for this
instrument, whether the series shown is raw or provider-adjusted, and which provider and
observation time the data came from. A person can tell the difference between a real 50%
move and an unadjusted split.

**Why this priority**: The product's stated position is that it never invents observations
to hide defects. A chart without this context is actively misleading, but the chart itself
has to exist first, so this follows Story 2 rather than blocking it.

**Independent Test**: Open an instrument that has a recorded corporate action and an open
quality finding and confirm both are visible in the detail view, anchored to their dates,
and that the displayed series states whether it is raw or adjusted.

**Acceptance Scenarios**:

1. **Given** an instrument with a recorded split or dividend inside the visible range,
   **When** the chart is drawn, **Then** the action is marked at its session and its type
   and value are readable without hovering.
2. **Given** an instrument with open quality findings, **When** its detail view opens,
   **Then** the findings are listed with their rule, affected sessions, and status.
3. **Given** a series that is provider-adjusted, **When** it is displayed, **Then** the
   view says so and names the provider and the observation time of the latest bar.
4. **Given** an instrument with a suspicious jump already recorded as a finding, **When**
   the chart is drawn over that session, **Then** the session is marked rather than
   smoothed, and the finding explains it.

---

### User Story 4 - Keep the view current without reloading (Priority: P3)

While Markets or a detail view is open, newly imported bars, resolved quality findings, and
completed imports update what is on screen without the person reloading and without losing
their filters, sort, selected range, zoom window, or scroll position. Connection state is
visible, and a dropped connection recovers the changes it missed.

**Why this priority**: The live path already exists from earlier features; this story
connects these views to it. It is the least valuable to ship first because the views are
useful when static, but it is required before the feature can be called complete.

**Independent Test**: With a detail view open, commit a new bar for the displayed
instrument and confirm the chart updates in place with the person's range, zoom, and
overlays intact; then drop the connection, commit another change, and confirm it arrives
on reconnection exactly once.

**Acceptance Scenarios**:

1. **Given** an open Markets view, **When** a new daily bar is committed for a listed
   instrument, **Then** that row's price, change, and freshness update in place without
   resetting filters, sort, or page.
2. **Given** an open detail view with a custom zoom window, **When** a new bar is committed
   for that instrument, **Then** the chart incorporates it while preserving the range,
   zoom, and overlay selections.
3. **Given** an open view, **When** the connection drops and is restored, **Then** changes
   committed during the outage are applied exactly once, and the view reports reconnecting,
   stale, and offline states while it is not current.
4. **Given** an open view, **When** a change is committed for an instrument that is not
   visible under the current filters, **Then** the view does not jump, reorder, or scroll
   to it.

---

### Edge Cases

- An instrument is delisted or deactivated while its detail view is open: the view states
  the lifecycle status and keeps the history readable rather than emptying.
- Stored history contains a session with zero or missing volume: the volume series shows
  the gap rather than drawing zero as if it were an observation.
- Two instruments share a ticker on different exchanges: every identity display is
  exchange-qualified, and search results distinguish them without the person guessing.
- A range is selected that begins before the instrument's first stored session: the chart
  starts at the first session that exists and says so.
- The stored history for one instrument is long enough that drawing every session at once
  would be unreadable or slow: the view remains responsive and the density adapts to the
  visible window.
- An instrument's currency differs from other rows in the table: every price states its own
  currency and no cross-currency comparison is implied or computed.
- A person opens a detail view by a URL naming an instrument that does not exist or is not
  readable: the response is the same as for one that does not exist.
- The person's session expires while a view is open: they are returned to sign-in without
  the page continuing to display data it can no longer refresh.

## Requirements *(mandatory)*

### Functional Requirements

**Browsing and search**

- **FR-001**: The system MUST list the curated instrument universe to any signed-in,
  active user, with exchange-qualified identity, listing currency, sector, country, and
  lifecycle status for every instrument.
- **FR-002**: The system MUST show, for each listed instrument, the latest stored close,
  the change from the prior stored session in both absolute and percentage terms, and the
  session date that close belongs to.
- **FR-003**: The system MUST show a freshness indication per instrument derived from its
  most recent stored session relative to the universe, and MUST distinguish "no stored
  history" from "history that is stale".
- **FR-004**: Users MUST be able to filter the list by exchange, country, sector, and
  lifecycle status, and to search by name, ticker, or ISIN, with filters combinable and
  individually removable.
- **FR-005**: Users MUST be able to sort the list by identity, price, change, the derived
  statistics of FR-007, and freshness. Sorting MUST order the whole result set, not the
  current page, and MUST be stable for equal values.
- **FR-006**: The system MUST paginate the list with a bounded page size and MUST keep
  filters, search, sort, and page position across reloads and back navigation.
- **FR-007**: The system MUST show descriptive 20-session and 90-session returns and a
  volatility measure per instrument, computed from stored sessions only, and MUST state
  when there are too few stored sessions to compute one rather than showing zero.
- **FR-008**: Users MUST be able to choose which optional columns are displayed, and that
  choice MUST persist across visits on the device where it was made.

**Instrument detail and charts**

- **FR-009**: The system MUST present, for one instrument, its exchange-qualified identity,
  ISIN, sector, industry, country, listing currency, lifecycle status, and the first and
  last session of its stored coverage.
- **FR-010**: The system MUST draw a candlestick series of stored daily open, high, low,
  and close, with an aligned volume series, over a selectable range.
- **FR-011**: Users MUST be able to zoom and pan within the stored coverage, and the
  visible window MUST NOT extend beyond it.
- **FR-012**: Users MUST be able to enable and disable moving-average overlays. An overlay
  MUST be drawn only where enough prior stored sessions exist to compute it and MUST NOT be
  interpolated across missing sessions.
- **FR-013**: The system MUST render missing sessions inside the visible range as
  interruptions in the series and MUST state how many sessions are missing in view. It MUST
  NOT interpolate, forward-fill, or otherwise invent an observation.
- **FR-014**: The system MUST state whether the displayed series is raw or
  provider-adjusted, and MUST name the provider and the observation time of the latest bar.
- **FR-015**: The system MUST mark recorded corporate actions at their sessions within the
  visible range and make their type and value readable without hover.
- **FR-016**: The system MUST list the instrument's open data-quality findings with rule,
  affected sessions, and status, and MUST mark affected sessions in the chart.
- **FR-017**: The system MUST make every value that the chart conveys visually also
  available as text, so the information does not depend on reading a graphic.

**Access and live behavior**

- **FR-018**: Every view and every underlying read in this feature MUST require an
  authenticated, active session, and MUST answer an unknown and an unauthorized instrument
  identically.
- **FR-019**: The system MUST load initial state over request/response and MUST apply
  subsequent committed changes from the existing authorized event stream. Polling MUST NOT
  be the primary update path.
- **FR-020**: Applying a live change MUST preserve the person's filters, sort, page,
  selected range, zoom window, overlay selections, and scroll position.
- **FR-021**: The system MUST show connected, reconnecting, stale, and offline states, and
  MUST apply changes missed during an outage exactly once on recovery.

### Test-First Proof *(mandatory)*

- **Initial failing test**: A query-layer test asserting that listing the universe returns
  each instrument's latest close, prior-session change, and coverage freshness ordered by a
  requested sort key. It must fail because the existing search returns identity only, with
  no price, change, freshness, or sort.
- **Expected red reason**: The returned rows carry no latest close and no change, and the
  requested ordering is ignored — a behavioral assertion on returned values, not a
  compilation or fixture failure.
- **Green evidence**: The instruments query, API, and view suites, the chart component
  suites, and the responsive and live-update end-to-end journeys for this feature.
- **Database migration proof**: If derived statistics or session-gap detection require
  stored support rather than query-time computation, an ordered migration test must prove a
  clean install and an upgrade from the current schema without manual steps. If everything
  is computed from existing tables at query time, this is N/A and the plan must say so.

### Responsive UI Behavior *(mandatory for user-facing features; otherwise state N/A)*

- **Mobile (320-767 CSS px)**: The Markets table becomes a single-column list of cards, one
  instrument per card, leading with identity, price, change, and freshness; other columns
  are reachable without horizontal page scrolling. Filters and sort open in a sheet rather
  than occupying the list. On detail, the chart occupies a full-width panel with a
  legible minimum height, range selection is a row of touch targets, and zoom and pan work
  by pinch and drag with equivalent buttons for anyone not using touch. Automated
  acceptance at 360x800 covers browsing, filtering, opening an instrument, changing range,
  and toggling an overlay.
- **Tablet (768-1023 CSS px)**: The table keeps its columns with filters in a persistent
  sidebar or header row; the detail view places identity and context beside the chart where
  width allows. Automated acceptance at 768x1024 covers the same journey plus orientation
  change with the chart's range and zoom preserved.
- **Desktop (1024+ CSS px)**: The full column set, persistent filters, and a larger chart
  with context panels beside it. Automated acceptance at 1440x900 covers the same journey
  plus keyboard-only operation.
- **Input and accessibility**: Every control is reachable and operable by keyboard with a
  visible focus indicator; nothing is available only on hover; touch targets meet a minimum
  size; text meets the contrast ratio required for its size in system, light, and dark
  themes; the layout tolerates 320 CSS pixels without horizontal page scrolling or clipped
  controls; filters, sort, range, zoom, and scroll position survive orientation change and
  theme change.

### Live Update Behavior *(mandatory for client-visible data; otherwise state N/A)*

- **Snapshot and events**: Initial state loads over request/response. Subsequent committed
  changes arrive as the existing shared-scope versioned events for daily bars, import runs,
  import items, and quality findings. This feature introduces no private-scope event and no
  new event type unless a committed change it makes visible has none, in which case the
  plan must define it as a versioned shared event written in the same transaction as the
  change.
- **Reliability**: Events are delivered only to authenticated, active users on the existing
  authorized stream. Ordering is by event identifier; a client resumes with the last
  identifier it applied; repeated identifiers are ignored; the client keeps a bounded
  record of what it has applied; and the view reports stale and offline states when it is
  not current.
- **Test evidence**: Automated scenarios for a change applied in place without losing view
  state, a duplicate event applied once, a reconnection replaying only missed changes, a
  change for an instrument outside the current filters not disturbing the view, and a
  second user's private events never reaching this view.

### Identity, Ownership, and Permissions *(mandatory for user/account data; otherwise state N/A)*

- **Bootstrap and invitations**: N/A. This feature creates no accounts and changes no
  membership; it relies entirely on the owner and invitation model already delivered.
- **Ownership and authorization**: All instrument, price, corporate-action, and
  quality-finding data is shared reference data readable by every authenticated, active
  user. This feature stores no private record on the server: the only per-person state is
  the optional-column selection, which is a display preference held in browser storage on
  the device that made it. No data in this feature is readable by an unauthenticated
  caller, and owner administration grants no additional access here.
- **Security evidence**: Automated proof that every read requires an active session, that
  an unknown and an unauthorized instrument identifier produce identical responses, that
  no request in this feature carries or returns another person's data, and that a
  deactivated or revoked session loses access to these views immediately.

### PWA and Notification Behavior *(mandatory when applicable; otherwise state N/A)*

N/A. Installability and notifications are separate roadmap items and this feature neither
requires nor changes them.

### Key Entities *(include if feature involves data)*

- **Instrument listing row**: One instrument as it appears in the universe list — its
  exchange-qualified identity, sector, country, lifecycle status, latest close and change,
  descriptive return and volatility statistics, and freshness. Derived from stored data
  rather than stored in its own right unless the plan proves otherwise.
- **Price series window**: The stored daily bars for one instrument over a bounded range,
  with the sessions that are absent from it identified as absent.
- **Overlay series**: A derived line computed from a price series window for display only,
  defined by its window length, and undefined where insufficient prior sessions exist.
- **Chart annotation**: A corporate action or quality finding anchored to the session it
  affects, carrying enough detail to explain a discontinuity.
- **Column preference**: One person's chosen set of optional list columns. A display
  preference held on the device, not a server record.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A signed-in person can go from opening the application to reading one
  instrument's price history in under 30 seconds without prior instruction.
- **SC-002**: The universe list returns its first page within two seconds on a typical
  connection for the full curated universe, and remains within that bound when sorted by
  any supported column.
- **SC-003**: An instrument's full stored daily history renders and remains interactive —
  zoom and pan respond without visible stalling — for the longest history in the universe.
- **SC-004**: Every instrument in the curated universe is reachable through search or
  filtering by name, ticker, or ISIN.
- **SC-005**: No displayed price series contains an observation that is not in stored data:
  an automated check over the full universe finds zero interpolated, forward-filled, or
  invented sessions.
- **SC-006**: Every recorded corporate action and open quality finding within a displayed
  range is visible in that view, verified across the full universe.
- **SC-007**: All browsing and chart journeys complete at 360x800, 768x1024, and 1440x900,
  and the interface tolerates 320 CSS pixels without horizontal page scrolling or clipped
  controls, in system, light, and dark themes.
- **SC-008**: Every interactive control is operable by keyboard alone and by touch alone,
  with no control reachable only by hovering.
- **SC-009**: A committed change reaches an open view within five seconds without the
  person reloading, and without losing filters, sort, range, zoom, overlays, or scroll
  position.
- **SC-010**: An unauthenticated request for any data in this feature is refused, and a
  request naming another instrument identifier is indistinguishable from one naming an
  identifier that does not exist.

## Assumptions

- The curated universe, its stored daily history, corporate actions, and quality findings
  are those already delivered by feature 002. This feature reads them and adds no provider
  integration, no import control, and no new market data.
- Authentication, session lifecycle, the protected-by-default route boundary, and the
  authorized event stream are those already delivered by feature 004. This feature adds no
  authentication behavior.
- The 20-session and 90-session returns and the volatility measure in FR-007 are
  **descriptive statistics for display only**, computed from stored sessions at read time.
  They are deliberately not the versioned, leakage-tested feature snapshots that the
  reusable feature engine will own in a later milestone, and nothing in this feature may
  become the definition those depend on. If the engine later computes them differently,
  this feature adopts the engine's definition rather than the reverse.
- Ranges are expressed in stored sessions rather than calendar days, because the product
  stores exchange sessions and a calendar window would silently include days no exchange
  was open.
- Prices are displayed in each instrument's own listing currency. No currency conversion,
  cross-currency comparison, or base-currency normalisation happens in this feature; that
  depends on FX history that does not yet exist.
- Relative strength, signals, scores, confidence, watchlists, and backtest context named in
  the product vision's Instrument Detail description are out of scope here and belong to
  the milestones that own strategies and user data.
- Hourly data remains deferred; every chart in this feature is daily.
- The charting approach is a plan-phase decision. The product vision requires that
  candlesticks, overlays, zoom and pan, and large series be evaluated for maintenance,
  licence, Vue and TypeScript support, responsiveness, dark mode, performance, and
  avoidable lock-in before a library is chosen, and the plan must record that evaluation.
