# Implementation Plan: Consented Email and Web Push Alerts

**Branch**: `027-notifications` | **Date**: 2026-09-18 | **Spec**: [spec.md](spec.md)

## Summary

A person asks to be told about certain kinds of change, on certain channels, outside certain hours.
The product writes a notification in the transaction that causes the change and delivers it from a
pass in the same process.

Four things shape the implementation.

**The VAPID key follows the signing key exactly.** Migration 0011 already solved "a secret this
instance owns": self-provisioned, database-resident, one forever, converging under simultaneous
starts. Copying that pattern rather than inventing a second one is most of the work here, and the
reason a restored backup keeps working instead of silently invalidating every subscription.

**Web Push is standard-library.** RFC 8291's encryption and RFC 8292's assertion need `crypto/ecdh`,
`crypto/hkdf`, `crypto/aes` and `crypto/ecdsa` and nothing else at Go 1.26. The encryption is
verified against RFC 8291's own worked example, so it is checked against the specification rather
than against itself.

**What a message may carry is a schema question, not a wording one.** A push payload travels through
a third party's server; an email leaves the building. So permitted keys are asserted per kind, and
every template is scanned by the advice-vocabulary guard that already scans the interface — which is
what makes the signal-change kind safe to have.

**SMTP already exists and is not rebuilt.** The Integrations section validates and verifies against
the real server when saved. What it gains is a test send, because a server can accept a connection,
refuse a sender, and look configured.

## Technical Context

**Language/Version**: Go 1.26 (backend), TypeScript 5 with Vue 3 (frontend)

**Primary Dependencies**: standard library only for the push cryptography. pgx/PostgreSQL 18,
PrimeVue 4. **No new module.**

**Storage**: PostgreSQL. One ordered migration: `instance_push_key`, `notification_preferences`,
`notification_quiet_hours`, `push_subscriptions`, `notifications`.

**Testing**: Go tests including a migration test asserting seven tracking columns absent, the RFC
8291 §5 vector, quiet hours across midnight and across a daylight-saving boundary, at-most-once
under two concurrent passes, cross-user isolation on every path, a gone-endpoint self-removal, and a
payload-content assertion per kind. Vitest; Playwright across the three viewport projects plus the
320 floor, through `e2e/mobile-layout.spec.ts`.

**Responsive UI Verification**: notification settings live inside the existing Account screen, which
the mobile-layout guard already walks. The new controls are switches and a time range; both stack.

**Live Delivery**: none, deliberately. A notification *is* the telling; an event about it would be a
notification about a notification, and an open page already has the live stream (FR-028).

**Identity and Ownership**: `user_id` on all four person-owned tables, every predicate reading it.
Service methods take the caller's identifier first. The delivery pass is the one thing that acts for
everybody, so its isolation is asserted hardest — as the paper fill pass was.

**PWA and Notifications**: this is the feature. A service worker is added (`public/sw.js`), scoped
to the origin, whose only job is to receive a push and show it. Verified in Chrome; Edge shares the
engine and the same code path.

**Red-Green-Refactor Proof**: the designated first red is
`TestNothingIsSentToSomebodyWhoDidNotAskForIt`: an account with no preferences, a change that raises
every kind, a delivery pass, and an assertion that the sender was never called. It fails on a value —
the sender counts a call — rather than on compilation.

**Database Evolution**: `0030_notifications.sql`, with a migration test proving a clean install and
an upgrade arrive with five tables, the singleton index on the key, `user_id` not nullable on the
four person-owned tables, the sent/sent_at check refusing a half-state, and the seven forbidden
columns absent.

**Target Platform**: Linux container serving the built SPA from the Go process; PostgreSQL 18.

**Performance Goals**: a pass over a thousand pending notifications completes within a stated
budget — enough to catch a per-notification query.

**Constraints**: nothing is sent to somebody who did not ask; nothing is sent twice; nothing raised
inside quiet hours is sent early or lost; no payload carries a figure or a holding; no template
contains advice vocabulary.

**Scale/Scope**: a handful of people, subscriptions in the tens, notifications in the hundreds a
month.

## Constitution Check

