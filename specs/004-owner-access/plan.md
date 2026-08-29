# Implementation Plan: Owner Access and Invitations

**Branch**: `002-instruments-market-data` | **Date**: 2026-08-29 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/004-owner-access/spec.md`

## Summary

Add the identity boundary required by every private Market Lens feature: an atomic,
host-authorized first-owner bootstrap; strong owner authentication and verified-email
recovery; invitation-only, passwordless member access using six-digit email codes;
durable three-attempt temporary blocking and ten-attempt owner-only lockout; opaque
server-side sessions; account administration; audited authorization; and user-scoped,
resumable SSE. The implementation stays inside the Go/Vue modular monolith and uses
PostgreSQL as the only durable coordination and event store.

## Technical Context

**Language/Version**: Go 1.26; Vue 3.5 with strict TypeScript 5.6

**Primary Dependencies**: Existing Go standard-library HTTP stack and pgx 5.9; add
`golang.org/x/crypto/argon2` for the owner's Argon2id credential. Use `crypto/rand`,
HMAC-SHA-256 token digests, constant-time comparison, secure cookies, Vue Router, and
PrimeVue 4.5. Email is behind a narrow delivery interface with SMTP as the first
replaceable adapter; tests use an in-memory capture adapter.

**Storage**: PostgreSQL 18 through ordered embedded SQL migrations. Persist normalized
users, one owner credential, bootstrap/recovery capabilities, invitations, login
challenges, rolling login failures and administrative locks, opaque sessions, security
audit events, email delivery attempts, and authorization-scoped client events. Store
only password hashes and keyed digests of high-entropy capabilities/session tokens;
store a purpose-separated keyed digest of six-digit codes so database disclosure does not permit an
offline enumeration of their one-million-value space.

**Testing**: Go domain/unit, HTTP contract, PostgreSQL migration/repository integration,
concurrency, authorization, timing-shape, secret-regression, and SSE tests; Vitest for
stores/forms/event reconciliation; Playwright for setup, owner/member login, invitation,
lockout/unlock, session management, responsiveness, accessibility, and theme journeys;
production build, Docker build, and Compose validation.

**Responsive UI Verification**: Playwright exercises first-owner setup, owner sign-in
and recovery, member email/code sign-in, invitation acceptance, session management,
member administration, and unlock at 360x800, 768x1024, and 1440x900. A 320x800 check
asserts no page overflow or clipped controls. All actions are keyboard/touch reachable,
non-hover dependent, zoom tolerant, and verified in system, light, and dark themes.

**Live Delivery**: REST loads authenticated account, session, invitation, and member
snapshots. Every visible mutation writes a versioned `client_events` row in the same
transaction. `/api/v1/events` reuses the feature-002 outbox and adds `user` and `owner`
scopes. Connection and `Last-Event-ID` replay reauthorize each row; revoked sessions are
terminated, duplicate events invalidate read models harmlessly, and slow clients are
disconnected to resume from durable storage.

**Identity and Ownership**: A database-enforced singleton permits one active owner.
Only a host-generated, 15-minute, single-use setup capability can create it. The owner
uses Argon2id plus verified-email recovery; members never have password credentials and
authenticate only with the latest unexpired emailed code. Domain services require an
authenticated principal, repositories take the principal/user scope explicitly, and
cross-user tests cover REST, export-shaped queries, cache keys, audit views, and SSE.

**PWA and Notifications**: PWA/Web Push and configurable market notifications are out
of scope. Transactional setup, invitation, member-code, recovery, and security-notice
email uses the same bounded delivery adapter and degrades without taking authenticated
research offline.

**Red-Green-Refactor Proof**: First add a PostgreSQL integration test racing two valid
owner bootstrap transactions against the current schema. Its expected red result is
the absence of the account/setup schema and one-owner invariant, not broken test setup.
Add migration `0007`, the minimum repository/service behavior, and prove exactly one
winner. Each subsequent slice starts with its own focused red test for credentials,
invitations, codes, blocks/locks, sessions, authorization, audit/outbox, HTTP, then UI.

**Database Evolution**:

- `0006_client_events.sql` remains owned by feature 002 and introduces the shared durable
  event outbox before authenticated scopes use it.
- `0007_identity_access.sql` adds users, the singleton-owner invariant, owner
  credentials, bootstrap/recovery capabilities, invitations, login challenges,
  failure/lock state, sessions, security audit events, and account email deliveries.
- `0008_client_event_authorization.sql` adds explicit shared/user/owner scope columns,
  constraints, and replay indexes to the existing event outbox.

Migration tests cover clean installs and upgrade from the current `0001`–`0006`
baseline. Every later correction is a new forward migration; there are no manual steps.

**Target Platform**: Current Linux production container and macOS/Linux development;
one Market Lens process plus PostgreSQL; TLS terminates at the deployment ingress/proxy.

**Project Type**: Self-hosted web application with a Go REST/SSE backend, embedded Vue
SPA, and host-side bootstrap command in the existing binary.

**Performance Goals**: Authentication and account snapshots complete within 500 ms p95
excluding email-provider latency; owner/member administration supports the expected
small household/team set within one second; session revocation ends access/SSE within
five seconds; email delivery state is visible within ten seconds.

**Constraints**: One owner; no public signup; member codes exactly six digits and valid
ten minutes; latest code only; 15-minute block after three consecutive wrong codes; owner-
only administrative lock after ten wrong codes in rolling 24 hours; one code delivery
per minute and five per rolling hour per member plus independent origin limits; generic
anti-enumeration responses; no secrets in logs/URLs/events/browser persistence; secure
same-site cookies and CSRF protection; 320 CSS-pixel tolerance.

**Scale/Scope**: One self-hosted instance, one owner, tens of invited members, hundreds
of concurrent sessions/devices, bounded account email volume, and one account/settings
UI area. Public SaaS tenancy, multiple owners, SSO, MFA, PWA, Web Push, trading records,
and market-alert preferences remain separate specifications.

## Constitution Check

*GATE: Passed before research and re-checked after design.*

| Principle | Design evidence | Result |
|---|---|---|
| Specification-driven | `spec.md` contains independent bootstrap, member-login, invitation, isolation, and lockout criteria. | PASS |
| Modular monolith | Identity, mail, sessions, authorization, and SSE remain packages in the existing process; PostgreSQL is the only external dependency. | PASS |
| Migration-only evolution | `0007` and `0008` own all persistent identity/event changes and have clean/upgrade tests. | PASS |
| Versioned contracts | REST snapshots and mutations plus authorized resumable SSE are documented under `/api/v1`. | PASS |
| Correctness/reproducibility | UTC expiry, rolling windows, single-use transitions, transaction boundaries, and injected clocks/randomness are explicit. | PASS |
| Test-driven development | Every implementation slice names a valid behavioral red and green suite. | PASS |
| Responsive accessible UI | Mobile/tablet/desktop/320, touch, keyboard, themes, safe errors, and lockout states are designed and tested. | PASS |
| Operational simplicity | One image plus PostgreSQL; SMTP is a replaceable integration; bootstrap is an existing-binary host command. | PASS |
| Secure identity/isolation | One owner, invitation-only members, standard password/token primitives, explicit ownership, and cross-user matrices are designed. | PASS |
| Live updates/notifications | Account changes use transaction-coupled scoped outbox events; transactional email is bounded and secret-safe. | PASS |

Post-design re-check: the data model enforces singleton owner and one-use transitions;
contracts use uniform member-login responses; authorization occurs before snapshot and
event replay; email/PWA boundaries remain explicit; no exception is required.

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
│   └── openapi.yaml
└── checklists/
    └── requirements.md
```

