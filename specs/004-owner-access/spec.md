# Feature Specification: Owner Access and Invitations

**Feature Branch**: `004-owner-access`

**Created**: 2026-08-28

**Status**: planned
<!-- Market Lens spec lifecycle: planned → in-progress → in-review → shipped. -->

**Input**: Establish exactly one first owner for a new Market Lens deployment, secure
authenticated access, owner-managed email invitations for additional members, passwordless
six-digit email-code login for non-owners, roles, owner-controlled member lockout recovery,
session revocation, and backend-enforced isolation for every user's private data and live
events. Public self-registration, social login, enterprise identity providers, billing,
and public multi-tenant SaaS are excluded.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Bootstrap the first owner safely (Priority: P1)

As the person deploying Market Lens, I can create exactly one first owner through a
time-bounded setup flow, so that an unclaimed deployment cannot remain publicly open or
be claimed by a remote stranger.

**Why this priority**: Every protected browser surface, invitation, private record, and
event requires a trusted initial authority.

**Independent Test**: Start a fresh deployment, create the owner through the host-
authorized setup flow, verify setup closes immediately, sign in with the owner's strong
credential, exercise verified-email recovery, and prove the same or another setup
credential cannot create a second owner.

**Acceptance Scenarios**:

1. **Given** no account exists, **When** the host operator requests setup, **Then** a
   single-use time-bounded setup capability is made available without entering logs or
   browser-visible configuration.
2. **Given** a valid setup capability, **When** the first person supplies a valid email,
   strong password, and display name, **Then** exactly one active owner is created and
   the setup capability is permanently consumed.
3. **Given** an owner already exists, **When** any setup URL or host setup request is
   attempted, **Then** no additional owner is created and the application exposes no
   information useful for taking over the account.
4. **Given** the active owner, **When** the correct owner email and strong credential are
   supplied, **Then** an authenticated owner session is established without using the
   member email-code flow.
5. **Given** the owner has lost the credential, **When** verified-email recovery is
   completed with a valid single-use expiring capability, **Then** the owner can set a
   new strong credential and all prior owner sessions and recovery capabilities are
   revoked.

---

### User Story 2 - Sign in with an emailed one-time code (Priority: P1)

As an invited member, I can sign in on any device by entering my email and the six-digit
one-time code sent to it, without creating or remembering a password. I can review
active sessions and sign out individual or all devices.

**Why this priority**: Authentication must remain recoverable and revocable without
weakening access to private financial records.

**Independent Test**: Request a code, sign in with the valid six-digit value, reject
expired/replayed/wrong values, exercise temporary and administrative lockouts, revoke
sessions, and verify revoked sessions cannot access snapshots or resume live events.

**Acceptance Scenarios**:

1. **Given** any submitted email, **When** a login code is requested, **Then** the
   browser receives the same safe response while only an active, verified, unlocked
   member receives a six-digit code by email.
2. **Given** a member's most recently issued unexpired code, **When** it is entered
   correctly once, **Then** the member receives an authenticated session and the code
   cannot be replayed.
3. **Given** three consecutive wrong codes for a member, **When** another code is
   requested or verified during the next 15 minutes, **Then** the attempt is blocked
   without disclosing account state and the member receives no usable session.
4. **Given** ten wrong code submissions for a member within a rolling 24-hour period,
   **When** any later login is attempted, **Then** the account remains locked until the
   owner explicitly unlocks it.
5. **Given** the owner unlocks a member, **When** the member requests and correctly
   enters a newly issued code, **Then** the member can sign in and prior codes remain
   invalid.
6. **Given** a session is revoked or an account is deactivated, **When** that session
   next reads, mutates, or reconnects to live events, **Then** access is denied promptly.

---

### User Story 3 - Invite and manage members by email (Priority: P1)

As the owner, I can invite another person by email, revoke or resend an outstanding
invitation, and deactivate a member, so that access remains deliberate and auditable.

**Why this priority**: The requested multi-user model is invitation-only and depends on
owner control rather than public registration.

**Independent Test**: Send an invitation, accept it once with the intended email, reject
expired/revoked/replayed/wrong-email acceptance, and verify deactivation ends access.

**Acceptance Scenarios**:

1. **Given** an authenticated owner and a valid email, **When** the owner invites it,
   **Then** an expiring single-use invitation is sent and its delivery state is visible
   without exposing its secret.
2. **Given** a valid invitation, **When** the intended recipient accepts it, **Then** one
   member account is activated without creating a password, email ownership is verified,
   and the invitation cannot be reused.
3. **Given** an invitation is expired, revoked, already used, or presented for another
   email, **When** acceptance is attempted, **Then** no account or session is created.
