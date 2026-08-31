# Research: Owner Access and Invitations

## Decision 1: Generic email-first authentication with an explicit owner fallback

**Decision**: Every sign-in begins with one email field. Submission always returns the
same accepted response and advances the client to the same six-digit-code screen. The
server sends a code only to an eligible active member. The screen also offers a
secondary `Use owner password` action; choosing it reveals a password field, and the
email/password endpoint returns only a generic failure. The server and client never
confirm whether the submitted email belongs to the owner, a member, or no account.

**Rationale**: The same visible progression avoids an owner-discovery oracle while
preserving the owner's requested password credential and members' passwordless access.

**Alternatives considered**: Automatically selecting password for the owner leaks the
owner identity through UI behavior; separate owner/member login pages encourage probing;
passwords for members violate the requirement.

## Decision 2: Argon2id for the owner credential

**Decision**: Encode the owner's password with Argon2id using a unique 128-bit salt and
self-describing parameters. Begin with OWASP's 19 MiB, two-iteration, one-lane minimum,
benchmark on the production container, and allow parameter increases without schema
change. Enforce at least 12 characters and permit at least 64 Unicode characters.

**Rationale**: Argon2id is memory-hard and self-describing hashes allow rehash-on-login.
Use the reviewed `golang.org/x/crypto/argon2` primitive.

