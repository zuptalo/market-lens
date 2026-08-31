# Phase 1 Read Model: Instrument Exploration and Financial Charts

**Feature**: `005-instrument-exploration` | **Date**: 2026-08-31

This feature owns no table. Everything below is a read model derived from tables feature
002 already created, and the derivations are stated precisely so that "what the chart shows"
is never a matter of interpretation.

## Source tables (existing, unchanged)

| Table | Used for |
| --- | --- |
| `instruments` | identity, sector, industry, country, currency, lifecycle status |
| `exchanges` | MIC, name, timezone — every identity display is exchange-qualified |
| `daily_price_bars` | open, high, low, close, adjusted close, volume, provider, observation times |
| `exchange_sessions` | the authoritative trading calendar; `status` of `open`, `half_day`, or `closed` |
| `corporate_actions` | splits, reverse splits, dividends, symbol changes, delistings with ex-dates |
| `data_quality_findings` | open and resolved findings by rule and affected session |
| `client_events` | the shared-scope outbox the SSE stream replays from |

## InstrumentListingRow

One instrument as it appears in the universe list. Produced entirely by the listing query;
never stored.

| Field | Derivation |
| --- | --- |
| `id`, `ticker`, `name`, `isin` | `instruments` |
| `exchange` | `exchanges.mic` and name, joined through `instruments.exchange_id` |
| `sector`, `industry`, `country`, `currency` | `instruments` |
| `status` | `instruments.active` plus any recorded delisting action |
| `latest_session` | `max(session_date)` in `daily_price_bars` for this instrument |
| `latest_close` | `close` at `latest_session` |
| `previous_close` | `close` at the session immediately preceding `latest_session` **in stored data**, not the previous calendar day |
| `change_absolute` | `latest_close - previous_close`; null when either is absent |
| `change_percent` | `change_absolute / previous_close`; null when `previous_close` is absent |
| `return_20`, `return_90` | `close` at `latest_session` over `close` 20 or 90 **stored sessions** earlier, minus one; null when fewer sessions exist |
| `volatility` | standard deviation of the last 20 stored session-over-session logarithmic returns, annualised by the square root of 252; null when fewer than 21 sessions exist |
| `stored_sessions` | count of stored bars, so a reader can see why a statistic is null |
| `freshness` | `latest_session` compared to the most recent `open` or `half_day` session for this instrument's exchange |

**Null is a value, not a zero.** Every derived field above is explicitly absent when the
sessions to compute it do not exist. FR-007 forbids showing zero in that case, and the
listing query returns null so the client cannot accidentally render one.

**Freshness** resolves to one of: `current` (the instrument has a bar for the exchange's
most recent open session), `stale` (it does not, with the number of open sessions missed),
or `no_history` (no stored bars at all).

## HistoryWindow

The chart's payload for one instrument over a bounded range.

| Field | Derivation |
| --- | --- |
| `instrument` | the identity fields of `InstrumentListingRow` |
| `coverage` | first and last stored session for this instrument, independent of the requested range |
| `requested_from`, `requested_to` | the resolved range boundaries in session dates |
| `bars` | stored `daily_price_bars` within the range, ascending by session |
| `missing_sessions` | dates where `exchange_sessions.status IN ('open','half_day')` and no bar exists — see below |
| `series_basis` | `raw` or `provider_adjusted`, from whether adjusted closes are present |
| `provider`, `observed_at` | provider and `last_observed_at` of the most recent bar in range |
| `actions` | `corporate_actions` whose `ex_date` falls in the range |
| `findings` | `data_quality_findings` for this instrument touching the range |

### Missing sessions

A missing session is a date the exchange was open and for which no bar is stored. It is
computed by left-joining the instrument's exchange calendar against its bars:

```sql
SELECT s.session_date
FROM exchange_sessions s
LEFT JOIN daily_price_bars b
  ON b.instrument_id = $1 AND b.session_date = s.session_date
WHERE s.exchange_id = $2
  AND s.status IN ('open', 'half_day')
  AND s.session_date BETWEEN $3 AND $4
  AND b.instrument_id IS NULL
ORDER BY s.session_date
```

This is the whole reason the chart can be honest about gaps rather than guessing. A
weekend, a Swedish midsummer, or a Danish Constitution Day is `closed` in the calendar and
is therefore not a gap; an open session with no bar is one, and is reported as one.

## OverlaySeries

A moving average computed for display from a `HistoryWindow`, never stored and never sent
by the server — the client derives it from the bars it already has.

- Defined by a window length in **stored sessions**.
- Undefined for any session with fewer than that many prior stored sessions; the series
  starts where it becomes defined rather than starting at zero.
- A missing session breaks the series. The overlay is not carried across a gap, because
  doing so would draw an average over observations that do not exist.

## ChartAnnotation

A corporate action or a quality finding anchored to the session it affects, carrying enough
to explain a discontinuity without leaving the view.

| Field | Meaning |
| --- | --- |
| `session_date` | the ex-date, or the session the finding concerns |
| `kind` | `corporate_action` or `quality_finding` |
| `label` | action type, or finding rule |
| `detail` | ratio, amount and currency, or old and new symbol; for a finding, its status |

## ColumnPreference

The optional columns one person has chosen to display. Held in browser storage on the
device (research R4), so it is not a server record, has no owner column, and appears in no
query. A device with no stored preference falls back to the default column set.

## Events consumed and produced

| Event | Direction | Effect |
| --- | --- | --- |
| `daily_bar.changed.v1` | consumed | payload names `instrument_id` and `session_date`; refresh that listing row, or that chart window if it is displayed |
| `quality_finding.changed.v1` | consumed | payload names `instrument_id`; refresh that instrument's findings |
| `import_run.changed.v1`, `import_item.changed.v1` | consumed | refresh freshness indications |
| `corporate_action.changed.v1` | **produced** | new, shared scope, version 1, written in the import transaction that upserts the action; payload names `instrument_id` and `ex_date` |

The new event exists because corporate actions are recorded today without publishing one,
and this feature makes them client-visible for the first time. Every other event already
exists and is reused unchanged.
