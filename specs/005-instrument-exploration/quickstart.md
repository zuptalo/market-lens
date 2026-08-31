# Quickstart: Instrument Exploration and Financial Charts

**Feature**: `005-instrument-exploration` | **Date**: 2026-08-31

How to run and verify this feature, and the evidence recorded while building it.

## Prerequisites

```sh
npm install
cp server/.env.example server/.env
make start
```

An integration database is required for the query tests:

```sh
export TEST_DATABASE_URL='postgres://market_lens:market_lens@127.0.0.1:5432/market_lens_test?sslmode=disable'
```

The universe and its history come from feature 002's import path. Without stored bars this
feature has nothing to display, which is itself worth seeing: an empty universe must render
an explanation rather than a blank table.

## What to verify by hand

1. Sign in, open **Markets**, and confirm every row is exchange-qualified, priced in its own
   currency, and states its freshness.
2. Sort by each supported column and page forward. Confirm the ordering holds *across*
   pages — a value on page two must not belong before a value on page one.
3. Filter to a sector with few members, then to one with none, and confirm the empty result
   explains itself and offers a way back.
4. Open an instrument. Confirm the candlestick and volume panes align, the range controls
   change what is drawn, and zoom and pan stay inside the stored coverage.
5. Enable a moving-average overlay and confirm it begins where it becomes computable rather
   than at the left edge, and that it breaks at a gap instead of spanning it.
6. Find an instrument with a recorded split and confirm the action is marked at its ex-date
   with its ratio readable **without hovering**.
7. Compare a session the exchange was closed against a session it was open with no stored
   bar. The first must not be reported as missing; the second must be.

## Automated verification

```sh
TEST_DATABASE_URL=... make verify
npm run test:e2e
docker build -t market-lens:local .
docker compose config --quiet
```

All four run green on this branch as of 2026-08-31:

| Command | Result |
| --- | --- |
| `make verify` | Go vet, gofmt, the full Go suite including PostgreSQL-backed integration tests, the production client build, and 140 Vitest tests across 24 files |
| `npm run test:e2e` | 174 Playwright tests across the mobile (360x800), tablet (768x1024) and desktop (1440x900) projects, plus explicit 320x800 checks |
| `docker build -t market-lens:local .` | Image builds; the client is compiled and served from the Go process |
| `docker compose config --quiet` | Valid |

## Red-green evidence

Each entry names the behavioral failure observed before implementation. A compile failure, a
broken fixture, an unavailable database, or an already-green test is not valid red evidence.

