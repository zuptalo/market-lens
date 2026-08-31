# Phase 0 Research: Instrument Exploration and Financial Charts

**Feature**: `005-instrument-exploration` | **Date**: 2026-08-31

Every unknown the plan's Technical Context raised is resolved here. Each decision names
what was chosen, why, and what was rejected, so a later reader can tell a considered choice
from an accident.

## R1. Charting approach for candlesticks, volume, overlays, zoom, and pan

**Decision**: Adopt a dedicated financial charting library — `lightweight-charts` — for the
candlestick, volume, and overlay panel only, wrapped behind one project component that
exposes a Market Lens shaped interface. Keep Chart.js, already sanctioned by the product
vision, for any general allocation or performance chart added later. Do not let the
library's types or objects escape the wrapper.

**Rationale**: The product vision requires this choice be evaluated for maintenance,
licence, Vue and TypeScript support, responsiveness, dark mode, performance, and avoidable
lock-in before adoption.

- *Purpose fit*: candlesticks, an aligned volume pane, line overlays, and interactive zoom
  and pan are its primary purpose rather than a plugin bolted onto a general chart library.
- *Performance*: canvas rendering handles roughly 2,500 daily bars per instrument — ten
  years of Nordic sessions — with room for the longer histories a wider universe implies.
  SC-003 requires interaction without visible stalling on the longest history in the
  universe, which rules out approaches that render one DOM node per bar.
- *Size*: small enough not to dominate the bundle of an application whose other pages are
  tables and forms.
- *Theming*: colours, grid, and text are configured at runtime, so system, light, and dark
  themes are a matter of passing our own tokens rather than shipping three stylesheets.
- *Interface*: an imperative, framework-agnostic API. That is a mild inconvenience in Vue
  and a real advantage for containment: it wraps cleanly in one component with explicit
  create, update, and dispose, and nothing about it leaks into the rest of the client.

**Verification required at adoption, not assumed here**: the exact published version, its
licence text, and whether the current release requires a visible attribution mark must be
read from the package being added and recorded in the plan's dependency note. The version
must be pinned. If the attribution requirement is unacceptable, the wrapper is the seam
that makes replacement a contained change rather than a rewrite — which is the point of
wrapping it.

**Alternatives considered**:

- *Chart.js with a financial plugin*: already present for general charts, but the financial
  plugin is a secondary community effort, and interactive zoom and pan over thousands of
  points is where it is weakest. Rejected because the chart is the feature.
- *ECharts*: capable and permissively licensed, with candlestick support and good
  performance, but a much larger general-purpose library whose surface area far exceeds
  this need. Rejected on size and on the breadth of API a future maintainer would face.
- *Highcharts Stock*: the strongest financial charting available and the wrong choice for a
  self-hosted personal product, because its licence is commercial. Rejected on licence.
- *uPlot*: extremely fast and tiny, but has no candlestick primitive; adopting it means
  writing and maintaining candlestick and volume rendering ourselves. Rejected as building
  a charting library in disguise.
- *D3 or hand-written canvas*: maximum control and the most work, with every interaction,
  axis, and accessibility affordance to build and test. Rejected for the same reason.

## R2. Where the derived list statistics come from

**Decision**: Compute latest close, prior-session change, 20-session return, 90-session
return, and volatility in SQL at query time using window functions over
`daily_price_bars`. Add no table, no materialised column, and no migration for them.

**Rationale**: The curated universe is 100 instruments. The widest statistic looks back 90
sessions, so a full listing reads on the order of 9,000 bar rows, and the existing
`daily_price_bars_instrument_session_idx` on `(instrument_id, session_date DESC)` serves
exactly that access pattern. Materialising values that cheap would add a schema, a refresh
path, and a staleness bug for no measurable gain.

It also keeps a promise the specification makes: these are display-only descriptive
statistics, deliberately not the versioned, leakage-tested feature snapshots a later
milestone will own. Giving them a table now would quietly make this feature the definition
that milestone has to live with.

**Guard**: an automated bound on the listing query against a full-universe fixture, so the
decision is revisited by a failing test rather than by a slow page, if the universe grows.

**Alternatives considered**: a materialised view refreshed after each import — rejected as
premature and a staleness risk; a stored statistics table written during import — rejected
because it pre-empts the feature engine's ownership of exactly these definitions.

