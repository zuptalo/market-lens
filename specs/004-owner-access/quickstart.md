# Quickstart: Owner Access and Invitations

This guide is the implementation/verification contract. It does not authorize manual
database mutation. Every schema change is an embedded ordered migration and every
behavior begins with a valid failing automated test.

## Prerequisites

- Go 1.26, Node/npm, Docker with Compose, and Playwright browsers.
- PostgreSQL started with `make db-up`.
- Feature 002 migration `0006_client_events.sql` implemented before feature 004's
  authorization extension.
- Test-only captured EODHD and SMTP adapters for automated flows. Submitted provider
  credentials must remain outside source, fixtures, logs, images, and browser storage.

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
AUTH_MEMBER_CODE_TTL=10m
AUTH_MEMBER_TEMP_BLOCK=15m
EXTERNAL_CREDENTIAL_KEY=<base64-encoded-32-random-bytes-from-a-secret-store>
EXTERNAL_CREDENTIAL_KEY_VERSION=1
```

SMTP and EODHD values are collected by the one-time setup wizard, encrypted in
PostgreSQL, and never configured through ordinary deployment variables. Production
startup fails closed if `AUTH_SECRET` or the separate external-credential key is absent,
malformed, or too short, if the configured key version disagrees with persisted rows,
or if secure cookies are disabled. Tests inject deterministic randomness, clock,
provider validator, terminal, and mail sender through constructors; no production debug
endpoint returns codes, capabilities, or credential fields.

## Red-green implementation checkpoints

### Recorded red evidence

The entries below are historical evidence from the implementation that existed before
the 2026-08-30 clarification. They remain useful for unaffected invariants. Any statement
about public/email owner recovery, environment-based SMTP, separate owner-first login,
`/recover`, or setup without provider credentials is **superseded** and is not evidence
that the current specification is complete.

- **T033 corrective red (2026-08-30)**: `go test ./internal/db -run '^TestExternalCredentialMigration' -count=1` compiled and failed with `embedded migration count = 8, want 9`. The new suite also defines clean-install and true migration-`0008` upgrade assertions for the encrypted credential schema, kind/version/bounds constraints, and retirement of historical usable owner-recovery capabilities. The integration cases are database-gated, but the embedded-migration assertion guarantees a valid red even without `TEST_DATABASE_URL`.
- **T034 corrective red (2026-08-30)**: `go test ./internal/credentials ./internal/config -run 'Test(CredentialEnvelope|Load.*ExternalCredential)' -count=1` compiled against a deliberately nonfunctional envelope shell and failed behaviorally because sealing/opening/rotation are unimplemented, invalid keys are accepted, and `Config` has no external credential key/version. The tests require random-nonce AES-GCM overhead, row/kind/payload/key-version AAD binding, tamper/wrong-key rejection, 16 KiB bounds, no partial batch on rotation failure, exactly 32 decoded key bytes, production fail-closed behavior, and secret-free errors.
- **T035 corrective red (2026-08-30)**: with the healthy Compose PostgreSQL, `TEST_DATABASE_URL=... go test ./internal/identity -run '^TestBootstrapCredentials' -count=1` compiled and failed behaviorally because invalid/unavailable EODHD validation was ignored, setup still committed after the validator should have advanced the capability past expiry, and `external_service_credentials` did not exist. The tests require failure to leave zero owners and one usable setup capability, final in-transaction expiry/race recheck, exactly two encrypted credential rows on success, atomic session/audit/events, and absence of submitted EODHD/SMTP plaintext.
- **T036 corrective red (2026-08-30)**: `TEST_DATABASE_URL=... go test ./internal/auth ./internal/api ./cmd/market-lens -run 'Test(SignInStart|OwnerPasswordReset|GenericSignIn|ExecuteAuthCommandRoutesOwnerPasswordReset|ExecuteOwnerPasswordReset)' -count=1` compiled and failed on the deliberate generic-sign-in/reset shells, a protected `POST /auth/sign-in/start` (`401`, wanted uniform `202`), and the setup-link-only CLI router. A separate API red returned `404` for authenticated owner integration status. The tests require identical owner/unknown/malformed progression, no owner OTP rows, retired recovery endpoint 404s, TTY/twice-entered/strong reset input, atomic password change plus all-session revocation/audit/events, secret-free CLI output, and status fields that omit all credential configuration/ciphertext.
- **T037 corrective red (2026-08-30)**: `npm run test:unit -- src/components/account/OwnerAuth.test.ts src/services/auth.test.ts src/stores/auth.test.ts` ran 11 tests with six intended failures: generic email/OTP/owner-password modes and code field were absent, the explicit password action and store transition did not exist, the setup form lacked EODHD/SMTP fields, and the client omitted those fields from setup. Existing session/CSRF/error tests remained green. Obsolete recovery calls were removed from the current client-mapping expectation rather than retained as a contradictory green.
- **T038 corrective red (2026-08-30)**: after a green production build, `npx playwright test e2e/owner-access.spec.ts --project=mobile-chromium` ran four journeys: the existing theme/state/320px assertion stayed green, while three intended reds timed out on the missing generic `Continue`/EODHD fields or found the obsolete forgot-password link. The revised journeys require generic email then explicit password switching before market access, retryable provider validation with retained setup inputs, secret-free storage, no recovery interaction, touch/keyboard operation, and the existing responsive/theme protections; the same file runs under tablet and desktop projects for the eventual green gate.
- **T039 green (2026-08-30)**: `TEST_DATABASE_URL=... go test ./internal/db -run 'Test(ExternalCredentialMigration|LoadMigrations)' -count=1` and the complete `./internal/db` suite pass. Forward migration `0009_external_credentials_and_owner_reset.sql` creates the bounded/versioned unique encrypted-credential rows, safely records then removes historical recovery state, replaces recovery-bearing constraints, and adds `owner_password_reset` as a session revocation reason. Clean install and an actual migration-`0008` upgrade are both proven; migrations `0007`/`0008` remain unchanged.
- **T040 repository red/green (2026-08-30)**: after the primitive/config reds went green, the SQL repository was deliberately returned to a compile-only shell and `TEST_DATABASE_URL=... go test ./internal/credentials -run '^TestCredentialRepository' -count=1` failed on unimplemented persistence. Restoring the implementation made the complete `./internal/credentials ./internal/config` suites green. The result validates exactly 32 decoded key bytes, random-nonce AES-256-GCM with row/kind/payload/key-version AAD, tamper/wrong-key/bounds failures, safe errors/status, explicit transaction-scoped inserts, and all-row rotation with audit/outbox; a tampered second row leaves both rows at the old version and the first ciphertext unchanged.
- **T041 green (2026-08-30)**: `TEST_DATABASE_URL=... go test ./internal/identity ./internal/marketdata/eodhd ./cmd/market-lens -run 'Test(Bootstrap|CredentialValidator|NewIdentityService|NewAuthenticationServiceAccepts)' -count=1` passes. EODHD validation uses the official User API plus a bounded ten-year-old non-US EOD entitlement probe, classifies invalid/forbidden/timeout safely, and never exposes the submitted key or provider URL. Validation occurs before the transaction; the capability/expiry is locked and rechecked afterward. Success commits exactly two encrypted rows with owner/credential/session/audit/events, while invalid, unavailable, expired-after-validation, and race-losing requests commit none.
- **T042 green (2026-08-30)**: the PostgreSQL-backed `./internal/auth ./internal/api` suites pass with one uniform generic sign-in progression for owner, unknown, and malformed email input, zero owner OTP challenges/deliveries, explicit owner-password verification, `404` retired recovery endpoints, deny-by-default application routes, and an owner-only integration response containing only kind/configured/ready/key version. The reset service changes the Argon2id credential, revokes every session with `owner_password_reset`, and writes the audit plus owner/user invalidation events in one transaction.
- **T043 green (2026-08-30)**: `go test ./cmd/market-lens -run 'TestExecute(AuthCommand|OwnerPasswordReset|CredentialKeyRotation)' -count=1` passes. Both secret-bearing commands reject non-TTY input, read twice without echo through `golang.org/x/term`, accept no secret flag or environment value, and emit only safe success metadata. Credential rotation delegates to the all-row transactional repository; invalid base64, mismatch, non-increasing/out-of-range version, tamper, or write failure performs no partial rotation.
- **T044 green (2026-08-30)**: all 36 Vitest tests and `vue-tsc --noEmit` pass. The generic login renders email first, always advances to OTP, and offers owner password only as an explicit secondary action. Expanded setup submits EODHD/SMTP fields without rendering the capability or retaining submitted secrets in browser storage; `/recover`, recovery UI, and recovery client/store methods are absent.
- **T045 green (2026-08-30)**: deployment contract and Compose validation pass with independent external key/version secret references. New installs generate version 1 once; upgrades preserve a complete pair, add both only when both are absent, reject incomplete pairs, and never rotate implicitly. PostgreSQL-backed credential tests prove startup accepts an open setup or a complete matching/decryptable pair and fails closed on wrong key, wrong version, tamper, or an incomplete pair.
- **T046 corrective US1 gate (2026-08-30)**: `TEST_DATABASE_URL=... make verify` passes all Go formatting/vet/tests against isolated PostgreSQL schemas, the production Vue build/typecheck, and all 36 Vitest tests. `npx playwright test e2e/owner-access.spec.ts` passes all 12 mobile/tablet/desktop journeys, including provider-failure retry with retained inputs, generic email/OTP/password equivalence, no recovery interaction, zero anonymous market requests, secret-free browser storage, themes/touch/keyboard use, and 320px overflow protection. `deploy/k8s/test.sh`, `docker compose config`, and `docker build -t market-lens:owner-access .` pass. Migration upgrade and raw-row tests prove historical recovery retirement and encrypted EODHD/SMTP ciphertext; CLI/PostgreSQL tests prove password reset revokes all sessions atomically and key rotation cannot partially commit.

- **T005 (2026-08-29)**: `go test ./internal/config -run '^TestLoadParsesAuthenticationAndSMTPConfiguration$' -count=1` failed because the current configuration has no `Auth` field. The broader focused run also proved production currently accepts missing/weak secrets and insecure cookies and does not validate authentication TTL or SMTP settings.
- **T006 (2026-08-29)**: `go test ./internal/auth -count=1` compiled and failed on the intended primitive behavior: the compile-only hasher exposed plaintext instead of Argon2id, tokens lacked 256-bit entropy, digests were not purpose-separated, and short server keys were accepted.
- **T008 (2026-08-29)**: `go test ./internal/httpx -run 'Test(Principal|CSRF|SessionCookies)' -count=1` compiled and failed because principals were not retained, unsafe requests bypassed CSRF validation, and no secure session cookie was emitted.
- **T007 (2026-08-29)**: `go test ./internal/mail -run '^TestSMTPSender' -count=1` compiled and failed because invalid messages were accepted, cancellation was ignored, provider failures were unclassified, and valid mail never reached the transport.
- **T013 (2026-08-29)**: with the Compose PostgreSQL healthy, `TEST_DATABASE_URL=... go test ./internal/db -run '^TestIdentityAccessMigrationCleanInstall$' -count=1` applied migrations through `0006` and failed specifically because the `users` identity table was absent. This was the first valid Feature 004 production-behavior red and advanced the feature lifecycle to in-progress.
- **T014 (2026-08-29)**: `go test ./internal/identity ./internal/auth -run 'Test(NormalizeEmail|UserValidation|BootstrapState|CapabilityValidation|Session)' -count=1` compiled against model shells and failed on email normalization, verified-owner lifecycle, bootstrap closure/replay, capability expiry/use, session expiry/touch/revocation, and safe-summary behavior.
- **T015 (2026-08-30)**: `TEST_DATABASE_URL=... go test -race ./internal/identity -run '^TestBootstrapService' -count=1` first failed behaviorally on setup issuance, then passed with one atomic capability-consume/owner/Argon2id-credential/session/bootstrap-close/audit transaction. Expired and concurrent replay created no second owner, and persistence inspection found no plaintext capability, password, session, or CSRF material.
- **T016 (2026-08-30)**: `TEST_DATABASE_URL=... go test -race ./internal/auth -run '^TestOwner(Login|Session)' -count=1` compiled against auth-service shells and failed on generic owner login/session behavior, then passed with password rehash-on-login, rotated session/CSRF material, keyed CSRF verification, idle and absolute expiry, individual/all-device revocation, active-account checks, and secret-free session summaries/persistence.
- **T017 (2026-08-30, superseded behavior)**: `TEST_DATABASE_URL=... go test -race ./internal/auth -run '^TestOwnerRecovery' -count=1` proved the earlier email-recovery design. The current specification forbids those endpoints/capabilities; corrective red tests must now prove their removal and the interactive reset transaction.
- **T018 (2026-08-30)**: `go test ./cmd/market-lens -run '^TestExecuteSetupLink' -count=1` compiled against a command shell and failed because no terminal setup URL was printed and closed bootstrap was reported as success. It now passes with one fragment-only capability output, safe ID/expiry structured logging, cancellation before issuance, and zero output on the generic closed result. The configured 15-minute lifetime and expiry boundary remain covered by the T005/T014/T015 suites.
- **T019 (2026-08-30, partially superseded)**: `go test ./internal/api -run '^Test(ApplicationAndSharedDataRoutes|ActiveSession|Owner|AccountSession)' -count=1` proved deny-by-default market routes, session cookies, CSRF, and safe snapshots. Its public recovery allowlist is obsolete and must become generic sign-in start plus password/code verification only.
- **T027/T029 (2026-08-30, partially superseded)**: migration-backed command/startup tests and the API suite proved the host-only setup command, identity/session wiring, and deny-by-default router. Environment SMTP and recovery handler portions are obsolete; setup-stored encrypted SMTP and the deployment-only password-reset command replace them.
- **T020/T028 (2026-08-30, partially superseded)**: `TEST_DATABASE_URL=... go test ./internal/events ./internal/identity ./internal/auth ./internal/api -count=1` proved scoped durable replay and session authorization. Recovery event behavior is obsolete; reset and integration-status events require new red tests.
- **T021/T030 (2026-08-30, partially superseded)**: the unit suites proved session restoration, protected routing, CSRF memory-only handling, and SSE reconnect behavior. Setup/login/recovery request shapes and forms require corrective generic-login/encrypted-setup tests.
- **T022/T031/T032 (2026-08-30, partially superseded)**: responsive tests proved protected routes, three viewport classes, themes, keyboard/touch use, and narrow overflow. `/recover`, separate owner login, and the old setup form are obsolete; the generic OTP/password switch and expanded setup wizard need new Playwright reds.
- **US1 historical checkpoint (2026-08-30)**: the complete stack was green for the earlier design and still proves bootstrap concurrency, secret-free browser storage, authorized replay, and anonymous market-data denial. It is not the current US1 completion gate until all corrective tests below are green.
- **US1 deployment gate (2026-08-30)**: the k3s contract test first failed because the existing deployment had no `AUTH_SECRET` reference. The installer now generates an independent 48-byte authentication key for new installs and adds only that missing key to older secrets without rotating database credentials; the Deployment consumes it and explicitly enables secure cookies. Compose passes externally supplied auth/origin values and its example requires local generation rather than embedding a credential. `deploy/k8s/test.sh`, `docker compose config --quiet`, the production image build, and a container fail-closed smoke check all pass; the no-secret image exits safely with `AUTH_SECRET is required in production`.

### 1. Corrective migration and credential envelopes

Write clean-install and `0008`-baseline upgrade tests first. They must fail because the
external-credential table and recovery-retirement constraint do not exist:

```bash
cd server
go test ./internal/db -run 'TestExternalCredentialMigration|TestOwnerRecoveryRetired' -count=1
```

Add only forward migration `0009_external_credentials_and_owner_reset.sql`; never edit
applied `0007`/`0008`. Then add encryption primitive reds for random nonce variation,
round-trip, tamper/wrong-key/wrong-AAD rejection, safe bounds, version mismatch, and
plaintext/log absence:

```bash
cd server
go test ./internal/credentials ./internal/config -run 'TestCredential|TestExternalCredential' -count=1
```

### 2. Atomic setup with provider validation

Extend the setup service tests before changing it. Valid EODHD validation must precede
the transaction; invalid, unauthorized, timed-out, or unavailable provider results must
leave the capability usable until expiry and persist no owner/ciphertext. A successful
request stores the owner, password hash, encrypted EODHD and SMTP envelopes, session,
audit, events, and closed bootstrap atomically. Race losers commit nothing.

```bash
cd server
go test -race ./internal/identity ./internal/credentials ./internal/marketdata \
  -run 'TestBootstrap|TestSetupCredential|TestEODHDSetupValidation' -count=1