4. **Given** delivery fails, **When** the owner reviews the invitation, **Then** a safe
   failure state and bounded resend action are available without exposing provider
   credentials or raw delivery errors.

---

### User Story 4 - Keep each user's private data isolated (Priority: P1)

As a member, I can trust that my tracking rules, holdings, trades, portfolios, alerts,
devices, exports, and live events are inaccessible to other members and are not visible
to the owner merely because they administer accounts.

**Why this priority**: Financial records are sensitive; multi-user support is unsafe if
administration silently grants access to another user's private activity.

**Independent Test**: Create equivalent private records for two members and prove that
all list/detail/mutation/export/live-event paths return only the authenticated user's
records while shared market reference data remains common.

**Acceptance Scenarios**:

1. **Given** two active members, **When** either requests or guesses the other's private
   record identifier, **Then** no private existence or content is disclosed.
2. **Given** the owner manages a member's status, **When** the owner views account
   administration, **Then** account/audit metadata is visible but private financial
   records are not unless a later explicit sharing feature grants access.
3. **Given** a private record changes, **When** live events are delivered, **Then** only
   authorized sessions for its owner receive the event while shared market-data events
   remain available to every active authenticated user.

### Edge Cases

- Two setup attempts race before the first owner commits.
- The setup, invitation, recovery, or verification capability expires during submission.
- An invited email differs only by case or Unicode representation from an existing user.
- An owner invites themselves, an existing active member, or a deactivated account.
- The only owner attempts to demote/deactivate themselves or transfer ownership.
- Email delivery is unavailable, delayed, duplicated, or reports a later bounce.
- Multiple login-code requests arrive concurrently or email delivers older codes after a
  newer code has invalidated them.
- A correct code arrives while the 15-minute block or administrative lock is active.
- Failed attempts are distributed across devices, network addresses, and application
  restarts to evade counters.
- An attacker deliberately triggers lockout for another person's known email address.
- The owner attempts to unlock themselves or a non-member account.
- An owner's password or any user's email changes while sessions exist on other devices.
- A deactivated user has an open SSE connection or queued private event replay.
- A request mixes identifiers from shared reference data and another user's private data.
- The database is restored from a backup containing consumed or expired capabilities.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: A fresh deployment MUST allow exactly one first owner to be created through
  a host-authorized, single-use, expiring setup capability.
- **FR-002**: Owner creation MUST atomically consume setup and MUST reject concurrent,
  replayed, expired, or post-bootstrap setup attempts.
- **FR-003**: The system MUST support normalized unique verified email identities and
  display names. The owner MUST use a strong owner credential; non-owner members MUST
  authenticate without passwords using emailed one-time codes.
- **FR-003a**: Owner sign-in and verified-email recovery MUST remain separate from the
  member code flow. The system MUST NOT request, accept, or retain a password or password
  credential for a non-owner member.
- **FR-004**: Public self-registration MUST NOT be available; only the first-owner setup
  and owner-authorized invitations may create accounts.
- **FR-005**: The system MUST provide secure authenticated sessions with inactivity and
  absolute expiry, request-forgery protection, renewal rules, and individual/all-device
  revocation.
- **FR-006**: Member code-request and verification responses MUST resist account
  enumeration and apply per-account plus per-origin throttling without blocking all users
  globally or revealing whether an email is registered, inactive, blocked, or locked.
- **FR-007**: Each member login code MUST contain exactly six numeric digits, expire 10
  minutes after issuance, be accepted only once, and be safely represented at rest. A
  newly issued code MUST invalidate every older unused code for that member.
- **FR-007a**: After three consecutive wrong member-code submissions, code issuance and
  verification for that account MUST be blocked for 15 minutes. The temporary block MUST
  survive process restarts and MUST NOT terminate already valid sessions.
- **FR-007b**: After ten wrong member-code submissions within a rolling 24-hour window,
  the member account MUST enter an administrative lock. Time passage and correct codes
  MUST NOT remove that lock; only an authenticated owner unlock action may do so.
- **FR-007c**: Owner unlock MUST be auditable, clear the member's temporary/rolling failure
  state, revoke all outstanding login codes, and require a newly issued code for the next
  sign-in. It MUST NOT reactivate a deactivated account or grant the owner access to the
  member's private financial records.
- **FR-007d**: Code requests MUST be bounded to one delivery per member per 60 seconds
  and no more than five deliveries per rolling hour, with equivalent origin-level abuse
  controls and a safe retry-after indication that does not disclose account existence.
- **FR-008**: The owner MUST be able to invite a normalized email as a member, see safe
  delivery/acceptance state, resend within bounds, and revoke an unused invitation.
