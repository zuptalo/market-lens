# Data model: The address a session was last seen from

## Changed table: `sessions`

Two columns are added by `0033_session_origin_address.sql`. Nothing existing is altered, dropped, or
backfilled, and `0007_identity_access.sql` — where this table was defined — is not edited.

| Column | Type | Null | Meaning |
|---|---|---|---|
| `created_ip` | `inet` | yes | The client address of the request that created the session |
| `last_seen_ip` | `inet` | yes | The client address of the most recent request that authenticated it |

**Why nullable, and why no backfill.** Every session that exists when this ships was created from a
request that is long over; there is no address to write and none may be invented (FR-004). A null
means *not recorded*, the screen says so in words, and the next authenticated request fills it in.

**Why `inet`.** The type validates and canonicalises. `203.0.113.12` and an IPv6 address written in
any of its legal forms each store as exactly one value, so the same device cannot appear as two
(FR-002). pgx v5 maps `netip.Addr` to `inet` directly, so no code in the repository parses or
formats an address.

**Retention.** None. Both columns live for the life of the row, including after the session is
revoked or expires (D2, FR-014). Sessions are never deleted, so a deployment accumulates one
address per sign-in; the cost is stated in the spec rather than left to be discovered.

**Unchanged on this table**: `origin_digest` keeps its type, its not-null constraint, and its
purpose. From this feature onward it digests the resolved client address rather than the peer's
(R5), which is a change in what it fingerprints, not in what it is.

## Go model

```go
type Session struct {
    // … existing fields …
    DeviceLabel  string
    OriginDigest []byte
    CreatedFrom  netip.Addr // zero value means not recorded
    LastSeenFrom netip.Addr // zero value means not recorded
}

type SessionSummary struct {
    // … existing fields …
    DeviceLabel  string
    CreatedFrom  netip.Addr
    LastSeenFrom netip.Addr
}
```

`netip.Addr`'s zero value is invalid (`IsValid() == false`), which is exactly the *not recorded*
state and needs no separate flag. `Session.Validate` gains one rule: an address that is present must
be valid — it may not be a zero value dressed up as a set one.

## Configuration

| Name | Type | Default | Secret |
|---|---|---|---|
| `TRUSTED_PROXIES` | Comma-separated CIDR prefixes; a bare address means a single host | empty — nothing is trusted | No. It belongs in the ConfigMap beside `ALLOWED_ORIGINS` |

Parsed once at startup into `[]netip.Prefix`. A value that does not parse stops the process with a
message naming the variable and the offending entry (R2).

## State

The addresses have no lifecycle of their own. They follow the session:

```text
session created   → created_ip = client address of that request (or null)
                    last_seen_ip = the same
each authenticated request
                  → last_seen_ip = client address of that request (or null),
                    written in the statement that already advances last_seen_at
revoked / expired → both columns unchanged, and kept (D2)
```

## Contract surface

`GET /api/v1/account/sessions` gains two fields per item:

| Field | Type | Meaning |
|---|---|---|
| `last_seen_from` | string or null | The address shown beside the device name |
| `created_from` | string or null | The address the session was created from |

Both are returned only for the caller's own sessions. No other endpoint gains an address, and no
event payload carries one (FR-011).