```

Inspect responses, captured logs/events, and raw rows to prove no submitted password,
EODHD key, SMTP field, bearer value, or decrypted configuration appears.

### 3. Generic sign-in and deployment-only owner reset

First add service/API reds proving every submitted email receives the same `202` body
and the client instruction is always OTP entry; only an eligible member delivery row is
created. Retain the secondary owner password endpoint with generic failure and existing
session protections. Add negative contract tests proving the old recovery endpoints are
404 and recovery capabilities cannot be inserted/used.

Then add CLI reds with an injected terminal: non-TTY, mismatch, weak input, EOF, and
failure roll back; a valid twice-entered no-echo password changes the hash, revokes all
owner sessions, and writes audit/events atomically without secret output.

```bash
cd server
go test ./internal/auth ./internal/api ./cmd/market-lens \
  -run 'TestSignInStart|TestOwnerLogin|TestOwnerPasswordReset|TestOwnerRecoveryUnavailable' -count=1
```

Before the US1 checkpoint, add a route-matrix red test and then make it green: anonymous,
expired, revoked, and deactivated sessions receive no market page, instrument/history,
import/quality, or SSE data, including while setup is still open. An active owner session
must reach the same shared reference data. Health, readiness, safe setup/auth endpoints,
and invitation acceptance remain the only anonymous routes.

### 4. Invitation-only activation

Add red tests for create/resend/revoke/accept, intended normalized email, conflicting
accounts, expiry, replay, delivery failures, and absence of member password state:

```bash
cd server
go test ./internal/identity ./internal/mail ./internal/api -run 'TestInvitation|TestMail' -count=1
```

Concurrent acceptance must create at most one member/session. A resend invalidates the
old capability before the new message is attempted.

### 5. Passwordless code and abuse controls

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

### 6. Authorization, audit, and SSE

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
- shared market snapshots and events are never available anonymously;
- revocation/deactivation rejects replay and closes an open stream within five seconds;
- `Last-Event-ID`, duplicates, slow consumers, and reconnects remain safe.

### 7. HTTP/UI flows

Implement handlers against `contracts/openapi.yaml`, then frontend services/stores/forms
against the handlers. Mutation handlers require same-origin CSRF; login/setup/acceptance
receive secrets in JSON bodies, never query parameters.

```bash
npm run test:unit
npm run typecheck
npx playwright test e2e/owner-access.spec.ts
```

Playwright covers:

- setup with EODHD/SMTP fields, provider failure retry, and permanent closure;
- one email-first sign-in progression whose OTP screen always offers the explicit
  secondary owner-password action without changing based on account identity;
- owner password login and absence of forgot-password/recovery UI/routes;
- invitation send, safe failure, resend, revoke, and acceptance;
- member code login on a second browser/device;
- invalid/expired/replayed code and 3/10-attempt transitions;
- owner unlock and fresh-code requirement;
- session list, individual revoke, all-device sign-out, and live-stream termination;
- disconnected/reconnecting/stale/offline state without form input loss;
- system/light/dark themes and keyboard/touch/non-hover use;
- 360x800, 768x1024, 1440x900, and no-overflow 320x800.

No browser request or rendered/storage value may contain an EODHD key, SMTP field,
ciphertext, credential deployment key, password, code, or capability after submission.

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

## Deployment-only owner reset and credential-key rotation

Use automated command tests for routine verification; do not place real passwords or
keys in shell history. In a disposable deployment, the reset is invoked interactively:

```bash
kubectl exec -it deployment/market-lens -- /app/market-lens auth owner-password reset
```

The command must reject a non-TTY, prompt twice without echo, revoke every owner session,
and require a normal password sign-in afterward. There is no `/recover` page or recovery
HTTP endpoint.

Key rotation follows [contracts/cli.md](./contracts/cli.md) and is verified against a
disposable database: a deliberate failure before commit leaves both credential rows at
the old version; success moves both together and the server starts only with the new
deployment key/version. Never paste either key into a command argument, transcript,
issue, or committed file.

## User Story 2 verification evidence (passwordless member login and lockout)

Recorded against PostgreSQL 18 and a captured SMTP sink on 2026-08-31.

- **Six-digit contract**: codes are generated uniformly, keep leading zeros under
  concurrency, are stored only as a keyed HMAC digest, expire after 10 minutes, and are
  usable once. Verified by `server/internal/auth/member_code_test.go`.
- **Newest-only issuance**: requesting a code supersedes any outstanding one, so a delayed
  older email never authenticates. Verified by
  `TestNewMemberCodeSupersedesTheOlderDeliveredCode`.
- **Code-value reuse**: only six-digit codes exist, so a retired value must be issuable
  again. Migration `0010_member_code_digest_scope.sql` scopes digest uniqueness to live
  challenges; verified by `TestRetiredMemberCodeValueMayBeIssuedAgainLater`.
- **3-attempt block and 10-attempt lock**: three consecutive wrong codes apply a durable
  15-minute block, and the tenth wrong code in a rolling 24 hours applies an owner-only
  lock. Both survive a new repository over the same database, and a correct code submitted
  while blocked or locked is still refused. Verified by
  `server/internal/auth/member_lockout_integration_test.go`.
- **Threshold cannot be bypassed**: crossing either threshold revokes the outstanding
  challenge, so a fresh code is always required afterwards; submissions made while blocked
  do not consume the rolling budget.
- **Serialized verification**: twelve devices submitting the correct code concurrently
  produce exactly one consumed challenge and one session
  (`TestConcurrentMemberVerificationSerializesFailureAccounting`).
- **Independent sliding windows**: per-account delivery is limited to one per minute and
  five per hour; per-origin request and verify buckets are separate and bound distributed
  guessing and spraying. Refused attempts never consume budget and retry hints are coarsened
  to whole minutes. Verified by `server/internal/auth/rate_limit_integration_test.go`.
- **Anti-enumeration**: member, owner, unknown, and malformed addresses all return the same
  202 body; account-level throttling, provider outage, temporary block, and administrative
  lock are all externally indistinguishable. Verified by
  `server/internal/auth/member_signin_integration_test.go` and
  `server/internal/api/member_auth_test.go`.
- **Owner-only unlock**: members, other members, anonymous callers, and a forged owner role
  are all refused; unlock clears failure history and outstanding codes without reactivating
  the account, granting a session, or revoking the member's existing sessions. Verified by
  `server/internal/identity/member_admin_integration_test.go`.
- **Scoped SSE**: member sign-in publishes a user-scoped `session.created.v1`; block, lock,
  and unlock publish an owner-scoped `member.changed.v1`. A member replaying the feed never
  receives the owner-scoped event, and no payload carries a code, token, or digest. Verified
  by `server/internal/events/member_events_integration_test.go`.
- **Live host run**: against a real server, captured SMTP sink, and the deployment EODHD
  credential, a member requested a code, signed in with it, and a code issued *before* a
  process restart still authenticated afterwards. Replay returned 401, three wrong codes
  produced the durable block, and the owner cleared it over HTTP with CSRF while the
  member's existing session was preserved.
- **Responsive and theme**: the passcode step and member administration pass at 360x800,
  768x1024, and 1440x900, retain typed input across all three themes, and do not overflow
  at 320 CSS pixels. Verified by `e2e/member-access.spec.ts` across all Playwright projects.

### Defects found and fixed during User Story 2

- `member_login_challenges.code_digest` was globally unique, which would have made code
  issuance fail permanently once retired digests accumulated. Scoped to live challenges by
  migration `0010`.
- `/auth/sign-in/start` serialised its body as `{"Message": ...}` while the contract and
  client both require `message`, which broke the entire passwordless journey. Found by
  running the real server; covered by
  `TestSignInStartBodyMatchesTheGenericContractFieldName`.
- The CSRF token was returned only at sign-in and held in memory, so after any page reload
  an authenticated person could not sign out, revoke a device, or administer members. The
  token is now also published in the script-readable `__Host-market_lens_csrf` cookie and
  recovered on restore.
- `AUTH_SECURE_COOKIES=false` could never work, because both cookies use the `__Host-`
  prefix that browsers accept only with `Secure`. Documented as always-true in
  `.env.example`; `http://localhost` is a secure origin, so local development is unaffected.
