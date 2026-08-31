# Feature Specification: Actionable Owner Setup Errors

**Feature Branch**: `010-setup-error-clarity`

**Created**: 2026-08-31

**Status**: shipped
<!-- Market Lens spec lifecycle: planned → in-progress → in-review → shipped. -->

**Input**: Creating the owner account reports "The request is invalid." whatever went wrong. A
password that is too short, a malformed email, a bad EODHD key, and a wrong SMTP setting are
indistinguishable. The person setting up the installation cannot tell which field to fix.

## Why this exists

`completeOwnerSetupHandler` maps every failure it does not specifically recognise to one
string, and `validateSetupCredentials` collapses eight distinct SMTP problems into
`SMTP configuration is invalid`. The client renders whatever arrives as a single
`<p role="alert">` above the form, attached to no field.

The result is a setup screen that says something is wrong with ten inputs and will not say
which. This was hit in practice with a short password: the form reported only that the
request was invalid.

There is a second, worse gap. **SMTP is never contacted during setup.** It is shape-checked
and then stored encrypted. A wrong host, port, username, or password is therefore accepted at
setup and only discovered later, when the first invitation or member login code silently
fails to deliver — and members cannot sign in any other way.

## Scope decision: SMTP is verified, and any failure blocks setup

Setup performs a real SMTP connection: dial, STARTTLS, authenticate when credentials are
given, and confirm the sender address is accepted. **Any** failure — credentials rejected,
host unreachable, TLS refused, sender refused — refuses setup with a message naming the cause.

**This is a deliberate, operator-chosen trade-off.** It means an installation whose mail
server is temporarily down or firewalled cannot be completed until mail works. The stricter
guarantee was chosen over installability: a Market Lens installation whose mail does not work
cannot invite anybody or issue a login code, so an owner-only installation created against
broken mail is a dead end that looks healthy.

**Constitution VIII compatibility.** Principle VIII requires "a deployment mode in which the
core application remains usable when that integration is unavailable". That requirement is
about *runtime*, and it still holds unchanged: a running installation whose mail server later
fails keeps serving, and `storedSMTPSender` already returns a retryable delivery error rather
than bringing the process down. This gate applies only to the one-time bootstrap, where the
operator is present at a terminal and able to fix the configuration immediately. No runtime
path gains a hard dependency on SMTP.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Be told which field is wrong (Priority: P1)

Someone creating the owner account submits the form with one or more bad values. The response
names every field that is wrong and what to do about each, and the form shows each message
against the input it belongs to.

**Why this priority**: It is the reported defect, and without it none of the rest is reachable.

**Independent Test**: Submit setup with an eleven-character password and confirm the response
names the password field and states the minimum length, and that the form marks that input.

**Acceptance Scenarios**:

1. **Given** a password shorter than 12 characters, **When** setup is submitted, **Then** the
   response names `password`, states the 12-character minimum, and names no other field.
2. **Given** several bad values at once, **When** setup is submitted, **Then** every bad field
   is reported in one response, so the operator fixes them in one pass rather than discovering
   them one round trip at a time.
3. **Given** any rejection, **When** the response is rendered, **Then** each message appears
   against its own input, the input is marked invalid for assistive technology, and a summary
   remains at the top of the form.
4. **Given** any rejection, **When** the response is inspected, **Then** it contains no
   submitted password, EODHD key, or SMTP password in any form.

---

### User Story 2 - Be told the provider credential is the problem (Priority: P1)

The EODHD key is reported separately from everything else, and a key the provider rejected is
distinguished from a provider that could not be reached.

**Why this priority**: These two failures need opposite responses — retype the key, or wait —
and today they are the same message.

**Independent Test**: Submit setup with a key the provider rejects and confirm the message
names the EODHD field and says the provider rejected it; repeat with the provider unreachable
and confirm the message says so instead.

**Acceptance Scenarios**:

1. **Given** a key the provider rejects, **When** setup is submitted, **Then** the response
   names `eodhd_api_key` and says the provider rejected it.
2. **Given** a provider that cannot be reached, **When** setup is submitted, **Then** the
   response says the provider could not be reached and that the key was not checked, and is
   distinguishable from rejection by both status and code.
3. **Given** either outcome, **When** the response is inspected, **Then** the submitted key
   does not appear in it.

---

### User Story 3 - Be told the mail configuration is wrong, at setup (Priority: P1)

Setup contacts the SMTP server. A wrong host, port, credential, or sender is reported then,
naming which, instead of surfacing later as invitations that never arrive.

**Why this priority**: Mail is the only way a member ever signs in. Setup completing against
broken mail produces an installation that cannot grow past its owner.

