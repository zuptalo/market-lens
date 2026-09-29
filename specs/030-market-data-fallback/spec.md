# Feature Specification: Market-data fallback provider

**Feature Branch**: `030-market-data-fallback`

**Created**: 2026-09-29

**Status**: shipped — v0.26.0, 2026-09-29
<!-- Market Lens spec lifecycle: planned → in-progress → in-review → shipped. -->

**Decisions taken before planning** (2026-09-29, recorded here because the spec was revised
after evidence was gathered; see `research.md`):

| Question | Decision | Why |
|---|---|---|
| What triggers the fallback? | **Authentication failure only.** Staleness is not a trigger in this feature | A drifted symbol looks exactly like staleness (see the symbol-drift history), and the four exchanges keep different holiday calendars, so a staleness trigger would fire the fallback on data-quality problems it cannot fix |
| How are backtests protected? | **Excluded, not flagged.** A backtest reads primary-sourced bars only | Principle V: identical inputs must give identical results, and fallback bars are temporary by design. Excluding them keeps a result reproducible across reconciliation |
| Where are the owner's controls? | **Owner commands**, like `marketdata backfill` and `resolve`, with the switch stored in the database | Keel applies images, never configuration, so an environment switch would silently not deploy; and every other market-data operation is already an owner command |
| Do strategy-view notices run on fallback sessions? | **Yes, unchanged** | The verification sample agreed with the primary's close on 100 of 100 instruments, so a view computed from a fallback bar is the view the primary would give; suppressing it would hide exactly what the owner kept the product running for |
| Is a lapse told to the owner even when the fallback is off? | **Yes** | Production's own import on 2026-09-29 failed authentication on every instrument and nobody was told: the failure notice fired only on an error, never on a run that ended failed |

**Input**: User description: "If the market-data subscription lapses, keep price bars arriving from
Yahoo Finance's public endpoints while the owner renews. Fall back only on authentication failure
or demonstrated staleness, keep provenance on every bar, never silently mix sources, reconcile
back to the primary once it works, tell the owner, and decide what downstream features trust."

**Authorization**: This specification is the explicit authorization CLAUDE.md requires before a
new market-data provider is introduced. It authorizes exactly one additional provider, for the
fallback role only. It does not authorize any other provider, any new service, or any new
infrastructure.

## Background

Today, an expired or invalid EODHD key makes every nightly import item fail with
`provider_authentication`. Stored bars, features, backtests and paper history are untouched, but
no new session arrives. Nothing alerts the owner, and the newest session silently ages. Every
feature that reads "the latest session" — signals, risk advice, paper fills — keeps working on
data that is getting older.

The bar table already records the producing provider on each row, and an overwritten bar is
preserved as a numbered revision. This feature builds on both rather than adding a parallel store.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Prices keep arriving through a lapse (Priority: P1)

The subscription lapses. The next nightly import is rejected by the primary provider as
unauthenticated. Instead of ending with no new bars, the import fetches that session from the
fallback provider for every instrument it can map, and the owner has current prices the next
morning.

**Why this priority**: This is the whole point. Without it the feature has no value.

**Independent Test**: Run a daily import against a primary that answers every request with an
authentication failure and a fallback that returns a fresh session; assert that a bar for the new
session exists for each mappable instrument, each tagged with the fallback provider.

**Acceptance Scenarios**:

1. **Given** the primary rejects requests as unauthenticated, **When** the daily import runs,
   **Then** each mappable instrument receives the new session's bar from the fallback and the run
   records that it used the fallback.
2. **Given** the primary answers with rate-limiting, a timeout or a server error, **When** the
   daily import runs, **Then** the fallback is not used and the primary's normal retry behavior
   applies.
3. **Given** the primary answers successfully, **When** the daily import runs, **Then** the
   fallback provider is never contacted.
4. **Given** the fallback is switched off, **When** the primary rejects authentication, **Then**
   the fallback is not contacted and the owner is told the import failed.

---

### User Story 2 - Fallback data is never silently mixed (Priority: P1)

The owner, a backtest, and a reader of any chart can tell which bars came from the fallback, and a
fallback bar can never quietly replace or blend into a primary one.

