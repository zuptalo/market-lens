# Feature Specification: Deterministic Strategies and Signals

**Feature Branch**: `015-strategies-and-signals`

**Created**: 2026-09-02

**Status**: planned
<!-- Market Lens spec lifecycle: planned → in-progress → in-review → shipped. -->

**Input**: User description: "Deterministic strategies and signals (Milestone 4). Versioned
strategies read stored features and emit immutable, explainable signals. A strategy emits a
signal, never an order: no risk engine, no position sizing, no portfolio accounting, no order
intents, no backtesting, no paper trading, no broker anything."

## User Scenarios & Testing *(mandatory)*

The engine computes twenty-four definitions plus the universe composite for every instrument
and session, and the Markets table shows three of them. Nothing turns those numbers into a
stated view. A person can see that an instrument returned 8% over ninety sessions and trades
above its 200-session average, but the product never says *this looks strong, by this
definition, for these reasons* — and never records that it said so, at a time, from data that
was knowable then.

That recording is the point. A view that is not written down cannot be checked later, and a
score with no reasons cannot be argued with. Everything the roadmap places after this —
backtesting, portfolio, paper trading — consumes strategy behaviour, so this feature decides
what a strategy *is* before anything depends on it.

### User Story 1 - A strategy states a view, and says why (Priority: P1)

A person opens an instrument and sees how the current strategy scores it as of its latest
session: an action, a score, and the factors that produced that score — which contributed
positively, which negatively, and by how much. They can see the feature values the strategy
read, so the score is traceable to numbers they can check themselves on the same screen.

**Why this priority**: It is the whole feature in one screen. Without the reasons, a score is
an oracle; with them, it is an argument a person can disagree with. Everything else here is
plumbing that makes this trustworthy.

**Independent Test**: Compute signals over the fixture universe, open one instrument, and
confirm the action, score and per-factor contributions are shown, that the contributions
account for the score, and that each names the feature value it came from.

**Acceptance Scenarios**:

1. **Given** an instrument with a computed signal, **When** a person views it, **Then** they
   see the action, the score, the strategy and version that produced it, the session it is as
   of, and each factor's contribution with its direction and magnitude.
2. **Given** a signal's contributions, **When** a person adds them up, **Then** they account
   for the stated score — no unexplained residual.
3. **Given** a factor derived from a feature, **When** a person reads that factor, **Then**
   they can see the feature value it used and the session that value is from.
4. **Given** an instrument the strategy cannot score — insufficient history, a feature the
   engine recorded as absent, a failed liquidity rule — **When** a person views it, **Then**
   they see a stated reason for the absence, never a neutral score and never a HOLD standing in
   for "no data".
5. **Given** any screen showing a signal, **When** a person reads it, **Then** it states that
   this is a strategy output rather than advice, in the same view rather than behind a link.

---

### User Story 2 - The same inputs always produce the same signal (Priority: P1)

Somebody recomputes a strategy version over a session that was already computed and gets an
identical signal — same score, same action, same contributions. A signal recorded in the past
never silently changes, and a signal produced by a superseded strategy version stays readable
and labelled with the version that produced it.

**Why this priority**: Equal to US1, because a reason that changes when nobody changed anything
is not a reason. Backtesting is worthless without this, and every later milestone rests on it.

**Independent Test**: Compute signals twice over the same fixture and diff every stored field —
zero differences. Then publish a second strategy version, recompute, and confirm the first
version's signals are unchanged and still attributed to it.

**Acceptance Scenarios**:

1. **Given** a computed set of signals, **When** the same strategy version is recomputed over
   the same sessions from the same feature values, **Then** every stored field is identical.
2. **Given** a published strategy version, **When** a parameter needs to change, **Then** a new
   version is published and the previous one is superseded rather than edited.
3. **Given** signals from a superseded version, **When** a person reads them, **Then** they are
   still readable and state the version that produced them.
4. **Given** a signal as of a session, **When** anything about a later session changes — a new
   bar, a revised bar, a recomputed feature — **Then** that signal does not change.
5. **Given** a corrected bar deep in history, **When** features are recomputed and signals
   follow, **Then** only signals at sessions whose inputs actually changed are rewritten, and
   the change is attributable to that correction.

---

### User Story 3 - The universe can be read in the strategy's order (Priority: P2)

