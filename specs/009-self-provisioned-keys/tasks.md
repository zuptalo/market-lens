---

description: "Task list for feature 009: self-provisioned signing key"
---

# Tasks: Self-Provisioned Signing Key

**Input**: Design documents from `/specs/009-self-provisioned-keys/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: Mandatory. Every production-code task below is preceded by a test task, and each
test task requires running the test and recording in `quickstart.md` that it failed for the
expected *behavioral* reason. A test that fails to compile, or fails on fixture setup, is not
a valid red and does not authorize the production task that follows it.

**Responsive UI**: N/A for this feature. It adds no screen. The one client change is a field
on a response the owner integration-status view already fetches; that view's existing
360x800 / 768x1024 / 1440x900 / 320px coverage is unchanged and must stay green.

**Live Delivery**: Rotation publishes `signing_key.rotated.v1` and reuses `sessions.revoked.v1`,
both inserted in the same transaction as the key replacement. Covered by T029-T030.

**Identity and Notifications**: The stored key is instance configuration, not user data, so
there is no ownership column and no cross-user isolation surface. The authorization boundary
that *does* need proving is that rotation requires host access and that the configuration
object is owner-only — T033 and T037.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: US1, US2, US3 from [spec.md](spec.md)

## Path Conventions

Go backend under `server/`, Vue client under `src/`, ops files at the repository root, per
the Project Structure section of [plan.md](plan.md).

## The constraint that governs every task

`AUTH_SECRET` moves into the database. `EXTERNAL_CREDENTIAL_KEY` does not, and no task below
may add a column, code path, command, migration, or test fixture that puts it — or anything
derived from it — into PostgreSQL. It encrypts provider credentials held *inside* that same
database, so storing it there would put the lock and its key in one file.

---

## Phase 1: Setup

**Purpose**: Establish the baseline this feature's reds are measured against.

- [x] T001 Confirm the branch is rebased on `main` and record the baseline: run `make verify` and `TEST_DATABASE_URL=... go test ./server/... -count=1`, and note in `specs/009-self-provisioned-keys/quickstart.md` that both are green before any change. A red that appears in this phase is a pre-existing failure, not this feature's red.
- [x] T002 Start PostgreSQL with `make db-up` and export `TEST_DATABASE_URL` for the integration-gated suites. Record in `quickstart.md` whether the database-gated tests actually ran; a skipped integration test is not evidence.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The schema and the configuration relaxation that every user story depends on.

**⚠️ CRITICAL**: No user story work begins until T008 is green.

- [x] T003 Write the failing migration test `server/internal/db/instance_signing_key_migration_test.go`, following `external_credentials_migration_test.go`: assert the embedded migration count is 11, then assert the clean-install schema and a real upgrade from the `0010` schema. Run `go test ./internal/db -run TestInstanceSigningKeyMigration -count=1` and record the red — it must fail on `embedded migration count = 10, want 11`, which is a valid behavioral red without a database.
- [x] T004 Extend T003's test with the constraints from [data-model.md](data-model.md): the `instance_signing_key_singleton` unique expression index rejects a second row; `source` accepts only `provisioned`/`supplied`; `(source = 'provisioned') = (key_material IS NOT NULL)`; `octet_length(fingerprint) = 32`; `generation > 0`; `rotated_at >= created_at`. Also assert `sessions.revoked_reason` accepts `signing_key_rotated` and still accepts every value `0009` allowed. Record the red.
- [x] T005 Create `server/internal/db/migrations/0011_instance_signing_key.sql` implementing exactly the DDL in [data-model.md](data-model.md): the table, the singleton unique index, the `COMMENT ON TABLE` recording why the credential key must never join it, and a `sessions_revoked_reason_check_v3` constraint replacing `_v2` with `signing_key_rotated` added. Do not edit migrations `0001`-`0010`. Run T003/T004 to green.
- [x] T006 [P] Write the failing configuration test in `server/internal/config/auth_test.go`: `config.Load()` with `ENV=production` and no `AUTH_SECRET` returns no error, and the returned `AuthConfig.Secret` is empty. **This is the feature's initial red.** Run `go test ./internal/config -run TestLoadProductionWithoutAuthSecret -count=1` and record that it failed with `AUTH_SECRET is required in production` — an assertion on the returned error value, not a compile failure. Keep the existing test that a *supplied* value shorter than 32 bytes is rejected; do not weaken it.
- [x] T007 [P] Write the failing configuration test in `server/internal/config/external_credentials_test.go`: `config.Load()` with `ENV=production` and no `EXTERNAL_CREDENTIAL_KEY` returns no error and `ExternalCredentialConfig.Configured` is false. Keep every existing assertion that a malformed key, a key without a version, or a wrong-length key is rejected. Record the red (`EXTERNAL_CREDENTIAL_KEY is required in production`).
- [x] T008 Remove the two production-presence guards in `server/internal/config/auth.go` and `server/internal/config/external_credentials.go`, keeping all shape and length validation. Run `go test ./internal/config -count=1` to green. The presence decision now belongs to the database-aware checks built in T034-T035; until those exist the tree is deliberately permissive, which is why Phase 5 may not be skipped.
- [x] T009 Add `RevokeSigningKeyRotated RevokeReason = "signing_key_rotated"` to `server/internal/auth/model.go` and include it in `validRevokeReason`, with a unit test in `server/internal/auth/session_test.go` asserting a session may be revoked with it. Write the assertion first and record the red.

**Checkpoint**: The schema can hold a key, the process can start in production without either environment value, and a session can record why a rotation ended it.

---

## Phase 3: User Story 1 - Deploy with only a database connection (P1) 🎯 MVP

**Goal**: A production start with only `DATABASE_URL` provisions its own signing key, reuses
it on restart, converges under concurrency, survives a restore onto another host, and never
re-keys a deployment that supplies `AUTH_SECRET`.

**Independent test**: Start against an empty database with only `DATABASE_URL`, confirm it
becomes healthy, sign in, restart the process, and confirm the session still works.

- [x] T010 [US1] Write the failing unit test `server/internal/auth/signing_key_test.go` for the fingerprint: `HMAC-SHA256(key, "market-lens/instance-signing-key/fingerprint/v1")` is 32 bytes, is stable for one key, differs across keys, and is computed without the key appearing in the returned value. Record the red.
- [x] T011 [US1] Write the failing unit test in `signing_key_test.go` covering all six rows of the resolution table in [data-model.md](data-model.md), each asserting the resolved key *or* the exact refusal message from [contracts/cli.md](contracts/cli.md). Assert every refusal names the variable and a remedy, and that no refusal contains key material, a fingerprint, or a key length. Record the red.
- [x] T012 [US1] Create `server/internal/auth/signing_key.go` with the fingerprint function, a `SigningKeySource` type, a `ResolveSigningKey` decision function that is pure over `(storedRow, suppliedSecret)`, and the 48-byte generator reading `crypto/rand`. Take the random source and the stored row as parameters so the decision is testable without a database. Run T010-T011 to green.
- [x] T013 [US1] Write the failing integration test `server/internal/auth/signing_key_integration_test.go` (gated on `TEST_DATABASE_URL`) asserting that resolving against an empty database inserts exactly one `provisioned` row with `generation = 1` and 48 bytes of key material, and that resolving again returns the identical key without writing. Record the red.
- [x] T014 [US1] Write the failing integration test asserting that resolving with `AUTH_SECRET` set against an empty database stores a `supplied` row with `key_material IS NULL` and only a fingerprint, and returns the supplied value — Acceptance Scenario 5, the guarantee that no existing deployment is re-keyed. Record the red.
- [x] T015 [US1] Write the failing concurrency integration test (SC-003): 100 goroutines resolve against one empty database simultaneously; assert exactly one row exists and all 100 returned keys are byte-identical. The test must not serialize the callers — a mutex around the call would prove nothing. Follow `server/internal/auth/concurrency_integration_test.go` for the harness. Record the red.
- [x] T016 [US1] Implement the repository half in `server/internal/auth/repository.go`: `SigningKeyRow(ctx)` reading the singleton, and `ProvisionSigningKey(ctx, row)` performing `INSERT ... ON CONFLICT DO NOTHING` followed by an unconditional `SELECT`, so the loser of a race adopts the winner's key. No advisory lock and no transaction — the singleton index is the guarantee. Run T013-T015 to green.
- [x] T017 [US1] Write the failing start-up test in `server/cmd/market-lens/main_test.go` asserting the resolved key, not `config.AuthConfig.Secret`, is what reaches `auth.NewSecrets`, and that resolution happens after `db.Migrate` and before any service is constructed. Record the red.
- [x] T018 [US1] Wire resolution into `run()` in `server/cmd/market-lens/main.go` immediately after `db.Migrate`, and change `newIdentityService` and `newAuthenticationService` to take the resolved key instead of reading `authConfig.Secret`. Both the CLI and server paths pass through this point, so `auth setup-link`, `auth owner-password reset`, and `auth credential-key rotate` inherit it with no change of their own. Run T017 to green.
- [x] T019 [US1] Write the failing restart/restore integration test (SC-002): resolve a key, sign in, discard and rebuild every service against the same database, and assert the session still verifies. Then assert the same across a simulated restore — a second pool against the same data with a different `DATABASE_URL` host component — proving the key travelled with the data. Record the red, then confirm green against T012/T016/T018.
- [x] T020 [US1] Write and green the failing test for the start-up log line in [contracts/cli.md](contracts/cli.md): captured `slog` output contains `signing_key` and `signing_key_generation` and contains neither the key material nor its base64 form. Implement the single `INFO` line in `main.go`.

**Checkpoint**: User Story 1 is independently deliverable. A production start needs only `DATABASE_URL`, restarts reuse the key, concurrent starts converge, a restore preserves sessions, and a supplied `AUTH_SECRET` still wins and is never stored.

---

## Phase 4: User Story 2 - Rotate the signing key deliberately (P2)

**Goal**: The owner can replace the signing key from the host. Rotation signs everybody out,
says so before it happens, is recorded without the key, and leaves exactly one usable key.

**Independent test**: Sign in, rotate from the host, confirm the existing session is refused
and a fresh sign-in succeeds.

**Depends on**: Phase 3 (there must be a key before there is a rotation).

- [x] T021 [US2] Write the failing integration test in `server/internal/auth/signing_key_integration_test.go`: rotation replaces the row with `source='provisioned'`, `generation` incremented, `rotated_at` set, and key material that differs from the previous value. Record the red.
- [x] T022 [US2] Write the failing integration test asserting rotation revokes every unrevoked session with `revoked_reason='signing_key_rotated'`, and that a session token issued before rotation is refused afterwards while a fresh sign-in succeeds (Story 2 scenario 1, SC-005). Record the red.
- [x] T023 [US2] Write the failing integration test asserting rotation invalidates every other row whose digest was computed under the old key, exactly as [data-model.md](data-model.md) specifies: usable `auth_capabilities` revoked, pending `invitations` moved to `revoked`, active `member_login_challenges` moved to `revoked`, `auth_rate_events` deleted. Assert `security_audit_events` rows are untouched, and assert `member_login_state.consecutive_failures` is untouched so a rotation does not unlock a locked-out member. Record the red.
- [x] T024 [US2] Write the failing integration test asserting atomicity (Story 2 scenario 3): a rotation that fails after the key update but before commit leaves the previous key in force, every session intact, and exactly one usable key; retrying then succeeds. Inject the failure through the transaction boundary, not by editing production code. Record the red.
- [x] T025 [US2] Implement `RotateSigningKey` in `server/internal/auth/repository.go` as one transaction performing every step in [contracts/cli.md](contracts/cli.md) in order, modelled on `credentials.Repository.Rotate`. Run T021-T024 to green.
- [x] T026 [US2] Write the failing test in `server/cmd/market-lens/auth_test.go` asserting `auth signing-key rotate` routes correctly, refuses without an interactive terminal, refuses when no key row exists, requires the typed `ROTATE` confirmation, and prints `signing_key_rotation=complete generation=<n> sessions_revoked=all`. Record the red.
- [x] T027 [US2] Implement `executeSigningKeyRotation` and its routing in `server/cmd/market-lens/main.go`, following `executeOwnerPasswordReset` for terminal handling and `executeCredentialKeyRotation` for confirmation. Run T026 to green.
- [x] T028 [US2] Write and green the failing test asserting the audit row: `security_audit_events` gains `signing_key.rotated.v1` with `outcome='succeeded'` and metadata `{"generation": <int>}`, containing no key material (Story 2 scenario 2). Assert the event type satisfies the `0007` regex constraint.
- [x] T029 [US2] Write the failing test asserting `client_events` gains `signing_key.rotated.v1` (scope `owner`) and `sessions.revoked.v1` (scope `user`) in the same transaction as the key replacement, so a reconnecting client cannot miss a committed rotation. Assert neither payload contains key material. Implement to green.
- [x] T030 [US2] Write and green the failing test asserting Last-Event-ID replay returns the rotation events to a client that reconnects across the rotation, and that a duplicate delivery is safe. Reuse the existing `client_events` resumption harness; this adds no new event contract, only a new type.

**Checkpoint**: User Story 2 is independently deliverable and does not disturb Story 1.

---

## Phase 5: User Story 3 - Be told what is missing before it breaks (P2)

**Goal**: The remaining external value is the only one an operator can get wrong, so the cost
of getting it wrong is visible at start and on request — without describing what is stored.

**Independent test**: Start without the credential key and confirm the message names the
value, says what becomes unreadable without it, and says to keep it with backups.

**Depends on**: Phase 2 (T008 removed the old guard; this phase restores the *correct* one).

- [x] T031 [US3] Write the failing integration test in `server/internal/credentials/repository_integration_test.go`: with no stored credentials and no key, `ValidateConfiguration` reports "not required" rather than an error; with stored credentials and no key it returns an error naming `EXTERNAL_CREDENTIAL_KEY` and what becomes unreadable, and revealing nothing about the ciphertext — no length, kind list, count, or key version. Record the red.
- [x] T032 [US3] Extend `ValidateConfiguration` (or add a sibling returning a requirement state) in `server/internal/credentials/repository.go` so presence is decided against stored ciphertext rather than against the environment. Keep the existing wrong-key path and its message unchanged. Run T031 to green.
- [x] T033 [US3] Write the failing test in `server/cmd/market-lens/main_test.go` for the four start-up outcomes in [contracts/cli.md](contracts/cli.md): no credentials + no key starts and warns once; no credentials + key starts silently; credentials + no key refuses; credentials + wrong key refuses with the existing message. Assert the warning names the value and says to keep it with backups (SC-006). Record the red, then wire `validateExternalCredentialConfiguration` in `main.go` to green.
- [x] T034 [US3] Write the failing test asserting the start-up log line reports `external_credential_key` and `operator_must_retain`, and that `operator_must_retain` is empty when nothing is supplied and nothing is stored — the state SC-007 describes. Implement to green.
- [x] T035 [US3] Write the failing test in `server/internal/api/auth_test.go` asserting `GET /api/v1/owner/integrations` returns the `configuration` object exactly as [contracts/openapi.yaml](contracts/openapi.yaml) specifies, that `integrations` is unchanged, and that the response contains no key, fingerprint, or key length. Record the red.
- [x] T036 [US3] Write the failing authorization test asserting the `configuration` object is served only to an owner: a member session and an anonymous request receive the existing `403`/`401` without any part of the object. Extend `server/internal/api/authorization_test.go`. Record the red.
- [x] T037 [US3] Implement the `configuration` object in `ownerIntegrationStatusHandler` in `server/internal/api/auth.go`, sourced from the resolved key's source and generation plus the credential-key requirement state. Run T035-T036 to green.
- [ ] T038 [P] [US3] **Not done - premise was wrong.** The plan assumed an existing owner integration-status view rendered the new object. There is no such view: `GET /api/v1/owner/integrations` has no client consumer anywhere in `src/`, so there is nothing to extend. Building one would be a new screen, which [spec.md](spec.md) explicitly excludes and which would need its own responsive design and Playwright coverage. FR-009's "on request" is satisfied by the endpoint itself, which is specified in [contracts/openapi.yaml](contracts/openapi.yaml) and covered by T035-T037. Rendering it is separate, optional work for whenever an owner settings screen exists.
- [x] T039 [P] [US3] Extend `src/services/auth.secrets.test.ts` so the built-bundle scan also rejects any signing-key material and the fingerprint label, alongside the existing `AUTH_SECRET`/`EXTERNAL_CREDENTIAL_KEY` terms. Record the red by seeding a fixture that contains the term, then remove the seed and confirm green.

**Checkpoint**: All three user stories are complete and independently testable.

---

## Phase 6: Polish and Cross-Cutting Concerns

- [x] T040 Write the failing non-disclosure lifecycle test (SC-004) covering provision → sign in → rotate in one run, capturing `slog` output, every HTTP response body, `security_audit_events` rows, `client_events` payloads, and command output, and scanning every captured byte for the key material, its base64 form, and its hex form. This task is deliberately last among the tests so it covers rotation output too. Record the red by asserting against a deliberately leaky temporary log line, then remove it and confirm green.
- [x] T041 Add a run to `scripts/production-surface.test.sh` that boots the built image against a throwaway PostgreSQL with **only** `DATABASE_URL` set, and asserts it becomes healthy and serves the sign-in page (SC-001). Keep the existing fully-configured run. This is the only check in the repository that exercises the production artifact behind the real Go router — the Playwright suite serves the client through `vite preview` and cannot see this class of failure.
- [x] T042 [P] Update `deploy/k8s/install.sh` to stop generating `AUTH_SECRET` for a new installation while never removing or overwriting one already present in `market-lens-secrets`, because removing it would sign every existing user out. Keep `EXTERNAL_CREDENTIAL_KEY` generation exactly as it is.
- [x] T043 [P] Update `deploy/k8s/test.sh`: replace the assertions that the installer generates `AUTH_SECRET` with assertions that it does not generate one and does not remove an existing one, and keep every `EXTERNAL_CREDENTIAL_KEY` assertion unchanged. Update `deploy/k8s/20-market-lens.yaml` to make the `AUTH_SECRET` env reference optional rather than required.
- [x] T044 [P] Update `.env.example`, `server/.env.example`, and `docker-compose.yml` so `AUTH_SECRET` is documented as optional and explicitly labelled "self-provisioned when unset", and `EXTERNAL_CREDENTIAL_KEY` is labelled "must be retained with your backups; never stored in the database".
- [x] T045 [P] Update `README.md` and `deploy/k8s/README.md`: a production deployment now needs `DATABASE_URL` alone, a restore needs the database plus `EXTERNAL_CREDENTIAL_KEY`, and the reason the two keys are treated differently. Document `auth signing-key rotate` beside `auth credential-key rotate`.
- [x] T046 Update `ROADMAP.md` and `specs/README.md` to move feature 009 to in-review, and set the `Status` line in `specs/009-self-provisioned-keys/spec.md` to `in-review`. Do not touch feature 002's lifecycle rows; re-marking 002 is separate work with its own evidence.
- [x] T047 Record every red and green in `specs/009-self-provisioned-keys/quickstart.md` under "Recorded red evidence" and "Recorded green evidence", each with the exact command and the observed behavioral failure. A checkpoint without recorded evidence is not complete.
- [x] T048 Run the full acceptance set and record outcomes in `quickstart.md`: `make verify`; `TEST_DATABASE_URL=... go test ./server/... -count=2`; `npm run test:unit`; `npm run test:e2e`; `docker build -t market-lens:local .`; `docker compose config --quiet`; `scripts/production-surface.test.sh`; `deploy/k8s/test.sh`. Run the Go suite twice to catch order dependence in the singleton table.
- [x] T049 Perform the six manual verification steps in [quickstart.md](quickstart.md) against a throwaway database — including the `pg_dump`/restore-on-another-host step, which is the only direct proof of SC-002 — and record the results. Run `git diff --check` and confirm no credentials, generated builds, coverage, browser output, or database data are tracked.

---

## Dependencies

```text
Phase 1 (T001-T002)
        │
