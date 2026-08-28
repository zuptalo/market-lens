# Feature Specification: Instruments and Daily Market Data

**Feature Branch**: `002-instruments-market-data`

**Created**: 2026-08-28

**Status**: in-progress
<!-- Market Lens spec lifecycle: planned → in-progress → in-review → shipped. -->

**Input**: Establish the first financial-domain capability after the completed application foundation: maintain a curated universe of approximately 50–100 liquid Swedish and Nordic equities available to the user through Danske Bank, ingest about ten years of daily market history where available through a replaceable data source, validate and report data quality, and expose enough status and browsing capability to prove the stored market data is usable. This feature deliberately excludes feature calculation, strategies, signals, backtesting, portfolios, risk evaluation, paper trading, automated broker execution, hourly data, machine learning, news, and fundamentals.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Maintain a trustworthy instrument universe (Priority: P1)

As a self-hosted investor, I can maintain a curated initial universe of liquid Swedish
and Nordic equities with unambiguous exchange-aware identities, so that later research
never confuses instruments that share a ticker or have changed symbols.

**Why this priority**: Every price, feature, signal, and backtest depends on stable
instrument identity and a clearly bounded research universe.

**Independent Test**: Load a representative curated universe containing instruments
from multiple Nordic exchanges, including repeated ticker text on different exchanges,
and verify that each instrument remains independently identifiable and discoverable.

**Acceptance Scenarios**:

1. **Given** a valid curated universe entry, **When** the universe is synchronized,
   **Then** the instrument is recorded with its stable internal identity, ISIN, ticker,
   exchange, trading currency, company name, country, instrument type, exchange time
   zone, and active status.
2. **Given** two instruments whose ticker text is identical but whose exchange identity
   differs, **When** both are synchronized, **Then** both remain distinct and neither
   overwrites the other.
3. **Given** an existing instrument whose descriptive metadata changes, **When** a later
   synchronization is performed, **Then** its stable identity and historical price
   association are preserved while the allowed metadata is updated.
4. **Given** an instrument removed from the current curated universe, **When** the
   universe is synchronized, **Then** it is retained for historical integrity and
   marked inactive rather than deleted.

---

### User Story 2 - Build reproducible daily price history (Priority: P1)

As an investor preparing future research, I can import daily price and volume history
for the active universe and safely repeat an import, so that I have a consistent market
data foundation without duplicates or manual database repairs.

**Why this priority**: Historical daily bars are the minimum dataset needed by every
planned analytical capability.

**Independent Test**: Import a deterministic fixture containing normal bars, a market
holiday, a corrected bar, and a split-adjusted series; repeat the same import and verify
the resulting history, counts, corrections, and provenance are deterministic.

**Acceptance Scenarios**:

1. **Given** an active instrument and valid source data, **When** a historical import is
   run for a requested date range, **Then** each daily bar records the trading session,
   open, high, low, close, volume, adjusted value when supplied, currency context, and
   source provenance.
2. **Given** approximately ten years of history are available from the selected source,
   **When** the initial backfill completes, **Then** all available daily sessions in that
   period are stored without duplicate instrument-session records.
3. **Given** the identical data snapshot and import request, **When** the import is
   repeated, **Then** the stored result is unchanged and no duplicate bars are created.
4. **Given** the source later supplies a correction for an already imported bar, **When**
   that date is re-imported, **Then** the corrected source values replace the earlier
   values in a traceable, deterministic manner.
5. **Given** a source exposes adjustment information or corporate actions, **When** the
   history is imported, **Then** the system retains sufficient provenance and adjustment
   context to prevent an apparent split discontinuity from being silently treated as an
   ordinary return in later research.

---

### User Story 3 - Diagnose market-data health (Priority: P2)

As the application operator, I can see the outcome of universe and price imports,
including rejected records and suspicious gaps, so that corrupted or incomplete data
does not silently feed later research.

**Why this priority**: A failed or partially valid import must be visible before the
dataset is trusted, but this status view depends on the core ingestion capability.

**Independent Test**: Process a fixture containing duplicates, invalid OHLC values,
negative values, out-of-order records, zero volume, and a suspicious price jump, then
verify the run summary distinguishes accepted, corrected, flagged, and rejected data.

**Acceptance Scenarios**:

1. **Given** an import is requested, **When** it finishes or fails, **Then** its start
   time, finish time, status, scope, processed count, accepted count, rejected count,
   flagged count, and safe error summary are retained.