A person opens a view of the whole curated universe ranked by the current strategy's score for
the latest session, sees which instruments the strategy ranks highest and lowest, and can move
from any row into that instrument's reasons.

**Why this priority**: It is how the strategy becomes useful rather than merely correct — one
instrument at a time answers "what does it think of this", but the ranking answers "what does
it think". It ranks below the first two because a wrong ranking presented beautifully is worse
than no ranking.

**Independent Test**: Compute signals over the fixture universe and confirm the ranked view
orders every scored instrument by score, states how many were scored and how many could not be,
and links each row to its reasons.

**Acceptance Scenarios**:

1. **Given** computed signals for a session, **When** a person opens the ranked view, **Then**
   instruments appear in score order with their action and score.
2. **Given** instruments the strategy could not score, **When** the ranked view is shown,
   **Then** they are stated as unscored with their reason rather than ranked last as if they
   had scored poorly.
3. **Given** a ranked row, **When** a person opens it, **Then** they reach that instrument's
   contributions for the same session and strategy version.
4. **Given** more than one published strategy version, **When** a person reads the ranking,
   **Then** the view states which version it is showing.

---

### User Story 4 - Signals follow the data without being asked (Priority: P3)

After an import and the feature pass that follows it, signals for the affected sessions are
recomputed without anybody running anything, and a person watching a screen sees the change
arrive. An owner can also run the computation deliberately — for the whole history, for one
strategy version, or for what a given import changed.

**Why this priority**: It is convenience over correctness: everything above can be exercised by
running the computation by hand. It matters because a stale signal presented as current is the
same class of quiet dishonesty this product avoids elsewhere.

**Independent Test**: Run an import that changes bars, let the feature pass run, and confirm
signals for exactly the affected sessions are recomputed and published.

**Acceptance Scenarios**:

1. **Given** a successful feature computation, **When** it finishes, **Then** signals for the
   sessions whose features changed are recomputed.
2. **Given** a signal computation that fails for one instrument, **When** the run finishes,
   **Then** that instrument keeps its previous signals, the failure is recorded against the
   run, and the other instruments' signals are written.
3. **Given** an owner at the command line, **When** they ask for a full recomputation, a single
   strategy version, or only what an import changed, **Then** each is possible and reports what
   it did.
4. **Given** a person watching a screen showing signals, **When** signals are recomputed,
   **Then** the change reaches them without a manual refresh.
5. **Given** a signal computation has never run in a deployment, **When** a person opens a
   signal surface, **Then** it states that no strategy has run rather than showing an empty
   view that reads as "nothing to say".

---

### Edge Cases

- An instrument listed too recently for the strategy's longest input window: unscored, with
  insufficient history as the stated reason.
- A feature the engine recorded as absent — a gap in the window, a zero denominator — feeding a
  factor: the factor is unavailable and the signal states which input was missing.
- The universe composite undefined for a session (fewer than ten contributors): factors that
  compare against the universe are unavailable, and a strategy that requires them scores
  nothing rather than silently comparing against nothing.
- Every instrument in the universe scores identically: the ranking is stable and repeatable
  rather than arbitrary, so the same input never produces two different orders.
- A strategy version is published while a computation is running: the running computation
  finishes under the version it started with, and its signals are attributed to that version.
- An instrument leaves the curated universe: its historical signals remain readable rather than
  disappearing, because a record of what was said is not invalidated by a later membership
  change.
- Two strategy versions are current at once: every surface states which it is showing, and
  nothing merges signals from different versions into one ranking.
- A signal's score sits exactly on a threshold between two actions: the boundary is defined and
  stated, so the same input never yields different actions on different runs.

## Requirements *(mandatory)*

### Functional Requirements

**Strategies as versioned, immutable definitions**

- **FR-001**: A strategy MUST be a published, versioned definition whose parameters — weights,
  thresholds, liquidity rules, the features it reads — are part of the version.
- **FR-002**: A published strategy version MUST NOT be edited. A change publishes a new version
  and supersedes the old one, which remains readable.
- **FR-003**: Every strategy version MUST state, in terms a person can read, what it is trying
  to express and which features it reads.
- **FR-004**: The system MUST record that a strategy exists to validate the platform rather
  than to claim optimality, and MUST NOT present any strategy's output as a recommendation to
  transact.

