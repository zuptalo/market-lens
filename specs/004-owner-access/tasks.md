# Tasks: Owner Access and Invitations

**Input**: Design documents from `specs/004-owner-access/`

**Prerequisites**: `spec.md`, `plan.md`, `research.md`, `data-model.md`,
`contracts/openapi.yaml`, and `quickstart.md`

**Tests**: Tests are mandatory. Every production behavior task is preceded by a focused
automated test that must be run and observed failing for the expected behavioral reason.
A compile failure, broken fixture, unavailable database, or already-green test is not
valid red evidence.

**Responsive UI**: User-facing phases cover 360x800, 768x1024, 1440x900, and 320x800,
with keyboard, touch, non-hover, state retention, and system/light/dark themes.

**Live Delivery**: Every client-visible mutation includes transaction-coupled versioned
SSE, authorized replay, duplicate-safe client handling, reconnection/offline states, and
bounded slow-consumer behavior.

## Phase 1: Setup and Test Support

**Purpose**: Establish deterministic security/mail test infrastructure and lifecycle
tracking without implementing authentication behavior.

- [x] T001 Update feature 004 to in-progress when T013 records the first valid red test in `specs/004-owner-access/spec.md`, `ROADMAP.md`, and `specs/README.md`
- [x] T002 [P] Add deterministic clock, cryptographic-random reader, and secret-output assertion helpers for tests in `server/internal/authtest/authtest.go` and `server/internal/authtest/authtest_test.go`
- [x] T003 [P] Add captured/failing transactional-email test doubles and safe message assertions in `server/internal/mail/mailtest/capture.go` and `server/internal/mail/mailtest/capture_test.go`
- [x] T004 Close the deferred fixture-builder task because existing repository-backed integration helpers cover the required US1 fixtures; no direct test SQL or production behavior is added

---

## Phase 2: Foundational Security Infrastructure

**Purpose**: Tested configuration and standard security primitives required by every
story. This phase blocks all user stories.

### Failing tests

- [x] T005 [P] Add auth secret, secure-cookie, expiry, setup/recovery/code TTL, SMTP, and production fail-closed configuration tests in `server/internal/config/auth_test.go`; run and record the expected red behavior
- [x] T006 [P] Add Argon2id encode/verify/rehash, random capability/session, purpose-separated digest, six-digit leading-zero, and constant-time verification tests in `server/internal/auth/crypto_test.go`; run and record the expected red behavior
- [x] T007 [P] Add SMTP sender contract tests for bounded messages, context cancellation, safe classified failures, and absence of secrets in errors/logs in `server/internal/mail/smtp_test.go`; run and record the expected red behavior
- [x] T008 [P] Add generic auth error, secure cookie, CSRF, and principal-context middleware contract tests in `server/internal/httpx/auth_test.go`; run and record the expected red behavior

### Implementation

- [x] T009 Implement validated authentication/session/mail configuration with secret-safe errors in `server/internal/config/auth.go` and wire it through `server/internal/config/config.go`
- [x] T010 Implement Argon2id owner hashes, random six-digit/member and high-entropy capability/session generation, purpose-separated HMAC digests, and constant-time verification in `server/internal/auth/crypto.go`; add `golang.org/x/crypto` to `server/go.mod` and `server/go.sum`
- [x] T011 Implement the narrow transactional email contract, safe templates, and bounded SMTP adapter in `server/internal/mail/delivery.go`, `server/internal/mail/templates.go`, and `server/internal/mail/smtp.go`
- [x] T012 Implement generic authentication errors, secure cookie helpers, CSRF validation, and authenticated principal context in `server/internal/httpx/auth.go`

**Checkpoint**: Security primitives are green, but no account can yet be created.

---

## Phase 3: User Story 1 - Bootstrap and Authenticate the First Owner (Priority: P1) MVP

**Goal (historical baseline)**: Exactly one host-authorized owner can be created, sign
in with a strong credential, manage/revoke sessions, and permanently close setup. The
checked recovery/setup/login portions below predate and are superseded by Phase 4.