## R3. Telling a real gap from a closed market

**Decision**: Determine missing sessions by left-joining `exchange_sessions` where
`status IN ('open','half_day')` against `daily_price_bars` for the instrument's exchange
over the requested window. A session the exchange was open for, with no stored bar, is a
gap. A date the exchange was closed is not.

**Rationale**: This is the difference between an honest chart and a misleading one, and the
data to make the distinction already exists: feature 002's `0005_nordic_calendars.sql`
populates a per-exchange session calendar with an explicit `open`, `half_day`, or `closed`
status. Without it we could only guess from weekends and would report every Nordic public
holiday as missing data, which would train a reader to ignore the warning entirely.

**Consequence for FR-013**: the count of missing sessions in view is a fact derived from
the calendar, not an estimate, and the chart interrupts the series at exactly those dates.

**Alternatives considered**: inferring gaps from weekday arithmetic — rejected as wrong on
every holiday; inferring from unusually large date deltas — rejected as a heuristic
presented as a fact.

## R4. Where a person's column choice lives

**Decision**: Store the optional-column selection in browser storage on the device, not in
a per-user table on the server. Amend the specification's Key Entities and Identity
sections to say per device rather than per account.

**Rationale**: FR-008 requires the choice persist across visits, which browser storage
satisfies. Putting it on the server would introduce this feature's only private-data table,
its only migration, its only private-scope authorization surface, and its only per-user SSE
concern — all for a display preference. That is a poor trade, and it makes the feature what
it should be: entirely read-only over shared reference data.

**Consequence**: the feature's Database Evolution is N/A for storage of its own, and its
cross-user isolation evidence reduces to proving that nothing here is readable without an
active session. A person using two devices sees their own choice on each, which is the
behaviour browser-stored preferences have everywhere else.

**Alternatives considered**: a `user_view_preferences` table — rejected above; no
persistence at all — rejected because FR-008 requires it and resetting a person's columns
on every visit is a small, repeated insult.

## R5. Making corporate actions visible live

**Decision**: Add a `corporate_action.changed.v1` shared-scope versioned event, written in
the same transaction that upserts the action during an import.

**Rationale**: Feature 002's import path already emits `daily_bar.changed.v1`,
`quality_finding.changed.v1`, `import_run.changed.v1`, and `import_item.changed.v1`, but
records corporate actions silently. FR-015 makes a recorded action client-visible for the
first time, and the constitution requires every client-visible committed change to publish
a versioned, authorized, transactionally coupled event. This is exactly the case the
specification anticipated when it said the plan must define such an event if one is
missing.

**Scope**: shared, like every other market-data event, carrying the instrument identifier
and ex-date in its payload so a client can decide whether the change concerns what it is
displaying. No schema change: `client_events` already accepts it.

**Alternatives considered**: relying on the bar event that usually accompanies an import —
rejected because "usually" is not a contract, and an action-only import would update
nothing on screen; polling for actions — rejected outright by the constitution.

## R6. Paginating a list sorted by a derived value

**Decision**: Keyset pagination on the composite `(sort value, instrument id)`, with the
sort applied in the database over the whole result set.

**Rationale**: FR-005 requires sorting to order the entire result set rather than the page
in front of the person, and to be stable for equal values. Many instruments share a sector
or a null statistic, so the instrument identifier is the tiebreaker that makes the order
total and the cursor unambiguous. Offset pagination would be simpler at 100 instruments and
would start skipping and repeating rows as soon as the universe grows or a bar arrives
mid-scroll; keyset does not, and matches the cursor pagination the existing endpoints
already use.

**Alternatives considered**: sorting in the client — rejected because it can only sort the
page it has; offset pagination — rejected for the drift above.

## R7. Ranges measured in sessions

**Decision**: Express every range and every lookback in stored exchange sessions, never in
calendar days.

**Rationale**: The stored unit is an exchange session. A "30 day" window silently includes
days no exchange was open, so the same label would mean a different number of observations
for Stockholm and Oslo in a week containing a Norwegian holiday. Sessions make the count
exact and comparable, and make "too few sessions to compute" a precise statement rather
than an approximation.

**Consequence**: range controls are labelled in sessions or in named periods whose session
count is stated, so a reader is never left inferring what a range contains.
