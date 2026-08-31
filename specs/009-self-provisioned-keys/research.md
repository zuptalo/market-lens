# Phase 0 Research: Self-Provisioned Signing Key

**Feature**: `009-self-provisioned-keys` | **Date**: 2026-08-31 |
**Spec**: [spec.md](spec.md)

Every unknown in the Technical Context is resolved below. Each decision records what the
current code does, so the plan does not re-derive it.

## D1. Where the signing key is stored

**Decision**: A dedicated singleton table `instance_signing_key`, created by ordered
migration `0011`. Not a generic `instance_settings` key/value bag.

**Rationale**: A typed table carries the constraints the value actually has — exactly one
row, exactly 48 bytes of key material, a monotonic generation counter, and a source
discriminator. A generic settings bag can express none of those, and it would make storing
`EXTERNAL_CREDENTIAL_KEY` in the database a one-line change by a future contributor who has
not read this specification. The table's name and its column comment state the boundary at
the point where it would be violated.

**Alternatives considered**:

- *Generic `instance_settings(key text primary key, value bytea)`* — rejected. It invites
  exactly the mistake the spec's central decision forbids, and it cannot constrain length or
  cardinality.
- *A file on a mounted volume* — rejected. It reintroduces a second artifact to back up,
  which is the problem the feature removes.
- *Deriving the key from `DATABASE_URL`* — rejected. The connection string is not secret,
  appears in process listings and manifests, and changes when the host or password changes,
  which would silently sign everybody out.

## D2. Singleton enforcement and concurrent provisioning

**Decision**: `CREATE UNIQUE INDEX instance_signing_key_singleton ON instance_signing_key
((true))`. Provisioning is `INSERT ... ON CONFLICT DO NOTHING` followed by an unconditional
`SELECT` of the row. The instance that loses the race reads the winner's key.

**Rationale**: The unique expression index makes "exactly one key" a database invariant
rather than an application convention, so FR-003 and SC-003 hold under any number of
simultaneous starts without an advisory lock or a transaction. `db.Migrate` itself takes no
lock today; adding one is not needed here because the insert is already idempotent.

**Alternatives considered**:

- *`pg_advisory_lock` around a read-then-write* — rejected as unnecessary serialization for
  a once-per-installation operation that a unique index already makes safe.
- *`CHECK (id = 1)` on a fixed primary key* — equivalent guarantee, but the expression index
  reads as the intent ("one row, ever") without a magic constant.

## D3. Key material: size and encoding

**Decision**: 48 raw bytes from `crypto/rand`, stored as `bytea`. `auth.NewSecrets` keeps
its existing `len(key) >= 32` guard.

**Rationale**: `deploy/k8s/install.sh` and `scripts/production-surface.test.sh` already
generate `openssl rand -base64 48`, so 48 bytes preserves the strength of every installation
that exists today and satisfies FR-002's "at least the current strength". A supplied
`AUTH_SECRET` continues to be used as `[]byte(secret)` exactly as
`server/cmd/market-lens/main.go` does now, so no existing deployment's digests change
meaning.

**Alternatives considered**:

- *Storing base64 text to match the environment variable's shape* — rejected. The variable
  is text only because environments carry text; the database holds bytes, and encoding it
  would invite a decode step that could silently change the key.

## D4. Distinguishing a supplied key from a provisioned one

**Decision**: The row carries `source text CHECK (source IN ('provisioned','supplied'))`.
For `provisioned`, `key_material` holds the bytes and `fingerprint` holds
`HMAC-SHA256(key, "market-lens/instance-signing-key/fingerprint/v1")`. For `supplied`,
`key_material` is `NULL` and only the fingerprint is stored.

**Rationale**: This is what makes FR-005's five outcomes decidable without ever writing an
operator's `AUTH_SECRET` into the database:

| Stored row | `AUTH_SECRET` set | Behavior |
|---|---|---|
| none | no | Provision, record `provisioned`. |
| none | yes | Use it, record `supplied` with its fingerprint only. Nothing is provisioned. |
| `supplied` | yes, fingerprint matches | Start normally. This is every existing deployment. |
| `supplied` | yes, fingerprint differs | Refuse; report that the supplied value changed. |
| `supplied` | no | Refuse; report that the installation expects a supplied value. |
| `provisioned` | yes | Refuse; report the conflict and name both remedies. |

The last two rows are the spec's edge cases, and refusing is the only behavior that does not
silently sign every user out. FR-005's "MUST take precedence" governs the fresh-database case
(Acceptance Scenario 5); its "MUST be reported rather than silently resolved" governs the
cases where a stored row already disagrees, and a refusal that names both remedies — remove
the variable, or rotate deliberately — is that report.

**Rationale for storing a fingerprint at all**: it is an HMAC of a fixed public label under a
key of at least 32 random bytes, so it is not invertible and not brute-forceable. It is the
same construction `auth.Secrets.Digest` already writes to `sessions.token_digest`, so the
database gains no capability it did not already have.

**Alternatives considered**:

- *Storing no fingerprint and comparing nothing* — rejected. A changed or removed
  `AUTH_SECRET` would then be indistinguishable from a first start, which is the silent
  sign-out this feature exists to remove.
- *Storing the supplied key itself once seen* — rejected. It would move an operator's
  externally held secret into the database without them asking, and it would make removing
  the variable a silent no-op rather than a reported change.

## D5. Where resolution happens in start-up

**Decision**: A new step in `run()` in `server/cmd/market-lens/main.go`, after
`db.Migrate(ctx, pool)` and before any service is constructed. The resolved key replaces
`cfg.Auth.Secret` as the argument to `auth.NewSecrets` in both `newIdentityService` and
`newAuthenticationService`. `config.AuthConfig.Secret` keeps only the *supplied* value.