**Independent Test (historical)**: The recorded suites prove the one-owner race, route
privacy, sessions, and scoped events; clarified US1 behavior is tested in Phase 4.

### Failing tests for User Story 1

- [x] T013 [US1] Add clean/baseline-upgrade migration and 100-way concurrent owner-bootstrap invariant tests in `server/internal/db/identity_access_migrations_test.go`; run against migrations through `0006` and record the expected missing-schema red behavior
- [x] T014 [P] [US1] Add owner/bootstrap/capability/session domain validation and state-transition tests in `server/internal/identity/model_test.go` and `server/internal/auth/session_test.go`; run and record the expected red behavior
- [x] T015 [US1] Add repository/service tests for setup capability issue/expiry/consume, exactly-one owner, Argon2id credential creation, setup closure, atomic audit, and session creation in `server/internal/identity/bootstrap_integration_test.go`; run and record the expected red behavior
- [x] T016 [P] [US1] Add owner login tests for generic failures, rehash, idle/absolute expiry, token rotation, individual/all-device revocation, inactive rejection, and safe session metadata in `server/internal/auth/owner_integration_test.go`; run and record the expected red behavior
- [x] T017 [P] [US1] Add owner recovery tests for uniform requests, single-use/expiry/supersession, delivery failure, credential replacement, and revocation of every prior session/capability in `server/internal/auth/recovery_integration_test.go`; run and record the expected red behavior
- [x] T018 [P] [US1] Add host-command tests proving `auth setup-link` prints a capability once, never logs it, expires it, and fails safely after bootstrap closure in `server/cmd/market-lens/auth_test.go`; run and record the expected red behavior
- [x] T019 [P] [US1] Add HTTP contract tests for setup status/completion, owner login/recovery, account snapshot, logout, session list/revoke, cookies, CSRF, error envelopes, and a protected-by-default route matrix proving anonymous/expired/revoked sessions receive no market page, instrument/history/import/quality, or SSE data while active sessions can read shared data in `server/internal/api/auth_test.go` and `server/internal/api/authorization_test.go`; run and record the expected red behavior
- [x] T020 [US1] Add outbox migration/constraint and owner/user scoped setup/session/recovery event tests including authenticated shared-event access, `Last-Event-ID` replay, anonymous denial, and revoked-session denial in `server/internal/events/authorization_integration_test.go`; run and record the expected red behavior
- [x] T021 [P] [US1] Add frontend service/store/form tests for setup, owner login/recovery, session management, CSRF, duplicate-safe event invalidation, reconnect/stale/offline state, and secret-free browser storage in `src/services/auth.test.ts`, `src/stores/auth.test.ts`, and `src/components/account/OwnerAuth.test.ts`; run and record the expected red behavior
- [x] T022 [US1] Add Playwright owner setup/login/recovery/session journeys plus anonymous market-route denial and authenticated market access for all themes at 360x800, 768x1024, and 1440x900, keyboard/touch, and 320x800 overflow checks in `e2e/owner-access.spec.ts`; run and record the expected missing-UI red behavior

### Implementation for User Story 1

