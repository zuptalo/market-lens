# Quickstart: Self-Provisioned Signing Key

This guide is the implementation/verification contract. It does not authorize manual
database mutation. The signing key is created by an ordered migration plus application code
on first start, never by an operator running SQL, and every behavior begins with a valid
failing automated test.

## Prerequisites

- Go 1.26, Node/npm, Docker with Compose.
- PostgreSQL started with `make db-up`; `TEST_DATABASE_URL` set for integration tests.
- Features 004 (`0007`–`0010`) applied. This feature adds `0011`.
- No ad-hoc SQL to prepare or repair the schema. The `testdb` helper creates isolated
  databases and runs migrations.

## Configuration contract

After this feature, a production start requires exactly one value:

```text
DATABASE_URL=postgres://...
```

Everything below is optional, and the application reports which of them it is using:

```text
AUTH_SECRET=                      # optional; when set it takes precedence and is never stored
EXTERNAL_CREDENTIAL_KEY=          # required only once provider credentials are stored
EXTERNAL_CREDENTIAL_KEY_VERSION=1 # required with the key
AUTH_SECURE_COOKIES=true          # unchanged; still required true in production
```

**The boundary, restated because it is the point of the feature**: `AUTH_SECRET` may live in
the database because it protects rows in that same database. `EXTERNAL_CREDENTIAL_KEY`
encrypts the EODHD key and SMTP password *inside* that database and therefore must not. No
task in this feature may store it, or anything derived from it, in PostgreSQL.

`AUTH_SECRET` remains supported and takes precedence, so every deployment that supplies it
today keeps working with no change and nobody is signed out. `deploy/k8s/install.sh` stops
*generating* it for new installations but must not remove it from an existing secret.

## Red-green implementation checkpoints

Record each red with the exact command, the observed behavioral failure, and the green that
follows. A test that fails to compile or fails on fixture setup is not a valid red.

1. **Configuration (the initial red)**.
   `go test ./internal/config -run TestLoadProductionWithoutAuthSecret -count=1`
   must fail with `AUTH_SECRET is required in production`. Green removes that guard while
   keeping the minimum-length check on a supplied value.
2. **Migration `0011`**. `go test ./internal/db -run TestInstanceSigningKeyMigration -count=1`
   fails first on `embedded migration count = 10, want 11`, which is a valid red without a
   database. Green proves a clean install and a real upgrade from the `0010` schema, the
   singleton index, every CHECK, and the `sessions_revoked_reason_check_v3` constraint.
3. **Resolution**. `go test ./internal/auth -run TestResolveSigningKey -count=1` covers all
   six rows of the resolution table in `data-model.md`, including each refusal's message.
4. **Concurrency (SC-003)**. 100 goroutines resolve against one empty database; assert one
   row and one distinct key value. No test may assert this by serializing the callers.
5. **Restart and restore (SC-002)**. Resolve, sign in, discard and rebuild every service
   against the same database, assert the session still verifies.
6. **Rotation (SC-005)**. One transaction; the prior session is refused afterwards; a fresh
   sign-in succeeds; capabilities, invitations, and challenges are invalidated; a failure
   part-way leaves exactly one usable key.
7. **Non-disclosure (SC-004)**. One lifecycle test over provision → sign in → rotate,
   scanning captured `slog` output, HTTP bodies, `security_audit_events` rows,
   `client_events` payloads, and command output for the key material and its base64 form.
8. **Credential-key reporting (SC-006)**. Start with no `EXTERNAL_CREDENTIAL_KEY` and no
   stored credentials: starts, warns. Insert stored credentials, restart without the key:
   refuses, names the value, describes nothing about the ciphertext.
9. **Production surface**. `scripts/production-surface.test.sh` gains a run of the built
   image with only `DATABASE_URL`. This is the only check in the repository that exercises
   the production artifact behind the real Go router; the Playwright suite serves the client
   through `vite preview` and cannot see this.

## Acceptance verification

Run in proportion to the change, and record the outcomes here:

```bash
make verify
TEST_DATABASE_URL=... go test ./server/... -count=1
docker build -t market-lens:local .
docker compose config --quiet
scripts/production-surface.test.sh
deploy/k8s/test.sh
npm run test:e2e
```

Then confirm by hand, against a throwaway database:

1. Start with only `DATABASE_URL`. It becomes healthy and logs
   `signing_key=provisioned generation=1`.
2. Restart. The generation stays 1 and an existing session still works.
3. Set `AUTH_SECRET` on that same installation and restart. It refuses, naming both remedies.
4. Unset it again and restart. It starts unchanged, still generation 1.
5. Run `auth signing-key rotate`. The prior session is refused; a fresh sign-in works;
   `security_audit_events` holds `signing_key.rotated.v1` with `{"generation": 2}` and no key.
6. `pg_dump` the database, restore it onto a different host with only `DATABASE_URL`, and
   confirm the session issued in step 5 still works — the property SC-002 exists to prove.

## Responsive UI

N/A. This feature adds no screen. The `configuration` object extends the existing owner
integration-status response, which already has its mobile, tablet, and desktop treatment.

## Recorded red evidence (2026-08-31)

Baseline before any change: `make verify` and
`TEST_DATABASE_URL=... go test ./server/... -count=1` both green, with the integration suites
actually running against PostgreSQL (`internal/auth 70.0s`, `internal/db 44.7s`), not skipped.

- **T003/T004 red**: `go test ./internal/db -run TestInstanceSigningKey -count=1` failed with
  `embedded migration count = 10, want 11`, and with a database, additionally
  `instance_signing_key table is absent` for both the clean-install and the
  upgrade-from-`0010` cases. Behavioral on all three.