2. **Given** malformed or impossible market values, **When** validation runs, **Then**
   invalid records are rejected and do not replace the last valid stored bar.
3. **Given** a missing expected session, unexpected zero volume, or suspicious price
   discontinuity, **When** validation runs, **Then** the condition is visibly flagged
   with instrument and session context without automatically inventing market data.
4. **Given** a failed or partially successful import, **When** the operator reviews the
   market-data status, **Then** the affected instruments and date range can be identified
   and the operator receives a safe host-side retry command that does not expose provider
   credentials in the browser.

---

### User Story 4 - Inspect available market history (Priority: P3)

As an investor, I can search the curated universe and inspect the latest known quote
summary and available daily-history range for an instrument, so that I can verify the
dataset before later analytical features are built.

**Why this priority**: A small inspection surface proves end-to-end usability while
leaving the richer screener, charting, and instrument analysis experience to the next
feature.

**Independent Test**: Search by ticker, company name, and ISIN; open an instrument; and
verify its identity, latest daily bar, history coverage, freshness, and quality warnings
are visible at each required viewport.

**Acceptance Scenarios**:

1. **Given** instruments from several exchanges, **When** the user searches by ticker,
   company name, or ISIN, **Then** matching results identify the exchange and currency
   and never rely on ticker alone.
2. **Given** an instrument with imported history, **When** the user opens its market-data
   summary, **Then** the latest daily OHLCV values, trading session, source, history date
   range, and current quality warnings are shown.
3. **Given** no matching instrument or no imported history, **When** the corresponding
   view is opened, **Then** a clear empty state is shown without presenting fabricated or
   stale values as current.

### Edge Cases

- An ISIN, exchange code, currency, time zone, or provider identifier is missing or
  malformed in a proposed universe entry.
- The same ISIN is submitted with conflicting exchange or instrument details.
- A source uses exchange-local dates while daylight-saving rules change; the trading
  session must not shift or duplicate because of UTC conversion.
- A date range includes weekends, exchange holidays, suspensions, an IPO, or a delisting;
  absent bars are not automatically classified as provider gaps.
- A source returns bars out of order, repeats a bar within one response, or paginates
  with overlapping records.
- OHLC relationships are impossible, a price is non-positive, or volume is negative.
- Zero volume is legitimate for a session but still requires a visible, non-destructive
  quality flag.
- A split, reverse split, dividend adjustment, symbol change, or source correction
  changes previously observed history.
- The source rate-limits, times out, returns only a partial range, or fails midway
  through a multi-instrument import.
- Two imports overlap in instrument and date scope.
- No usable daily bar exists for an active instrument.
- The browser loses connectivity or the viewport changes while filters or an instrument
  selection are active.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST maintain a curated initial universe of approximately
  50–100 liquid Swedish and Nordic equities believed by the user to be purchasable
  through Danske Bank.
- **FR-002**: Each instrument MUST have a stable internal identity independent of ticker
  and MUST record ISIN, ticker, exchange identity, company name, currency, country,
  instrument type, exchange time zone, and active status.
- **FR-003**: The system MUST distinguish instruments by stable identity and exchange
  context; ticker alone MUST NOT establish uniqueness.
- **FR-004**: The system MUST retain inactive, renamed, and removed instruments and their
  historical associations rather than deleting them during synchronization.
- **FR-005**: The system MUST associate source-specific identifiers with instruments
  without making the rest of the product depend on one market-data source.
- **FR-006**: A market-data source MUST be replaceable without changing the meaning of
  stored instruments or daily bars.
- **FR-007**: The first configured source MUST support instrument resolution and
  historical daily OHLCV retrieval for the initial universe.
- **FR-008**: The system MUST support requested date-range backfills and incremental
  daily updates for active instruments.
- **FR-009**: The initial backfill MUST request approximately ten years of daily history
  per active instrument when the selected source makes that history available.
- **FR-010**: Each stored daily bar MUST identify one instrument and trading session and
  contain open, high, low, close, volume, source provenance, and adjustment information
  when supplied.
- **FR-011**: No more than one current daily bar MAY exist for the same instrument and
  trading session within the daily interval.
- **FR-012**: Repeating an import with identical source data MUST be idempotent; a later
  source correction MUST update the current bar deterministically and remain traceable
  to its source and import run.