**The first strategy**

- **FR-005**: The system MUST publish one deterministic multi-factor momentum and trend
  strategy built only from features the engine already computes.
- **FR-006**: Its factors MUST be drawn from momentum, trend, relative strength against the
  universe composite, volume confirmation, a volatility penalty, market regime, and sector
  strength; each factor MUST name the feature or features it reads.
- **FR-007**: Every factor's contribution to the score MUST be individually recorded, with its
  direction and magnitude.

**Signals**

- **FR-008**: A signal MUST record the instrument, the session it is as of, the strategy and
  version, the parameters in force, the score, the action, the confidence, the feature values
  read, and the per-factor contributions.
- **FR-008a**: A signal MUST exist for **every instrument in the universe at every session it
  has stored history for**, under each published strategy version — either a scored signal or a
  stated absence. Reading "what did this strategy say about this instrument on this date" MUST
  therefore be a single lookup rather than a search backwards for the most recent one.
- **FR-009**: Actions MUST be one of `BUY`, `HOLD`, `REDUCE`, `SELL`, `WATCH`. A strategy is
  not required to recommend a trade, and `HOLD` MUST mean a stated view rather than an absence.
- **FR-010**: The action boundaries MUST be defined by the strategy version, so that a score
  maps to exactly one action and the same score always maps to the same action.
- **FR-011**: The recorded contributions MUST account for the stated score, leaving no
  unexplained remainder.
- **FR-012**: Where a signal cannot be produced, the system MUST record the absence with its
  reason against the instrument and session, and MUST NOT record a score, a neutral action, or
  a default.
- **FR-013**: Confidence MUST express **agreement between the factors**: how much of the
  available contribution weight points the same way as the score. It MUST be derived
  deterministically from the contributions already recorded, MUST be stated in those terms
  wherever it is shown, and MUST NOT be presented as a probability that the view is correct.
- **FR-013a**: Where factors disagree, confidence MUST fall; where every available factor points
  the same way, it MUST be at its maximum. A signal with only one available factor MUST NOT
  report the same confidence as one where seven factors agree, because unanimity among one
  factor is not agreement.

**Reproducibility and time**

- **FR-014**: Recomputing a strategy version over a session from the same feature values MUST
  produce an identical signal in every stored field.
- **FR-015**: A signal as of a session MUST read only feature values as of that session or
  earlier. No signal may be influenced by a later session.
- **FR-016**: A stored signal MUST NOT change except by recomputation attributable to a change
  in its own inputs, and the system MUST be able to state which run wrote it.
- **FR-017**: Signals produced by a superseded strategy version MUST remain readable and
  labelled with that version.

**Computation**

- **FR-018**: The system MUST recompute signals for the sessions whose feature values changed,
  after the feature computation that changed them, without human action.
- **FR-019**: An owner MUST be able to trigger a computation for the full history, for one
  strategy version, or for only what a given import changed, and each MUST report what it did.
- **FR-020**: A failure computing one instrument MUST NOT abandon the run or discard that
  instrument's previous signals; it MUST be recorded with a reason and the run reported as
  partial.
- **FR-021**: No read-only interaction may trigger a computation.

**Reading them**

- **FR-022**: Signals MUST be readable for one instrument as of a session, and as a ranking of
  the universe for a session, through the authorized interface every other read uses.
- **FR-023**: Every committed change to signals MUST publish a versioned, authorized,
  resumable event, so a connected screen reflects it without polling.
- **FR-024**: Every surface showing a signal MUST state that it is a strategy output rather
  than advice, within the same view.
- **FR-025**: A ranked view MUST state which strategy version and session it is showing, and
  MUST distinguish instruments that scored from instruments that could not be scored.
- **FR-026**: Instruments that have left the curated universe MUST retain their historical
  signals as readable records.

### Test-First Proof *(mandatory)*

- **Initial failing test**: `TestTheSameStrategyVersionScoresAnInstrumentIdentically` — computes
  a signal for one fixture instrument as of one session, recomputes it, and asserts every stored
  field including each factor contribution is identical. It fails because no strategy exists and
  no signal is produced.
- **Expected red reason**: The assertion on the produced signal fails because there is no
  signal — a stated, behavioural absence, not a compilation failure. The test must be written so
  that it compiles against the signal shape before any strategy computes one.
