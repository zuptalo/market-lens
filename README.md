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

Open <http://localhost:8080>. Set unique `POSTGRES_PASSWORD`, `AUTH_SECRET`, and
`EXTERNAL_CREDENTIAL_KEY` values in a local `.env` before starting Compose. Generate the
two independent application keys with `openssl rand -base64 48` and
`openssl rand -base64 32`, respectively, and keep `EXTERNAL_CREDENTIAL_KEY_VERSION=1`
until an explicit credential-key rotation. Never commit that file.

## Account access

Market Lens has no public sign-up. One owner is created from a host-issued setup link, and
everybody else joins by invitation.

### First run

```sh
docker compose exec app market-lens auth setup-link
```

The command prints one URL whose fragment carries a single-use capability valid for 15
minutes. It is printed once and never logged, so copy it before closing the terminal. Open
it and complete the wizard, which asks for the owner's name, email, and password, the EODHD
API key, and the SMTP host, port, sender address, and credential. The EODHD key is validated
against the provider before anything is written; if the provider is unreachable, nothing
commits and the same link can be retried. On success setup closes permanently and the link
stops working.

### Adding people

The owner invites by email from **Account settings**. Each invitation is single-use, expires
in seven days, and can be resent or revoked. An invited person clicks the emailed link, gives
a display name, and is signed in. Members never choose a password: they sign in by entering
their email and the six-digit code that arrives by mail. Three wrong codes block sign-in for
15 minutes; ten in a rolling day lock the account until the owner unlocks it.

### What the host holds and what it never sees

`AUTH_SECRET` and `EXTERNAL_CREDENTIAL_KEY` are the only account secrets in the environment.
The owner password, the EODHD key, and the SMTP credential are entered once in the wizard and
stored encrypted; there is no environment variable for any of them and no way to read one
back. Rotate the encryption key only with the command below, after putting the new key in
`EXTERNAL_CREDENTIAL_KEY` and raising `EXTERNAL_CREDENTIAL_KEY_VERSION`. Every stored secret
is re-encrypted in one transaction, so a failure part-way leaves nothing half-rotated:

```sh
docker compose exec app market-lens auth credential-key rotate --new-version 2
```

If the owner is locked out, reset the password from the host. Both commands read the new
secret interactively from a terminal, accept no flag or environment value, and revoke every
existing session:

```sh
docker compose exec app market-lens auth owner-password reset
```

There is no email password recovery and no public recovery page, by design: the owner account
is the one account an attacker gains nothing by phishing.

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