- [x] T023 [US1] Add ordered users, singleton bootstrap, capabilities, owner credentials, sessions, audit, delivery, challenge/failure/rate-limit, and invitation schema with one-owner/one-active-token constraints in `server/internal/db/migrations/0007_identity_access.sql`
- [x] T024 [P] [US1] Implement user, capability, owner credential, session, audit, and delivery domain types in `server/internal/identity/model.go` and `server/internal/auth/model.go`
- [x] T025 [US1] Implement explicit-SQL bootstrap/user/credential/session/recovery/audit/delivery persistence and transactional locking in `server/internal/identity/repository.go` and `server/internal/auth/repository.go`
- [x] T026 [US1] Implement host-authorized owner bootstrap, owner login, recovery, secure session lifecycle, and transactional email handoff in `server/internal/identity/service.go` and `server/internal/auth/service.go`
- [x] T027 [US1] Implement `auth setup-link` through the shared identity service and wire validated auth/mail dependencies at startup in `server/cmd/market-lens/main.go`
- [x] T028 [US1] Add explicit shared/user/owner event scope constraints and replay indexes in `server/internal/db/migrations/0008_client_event_authorization.sql`, then implement transactional event writes and authorization filters in `server/internal/events/repository.go` and `server/internal/events/service.go`
- [x] T029 [US1] Implement thin setup, owner auth/recovery, account, logout, and session handlers in `server/internal/api/auth.go` and `server/internal/api/sessions.go`; register explicit public routes and apply authenticated-principal middleware by default to market pages, instrument/history/import/quality REST routes, SSE, and every non-public API in `server/internal/api/router.go`
- [x] T030 [US1] Implement typed auth REST access, in-memory CSRF handling, authenticated store, protected market route guards, and duplicate-safe authorized SSE invalidation in `src/types/auth.ts`, `src/services/auth.ts`, `src/services/events.ts`, `src/stores/auth.ts`, `src/composables/useAuth.ts`, and `src/router/index.ts`
- [x] T031 [US1] Implement mobile-first owner setup, sign-in, recovery, and session-management UI in `src/components/account/OwnerAuth.vue`, `src/components/account/SessionList.vue`, `src/views/OwnerSetupView.vue`, `src/views/LoginView.vue`, and `src/views/AccountSettingsView.vue`
- [x] T032 [US1] Make all US1 Go/Vitest/Playwright suites green and record bootstrap race, recovery revocation, zero anonymous market REST/SSE disclosure, authenticated shared-data access, secret, and viewport evidence in `specs/004-owner-access/quickstart.md`

**Checkpoint (historical)**: The earlier implementation established protected sessions
and zero anonymous market REST/SSE disclosure. Phase 4 is the current US1 release gate.

---

## Phase 4: Corrective User Story 1 - Generic Sign-in and Encrypted Setup (Priority: P1)

**Goal**: Conform the checked US1 baseline to the 2026-08-30 clarifications before
member, invitation, deployment, or Feature 002 backfill work continues.

**Independent Test**: Complete setup with validated EODHD plus encrypted EODHD/SMTP
credentials; observe the same email-to-OTP progression for owner, member, and unknown
addresses; deliberately choose owner password; reset it only from an interactive pod
shell; and prove no provider/authentication secret or public market data is disclosed.

### Failing tests for corrective User Story 1

- [x] T033 [US1] Add clean-install and migration-`0008` upgrade tests for external credential rows, kind/version/bounds constraints, and forward-only retirement of usable owner-recovery capabilities in `server/internal/db/external_credentials_migration_test.go`; run against `0008` and record the expected missing-schema red
- [x] T034 [P] [US1] Add independent credential-key configuration and AES-GCM tests for random-nonce round trip, wrong key, tamper, row-bound AAD, version/bounds failure, atomic rotation, and secret-free errors/logs in `server/internal/config/external_credentials_test.go` and `server/internal/credentials/envelope_test.go`; run and record the expected red
- [x] T035 [P] [US1] Add setup integration tests for EODHD validation success/entitlement failure/timeout/outage, capability expiry/race recheck, zero failed persistence, atomic encrypted EODHD/SMTP commit, and raw-row plaintext absence in `server/internal/identity/bootstrap_credentials_integration_test.go`; run and record the expected red
- [x] T036 [P] [US1] Add service/API/CLI tests for identical sign-in-start responses, member-only OTP delivery, generic owner-password failure, old recovery endpoint/capability rejection, TTY-only twice-entered reset, all-session revocation, safe integration status, and secret-free output in `server/internal/auth/sign_in_integration_test.go`, `server/internal/api/auth_test.go`, and `server/cmd/market-lens/auth_test.go`; run and record the expected red
- [x] T037 [P] [US1] Replace frontend unit expectations with expanded setup and one email-to-OTP flow that always offers the secondary owner-password action, has no forgot-password/recovery path, retains safe input, and never stores submitted secrets in `src/services/auth.test.ts`, `src/stores/auth.test.ts`, and `src/components/account/OwnerAuth.test.ts`; run and record the expected red
- [x] T038 [US1] Replace focused Playwright expectations with provider-failure setup retry, successful expanded setup, owner/member/unknown-identical progression, explicit password switching, absent recovery routes, protected markets, required themes/viewports/touch/keyboard, and 320x800 overflow checks in `e2e/owner-access.spec.ts`; run and record the expected red