### Source Code (repository root)

```text
server/
├── cmd/market-lens/main.go                 # serve and host-only bootstrap command
└── internal/
    ├── api/
    │   ├── auth.go
    │   ├── members.go
    │   ├── invitations.go
    │   ├── sessions.go
    │   └── events.go
    ├── auth/
    │   ├── model.go
    │   ├── password.go
    │   ├── repository.go
    │   ├── service.go
    │   └── session.go
    ├── authorization/policy.go
    ├── db/migrations/0007_...0008_*.sql
    ├── events/
    │   ├── repository.go
    │   └── service.go
    ├── identity/
    │   ├── model.go
    │   ├── repository.go
    │   └── service.go
    └── mail/
        ├── delivery.go
        ├── smtp.go
        └── templates.go

src/
├── components/account/
│   ├── EmailCodeForm.vue
│   ├── InvitationForm.vue
│   ├── MemberList.vue
│   └── SessionList.vue
├── composables/useAuth.ts
├── services/auth.ts
├── services/events.ts
├── stores/auth.ts
├── types/auth.ts
└── views/
    ├── AcceptInvitationView.vue
    ├── LoginView.vue
    ├── OwnerSetupView.vue
    └── AccountSettingsView.vue

e2e/owner-access.spec.ts
```

Tests live beside Go/Vue packages. PostgreSQL integration helpers remain under
`server/internal/testdb`; captured mail and deterministic clock/random sources are test
doubles, never production bypasses.