- `/favicon.svg` was protected by default, so every anonymous page load produced a failed
  icon request. It is now part of the public sign-in shell.

## User Story 3 verification evidence (invite and manage members by email)

- **Owner-only lifecycle**: list, create, resend, and revoke all require the persisted owner
  role; a member is refused every one. Verified by
  `server/internal/identity/invitation_delivery_integration_test.go` and
  `server/internal/api/invitations_test.go`.
- **Normalised, unique, expiring**: addresses are normalised for uniqueness (domain lowered,
  local part preserved), only one pending invitation may exist per address, the owner's own
  address can never be invited, and capabilities expire after seven days. Verified by
  `server/internal/identity/invitation_integration_test.go`.
- **Resend lineage**: resending mints a replacement capability, restarts the window, and makes
  every earlier capability for that invitation unusable.
- **Concurrent acceptance**: eight simultaneous acceptances of one capability produce exactly
  one member, one consumed invitation, one session, one audit row, and zero password
  credentials; the address is verified by the act of holding the capability.
- **Identity binding**: a capability cannot onboard a different address, and acceptance of a
  conflicting identity creates nothing.
- **Passwordless**: no acceptance path accepts or stores a password. The API rejects a request
  carrying a `password` field, and the UI presents no password input anywhere in the journey.
