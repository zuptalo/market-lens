# Research: Market Data Navigation, Sector Data, and Continuous Listing

Phase 0 decisions for `specs/014-market-data-navigation/spec.md`. Each records what was
chosen, why, and what was rejected.

---

## R-001: How the total is counted alongside a keyset page

**Decision**: A separate `count(*)` over the same filter, executed **only when the request
carries no cursor**, and returned in the same response as the first page. Subsequent pages
report no total; the client keeps the one it has.

**Rationale**: Keyset pagination's whole value is that a page request stops as soon as it has
`limit + 1` rows — the index scan terminates early. A window function (`count(*) OVER ()`) in
the same statement would hand back the total in one round trip, but it forces the planner to
materialise every matching row before the limit applies, turning every page request into a full
scan of the filtered set. Paying that once per filter change, rather than once per page, keeps
the page request the cheap thing it was designed to be.

Counting on cursor-less requests also lands the total exactly where the client already needs it:
the live-update row refresh (`MarketsView`'s `listingRefresh`) already re-reads the loaded rows
with a cursor-less request, so the corrected total that FR-019 requires arrives through a path
that exists rather than a new one.

**Alternatives considered**:

- *`count(*) OVER ()` in the page query* — rejected for the early-termination loss above. It
  would be invisible at 100 instruments and expensive at 100,000, and the listing query is the
  one place in this codebase already written for a universe larger than today's.
- *A separate count endpoint* — rejected as a second round trip and a second contract for one
  number that belongs with the page it describes.
- *An approximate count from table statistics* — rejected because the count is filtered, and
  because "about 340" is a worse answer than "342" at this scale. Revisit only if the universe
  grows enough that an exact filtered count stops being free, at which point a capped total
  ("999+") is the honest presentation.

---

## R-002: How the next page loads on scroll

**Decision**: An `IntersectionObserver` watching a sentinel element rendered after the last row.
When the sentinel enters the viewport (with a root margin of roughly one viewport height, so
rows arrive before the reader reaches blank space), the next page is requested. A focusable
"Load more" control stays rendered at the same position and does the same thing when activated.

**Rationale**: The observer costs nothing while idle, unlike a throttled scroll handler, and it
is the mechanism the platform provides for exactly this question. Keeping the button is not a
fallback for old browsers — it is the keyboard and screen-reader path (FR-008). An
infinite list with no focusable end is a well-known way to strand those readers, and hiding the
control once the observer works would recreate that.

**Alternatives considered**:

- *Scroll event with throttling* — rejected: more code, worse behaviour under momentum
  scrolling, and it runs on the main thread during the scroll it is measuring.
- *Virtual scrolling over the whole result set* — rejected as a much larger change that solves a
  problem this universe does not have. A hundred rows, or even a few thousand, render fine.
- *Replacing the button with the observer* — rejected: FR-008 exists because of it.

**Test seam**: `IntersectionObserver` does not exist in the Vitest DOM environment. The
component takes its observer from a small injectable factory so unit tests can trigger
intersection directly; Playwright exercises the real thing by scrolling.

---

## R-003: What happens to loaded rows when live changes alter the result set

**Decision**: Rows already loaded are **never moved, reordered or removed** by a live event.
A change to a loaded instrument updates that row in place. When an event means the loaded rows
no longer reflect the filtered set — a row that no longer matches, or a new instrument that now
would — the stated total is corrected and a refresh is offered. The reader chooses when the list
changes shape.

**Rationale**: Content moving under a reader is disorienting with a mouse and disabling with a
screen reader or a magnifier, where the reading position is a point in a rendered layout rather
than a scroll offset. It also breaks the keyset guarantee in the reader's favour rather than the
data's: a list assembled from several page requests is a snapshot of several moments, and
pretending otherwise by silently reordering would make "no row repeats or is skipped" false in
the only way the reader could actually notice.

**Alternatives considered**:

- *Remove non-matching rows immediately* — rejected for the reason above.
- *Reload the whole list on any membership change* — rejected: it discards the reader's position
  and, during an import, would fire continuously.
- *Say nothing and let the list drift* — rejected: the total would silently disagree with what
  is on screen, which is the class of quiet dishonesty this product avoids elsewhere.

---

## R-004: Restoring the reader's place after visiting an instrument

**Decision**: The listing's loaded pages, filters, sort and scroll offset are held in a
module-scoped cache in the listing service, keyed by the exact query string. Returning to
`/markets` with an identical query restores from that cache and then restores the scroll offset;
any difference in the query discards it and loads page one. The cache holds one entry, lives for
the browser session, and is dropped when the query changes.

**Rationale**: FR-017 asks for the reader's place back after clicking into an instrument, which
is the most common navigation on this screen. Re-requesting every loaded page to rebuild the list
would be several requests and a visible rebuild; keeping one query's worth of rows in memory is
cheap and exact. Keying on the whole query string means a changed filter can never restore rows
that do not belong to it.

**Alternatives considered**:

- *Keep the component alive with `<KeepAlive>`* — rejected: it preserves far more than the
  listing (timers, event subscriptions, the live connection) and makes the view's lifecycle
  harder to reason about than one explicit cache.
- *Serialise the loaded rows into session storage* — rejected: the rows are a snapshot that goes
  stale, and restoring a stale snapshot from a previous tab session is worse than reloading.
- *Restore only the filters, not the rows* — rejected as the status quo the requirement exists to
  improve.

---

## R-005: How sector classification is stored

**Decision**: A `sectors` reference table holding the vocabulary (`code`, `name`, `display_order`),
including an explicit `unclassified` member. `instruments.sector` becomes a **NOT NULL** foreign
key to it, defaulting to `unclassified`, alongside `sector_source` and `sector_reviewed_on`
recording where each classification came from and when it was last checked.

**Rationale**: Making the column NOT NULL against a vocabulary that contains `unclassified` makes
today's failure state — an instrument with no classification at all, invisible until somebody
looks at the screen — unrepresentable. That is what FR-025 asks for, and a database constraint
is a stronger guarantee than a convention. The provenance columns are what make FR-024's
"stale is visible as stale" possible at all: curated data with no review date is
indistinguishable from current data.

**Alternatives considered**:

- *Keep `sector` as free text* — rejected: the filter's choices would be whatever happened to be
  stored, and a typo would create a sector.
- *A separate `instrument_classifications` table* — rejected as premature. One classification per
  instrument, with no history requirement in the spec, is a column not a table. It becomes a
  table the day classifications need history.
- *A PostgreSQL enum* — rejected: adding a value to an enum is a schema change with awkward
  transactional behaviour, where adding a row to a reference table is a migration like any other.

---

## R-006: Where the sector filter's choices come from

**Decision**: The vocabulary is served from the database and rendered from the response. The
hardcoded client list is deleted.

**Rationale**: FR-021 requires the filter to offer only values that exist in the data, which a
constant in a Vue component cannot do. Research also turned up a defect the specification did not
know about: the current hardcoded list in `src/components/finance/InstrumentFilters.vue` contains
**both "Information Technology" and "Technology"** — two names for one idea, one of which could
never have matched anything even if the column had been populated. That list is not merely
disconnected from the data; it is internally inconsistent. Serving the vocabulary removes the
class of problem rather than correcting this instance of it.

**Alternatives considered**:

- *Derive the choices from `SELECT DISTINCT sector` over the instruments* — rejected: it makes
  the filter's options depend on which rows currently exist, so a sector with no members would
  vanish and reappear. The vocabulary is reference data; membership is not.
- *Correct the hardcoded list* — rejected: it would be correct until the next migration.

---

## R-007: The operational screen and the feature engine's runs

**Decision**: A new authenticated route, `/operations`, carrying what is on Market Data today
(import runs, their items, quality findings) plus a new read of the feature engine's runs. Market
Data keeps a compact freshness summary sourced from the import-status read it already performs,
limited to the single most recent run, which links to `/operations`.

**Rationale**: The engine has run in production since v0.9.0 with no interface at all: a failed
or partial computation leaves the Markets statistics stale with nothing on any screen to say so.
The operational screen is where that belongs, and it is the reason this story is P1 rather than
cosmetic. Reusing the existing import-status read for the Market Data badge avoids inventing an
endpoint for a single line of text.

**Alternatives considered**:

- *Put the engine's runs on a separate screen again* — rejected: two operational screens is the
  same mistake at a smaller scale.
- *A new summary endpoint for the freshness badge* — rejected as unnecessary; the existing
  endpoint takes a limit.

---

## R-008: Budgets

**Decision**: The first page including its total stays inside the two-second bound the listing
already meets (feature 005, SC), measured over the curated universe. Each subsequent page stays
inside the same bound. The count is asserted separately at fixture scale so that a regression in
the count query is attributable.

**Rationale**: SC-007 states the user-facing bound; this fixes the number the tests assert. The
existing `TestTheFirstPageOfTheUniverseStaysWithinItsBudget` already measures the page; extending
it to cover the total keeps one budget rather than two.

**Note on measurement**: the engine's budget tests learned this the hard way — `go test ./...`
runs the database-heavy packages concurrently, so a budget pinned to a quiet-machine measurement
fails on contention rather than on a defect. These budgets are stated as the user-facing bound,
not as the measured figure.

---

## R-009: Announcing arrivals to assistive technology

**Decision**: A polite live region states the position and total after each page arrives — "50
more instruments. Showing 100 of 342." — and states the end of the list when it is reached. Focus
is never moved by an arrival.

**Rationale**: FR-016. A polite region is announced at the next pause rather than interrupting,
which suits content that arrives while the reader is moving. Moving focus to new rows would
interrupt navigation and lose the reader's place, which is the failure this whole story is about.

**Alternatives considered**:

- *`aria-live="assertive"`* — rejected: it interrupts, and arriving rows are not urgent.
- *Announcing every row* — rejected: fifty announcements per page is noise, not information.