**Why this priority**: The two providers can adjust for splits and dividends differently. Mixed
series would produce false jumps that corrupt features, backtests and paper results without any
error being raised. This is the failure mode that makes a fallback worse than a gap.

**Independent Test**: Import a session from the fallback over a primary-sourced history and assert
that the primary bars are unchanged, the fallback bars carry their provider, and a backtest over
the window reports it includes fallback data.

**Acceptance Scenarios**:

1. **Given** an instrument with primary-sourced history, **When** fallback bars are stored for
   newer sessions, **Then** no existing primary-sourced bar is modified by the fallback.
2. **Given** a fallback bar and a later primary bar for the same session, **When** the primary
   import runs, **Then** the primary bar replaces it and the fallback bar is kept as a revision.
3. **Given** a backtest whose window contains fallback bars, **When** it runs, **Then** it reads
   only primary-sourced bars, and gives the same result after reconciliation as before.
4. **Given** the fallback source reports a split or dividend after the instrument's newest primary
   bar, **When** the fallback import evaluates it, **Then** that instrument is not imported from the
   fallback and is reported as held back.

---

### User Story 3 - The owner is told, and can see it (Priority: P1)

The owner learns that the app is running on fallback data — in the app, live, and by any channel
they have opted into — and learns again when it stops.

**Why this priority**: A silent fallback hides the very problem it is covering for. The lapse still
needs fixing; the fallback only buys time.

**Independent Test**: Trigger fallback, then assert a banner is present in a fresh page load, a
state-change event reaches an open session without a reload, and one notification is raised per
opted-in channel per state change.

**Acceptance Scenarios**:

1. **Given** the app has entered fallback, **When** the owner opens any page, **Then** a banner
   states that prices are running on fallback data, since when, and how many instruments are
   affected.
2. **Given** a session is open when fallback begins, **When** the state changes, **Then** the
   banner appears without a reload.
3. **Given** the owner has opted into email or push for this kind of notice, **When** fallback
   begins, **Then** exactly one telling is sent per channel for the whole state change, not one per
   instrument.
4. **Given** the owner has not opted in, **When** fallback begins, **Then** nothing is sent and the
   banner is the only signal.
5. **Given** a non-owner member, **When** fallback begins, **Then** they see the banner but cannot
   change any fallback setting or see the credential state.

---

### User Story 4 - Reconciliation back to the primary (Priority: P2)

Once the subscription works again, the owner runs one explicit repair that re-imports every
fallback-covered session from the primary, replaces those bars, and ends the fallback state.

**Why this priority**: Without reconciliation the fallback's bars become permanent and the series
stays mixed. It is P2 only because the fallback delivers value before it is needed.

**Independent Test**: Seed fallback-covered sessions, run reconciliation against a working primary,
and assert every covered bar now carries the primary provider, the originals exist as revisions,
and the fallback state has ended.

**Acceptance Scenarios**:

1. **Given** fallback-covered sessions and a working primary, **When** reconciliation runs,
   **Then** each covered session is re-fetched from the primary and its bar replaced, with the
   fallback bar kept as a revision.
2. **Given** the primary still fails authentication, **When** reconciliation runs, **Then**
   nothing is replaced, the fallback state continues, and the result says why.
3. **Given** reconciliation is interrupted part-way, **When** it is run again, **Then** it resumes
   over the sessions still fallback-sourced and never duplicates work or leaves a half-replaced
   instrument.
4. **Given** the primary works again but reconciliation has not been run, **When** the daily import
   runs, **Then** new sessions come from the primary, older fallback sessions stay flagged, and the
   owner is told reconciliation is pending.
5. **Given** reconciliation has replaced a session, **When** features and signals are next
   computed, **Then** they are recomputed from the reconciled bars.

---

### User Story 5 - Symbol mapping is verified, never guessed (Priority: P2)

Before any fallback bar is trusted, each stored instrument is matched to the fallback provider's
symbol and the owner can see which instruments could not be matched.

**Why this priority**: The project already learned that inferred symbols are wrong — the obvious
guess for one instrument was not the listed symbol. A wrong mapping would store another company's
prices under an instrument.