- **Capability handling**: the capability exists only in the emailed link fragment and as a
  keyed digest at rest. It never appears in an API response, the database in plaintext, the
  logs, browser storage, or the address bar after the acceptance page reads it.
- **Provider outage**: a failed send leaves the invitation pending and resendable with safe
  `Not delivered` state; provider error codes and the SMTP host never reach the interface or
  logs, and existing owner administration keeps working.
- **Deactivation**: deactivating a member revokes every session with reason `user_deactivated`,
  revokes outstanding login codes, prevents new codes from being issued, and notifies both the
  owner console and the member's own devices. Reactivation restores sign-in without a second
  invitation and never revives revoked sessions. Verified by
  `server/internal/identity/member_status_integration_test.go`.
- **Responsive**: invitation and member administration pass at 360x800, 768x1024, and
  1440x900, retain typed input across all three themes, and do not overflow at 320 CSS pixels.
  Verified by `e2e/invitations.spec.ts` across all Playwright projects.

### Defects found and fixed during User Story 3

- `/invite` was protected by default, so an invited person could never reach the page their
  email pointed at. It now belongs to the public sign-in shell.

## User Story 4 verification evidence (private data and event isolation)

### Recorded red evidence

- **T074/T080 (2026-08-31)**: the authorization policy table was written first and failed
  against a missing package, then passed once `server/internal/authorization/policy.go`
  expressed anonymous, deactivated, member, and owner decisions for shared, self-private,
  other-user-private, and owner-administration resources, failing closed on an unknown scope.
