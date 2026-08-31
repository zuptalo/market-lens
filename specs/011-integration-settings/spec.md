# Feature Specification: Owner Integration Settings

**Feature Branch**: `011-integration-settings`

**Created**: 2026-08-31

**Status**: in-review
<!-- Market Lens spec lifecycle: planned → in-progress → in-review → shipped. -->

**Input**: The owner should be able to see and change the EODHD and SMTP configuration after
setup, and have it checked before it is saved.

## Why this exists

Provider credentials are write-once. `Insert` runs during bootstrap and nothing else ever
writes a value: the only other statement touching `external_service_credentials` is the
`UPDATE` inside `Rotate`, which re-encrypts the *same* plaintext under a new key. There is no
endpoint, no host command, and no screen that changes an EODHD key or an SMTP setting.

So an installation whose EODHD key expires, or whose mail password is changed by the mail
provider, has no supported recovery. Market data stops importing, or every invitation and
login code stops arriving, and the only route back is a new database — which means losing
every account, session, and imported price bar.

The read side is nearly as bare. `GET /api/v1/owner/integrations` returns whether each
integration is configured and its key version, and nothing else. Even the SMTP host is not
readable, so an owner cannot see what their installation is currently pointed at. No client
code calls the endpoint at all.

Feature 010 built the two things this needs — a live SMTP verifier and an EODHD validator with
field-level errors — but wired them only into bootstrap.

## What "verified before saved" means here

Nothing is stored until it has been proven to work, using the same checks bootstrap runs. A
submission that cannot be verified is refused and nothing changes. This matches the rule
already chosen for setup: an installation whose mail does not work cannot invite anybody, and
one whose market-data key is dead cannot import, so storing either unchecked only moves the
failure somewhere harder to see.

The owner can also check without saving, so a change can be tried against a running
installation before committing to it.

## Scope decisions

**Partial updates are allowed.** Each integration is updated independently. Requiring the
EODHD key to be re-entered in order to change an SMTP port would be absurd, because the key
cannot be read back to begin with.

**Secrets are never returned.** The EODHD key and the SMTP password are write-only: the API
reports that they are set and when they were last validated, never their values. Host, port,
sender, and username are configuration rather than secrets and are returned, because an owner
cannot edit what they cannot see.

**An omitted SMTP password means "keep the current one".** An owner changing a port must not
have to retype a password they cannot read. An explicitly empty password means "remove
authentication", which is a different intent and is honoured as such.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - See what this installation is configured to use (Priority: P1)

The owner opens account settings and sees the current EODHD and SMTP configuration: which
integrations are set, when the provider key was last validated, and the mail host, port,
sender, and username.

**Why this priority**: Nothing can be corrected before it can be seen, and today none of it is
visible anywhere.

**Independent Test**: Sign in as the owner, open account settings, and confirm the mail host
and port that setup stored are displayed and that no secret appears anywhere in the response.

**Acceptance Scenarios**:

1. **Given** a configured installation, **When** the owner opens settings, **Then** the SMTP
   host, port, sender, and username are shown, along with whether a password is set.
2. **Given** a configured installation, **When** the settings are read, **Then** the EODHD key
   is reported as configured with its last validation time, and its value is absent.
3. **Given** any read, **When** the response is inspected, **Then** it contains no EODHD key
   and no SMTP password in any form.
4. **Given** a member session or an anonymous request, **When** the settings are requested,
   **Then** they are refused and no part of the configuration is disclosed.

---

### User Story 2 - Check a change before committing to it (Priority: P1)

The owner edits the configuration and asks for it to be checked. The result reports whether
each integration works, naming any field that is wrong, and changes nothing either way.

**Why this priority**: It is the "verify before save" the feature is named for, and it is what
makes changing a live installation's mail settings safe to attempt.

**Independent Test**: Submit a check with a deliberately wrong SMTP password and confirm the
response names the credential fields and that the stored configuration is unchanged.

**Acceptance Scenarios**:

1. **Given** a working configuration, **When** the owner checks it, **Then** the response
   reports success and nothing is written.
2. **Given** a wrong SMTP password, **When** the owner checks it, **Then** the response names
   the credential fields and the stored configuration is unchanged.
3. **Given** an EODHD key the provider rejects, **When** the owner checks it, **Then** the
   response names the provider field and distinguishes rejection from unreachability.
4. **Given** a check of any outcome, **When** the stored rows are inspected, **Then** their
   ciphertext and `updated_at` are untouched.

---

### User Story 3 - Save a change, only if it works (Priority: P1)

The owner saves the configuration. It is verified first; on success the new values are stored
encrypted and the change is recorded. On any failure nothing is written and the reason names
the field.

**Why this priority**: It is the recovery path that does not exist today.

**Independent Test**: Save a corrected EODHD key and confirm market-data status reports the new
validation time; save a broken one and confirm the stored key is unchanged.

**Acceptance Scenarios**:

1. **Given** a verified change, **When** it is saved, **Then** the new values are stored
   encrypted under the current key version and the previous values are gone.