**Independent Test**: Run the mapping audit over the seeded universe with a recorded fallback
catalog and assert every instrument is classified, none is mapped by inference alone, and unmapped
ones are listed.

**Acceptance Scenarios**:

1. **Given** the seeded universe, **When** the mapping audit runs, **Then** every instrument is
   reported as verified, unmapped, or mismatched, with the evidence for each.
2. **Given** a candidate symbol whose reported currency, exchange or name disagrees with the stored
   instrument, **When** it is evaluated, **Then** it is marked mismatched and never used.
3. **Given** an unmapped instrument, **When** fallback runs, **Then** it receives no fallback bars,
   keeps its last primary bar, and is reported by the audit as unmapped.
4. **Given** a mapping stored by migration, **When** fallback is needed, **Then** only mapped
   instruments are fetched; the migration holds only mappings verified by the audit's rule.

---

### User Story 6 - Fallback data is trusted narrowly (Priority: P2)

Displayed prices and derived views update from fallback data; anything that acts as though prices
were authoritative pauses.

**Why this priority**: Paper fills and risk advice turn data into decisions. An unofficial price
should not silently become a recorded fill.

**Independent Test**: With fallback bars as the newest session, assert prices and signals display
with a fallback marker, and that no paper fill is created from a fallback bar.

**Acceptance Scenarios**:

1. **Given** fallback bars as the newest session, **When** the owner views any screen, **Then**
   prices are shown and the banner states they are fallback-sourced and that paper fills are paused.
2. **Given** a pending paper order whose fill bar is fallback-sourced, **When** fills are
   evaluated, **Then** the order is not filled, stays pending, and is not given up on.
3. **Given** reconciliation replaces that bar, **When** fills are next evaluated, **Then** the
   order fills on the primary bar under the existing fill rule, as if the fallback had never
   covered that session.
4. **Given** risk advice would be computed from a fallback bar, **When** it is shown, **Then** the
   banner is on screen beside it.

---

### User Story 7 - The fallback fails gracefully too (Priority: P3)

When the fallback provider is blocked, changed or unavailable, the import ends cleanly, says both
providers failed, and never stores partial or malformed data.

**Why this priority**: The fallback rests on unofficial endpoints, so failure is expected and must
be safe rather than prevented.

**Independent Test**: Run with both providers failing, and separately with a malformed fallback
response, and assert no bar is written and the run reports both errors.

**Acceptance Scenarios**:

1. **Given** the primary rejects authentication and the fallback is unreachable, **When** the
   daily import runs, **Then** no bars are written, the run ends failed with both causes recorded,
   and the next night tries again.
2. **Given** a fallback response that fails bar validation, **When** it is evaluated, **Then** that
   instrument's bar is rejected and recorded, and other instruments proceed.
3. **Given** the fallback is being rate-limited, **When** the import runs, **Then** it makes at most
   the bounded retries per instrument and never more than two requests at once.

---

### Edge Cases

- The primary recovers mid-run: instruments already imported from the fallback stay flagged and
  await reconciliation; instruments not yet reached use the primary.
- The lapse lasts several sessions: each night asks for every session since the last primary price,
  so a lapse is covered continuously, and a night on which the fallback also failed is filled by the
  next one that succeeds.
- A corporate action (split) occurs during fallback: fallback bars for that instrument after the
  action are held back and reported, because adjustment consistency cannot be verified.
- Fallback and primary disagree on the session's close by more than 0.5% on the day both are
  available: the disagreement is shown in the reconciliation result.
- The fallback returns a bar for a non-trading day or a session already closed on the exchange
  calendar: it is rejected by the same calendar validation as the primary.
- Two imports overlap (nightly plus manual): the per-instrument import lock is shared across
  providers (FR-010a), so a fallback and a primary import cannot write one instrument at once.
- The primary recovers: its nightly window re-observes the trailing sessions (feature 016), so
  fallback bars inside that window are replaced by the next successful primary import without any
  command; reconciliation is needed only for sessions older than the window.
