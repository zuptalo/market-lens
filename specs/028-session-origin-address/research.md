# Phase 0 research: The address a session was last seen from

Five questions had to be settled before any code could be written. Two came from the spec's owner
decisions; three surfaced while reading the code that would have to change.

## R1 — How the client's address is determined

**Decision**: a pure function in `httpx`:

```go
func ClientAddress(request *http.Request, trusted []netip.Prefix) (netip.Addr, bool)
```

It starts from the peer address (`RemoteAddr`, host part). If the peer is not inside any trusted
prefix, that is the answer and no header is read at all. If it is, the `X-Forwarded-For` chain is
walked **right to left**, and the first hop that is not itself inside a trusted prefix is the
client. If every hop is trusted, or the header is absent, the peer is the answer.

**Rationale**: right-to-left is the only direction that cannot be lengthened by the client. A
client that sends `X-Forwarded-For: 1.2.3.4` has its value pushed leftward by each real proxy that
appends to it, so reading from the right skips exactly the hops the deployment vouches for and
stops at the first one it does not. Reading left-to-right returns whatever the client wrote.

**Alternatives considered**:

- *Leftmost `X-Forwarded-For`*, the common implementation. Rejected by D1: forgeable by anybody,
  and a security screen showing a forged address is worse than one showing none.
- *RFC 7239 `Forwarded`*. Traefik sets `X-Forwarded-For`; supporting a header nothing in this
  deployment sends would be untested code on a security path. Noted as a future addition if the
  ingress ever changes.
- *A `net/http` middleware that rewrites `RemoteAddr`*. Rejected: it makes the untrusted value
  indistinguishable from the trusted one for every other reader of the request.

**Malformed chains**: any hop that does not parse as an address invalidates the whole chain and the
peer address is used. A partially parsed chain is not evidence.

## R2 — Where the trust list comes from

**Decision**: `TRUSTED_PROXIES`, a comma-separated list of CIDR prefixes, empty by default. A bare
address is accepted and read as a single-host prefix (`/32` or `/128`). It is non-secret and belongs
in the Kubernetes ConfigMap beside `ALLOWED_ORIGINS`, not in the secret.

**A malformed value stops the process.** This amends **FR-006**, which as drafted said an
unparseable value means nothing is trusted. The amendment was made here and written back into the
spec, for one reason: a trust list that silently failed to parse produces a screen that looks
completely normal — full of plausible internal addresses — and nobody would ever find out. The
existing configuration code already fails this way for `AUTH_SECRET`, so this follows the house
rule rather than inventing one.

**Rationale for CIDR over a boolean `TRUST_PROXY`**: a boolean cannot express *which* peer is
trusted, so it degenerates into trusting whatever connected — the rejected option in D1.

**Note for this deployment**: the k3s pod sees Traefik from the cluster pod network. The value to
set is that network, and `quickstart.md` says how to read it off a running cluster rather than
guessing it.

## R3 — How the address is stored

**Decision**: two nullable `inet` columns on `sessions` — `created_ip` and `last_seen_ip`. pgx v5
maps `netip.Addr` to `inet` natively, so there is no string handling in the repository at all.

**Rationale**: `inet` validates and canonicalises in the database, which satisfies FR-002 without
writing a formatter, and makes storing a non-address impossible rather than merely unlikely. `text`
would have needed a check constraint that duplicates what the type already does, and would have let
`::ffff:203.0.113.12` and `203.0.113.12` sit in the table as two different devices.

**Alternatives considered**: `text` with a regular-expression constraint (rejected as above);
`bytea` with a 4/16-byte check (rejected: unreadable in a shell, and the product's own operator is
the reader when something goes wrong).

## R4 — Where the write happens

**Decision**: `last_seen_ip` is written in the `UPDATE sessions SET last_seen_at=…,idle_expires_at=…`
that already runs on every authenticated request (`server/internal/auth/repository.go:142`). No new
statement, no new round trip.

**The address is written as determined, including when it is unknown.** If a request's address
cannot be resolved, the column becomes null rather than keeping the previous value. The column means
*the address of the most recent request*; a preserved older address beside a fresh timestamp would
be a quiet lie, and FR-003 already says the row must then say nothing.

**Threading it in**: `AuthenticateSession(ctx, token)` becomes
`AuthenticateSession(ctx, token, clientAddress)`. The alternative — carrying the address in the
request context — was rejected because it makes a security-relevant input invisible at the call
site and untypeable in the interface, and this interface is the one place every authenticated
request passes through.

## R5 — What this does to the existing origin digest

**Finding, discovered while reading the code**: `origin_digest` is written on session creation and
on every audit event, and **is never read back or verified anywhere**. Nothing compares it; no
authentication depends on it. It is a write-only correlation fingerprint.

Worse, behind the ingress it has been a fingerprint of the ingress: every session and every audit
row on the production deployment currently carries the digest of the same internal address, so the
one thing the digest was for — telling two origins apart without revealing either — has not worked
in production since the day it was deployed.

**Decision**: the digest stays exactly where it is, and is now computed over the *resolved client*
address rather than the peer. Nothing about its storage, type, or purpose changes, and no migration
touches it. Digests written before this change are not comparable with those written after — which
costs nothing, because nothing compares them, and which restores the property the column was
supposed to have.

This is the strongest argument that the feature is not merely a convenience: the audit log has been
recording one indistinguishable origin for every event, and resolving the client address fixes that
at the same time as it fills the column the screen will show.
