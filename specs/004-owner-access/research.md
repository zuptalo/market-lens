# Research: Owner Access and Invitations

## Decision 1: Separate owner and member authentication

**Decision**: The single owner authenticates with a strong password and verified-email
recovery. Invited members never create password credentials and authenticate with the
latest six-digit code delivered to their verified email.

**Rationale**: This exactly preserves the user's explicit role distinction. Keeping the
flows separate prevents a member password endpoint from becoming an undocumented
fallback and lets owner recovery remain available when transactional member login is
temporarily blocked.

**Alternatives considered**: Passwords for all users contradict the requirement;
passwordless owner login removes the requested distinction and increases dependence on
email availability; social/enterprise identity adds unapproved integrations.

## Decision 2: Argon2id for the owner credential

**Decision**: Encode the owner's password with Argon2id using a unique 128-bit salt and
self-describing parameters. Begin with OWASP's 19 MiB, two-iteration, one-lane minimum,
benchmark on the production container, and allow parameters to increase without schema
change. Enforce a minimum 12-character password and permit at least 64 Unicode
characters without composition rules.

**Rationale**: Argon2id is the current memory-hard recommendation for new password
storage. Self-describing hashes allow rehash-on-login after future parameter changes.
The implementation uses the reviewed `golang.org/x/crypto/argon2` primitive rather than
custom cryptography.