- **T075 (2026-08-31)**: `TEST_DATABASE_URL=... go test ./internal/authorization -count=1`
  compiled against a scope parameter and a `Deactivated` audience field that were accepted but
  ignored, and failed behaviorally: *"a deactivated member replayed 1 events"* and *"a member
  listed 2 administration records"*. The two session/replay isolation cases in the same file
  passed, proving the US1-US3 scoping they cover.
- **T076 (2026-08-31)**: `TEST_DATABASE_URL=... go test ./internal/events -count=1` failed on
  `member audience = {UserID:"...", Role:"", Deactivated:false}, want the persisted member role`
  against a stub resolver. The replay contract case (no duplicates, no gaps, bounded batches,
  foreign cursors) passed. `go test ./internal/api -run 'Stream'` failed with *"the stream never
  revalidated its session"* and *"revalidation ran 0 times under a one-hour heartbeat"*.
- **T077 (2026-08-31)**: `go test ./internal/api -run 'Owner|Private'` failed on seven of eight
  owner routes, which answered `200`/`201`/`204` to an authenticated member against a
  deliberately permissive administration service, and on all three client-asserted-owner
  overrides (`X-Role`, `X-User-Id`, `?role=`), each of which reached the service.
- **T078 (2026-08-31)**: `npx vitest run` failed on `drops user-scoped events addressed to
  another account` and on both `setAudience` expectations in the store, while route protection,
  auth-expiry clearing, and the no-polling assertions passed.
