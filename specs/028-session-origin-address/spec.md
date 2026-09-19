# Feature Specification: The address a session was last seen from

**Feature**: 028 | **Branch**: `028-session-origin-address` | **Milestone**: Security-A
**Status**: planned — two decisions resolved by the owner (D1, D2); no open clarifications
**Created**: 2026-09-19

**Input**: "Signed-in devices should show the network address each session was last seen from,
displayed in full (for example 203.0.113.12), alongside the device name."

## Summary

Signed-in devices answers one question: *is every one of these me?* It currently answers with a
device name alone. A name is weak evidence — two people using Chrome on a Mac produce the same
row, and a stolen session token produces a row that looks exactly like the person it was stolen
from. The address a session is signing in from is the fact that separates them: an owner who has
never left Helsinki and sees a session live from somewhere else has learned something no device
name could tell them.

The product does not have that fact. Sessions store a keyed HMAC of the address (feature 004,
`origin_digest`), which is one-way by design and therefore unreadable by the product itself. This
feature records the address on the session row and shows it, in full, to the one person entitled
to see it: the person whose session it is.

## The decision this amends

Feature 004 chose to keep origins as purpose-separated digests and wrote it down twice:
`origin_digest` is *"keyed coarse origin metadata, not displayed raw"* and *"keyed, never raw
origin"*. That decision was right for what it was protecting — an audit log that accumulates
forever and is read by an owner about other people.

**This feature narrows it, it does not reverse it.**

| Where an origin appears | Before | After |
|---|---|---|
| `auth_events` audit log | Keyed digest | **Keyed digest, unchanged** |
| Login-failure records | Keyed digest | **Keyed digest, unchanged** |
| Session row | Keyed digest | Digest **plus** the address, readable |
| Anyone other than the session's owner | Nothing | **Nothing** |
| A session that can no longer authenticate | Digest | Digest **plus the address it kept** |

The digest on the session row is not verified anywhere in the product — nothing compares it, no
authentication depends on it. So what changes here is not a security check. It is a disclosure:
one person can now read one fact about their own sessions, and the product keeps it.

## Decisions resolved by the owner

### D1 — Which address is the client's?

Production serves through Traefik: the container sees the ingress as its peer, so the address the
product records today is an internal cluster address, the same one for every device. Showing that
would make every row identical and the feature worthless. The client's own address is only
available in a forwarded header, which any client can also set — so the question is what the
product is willing to believe, and from whom.

**Decision: believe a forwarded address only when the immediate peer is a configured trusted
proxy; otherwise record the peer's own address.**

| | |
|---|---|
| Configured | A list of trusted proxy networks, supplied as non-secret deployment configuration and empty by default |
| Behind the ingress | The peer matches a trusted network, so the forwarded chain is read and the first hop that is *not* itself trusted is recorded |
| In development | Nothing is configured, nothing is trusted, the peer address is recorded — which is the right answer there |
| A forged header | Arrives from an untrusted peer, is ignored, and the peer's own address is recorded instead |

The rejected alternative was to believe the leftmost forwarded address unconditionally. It needs
no configuration and is forgeable by anybody: a security screen that shows whatever an attacker's
session claims is worse than one that shows nothing.

### D2 — How long is an address kept?

**Decision: the address stays on the session row for the life of the row, including after the
session is revoked or expires.** A person auditing their account keeps the record of where each
past session was signing in from, which is the evidence that matters after something has gone
wrong rather than before.

The cost is stated rather than discovered later: sessions are never deleted, so the database
accumulates one plaintext address per session for as long as the account exists — twenty-three
already-ended sessions on the first production account means twenty-three stored addresses. That
is a real change in what a database backup contains, and it is the reason feature 004 kept
digests. Everything in *Not disclosing more than the one fact* exists to bound it: the addresses
go nowhere, are readable by one person, and are never joined to anything.

## User scenarios

### US1 — Recognise your own devices (P1)

A person opens Signed-in devices on their phone. Each row names the device and the address that
device was last seen from — `Safari on iPhone`, `203.0.113.12`. They recognise both, and close the
screen having learned that nothing is wrong. This is the ordinary case and it must be the quiet
one: two short facts per row, readable on a 320-pixel screen without scrolling sideways.

**Independently testable**: sign in from a known address, read the list, assert the address shown
is the address the request came from.

### US2 — Fail to recognise one (P1)

The same person sees a third row: a browser they do not use, from an address in a country they
have never signed in from. They revoke it from that screen. The address is what made the row
legible as wrong; without it the row was just another browser.

**Independently testable**: create a second session from a different address, assert both
addresses are shown distinctly, and that revoking one leaves the other untouched.

