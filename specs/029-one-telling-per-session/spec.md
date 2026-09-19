# Feature Specification: One telling per session, not one per instrument

**Feature**: 029 | **Branch**: `039-one-telling-per-session` | **Milestone**: Notifications-B
**Status**: planned — corrects shipped behaviour in feature 027; one decision resolved by the owner
**Created**: 2026-09-19

**Input**: Eleven instruments changed view on one session. Eleven push notifications arrived on one
phone, all reading "A strategy changed its view / Open Market Lens to read it.", and eleven emails
arrived beside them.

## What happened

On 2026-09-19 the nightly pass found eleven instruments whose view under `momentum_trend` had
changed — DANSKE `WATCH→BUY`, NHY `SELL→REDUCE`, EQT `REDUCE→HOLD`, and eight more. Every one of
them was correct, and each was raised as its own notification on each channel.

The result is the screenshot that started this: a lock screen filled with eleven **byte-identical**
messages. They are identical because feature 027 decided, rightly, that a push payload may not
carry an instrument name — it rests on a third party's server until the browser collects it
(FR-015). So the one field that told them apart is deliberately stripped, and what is left repeats.

This is not a delivery defect. Nothing looped, nothing retried, nothing was sent twice. The product
did exactly what it was told, eleven times, and the instruction was wrong.

**The product already knows how to do this correctly.** The same nightly pass raised
`decision_waiting` once, with a count, and it arrived as one message: *"27 decisions waiting in
Market Lens"*. Signal changes call the same function in a loop.

## The decision the owner resolved

| Question | Decision | Consequence |
|---|---|---|
| How should a session's changes be grouped? | **Always one telling per session, on both channels** | However many change, one push and one email. No threshold to tune, and a volatile session can never flood a phone. A quiet session with one change says "1 strategy view changed" |

The rejected alternatives were a threshold (send individually up to three, collapse above it) and
collapsing the push while leaving the emails alone. Both keep a number in the product that has to
be justified, and the second leaves the inbox exactly as it is.

## Why the push looked worse than it should have

`sw.js` already asks the browser to collapse messages of the same kind — `tag: payload.kind`,
`renotify: false`. On this iPhone it did not: all eleven are listed separately. Whatever the
platform does with a tag, it is not a guarantee this product may lean on, and the screenshot is the
evidence. **Sending one message is the fix; the tag stays as a courtesy, not as the mechanism.**

## User scenarios

### US1 — A noisy session says one thing (P1)

Eleven instruments change view overnight. One push arrives: *"11 strategy views changed."* One
email arrives, listing all eleven with the view each moved from and to, and carrying the strategy's
caveat once rather than eleven times.

**Independently testable**: commit eleven changed signals, run the pass, and count the notifications
raised per person per channel — one, with a count of eleven and every ticker in its detail.

### US2 — A quiet session still says something (P1)

One instrument changes. One push, one email, and the email names that instrument and its two views
exactly as it does today. The collapsed form must not make the ordinary case vaguer than the form
it replaces.

**Independently testable**: one change, one notification, and the email body still contains the
ticker, the strategy, and both views.

### US3 — Running the pass twice says nothing twice (P2)

The pass runs again over the same session — a restart, a retry, a second invocation. Nothing further
is raised, because the telling is about the session and that session has already been told about.

**Independently testable**: run the raise twice over identical data and assert the second run raises
nothing for a person who already has that session's notification.

### US4 — Somebody who opts in tomorrow hears about tomorrow (P2)

The existing rule survives: a person who enables the kind after a session has been raised is not
sent that session's telling. Consent is not retroactive, and idempotency must not become a backlog.

**Independently testable**: raise a session, enable the preference afterwards, raise the same
session again, and assert nothing arrives.

## Functional requirements

### Grouping

- **FR-001** A session's signal changes produce exactly one notification per consenting person per
  channel, whatever the number of instruments that changed.
- **FR-002** That notification carries the number of changes as its count, and the per-instrument
  detail — ticker, strategy, view before, view after — as a list.