### Implementation for corrective User Story 1

- [x] T039 [US1] Add forward migration `server/internal/db/migrations/0009_external_credentials_and_owner_reset.sql` without editing `0007`/`0008`; revoke historical recovery capabilities and enforce credential constraints
- [x] T040 [P] [US1] Implement validated external key/version configuration, AES-256-GCM envelopes with row-bound AAD, explicit-SQL persistence, safe status, and transactional all-row rotation in `server/internal/config/external_credentials.go` and `server/internal/credentials/`
- [x] T041 [US1] Validate EODHD through the bounded existing adapter before setup transaction and atomically persist encrypted EODHD/SMTP with owner/session/audit/events in `server/internal/identity/model.go`, `server/internal/identity/repository.go`, and `server/internal/identity/service.go`
- [x] T042 [US1] Implement generic sign-in start, retain explicit generic owner-password verification, remove public/email recovery runtime behavior, expose owner-only safe integration status, and update the public route allowlist in `server/internal/auth/` and `server/internal/api/`
- [x] T043 [US1] Implement TTY-only owner-password reset and credential-key rotation through shared transactional services using `golang.org/x/term` in `server/cmd/market-lens/`; never accept secret flags, pipes, or password environment values
- [x] T044 [US1] Implement typed expanded setup and generic email/OTP/password UI, delete the recovery view/route, and preserve memory-only CSRF/session/event behavior in `src/types/auth.ts`, `src/services/auth.ts`, `src/stores/auth.ts`, `src/router/index.ts`, `src/components/account/OwnerAuth.vue`, `src/views/OwnerSetupView.vue`, and `src/views/LoginView.vue`
- [x] T045 [US1] Wire the separate credential key/version into startup, Kubernetes, Compose, installer upgrade behavior, examples, and contract tests without baking/logging/accidentally rotating it in `server/cmd/market-lens/main.go`, `deploy/k8s/`, `docker-compose.yml`, and `.env.example`
- [x] T046 [US1] Make all corrective Go/Vitest/Playwright/deployment suites green and record upgrade, provider retry, ciphertext inspection, generic-flow equivalence, CLI reset/session revocation, zero recovery, zero anonymous market data, and responsive evidence in `specs/004-owner-access/quickstart.md`

**Checkpoint**: Clarified authentication is the release gate before Feature 002 backfill
work or production deployment resumes.

---

## Phase 5: User Story 2 - Passwordless Member Login and Lockout (Priority: P1)

**Goal**: An active invited member signs in on any device using only the latest emailed
six-digit code, with durable throttling, temporary blocking, administrative lockout, and
owner-only unlock.

**Independent Test**: Request and consume a code once, reject expired/replayed/older
codes, trigger the third-attempt 15-minute block and rolling tenth-attempt lock across
devices/restarts, then owner-unlock and require a fresh code.

### Failing tests for User Story 2