**Structure Decision**: Preserve the root Vue frontend and `server/` Go backend.
Handlers only decode/respond, auth and identity services own state transitions,
repositories own scoped SQL, authorization policies are reusable, and transactional
mutations accept the event/audit writers needed to commit all effects together.

## Implementation Sequence

1. **Persistence invariant**: Red concurrent-bootstrap migration test; add `0007` with
   normalized users and database-enforced one-owner/setup constraints.
2. **Owner bootstrap and authentication**: Add secure host capability generation,
   Argon2id credential storage, owner sign-in, recovery, opaque sessions, cookies, CSRF,
   expiry, and revocation through red-green slices.
3. **Invitation and member activation**: Add owner-only invitation create/resend/revoke,
   bounded delivery, single-use acceptance, and passwordless member activation.
4. **Member email-code authentication**: Add newest-only six-digit challenges, safe
   keyed storage, generic request/verification responses, independent account/origin
   throttles, three-attempt 15-minute block, rolling-ten administrative lock, and audited
   owner unlock. Verify concurrency and restart durability.
5. **Authorization and live events**: Add principal middleware, repository scope rules,
   owner-admin policies, `0008` event scopes, transactional audit/outbox writes, replay
   authorization, and prompt open-stream termination on revocation.
6. **REST/SSE and frontend**: Implement contracts, route guards, setup/login/acceptance/
   recovery/session/admin views, event-driven snapshot invalidation, and clear connected,
   reconnecting, stale, offline, blocked, and locked states.
7. **Verification**: Run Go/Vitest/Playwright suites, secret and cross-user matrices,
   `make verify`, production/Docker builds, and Compose configuration validation.

## Decisions Required Before Implementation

| Decision | Resolution |
|---|---|
| Owner vs member authentication | Exactly one owner uses a strong Argon2id credential and verified-email recovery; every non-owner uses email codes only. |
| Member code | Six cryptographically random numeric digits, 10-minute lifetime, latest-only, single-use, keyed digest at rest. |
| Abuse thresholds | Three consecutive wrong codes block issuance/verification for 15 minutes; ten wrong codes in rolling 24 hours create an owner-only administrative lock. |
| Request limits | One delivery per member per 60 seconds and five per rolling hour, plus independent origin buckets; responses remain generic. |
| Sessions | Opaque random tokens in Secure/HttpOnly/SameSite cookies, server-side digest/state, eight-hour idle and 30-day absolute limit, rotation after authentication/recovery, CSRF token for mutations. |
| Owner bootstrap | Host command produces a 15-minute single-use capability shown once to the terminal and never logged; bootstrap closes atomically after first owner. |
| Email | Replaceable adapter; SMTP first; transaction records desired delivery and an in-process dispatcher attempts it with bounded retry after commit. Core authenticated use survives outages. |
| Authorization | Shared data is explicit; member-private rows require user ID; owner role grants account administration only, not private financial-data access. |
| Live delivery | Reuse PostgreSQL outbox; shared, user, and owner scopes; reauthorize every connection/replay and disconnect revoked sessions. |

## Safely Deferred Decisions

- Additional owners, ownership transfer, owner deletion, owner passwordless login, MFA,
  passkeys, social login, and enterprise SSO.
- Public signup, public SaaS tenancy, groups, delegated access, and private-data sharing.
- Installable PWA, Web Push, market-alert preferences, quiet hours, and device push
  subscriptions.
- Personal holdings/trades, tracking rules, portfolios, sell suggestions, and their
  notification content.
- Legal retention/export/deletion policy beyond the minimum security audit needed here.

## Complexity Tracking

No constitution violations require justification.
