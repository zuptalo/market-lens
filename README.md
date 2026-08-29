# Market Lens

[![Market Lens CI](https://github.com/zuptalo/market-lens/actions/workflows/ci.yml/badge.svg)](https://github.com/zuptalo/market-lens/actions/workflows/ci.yml)

Market Lens is a self-hosted application for stock research, strategy experimentation,
portfolio analysis, and paper trading. The current product slice provides a curated
100-instrument Nordic universe, reproducible daily market-data imports, quality and
import health, read-only inspection, and durable resumable live updates. Strategies,
backtests, portfolios, and trading remain governed by later specifications.

## Technology stack

- Go 1.26 modular monolith using `net/http`, REST snapshots, durable resumable SSE,
  structured `slog` logging, pgx, and embedded SQL migrations
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
<http://localhost:5173>. Before launching, it stops existing listeners on the configured
backend port (`PORT`, default 8080) and Vite port 5173. Verify the API with:

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

## Market-data operations

Market-data mutations are host-only. Configure an EOD Historical Data All World token
in an ignored host environment file, then run the shared importer from `server/`:

```sh
go run ./cmd/market-lens marketdata backfill --universe nordic-liquid-v1 --years 10
go run ./cmd/market-lens marketdata update --universe nordic-liquid-v1 --days 7
go run ./cmd/market-lens marketdata retry --run '<run-uuid>'
```

The commands print only a run ID and safe aggregate totals. The browser and `/api/v1`
expose read-only instrument, price, import, quality, and resumable SSE contracts; they
never receive the provider token. See [market-data operations](docs/MARKET-DATA.md) for
configuration, scheduling, provider limitations, calendar maintenance, and acceptance
inspection.

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

Verified squash merges to protected `main` automatically create a semantic GitHub
Release and publish one multi-platform image under full-version, major/minor, commit, and
`latest` tags. The running version is visible in the application shell and health API.
See `docs/GITHUB-ACTIONS.md` for delivery and sensitive-configuration policy.

The production k3s manifests under `deploy/k8s/` provide PostgreSQL, HTTP-to-HTTPS
redirection, Traefik-managed Let's Encrypt TLS, and two-minute Keel polling of the public `latest` image. See
`deploy/k8s/README.md` for installation and operational details.

Market-data domain functionality and schema are governed by Feature 002. Strategy,
backtest, portfolio, and trading models are intentionally absent until their own
reviewed specifications authorize them.

The product direction includes exactly one first-owner bootstrap, owner-invited users,
backend-enforced private-data isolation, per-user tracking/trading records, an
installable Chrome/Edge PWA, and consented email/Web Push alerts. These cross-cutting
capabilities require their own reviewed specifications before implementation.

## Product planning

- [`docs/product-vision.md`](docs/product-vision.md) is the durable long-term product
  baseline.
- [`ROADMAP.md`](ROADMAP.md) shows what is shipped, planned, backlogged, and deferred.
- [`specs/README.md`](specs/README.md) is the feature-specification registry and explains
  how to resume work in a future session.