- [x] T047 [P] [US2] Add member challenge tests for exactly six digits, leading zeros, keyed-at-rest representation, 10-minute expiry, latest-only invalidation, one use, concurrency, and safe mail content in `server/internal/auth/member_code_test.go`; run and record the expected red behavior
- [x] T048 [US2] Add PostgreSQL integration tests for serialized verification, three consecutive failures, 15-minute durable block, rolling-ten/24-hour administrative lock, process restart, and submissions during blocked/locked periods in `server/internal/auth/member_lockout_integration_test.go`; run and record the expected red behavior
- [x] T049 [P] [US2] Add independent account/origin sliding-window tests for one delivery/minute, five/hour, distributed guessing, spraying, generic/coarse retry responses, and unknown-email timing shape in `server/internal/auth/rate_limit_integration_test.go`; run and record the expected red behavior
- [x] T050 [P] [US2] Add owner-unlock authorization tests proving member/non-owner rejection, failure/code clearing, audit/outbox effects, no reactivation, existing-session preservation, and no private-data access in `server/internal/identity/member_admin_integration_test.go`; run and record the expected red behavior
- [x] T051 [P] [US2] Add HTTP contract tests for member code request/verify and owner unlock covering uniform failures, secure sessions, CSRF, 401/429 behavior, and no password fields in `server/internal/api/member_auth_test.go`; run and record the expected red behavior
- [x] T052 [P] [US2] Add frontend email/code and owner lock-state tests for numeric input/autocomplete, paste, resend countdown, generic failures, session establishment, event refresh, and accessible blocked/locked administration in `src/components/account/EmailCodeForm.test.ts` and `src/components/account/MemberList.test.ts`; run and record the expected red behavior
- [x] T053 [US2] Extend Playwright with member login on a second browser, delayed older mail, expiry/replay, 3/10 thresholds, owner unlock, fresh-code requirement, themes/viewports, offline input retention, and 320x800 checks in `e2e/owner-access.spec.ts`; run and record the expected red behavior

### Implementation for User Story 2

- [x] T054 [P] [US2] Implement member challenge, login state, rolling failure, and rate-event domain behavior in `server/internal/auth/member.go`
- [x] T055 [US2] Implement explicit-SQL newest-only challenge issuance/consume, serialized durable failure transitions, independent sliding-window buckets, cleanup, and owner unlock persistence in `server/internal/auth/repository.go`
- [x] T056 [US2] Implement generic member code request/verification, transactional email handoff, session creation, 3/10 threshold behavior, and owner-only unlock in `server/internal/auth/service.go` and `server/internal/identity/service.go`
- [x] T057 [US2] Implement member-code and owner-unlock audit/outbox event publication with user/owner scope and secret-minimal payloads in `server/internal/events/service.go`
- [x] T058 [US2] Implement thin member code request/verify and owner unlock handlers in `server/internal/api/auth.go` and `server/internal/api/members.go`
- [x] T059 [US2] Implement typed member-code API/store behavior and mobile-first email/code form plus owner blocked/locked/unlock presentation in `src/services/auth.ts`, `src/stores/auth.ts`, `src/components/account/EmailCodeForm.vue`, and `src/components/account/MemberList.vue`
- [x] T060 [US2] Make all US2 Go/Vitest/Playwright suites green and record cross-device/restart threshold, anti-enumeration, owner-unlock, SSE, and viewport evidence in `specs/004-owner-access/quickstart.md`

**Checkpoint**: Members can authenticate without passwords and brute-force thresholds
cannot be bypassed through devices, origins, or restarts.

---

## Phase 6: User Story 3 - Invite and Manage Members by Email (Priority: P1)

**Goal**: The owner can create, inspect, resend, revoke, and safely deliver invitations;
the intended recipient activates exactly one passwordless member account.

**Independent Test**: Send and accept an invitation once, reject every expired/revoked/
replayed/wrong-email conflict, exercise resend and provider failure, then deactivate the
member and prove access ends.

### Failing tests for User Story 3