- **T079 (2026-08-31)**: `npx playwright test e2e/owner-access.spec.ts` failed two of the five
  new journeys: another member's private event still refreshed this member's snapshot, and a
  refused stream left the member sitting on `/account`.

### Zero-disclosure matrix

Every path is exercised for two members plus the owner. "Nothing" means no record, no content,
no count, no existence signal, and no differing response.

| Access path | Other member's private data | Owner-administration data | Shared market data |
| --- | --- | --- | --- |
| REST read by guessed UUID | nothing; identical response to an unknown UUID | `403` with no body detail | readable by every active user |
| REST list / pagination | nothing; counts reflect only the caller | `403` before the service is called | readable |
| Aggregates and counts | nothing | `403` | readable |
| Export-shaped listings (sessions, members) | nothing | `403` | readable |
| Search / cursor reuse | a foreign cursor reveals nothing and skips nothing of the caller's own | `403` | readable |
| Audit / administration metadata | nothing | owner only, account and security state only | n/a |
| Replay cache key | keyed by audience, so two callers with an identical cursor and limit share only `shared` rows | owner only | shared to all |
| SSE connect | audience resolved from the durable user record, never the request | owner scope only for the persisted owner | delivered to every active user |
| SSE replay (`Last-Event-ID`) | nothing; identifiers gap without disclosing what filled them | owner only | delivered |
| SSE after revocation or deactivation | stream ends within five seconds | stream ends | stream ends |
| Client-side envelope handling | a `user`-scope event whose subject is not the signed-in account is dropped before it can invalidate anything | `owner` scope dropped for a member | applied |