| Principle | Assessment |
|---|---|
| **I. Specification-driven** | PASS. Three open decisions put to the owner and resolved before planning. Five exclusions, each testable. |
| **II. Modular monolith** | PASS. One new Go package plus a push sub-package. No broker, no queue, no third-party notification service; the pass runs in process like the others. |
| **III. Migration-only evolution** | PASS. One ordered migration, exercised by a test. No seed data — a seeded preference row would look like a decision had been made. |
| **IV. Versioned contracts** | PASS. Nine operations, declared `user_private` except the unsubscribe path, which is declared `public` and constrained to one preference. |
| **V. Correctness and reproducibility** | PASS. The encryption is checked against RFC 8291's own vector. Delivery state is a machine, so at-most-once holds under two passes rather than by care. |
| **VI. Test-driven development** | PASS. The first red is the property the whole feature depends on being true: nothing reaches somebody who did not ask. |
| **VII. PrimeVue-first, accessible, responsive** | PASS. `ToggleSwitch` and `DatePicker` in time mode; the settings sit in the Account screen the mobile guard already walks. |
| **VIII. Operational simplicity** | PASS. The VAPID key provisions itself, so the deployment gains no new configuration. Email uses the SMTP server already configured. |
| **IX. Identity, ownership, isolation** | PASS. Ownership explicit on four tables and proved on every path including the pass. The unsubscribe token is single-purpose and cannot widen. |
| **X. Live updates and consented notifications** | PASS — this is the principle the feature exists to satisfy. Granular per-kind per-channel opt-in, per-device revocation, minimal payloads, unsubscribe without signing in, and retry with a recorded reason on provider failure. |

**Post-design re-check**: PASS. Phase 1 adds five tables, one package, nine operations, one service
worker and one delivery pass. Nothing introduces a third-party service, a stored payload, a tracking
column, or a message a person did not ask for.

## Project Structure

```text
specs/027-notifications/
├── plan.md · spec.md · research.md · data-model.md · quickstart.md
├── contracts/openapi.yaml
├── checklists/requirements.md
└── tasks.md
```

```text
server/
└── internal/
    ├── db/migrations/0030_notifications.sql        # NEW: five tables, one singleton index
    ├── notify/                                     # NEW package
    │   ├── model.go                                # kinds, channels, states, preferences
    │   ├── preferences.go                          # what a person asked for, per kind per channel
    │   ├── quiet.go                                # the window, including across midnight
    │   ├── raise.go                                # fan-out, in the caller's transaction
    │   ├── deliver.go                              # the pass: claim, send, retry, abandon
    │   ├── templates.go                            # what each kind says, and what it may not
    │   ├── unsubscribe.go                          # the signed single-purpose token
    │   ├── service.go · repository.go
    │   └── *_test.go                               # the first red lives here
    ├── notify/push/                                # NEW: RFC 8291 and RFC 8292
    │   ├── vapid.go                                # the ES256 assertion
    │   ├── encrypt.go                              # aes128gcm, checked against RFC 8291 §5
    │   ├── sender.go                               # the HTTP post, and 404/410 self-removal
    │   └── *_test.go
    └── api/notifications.go                        # NEW: the nine operations

src/
├── views/AccountSettingsView.vue                   # hosts the new section
├── components/account/
│   ├── NotificationSettings.vue                    # NEW: the switches, quiet hours, devices
│   └── IntegrationSettings.vue                     # gains a test send
├── services/notifications.ts                       # NEW: the calls and the subscribe dance
└── views/UnsubscribeView.vue                       # NEW: the public one-purpose page

public/sw.js                                        # NEW: receive a push, show it
e2e/notifications.spec.ts                           # NEW
```

**Structure Decision**: `notify` owns consent, raising and delivery; `notify/push` owns only the
cryptography and the HTTP, so it can be tested against the RFCs without a database. Email reuses
`internal/mail` unchanged — the templates are new, the transport is not.

## Complexity Tracking

No constitution violations require justification. Five choices are less obvious than the
alternative:

| Choice | Why | Simpler alternative rejected because |
|---|---|---|
| VAPID key in the database | A restored backup keeps working; losing it silently stops every push (R-001) | An environment variable makes it a deployment concern and one more thing to lose invisibly |
| Web Push implemented, not imported | Everything needed is standard library at 1.26, and this path handles a private key (R-002) | A module wraps exactly these calls and adds an audit surface |
| Notification written in the caller's transaction | "If the change rolls back, so does the notification" without a compensating action (R-003) | Raising afterwards is simpler and tells people about things that did not happen |
| Quiet hours as local time plus a zone | A person means "while I am asleep", and daylight saving moves that against UTC twice a year (R-004) | A UTC offset is simpler and wrong twice a year |
| Push payload carries a count, not a figure | The payload rests on a third party's server until collected; the threat is accumulation (R-006) | A useful payload is friendlier and puts holdings on somebody else's disk |
