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
shipped: `v0.5.0` carries features 002, 003, 004 and 009 through 012, and `v0.6.0` carries
005, Instrument Exploration and Financial Charts.

The next product feature is the Reusable Feature Engine. Its specification and implementation
plan are on `main` at `specs/013-feature-engine/plan.md`, with research, data model,
contracts and the task list `specs/013-feature-engine/tasks.md` alongside; implementation
remains, and must begin with T016 observed red. It computes versioned,
point-in-time features over stored sessions and takes ownership of the three descriptive
statistics feature 005 currently derives in its listing query — adopting their definitions
verbatim, so that no displayed number changes when the source of it does.

Two constraints that outlive any single feature:

- `AUTH_SECRET` is self-provisioned and database-resident, while `EXTERNAL_CREDENTIAL_KEY`
  must never be stored in the database, because it encrypts provider credentials held inside
  that same database.
- No custom UI component may be built where PrimeVue provides one. This is enforced by
  `src/components/library-usage.test.ts`, which also fails if the stylesheet restyles a
  control the theme owns. The single permitted exception, and its reason, is recorded there.
<!-- SPECKIT END -->
