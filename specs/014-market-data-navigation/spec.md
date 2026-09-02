# Feature Specification: Market Data Navigation, Sector Data, and Continuous Listing

**Feature Branch**: `014-market-data-navigation`

**Created**: 2026-09-02

**Status**: planned
<!-- Market Lens spec lifecycle: planned → in-progress → in-review → shipped. -->

**Input**: User description: "Market Data view structure, sector data, and continuous listing
navigation. Three connected problems on the Market Data screen: the Operations 'Recent imports'
report sits directly below the instrument table and serves neither the researcher nor the
operator; the Sector column and filter are empty for every instrument; and the listing pages
with a 'Load more' button where the reader wants automatic loading on scroll plus a total
count and a sense of position."

## User Scenarios & Testing *(mandatory)*

Today the Market Data screen is two unrelated things stacked vertically. Above the fold it is a
research tool — a hundred instruments with prices, changes, returns, volatility and freshness.
Below it, after roughly two thousand pixels of table on a phone, it becomes a maintenance
console reporting which import ran and how many bars it accepted. A person reading the market
scrolls past the maintenance console to reach the end of nothing in particular; a person
checking whether last night's import succeeded has to scroll the entire universe to find out.
One of the columns in the research half, Sector, is blank in every row, and the filter above it
offers a choice that can only ever return an empty result.

### User Story 1 - Operational state has its own place (Priority: P1)

A person responsible for the deployment wants to know whether the data is current: did last
night's import run, did it accept what it fetched, are there open data-quality findings, has
the feature engine computed since the last import. They open a place dedicated to that and see
it immediately, without scrolling through the instrument universe. A person doing research
opens Market Data and sees only the market, with a compact indication of how current the data
is and a way to reach the operational detail when they want it.

**Why this priority**: It costs nothing in data and fixes the structural mistake underneath the
other two stories — the screen currently cannot be improved as a research tool while it is also
a maintenance console. It also gives the feature engine's runs their first interface: today the
engine can fail in production and no screen says so.

**Independent Test**: Sign in, open the operational view directly, and confirm every current
import run, item outcome, finding and feature run is visible without visiting Market Data; then
open Market Data and confirm the instrument table is the whole page apart from a compact
freshness summary that links to the operational view.

**Acceptance Scenarios**:

1. **Given** an authenticated person on any screen, **When** they navigate to the operational
   view, **Then** they see the most recent import runs with their status and counts, the
   per-instrument item outcomes for a selected run, open data-quality findings, and the most
   recent feature-engine runs with their kind, status, instrument count and value count.
2. **Given** an authenticated person on Market Data, **When** the page has loaded, **Then** the
   only content below the instrument table is the end of the table, and the freshness of the
   data is stated once, compactly, near the top of the page.
3. **Given** a person reading the compact freshness summary on Market Data, **When** they
   activate it, **Then** they arrive at the operational view with the same information expanded.
4. **Given** an import fails, **When** the person opens the operational view, **Then** the
   failure, its sanitized reason and the affected instruments are visible without any further
   navigation.
5. **Given** the feature engine's most recent run failed or ended partial, **When** the person
   opens the operational view, **Then** that run's status and the count of failed instruments
   are visible, because a silent engine failure currently leaves the displayed statistics stale
   with nothing to say so.

---

### User Story 2 - The universe reads as one continuous list (Priority: P2)

A person browsing the curated universe scrolls, and the next rows arrive without being asked
for. They can see how far into the result set they are and how large it is, so that "I have
seen enough" and "there is nothing more" are distinguishable. Somebody navigating by keyboard
or with a screen reader is not trapped in a list that grows forever with no announced end.

**Why this priority**: This is the interaction a researcher performs every session, and the
current button interrupts it once per fifty rows. It ranks below the structural fix because it
is a refinement of something that already works, where the Operations split repairs something
that is actively wrong.

**Independent Test**: Load Market Data with a filter that matches more rows than one page,
scroll to the bottom of the loaded rows without touching any control, and confirm the next page
appears, the stated position advances, and the stated total is correct against the same filter.

**Acceptance Scenarios**:

1. **Given** a result set larger than one page, **When** the reader scrolls near the end of the
   loaded rows, **Then** the next page loads without any action, and the rows already read stay
   exactly where they were on screen.
2. **Given** a result set of any size, **When** the first page has loaded, **Then** the page
   states how many rows are currently shown and how many match the filter in total.
3. **Given** the reader has reached the last row, **When** no further rows exist, **Then** the
   page says so explicitly rather than continuing to suggest that more may arrive.