### Enforcement points

- **Policy**: `server/internal/authorization/policy.go` holds every decision as one readable
  table. Ownership of the instance is administrative authority; it deliberately grants no window
  into another member's private activity.
- **Transport**: `httpx.RequireOwner` gates every `/api/v1/owner/*` route, so a route that
  forgets its own check is still refused. The owner actor is always the authenticated principal;
  no header, query parameter, or body field can name a role or a subject.
- **Persistence**: `identity.Repository` refuses administration queries without an owner scope,
  and `events.Repository` refuses replay for a deactivated audience. A forgotten service check
  cannot become a disclosure.
- **Stream**: `api/events.go` resolves the audience from the durable user record at connect and
  again every two seconds, bounded to at most five, and ends the stream on revocation,
  deactivation, or an unresolvable account. Revalidation is a read: it never extends the idle
  window, so watching a stream does not keep an idle session alive.
- **Client**: `AuthorizedEventStream` verifies each private event against the signed-in account
  as defense in depth, and a refused stream returns the person to sign-in instead of leaving
  private data on screen that it can no longer refresh.

### Verification

```bash
TEST_DATABASE_URL=... go test ./... -count=1   # all Go packages pass
npx vitest run                                 # 70 tests pass
npx playwright test                            # 81 journeys pass across all three viewports
TEST_DATABASE_URL=... make verify              # formatting, vet, Go tests, build, unit tests
```

### Defects found and fixed during User Story 4

- Owner administration was enforced only inside the identity service. A handler wired to a
  service that omitted the check would have served another member's account and security
  metadata; the boundary now exists at the router and again at the query layer.
- An open event stream outlived the authentication that admitted it. A revoked session or a
  deactivated account kept receiving events until the client happened to reconnect.
- The stream envelope carried no subject, so a client had no way to notice a misaddressed
  private event. `subject_user_id` is now published for `user`-scope events only.
- Losing authorization behind an open page left the person on a private view with stale data
  and no route away from it.

## Cross-story security and operability evidence

### Secret regression (T085)

`server/internal/auth/secrets_test.go` drives one complete lifecycle — setup capability,
owner bootstrap with an EODHD key and an SMTP credential, invitation, passwordless
acceptance, member code issuance and verification — then looks for every supplied and minted
secret in every place one could survive:

- **Durable storage**: every row of every table is rendered whole with `SELECT t::text`, so a
  secret cannot hide in a column the test forgot to name. Rows written by the migrations are
  snapshotted first and excluded, so reference data can never be mistaken for a leak.
- **Logs** at debug level, the verbosity a host turns on while diagnosing a problem.
- **Errors** returned for a wrong password, a wrong code, and a replayed invitation.
- **Mail**: no message carries the host's own credentials.
- **REST**: `/account`, `/account/sessions`, `/owner/members`, `/owner/invitations`,
  `/owner/integrations`, `/setup/status`, with the caller's own session cookie redacted so the
  rest of the corpus still applies.
- **SSE**: the stream body, which additionally may not contain the word `digest`.

`src/services/auth.secrets.test.ts` covers the browser: every secret is sent once and none
survives in `localStorage`, `sessionStorage`, or `document.cookie`; the CSRF token lives in
memory and is dropped the moment the session ends; a request error never echoes what was
typed; and neither the shipped bundle nor the source embeds a credential name or key.

### Full account acceptance (T086)

`server/internal/auth/acceptance_integration_test.go` runs one installation end to end:
setup closes after the first owner and a replayed capability creates nobody; an invitation
produces a member with zero password credentials; both roles sign in their own way; three
wrong codes block for fifteen minutes and the block elapses on its own; ten in a rolling day
escalate to an owner-only lock that no new code can bypass and that only the owner clears;
sessions are listable and revocable by their owner; an SMTP outage leaves the uniform
sign-in response and owner administration working while disclosing nothing about the
provider; a process restart preserves everything durable and keeps setup closed; and the
whole run replays in order, once each, to exactly the audience entitled to it.

