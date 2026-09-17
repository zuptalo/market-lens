# Market Lens instructions for Codex

Market Lens is a self-hosted stock research and strategy experimentation application in
its foundation stage. The authoritative engineering principles are in
`.specify/memory/constitution.md`; read them before planning or changing production
code. `CLAUDE.md` contains the equivalent project guidance for Claude-based tooling.

## Architecture

- Backend: Go 1.26 modular monolith, standard-library REST/JSON, pgx/PostgreSQL,
  embedded ordered SQL migrations, `slog`, in-process background work, and authorized
  resumable SSE for client-visible changes.
- Frontend: Vue 3, strict TypeScript, Vite, Vue Router, PrimeVue 4, and reusable
  project-level components.
- Production: one application image serving the built Vue client from Go, with
  PostgreSQL as the only separate service.
- Tests: Go tests, Vitest, Playwright, production builds, and Compose validation.

## Mandatory development workflow

1. Start meaningful behavior changes from a reviewed specification under `specs/`.
2. Before writing or changing production code, write a focused automated test that
   expresses the required behavior.
3. Run the test and verify that it fails for the expected reason. A test that already
   passes, fails because of broken setup, or does not exercise the behavior is not a
   valid red test.
4. Write the minimum production code needed to make that test pass.
5. Run the relevant suite and verify green before refactoring; keep it green throughout
   refactoring.

Do not weaken, skip, delete, or rewrite a test merely to obtain green results. If a
production behavior cannot be covered by a valid automated failing test, stop and amend
the specification or test approach before implementing it.

## Database rules

- Every schema change, seed/reference-data change, backfill, correction, and other
  persistent database transformation must be implemented as an ordered migration.
- Never use manual SQL, console edits, ad-hoc scripts, or direct database manipulation
  as an implementation, deployment, repair, or production-support step.
- Never edit an applied migration. Correct it with a new forward migration.
- Normal runtime reads and writes happen through application code. Operational database
  changes must remain reproducible from migrations because production database access
  is not assumed.

## Additional rules

- Keep `/api/v1/health` as liveness and `/api/v1/ready` as dependency readiness.
- Keep handlers thin and generic transport helpers in `server/internal/httpx`.
- REST may load initial snapshots, but every client-visible committed domain change must
  also be delivered through a versioned, authorized, resumable SSE contract backed by
  transactionally coupled durable events. Polling is not the primary live-update path.
- Bootstrap exactly one first owner, close setup after success, and add users only
  through owner-authorized expiring single-use email invitations. Enforce ownership and
  authorization in backend services and queries; test cross-user data and event isolation.
- Email and Web Push require explicit granular consent, per-device revocation, minimal
  private payloads, unsubscribe controls, and graceful provider-outage behavior. PWA
  behavior must cover supported Chrome/Edge mobile, tablet, and desktop installations.
- Preserve system, light, and dark themes; prefer PrimeVue primitives before custom UI.
- Design every user-facing element mobile-first and specify its mobile, tablet, and
  desktop behavior. Verify representative 360x800, 768x1024, and 1440x900 viewports,
  tolerate 320 CSS pixels without accidental page scrolling or clipped controls, and
  never rely on hover-only interaction. Tables, charts, dialogs, menus, and navigation
  require intentional small-screen behavior.
- Do not introduce new services or integrations without an explicit specification.
- Never commit `.env` files, generated builds, coverage, browser output, or database data.
- Store sensitive CI/CD values only in GitHub Actions secrets. Store non-sensitive CI/CD
  configuration in Actions variables; variables are not secret. Never hard-code or log
  credentials, and never bake them into images or pass them as Docker build arguments.

Run `make verify`, relevant Playwright tests, the Docker build, and Compose validation in
proportion to the change. Never disable a failing check to hide a regression.

<!-- SPECKIT START -->
For durable cross-session context, read `docs/product-vision.md`, `ROADMAP.md`, and
`specs/README.md`, which is the authority on lifecycle status. Everything specified so far is
shipped: `v0.5.0` carries features 002, 003, 004 and 009 through 012, `v0.6.0` carries 005,
Instrument Exploration and Financial Charts, and `v0.9.0` carries 013, the Reusable Feature
Engine.

The engine (`server/internal/features`, `specs/013-feature-engine/`) computes twenty-four
versioned definitions plus the universe composite from stored sessions alone. Three rules
govern anything built on it: a value may never read a session later than its own; a definition
is never changed in place, only superseded by a new version; and a statistic that cannot be
computed is stored and shown as an absence with a reason, never as a zero. The Markets table
reads `return_20`, `return_90` and `volatility_20` from it — feature 005's definitions adopted
verbatim as version 1, so adopting the engine moved no displayed number.

Feature 014 (`specs/014-market-data-navigation/`) shipped after it. Three rules it leaves
behind: operational reporting lives on `/operations`, not on a research screen; the instrument
listing counts its filtered total only for a cursor-less request, because counting per page
would defeat the keyset paging it is built on; and sector is curated reference data whose column
is NOT NULL against a vocabulary containing `unclassified`, so an instrument cannot enter the
universe with no classification state — which is exactly how the column sat empty for a hundred
instruments without anyone noticing.