Phase 2 (T003-T009)   ← blocking; T005 and T008 gate everything
        │
        ├────────────────────────────┐
Phase 3 US1 (T010-T020)              │
        │                            │
Phase 4 US2 (T021-T030)      Phase 5 US3 (T031-T039)
        │                            │
        └──────────┬─────────────────┘
                   │
Phase 6 Polish (T040-T049)
```

- **US1 blocks US2**: there must be a key before there is a rotation.
- **US1 does not block US3**: the credential-key work touches a different package and can
  proceed in parallel once Phase 2 is green. T034 is the one point they meet, in the shared
  start-up log line.
- **T040 depends on every producer of output**, which is why it sits in Phase 6 rather than
  beside the code it audits.

## Parallel execution

Within Phase 2: T006 and T007 are independent files.
Within Phase 5: T038 and T039 are client-side and independent of T031-T037.
Within Phase 6: T042, T043, T044, and T045 touch disjoint files.

Across stories: once Phase 2 is green, one agent can take Phase 3 → Phase 4 while another
takes Phase 5. They meet only at T034.

## Implementation strategy

**MVP is Phase 1 + Phase 2 + Phase 3.** That alone delivers the feature's stated purpose —
a production deployment that starts with `DATABASE_URL` alone and survives a restore — and it
is independently shippable. Rotation and the credential-key reporting are genuine
improvements but neither is required for SC-001 or SC-002.

Ship in that order, keeping every suite green at each checkpoint.

## Task count

49 tasks: 2 setup, 7 foundational, 11 for US1, 10 for US2, 9 for US3, 10 polish.

## Completion (2026-08-31)

48 of 49 complete. T038 is deliberately not done; the reason is recorded on the task itself.

Two things were found that the plan did not anticipate, both recorded in
[quickstart.md](quickstart.md):

1. **A start-up defect only the container check could see.** `newIdentityService` and
   `newAuthenticationService` built a credential cipher unconditionally, so a deployment given
   only `DATABASE_URL` — the configuration this whole feature exists to support — failed to
   start, while every Go test passed because each supplied a key. Fixed test-first: the cipher
   is optional, and owner setup refuses at the point of use naming the value.
2. **Two assertions were written after their code.** T020 and T040 had no natural red, so each
   was proven to bite by seeding the exact defect it guards against and reverting.

SC-002 was proven directly rather than by proxy: a `pg_dump` restored onto a different
database produced a byte-identical signing key, and a container started against the restored
data reused generation 1 instead of provisioning a second key.
