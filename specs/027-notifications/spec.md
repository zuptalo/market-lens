# Feature Specification: Consented Email and Web Push Alerts

**Feature**: 027 | **Branch**: `027-notifications` | **Milestone**: Notifications-A
**Status**: Reviewed — three decisions resolved by the owner before planning

## Summary

The product has spent seven milestones accumulating things only a person can decide, and it has no
way to tell them. A finding waits, a limit is breached, a paper order fills overnight — and all of
it sits unseen until somebody happens to open the app. This feature lets a person **ask** to be told,
on the channels they choose, about the kinds they choose, and stop at any time from any device.

Everything here is opt-in and nothing is on by default. A product that mails somebody without being
asked has made a decision on their behalf, which is the thing this codebase has refused at every
turn.

## Decisions resolved by the owner

| Question | Decision | Consequence |
|---|---|---|
| What earns a notification? | **All four kinds**: decisions waiting, paper fills, pipeline failures, signal changes | The fourth is the one that edges toward advice, so its wording is constrained by requirement rather than by care (FR-021) |
| When does it go out? | **Immediately, with quiet hours** | Delivery is coupled to the change that caused it; a quiet window holds and then releases rather than dropping |
| How granular is the opt-in? | **Per kind, per channel** | Eight switches per person. Wanting a fill on your phone but not in your inbox is an ordinary preference |

## The kind that needed a second look

**Signal changes were chosen knowing the risk.** A push saying *"NOKIA is now a buy"* reads as a
recommendation however carefully it is worded, and feature 021 measured this strategy and found it
lost to two of its three benchmarks over ten years. So the requirement is not "word it carefully":

- **FR-021** A signal notification states **what changed**, never what to do. It names the
  instrument, the strategy, and the two views — the one it held and the one it holds now — and it
  carries the strategy's own caveat. It contains no imperative, no recommendation vocabulary, and
  no figure presented as an outcome.
- **FR-022** The advice-vocabulary guard that already scans the interface extends to every
  notification template, so the constraint is enforced by test rather than by review.

## User scenarios

### US1 — Ask to be told, and be told (P1)

A person switches on "a decision is waiting for me" for email. A quality finding is raised that
needs settling. Within the product's stated delivery window they receive one message: what kind of
thing is waiting, how many, and a link to the screen that owns it. No figures, no holdings.

**Independently testable**: set the preference, commit the change that raises the event, run the
delivery pass, and read the recorded delivery and its content.

### US2 — Be told on a phone, quietly (P1)

The same person subscribes their phone. A paper order fills at 03:00, inside the quiet hours they
set. Nothing arrives at 03:00. When the window ends, one push arrives.

**Independently testable**: subscribe a device, set quiet hours, commit a fill inside the window,
and observe that delivery is deferred rather than sent or dropped.

### US3 — Stop, from anywhere (P1)

They revoke that phone from any signed-in device. Nothing more reaches it, and the subscription is
gone rather than merely muted. Separately, every email carries a link that turns off the kind that
sent it, without signing in.

**Independently testable**: revoke and confirm no further delivery is attempted; follow an
unsubscribe link from a signed-out browser and confirm the preference changed and nothing else did.

### US4 — The provider is down (P2)

The mail server refuses connections for an hour. Nothing is lost: each pending notification is
retried with a widening gap, and one that cannot be delivered after a stated number of attempts is
recorded as failed with its reason, visible to the person it was for.

**Independently testable**: a sender that fails, then succeeds; assert the message is delivered once
and only once, and that a permanently failing one ends as failed rather than disappearing.

## Functional requirements

### Consent

- **FR-001** Every notification kind is off for every person until they switch it on, per channel.
  A newly created account has nothing enabled and receives nothing.
- **FR-002** The four kinds are: `decision_waiting`, `paper_fill`, `pipeline_failure`,
  `signal_change`. The two channels are `email` and `web_push`.
- **FR-003** `pipeline_failure` is offered to the owner only, because it is the only kind nobody
  else can act on. It is absent from a member's preferences rather than present and refused.
- **FR-004** A person sets quiet hours as a start time, an end time and a timezone. A notification
  raised inside the window is held and delivered when the window ends, never dropped and never sent
  early.
- **FR-005** Every email carries a one-click unsubscribe for the kind that sent it, usable without
  signing in, and it changes exactly that one preference.

### Web Push