4. **Given** a person navigating by keyboard only, **When** they reach the end of the loaded
   rows, **Then** they can load the next page with a focusable control and are told, through an
   assistive-technology announcement, how many rows arrived and how many remain.
5. **Given** the reader changes a filter, a search term or the sort, **When** the new result set
   loads, **Then** the list returns to the first page, the stated total reflects the new filter,
   and no rows from the previous result set remain visible.
6. **Given** a page fails to load while scrolling, **When** the failure occurs, **Then** the
   rows already read remain, the failure is stated, and a control offers to retry — the list is
   never silently truncated.

---

### User Story 3 - Sector is real information (Priority: P3)

A person filtering the universe by sector gets a working filter over real classifications: every
instrument states its sector, and choosing one returns exactly the instruments in it. What they
must not see is what they see today — a column of blanks and a filter that returns nothing.

**Why this priority**: It affects one column and one filter rather than the shape of the page,
but leaving it as it stands is worse than resolving it either way: an empty column reads as
broken data, and a filter that cannot match anything wastes the reader's attention.

**Independent Test**: Open Market Data and confirm that every instrument states a sector or
states that it is unclassified, and that filtering by any offered sector returns exactly the
instruments classified in it.

**Acceptance Scenarios**:

1. **Given** sector data exists for the curated universe, **When** the listing is displayed,
   **Then** every instrument states its sector, and no row shows a blank where a sector is
   expected.
2. **Given** sector data exists, **When** the reader filters by a sector, **Then** the result
   contains exactly the instruments classified in that sector and the stated total reflects it.
3. **Given** an instrument whose sector is genuinely unknown, **When** its row is displayed,
   **Then** it states that the classification is unknown rather than showing an empty cell,
   and it is reachable through a filter choice that means "unclassified".
4. **Given** a fresh installation, **When** the database is migrated and no import has run,
   **Then** every instrument in the curated universe already carries its classification,
   because the classification is reference data rather than something an import supplies.
5. **Given** a reader inspecting where a classification came from, **When** they read an
   instrument's sector, **Then** the source and the date it was last reviewed are available to
   them rather than the classification being presented as unattributed fact.

---

### Edge Cases

- A filter matching zero instruments: the page states the total as zero and explains the empty
  result rather than showing an empty table with a spinner that never resolves.
- A filter matching exactly one page: no further loading is attempted, and the end of the list
  is stated on first render.
- A live update arrives for a row the reader has already scrolled past: the row's values update
  in place; it is not moved or removed under the reader, because content shifting beneath a
  reader's position is both disorienting and, for a screen-reader user, disabling.
- A live update makes a loaded row no longer match the current filter, or adds an instrument
  that would now match: the loaded rows are left as they are, the stated total is updated, and
  the discrepancy is stated with an offer to refresh — the reader chooses when their view
  changes shape.
- The reader scrolls very fast through several pages: page requests are coalesced so that the
  screen never issues more requests than pages actually needed, and the rows arrive in order.
- The reader returns to Market Data from an instrument's detail page: the filters, sort and
  scroll position they left are still in effect, and the pages they had loaded are restored or
  reloaded without losing their place.
- The connection drops mid-scroll: loading stops, the state is stated, and scrolling resumes
  loading when the connection returns.
- An operational view with no import runs at all — a fresh installation: it explains that no
  import has run and how to start one, rather than rendering an empty table.
- The feature engine has never run: the operational view states that rather than implying the
  statistics on Market Data are current.

## Requirements *(mandatory)*

### Functional Requirements

**Structure and operational reporting**

- **FR-001**: The system MUST present operational reporting — import runs, per-instrument import
  outcomes, data-quality findings, and feature-engine runs — on a screen of its own, reachable
  from the primary navigation.
- **FR-002**: The Market Data screen MUST NOT contain the operational report. It MUST retain a
  compact statement of how current the data is, which MUST lead to the operational screen.
- **FR-003**: The operational screen MUST show, for the most recent feature-engine runs, the
  kind of run, its status, when it started and finished, how many instruments it covered, how
  many values it wrote, and how many instruments failed.
- **FR-004**: The operational screen MUST state, for each import run, its status and its
  processed, accepted, rejected and flagged counts, and MUST make the per-instrument outcomes
  of a selected run reachable.
- **FR-005**: Every error the operational screen displays MUST be a sanitized, safe message,
  never a raw provider response, and MUST NOT include any credential or token.
- **FR-006**: The operational screen MUST be reachable by direct URL and MUST require an
  authenticated session, refusing anonymous access the way every other authenticated screen does.

