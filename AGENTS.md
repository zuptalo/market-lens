# Market Lens instructions for Codex

Market Lens is a self-hosted stock research and strategy experimentation application in
its foundation stage. The authoritative engineering principles are in
`.specify/memory/constitution.md`; read them before planning or changing production
code. `CLAUDE.md` contains the equivalent project guidance for Claude-based tooling.

## Architecture

- Backend: Go 1.26 modular monolith, standard-library REST/JSON, pgx/PostgreSQL,
  embedded ordered SQL migrations, `slog`, and in-process background work.
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
`specs/README.md`. The current Instruments and Daily Market Data feature is governed by
`specs/002-instruments-market-data/spec.md`; read its `plan.md` and supporting artifacts
for technology choices, structure, commands, contracts, and boundaries.
<!-- SPECKIT END -->
