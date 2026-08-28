# Market Lens project instructions

Market Lens is a self-hosted stock research and strategy experimentation application in
its foundation stage. Financial domain behavior must be introduced through reviewed
Spec Kit specifications; do not infer schemas, providers, trading behavior, or portfolio
rules from the product summary alone.

The authoritative engineering principles are in `.specify/memory/constitution.md`.
Read them before planning or changing production code.

## Architecture

- Backend: Go 1.26 modular monolith, standard-library REST/JSON HTTP server, pgx/PostgreSQL, embedded ordered SQL migrations, `slog`, and background work in-process.
- Frontend: Vue 3, strict TypeScript, Vite, Vue Router, PrimeVue 4, and project-level reusable components.
- Production: one container builds the Vue app and serves it from the Go process; PostgreSQL is the only separate service.
- Tests: Go tests, Vitest, Playwright, plus Docker build and Compose validation in CI.

## Working rules

- Start meaningful features from `specs/` using the preserved `.specify/` workflow.
- Follow strict red-green-refactor TDD: write an automated test first, run it and verify
  that it fails for the expected behavioral reason, then write the minimum production
  code to make it pass. No production-code change is allowed without that valid red test.
- Never weaken, skip, delete, or rewrite a test merely to obtain a green result. If a
  behavior cannot be tested automatically, stop and resolve the specification or test
  strategy before implementing it.
- Keep `/api/v1/health` as liveness and `/api/v1/ready` as dependency readiness.
- Implement every schema change, seed/reference-data update, backfill, and data correction
  as an ordered migration. Never use manual SQL, console edits, or ad-hoc database
  manipulation for deployment or repair, and never edit an applied migration.
- Keep handlers thin and generic transport helpers in `server/internal/httpx`.
- Preserve light, dark, and system themes and prefer PrimeVue primitives before custom widgets.
- Design every user-facing element mobile-first and define its behavior on mobile,
  tablet, and desktop. Verify representative 360x800, 768x1024, and 1440x900
  viewports, tolerate 320 CSS pixels without accidental page scrolling or clipped
  controls, and never rely on hover-only interaction. Tables, charts, dialogs, menus,
  and navigation require intentional small-screen behavior.
- Do not introduce microservices, Redis, message brokers, Kubernetes, Python services, ML infrastructure, broker integrations, or market-data providers without an explicit future specification.
- Never commit local `.env` files, generated builds, coverage, browser output, or database data.
- Store sensitive CI/CD values only in GitHub Actions secrets and non-sensitive CI/CD
  configuration in Actions variables. Never hard-code, log, or bake credentials into an
  image, and never use Docker build arguments for secrets.

## Verification

Run `make verify`, `npm run test:e2e`, `docker build -t market-lens:local .`, and
`docker compose config` in proportion to the change. Do not disable a failing check to
hide a regression.