- **FR-006** The instance's VAPID key pair is **generated by the product on first start and stored
  in the database**, exactly as the signing key is: a deployment needs only `DATABASE_URL`, and a
  database backup remains a complete installation.
- **FR-007** There is exactly one key pair, forever, converging without an advisory lock when two
  instances start at once. The public key is readable by any signed-in caller; the private key
  leaves the server only as a signature.
- **FR-008** A person may subscribe any number of devices. Each records what the browser gave and
  nothing about the device beyond a label the person can read.
- **FR-009** A subscription is revocable from any signed-in device of the same person, and is
  deleted rather than flagged.
- **FR-010** A push the browser's own service rejects as gone (404 or 410) deletes that
  subscription without a person doing anything. A browser that forgot is not an error to report.

### Delivery

- **FR-011** A notification is written in the same transaction as the change that caused it. If the
  change rolls back, so does the notification.
- **FR-012** Delivery is attempted by a pass that runs in process. A notification is delivered at
  most once per channel, enforced by stored state rather than by the pass being careful.
- **FR-013** A failed attempt is retried with a widening gap. After a stated number of attempts the
  notification is `failed`, with the reason kept.
- **FR-014** A person can read what was sent to them, when, and what failed — without the payload,
  which is not stored beyond what is needed to send it.

### What a message may contain

- **FR-015** A push payload carries the kind, a count, and a path within the product. It carries no
  monetary figure, no instrument name, no holding, and no strategy opinion — a push payload travels
  through a third party's server and is stored there until it is collected.
- **FR-016** An email may name an instrument and a kind, and may not carry a holding, a quantity, a
  valuation, a return, or a cash balance. What the person owns is not sent anywhere.
- **FR-017** Every message states that the person asked for it and how to stop.

### Ownership and isolation

- **FR-018** Preferences, subscriptions and notifications belong to one person and are enforced in
  services and queries. Two people's are invisible to each other in reads, writes and events.
- **FR-019** A notification raised by a shared change (a finding, a pipeline failure) produces one
  row per consenting person, never one shared row.
- **FR-020** A deactivated account receives nothing, and its pending notifications are not delivered.

### Honesty

- **FR-021** A signal notification states what changed and never what to do. (See above.)
- **FR-022** The advice-vocabulary guard extends to notification templates.
- **FR-023** No notification claims an outcome, a performance, or a reason to act.

## Scope by exclusion

- **FR-024** No SMS, no native mobile push, no chat integrations, no webhooks.
- **FR-025** No marketing, digest, or engagement messaging. Every message is caused by a change the
  person asked to hear about.
- **FR-026** No third-party notification service. Email goes through the SMTP server the owner
  configured; push goes directly to the browser's own endpoint.
- **FR-027** No read receipts, open tracking, or click tracking.
- **FR-028** No notification in the browser while the app is open — that is what the live stream is
  for, and a duplicate would be worse than nothing.

## Success criteria

- **SC-001** A new account has eight preferences, all off, and receives nothing until it asks.
- **SC-002** A notification raised inside quiet hours is delivered after the window ends, once.
- **SC-003** A delivery attempted twice is sent once, proved by a sender that counts.
- **SC-004** A revoked subscription receives nothing further, and a gone endpoint removes itself.
- **SC-005** No push payload and no email body contains a monetary figure, a quantity, or a holding,
  asserted across every template.
- **SC-006** Two people's preferences, subscriptions and notifications are provably invisible to one
  another on every path.
- **SC-007** Every template passes the advice-vocabulary guard.
- **SC-008** Every screen behaves at 360x800, 768x1024 and 1440x900, tolerates 320 CSS pixels
  without page-level horizontal scrolling, and states every figure as text.

## Key entities

- **Push key pair** — one per instance, self-provisioned, database-resident.
- **Notification preference** — one row per person per kind per channel, off until asked.
- **Quiet hours** — one row per person: start, end, timezone.
- **Push subscription** — one per device: endpoint, the browser's two keys, a readable label.
- **Notification** — one per person per kind per event: state, attempts, last reason, delivered at.

## Assumptions

- Email uses the SMTP server already configured in Account settings. This feature adds a **test
  send** there, because a mail path that is only exercised by a real alert is a mail path nobody
  finds out is broken until it matters.
- Web Push is verified in Chrome and Edge, the browsers this product's PWA supports.
- Quiet hours default to off. A default window would be a decision about somebody's sleep.