### US3 — The address follows the session (P2)

A person signs in at home, travels, and uses the product from a hotel. The row shows the hotel's
address, not the home one, because the column says *last seen from* and must not quietly mean
*signed in from* — a session hijacked after sign-in would be invisible under the latter.

**Independently testable**: authenticate a session from one address, then from a second, and
assert the stored address changed with the activity timestamp it accompanies.

### US4 — Nobody else's business (P1)

An owner administering members sees how many devices each member has signed in on. They do not see
those members' addresses, on any screen, in any event, or in any API response.

**Independently testable**: an owner reading every member-facing endpoint receives no address for
any account but their own.

## Functional requirements

### Recording

- **FR-001** A session records the network address of the request that created it and the address
  of the most recent request that authenticated it. The second is updated in the same write that
  advances the session's activity timestamp, so the two can never disagree.
- **FR-002** An address is stored in its canonical text form. Both IPv4 and IPv6 are supported;
  an IPv6 address is stored in its compressed lower-case form rather than as it happened to be
  written on the wire, so the same device does not appear as two different addresses.
- **FR-003** When the address of a request cannot be determined, the session records no address
  rather than a placeholder. A row with nothing recorded says so in words; it never displays
  something that could be mistaken for an address.
- **FR-004** Sessions that exist before this feature ships have no recorded address. Their next
  authenticated request records one. No backfill is performed, and no address is invented for a
  request that already happened.
- **FR-005** The address of a request is the peer's address, except when the peer belongs to a
  configured trusted-proxy network: then it is the first address in the forwarded chain that is
  not itself a trusted proxy. A forwarded header arriving from an untrusted peer is ignored
  entirely (D1).
- **FR-006** The trusted-proxy networks are non-secret deployment configuration, empty by default:
  an absent value trusts nothing, which is the right answer in development. A value that does not
  parse stops the process with a message naming the variable and the entry that failed — amended
  during planning, because a trust list that silently failed to parse produces a screen full of
  plausible internal addresses that nobody would ever question. A malformed forwarded chain is
  discarded whole; the product never records a partially parsed address.

### Showing

- **FR-007** Signed-in devices shows, for each listed session, the device name and the address it
  was last seen from, in full and unredacted.
- **FR-008** The address is shown as text, selectable and copyable, and is never the only thing
  distinguishing two rows — the device name and last-active time remain.
- **FR-009** A person sees addresses only for their own sessions. No screen, endpoint, export or
  event discloses one account's address to another account, including to an owner.
- **FR-010** The address is not rendered as a link, a map, a flag, or a location. The product
  states the address it recorded and makes no claim about where that is or who it belongs to.

### Not disclosing more than the one fact

- **FR-011** No address is written to an event payload. `session.created.v1` and
  `session.revoked.v1` keep their current shape; the list refetches its snapshot as it does today.
- **FR-012** The audit log and login-failure records keep the keyed digest and gain no address.
  The narrowing in *The decision this amends* is bounded to the session row.
- **FR-013** No address appears in an application log line, an error message, or a metric label.
- **FR-014** An address is kept for the life of the session row, including after the session is
  revoked, expires, or its account is deactivated (D2). It is never rewritten by anything other
  than a request that successfully authenticated that same session, so a stored address is always
  a record of something that actually happened.
- **FR-015** An ended session's address is readable by the person it belonged to and by nobody
  else, on the same terms as a live one. Ending a session narrows nothing and widens nothing.

### Ownership and isolation

- **FR-016** The address is user-owned data under the boundary feature 022 established, enforced
  in services and queries rather than by filtering in the client.
- **FR-017** A deactivated or locked account discloses no address to anybody, including the owner
  who deactivated it.

## Scope by exclusion

- **FR-018** No geolocation, no ASN lookup, no reverse DNS, no third-party address service. The
  product sends the address nowhere.
- **FR-019** No sign-in history, no per-request address trail. One address per session — the last
  one. A log of every address a person has ever used is a different feature with a different
  privacy question, and this is not it.
- **FR-020** No alerting on a changed address, and no notification when a session appears from
  somewhere new. Feature 027's consent model would have to cover it first.
- **FR-021** No blocking, rate-limiting, or authorization decision based on the address. Nothing
  about authentication changes.
- **FR-022** No screen for ended sessions. The device list continues to show only sessions that
  can authenticate; the addresses kept under FR-014 are retained data, not a new surface.

## Test-First Proof *(mandatory)*

- **Initial failing test**: a Go integration test that authenticates a session from a known
  address, reads the session list, and asserts the response carries that address for that session.