- The owner never renews: the banner stays; nothing is re-sent.
- A backtest was run before the lapse: its inputs were primary-sourced, and so are every later
  run's (FR-027).

## Requirements *(mandatory)*

### Functional Requirements

**Triggering**

- **FR-001**: The system MUST use the fallback provider only for instruments whose primary import
  item in the same scheduled run ended with an authentication failure.
- **FR-002**: The system MUST NOT use the fallback for rate-limit, timeout, or server-error
  outcomes; those continue to follow the primary's retry behavior.
- **FR-003**: The system MUST NOT contact the fallback provider while the primary succeeds and data
  is fresh.
- **FR-004**: Fallback MUST be off by default, switched by an owner command whose state is stored
  in the database, so an install that never wants unofficial data never receives any.
- **FR-004a**: A scheduled import that ends failed MUST raise the existing pipeline-failure notice
  to the owner, whether or not the fallback is on.

**Provenance and integrity**

- **FR-005**: Every bar MUST record the provider that produced it, and the fallback provider's bars
  MUST be distinguishable from the primary's in every read path.
- **FR-006**: A fallback import MUST NOT modify or delete any primary-sourced bar.
- **FR-007**: A primary import MUST replace a fallback-sourced bar for the same session, preserving
  the replaced bar as a revision.
- **FR-008**: The fallback MUST request only sessions after the instrument's newest
  primary-sourced bar, and MUST NOT write any bar for an instrument whose fallback source reports a
  split or dividend in that window; that instrument MUST be reported as held back. A fallback bar's
  adjusted close equals its close, which is exact while no action follows the last primary bar.
- **FR-008a**: A fallback import MUST NOT store corporate actions; the primary stays their only
  source.
- **FR-008b**: A fallback import MUST NOT raise or settle data-quality findings. Findings are the
  primary's to raise and the owner's to decide (feature 017); a clean fallback bar is not evidence
  that a condition the primary reported has passed. Rejected fallback bars are still not stored.
- **FR-009**: Fallback bars MUST pass the same validation as primary bars, including calendar,
  price-range, currency and volume checks.
- **FR-010**: The fallback MUST ask only for the sessions after the instrument's newest
  primary-sourced bar, up to the run's last session. It never reaches into the history before the
  lapse, and because the window always starts at the last primary price, the corporate-action check
  of FR-008 always covers everything since that price. Nights on which the fallback also failed are
  filled by the next night that succeeds; an instrument with no primary price is never covered.
- **FR-010a**: A fallback import MUST hold the same per-instrument import lock as the primary, so the
  two can never write one instrument at the same time.

**Symbol mapping**

- **FR-011**: The system MUST provide a mapping audit that classifies each instrument as verified,
  unmapped, mismatched or unverified. Verified means the fallback reports the instrument's currency
  and a close within 0.5% of the stored primary close on the newest primary-sourced session; the
  evidence (both closes, the session, both currencies) is shown.
- **FR-012**: The system MUST NOT derive a fallback symbol by string manipulation of the primary
  symbol without verification.
- **FR-013**: The system MUST fetch fallback bars only for instruments with a verified mapping,
  and MUST list all others as uncovered.
- **FR-014**: Verified mappings MUST be stored by an ordered migration, never by manual edit, and
  MUST be keyed on an identifier that survives a rename.

**Visibility and notification**

- **FR-015**: While any instrument holds a fallback-sourced bar, the system MUST show a persistent
  banner giving the start date and the count of covered instruments. Instruments not covered —
  unmapped, held back, or refused by the fallback — are reported per instrument in the fallback
  run's items on Operations and by `marketdata fallback audit`, not in the banner.
- **FR-016**: The system MUST publish a versioned, authorized, resumable live event for each
  fallback state change: entered, reconciliation pending, ended, and a change in the counts.
- **FR-017**: The system MUST raise one notification per opted-in channel per state change, never
  one per instrument, with a minimal payload that names no instrument or price. A push is built
  without the detail (feature 027), so it says the source changed; the email says which way.
- **FR-018**: The notice kind MUST default to off for every channel until the owner opts in, and
  MUST be revocable per device using the existing consent controls.