Primary sources: [OWASP Password Storage Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html)
and [RFC 9106](https://datatracker.ietf.org/doc/html/rfc9106).

**Alternatives considered**: SHA-family fast hashes are unsuitable for passwords;
bcrypt is mainly a legacy choice and has input-length complications; reversible
encryption exposes plaintext after key compromise.

## Decision 3: Opaque, server-side sessions

**Decision**: Generate at least 256 random bits for each session token, send it only in
a `Secure`, `HttpOnly`, `SameSite=Lax`, host-only cookie, and store only a server-keyed
digest. Enforce eight hours of inactivity and a 30-day absolute lifetime server-side.
Rotate on successful authentication/recovery, revoke individually or globally, and use
a separate same-origin CSRF token/header for state-changing requests.

**Rationale**: An opaque token permits immediate database-backed revocation, per-device
management, role/status re-evaluation, and SSE termination without JWT invalidation
complexity. Server-side expiry is authoritative.

Primary source: [OWASP Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html).

**Alternatives considered**: Browser local storage exposes bearer tokens to script;
self-contained JWTs complicate immediate revocation; source-IP binding breaks normal
mobile/network changes and is not a reliable identity factor.

## Decision 4: Random capabilities and digest separation

**Decision**: Bootstrap, invitation, and recovery links contain independent 256-bit
random URL-safe capabilities, are single-use and expiring, and persist only a keyed
HMAC-SHA-256 digest plus non-secret metadata. Session, capability, and member-code
digests use purpose-separated keys derived from one required server secret.

**Rationale**: High-entropy random values resist guessing and keyed digests prevent a
database-only attacker from replaying stored values. Purpose separation prevents a
value from one flow being accepted by another. Recovery invalidates earlier recovery
capabilities and prior owner sessions.

Primary source: [OWASP Forgot Password Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Forgot_Password_Cheat_Sheet.html).

**Alternatives considered**: Persisting plaintext makes a database read an account
takeover; secrets in URL query strings leak into logs/referrers, so capability links use
a fragment that the SPA exchanges in a request body; signed self-contained tokens make
revocation and one-use enforcement harder.

## Decision 5: Six-digit member codes with latest-only issuance

**Decision**: Generate codes uniformly with rejection sampling from `crypto/rand` over
`000000`–`999999`, preserve leading zeros, expire after ten minutes, allow one use, and
invalidate every earlier unused challenge when a newer one is committed. Store only an
HMAC digest keyed separately from other tokens; compare in constant time.

**Rationale**: The user's six-digit requirement provides a small online search space,
so secrecy depends on strict online controls rather than code complexity. A server-keyed
digest prevents offline enumeration after database-only disclosure. Latest-only behavior
handles delayed/out-of-order email predictably.

**Alternatives considered**: Random alphanumeric codes violate the specified UX;
multiple simultaneously valid codes expand the attack surface and confuse users;
plain or unkeyed code hashes are enumerable offline.

## Decision 6: Account and origin controls are independent

**Decision**: Track wrong-code outcomes durably against the member regardless of device
or network, and independently limit origin activity across emails. Three consecutive
wrong codes create a 15-minute account block. Ten wrong submissions within a rolling
24-hour window create an administrative lock that only the owner can clear. Code
delivery is limited to one per member per minute and five per rolling hour; origin
limits are configurable but bounded defaults and generic `429` responses are mandatory.

**Rationale**: Account counters stop distributed guessing; origin counters stop one
source spraying many accounts. Independent buckets avoid the bypass created by one
combined email/origin key. Uniform responses and coarse retry guidance avoid account-
existence disclosure. Lockout is deliberately durable because the user requires owner-
only recovery after the tenth failure.

Primary sources: [OWASP Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)
and [OWASP Bot Management and Anti-Automation Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Bot_Management_and_Anti-Automation_Cheat_Sheet.html).

**Alternatives considered**: IP-only counters are bypassed through distributed sources;
account-only counters permit broad credential spraying; a global limiter creates an
instance-wide denial of service; CAPTCHA adds an external/browser dependency and does
not replace throttling.

## Decision 7: Lockout denial-of-service mitigation

**Decision**: Requests for unknown/inactive/blocked/locked emails follow the same public
response envelope and roughly equivalent work path, but never send a code. Repeated
requests do not themselves increment wrong-code counters. Only verification submissions
against an active challenge count; after a challenge is exhausted or the temporary
block begins, further submissions during the block do not inflate the rolling count.
The owner sees a safe audit summary and can unlock an active member.

**Rationale**: A known email can still be deliberately attacked, which is inherent in
the explicit permanent-lock threshold. Counting only eligible verification outcomes and
adding source throttling limits how quickly an attacker can trigger it, while audit and
owner unlock make recovery controlled. Code requests alone cannot lock a victim.

**Alternatives considered**: Counting every request enables trivial lockout; revealing
lock state helps targeted attackers; resetting the rolling count with a correct code
would let an attacker with intermittent mailbox access erase evidence.

## Decision 8: Invitation acceptance verifies email without a password

**Decision**: An owner creates an invitation for one normalized email. The recipient
opens the expiring single-use fragment capability and confirms the same email; successful
acceptance atomically activates one member with verified email and consumes the invite.
The member may receive a session from acceptance, and all later devices use email-code
login.

**Rationale**: Possession of the invitation delivered to the intended mailbox proves
control for activation, while the explicit email comparison prevents accidental use by
another signed-in identity. No member password state is created.

**Alternatives considered**: Public registration violates invitation-only scope;
requiring a password violates member passwordlessness; accepting any authenticated
email permits invitation reassignment.

## Decision 9: Transactional email outbox with replaceable SMTP adapter

**Decision**: Account mutations commit an email-delivery record without plaintext
secret retention beyond the minimum in-memory handoff needed to render/send. An
in-process dispatcher calls a narrow `Sender` interface with bounded retries and records
safe status. SMTP is the first configured adapter; tests use capture/failure senders.
Authenticated product use remains available while delivery is degraded.

**Rationale**: A persisted delivery lifecycle provides owner-visible retry state and
crash recovery without another service. Provider details remain outside domain logic.

**Alternatives considered**: Synchronous-only sending couples database success to
provider latency; a broker violates deployment constraints; a provider-specific SDK
creates avoidable lock-in. Because secrets must not be persisted in delivery rows, a
crash before sending requires issuing a fresh capability/code rather than reconstructing
the old plaintext secret.

## Decision 10: Transactional audit and authorization-scoped SSE

**Decision**: Each client-visible account mutation writes safe audit and client-event
rows in its database transaction. Event scopes are `shared`, `user`, or `owner`; a user
scope requires its subject user ID and an owner scope is visible only to the current
owner. The stream authenticates at connect and rechecks session/account state during
replay and periodically while connected.

**Rationale**: The outbox prevents commit/event races. Explicit scope makes accidental
private broadcast structurally invalid. Compact events identify changed read models;
the client refetches authorized snapshots and tolerates duplicates.

**Alternatives considered**: In-memory notification loses committed changes; polling
violates the constitution; embedding account snapshots in events increases privacy and
staleness risk; granting the owner all user-scoped events violates private-data isolation.

## Decision 11: Email normalization is conservative

**Decision**: Trim surrounding Unicode whitespace, reject control characters, validate
one syntactically plausible address, and lowercase the domain. Preserve the local part
as entered for delivery but use a case-folded normalized comparison key consistently.
Do not perform provider-specific dot/plus rewriting. A verified email change is a later
explicit workflow.

**Rationale**: Provider-specific aliases are not universal. One deterministic key is
needed for invitation/account conflict checks and anti-enumeration buckets.

Primary source: [OWASP Email Validation and Verification Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Email_Validation_and_Verification_Cheat_Sheet.html).

**Alternatives considered**: Case-sensitive identity permits duplicates; aggressive
provider rewriting merges distinct mailboxes; real-time SMTP validation leaks metadata
and is unreliable.