- **Green evidence**: The strategy suite in the backend, the API contract suite, the Vitest
  suites for the signal surfaces, and the Playwright journeys across mobile, tablet and desktop.
- **Database migration proof**: Strategy versions and signals are persistent. An ordered
  migration introduces them, and a migration test MUST prove a clean install and an upgrade from
  the current schema both arrive with the first strategy version published and the constraints
  in force, with no manual step.

### Responsive UI Behavior *(mandatory for user-facing features; otherwise state N/A)*

- **Mobile (320-767 CSS px)**: The instrument's signal is a stacked card — action, score,
  version and session first, then the factor contributions as a list, each naming its feature
  and value. Contributions are readable without horizontal scrolling; a bar or similar visual
  showing direction and magnitude must not be the only way the magnitude is conveyed. The ranked
  universe view reuses the stacked presentation the instrument listing already uses. At 360x800
  an automated scenario asserts the signal, its reasons and the not-advice statement are all
  reachable without horizontal page scrolling. At the 320 pixel floor nothing clips or overlaps.
- **Tablet (768-1023 CSS px)**: Contributions may sit beside the score. The ranked view presents
  as a table. At 768x1024 an automated scenario asserts the same content is present and the
  table's own horizontal scrolling stays contained.
- **Desktop (1024+ CSS px)**: As tablet, with the full contribution detail visible without
  interaction. At 1440x900 an automated scenario asserts the ranking, the reasons and the
  version statement.
- **Input and accessibility**: Every contribution's direction and magnitude MUST be available as
  text, not colour or length alone — a red bar means nothing to a screen reader and little to a
  person who cannot distinguish it. No interaction depends on hover. The ranking is keyboard
  navigable and each row's link to its reasons is focusable. Orientation changes and zoom to
  200% preserve the view and the reader's place.

### Live Update Behavior *(mandatory for client-visible data; otherwise state N/A)*

- **Snapshot and events**: A screen loads signals over the authorized read and receives changes
  as a new versioned, named event published when a signal computation commits. The event
  carries enough to identify what changed — the instruments and the session range — without
  carrying the signals themselves.
- **Reliability**: The event is shared scope, ordered, resumable from the last delivered
  identifier, and duplicate-safe, exactly as the existing market-data and feature events are.
  Publication is transactionally coupled to the signals it describes, so a reconnecting client
  cannot miss a committed change.
- **Test evidence**: Automated scenarios MUST cover a recomputation reaching an open instrument
  view and an open ranking; a burst of changes during a full recomputation coalescing into a
  bounded number of requests; reconnection replaying only missed events; duplicate delivery
  applied once; and a deactivated or unauthenticated client receiving nothing.

### Identity, Ownership, and Permissions *(mandatory for user/account data; otherwise state N/A)*

- **Bootstrap and invitations**: Unchanged by this feature.
- **Ownership and authorization**: Strategies and signals are shared reference data, not
  per-user records: every authenticated user sees the same signals, and nothing here is scoped
  to a person. Triggering a computation is an owner action at the command line, not an
  interface action, so no new role or permission is introduced.
- **Security evidence**: Automated tests MUST prove the signal reads are refused to an
  unauthenticated request and to a deactivated account, and that no surface exposes a
  credential, a token or a raw provider error.

### Key Entities *(include if feature involves data)*

- **Strategy**: A named, versioned, published definition of how to turn features into a view,
  including its parameters, the features it reads, its action boundaries, and how it defines
  confidence. Superseded rather than edited.
- **Signal**: One strategy version's view of one instrument as of one session — score, action,
  confidence — or the stated reason no view could be formed.
- **Factor contribution**: One named component of a signal's score, with the feature values it
  read, its direction and its magnitude. The unit of explanation.
- **Strategy run**: One computation over a universe and a set of sessions, with its kind,
  status, counts and per-instrument outcomes — the record that lets a person ask when a signal
  was produced and by which run.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Recomputing every signal over the whole stored history produces zero differences
  from the previous computation, field for field.
- **SC-002**: For every signal shown, the stated factor contributions account for the score
  with no unexplained remainder.
- **SC-003**: A person can move from a ranked universe to the reasons behind any single score
  in one step, at every supported viewport.