**Rationale**: The existing order is already load config → connect → migrate → validate
external credentials → build services, so the key is resolvable at exactly the point the
credential check already runs. Nothing before that point needs the key. Both CLI commands and
the HTTP server pass through this path, so `auth setup-link`, `auth owner-password reset`,
and `auth credential-key rotate` inherit the resolved key with no change of their own.

**Consequence for `loadAuth`**: the production guard `AUTH_SECRET is required in production`
is removed, because FR-001 requires a production start with only `DATABASE_URL`. The
minimum-length validation of a *supplied* value stays, so a too-short value is still rejected
before the process touches the database. This removal is the feature's initial red test.

## D6. Rotation

**Decision**: A host command `auth signing-key rotate`, executing one transaction that
generates a new key, updates the singleton row to `source='provisioned'` with
`generation = generation + 1`, revokes every unrevoked session with the new reason
`signing_key_rotated`, deletes outstanding `auth_capabilities` and member codes, clears
`auth_rate_events`, appends `signing_key.rotated.v1` to `security_audit_events`, and inserts
a `sessions.revoked.v1` client event.

**Rationale**: The signing key is the HMAC key behind every `*_digest` column —
`sessions.token_digest`, `sessions.csrf_digest`, `auth_capabilities.token_digest`,
member `code_digest`, and `auth_rate_events.bucket_digest`. After rotation none of them can
ever be verified again, so leaving them in place would leave rows that can only fail. Clearing
them in the same transaction is what makes FR-007's "never between two keys" true. The
transaction mirrors `credentials.Repository.Rotate`, which already performs precisely this
shape of work, so the pattern is established rather than invented.

**Confirmation**: rotation requires an interactive terminal and prints the effect before
committing, matching `executeOwnerPasswordReset`. The audit metadata records the new
generation number and never the key.

**Alternatives considered**:

- *An owner-authenticated HTTP endpoint* — rejected. An owner session is itself derived from
  the key being replaced, so authorizing rotation with one is circular. Host access is the
  correct boundary, as the spec states.
- *Keeping the previous key for a grace period* — rejected. It contradicts FR-007's single
  usable key and would keep a compromised key live exactly when it must not be.

## D7. The credential key stays external, and says so

**Decision**: `EXTERNAL_CREDENTIAL_KEY` remains an environment value. Its production
requirement moves out of `config.loadExternalCredentials` and into the database-aware check
that `validateExternalCredentialConfiguration` already performs, which becomes:

| Stored credentials | Key supplied | Behavior |
|---|---|---|
| none | no | Start; warn once, naming the value and what cannot be stored without it. |
| none | yes | Start. |
| present | no | Refuse; name the value and what becomes unreadable. Describe nothing stored. |
| present | wrong | Refuse with the existing "does not match stored credentials" message. |

**Rationale**: Today `loadExternalCredentials` fails a production start whenever the variable
is absent, before any database connection exists. That guard directly contradicts FR-001, and
it is also the wrong guard: what matters is not whether the variable is set but whether
anything encrypted under it exists. Moving the decision to where the ciphertext is visible
makes a fresh production install start with `DATABASE_URL` alone (SC-001) while making a
restore that would lose credentials refuse loudly (SC-006).

**No move into the database**: this decision is settled and out of scope for any task in this
plan. The plan adds no code path, column, or command that could place the credential key in
PostgreSQL.

## D8. Reporting configuration state

**Decision**: Extend the existing owner-only `GET /api/v1/owner/integrations` response with a
sibling `configuration` object naming which values are self-provisioned and which the
operator must retain. One structured log line at start reports the same facts.

**Rationale**: The spec's Responsive UI section states this feature adds no screen and reuses
the integration-status view, so extending that response is the change that satisfies FR-009
without new UI. The object contains booleans and names only — never a key, a fingerprint, or
a length.

**Alternatives considered**:

- *A new `/api/v1/owner/configuration` endpoint* — rejected as a second surface for one
  object the owner already fetches on the same screen.
- *Reporting through `/api/v1/ready`* — rejected. Readiness is unauthenticated; it must not
  describe an installation's secret configuration.

## D9. Test strategy

**Decision**:

- **Red test first**: `server/internal/config/auth_test.go` asserts that loading with
  `ENV=production` and no `AUTH_SECRET` succeeds. It fails today with
  `AUTH_SECRET is required in production` — a behavioral assertion on the returned error,
  not a compile or fixture failure.
- **Concurrency (SC-003)**: an integration test in `server/internal/auth` starting 100
  goroutines that resolve against one empty `testdb` database, asserting one row and one
  distinct key. `server/internal/auth/concurrency_integration_test.go` establishes the shape.
- **Migration proof**: `server/internal/db/instance_signing_key_migration_test.go`, following
  `external_credentials_migration_test.go`, proving both a clean install and an upgrade from
  the 0010 schema.
- **Secret non-disclosure (SC-004)**: a lifecycle test capturing `slog` output, HTTP
  responses, audit rows, client events, and command output over provision → sign in →
  rotate, scanning every byte for the key material and its base64 form.
- **Restart and restore (SC-002)**: an integration test that resolves a key, signs in,
  discards and rebuilds every service against the same database, and asserts the session
  still verifies.
- **Production surface**: `scripts/production-surface.test.sh` gains a run with only
  `DATABASE_URL`, which is the only test in the repository that exercises the built image
  behind the real Go router. Per the container-CI lesson recorded for this project, this is
  the check that proves the deployed artifact starts, not the Playwright suite.

**Rationale**: Each success criterion has exactly one automated owner, and every one runs in
an existing suite with an existing pattern to copy.
