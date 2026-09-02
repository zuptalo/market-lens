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

In planning now: feature 016, rolling re-observation of recent sessions. Specification and plan
are at `specs/016-rolling-reobservation/plan.md`; implementation must start with
`TestARestatedCloseIsCorrectedByTheNextRoutinePass` observed red. The nightly pass asks the source
about exactly one session, so the correction path feature 002 specified and 013 and 015 extended
has never run in normal operation. Two measured properties make the fix small and are worth
knowing before touching this code: a source range costs one request per instrument whatever its
width, and an unchanged re-observation performs no write — which is what keeps a quiet night from
triggering a recomputation cascade, because the incremental feature scope is derived from the
bar's `import_run_id`.

Two constraints that outlive any single feature:

- `AUTH_SECRET` is self-provisioned and database-resident, while `EXTERNAL_CREDENTIAL_KEY`
  must never be stored in the database, because it encrypts provider credentials held inside
  that same database.
- No custom UI component may be built where PrimeVue provides one. This is enforced by
  `src/components/library-usage.test.ts`, which also fails if the stylesheet restyles a
  control the theme owns. The single permitted exception, and its reason, is recorded there.
<!-- SPECKIT END -->