- [x] T061 [P] [US3] Add invitation domain/repository tests for normalized email, pending uniqueness, seven-day expiry, resend lineage, old-token invalidation, revoke, replay, wrong email, and conflicting owner/member identities in `server/internal/identity/invitation_integration_test.go`; run and record the expected red behavior
- [x] T062 [US3] Add concurrent acceptance tests proving one member, verified email, no password credential, one consumed invite, optional initial session, and atomic audit/outbox effects in `server/internal/identity/invitation_acceptance_integration_test.go`; run and record the expected red behavior
- [x] T063 [P] [US3] Add delivery lifecycle tests for bounded resend, safe pending/sent/failed/abandoned state, SMTP outage, process loss before handoff, no secret persistence, and existing-session availability in `server/internal/mail/invitation_delivery_integration_test.go`; run and record the expected red behavior
- [x] T064 [P] [US3] Add member deactivate/reactivate tests for owner-only authorization, session/code revocation, open-SSE termination, reactivation without login, and owner self-action rejection in `server/internal/identity/member_status_integration_test.go`; run and record the expected red behavior
- [x] T065 [P] [US3] Add HTTP contract tests for invitation list/create/resend/revoke/accept and member list/status, including pagination, CSRF, safe delivery errors, and role enforcement in `server/internal/api/invitations_test.go` and `server/internal/api/members_test.go`; run and record the expected red behavior
- [x] T066 [P] [US3] Add frontend invitation/member administration and acceptance tests for loading/empty/error, safe delivery state, confirmation, focus, event refresh, and no password field in `src/components/account/InvitationForm.test.ts` and `src/views/AcceptInvitationView.test.ts`; run and record the expected red behavior
- [x] T067 [US3] Extend Playwright with invite/send/fail/resend/revoke/accept/deactivate/reactivate journeys, all required viewports/themes, touch/keyboard, orientation state, and 320x800 checks in `e2e/owner-access.spec.ts`; run and record the expected red behavior

### Implementation for User Story 3

- [x] T068 [P] [US3] Implement invitation and delivery state domain behavior in `server/internal/identity/invitation.go`
- [x] T069 [US3] Implement explicit-SQL invitation lifecycle, concurrent acceptance, member activation/status transitions, session/code revocation, audit, and scoped events in `server/internal/identity/repository.go`
- [x] T070 [US3] Implement owner invitation/member administration and passwordless acceptance services with bounded transactional email handoff in `server/internal/identity/service.go`
- [x] T071 [US3] Implement thin invitation and member administration/acceptance handlers in `server/internal/api/invitations.go` and `server/internal/api/members.go`
- [x] T072 [US3] Implement typed invitation/member APIs and mobile-first invitation acceptance/administration UI in `src/services/auth.ts`, `src/components/account/InvitationForm.vue`, `src/components/account/MemberList.vue`, `src/views/AcceptInvitationView.vue`, and `src/views/AccountSettingsView.vue`
- [x] T073 [US3] Make all US3 Go/Vitest/Playwright suites green and record concurrency, passwordless activation, provider-outage, deactivation/SSE, and viewport evidence in `specs/004-owner-access/quickstart.md`

**Checkpoint**: Membership is invitation-only, auditable, and operationally manageable.

---

## Phase 7: User Story 4 - Enforce Private Data and Event Isolation (Priority: P1)

**Goal**: Shared market reference data stays common while every private record, query,
export-shaped result, cache key, and SSE event is scoped to its authenticated user; owner
administration never implies financial-data access.

**Independent Test**: Create equivalent private test resources for two members and prove
zero cross-user existence/content/count/event disclosure across every access path while
shared market events reach both.

### Failing tests for User Story 4