### Time and concurrency (T087)

`server/internal/auth/concurrency_integration_test.go` runs 100 simultaneous callers per
invariant, under `-race`:

| Race | Invariant |
| --- | --- |
| 100 setup claims on one capability | exactly one owner, one session, one credential, two encrypted provider rows, setup closed |
| 100 acceptances of one invitation | exactly one member, no pending invitation left, no password anywhere |
| 20 simultaneous code requests | at most one usable code; each issuance supersedes the last |
| 100 verifications of one code | exactly one success and exactly one new session |
| 100 rate-bucket attempts | exactly the limit allowed; a refusal consumes no budget and does not push the window; separate buckets do not share |
| revocation racing authentication | no usable session survives, and an open stream cannot keep one |
| 100 simultaneous wrong codes | one coherent escalation, the failure counter reset by it, and the correct code cannot slip past the block |

### API contract reconciliation (T088)

`server/internal/api/contract_test.go` parses both `contracts/openapi.yaml` and the router,
and fails in either direction: a route that exists without documentation, or documentation
without a route. Liveness, readiness, the retired recovery endpoints, and routes owned by
earlier features are the only exemptions, each named explicitly. The test was verified to
catch drift by temporarily registering an undocumented route, which it reported. It also
holds the contract to its own access-boundary declaration and refuses to let a retired
recovery endpoint reappear.

Operator documentation for secret-safe bootstrap now lives in `README.md` under **Account
access**, with the environment contract in `.env.example` and `server/.env.example`. Both
state plainly that the owner password, the EODHD key, and the SMTP credential are never
environment values: they are entered once in the wizard, stored encrypted, and cannot be read
back.

### Accessibility and responsive acceptance (T089)

`e2e/accessibility.spec.ts` runs under all three Playwright projects and covers:

- **Accessible names**: no visible control is announced unlabelled.
- **Focus**: keyboard focus is visibly indicated, and tabbing reaches every operable control
  on the account page without falling back to the body.
- **Contrast**: every text node is measured against the rendered background at the WCAG AA
  ratio (4.5:1, or 3:1 for large text). Getting this right required compositing: Chromium
  reports some computed colours as `color(srgb ...)` with 0-1 channels, and the interface uses
  semi-transparent tints that must be layered over what is behind them. The check polls until
  colours settle so a mid-transition frame is never measured.
- **No hover-only interaction**: stylesheet rules are inspected for controls revealed only on
  `:hover`, the pattern that strands touch users entirely.
- **Every viewport**: 320x800, 360x800, 768x1024, and 1440x900 each assert no horizontal
  scrolling, no control pushed outside the viewport, and — at phone and tablet widths — no
  touch target under 24 pixels tall.
- **State retention**: typed input survives all three themes and a portrait/landscape rotation.

### Defects found and fixed during accessibility acceptance

- `.eyebrow` section labels reused the light-theme teal on the dark ground, reaching 3.46:1 —
  under the ratio small uppercase text needs.
- The theme toggle inherited PrimeVue's muted secondary text and fell to 1.64:1 on the dark
  header. It is the control that tells somebody which theme is active, so it has to be read,
  not merely seen.

Both were invisible until the contrast helper composited alpha correctly; the first two
versions of that helper reported confident nonsense, including ratios of exactly 1.00:1 for
readable text. The helper is worth more than the fixes it found.

### Full delivery verification (T090)

| Gate | Result |
| --- | --- |
| `TEST_DATABASE_URL=... make verify` | pass — gofmt, `go vet ./...`, all Go tests, production Vue build with `vue-tsc`, 75 Vitest tests |
| `npx playwright test` | pass — 105 journeys across mobile, tablet, and desktop projects |
| `go test -race ./... -count=1` | pass — every package, including the 100-way concurrency suite |
| `go test ./internal/db -count=1` | pass — clean install and migration upgrade |
| `docker build -t market-lens:owner-access .` | pass |
| `docker compose config --quiet` | pass |
| `deploy/k8s/test.sh` | pass — `k3s manifest contract: ok` |

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
  member login/invitation actions report only safe retryable delivery state. Owner
  password login remains available.
- EODHD unavailable during setup: setup remains open, no owner or credential row is
  committed, and the still-valid capability may retry.
- Credential-key mismatch after setup: readiness fails and integrations fail closed;
  no ciphertext, provider field, or decryption detail is returned.
- Database unavailable: readiness fails and authentication fails closed; health remains
  liveness.
- Client offline/SSE disconnected: UI labels data stale/offline, preserves input, and
  resumes authorized events or refreshes snapshots after reconnect.
- Member administratively locked: existing valid sessions remain unless separately
  revoked/deactivated; new login stays unavailable until owner unlock and a fresh code.
