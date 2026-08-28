# Quickstart: Owner Access and Invitations

This guide is the implementation/verification contract. It does not authorize manual
database mutation. Every schema change is an embedded ordered migration and every
behavior begins with a valid failing automated test.

## Prerequisites

- Go 1.26, Node/npm, Docker with Compose, and Playwright browsers.
- PostgreSQL started with `make db-up`.
- Feature 002 migration `0006_client_events.sql` implemented before feature 004's
  authorization extension.
- A test-only captured-mail sender for automated flows. Real SMTP credentials are
  optional until deployment validation and must remain outside source/logs/images.

For local integration tests, use `TEST_DATABASE_URL`. Never run ad-hoc SQL to prepare or
repair the schema; the test helper creates isolated databases and runs migrations.

## Configuration contract

Planning names below are subject to implementation tests but their security roles are
fixed:

```text
AUTH_SECRET=<at-least-32-random-bytes-from-a-secret-store>
AUTH_SECURE_COOKIES=true
AUTH_OWNER_IDLE_TIMEOUT=8h
AUTH_SESSION_ABSOLUTE_TIMEOUT=720h
AUTH_SETUP_TTL=15m
AUTH_OWNER_RECOVERY_TTL=30m
AUTH_MEMBER_CODE_TTL=10m
AUTH_MEMBER_TEMP_BLOCK=15m
SMTP_HOST=<optional-host>
SMTP_PORT=587
SMTP_USERNAME=<secret-store-value>
SMTP_PASSWORD=<secret-store-value>
SMTP_FROM=<validated-sender>
```

Production startup fails closed if `AUTH_SECRET` is absent/too short or secure cookies
are disabled. Tests inject deterministic randomness, clock, and mail senders through
constructors; no production debug endpoint returns codes or capabilities.

## Red-green implementation checkpoints

### 1. One-owner database invariant

Write and run the focused integration test first:

```bash
cd server
go test ./internal/identity -run TestBootstrapCreatesExactlyOneOwner -count=1
```

Valid red: the current schema/service cannot represent setup or enforce one owner. Add
`0007_identity_access.sql` and the smallest repository/service slice, then repeat until
green. Add a 100-race test and clean/baseline migration upgrade tests before refactoring.

### 2. Owner credential, recovery, and sessions

Add separate red tests for Argon2id encode/verify/rehash, generic failed login, session
cookie attributes, CSRF rejection, inactivity/absolute expiry, recovery one-use, and
recovery revoking all previous sessions. Run:

```bash
cd server
go test ./internal/auth ./internal/api -run 'TestOwner|TestSession|TestCSRF' -count=1
```

Inspect captured logs/responses/database rows and prove neither password nor bearer/
recovery capability appears.

### 3. Invitation-only activation

Add red tests for create/resend/revoke/accept, intended normalized email, conflicting
accounts, expiry, replay, delivery failures, and absence of member password state:

```bash
cd server
go test ./internal/identity ./internal/mail ./internal/api -run 'TestInvitation|TestMail' -count=1
```

Concurrent acceptance must create at most one member/session. A resend invalidates the
old capability before the new message is attempted.

### 4. Passwordless code and abuse controls

Add red tests in this order:

1. Code generation always produces exactly six digits, including leading zeros.
2. Only the newest unexpired challenge succeeds once.
3. Unknown/inactive/blocked/locked request responses are indistinguishable.
4. One delivery/minute and five/hour per member, plus independent origin buckets.
5. Three consecutive eligible wrong submissions cause a durable 15-minute block.
6. Ten eligible wrong submissions within rolling 24 hours cause administrative lock.
7. Multiple devices/origins/process restarts cannot bypass either threshold.
8. Requests or submissions during a block do not amplify victim lockout counts.
9. Only the owner can unlock; unlock clears counters, revokes codes, and does not
   reactivate the member or reveal private financial data.

```bash
cd server
go test ./internal/auth ./internal/api -run 'TestMemberCode|TestLoginBlock|TestMemberUnlock|TestRateLimit' -count=1
```

Use an injected clock instead of sleeps. Concurrency tests must run with multiple
database connections and serialize on the member/bucket state, not an in-process mutex.

### 5. Authorization, audit, and SSE

Add `0008_client_event_authorization.sql` only after red migration/constraint tests.
Then verify:

```bash
cd server
go test ./internal/authorization ./internal/events ./internal/api -run 'TestAuthorization|TestAudit|TestEvent' -count=1
```

The matrix must prove:

- members cannot list/manage other members or invitations;
- the owner can see account/security metadata but cannot read member financial data;
- user-scoped events go only to that active user's sessions;
- owner events go only to the active owner;
- shared market events remain available to all active users;
- revocation/deactivation rejects replay and closes an open stream within five seconds;
- `Last-Event-ID`, duplicates, slow consumers, and reconnects remain safe.

### 6. HTTP/UI flows

Implement handlers against `contracts/openapi.yaml`, then frontend services/stores/forms
against the handlers. Mutation handlers require same-origin CSRF; login/setup/acceptance
receive secrets in JSON bodies, never query parameters.

```bash
npm run test:unit
npm run typecheck
npx playwright test e2e/owner-access.spec.ts
```

Playwright covers:

- setup and permanent closure;
- owner login/recovery;
- invitation send, safe failure, resend, revoke, and acceptance;
- member code login on a second browser/device;
- invalid/expired/replayed code and 3/10-attempt transitions;
- owner unlock and fresh-code requirement;
- session list, individual revoke, all-device sign-out, and live-stream termination;
- disconnected/reconnecting/stale/offline state without form input loss;
- system/light/dark themes and keyboard/touch/non-hover use;
- 360x800, 768x1024, 1440x900, and no-overflow 320x800.

## Host bootstrap verification

The host-side command may create a setup capability only while bootstrap is open. It
prints the capability URL once to its invoking terminal and ordinary structured logs
must contain only a safe capability ID/expiry.

Illustrative command shape:

```bash
cd server
go run ./cmd/market-lens auth setup-link
```

Do not capture or commit its output. Automated CLI tests use deterministic fake output
and inspect secret-regression logs. After owner creation, rerunning the command returns a
safe closed result and cannot create/reopen setup.

## Full verification

Run in proportion to the completed feature, finishing with:

```bash
make verify
npx playwright test e2e/owner-access.spec.ts
docker build -t market-lens:owner-access .
docker compose config
```

For deployment smoke validation, use a disposable instance and real test mailbox. Never
paste codes, capabilities, SMTP passwords, `AUTH_SECRET`, or session cookies into issue
logs or committed artifacts.

## Expected safe degraded behavior

- SMTP outage: existing sessions and authenticated research work; new email-dependent
  login/invitation/recovery reports only safe retryable delivery state.
- Database unavailable: readiness fails and authentication fails closed; health remains
  liveness.
- Client offline/SSE disconnected: UI labels data stale/offline, preserves input, and
  resumes authorized events or refreshes snapshots after reconnect.
- Member administratively locked: existing valid sessions remain unless separately
  revoked/deactivated; new login stays unavailable until owner unlock and a fresh code.