**Continuous listing**

- **FR-007**: The listing MUST load the next page automatically when the reader approaches the
  end of the loaded rows, without requiring an action.
- **FR-008**: The listing MUST also offer a focusable control that loads the next page, so that
  a person who cannot or does not scroll — keyboard, screen reader, reduced motion — has an
  equivalent way to continue.
- **FR-009**: The listing MUST state how many rows are currently loaded and how many rows match
  the current filter in total, and MUST update both as pages load.
- **FR-010**: The listing MUST state explicitly when the end of the result set has been reached.
- **FR-011**: The total MUST be computed over the whole filtered result set, not over the loaded
  pages, and MUST be recomputed whenever the filter, search term or sort changes.
- **FR-012**: Loading a further page MUST NOT move, duplicate or skip any row already loaded.
- **FR-013**: Changing a filter, search term or sort MUST return the listing to the first page
  and discard previously loaded rows.
- **FR-014**: The listing MUST issue at most one page request at a time and MUST coalesce
  scroll-triggered requests, so that fast scrolling cannot multiply requests.
- **FR-015**: A failed page load MUST leave the loaded rows intact, state the failure, and offer
  a retry.
- **FR-016**: Assistive technology MUST be informed when rows arrive — how many were added and
  what the new position and total are — through a polite announcement that does not steal focus.
- **FR-017**: Returning to the listing from an instrument's detail view MUST restore the
  reader's filters, search term and sort. Restoring the pages they had loaded and their offset
  within them is **out of scope**: opening an instrument is a full page load in this
  application, so no client state survives it, and changing that is a separate decision about
  how the application navigates (research R-004, amended during implementation).

**Live updates within a loaded listing**

- **FR-018**: A live change to an instrument already loaded MUST update that row in place
  without moving it and without changing the reader's scroll position.
- **FR-019**: When live changes mean the loaded rows no longer reflect the filtered result set —
  a row that no longer matches, or a new instrument that now does — the system MUST update the
  stated total and offer the reader a way to refresh, and MUST NOT reorder or remove loaded rows
  on its own.

**Sector**

- **FR-020**: The system MUST NOT present a filter whose every possible choice returns an empty
  result. Sector data is therefore carried for the curated universe (decided 2026-09-02; see
  *Resolved decisions*), and the sector column, filter and sort remain.
- **FR-021**: Every instrument in the curated universe MUST have either a stated sector or an
  explicit "unclassified" state. The filter MUST offer only values that exist in the data,
  including "unclassified" when any instrument holds it.
- **FR-022**: Sector classification MUST arrive through the same reviewed, ordered migration
  mechanism as every other schema and reference-data change. It MUST NOT be entered by hand
  into the deployed database, and no classification may be introduced by a provider import.
- **FR-023**: Sector values MUST come from a fixed vocabulary declared in the migration, not
  from free text. Adding a sector to the vocabulary MUST be a reviewed change, so that the
  filter's choices are a known, closed set rather than whatever happens to be stored.
- **FR-024**: Each classification MUST record where it came from and when it was last reviewed,
  so that a stale classification is visible as stale rather than presented as current fact.
- **FR-025**: Adding an instrument to the curated universe MUST classify it in the same
  migration that adds it, or record it as unclassified. It MUST NOT be possible for an
  instrument to enter the universe with no classification state at all, because that is the
  condition this feature exists to end.
- **FR-026**: The interface MUST distinguish "unclassified" from a missing value. An
  unclassified instrument states that it is unclassified; no row shows an empty sector cell.

### Test-First Proof *(mandatory)*

- **Initial failing test**: `TestTheMarketsListingReportsItsTotalAndPosition` — an automated
  test asserting that a listing request over a filter matching more rows than one page reports
  the total number of matching instruments alongside the page of rows. It must fail because the
  listing currently returns rows and a cursor only, and reports no total at all.
- **Expected red reason**: The assertion on the reported total fails with the total absent or
  zero while the filtered result set is known to contain more instruments than the page holds.
  A compilation failure caused by the field not existing yet does not satisfy this requirement:
  the test must be written so that it compiles and fails on the value.
- **Green evidence**: The Go listing suite (`server/internal/instruments`), the API contract
  suite (`server/internal/api`), the Vitest suites for the Markets view and the listing service,
  and the Playwright journeys across the mobile, tablet and desktop projects.
- **Database migration proof**: An ordered migration introduces the classification vocabulary
  and the classification of every instrument in the curated universe. A migration test MUST
  prove that a clean installation and an upgrade from the current schema both end with every
  curated instrument carrying a classification state and a recorded source, with no manual
  step, and that the vocabulary constrains what may be stored.