- **SC-004**: No instrument is ever shown a score, an action or a confidence derived from a
  feature value the engine recorded as absent; each such case states which input was missing.
- **SC-005**: No signal changes as a consequence of data from a session later than its own,
  proven by extending history and recomputing.
- **SC-006**: Every surface that shows a signal states, in that view, that it is a strategy
  output and not advice.
- **SC-007**: After an import and its feature computation, signals for the affected sessions
  are current without human action, and a person watching a screen sees the change arrive.
- **SC-008**: Signals produced by a superseded strategy version remain readable and correctly
  attributed after a new version is published.
- **SC-009**: A full computation over the curated universe and its stored history completes
  within a stated time budget on the deployment's own hardware, and an incremental computation
  after one import completes within a smaller stated budget.
- **SC-010**: Every factor's direction and magnitude is available as text to a screen reader,
  not conveyed by colour or bar length alone.

## Assumptions

- Signals are computed for the full stored history, not only the latest session. Backtesting
  (Milestone 5) needs the history, and computing it later would mean recomputing everything
  anyway.
- One signal per instrument, per session, per strategy version — roughly 250,000 rows for
  today's universe and history, against the feature store's 5.8 million. Storing only the
  sessions where the view changes would be smaller, but it turns every point-in-time question
  into "the most recent signal on or before this session", which is easy to get subtly wrong
  and would be relied on by every later milestone. The decision (2026-09-02) trades storage,
  which is cheap here, for a lookup that cannot be misread.
- The strategy reads only stored features, never bars directly. The engine already owns
  definitions, versioning and the no-lookahead guarantee; a strategy reaching past it would
  fork that logic, which the product vision explicitly forbids.
- Scoring is cross-sectional: an instrument's score depends on how it compares with the rest of
  the universe for that session, as ranking implies. This makes the computation ordered the way
  the composite already is — the universe's values must exist before any instrument's rank does.
- One strategy ships. The versioning, storage and interfaces are built so that a second is a
  new published version rather than new machinery, but proving that with two strategies is not
  in scope.
- Sector strength is available as a factor only because sector classification shipped in feature
  014; before that it would have been a factor over an empty column.
- Signals are shared, not per-user. Personal tracking and watchlists are Milestone 6.
- No strategy parameter is editable through the interface. Publishing a version is a reviewed
  change like every other definition, which keeps "what produced this signal" answerable.
- Costs, position sizing, risk limits and order intents are out of scope and are named here only
  to make the boundary explicit.

## Resolved decisions

### Confidence expresses agreement between factors (decided 2026-09-02)

The product vision persists a confidence field without defining it. Three definitions were
weighed, each measuring something different.

| Option | Answer | Implications |
|--------|--------|--------------|
| **A (chosen)** | Agreement between factors — how much of the available contribution weight points the same way as the score | Cheap, reproducible from contributions already recorded, and explains itself in a sentence: "six of seven factors agree". Honest about what it is — internal consistency, not a probability of being right. |
| B | Distance from the action boundary | Directly meaningful for the decision at hand, but conflates "clearly inside this band" with "likely to be correct". |
| C | Completeness of inputs | Makes the absence rules visible, but measures data coverage rather than conviction, which the word does not suggest. |

**Chosen: A**, with the constraint in FR-013 that it is never presented as a probability that
the view is correct. Nothing in this feature can support that claim, and the word invites it —
which is why the requirement fixes both the definition and how it must be described.

### A signal exists for every instrument at every session (decided 2026-09-02)

| Option | Answer | Implications |
|--------|--------|--------------|
| **A (chosen)** | One signal per instrument, session and strategy version — scored or a stated absence | "What did it say on this date" is one lookup. About 250,000 rows per version against a feature store of 5.8 million; most rows repeat the previous session's view, which is the price. |
| B | Only when the action changes | Far fewer rows and a natural reading of "when did it change its mind", but every point-in-time query becomes "most recent on or before", which every later milestone would depend on getting right. |
| C | Every session, contributions only when they change | Middle path, more machinery than either extreme. |

**Chosen: A.** Storage is the cheap resource here and correctness of the point-in-time lookup is
not; backtesting, portfolio and paper trading all read signals as of a date, and each of them
inheriting a subtle "most recent on or before" rule is a poor trade for space this deployment
has.
