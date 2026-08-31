# Quickstart: Instrument Exploration and Financial Charts

**Feature**: `005-instrument-exploration` | **Date**: 2026-08-31

How to run, verify, and record evidence for this feature. Sections marked *(to record)* are
filled in as implementation proceeds.

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
npx playwright test
docker build -t market-lens:instrument-exploration .
docker compose config --quiet
```

## Red-green evidence *(to record)*

Each entry names the command, the behavioral failure observed before implementation, and
the suite that proved it green afterwards. A compile failure, a broken fixture, an
unavailable database, or an already-green test is not valid red evidence.

## Honesty matrix *(to record)*

The claim this feature rests on is that nothing is drawn that is not in stored data. Record
evidence for each row across the full universe.

| Claim | Evidence |
| --- | --- |
| No interpolated, forward-filled, or invented session appears in any series | |
| A closed exchange day is never reported as a missing session | |
| An open session with no stored bar is always reported as missing | |
| An overlay is undefined where prior sessions are insufficient, and breaks at a gap | |
| Every corporate action in a displayed range is visible in that view | |
| Every open quality finding for a displayed instrument is listed | |
| A derived statistic with too few sessions is absent, never zero | |
| Prices are never compared or converted across currencies | |

## Responsive and accessibility evidence *(to record)*

| Check | 320x800 | 360x800 | 768x1024 | 1440x900 |
| --- | --- | --- | --- | --- |
| Universe list readable, no horizontal page scrolling | | | | |
| Chart legible and interactive | | | | |
| Range, zoom, and overlay controls operable by touch | | | | |
| The same controls operable by keyboard alone | | | | |
| Text meets AA contrast in system, light, and dark | | | | |
| Range, zoom, and scroll survive orientation and theme change | | | | |

## Live delivery evidence *(to record)*

| Scenario | Evidence |
| --- | --- |
| A committed bar updates its row without resetting filters, sort, or page | |
| A committed bar updates an open chart without losing range, zoom, or overlays | |
| A duplicate event is applied exactly once | |
| A reconnection replays only what was missed | |
| A change outside the current filters does not disturb the view | |
| A recorded corporate action publishes `corporate_action.changed.v1` in its own transaction | |
| An unauthenticated caller receives no event and no data | |

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
