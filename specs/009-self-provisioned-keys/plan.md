# Implementation Plan: Self-Provisioned Signing Key

**Branch**: `009-self-provisioned-keys` | **Date**: 2026-08-31 |
**Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/009-self-provisioned-keys/spec.md`

## Summary

A production start requires three coupled values today, and two of them fail silently when
lost. This feature reduces the required set to `DATABASE_URL` by provisioning the signing key
on first start and storing it with the data, so a database backup is a complete, restorable
installation.

The technical approach is small and sits entirely inside the existing start-up sequence.
Ordered migration `0011` adds a singleton `instance_signing_key` table. A resolution step runs
in `run()` immediately after `db.Migrate`, where the credential-configuration check already
runs, and supplies the resolved key to `auth.NewSecrets` in place of `config.AuthConfig.Secret`.
A supplied `AUTH_SECRET` still wins and is never written to the database — only a
non-invertible fingerprint of it is, which is what lets a changed or removed value be reported
instead of silently signing everybody out. A host command rotates the key in one transaction.

`EXTERNAL_CREDENTIAL_KEY` stays external. Its production requirement moves from
`config.loadExternalCredentials`, which fails before a database connection exists, to the
database-aware check that can see whether any ciphertext actually needs it. That relocation is
what allows a fresh production install to start with `DATABASE_URL` alone while making a
restore that would lose provider credentials refuse loudly.

## Technical Context

**Language/Version**: Go 1.26 (backend, the whole of this feature); TypeScript 5 / Vue 3 only
insofar as the existing owner integration-status view renders one added object.

**Primary Dependencies**: standard library `crypto/rand`, `crypto/hmac`, `crypto/sha256`;
pgx/pgxpool; `log/slog`. No new dependency.

**Storage**: PostgreSQL. One new table, `instance_signing_key`, and a replaced CHECK
constraint on `sessions.revoked_reason`. Migration `0011_instance_signing_key.sql`.

**Testing**: Go tests in `internal/config`, `internal/db`, `internal/auth`, `internal/api`,
and `cmd/market-lens`; `scripts/production-surface.test.sh` for the built image;
`deploy/k8s/test.sh` for the manifests. Vitest for the one client-side status field.

**Responsive UI Verification**: N/A. No new screen. The `configuration` object is added to a
response the owner integration-status view already fetches, and that view's 360x800,
768x1024, 1440x900, and 320px behavior is unchanged and already covered.

**Live Delivery**: Rotation publishes `signing_key.rotated.v1` (scope `owner`) and reuses the
existing `sessions.revoked.v1` (scope `user`), both inserted in the same transaction as the
key replacement, so a reconnecting client cannot miss a committed rotation. No new client
event handling: the client already acts on `sessions.revoked.v1`. Resumption, ordering, and
duplicate-safety are the existing `client_events` contract, unchanged.

**Identity and Ownership**: Bootstrap and invitations are unchanged. The stored key is
instance configuration, not user data, and carries no owner column. Reading or rotating it
requires host access rather than an owner session, because an owner session is itself derived
from it — authorizing rotation with one would be circular. The `configuration` object is
served only behind the existing `httpx.RequireOwner` guard on
`GET /api/v1/owner/integrations`.

**PWA and Notifications**: N/A.

**Red-Green-Refactor Proof**: The first test asserts that `config.Load()` with
`ENV=production` and no `AUTH_SECRET` returns no error. It fails today with
`AUTH_SECRET is required in production` — a behavioral assertion on the returned error value,
not a compilation or fixture failure. Green is the `./internal/config`, `./internal/auth`,
`./internal/db`, and `./cmd/market-lens` suites plus the concurrency and restart integration
tests. Full checkpoint list in [quickstart.md](quickstart.md).

**Database Evolution**: One ordered migration, `0011_instance_signing_key.sql`: create
`instance_signing_key`, create the singleton unique index, add the table comment recording the
boundary, and replace `sessions_revoked_reason_check_v2` with a `_v3` constraint adding
`signing_key_rotated`. `server/internal/db/instance_signing_key_migration_test.go` proves both
a clean install and a real upgrade from the `0010` schema. Migrations `0001`–`0010` are not
edited. No manual step exists at any point, including for existing production.

**Target Platform**: Linux container, one Market Lens image plus PostgreSQL.

**Project Type**: Go modular monolith with an embedded Vue client.

**Performance Goals**: Resolution is one `INSERT ... ON CONFLICT DO NOTHING` and one `SELECT`
per process start. No request-path cost: the key is held in the existing `auth.Secrets` value
exactly as the environment-supplied key is today.

**Constraints**: Provisioning must be correct under simultaneous starts with no advisory lock
(SC-003). No key material may reach any output (SC-004). No existing deployment may be
re-keyed by upgrading (FR-005).

**Scale/Scope**: One key per installation. Roughly 400 lines of production Go across five
packages, one migration, one host command, one added response object, and the ops files that
stop generating `AUTH_SECRET`.

## Constitution Check

*Re-evaluated after Phase 1 design; unchanged.*

| Principle | Assessment |
|---|---|
| I. Specification-driven | Spec reviewed, checklist complete, no `[NEEDS CLARIFICATION]`. The central decision — which key may move — is argued in the spec and settled by the operator. **Pass.** |
| II. Modular monolith | One Go application, one image, no new service or infrastructure. Resolution lives in `internal/auth` beside the `Secrets` type that consumes it. **Pass.** |
| III. Migration-only | The table, the index, and the constraint change are one ordered migration with a clean-install and upgrade test. First-start provisioning is ordinary application persistence through reviewed code, not operational mutation. No manual SQL for deployment or repair, including for the existing production database. **Pass.** |
| IV. Versioned contracts | `signing_key.rotated.v1` follows the existing event-type pattern and versioning. The one HTTP change is an additive object on an existing `/api/v1` path, specified in `contracts/openapi.yaml`. **Pass.** |
| V. Correctness | Resolution is a pure decision over (stored row, environment) with all six outcomes enumerated and individually tested. Rotation is one transaction. **Pass.** |
| VI. Test-driven | Every change has a named test that fails first for a behavioral reason; the initial red is identified precisely. Nine checkpoints in `quickstart.md`. No existing test is weakened — in particular the production fail-closed tests for the credential key are kept and extended, not deleted. **Pass.** |
| VII. PrimeVue-first responsive UI | No new user-facing surface. The added object renders in an existing owner view whose responsive behavior is unchanged. **Pass** (gate not applicable rather than waived). |
| VIII. Self-hosted simplicity | Reduces required configuration from three values to one, which is this principle's direction. No new infrastructure. `EXTERNAL_CREDENTIAL_KEY` remains out of the image, out of build arguments, and out of the database. **Pass.** |
| IX. Secure identity and isolation | No change to who may hold an account. Custom cryptography is not introduced: provisioning is `crypto/rand`, and the fingerprint is the same HMAC-SHA256 construction `auth.Secrets.Digest` already uses. Rotation's authorization boundary is host access, argued above. **Pass.** |
| X. Live updates | Rotation's events are transactionally coupled to the state change. No notification behavior is added. **Pass.** |

**Gate result: pass, no violations.** The Complexity Tracking table is therefore empty.

**The gate that matters most here** is III together with VI: this feature stores a secret, so
the migration proof is mandatory and the non-disclosure evidence (SC-004) must cover a
complete lifecycle rather than a single code path.

## Project Structure

### Documentation (this feature)

```text
specs/009-self-provisioned-keys/
├── plan.md              # This file
├── spec.md              # Reviewed specification
├── research.md          # Phase 0: nine resolved decisions
├── data-model.md        # Phase 1: table, invariants, transitions, events
├── quickstart.md        # Phase 1: implementation/verification contract
├── contracts/
│   ├── cli.md           # auth signing-key rotate; start-up refusals; start log line
│   └── openapi.yaml     # configuration object on GET /api/v1/owner/integrations
├── checklists/
│   └── requirements.md  # Complete
└── tasks.md             # Phase 2 output, created by /speckit.tasks — not by this plan
```

### Source code

```text
server/
├── cmd/market-lens/
│   ├── main.go                              # resolve key after db.Migrate; auth signing-key rotate
│   └── auth_test.go                         # command routing, TTY refusal, secret-free output
├── internal/config/
│   ├── auth.go                              # drop the production AUTH_SECRET guard; keep length check
│   ├── auth_test.go                         # THE INITIAL RED
│   ├── external_credentials.go              # drop the production presence guard; keep shape validation
│   └── external_credentials_test.go         # presence now decided against stored ciphertext
├── internal/auth/
│   ├── signing_key.go                       # NEW: resolution, provisioning, fingerprint, rotation
│   ├── signing_key_test.go                  # NEW: six resolution outcomes and their messages
│   ├── signing_key_integration_test.go      # NEW: concurrency, restart, rotation transaction
│   ├── repository.go                        # rotation transaction; revoke-all with the new reason
│   └── model.go                             # RevokeSigningKeyRotated
├── internal/credentials/
│   └── repository.go                        # ValidateConfiguration reports required-but-missing
├── internal/api/
│   ├── auth.go                              # configuration object on the integrations response
│   └── auth_test.go                         # owner-only; no secret in the body
└── internal/db/
    ├── migrations/0011_instance_signing_key.sql          # NEW
    └── instance_signing_key_migration_test.go            # NEW: clean install and 0010 upgrade