- **Expected red reason**: the session response has no address field and the service records none,
  so the assertion fails on a missing value — a behavioural failure, not a compile error. The
  companion front-end test asserts the address is rendered in the device row and fails because
  nothing renders it.
- **Green evidence**: `make verify` (Go integration suite, typecheck, unit suite) plus the
  Playwright device-list scenario at the three named viewports.
- **Database migration proof**: a migration test proving a clean database upgrades to the new
  session columns without manual steps, and that an existing session row with no address is valid
  and remains authenticable.

## Responsive UI Behavior *(mandatory)*

- **Mobile (320–767 CSS px)**: the stacked Device cell carries the device name on the first line
  and the address beneath it in a secondary, smaller weight. A full IPv6 address is 39 characters
  and must wrap rather than widen the page; at 360×800 and at 320 CSS px the list scrolls
  vertically only.
- **Tablet (768–1023 CSS px)**: as the table layout, with the address in the Device column beneath
  the name; verified at 768×1024.
- **Desktop (1024+ CSS px)**: the address sits in its own column between Device and Last active;
  verified at 1440×900.
- **Input and accessibility**: the address is selectable text reachable by keyboard, not a tooltip
  and not hover-revealed. It is announced as part of the row, and the Revoke control's accessible
  name continues to identify the device it will revoke.

## Live Update Behavior *(mandatory)*

- **Snapshot and events**: the address arrives only in the authenticated REST snapshot of the
  session list. No new event type, and no change to the payloads of `session.created.v1` or
  `session.revoked.v1` — an event payload is the wrong place for a fact this feature deliberately
  shows to exactly one person.
- **Reliability**: unchanged. The list continues to refetch on the existing user-scoped session
  events, and resumption, ordering and duplicate handling are untouched.
- **Test evidence**: the existing cross-user event-isolation suite is extended with an assertion
  that no session event payload carries an address.

## Identity, Ownership, and Permissions *(mandatory)*

- **Bootstrap and invitations**: unchanged.
- **Ownership and authorization**: a session's address is readable only by the account that owns
  the session, enforced in the query that builds the list. The owner's member administration view
  keeps its device *count* and gains nothing.
- **Security evidence**: a cross-user test asserting a member cannot read another member's
  addresses and an owner cannot read a member's, on every path that returns a session.

### PWA and Notification Behavior

N/A — this feature sends nothing and changes no notification behaviour.

## Success criteria

- **SC-001** A person reading their own device list sees, for every listed session, the address
  that session's most recent request came from — matching the address the request actually
  originated at, not the proxy in front of it.
- **SC-002** Using a session from a second address changes the address shown on that row, and
  changes no other row.
- **SC-003** No account can obtain another account's address through any endpoint, event, or
  export, proved by cross-user tests.
- **SC-004** A revoked or expired session still holds the address it was last seen from, and that
  address is unchanged from the moment the session stopped being usable — asserted by reading the
  stored row directly before and after revocation.
- **SC-005** No address appears in any event payload or log line, asserted across the event
  catalogue and the notification templates.
- **SC-006** The device list renders at 360×800, 768×1024 and 1440×900, and tolerates 320 CSS px
  with a full-length IPv6 address without page-level horizontal scrolling.
- **SC-007** A session created before this feature shipped is listed without an address and gains
  one on its next use, with no manual step and no migration backfill.
- **SC-008** A forwarded header sent by a client the deployment does not trust changes nothing
  about what is recorded, proved by a request that forges one against an untrusted peer.
- **SC-009** Behind a configured trusted proxy, two devices signing in from two different public
  addresses produce two different rows — the outcome the deployment does not get today.

## Key entities

- **Session origin** — two values on an existing session: the address it was created from and the
  address it was last seen from. Nullable while unrecorded, kept for the life of the row once
  written, readable only by the session's owner, never shared and never derived from.
- **Trusted proxy networks** — non-secret deployment configuration, empty by default, naming the
  networks whose forwarded headers the product will read.

## Assumptions

- The device name shown beside the address is already derived from the user-agent string rather
  than printed whole. That change shipped separately and is a prerequisite for this row being
  readable at all on a phone.
- Addresses are kept for the life of the session row (D2), and sessions are never deleted today.
  A deployment's database therefore grows one plaintext address per sign-in, indefinitely. That is
  accepted deliberately; if a retention rule is wanted later it is a schema change with its own
  spec, and it can be applied to the accumulated rows by migration.
- No sweeper, no background job, and no new scheduled work: this feature writes on a path that
  already writes.
- The keyed `origin_digest` column stays exactly as feature 004 defined it. Applied migrations are
  never edited; this feature adds columns.