Primary sources: [OWASP Password Storage Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html)
and [RFC 9106](https://datatracker.ietf.org/doc/html/rfc9106).

**Alternatives considered**: Fast hashes are unsuitable; bcrypt has legacy input-length
constraints; reversible password encryption exposes plaintext after key compromise.

## Decision 3: Opaque, server-side sessions

**Decision**: Generate at least 256 random bits per session token, send it only in a
`Secure`, `HttpOnly`, `SameSite=Lax`, host-only cookie, and store only a server-keyed
digest. Enforce eight hours idle and 30 days absolute lifetime. Rotate on successful
authentication, revoke individually or globally, and require a separate same-origin
CSRF token/header for state-changing requests.

**Rationale**: Opaque sessions permit immediate database-backed revocation, per-device
management, role/status re-evaluation, and SSE termination.

Primary source: [OWASP Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html).

**Alternatives considered**: Browser storage exposes bearer tokens to script;
self-contained JWTs complicate immediate revocation; IP binding breaks normal roaming.

## Decision 4: Random capabilities and purpose-separated digests

**Decision**: Bootstrap and invitation links contain independent 256-bit random URL-safe
capabilities, are single-use and expiring, and persist only a keyed HMAC-SHA-256 digest
plus non-secret metadata. Session, capability, member-code, email, and origin digests use
purpose-separated keys derived from the required `AUTH_SECRET`. No owner-recovery
capability is issued or accepted.

**Rationale**: High entropy resists guessing, keyed digests prevent database-only replay,
and purpose separation prevents cross-flow substitution. Capability fragments keep the
secret out of routine URL request logs until the SPA exchanges it in a JSON body.

**Alternatives considered**: Plaintext storage makes database access sufficient for
takeover; query-string secrets leak; signed self-contained tokens complicate one-use
enforcement.

## Decision 5: AES-256-GCM envelopes for external credentials

**Decision**: Encrypt a versioned canonical JSON secret payload for each credential kind
with AES-256-GCM. Use Go 1.26 `cipher.NewGCMWithRandomNonce`, which generates a random
96-bit nonce and prepends it to the sealed value. Bind the row UUID, credential kind,
payload schema version, and deployment key version as additional authenticated data.
Store only ciphertext, versions, and explicitly safe metadata. The 32-byte encryption
key is base64-encoded in `EXTERNAL_CREDENTIAL_KEY`, is distinct from `AUTH_SECRET`, and
never enters PostgreSQL, images, build arguments, APIs, logs, audit, or events.

**Rationale**: GCM supplies confidentiality and integrity; random nonce generation by
the standard-library construction removes application nonce bookkeeping. Row-bound AAD
detects ciphertext substitution. Go documents a 28-byte overhead and a per-key ceiling
of 2^32 messages, far beyond this feature's two long-lived credential rows.

Primary sources: [Go crypto/cipher documentation](https://pkg.go.dev/crypto/cipher) and
[NIST SP 800-38D](https://csrc.nist.gov/pubs/sp/800/38/d/final).

**Alternatives considered**: Plaintext database values violate the requirement;
reusing `AUTH_SECRET` collapses compromise boundaries; CBC without authentication allows
undetected modification; application-generated nonces add avoidable misuse risk.

## Decision 6: Explicit credential-key rotation with versioned envelopes

**Decision**: Each row records a positive `key_version`; configuration records the
matching `EXTERNAL_CREDENTIAL_KEY_VERSION` (initially `1`). Rotation is a deliberate
interactive operational command that receives the old key through the existing process
environment and the new key/version through no-echo TTY prompts, decrypts and re-encrypts
all credential rows in one transaction, and records a safe audit event. Deployment is
then updated to the new key/version. Normal startup refuses a configured version that
does not match persisted credentials; it never silently rewrites rows.

**Rationale**: Version metadata makes rotation auditable and prevents ambiguous key
selection, while an explicit transaction avoids partially rotated state. Rotation is
rare and does not justify a persistent keyring or new service.

**Alternatives considered**: An unversioned key makes recovery/rotation ambiguous; a
database keyring defeats separation; automatic rotation at startup risks partial writes
and accidental lockout.

## Decision 7: EODHD validation precedes the setup transaction

**Decision**: Before owner creation, make a bounded, cancellable request through the
existing EODHD adapter using the submitted key. Validate the token through the official
User API, then require a non-empty ten-year-old EOD response for a non-US instrument;
this entitlement probe is an inference because the User API exposes subscription type
and limits but not the commercial plan name. Do not persist the key or
consume the setup capability before validation succeeds. After validation, open one
transaction, re-lock/recheck the capability and bootstrap state, then atomically write
the owner, password hash, encrypted EODHD/SMTP credentials, closed bootstrap state,
session, audit, and scoped events. A race loser or expired capability commits nothing.

**Rationale**: External I/O outside a database transaction avoids holding locks, while
the final locked recheck preserves one-owner atomicity. Provider failure remains safely
retryable until capability expiry.

**Alternatives considered**: Persist-then-validate can close setup with unusable data;
validating inside the transaction holds locks across network latency; asynchronous
validation violates the required setup guarantee.

Primary sources: [EODHD User API](https://eodhd.com/financial-apis/user-api) and
[EODHD API quick start](https://eodhd.com/financial-apis/quick-start-with-our-financial-data-apis).

## Decision 8: Interactive deployment-only owner password reset

**Decision**: `market-lens auth owner-password reset` requires a real terminal, reads
the new password twice without echo via `golang.org/x/term.ReadPassword`, rejects
mismatch/weak input, and accepts no password flag, environment variable, pipe, public
route, or email capability. One transaction replaces the Argon2id hash, revokes all
owner sessions, writes safe audit and owner-scoped event rows, and clears cookies only
when clients next fail authentication. CLI output contains no email, hash, or secret.

**Rationale**: Shell access is the deployment's proof of authority and keeps recovery
outside remotely probeable surfaces. Transactional revocation prevents an old session
from surviving a credential reset.

Primary source: [`golang.org/x/term` documentation](https://pkg.go.dev/golang.org/x/term).

**Alternatives considered**: Public/email recovery contradicts the chosen ownership
model; password arguments and environment variables leak through process or deployment
metadata; accepting stdin permits accidental automation and secret capture.

## Decision 9: Six-digit member codes with latest-only issuance

**Decision**: Generate codes uniformly with rejection sampling from `crypto/rand` over
`000000`–`999999`, preserve leading zeros, expire after ten minutes, allow one use, and
invalidate every earlier unused challenge when a newer one commits. Store only a
purpose-keyed HMAC digest and compare in constant time.

**Rationale**: The small online search space requires strict online controls. A keyed
digest prevents offline enumeration after database-only disclosure.

**Alternatives considered**: Alphanumeric codes violate the UX; simultaneous codes
expand attack surface; unkeyed hashes are enumerable offline.

## Decision 10: Independent account and origin abuse controls

**Decision**: Three consecutive eligible wrong codes block the member for 15 minutes;
ten eligible wrong submissions in a rolling 24 hours create an owner-clearable lock.
Independently limit origin requests/verifications and member delivery to one per minute
and five per hour. Unknown/inactive/blocked/locked/owner emails follow the same public
response and roughly equivalent work path, but receive no code.

**Rationale**: Account counters stop distributed guessing and origin counters stop one
source spraying accounts. Counting only an eligible active challenge avoids trivial
victim lockout from requests alone.

Primary sources: [OWASP Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)
and [OWASP Bot Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Bot_Management_and_Anti-Automation_Cheat_Sheet.html).

**Alternatives considered**: IP-only or account-only limiting has direct bypasses;
CAPTCHA adds an unapproved integration and does not replace throttling.

## Decision 11: Invitation acceptance verifies email without a password

**Decision**: The owner invites one normalized email. The recipient opens an expiring
single-use fragment capability and confirms that same email; acceptance atomically
activates one verified member and consumes the invitation. The member may receive an
initial session and later uses email codes only.

**Rationale**: Mailbox possession plus exact normalized-email matching proves intended
activation without creating member password state.

**Alternatives considered**: Public registration violates invitation-only scope;
passwords violate member passwordlessness; accepting another identity permits transfer.

## Decision 12: Setup-stored SMTP with a transactional delivery outbox

**Decision**: Setup collects host, port, sender, optional username, and optional
password. Encrypt the whole versioned SMTP configuration envelope because usernames,
infrastructure endpoints, and sender details can all be sensitive. Expose only
`configured`, `ready`, and sanitized delivery lifecycle values. An in-process dispatcher
decrypts just in time, calls a narrow SMTP `Sender` interface with bounded retries, and
discards plaintext after use. Tests use capture/failure senders.

**Rationale**: Persisted safe delivery state gives crash recovery without another
service; one encrypted envelope prevents accidental partial disclosure through columns
or read models. Existing authenticated use remains available during provider outage.

**Alternatives considered**: Environment-only SMTP contradicts setup collection;
synchronous-only delivery couples commits to provider latency; a broker violates the
deployment model; plaintext metadata creates unnecessary disclosure.

## Decision 13: Transactional audit and authorization-scoped SSE

**Decision**: Every visible account mutation writes safe audit and client-event rows in
its transaction. Event scopes are `shared`, `user`, or `owner`; the stream authenticates
at connect and rechecks session/account state during replay and periodically while open.
Events contain only invalidation metadata. Credential creation/rotation/reset events
reveal status, kind, and version at most—never configuration or provider response data.

**Rationale**: Transactional outbox rows prevent commit/event races. Explicit scope
makes accidental private broadcast structurally invalid.

**Alternatives considered**: In-memory signals lose changes; polling violates the
constitution; embedded snapshots increase privacy and staleness risk.

## Decision 14: Conservative email normalization

**Decision**: Trim surrounding Unicode whitespace, reject control characters, validate
one plausible address, lowercase the domain, preserve the local part for delivery, and
use one case-folded comparison key. Do not rewrite provider-specific dots or plus tags.

**Rationale**: Provider-specific alias rules are not universal; deterministic comparison
is required for account conflicts and anti-enumeration buckets.

Primary source: [OWASP Email Validation and Verification Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Email_Validation_and_Verification_Cheat_Sheet.html).

**Alternatives considered**: Case-sensitive identity permits duplicates; aggressive
rewriting merges distinct mailboxes; real-time SMTP validation is unreliable and leaks.
