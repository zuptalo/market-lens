# Phase 1 Data Model: Self-Provisioned Signing Key

**Feature**: `009-self-provisioned-keys` | **Date**: 2026-08-31

One new table and two constraint changes to existing tables. All of it arrives in ordered
migration `0011_instance_signing_key.sql`. No table in this feature stores
`EXTERNAL_CREDENTIAL_KEY` or anything derived from it.

## Entity: Instance signing key

The self-provisioned secret used to derive session, CSRF, capability, origin, and
login-code digests. Exactly one per installation, created once, replaced only by explicit
rotation, never emitted.

```sql
CREATE TABLE instance_signing_key (
    id            uuid PRIMARY KEY,
    source        text NOT NULL CHECK (source IN ('provisioned', 'supplied')),
    key_material  bytea CHECK (key_material IS NULL OR octet_length(key_material) BETWEEN 32 AND 128),
    fingerprint   bytea NOT NULL CHECK (octet_length(fingerprint) = 32),
    generation    integer NOT NULL CHECK (generation > 0),
    created_at    timestamptz NOT NULL,
    rotated_at    timestamptz,
    CHECK ((source = 'provisioned') = (key_material IS NOT NULL)),
    CHECK (rotated_at IS NULL OR rotated_at >= created_at)
);

CREATE UNIQUE INDEX instance_signing_key_singleton ON instance_signing_key ((true));

COMMENT ON TABLE instance_signing_key IS
    'The signing key protects rows in this same database, so it may live here. '
    'EXTERNAL_CREDENTIAL_KEY encrypts secrets against this database being read and must '
    'never be stored in it. See specs/009-self-provisioned-keys/spec.md.';
```

| Field | Meaning |
|---|---|
| `source` | `provisioned` when the application generated the key; `supplied` when the operator sets `AUTH_SECRET`. |
| `key_material` | The 48 random bytes, present only for `provisioned`. Always `NULL` for `supplied`, so an operator's externally held secret is never written here. |
| `fingerprint` | `HMAC-SHA256(key, "market-lens/instance-signing-key/fingerprint/v1")`. Lets a start detect a changed or removed `AUTH_SECRET` without storing it. |
| `generation` | Starts at 1, increments on every rotation. The only key identifier that may appear in logs or audit metadata. |
| `rotated_at` | `NULL` until the first rotation. |

### Invariants

- **Exactly one row, forever.** Guaranteed by `instance_signing_key_singleton`, which is what
  makes concurrent first starts converge (FR-003, SC-003) without a lock.
- **A supplied key is never persisted.** The `(source = 'provisioned') = (key_material IS NOT
  NULL)` check makes the alternative unrepresentable.
- **The row is never deleted.** Rotation is an `UPDATE`; there is no delete path, so an
  installation cannot fall back to provisioning a second key after having had one.

### State transitions

```text
                  no row
                 /      \
   AUTH_SECRET unset    AUTH_SECRET set
        |                     |
   source=provisioned    source=supplied
   generation=1          generation=1, key_material=NULL
        |                     |
        |  rotate             |  rotate
        v                     v
   source=provisioned, generation+1, rotated_at=now
```

Start-up resolution against an existing row is a pure decision — it writes nothing:

| Row `source` | `AUTH_SECRET` | Resolution |
|---|---|---|
| `provisioned` | unset | Use `key_material`. |
| `provisioned` | set | **Refuse.** Report the conflict; name removing the variable or rotating. |
| `supplied` | set, fingerprint matches | Use the supplied value. |
| `supplied` | set, fingerprint differs | **Refuse.** Report that the supplied value changed. |
| `supplied` | unset | **Refuse.** Report that this installation expects a supplied value. |

Every refusal names the variable and the remedy. None reveals key material, its fingerprint,
or its length.

## Changes to existing entities

### `sessions.revoked_reason`

Migration `0011` replaces the `sessions_revoked_reason_check_v2` constraint added by `0009`
with a `_v3` constraint adding `signing_key_rotated`, and `auth.RevokeReason` gains the
matching constant. Reusing `administrative` was rejected: an operator reading the audit trail
after a rotation must be able to tell why every session ended.

### Rows cleared by rotation

Every one of these carries a digest computed under the old key, so after rotation none can
ever verify. Rotation clears them in the same transaction that replaces the key.

| Table | Column | Action on rotation |
|---|---|---|
| `sessions` | `token_digest`, `csrf_digest`, `origin_digest` | `revoked_at = now`, `revoked_reason = 'signing_key_rotated'` for every unrevoked row. |
| `auth_capabilities` | `token_digest` | `revoked_at = now` for every usable row. An outstanding owner-setup link stops working, which is the intended effect. |
| `invitations` | `token_digest` | Pending rows become `state = 'revoked'`, `revoked_at = now`. Accepted, revoked, and expired rows are untouched, so the invitation history survives. |
| `member_login_challenges` | `code_digest` | Active rows become `state = 'revoked'`, `invalidated_at = now`. Codes in flight can no longer be verified. |
| `auth_rate_events` | `bucket_digest` | Deleted. The table has no state column and its buckets are keyed by a digest under the old key, so surviving rows would neither match nor expire. Deleting them does not unlock anybody: the 3-of-10 member lockout lives in `member_login_state.consecutive_failures`, which rotation leaves alone. |

`security_audit_events.origin_digest` is **not** cleared: it is an append-only historical
record, protected by the `security_audit_events_append_only` trigger, and its rows are never
verified against a live key.

## Events

| Event | Version | Scope | Emitted when | Payload |
|---|---|---|---|---|
| `signing_key.rotated.v1` | 1 | `owner` | Rotation commits | `{"generation": <int>}` |
| `sessions.revoked.v1` | 1 | `user` | Rotation commits | Existing shape, reused unchanged |

`signing_key.rotated.v1` is inserted into both `security_audit_events` and `client_events` in
the rotation transaction, matching `credential.key_rotated.v1`. It satisfies the
`^[a-z_]+\.[a-z_]+\.v[1-9][0-9]*$` constraint from `0007`. Neither payload contains key
material; `generation` is an ordinal, not a secret.

Connected clients learn of a rotation through `sessions.revoked.v1`, which the client already
handles — the spec introduces no new client-side event handling.