- [x] T074 [P] [US4] Add authorization policy table tests for anonymous, active member, deactivated member, owner-admin, shared data, self-private data, and other-user private data in `server/internal/authorization/policy_test.go`; run and record the expected red behavior
- [x] T075 [US4] Add a cross-user repository/service matrix with representative owned resources, guessed UUIDs, lists, aggregates, exports, searches, audit/admin metadata, and cache keys in `server/internal/authorization/isolation_integration_test.go`; run and record the expected red behavior
- [x] T076 [P] [US4] Add authorized SSE tests for shared/user/owner scope, `Last-Event-ID` replay, duplicates, gaps, reconnect, role/status changes, slow consumers, and five-second revocation/deactivation termination in `server/internal/events/isolation_integration_test.go`; run and record the expected red behavior
- [x] T077 [P] [US4] Extend the US1 protected-route matrix with owner/member administration boundaries, backend principal propagation into private repository scopes, scoped 404 vs forbidden behavior, CSRF, and no client-supplied owner override in `server/internal/api/authorization_test.go`; run and record the expected red behavior
- [x] T078 [P] [US4] Add frontend tests proving route protection, auth-expiry handling, private event filtering defense-in-depth, owner-admin boundaries, and snapshot refresh without primary polling in `src/stores/auth.test.ts` and `src/services/events.test.ts`; run and record the expected red behavior
- [x] T079 [US4] Extend Playwright with two-member/owner isolation, guessed routes, open-stream revoke/deactivate, shared market event, reconnect, and responsive authorization-state journeys in `e2e/owner-access.spec.ts`; run and record the expected red behavior

### Implementation for User Story 4

- [x] T080 [P] [US4] Implement explicit authorization decisions and owned/shared resource scope types in `server/internal/authorization/policy.go`
- [x] T081 [US4] Extend the US1 authenticated-principal boundary with owner-admin middleware and explicit private repository/service scope inputs in `server/internal/api/router.go`, `server/internal/httpx/auth.go`, `server/internal/identity/repository.go`, and `server/internal/events/repository.go`
- [x] T082 [US4] Implement scoped SSE connection/replay authorization, periodic session/status recheck, bounded consumer disconnect, and safe event serialization in `server/internal/api/events.go` and `server/internal/events/service.go`
- [x] T083 [US4] Implement frontend protected routing, authorized duplicate-safe SSE lifecycle, stale/offline/auth-expired states, and snapshot invalidation without polling in `src/router/index.ts`, `src/services/events.ts`, `src/stores/auth.ts`, and `src/App.vue`
- [x] T084 [US4] Make all US4 Go/Vitest/Playwright suites green and record zero-disclosure REST/query/export/cache/SSE matrix evidence in `specs/004-owner-access/quickstart.md`

**Checkpoint**: Identity is a backend-enforced security boundary for future private data.

---

## Phase 8: Polish, Security Acceptance, and Delivery

**Purpose**: Prove cross-story security, responsiveness, operability, and deployability.

- [x] T085 [P] Add secret-regression tests over database inspection, logs, errors, CLI output, REST/SSE, audit, mail state, browser storage, and built assets in `server/internal/auth/secrets_test.go` and `src/services/auth.secrets.test.ts`
- [x] T086 [P] Add full account acceptance tests covering bootstrap, invitations, owner/member auth, 3/10 lockout, unlock, sessions, email outage, restart, and authorized event replay in `server/internal/auth/acceptance_integration_test.go`
- [x] T087 [P] Add time/concurrency stress tests for 100 setup races, simultaneous code issuance/verification, rate-bucket boundaries, invitation acceptance, session revocation, and lockout transitions in `server/internal/auth/concurrency_integration_test.go`
- [x] T088 Reconcile the implemented API with `specs/004-owner-access/contracts/openapi.yaml` and document secret-safe environment/bootstrap/SMTP operations in `server/.env.example`, `.env.example`, `README.md`, and `specs/004-owner-access/quickstart.md`
- [x] T089 Run accessibility and responsive Playwright acceptance at 360x800, 768x1024, 1440x900, and 320x800 in system/light/dark themes, including touch/keyboard/non-hover and orientation/input retention, then record evidence in `specs/004-owner-access/quickstart.md`
- [x] T090 Run `make verify`, all owner-access Playwright tests, production/Docker builds, `docker compose config --quiet`, `deploy/k8s/test.sh`, clean/upgrade migrations, and secret/cross-user suites; record results in `specs/004-owner-access/quickstart.md`
- [x] T091 Update feature 004 to in-review in `specs/004-owner-access/spec.md`, `ROADMAP.md`, and `specs/README.md`; run `git diff --check` and confirm no secrets, generated builds, browser output, coverage, or database data are tracked