- **FR-013**: Trading-session interpretation MUST respect the instrument exchange time
  zone and MUST use an exchange calendar when determining whether an absent session is
  an expected closure or a potential data gap.
- **FR-014**: The system MUST reject bars with non-positive prices, negative volume, or
  impossible OHLC relationships and MUST preserve the last valid stored value when a
  replacement is rejected.
- **FR-015**: The system MUST detect and report duplicate source bars, out-of-order
  records, unexpected zero volume, missing expected sessions, provider gaps, suspicious
  price jumps, and possible corporate-action discontinuities.
- **FR-016**: Quality warnings MUST NOT silently alter, interpolate, or invent market
  values.
- **FR-017**: The system MUST retain adjustment or corporate-action context supplied by
  the source, including the methodology or provenance required to interpret adjusted
  prices; limitations in source coverage MUST be visible.
- **FR-018**: Every universe synchronization and market-data import MUST retain a run
  record containing its type, requested scope, source, start and finish times, final
  status, record counts, and a sanitized error summary when applicable.
- **FR-019**: A multi-instrument import MUST report partial success explicitly and MUST
  allow failed instrument/date scopes to be safely retried without duplicating accepted
  data.
- **FR-020**: Concurrent or overlapping imports MUST produce the same valid final data as
  equivalent non-overlapping imports or reject the conflict explicitly; they MUST NOT
  create duplicates or partially overwrite valid bars.
- **FR-021**: Users MUST be able to list and search instruments by ticker, company name,
  and ISIN and filter by exchange, country, currency, and active status.
- **FR-022**: Users MUST be able to inspect an instrument's identity, latest known daily
  bar, available-history range, source freshness, and unresolved quality warnings.
- **FR-023**: Operators MUST be able to review recent synchronization and import runs,
  their outcomes, affected scopes, counts, and sanitized errors, and receive an explicit
  host-side command for safely retrying a failed scope.
- **FR-024**: The system MUST distinguish “latest known daily value” from a real-time
  quote and MUST show the relevant trading session and freshness rather than implying
  live pricing.
- **FR-025**: All stored event times MUST have an unambiguous UTC representation while
  preserving the exchange-local trading-session meaning.
- **FR-026**: This feature MUST NOT calculate research features, generate investment
  recommendations, execute backtests or trades, value portfolios, or claim that the
  curated universe is guaranteed to match current broker availability.

### Test-First Proof *(mandatory)*

- **Initial failing test**: An instrument-universe persistence test that attempts to
  store two instruments with the same ticker on different exchanges and retrieve both
  by their stable identities.
- **Expected red reason**: The foundation contains no instrument domain model or storage,
  so the behavioral assertion that both exchange-qualified identities persist cannot be
  satisfied; failure due only to missing test setup or compilation does not qualify.
- **Green evidence**: Instrument, import, validation, transport, user-interface, and
  end-to-end suites pass, followed by the repository-wide verification command and the
  production container and Compose checks.
- **Database migration proof**: Migration tests must prove that a clean database and the
  current baseline both upgrade through the new ordered domain migrations, that the
  intended identity and daily-bar uniqueness constraints are enforced, and that no
  manual data mutation is required.

### Responsive UI Behavior *(mandatory for user-facing features; otherwise state N/A)*

- **Mobile (320-767 CSS px)**: Instrument search, filters, status summaries, and
  latest-value details remain usable as a single-column flow. Dense results
  use compact stacked rows or explicitly contained detail regions rather than forcing
  the page shell to scroll horizontally. At 360x800, a user can search, select an
  instrument, inspect freshness/warnings, return to the result set with state preserved,
  and review an import failure. At 320 CSS pixels, no status or control is clipped.
- **Tablet (768-1023 CSS px)**: At 768x1024, filters and summaries may share available
  width while result details remain readable, all primary actions stay visible, and
  changing orientation preserves the query, filters, selection, and scroll context.
- **Desktop (1024+ CSS px)**: At 1440x900, the universe and import status can use a
  denser tabular presentation with adjacent filters or detail panels, while preserving
  the same information and actions available at smaller sizes.
- **Input and accessibility**: All search, filter, selection, and disclosure
  controls are keyboard and touch operable, visibly focused, semantically labelled, and
  do not depend on hover. Status and quality meaning is conveyed by text in addition to
  color. System, light, and dark themes remain supported, browser zoom does not hide
  controls, and viewport changes do not discard entered state.

