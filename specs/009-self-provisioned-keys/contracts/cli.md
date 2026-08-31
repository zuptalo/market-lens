# Host Command Contract: Signing Key

**Feature**: `009-self-provisioned-keys` | **Date**: 2026-08-31

Extends the existing `auth` command family in `server/cmd/market-lens/main.go`. Host access
is the authorization boundary — an owner session is derived from the key these commands
manage, so authorizing them with one would be circular.

## `market-lens auth signing-key rotate`

Replaces the instance signing key and ends every session.

**Preconditions**

- The database is reachable and migrated.
- A signing key row exists. Rotating before first provisioning is refused; a start
  provisions.
- Standard input is an interactive terminal. Non-interactive invocation is refused, matching
  `auth owner-password reset`.

**Interaction**

```text
$ market-lens auth signing-key rotate
This ends every active session and invalidates outstanding invitations, owner setup
links, and login codes. Type ROTATE to continue: ROTATE
signing_key_rotation=complete generation=2 sessions_revoked=all
```

**Behavior**

One transaction, committed or rolled back whole (FR-007):

1. Generate 48 bytes from `crypto/rand`.
2. `UPDATE instance_signing_key SET source='provisioned', key_material=$1, fingerprint=$2,
   generation=generation+1, rotated_at=$3`.
3. Revoke every unrevoked session with `revoked_reason='signing_key_rotated'`.
4. Revoke usable `auth_capabilities`, pending `invitations`, and active
   `member_login_challenges`; delete `auth_rate_events`.
5. Append `signing_key.rotated.v1` to `security_audit_events` with metadata
   `{"generation": <int>}`.
6. Insert `signing_key.rotated.v1` (scope `owner`) and `sessions.revoked.v1` (scope `user`)
   into `client_events`.

**Exit codes**: `0` on success; non-zero with a message on refusal or failure.

**Never emitted**: key material, its base64 form, its fingerprint, or its length — in stdout,
stderr, logs, audit metadata, or events. `generation` is an ordinal and is safe.

**Failure**: a rotation that fails part way rolls back, leaving the previous key in force and
every session intact. Retrying is safe (FR-007, Story 2 scenario 3).

## Start-up refusals

These are not commands, but they are the operator-facing contract of a start. Each names the
variable and the remedy, and describes nothing that is stored.

| Condition | Message |
|---|---|
| Row is `supplied`, `AUTH_SECRET` unset | `AUTH_SECRET was supplied when this installation was first started and is now missing; restore it, or run auth signing-key rotate to replace it and sign everybody out` |
| Row is `supplied`, `AUTH_SECRET` differs | `the supplied AUTH_SECRET does not match the value this installation was started with; restore the previous value, or run auth signing-key rotate to adopt a new one` |
| Row is `provisioned`, `AUTH_SECRET` set | `this installation provisioned its own signing key; remove AUTH_SECRET from the environment, or run auth signing-key rotate to replace the stored key` |
| Stored credentials exist, `EXTERNAL_CREDENTIAL_KEY` unset | `EXTERNAL_CREDENTIAL_KEY is required because encrypted provider credentials are stored; without it the stored EODHD key and SMTP password cannot be read` |

## Start-up log line

Emitted once per start at `INFO`, satisfying FR-009's "at start":

```json
{"level":"INFO","msg":"instance configuration resolved",
 "signing_key":"provisioned","signing_key_generation":1,
 "external_credential_key":"supplied","operator_must_retain":["EXTERNAL_CREDENTIAL_KEY"]}
```

`signing_key` is `provisioned` or `supplied`. `operator_must_retain` is empty when no
provider credentials are stored and no `AUTH_SECRET` is supplied — the state SC-007
describes, where the operator retains one value beyond the database instead of three.