- **T006 red (the feature's initial red)**: `go test ./internal/config -run
  TestLoadProductionWithoutAuthSecretSucceeds -count=1` failed with
  `production configuration without AUTH_SECRET failed: AUTH_SECRET is required in
  production` — an assertion on the returned error value, not a compile failure.
- **T007 red**: the same run reported
  `production configuration without EXTERNAL_CREDENTIAL_KEY failed: EXTERNAL_CREDENTIAL_KEY
  is required in production`.
- **T009 red**: `go test ./internal/auth -run TestSessionAcceptsSigningKeyRotationAsRevokeReason`
  first failed to *compile* on an undefined constant, which is not a valid red. The constant
  was declared without being added to `validRevokeReason`, and the test was rerun to obtain
  the behavioral failure `signing key rotation was rejected as a revocation reason: session
  revocation is invalid`.
- **T010/T011 red**: `go test ./internal/auth -run 'TestSigningKeyFingerprint|TestGenerateSigningKey|TestResolveSigningKey'`
  failed with `fingerprint length = 0, want 32`, `signing key generation is unimplemented`,
  and `signing key resolution is unimplemented` across all seven resolution outcomes,
  including each refusal failing to name `AUTH_SECRET` and `signing-key rotate`.
- **T013/T014/T015/T019 red**: the database-backed suite failed with
  `instance signing key persistence is unimplemented`, including
  `0 of 100 simultaneous starts resolved a key, want all of them`.
- **T017 red**: `go test ./cmd/market-lens -run TestResolvedSigningKeyReachesBothServices`
  failed with `a start with only a database connection failed: instance signing key
  resolution is not wired`.
- **T021-T024 red**: the rotation suite failed with
  `instance signing key rotation is unimplemented`.
- **T026 red**: `go test ./cmd/market-lens -run TestExecuteSigningKeyRotation` failed on all
  four cases with `instance signing key rotation is not wired`.
- **T031 red**: `go test ./internal/credentials -run TestCredentialRequirementFollows` failed
  with `stored credential detection is unimplemented`.
- **T033 red**: the four start-up outcomes failed on exactly one case —
  `an installation that cannot read its stored credentials started anyway` — which is the
  single behavior that changed.

### Reds proven by seeded defect

Two assertions were written after the code they guard, so no natural red was observed. Each
was instead proven to bite by seeding the exact defect it exists to catch, then reverting:

- **T020**: adding `"seeded_defect_key", string(signingKey.Key)` to the start-up log line made
  `TestLogInstanceConfigurationNamesRetainedValuesWithoutSecrets` fail on all three cases with
  `configuration report disclosed a secret`. Reverted; green.
- **T040**: adding the base64 key to the rotation audit metadata made the schema-wide sweep
  fail with `key material found in client_events.payload` and
  `key material found in security_audit_events.metadata`. Reverted; green.

## Recorded green evidence (2026-08-31)

- `TEST_DATABASE_URL=... go test ./server/... -count=1` — every package green, integration
  suites running rather than skipping.
- `make verify` — `gofmt`, `go vet`, the Go suite, `npm run build`, and 82 Vitest tests green.
- `docker compose config --quiet` and `deploy/k8s/test.sh` green.
- `scripts/production-surface.test.sh` green against a freshly built image, **including** the
  new phase: a container given nothing but `DATABASE_URL` becomes healthy, reports ready,
  sends a browser to sign-in, opens setup, logs `"signing_key":"provisioned"` with no key
  material, and after a restart still logs `"signing_key_generation":1`. The paired check —
  an installation first started *with* `AUTH_SECRET` refusing to come up without it, naming
  `AUTH_SECRET` — also passes, so SC-001 and the no-silent-re-key guarantee are both proven
  against the real production artifact.

### A defect only the production-surface check could find

The first run of the minimal-configuration phase failed with
`external credential key must contain exactly 32 bytes`, and the container exited. Every Go
test passed at that moment, because each one supplied a credential key. `newIdentityService`
and `newAuthenticationService` built a `credentials.Cipher` unconditionally, so a deployment
with only `DATABASE_URL` — the exact configuration this feature exists to support — could not
start at all.

The fix was driven by a new test first
(`TestBootstrapRefusesClearlyWhenNoCredentialKeyIsConfigured`, red with `a deployment without
EXTERNAL_CREDENTIAL_KEY could not build its identity service`): the cipher is now optional,
`identity.NewService` accepts its absence, owner setup refuses at the point of use naming
`EXTERNAL_CREDENTIAL_KEY` and leaves no owner or credential behind, and mail delivery reports
a retryable failure rather than crashing the process.

This is the second time this repository's container check has caught something the whole unit
and browser suite could not see. Trust it for anything about start-up or the front door.

### Full acceptance run (2026-08-31)

- `TEST_DATABASE_URL=... go test ./server/... -count=2` — all 18 packages green on both runs,
  which is what proves the singleton `instance_signing_key` table carries no order dependence
  between tests.
- `npm run test:e2e` — 108 Playwright journeys passed across mobile, tablet, and desktop
  projects, unchanged by this feature.
- `make verify`, `docker build`, `docker compose config --quiet`, `deploy/k8s/test.sh`,
  `scripts/production-surface.test.sh` — all green.
- `git diff --check` clean; no `.env`, build output, coverage, browser output, or database
  data is tracked.

### Manual verification against throwaway containers

| Step | Result |
|---|---|
| 1. Start with only `DATABASE_URL` | Healthy; `"signing_key":"provisioned","signing_key_generation":1` |
| 2. Restart | Still generation 1; the stored key is reused, not replaced |
| 3. Add `AUTH_SECRET` to that same installation | Refused: *this installation provisioned its own signing key; remove AUTH_SECRET from the environment, or run auth signing-key rotate to replace the stored key* |
| 4. Remove it again | Starts unchanged, still generation 1 |
| 5. `auth signing-key rotate` | Covered by integration tests. The command requires an interactive terminal by design, so it cannot be driven from a non-interactive container run; the transaction, session revocation, audit row, and events are proven in `signing_key_integration_test.go`. |
| 6. `pg_dump` and restore onto a different database | The `instance_signing_key` row is byte-identical after the restore, and a container started against the restored data logs generation 1 — it adopted the travelling key rather than provisioning a second one. **This is the direct proof of SC-002.** |

The paired negative case also holds: an installation first started *with* `AUTH_SECRET`
refuses to come up without it, naming the value, rather than quietly re-keying itself.