**Independent Test**: Submit setup with a valid shape but credentials the server rejects, and
confirm setup refuses and names the SMTP credentials.

**Acceptance Scenarios**:

1. **Given** SMTP credentials the server rejects, **When** setup is submitted, **Then** setup
   refuses, names `smtp_username`/`smtp_password`, and reports that the server rejected them.
2. **Given** an SMTP host or port nothing is listening on, **When** setup is submitted,
   **Then** setup refuses and reports that the server could not be reached, naming the host
   field and the timeout.
3. **Given** a server that refuses the sender address, **When** setup is submitted, **Then**
   setup refuses and names `smtp_from`.
4. **Given** a successful verification, **When** setup completes, **Then** no message was
   sent to anybody — verification ends at `MAIL FROM` and resets.
5. **Given** any SMTP failure, **When** the response is inspected, **Then** it contains
   neither the SMTP password nor any server banner text that might echo it.

---

### Edge Cases

- Both the EODHD key and the SMTP configuration are wrong: both are reported in one response.
- Local shape errors exist *and* the network values are wrong: only the shape errors are
  reported, and no network call is made, so a typo never burns a provider rate limit.
- The setup capability is expired or already used: that is reported on its own, because no
  field the operator can retype will fix it.
- SMTP verification hangs: it is bounded by a timeout and reported as unreachable.
- A server returns a failure whose text contains the submitted password: the text is not
  passed through; only a classified reason is reported.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Every setup rejection caused by a submitted value MUST identify the field and
  state what to change.
- **FR-002**: All field problems detectable in one pass MUST be reported together.
- **FR-003**: Local shape validation MUST complete before any network call, and MUST suppress
  those calls when it fails.
- **FR-004**: The password rule (12–1024 characters) MUST be stated in the message rather than
  implied.
- **FR-005**: A provider credential the provider rejected MUST be distinguishable from a
  provider that could not be reached, by both HTTP status and machine-readable code.
- **FR-006**: Setup MUST verify SMTP by connecting, negotiating TLS, authenticating when
  credentials are supplied, and confirming the sender is accepted.
- **FR-007**: SMTP verification MUST NOT deliver a message to anybody.
- **FR-008**: Any SMTP verification failure MUST refuse setup and MUST report a classified
  reason: credentials rejected, unreachable, TLS refused, or sender refused.
- **FR-009**: No error response, log line, or audit entry may contain a submitted password,
  EODHD key, or SMTP password, nor raw server response text that could echo one.
- **FR-010**: A refused setup MUST leave no owner, no credential row, and the capability
  usable for a corrected retry within its lifetime.
- **FR-011**: The client MUST render each message against its input, mark that input invalid
  for assistive technology, and keep a summary that moves focus on failure.

### Test-First Proof *(mandatory)*

- **Initial failing test**: an API test asserting that setup with an eleven-character password
  returns a body naming the `password` field. It must fail because the handler currently
  returns `{"error":{"code":"invalid_request","message":"The request is invalid."}}` with no
  field information — a behavioral assertion on the response body.
- **Expected red reason**: the response contains no `fields` array.
- **Green evidence**: the identity, api, and mail suites, plus a Vitest assertion that the
  form renders a field message, plus a Playwright journey that submits a bad setup and reads
  the message at mobile, tablet, and desktop viewports.
- **Database migration proof**: N/A. No schema change; this feature alters only what is
  reported and one added network check.

### Responsive UI Behavior *(mandatory for user-facing features; otherwise state N/A)*

The setup form gains per-field messages. They must be readable and not cause horizontal
overflow at 360x800, 768x1024, 1440x900, or 320 CSS pixels. Messages sit directly beneath
their input, are announced through `aria-describedby`, and the input carries `aria-invalid`.
The existing summary alert stays and receives focus on failure so a keyboard or screen-reader
user is told immediately. No interaction depends on hover.

### Live Update Behavior *(mandatory for client-visible data; otherwise state N/A)*

N/A. A refused setup commits nothing, so there is no domain change to publish.

### Identity, Ownership, and Permissions *(mandatory for user/account data; otherwise state N/A)*

- **Bootstrap and invitations**: unchanged in who may create an account. Setup becomes harder
  to complete incorrectly, not easier to complete.
- **Ownership and authorization**: the endpoint stays public and capability-gated. Richer
  errors must not become an oracle: the messages describe only values the caller just
  submitted, never anything about installation state.
- **Security evidence**: tests proving no submitted secret appears in any response or log;
  that a refused setup leaves no owner and no credential row; and that a rejected capability
  is reported without revealing whether an owner already exists.