- **FR-019**: There are no reminders while fallback persists; the banner is the standing signal.
  The owner is told when it begins and when it ends.

**Reconciliation**

- **FR-020**: The system MUST provide one explicit owner-invoked reconciliation operation that
  re-imports every fallback-sourced session from the primary.
- **FR-021**: Reconciliation MUST be resumable, MUST leave no instrument half-replaced, and MUST be
  safe to run repeatedly.
- **FR-022**: Reconciliation MUST report, per session, the fallback and primary closes and any
  difference beyond 0.5%; both remain recoverable, the fallback bar as a revision.
- **FR-023**: Reconciliation MUST trigger recomputation of every derived value that used a
  replaced bar.
- **FR-024**: Reconciliation MUST perform no data change outside the application's own operations
  and ordered migrations; no manual SQL is part of any documented procedure.

**Downstream trust**

- **FR-025**: Derived views and signals MAY be computed from fallback bars. The marker is the
  persistent banner on every screen, which states how many instruments' newest prices are
  fallback-sourced; a per-figure marker is out of scope for this feature.
- **FR-026**: Paper-trading fills MUST NOT be created from a fallback-sourced bar; an affected
  order MUST stay pending, MUST NOT be given up on while it waits, and the banner states that fills
  are paused.
- **FR-027**: A backtest MUST read primary-sourced bars only, so its result never depends on
  fallback data and does not change when reconciliation replaces it.
- **FR-028**: A risk-limit advice line computed from a fallback bar is covered by the same banner
  (FR-025).

**Failure handling**

- **FR-029**: When both providers fail, the system MUST write no partial bar, record both causes,
  and retry at the next scheduled import.
- **FR-030**: The fallback client MUST make one request per instrument per run, with at most two
  concurrent requests and the existing bounded retry on rate limiting.
- **FR-031**: The system MUST NOT log or expose provider responses, headers, or any credential in
  errors, using the same safe-error normalization as the primary.

**Ownership and access**

- **FR-032**: Only the owner MAY enable or disable fallback, run the mapping audit, or run
  reconciliation — as owner commands, like every other market-data operation; members MAY view the
  banner.
- **FR-033**: The fallback provider requires no credential; the system MUST NOT add a secret for it.

### Test-First Proof *(mandatory)*

- **Initial failing test**: An import-service test where the primary provider returns an
  authentication failure and the fallback returns a valid new session; it asserts a bar tagged
  with the fallback provider exists. It fails because the service today ends the run with no bar.
- **Expected red reason**: The assertion on the stored bar fails — there is no bar for the new
  session — not a compile or setup error. A paired test with a rate-limit primary asserts the
  fallback was not called.
- **Green evidence**: The market-data package's unit and integration suites, the reconciliation
  and mapping-audit suites, the notification and live-event suites, `make verify`, and a Playwright
  scenario for the banner.
- **Database migration proof**: A migration test proving a clean database and an upgraded one both
  reach the new schema with no manual step, that existing bars keep their provider, and that
  verified mappings load from the ordered migration.

### Responsive UI Behavior *(mandatory for user-facing features; otherwise state N/A)*

- **Mobile (320-767 CSS px)**: The banner is a full-width strip above content that wraps its text
  and never overlaps navigation. Its detail (affected and uncovered counts) opens in a dialog sized
  to the viewport. The fallback marker on a price is an icon plus text, not colour alone.
  Acceptance is automated at 360x800, with a 320px tolerance check for no clipping or page scroll.
- **Tablet (768-1023 CSS px)**: The banner sits beneath the header; the detail opens as a dialog.
  Automated acceptance at 768x1024.
- **Desktop (1024+ CSS px)**: The banner sits beneath the header; the detail opens as a dialog or
  side panel. Automated acceptance at 1440x900.
- **Input and accessibility**: The banner and its detail are keyboard-operable and announced to
  assistive technology; the fallback marker never depends on hover; the state survives rotation and
  zoom; light, dark and system themes are supported using existing components only.

### Live Update Behavior *(mandatory for client-visible data; otherwise state N/A)*