2. **Given** a change that fails verification, **When** it is saved, **Then** nothing is
   written and the response names the field, exactly as a check would have.
3. **Given** an SMTP change that omits the password, **When** it is saved, **Then** the stored
   password is retained and used for the verification.
4. **Given** an SMTP change with an explicitly empty password and username, **When** it is
   saved, **Then** authentication is removed and the change verifies without credentials.
5. **Given** a saved change, **When** the audit trail is read, **Then** it records which
   integration changed, by whom, and no value.
6. **Given** a saved change, **When** a connected owner client is listening, **Then** it
   receives a versioned event and no secret in the payload.
7. **Given** a save that fails part way, **When** the rows are inspected, **Then** both
   integrations are on their previous values, never one of each.

---

### Edge Cases

- Both integrations are submitted and only one verifies: nothing is saved, and the response
  names only the failing one.
- The credential key is missing: reading settings still works, but any change is refused
  naming `EXTERNAL_CREDENTIAL_KEY`, because a value that cannot be encrypted cannot be stored.
- A save arrives while a credential-key rotation is in progress: the two must not interleave
  into a mixed key version.
- An owner submits no changed integration at all: refused as an empty request rather than
  silently succeeding.
- The submitted SMTP password is unchanged but the host moved: verification runs against the
  new host with the retained password.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The owner MUST be able to read the current non-secret configuration of every
  integration, including SMTP host, port, sender, and username.
- **FR-002**: The EODHD key and SMTP password MUST never be returned by any endpoint.
- **FR-003**: The owner MUST be able to verify a submitted configuration without storing it.
- **FR-004**: A configuration MUST be verified before it is stored, using the same checks
  bootstrap runs.
- **FR-005**: A submission that fails verification MUST store nothing and MUST report the
  failure using the field-error contract feature 010 established.
- **FR-006**: Integrations MUST be updatable independently.
- **FR-007**: An omitted SMTP password MUST retain the stored one; an explicitly empty
  username and password MUST remove authentication.
- **FR-008**: A save MUST be atomic across every integration it changes.
- **FR-009**: A save MUST record an audit entry naming the integration and the actor, with no
  value, and MUST publish a versioned owner-scoped event carrying no secret.
- **FR-010**: Reading, verifying, and saving MUST each require an owner, enforced in the
  service and not only at the route.
- **FR-011**: A submission that changes no integration MUST be refused.

### Test-First Proof *(mandatory)*

- **Initial failing test**: an API test asserting `PUT /api/v1/owner/integrations` stores a new
  SMTP host. It must fail because the route does not exist and the router answers 405/404 — a
  behavioral assertion on the response status.
- **Expected red reason**: no handler is registered for the method and path.
- **Green evidence**: the identity, credentials, and api suites; Vitest for the settings
  section; a Playwright journey at all three viewports.
- **Database migration proof**: N/A. `external_service_credentials` already carries everything
  needed; this feature adds writes to an existing table, not columns.

### Responsive UI Behavior *(mandatory for user-facing features; otherwise state N/A)*

An integrations section is added to the existing account settings screen. It must be usable at
360x800, 768x1024, 1440x900, and tolerate 320 CSS pixels without horizontal page scrolling or
clipped controls. Field errors render against their inputs exactly as feature 010's setup form
does. The check and save actions are separate, both reachable by keyboard, neither dependent on
hover, and both report progress and outcome to assistive technology.

### Live Update Behavior *(mandatory for client-visible data; otherwise state N/A)*

A saved change publishes `integration.updated.v1`, scope `owner`, version 1, in the same
transaction as the write, carrying the integration kind and the new validation time and no
secret. Reconnecting owner clients replay it through the existing `client_events` contract. No
new stream behavior is introduced.

### Identity, Ownership, and Permissions *(mandatory for user/account data; otherwise state N/A)*

- **Bootstrap and invitations**: unchanged.
- **Ownership and authorization**: integration configuration is installation-wide, not
  user-owned. Every operation requires an owner, checked in the service against the persisted
  role as owner administration already does, and again at the route. A member must not learn
  even that a mail host exists.
- **Security evidence**: tests proving no secret is returned by any of the three operations;
  that a member and an anonymous caller are refused; that a failed save leaves the ciphertext
  byte-identical; and that the audit entry names no value.

### PWA and Notification Behavior *(mandatory when applicable; otherwise state N/A)*

N/A.

### Key Entities *(include if feature involves data)*

- **Integration settings**: the non-secret view of one integration — kind, whether configured,
  last validation, and for SMTP the host, port, sender, username, and whether a password is
  set. Derived from `external_service_credentials`; never stored separately.

## Success Criteria *(mandatory)*

- **SC-001**: An owner can change the SMTP host, port, sender, username, and password from the
  browser and the change takes effect without a restart.
- **SC-002**: An owner can replace the EODHD key from the browser and the new validation time
  is reflected in the status.
- **SC-003**: A configuration that fails verification is never stored, proven by comparing the
  stored ciphertext before and after.
