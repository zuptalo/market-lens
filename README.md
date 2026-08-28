# Market Lens

[![Market Lens CI](https://github.com/zuptalo/market-lens/actions/workflows/ci.yml/badge.svg)](https://github.com/zuptalo/market-lens/actions/workflows/ci.yml)

Market Lens is intended to be a self-hosted application for stock research,
backtesting, signal generation, portfolio analysis, and paper trading. The project is
currently at the **foundation/bootstrap stage**: reusable application plumbing is in
place, while financial domain functionality will be defined and implemented from
future specifications.

## Technology stack

- Go 1.26 modular monolith using `net/http`, structured `slog` logging, pgx, and embedded SQL migrations
- PostgreSQL 18
- Vue 3, TypeScript, Vite, Vue Router, and PrimeVue 4
- Go tests, Vitest, and Playwright
- Multi-stage Docker build with the Vue application served by the Go process

## Local development

Prerequisites: Go 1.26, Node.js 22, npm, and Docker with Compose.

```sh
npm install
cp server/.env.example server/.env
make start
```

This starts PostgreSQL, the Go API on <http://localhost:8080>, and Vite on
<http://localhost:5173>. Verify the API with:

```sh
curl http://localhost:8080/api/v1/health
curl http://localhost:8080/api/v1/ready
```

To run the processes individually:

```sh
make db-up
make backend
make frontend
```

## Docker Compose

Run the production-shaped single application image and PostgreSQL:

```sh
docker compose up --build
```

Open <http://localhost:8080>. Set `POSTGRES_PASSWORD` in a local `.env` before any
non-development deployment.

## Tests and builds

```sh
make verify
npm run test:e2e
docker build -t market-lens:local .
docker compose config
```

Playwright requires Chromium once per machine: `npx playwright install chromium`.

## Repository structure

```text
src/                 Vue application shell and reusable frontend code
server/cmd/          Go application entry point
server/internal/     API, config, database, migration, and HTTP infrastructure
e2e/                 Playwright smoke and future feature scenarios
specs/               Future Market Lens specifications
.specify/             Spec Kit templates, scripts, and workflow metadata
.claude/              Reusable Spec Kit agent skills
AGENTS.md             Codex project instructions
CLAUDE.md             Claude project instructions
.github/workflows/    Continuous integration
docs/                 Architectural and developer notes
```

Successful pushes to `main` publish the tested multi-platform container image as
`ghcr.io/zuptalo/market-lens:latest`. See `docs/GITHUB-ACTIONS.md` for the image tags and
the required variables/secrets policy.

The production k3s manifests under `deploy/k8s/` provide PostgreSQL, HTTP-to-HTTPS
redirection, Traefik-managed Let's Encrypt TLS, and two-minute Keel polling of the public `latest` image. See
`deploy/k8s/README.md` for installation and operational details.

Actual Market Lens domain functionality and schema will be implemented from specs
later; the baseline intentionally contains no market-data, strategy, backtest,
portfolio, or trading models.