src/services/          # one added status field and its Vitest assertion
scripts/production-surface.test.sh   # a run with only DATABASE_URL
deploy/k8s/install.sh  # stop generating AUTH_SECRET; never remove an existing one
deploy/k8s/test.sh     # assert the installer no longer generates it
.env.example, server/.env.example, docker-compose.yml, README.md, deploy/k8s/README.md
```

**Structure Decision**: The existing Go modular monolith with an embedded Vue client, as
listed above. Resolution belongs in `internal/auth` because that package already owns
`Secrets`, the only consumer of the key, and already owns the session and capability tables
rotation must clear. Placing it in `internal/config` was rejected: `config` is deliberately
environment-only and has no database access, and giving it one would blur the boundary that
makes configuration loading testable without PostgreSQL.

## Implementation sequence

Phase 2 (`/speckit.tasks`) will expand this; the dependency order is fixed by the design.

1. Migration `0011` and its migration test. Everything else needs the table.
2. Configuration guards relax — the initial red, and the change that makes SC-001 reachable.
3. Resolution and provisioning in `internal/auth`, with the six-outcome test and the
   concurrency test.
4. Start-up wiring in `main.go`; the restart/restore integration test.
5. Rotation: revoke reason, repository transaction, host command, audit and events.
6. Credential-key reporting: relocated requirement, start warning, refusal messages.
7. The `configuration` object on the owner integrations response, and its client field.
8. Non-disclosure lifecycle scan (SC-004), which must run after every producer of output
   exists — this is deliberately last so it covers rotation output too.
9. Ops and documentation: installer, examples, Compose, README, production-surface run.

## Risks

| Risk | Mitigation |
|---|---|
| An upgrade silently re-keys a deployment that supplies `AUTH_SECRET`, signing everybody out — the exact failure this feature removes. | The `supplied` row records a fingerprint on first sight, and every disagreement refuses rather than resolves. Covered by checkpoint 3 and by manual step 3 in `quickstart.md`. |
| A future change stores `EXTERNAL_CREDENTIAL_KEY` in the database because "the other key is there". | The table comment states the boundary at the point of violation, `data-model.md` and `quickstart.md` restate it, and no column, code path, or command in this feature could carry it. |
| Rotation leaves rows whose digests can never verify. | Rotation clears sessions, capabilities, invitations, challenges, and rate events in the same transaction; enumerated in `data-model.md` and covered by checkpoint 6. |
| `db.Migrate` takes no lock, so simultaneous first starts could race on the migration itself. | Pre-existing and out of scope. Key provisioning does not depend on it: the singleton unique index makes the insert idempotent regardless of migration ordering. Noted so a task does not silently widen its scope. |