---

## Dependencies and Execution Order

### Phase Dependencies

- **Setup (Phase 1)** starts immediately; T001 is completed only after the valid US1 red.
- **Foundation (Phase 2)** depends on test-support tasks T002–T003 and blocks all stories.
- **US1 (Phase 3)** depends on Foundation and is the security MVP; it establishes users,
  owner sessions, audit, and scoped event persistence used later.
- **Corrective US1 (Phase 4)** depends on the checked baseline and blocks every later
  phase, deployment, and Feature 002 backfill operation.
- **US2 (Phase 5)** depends on corrected US1 persistence/sessions; integration fixtures may create
  an active member through repository helpers before the invitation UI exists.
- **US3 (Phase 6)** depends on corrected US1 and integrates with the already-proven US2 member-code
  flow; its invitation service remains independently testable through acceptance.
- **US4 (Phase 7)** depends on the protected-by-default US1 event/session foundation and
  completes after US2/US3 so the cross-user matrix covers their private/admin routes and
  state changes; anonymous market denial is not deferred to this phase.
- **Acceptance (Phase 8)** depends on all stories.

### Within Every Behavior Slice

- Write/run the listed test first and record a meaningful behavioral red.
- Implement only the minimum production behavior required for green.
- Run the focused suite before refactoring and keep it green.
- Schema corrections are new forward migrations; never edit an applied migration.
- No handler, event, log, mail status, or UI test helper may expose a real secret.

### Parallel Opportunities

- T002–T004 touch independent test-support areas.
- Foundation configuration, crypto, SMTP, and HTTP tests T005–T008 can be prepared in
  parallel before their corresponding implementation tasks.
- Within US1, domain, login, recovery, CLI, HTTP, and frontend red tests T014–T021 are
  separable after the migration red is established.
- Within US2, code, rate, unlock, HTTP, and frontend tests T047–T052 are separable.
- Within US3, invitation lifecycle, mail failure, status, HTTP, and frontend tests are
  separable; concurrent acceptance T062 follows the lifecycle fixture.
- US4 policy, SSE, API, and frontend red tests T074/T076–T078 can run in parallel before
  the end-to-end matrix.
- Secret, acceptance, and stress tests T085–T087 touch separate files.

## Parallel Examples

### User Story 1

```text
T014: identity/session model tests
T016: owner login/session integration tests
T017: owner recovery integration tests
T018: host setup command tests
T019: HTTP contract tests
T021: frontend service/store/form tests
```

### User Story 2

```text
T047: six-digit challenge tests
T049: independent account/origin limiter tests
T050: owner unlock tests
T051: HTTP contract tests
T052: member/owner frontend tests
```

### User Stories 3 and 4

```text
T061: invitation lifecycle tests
T063: email delivery degradation tests
T064: member status tests
T074: policy table tests
T076: scoped SSE isolation tests
T077: protected-route tests
```

## Implementation Strategy

### MVP First

1. Complete Setup and Foundation.
2. Complete corrected US1 and independently prove exactly-one owner, encrypted provider
   setup, generic sign-in, deployment-only reset, sessions, audit, protected market
   REST/SSE delivery, and zero anonymous application-data disclosure.
3. Validate the security boundary before enabling invited member access.

### Incremental Delivery

1. Owner bootstrap/authentication MVP.
2. Passwordless member code flow with durable 3/10 controls.
3. Invitation/member lifecycle and email degradation.
4. Complete cross-user authorization/SSE matrix.
5. Full responsive/security/container acceptance.

The task list deliberately excludes PWA installation, Web Push, user holdings/trades,
tracking rules, sell suggestions, market-alert preferences, multiple owners, SSO, and
public signup; each requires its own reviewed feature specification.