### Responsive UI Behavior *(mandatory for user-facing features; otherwise state N/A)*

- **Mobile (320-767 CSS px)**: The instrument listing remains the stacked-card presentation it
  uses today. New pages append below the last card. The position and total are stated once, in
  a compact form that does not consume a full row of vertical space, and remain reachable while
  scrolling rather than only at the top of the page. The automatic loading threshold accounts
  for momentum scrolling, so a fast flick does not reach an empty region before rows arrive. The
  operational screen presents its runs as stacked cards with the same treatment. At 360x800 an
  automated scenario scrolls the listing, asserts that a further page arrived without a tap,
  that the stated position advanced, and that no horizontal scrolling was introduced. At the 320
  CSS pixel floor, the position and total statement must not clip or wrap into an unreadable
  form.
- **Tablet (768-1023 CSS px)**: The listing presents as a table with the column set the reader
  has chosen. The position and total sit with the table's controls. At 768x1024 an automated
  scenario asserts continuous loading, the stated total, and that the table's horizontal
  scrolling contains itself rather than moving the page.
- **Desktop (1024+ CSS px)**: As tablet, with the full column set available. At 1440x900 an
  automated scenario asserts continuous loading over at least three pages, that no row repeats
  or is skipped across the whole scroll, and that the end of the list is stated on arrival.
- **Input and accessibility**: The focusable next-page control is reachable by keyboard at the
  end of the loaded rows and is never hidden by the automatic loading. Arriving rows are
  announced politely, with the count added and the new position, and focus is never moved by an
  arrival. No interaction depends on hover. Rotating the device preserves the loaded pages, the
  reader's position, and the filters. Browser zoom to 200% preserves every control and the
  position statement. The reader's place in the list survives a return from an instrument detail
  view.

### Live Update Behavior *(mandatory for client-visible data; otherwise state N/A)*

- **Snapshot and events**: The listing's first page and its total arrive over the existing
  authorized read. Changes continue to arrive as the versioned, named events the system already
  publishes for bars, imports, corporate actions, quality findings and feature values. This
  feature adds no new event type; it defines what the client does with the existing ones once
  more than one page is loaded.
- **Reliability**: Events remain shared-scope, authorized, ordered and resumable from the last
  delivered identifier, with duplicates applied exactly once, exactly as they are today. A
  reader who has loaded several pages MUST NOT receive a different event stream from one who has
  loaded a single page: the difference is only in which rows the client applies a change to.
- **Test evidence**: Automated scenarios MUST cover a change to a row on a page other than the
  first; a burst of changes arriving during automatic loading, coalesced into a bounded number
  of requests; a change to an instrument absent from the loaded rows, causing no request; a
  reconnection with pages already loaded, resuming from the last delivered event without
  refetching the pages; and a change that makes a loaded row no longer match the filter,
  verifying that the row is not removed and the stated total is corrected.

### Identity, Ownership, and Permissions *(mandatory for user/account data; otherwise state N/A)*

- **Bootstrap and invitations**: Unchanged by this feature.
- **Ownership and authorization**: The operational screen shows deployment-wide facts — imports,
  findings, engine runs — which are shared, not per-user, and are already visible to every
  authenticated user on the current Market Data screen. Moving them to a screen of their own
  MUST NOT widen that audience: the screen requires an authenticated session and refuses
  anonymous access, and no per-user data appears on it.
- **Security evidence**: An automated test MUST prove that the operational screen's data is
  refused to an unauthenticated request and to a deactivated account, and that it exposes no
  credential, token or raw provider error.

### PWA and Notification Behavior *(mandatory when applicable; otherwise state N/A)*

N/A. This feature adds no installability or notification behavior.

### Key Entities *(include if feature involves data)*

- **Instrument classification**: The sector, and optionally the industry, an instrument belongs
  to, together with where the classification came from and when it was last reviewed. Only
  meaningful if sector data is carried.
- **Listing result summary**: The number of instruments matching a filter, distinct from the
  number currently loaded. It is derived at read time, not stored.
- **Operational run**: An import run or a feature-engine run, with its kind, status, timing and
  counts. Both already exist; this feature gives them a place to be read.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A person can determine whether last night's data arrived, and whether the derived
  statistics were recomputed after it, without scrolling past any instrument data.
- **SC-002**: The Market Data screen contains market data only: no operational report appears on
  it at any viewport width.
- **SC-003**: A reader can move from the first instrument to the hundredth without activating
  any control.