- **SC-004**: No EODHD key and no SMTP password appears in any response body, log line, audit
  entry, or event payload, verified by scanning a complete read-verify-save cycle.
- **SC-005**: A member session and an anonymous request are refused all three operations.
- **SC-006**: Changing the port while omitting the password keeps mail working.
- **SC-007**: The settings section is usable at 360x800, 768x1024, 1440x900, and 320 pixels.

## Assumptions

- SMTP host, port, sender, and username are configuration rather than secrets. The password
  and the provider key are secrets. Returning the former is what makes the settings editable;
  returning the latter never happens.
- Reusing bootstrap's verifier and validator keeps setup and settings from drifting into two
  different definitions of "working configuration".
- The credential key is unchanged by this feature. Rotation stays the separate host command it
  already is.

## Implementation notes

Kept in this file rather than a separate plan and tasks pair, for the same reason as feature
010: one behavior across known files, no schema change, and no new contract style — the error
envelope and field codes are the ones feature 010 already defined.

- `credentials`: `Replace` writing new ciphertext for one or both kinds in a single
  transaction, plus a non-secret settings read.
- `identity`: `IntegrationSettings`, `VerifyIntegrations`, and `UpdateIntegrations` on the
  existing service, which already holds the validator, the verifier, and the cipher.
- `api`: `GET` extended, `POST /api/v1/owner/integrations/verify`, and
  `PUT /api/v1/owner/integrations`, all behind `RequireOwner`, mutations behind `RequireCSRF`.
- `src`: an integrations section in `AccountSettingsView`, reusing feature 010's field-error
  rendering.

## Implementation evidence (2026-08-31)

**Initial red.** `go test ./internal/api -run TestOwnerIntegrationsAreEditable` returned
`404 {"error":"not found"}` for both new routes — the write path genuinely did not exist.

**Service red.** The three service methods were undefined; once declared, the integration
suite drove the behavior: verification storing nothing, a rejected configuration leaving the
ciphertext byte-identical, an omitted password being filled in from storage, an explicit empty
pair removing authentication, and a member being refused all three operations.

**Two guards fired that no test of mine had written.**

1. `TestEveryImplementedAPIRouteIsDocumentedAndEveryDocumentedRouteExists` rejected both new
   routes until they were described in `specs/004-owner-access/contracts/openapi.yaml`.
2. The shipped-asset secret scan rejected the input id `integration-eodhd-key`, because it
   contains the forbidden literal `eodhd-key`. The id was renamed rather than the guard
   relaxed.

**An accessibility regression, and a pre-existing one behind it.** The new section's alert
measured 2.54:1 against the dark ground, failing the WCAG AA gate. The cause was not the new
code: `#a51d2d` had no dark-theme override, so *every* account-section alert was unreadable in
dark mode. The new section was simply the first alert that test actually rendered. Fixed for
all of them with the `#ffadb7` the stylesheet already uses for error text on dark.

**A wiring defect two test layers could not see.** `IntegrationAdmin` was never wired into the
router, and `credentials` was never assigned in `NewService` — two string edits that silently
did not match. The API tests passed because they inject dependencies explicitly, and the
identity tests passed because the fixture set the private field directly. It surfaced only on
the real screen, as an empty form. The fixture now builds through `NewService` so the
constructor is exercised, `main.go` carries a compile-time interface assertion, and the read
handler logs the failure instead of silently omitting the settings.

**Green.** Full Go suite, 97 Vitest tests, 111 Playwright journeys, `make verify`,
`docker compose config`, `deploy/k8s/test.sh`.

**Verified against the running installation.** The owner settings screen loaded the real
stored configuration (`smtp.gmail.com:587`, sender and username shown, password and provider
key reported as present and never returned), and "Check without saving" performed a real
STARTTLS connection and authentication, reporting *These settings work. Nothing has been saved
yet.* That also confirmed a configuration stored before any verification existed is valid.

## Amendment: per-integration confirmation (2026-08-31)

Requested after the first build: each section should say for itself whether it is correct,
rather than the form reporting one combined result.

**The subtlety that shaped it.** "No error was reported" is not the same as "verified". When a
submitted value is the wrong shape, `checkIntegrations` returns before any network call, so
*neither* integration was contacted - and a section left blank was never submitted at all.
Reporting either as working would assert something that never happened. Each check therefore
returns an explicit outcome per integration: `verified`, `failed`, or `not_checked`, carried on
both the success and the field-error responses.

The client distinguishes one more case the server cannot: `not_checked` for a section the
person never filled in reads *"the saved key is unchanged"*, not *"something else needs
fixing"* - which would send somebody hunting for a problem that does not exist.

**Contract change**: `PUT /owner/integrations` now answers `200` with the same body as the
check, instead of an empty `204`, because a save should confirm what it verified.

**Verified against the running installation**: a check reported the mail server green from a
real STARTTLS connection, and the provider key as informational rather than green, because the
field was left blank and nothing was sent.