### Key Entities *(include if feature involves data)*

- **Instrument**: A stable security identity with ISIN, ticker, company name, trading
  currency, country, type, exchange association, time zone, lifecycle status, and one or
  more source-specific identifiers.
- **Exchange**: The trading venue identity and calendar context used to disambiguate
  instruments and interpret their trading sessions.
- **Universe Membership**: The curated inclusion of an instrument in the user's research
  universe, including active/inactive state and provenance for the curation decision.
- **Daily Price Bar**: One instrument's OHLCV observations for one exchange trading
  session, with adjustment context, currency, source provenance, and the import run that
  observed it.
- **Corporate Action Context**: Source-provided split, reverse-split, dividend, symbol
  change, delisting, or adjustment information needed to interpret historical prices;
  coverage may be partial but must be explicit.
- **Import Run**: The observable execution record for universe synchronization or market
  data ingestion, including requested scope, source, timing, outcome, counts, retry
  relationship, and sanitized errors.
- **Data Quality Finding**: A non-destructive warning or rejection tied to an instrument,
  trading session, import run, rule, severity, and resolution state.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A fresh deployment can load a curated universe of 50–100 Swedish and Nordic
  equities, and 100% of accepted instruments have a stable identity, ISIN, exchange,
  currency, country, and exchange time zone.
- **SC-002**: For at least 95% of active instruments, the initial import stores every
  daily session the selected source provides across the preceding ten years or the full
  available history for newer listings, with limitations reported for the remainder.
- **SC-003**: Repeating the same complete universe and history imports produces zero
  duplicate instruments and zero duplicate instrument-session bars and leaves identical
  user-visible market history.
- **SC-004**: In a validation fixture containing every required invalid or suspicious
  condition, 100% of invalid records are rejected, 100% of required suspicious
  conditions are flagged, and no valid existing bar is lost.
- **SC-005**: After an import completes, its final outcome and counts are visible to the
  operator within 10 seconds, and every failed instrument/date scope can be identified
  and retried without manual database access.
- **SC-006**: A user can find an instrument by ticker, company name, or ISIN and inspect
  its latest known daily value, coverage, freshness, and warnings in under 30 seconds.
- **SC-007**: The complete inspection and failed-import review journeys pass at 360x800,
  768x1024, and 1440x900, and the interface remains usable without unintended page-level
  horizontal scrolling at 320 CSS pixels.
- **SC-008**: No screen or exported status describes daily end-of-session data as a
  real-time quote, and every displayed latest value includes its trading session.

## Assumptions

- The existing application foundation, health/readiness behavior, migration runner,
  application shell, themes, container image, and Compose deployment are dependencies
  and are not rebuilt by this feature.
- The single user curates or approves the initial universe; without a Danske Bank broker
  integration, the application cannot guarantee current purchasability and will label
  that limitation clearly.
- The current foundation has no user authentication while a separate deployment spec
  targets a public hostname. This feature therefore exposes read-only browser and HTTP
  views only. Initial backfills and retries require authenticated host access and use the
  same validated application workflow; browser-based administrative mutations require a
  separately reviewed authentication specification.
- The initial universe favors liquid common equities across Sweden, Denmark, Norway, and
  Finland; funds, exchange-traded products, derivatives, and other security types are
  outside the initial curated set even though instrument identity can represent future
  types.
- Daily history is the only imported interval in this feature. Hourly and intraday data
  require a later specification.
- One external source may be configured first, but its selection is an implementation-
  planning decision subject to Nordic coverage, history, adjustment data, licensing,
  reliability, rate limits, and self-hosted use.
- Source-provided adjusted values and corporate-action information may be incomplete.
  This feature records available context and exposes limitations; comprehensive
  corporate-action reconciliation requires a later specification if the first source
  cannot support it.
- Automated daily scheduling may be included if supported by the selected source, but
  the operator can always request the initial backfill and a safe host-side retry. The exact post-close
  schedule is selected during planning using exchange calendars and source availability.
- Rich candlestick charts, screeners, feature values, rankings, and instrument analytics
  belong to the following instrument-exploration and feature-engine specifications.
- FX conversion is not required for storing an instrument's native-currency bars. FX
  history and SEK portfolio valuation require a later specification.
- Market-data licensing terms govern whether imported data may be redistributed; V1 is
  for the user's private, self-hosted research.