Feature 015 (`server/internal/strategies`, `specs/015-strategies-and-signals/`) shipped after
that: versioned strategies that read the engine's values and record an explained view. A strategy
emits a signal, never an order — no risk engine, sizing, portfolio, backtest or execution, each of
which is its own later milestone. Four rules it leaves behind: a signal is a view or a stated
absence and never a neutral HOLD standing in for missing data, which the table's own check
constraint enforces; contributions are snapped to the stored precision before the score is derived
from them, so the explanation reconciles with the score by construction; a computation runs a
validation pass before it writes anything, so a failed instrument keeps a whole earlier series
rather than a mixture of two runs; and cross-sectional factors mean one instrument's change moves
every other instrument's rank for that session, so an incremental pass rescores the whole universe
over the affected sessions.

Feature 016 (`specs/016-rolling-reobservation/`) shipped in `v0.13.0`: the scheduled pass re-asks
the source about the last five trading sessions rather than only the one that just closed, so a
restated close is corrected through the path 002 specified and 013 and 015 extended — which had
never run in normal operation, because nothing ever re-imported a date. Three things it leaves
behind. A source range costs one request per instrument whatever its width, so the window is not a
cost dial. An unchanged re-observation performs no write, which is what keeps a quiet night from
triggering a recomputation cascade — the incremental feature scope is derived from the bar's
`import_run_id`, so if that ever changed, every night would rescore the whole universe with no
symptom but a slower pass. And a run reports how many sessions it corrected, distinctly from how
many it stored, because only a correction means every derived value moved underneath.

Feature 017 (`specs/017-unsettleable-findings/`) shipped in `v0.14.0`, fixing what 016 exposed. A
finding the source keeps reporting can never satisfy the resolution rule, so the nightly reach-back
ran for ever and every run stayed amber. The fix is one predicate, not a mechanism: a re-examined
finding records that it was examined and stops driving the reach-back, while staying `open` —
because `status='open'` is also the *resolution* rule's predicate, so a fourth status would have
made every re-examined finding permanent. What the product cannot settle waits for an owner to
accept as a limitation, and a rejection matching a finding already awaiting a decision stops
deciding an item's status while still being counted, so a suppressed badge never suppresses a
number.

Feature 021 (`server/internal/backtest`, `server/internal/series`,
`specs/021-reproducible-backtesting/`) shipped after that: Milestone 5, reproducible backtesting.
It replays stored signals — never recomputes them — under a stated, immutable configuration and
records trades, positions, an equity curve and six measures beside each market's benchmark. Five
rules it leaves behind, four of them enforced by the database rather than by the simulation,
because each is a way to make a result look better than it was and each is invisible in a chart:

- a trade may not execute on the session whose close produced its signal, and executes at the
  **open** of the next session the instrument actually traded;
- a trade names a signal by foreign key, so it cannot exist without its reason;
- an equity point carries a value or a stated absence, never yesterday's number carried forward,
  and a position records which session its price actually came from;
- a measure set reports all six figures or states why it reports none — a subset would be the
  flattering half;
- a published configuration is superseded, never edited, enforced by a trigger.

Two further things it settled. The simulation reads stored data only and the `backtest` package
cannot import a provider client, HTTP, or `marketdata` — a test parses its imports and fails if one
appears, because mocking a provider would prove only that one path did not call it today. And
currency conversion is new behaviour confined to backtesting: rates are stored in one direction
with the accounting currency as the base, converting divides, and nothing else in the product
converts anything.

Feature 022 (`server/internal/portfolio`, `specs/022-personal-portfolio/`) began Milestone 6: a
person's own holdings, and the product's **first user-owned domain records**. Everything before it is
shared reference data, which is why cross-user isolation had only ever been tested on identity
itself. The boundary it establishes is what risk limits and order intents inherit.

Five things it leaves behind:

- **Ownership is inherited, not invented.** `authorization.ScopeUser`, `PrivateScopeFor` and
  user-scoped `client_events` all existed from feature 004; this is their first domain use. What
  needed testing was whether each query remembers to use them, so every read and write path has its
  own cross-user test.
- **`user_id` is denormalised onto every trade**, held equal to the portfolio's owner by a composite
  foreign key. A query scoped only by portfolio would return the right rows for the wrong person the
  first time an identifier leaked — a breach, not a bug.
- **Nothing derived is stored.** Positions, cost and realised results are a fold over the trades, so
  a correction is a re-read rather than a repair, and a superseded version can be kept without any
  figure disagreeing with it.
- **Cost conservation is structural, not approximate.** Lots carry their remaining cost rather than a
  cost per share: dividing then multiplying back loses fractions at the twelfth place, and a
  reconciliation property test caught it on its first attempt.
- **Cash is not tracked, and the product says so.** No portfolio return is reported at all, because
  it does not know what was paid in. The comparison is per holding against its own market instead.

The decimal arithmetic moved to `server/internal/decimal` when this feature needed it too. One
implementation, because two would eventually disagree about the same trade.

Two constraints that outlive any single feature:

- `AUTH_SECRET` is self-provisioned and database-resident, while `EXTERNAL_CREDENTIAL_KEY`
  must never be stored in the database, because it encrypts provider credentials held inside
  that same database.
- No custom UI component may be built where PrimeVue provides one. This is enforced by
  `src/components/library-usage.test.ts`, which also fails if the stylesheet restyles a
  control the theme owns. The single permitted exception, and its reason, is recorded there.
<!-- SPECKIT END -->