| Behavior | Observed red | Green |
| --- | --- | --- |
| Listing returns price, change and freshness (T013, the designated first red) | `the listing reported no latest session for an instrument with 300 stored bars`, `no prior-session change: absolute=<absent>`, `stored session count was 0, expected 300` | `TestListingReturnsLatestCloseChangeAndFreshness` |
| Requested sort orders the whole result set | `descending order is not the reverse of ascending: position 0 held ABB and LONG` | `TestListingOrdersTheWholeResultSetByTheRequestedSort` |
| Freshness from the exchange calendar | `an instrument with a bar for the most recent open session was reported ""` | `TestListingReportsFreshnessAgainstTheExchangeCalendar` |
| A statistic with too few sessions is absent | `the fixture instrument stored 0 sessions, expected 20` | `TestListingLeavesUncomputableStatisticsAbsentRatherThanZero` |
| Keyset paging repeats and skips nothing | `paging sorted by name returned 20 distinct instruments, expected 105` | `TestListingPagesWithoutRepeatingOrSkippingARow` |
| Sector filter is applied | `filtering by sector returned LONG, whose sector is "Industrials"` | `TestListingFiltersBySectorExchangeAndSearch` |
| Handler speaks the contract's vocabulary | `the handler did not pass the contract's parameters through: ListingFilter{...all zero...}`; `?sort=whatever was accepted with 200` | `TestListingEndpointAcceptsTheContractsQueryVocabulary` |
| Missing sessions come from the calendar | `reported 0 missing sessions, expected 3: [] vs [2026-04-30 2026-06-03 2026-06-04]` | `TestHistoryReportsOnlyOpenSessionsWithNoStoredBarAsMissing` |
| Window counted in sessions, coverage independent of it | `a ten-session window returned 300 bars`; `coverage reported 0 bars` | `TestHistoryCoverageIgnoresTheRequestedWindow`, `TestHistoryCountsTheWindowInSessionsNotCalendarDays` |
| History endpoint and its 404 equivalence | route removed and suite rerun: `history response = 404`, `?sessions=1 returned 404, expected 400`, and the contract test reported `the contract describes routes that do not exist: [GET /instruments/{}/history]` | `TestHistoryEndpointReturnsTheChartsPayload`, `TestHistoryEndpointAnswersUnknownAndUnauthorizedIdentically` |
| Corporate action publishes an event | `recording a corporate action published 0 events, expected exactly one`; `1 corporate actions are stored but 0 events were published` | `TestImportPublishesAnEventWhenItRecordsACorporateAction` |
| Client subscribes by event name | `not subscribed to daily_bar.changed.v1: expected [ 'open', 'message', 'error' ] to include 'daily_bar.changed.v1'` | `subscribes by event name, because that is what the server sends` |
| Absent statistic survives the wire as absent | mapper written with `?? 0`: `expected +0 to be null` | `fetchInstrumentListing` suite |
| Moving average breaks at a gap | naive window: `expected 104.5 to be null` at the first session after the gap | `movingAverage` suite |
| Chart interrupts rather than bridges | `no point marks the missing session 2026-05-25` | `PriceChart` suite |
| Table asks the server to reorder | `expected undefined to be truthy` (no sort emitted); `expected 119 to be less than 56` (rows re-sorted locally) | `InstrumentTable` suite |
| Range controls labelled in sessions, keyboard-operable | six of seven assertions failed against a day-labelled, click-only first version | `ChartRangeControls` suite |
| Column preference survives a visit | non-persisting first version: `expected [...defaults] to deeply equal [ 'country', 'return90' ]` | `instrumentColumns` suite |

Two implementations were written before their tests — the history handler and the chart
annotation panel. Rather than let that pass, the route was removed and the suite rerun; it
failed on all four history cases and the contract test additionally caught the endpoint being
documented but unrouted. The tests bite.

## Honesty matrix

Checked over every instrument in the fixture universe, not over one convenient example.

| Claim | Evidence |
| --- | --- |
| No interpolated, forward-filled, or invented session appears in any series | `TestNothingIsDrawnThatIsNotStored` — every returned session is verified to exist in `daily_price_bars` |
| A closed exchange day is never reported as a missing session | `TestNothingIsDrawnThatIsNotStored` checks each reported gap against `exchange_sessions.status`; the fixture deliberately contains a real Swedish holiday inside the charted range |
| An open session with no stored bar is always reported as missing | `TestHistoryReportsOnlyOpenSessionsWithNoStoredBarAsMissing`, compared against the same left join computed independently in the fixture |
| An overlay is undefined where prior sessions are insufficient, and breaks at a gap | `movingAverage` suite; the naive version was caught averaging across the gap |
| Every corporate action in a displayed range is visible in that view | `TestEveryRecordedActionAndFindingInRangeIsReturned`, universe-wide |
| Every open quality finding for a displayed instrument is listed | `TestEveryRecordedActionAndFindingInRangeIsReturned`; rendered by `ChartAnnotations` without hover |
| A derived statistic with too few sessions is absent, never zero | `TestNothingIsDrawnThatIsNotStored` rule 4; `fetchInstrumentListing` and `InstrumentTable` suites on the client |
| Prices are never compared or converted across currencies | `TestNothingIsDrawnThatIsNotStored` rule 5: every bar's currency equals its listing currency; the table states the currency on every price |
| Every instrument is reachable by name, ticker and ISIN (SC-004) | `TestListingFiltersBySectorExchangeAndSearch` |

## Responsive and accessibility evidence

`npm run test:e2e` — 174 passing across the mobile, tablet and desktop projects.

| Check | Evidence |
| --- | --- |
| Universe list readable, no horizontal page scrolling at 320/360/768/1440 | `tolerates 320 pixels without scrolling the page sideways`; `/markets fits and stays usable at <viewport>` for all four |
| Chart legible and interactive, longest history | `renders the chart and stays interactive on the longest history` — 2,500 sessions render and zoom/pan settle within budget (SC-003) |
| Range, zoom and overlay controls operable by touch | `ChartRangeControls` are real buttons at the minimum touch size; measured by the touch-target check at every viewport |
| The same controls operable by keyboard alone | `operates the chart controls by keyboard alone`; `sorts from the sheet on a small screen and from the headers on a large one` |
| Text meets AA contrast in system, light and dark | `<path> has named controls, visible focus and readable contrast` for both new views |
| Range and scroll survive orientation and theme change | `keeps the range and scroll position across an orientation change`; theme cycling in `e2e/market-data.spec.ts` |
| Nothing depends on hover | `nothing important is reachable only by hovering`; every annotation is also a list entry |

**Two gaps the responsive suite found rather than confirmed.** On a phone the table stacks
into cards and its header row is off screen, so there was no way to sort at all — sorting now
lives in the filters sheet. And the detail view's back link was about sixteen pixels tall,
below a comfortable touch target; it now meets the minimum.

The touch-target check exempts the charting library's own attribution anchor: its size and
placement are the library's, the licence requires the link, and altering it is not ours to do.

## Live delivery evidence

| Scenario | Evidence |
| --- | --- |
| A committed bar updates its row without resetting filters, sort, or page | `applyLiveChange` refreshes only the named row; `MarketsView` suite |
| A committed bar updates an open chart without losing range, zoom, or overlays | `keeps the chosen range and overlays when a change arrives for this instrument`; `applies a live change without losing the chosen range or overlays` (e2e) |
| A duplicate event is applied exactly once | `applies a repeated event identifier exactly once`, both in the service and through the view's real subscription |
| A reconnection replays only what was missed | `refreshes duplicate-safely, reconnects from the last event ID, and exposes connection state` |
| A change outside the current filters does not disturb the view | `ignores a change for an instrument that is not the one on screen` |
| A recorded corporate action publishes `corporate_action.changed.v1` in its own transaction | `TestImportPublishesAnEventWhenItRecordsACorporateAction`, `TestACorporateActionEventIsWrittenInTheSameTransactionAsTheAction` |
| An unauthenticated caller receives no event and no data | `TestListingEndpointRequiresAnActiveSession`, `TestHistoryEndpointRequiresAnActiveSession`, and the existing event isolation suite |

**A defect that had already shipped.** The client subscribed to the stream's generic
`message` event, but the server writes every event with a name, and a browser `EventSource`
routes a named event only to a listener registered for that name. Nothing was ever
delivered, and the failure was silent: the connection opened, the state read "connected", and
no update arrived. The suite could not see it because the test double dispatched under the
same name the production code subscribed to — the test and the bug agreed with each other.
The double now dispatches by the event's own name, as the server does.

## Production verification

Released as `v0.6.0` and rolled out by Keel on 2026-08-31.

| Check | Result |
| --- | --- |
| `GET /api/v1/health` | `{"service":"market-lens","status":"ok","version":"0.6.0"}` |
| `GET /api/v1/ready` | `{"status":"ready"}` |
| Rollout | `deployment "market-lens" successfully rolled out`, one pod running, no restarts |
| `GET /api/v1/instruments` unauthenticated | `401 authentication_required` |
| `GET /api/v1/instruments/{id}/history` unauthenticated | `401 authentication_required` — identical body to an unknown identifier |
| `/markets` and `/markets/{id}` as a browser navigation | `302` to sign-in, not a JSON error |

## Dependency note

Recorded 2026-08-31 at the moment the dependency was added, read from the installed
package rather than assumed.

| | |
| --- | --- |
| Package | `lightweight-charts` |
| Version | `5.2.1`, pinned exactly in `package.json` (no `^`, no `~`) |
| Licence | Apache-2.0 (`node_modules/lightweight-charts/LICENSE`) |
| Publisher | TradingView — <https://www.tradingview.com/lightweight-charts/> |

**There is an attribution requirement, and it is binding.** The README states:

> You shall add the "attribution notice" from the NOTICE file and a link to
> <https://www.tradingview.com/> to the page of your website or mobile application that is
> available to your users.

Two things follow, both worth stating because neither is obvious:

1. **No NOTICE file ships in the npm package.** The requirement names one, and the
   published tarball does not contain it. The link is therefore the part of the obligation
   we can actually discharge from the package we installed.
2. **The link requirement is satisfied by the chart itself.** The library's
   `layout.attributionLogo` option renders the required link to TradingView on the chart.
   It defaults to `true` in version 5, which means the obligation is met by *not*
   interfering — and would be silently broken by anyone setting it to `false` to tidy the
   chart up.

`PriceChart.vue` sets `attributionLogo: true` explicitly rather than relying on the
default, and a test asserts it stays on, so the licence obligation is enforced by the suite
instead of by memory. If the attribution ever proves unacceptable, `PriceChart.vue` is
still the only file that must change.