- **FR-009**: Invitation acceptance MUST require the intended email, consume exactly one
  active invitation, activate the member without a password, establish verified email
  ownership, and reject expiration, revocation, replay, and conflicting accounts.
- **FR-010**: Initial roles MUST be `owner` and `member`; exactly one active owner MUST
  exist until a separately specified ownership-transfer flow is available.
- **FR-011**: The owner MUST be able to deactivate/reactivate members and revoke their
  sessions without gaining implicit access to their private financial records.
- **FR-012**: Every user-private entity MUST carry explicit ownership and every backend
  read, write, delete, export, aggregate, cache, and search MUST enforce it.
- **FR-013**: Shared reference data MUST be explicitly distinguished from private data
  so access rules cannot rely on missing ownership as an accidental convention.
- **FR-014**: REST responses, errors, audit records, logs, SSE payloads/replay, browser
  state, and delivery-status records MUST contain no owner password, plaintext login
  code, capability secret, session secret, or provider secret. Login email contains only
  the minimum code, expiry, requested-login context, and safety guidance.
- **FR-015**: Live-event delivery MUST authenticate on connection and replay, scope
  private events to the owning user, allow shared events for all active users, and end
  promptly when a session/account is revoked.
- **FR-016**: Security-relevant activity MUST retain a safe audit record covering setup,
  sign-in/code outcomes, temporary blocks, administrative locks/unlocks, owner recovery,
  invitations, email/role/status changes, and session revocation without recording codes,
  secrets, or unnecessary private financial data.
- **FR-017**: Email delivery failures MUST degrade safely, remain retryable and visible
  to the appropriate user, and MUST NOT make existing authenticated research unavailable.
- **FR-018**: All account, invitation, credential, session, audit, ownership, and event-
  scope persistence changes MUST be reproducible through ordered migrations.
- **FR-019**: The feature MUST NOT implement social login, enterprise SSO, public SaaS
  tenancy, billing, user-to-user financial-data sharing, PWA installation, Web Push,
  portfolio/trade tracking, strategy sell suggestions, or notification preference logic.

### Test-First Proof *(mandatory)*

- **Initial failing test**: A migration/service integration test races two valid first-
  owner submissions and asserts that exactly one owner exists, setup is consumed, and
  the loser receives a safe closed-setup result.
- **Expected red reason**: The current foundation has no account/setup persistence or
  authorization service, so the one-owner invariant cannot be satisfied; a compile or
  database setup failure is not valid red evidence.
- **Green evidence**: Account/domain, migration/repository, session/security, email
  and six-digit-code contract, temporary/administrative lockout, owner unlock, HTTP/SSE,
  frontend, responsive Playwright, secret-regression, and cross-user isolation suites
  pass, followed by repository/container/deployment verification.
- **Database migration proof**: Tests prove clean and current-baseline upgrades, exactly
  one active owner, unique normalized email, single-use capability/code constraints,
  durable failure windows/locks, explicit ownership foreign keys, session/invitation
  expiry/revocation, and no manual mutation.

### Responsive UI Behavior *(mandatory for user-facing features; otherwise state N/A)*

- **Mobile (320-767 CSS px)**: Setup, email/code sign-in, owner recovery, invitation acceptance, session
  management, and owner member/invitation lists use a single-column touch-friendly flow.
  At 360x800 every primary action and security state is reachable; at 320px no secret,
  error, form control, dialog, or member action is clipped or causes page overflow.
- **Tablet (768-1023 CSS px)**: At 768x1024 forms remain focused and member/invitation
  summaries may use two columns while state and validation survive orientation changes.
- **Desktop (1024+ CSS px)**: At 1440x900 account administration may use denser tables
  and adjacent detail panels without exposing actions unavailable on smaller screens.
- **Input and accessibility**: All flows are keyboard/touch operable, visibly focused,
  semantically labelled, non-hover dependent, zoom tolerant, theme compatible, and do
  not disclose whether an unrelated email/account exists.

### Live Update Behavior *(mandatory for client-visible data; otherwise state N/A)*

- **Snapshot and events**: REST supplies the authenticated account/session/invitation
  snapshot; versioned SSE announces authorized account, invitation, delivery, session,
  and role/status changes.
- **Reliability**: Reconnection/resumption re-evaluates current authorization before
  replay. Revoked sessions receive no later private event, duplicates are harmless, and
  disconnected/stale/offline state is visible without weakening sign-out.
- **Test evidence**: Automated tests cover replay during role/status changes, duplicate
  events, revocation of an open stream, slow consumers, and two-user isolation.

### Identity, Ownership, and Permissions *(mandatory for user/account data; otherwise state N/A)*

- **Bootstrap and invitations**: Exactly one time-bounded owner setup; verified-email,
  expiring, single-use owner invitations; initial owner/member roles; no public signup.