### PWA and Notification Behavior *(mandatory when applicable; otherwise state N/A)*

N/A.

### Key Entities *(include if feature involves data)*

- **Setup field error**: a field name, a machine-readable code, and an operator-facing
  message. Transport-only; never persisted.

## Success Criteria *(mandatory)*

- **SC-001**: Every one of the ten setup inputs, when submitted invalid alone, produces a
  response naming that field.
- **SC-002**: A setup submitted with five bad fields reports all five in one response.
- **SC-003**: An EODHD key rejected by the provider and a provider that cannot be reached
  produce different HTTP statuses and different codes.
- **SC-004**: Setup against an SMTP server that rejects the credentials refuses, and the
  refusal names the credential fields.
- **SC-005**: Setup against an unreachable SMTP host refuses within the verification timeout.
- **SC-006**: No submitted password, EODHD key, or SMTP password appears in any response body,
  log line, or audit row, verified by scanning a complete failing setup.
- **SC-007**: A refused setup can be corrected and retried with the same capability.
- **SC-008**: The setup form shows every returned message against its own input at 360x800,
  768x1024, 1440x900, and 320 pixels wide.

## Assumptions

- The SMTP verification gate applies only to bootstrap. Runtime delivery keeps its existing
  degraded behavior, so the constitution's integration-availability requirement is unaffected.
- Verification ending at `MAIL FROM` followed by `RSET` is enough to prove the sender is
  accepted without delivering anything. Servers that only reject a sender at `RCPT TO` will
  not be caught, which is accepted rather than sending mail to a real recipient during setup.
- Reporting which submitted field is wrong is not an information leak: the caller supplied
  every one of those values in the same request.
- The existing `mail.SMTPConfig` dial and STARTTLS path is reused, so verification and
  delivery cannot drift apart in how they reach a server.

## Implementation notes

Kept in this file rather than a separate plan and tasks pair, because the change is one
behavior across three known files plus a new verifier, with no schema change and no new
contract surface beyond an added `fields` array on an existing error envelope.

- `identity`: a `SetupValidationError` carrying `[]SetupFieldError`; validation collects
  rather than returns on first failure; a `SMTPVerifier` dependency injected like the existing
  `EODHDCredentialValidator` so tests stub it.
- `mail`: `VerifySMTP` reusing the existing dial/STARTTLS/auth path, ending at `MAIL FROM` and
  `RSET`, returning a classified `VerificationError`.
- `api`: `completeOwnerSetupHandler` renders `fields` on the existing error envelope; 400 for
  values the operator typed, 503 for a dependency that could not be reached, both blocking.
- `src/components/account/OwnerAuth.vue`: a `fieldErrors` prop rendered per input with
  `aria-invalid` and `aria-describedby`.

## Implementation evidence (2026-08-31)

**Initial red.** `go test ./internal/api -run TestOwnerSetupNamesEveryField` failed with
`{"error":{"code":"invalid_request","message":"The request is invalid."}}` for all six cases —
the reported defect, reproduced as an assertion.

**Second red.** `go test ./internal/identity -run TestBootstrapReportsEveryBadFieldAtOnce`
showed the collapse directly: `SMTP configuration is invalid` for a bad port, a blank host, a
malformed sender, and a half-supplied credential pair alike. It also caught that the display
name was never validated at all — the blank-name case fell through to an unrelated error.

**Third red.** `go test ./internal/mail -run TestVerifySMTP` failed with
`SMTP verification is unimplemented` across every classified outcome.

**Client red.** `OwnerAuthFields.test.ts` failed because the form rendered one alert and no
`aria-invalid` on any input.

**Green.** Full Go suite, 90 Vitest tests, 108 Playwright journeys, `make verify`.

Verified against the running development server:

```
POST /api/v1/auth/owner/setup   → 400 invalid_setup, six fields each named:
  email invalid_format · display_name invalid_format · password too_short
  smtp_port out_of_range · smtp_from invalid_format · smtp_password required
```

**Two existing tests were updated rather than weakened.**
`TestBootstrapCredentialsRejectProviderFailureWithoutConsumingSetup` asserted the two removed
sentinels; it now asserts the field, the code, and the unreachable flag, which is strictly
more specific, and keeps every other assertion. The command tests gained a stubbed verifier,
because a test that is not about mail must not depend on a reachable mail server.

**Not verified end to end in the browser.** Owner setup had already been completed on the
development database by the time the work was finished, and setup closes permanently. The
form rendering is covered by component tests and the wire contract by the API test above;
exercising the real screen again needs a fresh database.