- **Snapshot and events**: The initial state is loaded over the standard snapshot request. Each
  committed state change — entered, reconciliation pending, ended, and the counts changing —
  publishes a versioned named event through the same durable, transactionally coupled storage as
  other domain events. Named events are used; listening for the default message type receives
  nothing.
- **Reliability**: Events are authorized per person, carry ids, and resume by last event id in
  order, with duplicates safe to apply and a bounded buffer for slow consumers.
- **Test evidence**: Automated reconnect, missed-event replay, duplicate, slow-consumer, and
  cross-user isolation scenarios, following the existing patterns.

### Identity, Ownership, and Permissions *(mandatory for user/account data; otherwise state N/A)*

- **Bootstrap and invitations**: N/A — no change to setup or invitations.
- **Ownership and authorization**: The fallback state is instance-wide shared data, visible to
  every member. Its controls and operations are owner-only and are enforced in the backend
  service, not the interface.
- **Security evidence**: Tests that a member is refused every owner operation, and that events
  reach only authorized sessions.

### PWA and Notification Behavior *(mandatory when applicable; otherwise state N/A)*

- **Installability**: Unchanged; the banner degrades to the last known state offline and is marked
  as possibly stale.
- **Consent and delivery**: A new notice kind, off by default per channel, with per-device
  revocation and unsubscribe through the existing controls. One telling per state change, minimal
  payload naming no instrument or price, and the reminder bound in FR-019.
- **Test evidence**: Denied and expired permissions, removed devices, unavailable providers, and
  the privacy of the payload, following the notification feature's patterns.

### Key Entities *(include if feature involves data)*

- **Fallback state**: The instance-wide condition, derived from bar provenance and recorded once
  per change so that a change is told exactly once of running on fallback data — when it began, why
  (authentication or staleness), how many instruments it covers, how many are uncovered, and
  whether reconciliation is pending.
- **Provider symbol mapping**: The verified link between a stored instrument and the fallback
  provider's symbol, with the evidence, its verification date, and its classification.
- **Price bar**: An existing entity; it gains no new identity, but its recorded provider now
  distinguishes primary from fallback and drives every marker and exclusion.
- **Reconciliation result**: The per-session record of the fallback and primary closes and any
  difference beyond tolerance.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: After a subscription lapse, 100% of instruments with a verified mapping have a bar
  for the newest session by the next morning's import.
- **SC-002**: No primary-sourced bar is ever modified by the fallback: across the test suite, the
  count of altered primary bars is zero.
- **SC-003**: The owner sees the fallback banner within one page load of the state change, and an
  open session shows it within 5 seconds without reload.
- **SC-004**: Exactly one notification is raised per opted-in channel per state change, regardless
  of how many instruments are affected.
- **SC-005**: Zero paper fills are created from fallback-sourced bars.
- **SC-006**: After reconciliation against a working primary, zero fallback-sourced bars remain in
  the covered window, and every replaced bar is recoverable as a revision.
- **SC-007**: Every instrument in the universe is classified by the mapping audit, and zero
  instruments are mapped without evidence.
- **SC-008**: When both providers fail, zero partial or malformed bars are stored and the run
  records both causes.
- **SC-009**: The banner and markers pass the 360x800, 768x1024 and 1440x900 acceptance
  scenarios and the 320px tolerance check.

## Assumptions

- The fallback provider's terms of service restrict automated and redistributed use. The owner
  accepts that risk for personal, self-hosted use; this feature does not make it compliant.
- The fallback provider's endpoints are unofficial and may change or block without notice; the
  feature is designed to fail safe, not to guarantee availability.
- The fallback provides daily bars only, and is not used for instrument discovery, corporate-action
  history, or fundamentals.
- The mapping for all 100 instruments was verified against production on 2026-09-29 (see
  `research.md`): 100 of 100 matched currency and close on the newest stored session.
- The primary provider remains the only source of record. The fallback is temporary by design and
  is never preferred once the primary works.
- The existing consent, live-event, ownership, and provider-abstraction mechanisms are reused; no
  new service, queue, or infrastructure is introduced.
- Bars for sessions before the lapse are not touched by this feature.