- **Ownership and authorization**: Shared market reference data is common; all tracking,
  trading, portfolio, alert, device, and future private records are owner-scoped by
  backend services and persistence queries. Account administration does not imply
  access to private financial data.
- **Security evidence**: Tests cover owner recovery, member-code expiry/replay, distributed
  guessing, three-attempt temporary blocking, ten-attempt administrative locking,
  owner-only unlock, session expiry/revocation, request forgery, enumeration resistance,
  concurrent setup/acceptance, and cross-user REST/export/SSE isolation.

### PWA and Notification Behavior *(mandatory when applicable; otherwise state N/A)*

N/A. Invitation, member login-code, verification, and owner-recovery email are
transactional account messages, not user-configurable market notifications. PWA/Web
Push require separate specifications.

### Key Entities *(include if feature involves data)*

- **User**: Stable person identity with normalized verified email, display name, role,
  lifecycle status, credential state, and audit timestamps.
- **Bootstrap Capability**: Single-use expiring host-authorized authority to create the
  first owner, permanently closed after success.
- **Invitation**: Owner-issued intended email, role, lifecycle/delivery state, expiry,
  safe retry lineage, and accepting user.
- **Owner Credential/Recovery Capability**: Safely represented owner authentication and
  recovery state with rotation, expiry, use, and revocation context.
- **Member Login Challenge**: One member's safely represented six-digit code challenge,
  issue/expiry/use state, delivery context, and invalidation lineage; plaintext codes are
  never retained after delivery preparation.
- **Login Failure State**: Per-member consecutive failures, rolling 24-hour failure
  history, 15-minute temporary block, administrative lock, and audited owner unlock.
- **Session**: One authenticated device/browser session with creation, activity,
  absolute/inactivity expiry, revocation, and safe device metadata.
- **Security Audit Event**: Immutable safe record of security-relevant actions/outcomes.
- **Owned Resource Boundary**: Required user ownership and authorization contract applied
  to every current or future private financial entity and event.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In 100 concurrent setup races, every fresh deployment creates exactly one
  owner and accepts zero later setup attempts.
- **SC-002**: A legitimate first owner can complete setup in under 3 minutes and an
  invited member can accept an invitation and sign in in under 5 minutes.
- **SC-003**: 100% of expired, revoked, replayed, wrong-email, and already-consumed setup,
  invitation, login-code, verification, and recovery attempts create no account or session.
- **SC-004**: A cross-user matrix covering every private list/detail/mutation/export/SSE
  path produces zero private existence, content, count, or event disclosures.
- **SC-005**: Revoking a session or deactivating a user prevents new reads and live-event
  replay within 5 seconds while leaving other active users connected.
- **SC-006**: Account email delivery failures expose a safe actionable state within 10
  seconds and existing authenticated research remains usable.
- **SC-007**: Setup, sign-in, recovery, invitation acceptance, and owner administration
  pass at 360x800, 768x1024, and 1440x900 and remain usable without page overflow at
  320 CSS pixels in system, light, and dark themes.
- **SC-008**: Secret-regression tests find zero credential/capability/session/provider
  secrets or plaintext member codes in logs, URLs, REST/SSE payloads, audit records,
  browser state, persistence inspection, or email delivery status.
- **SC-009**: Every three consecutive wrong member codes causes a 15-minute block, every
  ten wrong codes within 24 hours causes an owner-only administrative lock, and 100% of
  attempts to bypass either threshold across devices/restarts fail.

## Assumptions

- The first owner uses a strong password plus verified-email recovery. Every non-owner
  member uses only six-digit emailed one-time codes and is never asked to create a
  password. Social and enterprise identity methods require later specifications.
- Initial roles are owner and member. The owner manages access metadata but cannot inspect
  another user's private financial activity unless a later sharing feature grants it.
- Transactional account email uses one replaceable configured delivery adapter; exact
  provider selection and self-hosted delivery configuration are planning decisions.
- Temporary member blocks last 15 minutes; administrative lockout counts ten wrong code
  submissions in a rolling 24-hour window. These explicit defaults implement the user's
  requested three-attempt and ten-attempt thresholds and may be amended only through a
  reviewed specification change.
- A host operator can obtain the first setup capability through a server-side command or
  equivalent local administrative channel that does not print it to ordinary logs.
- The product remains one self-hosted Market Lens instance, not a public SaaS tenant
  platform; invited users collaborate on shared reference data while private records stay
  isolated.
- Ownership transfer, owner deletion, additional owners, group sharing, data delegation,
  legal retention/export/deletion policy, PWA, Web Push, personal trade tracking, and
  strategy-driven notifications require separate reviewed specifications.
