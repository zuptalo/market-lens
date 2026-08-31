# Implementation Plan: Owner Access and Invitations

**Branch**: `004-owner-access` | **Date**: 2026-08-30 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/004-owner-access/spec.md`

## Summary

Protect every application page, market-data API, and event stream behind an active
database-backed session. Bootstrap exactly one password-authenticated owner from a
short-lived host-generated capability, validating an EODHD API key before atomically
storing the owner plus encrypted EODHD and SMTP configuration. Present one generic
email-first sign-in flow: all users advance to the same six-digit-code screen and only
the owner may deliberately choose the secondary password action. Members remain
invitation-only and passwordless. Forgotten owner passwords are reset only by an
interactive command inside the deployment, which revokes every owner session and emits
durable audit/event records.

The implementation remains within the Go modular monolith and Vue client. PostgreSQL
stores identity, sessions, abuse controls, audit/outbox records, and authenticated-
encryption envelopes. `AUTH_SECRET` protects opaque-token digests; a distinct
deployment-held 256-bit credential key protects provider credentials using AES-256-GCM.
Applied migrations `0007` and `0008` remain immutable; a new forward migration adds the
credential model and retires public owner recovery.

## Technical Context

**Language/Version**: Go 1.26; Vue 3 with strict TypeScript

**Primary Dependencies**: Go standard-library HTTP/JSON, `pgx/v5`,
`golang.org/x/crypto/argon2`, `golang.org/x/term`, Vue Router, Pinia, PrimeVue 4, Vite

**Storage**: PostgreSQL through embedded ordered migrations; no separate cache, broker,
or secrets service

**Testing**: Go unit/integration/race tests, Vitest, Playwright, production build,
Docker build, Kubernetes/Compose contract validation

**Responsive UI Verification**: Playwright exercises setup and generic sign-in at
360x800, 768x1024, and 1440x900, plus a 320x800 overflow assertion. Keyboard, touch,
non-hover operation, system/light/dark themes, retained form state, and accessible
password/OTP switching are explicit assertions.

**Live Delivery**: REST loads safe setup, account, session, member, invitation, and
integration-status snapshots. Every committed client-visible mutation writes a
versioned `shared`, `user`, or `owner` event in the same transaction. Authorized SSE
supports `Last-Event-ID`, ordered replay, duplicate suppression, bounded slow consumers,
periodic session revalidation, and connected/reconnecting/stale/offline tests. Secrets,
email codes, capability values, addresses, and provider error bodies never enter events.

**Identity and Ownership**: A host-only capability bootstraps one owner and permanently
closes setup. Members are created only by owner-authorized expiring single-use email
invitations. Services and queries enforce owner/member boundaries; route-matrix and
cross-user tests prove anonymous users receive no market data, members cannot administer
others, and the owner cannot read another user's private financial data.

**PWA and Notifications**: Existing PWA behavior remains available after authentication.
This feature introduces transactional account email only; setup explicitly supplies the
SMTP sender configuration and subsequent invitation/code requests constitute the
purpose-specific email action. Delivery failures expose safe status and never disable
existing authenticated research. Web Push is outside this feature.

**Red-Green-Refactor Proof**: The first corrective test extends clean-install and
baseline-upgrade migration tests to require encrypted external credentials and prohibit
usable owner-recovery capabilities. It must fail against migrations through `0008` for
the missing schema/constraint. Subsequent focused red tests cover encryption, atomic
provider validation/setup, generic sign-in, interactive reset, contracts, and UI before
the smallest production change. Focused suites go green before refactoring; full Go,
Vitest, Playwright, build, and deployment checks close the feature.

**Database Evolution**: Preserve applied `0007_identity_access.sql` and
`0008_client_event_authorization.sql`. Add `0009_external_credentials_and_owner_reset.sql`
for encrypted credential envelopes/safe metadata and forward-only retirement of public
owner recovery. Migration tests cover clean install, upgrade from the `0008` baseline,
constraints, and absence of credential plaintext. No manual SQL or production repair
script is permitted.

**Target Platform**: One Linux application container/pod serving the Vue build and Go
API, with PostgreSQL as the only separate service; modern Chrome/Edge mobile, tablet,
and desktop clients

**Project Type**: Self-hosted web application and operational CLI in one Go binary

**Performance Goals**: Ordinary authenticated REST p95 below 500 ms at foundation
scale; setup provider validation is bounded by an explicit 10-second timeout; session
revocation/deactivation stops authorized SSE within five seconds; authentication
throttling remains correct under concurrent requests.

**Constraints**: No public market/API data, identity enumeration, plaintext provider
credentials, public owner recovery, secret-bearing logs/events/browser storage, manual
database changes, or new runtime service. EODHD validation must succeed before any setup
state commits. SMTP/provider outage degrades only dependent actions. Production fails
closed without both required deployment-held keys once credential persistence is in use.

**Scale/Scope**: Exactly one owner, a small invitation-only member group, modest devices
per user, one EODHD credential, one SMTP configuration, and the existing Feature 002
market-data workload

## Constitution Check

*GATE: Passed before Phase 0 research and re-checked after Phase 1 design.*

- **Specification first**: PASS. The reviewed Feature 004 specification contains the
  2026-08-30 generic-login, encrypted-credential, provider-validation, SMTP, and
  deployment-only reset clarifications.
- **Test-first delivery**: PASS. Each behavior change has an explicit focused red test
  before production changes; obsolete recovery greens are retained only as historical
  evidence and receive new contradictory red tests.
- **Forward-only PostgreSQL evolution**: PASS. Existing migrations are not edited;
  `0009` is clean-install and baseline-upgrade tested.
- **Secure identity and ownership**: PASS. Setup creates exactly one owner, invitations
  exclusively add members, backend checks enforce scope, and anti-enumeration plus
  cross-user isolation are tested.
- **Private by default**: PASS. Only health, readiness, safe setup status, bounded setup,
  generic sign-in, password verification, code verification, and invitation acceptance
  are public. Market pages, snapshots, import state, findings, account administration,
  integration status, and SSE require an active session.
- **Durable live delivery**: PASS. Every visible mutation is transactionally coupled to
  a scoped durable event with authorized replay and disconnect behavior.
- **Responsive and accessible UI**: PASS. Mobile/tablet/desktop/narrow, touch,
  non-hover, keyboard, theme, and reconnect behavior are specified and tested.
- **Deployment simplicity and secret safety**: PASS. The monolith and PostgreSQL remain
  the only services. Deployment keys remain outside the database/image/logs; encrypted
  provider values never cross a read API.
- **Graceful external-provider behavior**: PASS. EODHD failure leaves setup unconsumed
  and retryable; SMTP failure preserves existing sessions and records only safe delivery
  state.

No constitution violation requires an exception.

## Project Structure

### Documentation (this feature)

```text
specs/004-owner-access/
├── spec.md
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── openapi.yaml
│   └── cli.md
└── tasks.md
```

### Source Code (repository root)

```text
server/
├── cmd/market-lens/             # server startup plus setup/reset CLI dispatch
├── internal/api/                # thin auth/account/owner/SSE handlers and router
├── internal/auth/               # password, sessions, generic sign-in, reset service
├── internal/config/             # auth and external credential-key configuration
├── internal/credentials/        # authenticated encryption and provider config store
├── internal/db/migrations/      # immutable ordered SQL, including new 0009
├── internal/events/             # authorization-scoped durable event replay
├── internal/identity/           # setup, users, invitations, ownership services
├── internal/mail/               # SMTP adapter and transactional dispatcher
└── internal/marketdata/         # EODHD validation adapter reused by setup

src/
├── components/account/          # setup, generic login, OTP/password, session controls
├── composables/                 # authenticated SSE/session lifecycle
├── router/                      # public-shell allowlist and protected routes
├── services/                    # typed API and event clients
├── stores/                      # in-memory account/CSRF/auth state
└── views/                       # login, setup, account administration

e2e/
└── owner-access.spec.ts         # responsive privacy/authentication journeys

deploy/k8s/                      # deployment secret wiring and validation
docker-compose.yml               # local configuration contract
```

**Structure Decision**: Extend the existing modular monolith. Authentication and
credential cryptography remain focused backend packages; handlers only translate HTTP,
and the Vue client consumes the documented safe contracts. No additional executable or
service is introduced—the operational reset is a subcommand of the application binary.

## Complexity Tracking

No constitution violations or exceptional complexity are accepted.