- **FR-003** The notification is identified by the session it is about, so a second pass over the
  same session raises nothing further for a person who already has it (US3). A person who consents
  after the fact is still not told about it (US4).

### What each channel says

- **FR-004** The push carries the kind, the count, and a path. It names no instrument, exactly as
  before, and its wording states the number so that one message is not mistaken for one change.
- **FR-005** The email names every instrument that changed, with the strategy and both views, one
  line each. It states what changed and never what to do, and it carries the strategy's caveat
  once.
- **FR-006** The caveat, the "you asked for this", and the unsubscribe link are unchanged, and the
  advice-vocabulary guard applies to the collapsed template exactly as it did to the single one.

### What may travel

- **FR-007** The per-instrument list is subject to the same schema that governs every other
  notification detail: a ticker, a strategy and two view names are permitted; anything else is
  refused where it is raised rather than caught in review of a template. No figure, holding,
  quantity or valuation may appear in the list, per item or in total.
- **FR-008** No push payload gains the list. What is permitted in an email is not permitted in a
  push, and the difference is asserted by test.

## Scope by exclusion

- **FR-009** No change to any other kind. `decision_waiting`, `paper_fill`, `pipeline_failure` and
  `release_deployed` already say one thing per event and are not touched.
- **FR-010** No digest across sessions, no daily summary, no frequency setting. One session, one
  telling; a person who does not want them turns the kind off.
- **FR-011** No change to quiet hours, retry, or unsubscribe behaviour.
- **FR-012** No reliance on the browser's notification `tag` to hide the problem.

## Test-First Proof *(mandatory)*

- **Initial failing test**: a Go integration test raising a session with three changed instruments
  and asserting one notification per person per channel with a count of three.
- **Expected red reason**: three rows are found where one was expected — a behavioural failure
  against the current per-instrument loop, not a compile error.
- **Green evidence**: `make verify`, plus the existing privacy and consent suites unchanged.
- **Database migration proof**: two migrations. `0034` adds the table that records which sessions
  have been told about, proved by a test that claims one twice and asserts the second claim takes
  nothing. `0035` is a data correction: an installation upgrading into this has a session that
  already produced one telling per instrument and no claim to show for it, so the first pass after
  the upgrade would say the whole thing once more. A test seeds that exact state — migrations
  through 34, old-style tellings keyed by ticker — and asserts the upgrade claims the session,
  while a clean installation claims nothing.

## Responsive UI Behavior

N/A — no screen changes. The Signals screen already lists every change and is unaffected.

## Live Update Behavior

N/A — no event contract changes.

## Identity, Ownership, and Permissions

Unchanged. One notification per person, never a shared row; the existing cross-user isolation tests
continue to apply to the collapsed form.

## PWA and Notification Behavior

- **Installability**: unchanged.
- **Consent and delivery**: unchanged — same kind, same two channels, same opt-in, same quiet hours.
  The payload gains a count in its wording and loses nothing.
- **Test evidence**: the existing denied-permission, expired-subscription, offline and revoked-device
  scenarios continue to apply; one is added for a repeated pass over the same session.

## Success criteria

- **SC-001** Eleven instruments changing view produce one push and one email, not eleven of each.
- **SC-002** The email names all eleven instruments with both views each; the push names none.
- **SC-003** A session already told about produces nothing on a second pass.
- **SC-004** A person who consents after a session was raised receives nothing for that session.
- **SC-007** Upgrading an installation that has already sent per-instrument tellings produces no
  further telling about the session those were about.
- **SC-005** No push payload contains a ticker, asserted across every kind.
- **SC-006** The collapsed email passes the advice-vocabulary guard, and says once that it is a
  strategy output rather than advice.

## Key entities

- **Signal-change telling** — one notification per person per channel per session: the session it is
  about, how many instruments changed, and the list of what each changed from and to.

## Assumptions

- The session a telling is about is the latest stored signal session, which is what the existing
  query already selects on.
- Eleven is not a worst case. The universe is larger than eleven instruments, and a regime change
  could move most of them at once; the collapsed form is bounded by one message either way, and the
  email's list grows rather than the number of messages.