- **SC-004**: At every point in that scroll the screen states how many instruments are shown and
  how many match the filter, and the stated total equals the number of instruments the same
  filter returns when counted directly.
- **SC-005**: Scrolling the entire curated universe neither repeats nor omits an instrument, at
  every supported viewport.
- **SC-006**: A person using only a keyboard, and a person using a screen reader, can reach the
  last instrument in the result set and are told when they have.
- **SC-007**: The first page of the listing, including its total, is presented within the same
  bound the listing already meets today, and loading each further page stays within that bound.
- **SC-008**: No filter offered in the interface can return an empty result for every possible
  choice.
- **SC-009**: Every instrument in the curated universe states a sector or states that it is
  unclassified; no instrument shows a blank, on a fresh installation and on an upgrade alike.
- **SC-010**: A live change to an instrument on a page other than the first updates that
  instrument's row without moving the reader's position and without reloading the pages already
  read.

## Assumptions

- The operational screen is visible to any authenticated user, preserving today's audience for
  the same information rather than narrowing it to the owner. Narrowing it would be a separate,
  deliberate decision about roles.
- The feature engine's runs belong on the operational screen. They are operational facts with no
  interface at all today, and their absence means a failed computation is invisible while the
  statistics it feeds go stale on screen.
- The exact total is worth a separate count over the filtered result set. Over a curated universe
  of this size the cost is negligible; the assumption would need revisiting if the universe grew
  by orders of magnitude, at which point an approximate or capped total ("999+") becomes the
  honest presentation.
- Keyset pagination is retained. The reader gains automatic loading and a stated total, not
  page-number navigation: numbered pages over a changing result set reintroduce exactly the
  repeated and skipped rows that keyset pagination exists to prevent.
- The reader's filters, search term and sort are restored when returning from an instrument
  detail view. The loaded pages and scroll offset are not: this application opens an instrument
  with a full page load, which discards client state (research R-004, amended).
- Live updates continue to use the event types already published. This feature defines client
  behavior over multiple loaded pages and adds no new event.
- The deployment's current market-data subscription excludes fundamental data, so sector cannot
  be fetched from the provider without a plan change. Curated reference data was chosen instead
  (see *Resolved decisions*), which means classification is deliberately independent of the
  provider and of the subscription remaining active.
- The classification is the project's own editorial judgement about each company, recorded with
  its source, using conventional sector names. It is not a reproduction of a licensed
  classification's assignments, and the specification does not claim conformance to one.
- Around a hundred hand-curated companies is a size a person can classify once and review
  occasionally. The assumption fails if the universe grows to a size nobody will re-review, at
  which point the provider question returns on its own merits.
- Industry, the finer classification beneath sector, stays out of scope. The interface offers no
  industry filter today and the same argument would have to be made again for it.
- Unrelated but recorded because it threatens the data this screen displays: the provider
  subscription is cancelled and the key expires 2026-09-29. Nothing in this feature depends on
  it, but every import after that date does.

## Resolved decisions

### Sector classification comes from curated reference data (decided 2026-09-02)

Sector is null for all 100 instruments today: the universe seed migration never populated it,
the code path that would write it has no caller, and the deployment's market-data plan excludes
fundamental data, so the provider cannot supply it. Three resolutions were weighed.

| Option | Answer | Implications |
|--------|--------|--------------|
| **A (chosen)** | Carry sector as curated reference data, introduced and maintained by ordered migration | No provider dependency and no new cost. One hundred classifications are reviewed once and change rarely. Staleness is possible and must therefore be visible, which FR-024 requires. Adding an instrument to the universe means classifying it in the same migration that adds it, which FR-025 requires. |
| B | Upgrade the market-data subscription to a plan including fundamental data | Classification stays current without human review and extends to any future universe. It costs roughly five times the current subscription for this one field today, adds a provider surface to build and test, and makes a displayed column dependent on a paid plan remaining active. |
| C | Remove the sector column, filter and sort | Smallest change and no false promise, but the product's own direction assumes slicing the universe by sector, so it would return as work rather than being settled. |

**Chosen: A.** The universe is hand-curated at a size a person can classify, the classification
changes on the order of years, and making a visible column depend on a subscription — one that
is currently cancelled — would trade a small cost for a standing fragility. The consequences
Option A carries are written into the requirements rather than left implicit: a fixed vocabulary
(FR-023), recorded provenance and review date (FR-024), classification at the moment an
instrument joins the universe (FR-025), and "unclassified" as a stated value rather than an
empty cell (FR-021, FR-026).
