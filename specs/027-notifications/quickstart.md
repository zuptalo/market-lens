# Quickstart: Consented Email and Web Push Alerts

## Asking to be told

Open **Account settings → Notifications**. Everything starts off. Four kinds, two channels each:

- **A decision is waiting** — a finding this product will not settle by itself, or a rule you set
  that is no longer being met.
- **A paper order settled** — filled, or could not. It happens overnight with nobody watching.
- **Market data did not arrive** — offered to the owner alone, because nobody else can act on it.
- **A strategy changed its view** — what a strategy makes of an instrument moved. It is a strategy
  output, not advice, and the message says so.

Turning one off stops it immediately, on every device.

## Quiet hours

A start time, an end time and a zone. Anything raised inside the window is **held until it ends** —
not sent early, and not dropped. A window whose end is before its start crosses midnight, which is
the ordinary case, and the zone is stored rather than an offset so the window follows the clock on
your wall through daylight saving.

## Push

Press **Subscribe this device**. The browser asks permission; if you refuse, nothing is recorded and
nothing is tried again. On an iPhone, add Market Lens to the home screen first — Safari only allows
push to an installed app.

Every device is listed by a name you can read and can be removed from any signed-in device. Removing
deletes it rather than muting it.

**A push says less than you might expect, on purpose.** "A paper order settled", not "ABB filled at
600.30". The payload rests on a push service's server until your browser collects it, and that
service is a third party nobody here chose. A count and a link are enough to bring you to the screen
that has the figures, which is where they are already protected.

## Email

Uses the SMTP server under **Integrations**. That section now has **Send a test message to
yourself** — press it. Saving already proves the server accepts a connection; this proves a message
arrives, which is a different failure. A server can connect, refuse the sender, and look configured
until the first real alert quietly does not turn up.

Every email carries a link that turns off the kind that sent it, without signing in. It changes that
one preference and can do nothing else.

## Checking it is not lying to you

- **A new account receives nothing.** Every switch is off and no row exists until you move one.
- **Nothing is sent twice.** Delivery state is a machine, so two passes running at once deliver once
  between them.
- **A provider outage costs an hour, not a message.** Failed attempts widen their gap and keep the
  reason; after five, it is abandoned and says why, where you can see it.
- **No message carries what you own.** No figure, no quantity, no valuation, no balance. An email
  may name an instrument; a push payload may not even do that. It is enforced by a schema at the
  source, not by care in a template.

## What it deliberately has no room for

No SMS, no chat integrations, no webhooks. No third-party notification service — email goes through
your own server and push goes directly to the browser's own endpoint. No read receipts, no open
tracking, no click tracking; the migration test asserts those columns do not exist. And no marketing
of any kind: every message exists because a change happened that you asked to hear about.

---

## Recorded evidence

`v0.23.0` on k3s, 2026-09-18. Nothing was seeded; every figure below is the deployment's own state.

**The key generated itself, which was the point.** No configuration was added to the deployment and
nothing was supplied:

```text
migration: 30 at 2026-09-18 17:53:37+00
push keys: 1 (private 32 bytes, public 65 bytes)
push key subject: https://market-lens.zuptalo.com
```

One pair, the right shapes, and a subject this instance derived from its own address. A restored
backup of this database keeps every subscription working; a lost key would have invalidated all of
them with no error anywhere, which is why it lives here rather than in an environment variable.

**Nothing is switched on and nothing is tracked:**

```text
preferences: 0   quiet hours: 0   subscriptions: 0   notifications: 0
tracking columns: 0
  (opened_at, clicked_at, read_at, tracking_id, campaign, user_agent, ip_address)
```

Zero preferences is the correct state, not an incomplete one: an absent row and a disabled one mean
the same thing, so seeding eight rows per account would have looked like a decision had been made
for somebody.

**Every constraint refuses what it should.** Run against production inside a transaction that was
rolled back, using a real account so nothing but the constraint under test could be responsible:

```text
refused: a second push key for this instance            -> instance_push_key_singleton
refused: a kind this product does not send              -> notification_preferences_kind_check
refused: a channel this product does not use            -> notification_preferences_channel_check
refused: the same kind and channel twice for one person -> notification_preferences_pkey
refused: a quiet window that never ends                 -> notification_quiet_hours_check
refused: a notification that says it was sent, not when -> notifications_check
refused: a subscription key of the wrong length         -> push_subscriptions_p256dh_check
refused 7 of 7
```

After the rollback: one push key, zero preferences.

**The public surface is genuinely public, and provably so.** This is the first feature here whose
production evidence is more than a wall of 401s, because push cannot work at all unless the browser
can fetch the service worker without a session:

```text
GET  /sw.js                               -> 200, text/javascript, 2585 bytes, 1 push handler
GET  /unsubscribe                         -> 200
POST /api/v1/notifications/unsubscribe    -> 400   (reached the handler, refused the bad token)
```

The **400 is the useful one**. Every other path on this deployment answers 401 whether or not it
exists, because authentication runs ahead of routing — so a 401 proves a route is guarded, never
that it exists. A 400 proves the opposite: the request passed the public allow-list, reached the
handler, and was refused on its merits.

**Everything else needs a session:**

```text
GET /api/v1/notifications/preferences     -> 401
GET /api/v1/notifications/subscriptions   -> 401
GET /api/v1/notifications/history         -> 401
GET /api/v1/notifications/push-key        -> 401
```

`/manifest.webmanifest` also answers 401 to an anonymous request, which is expected: it is linked
with `crossorigin="use-credentials"` so a signed-in browser sends its cookie when fetching it.

**What to check when you next open it.** Account settings → Notifications: every switch off, and the
statement saying so. Press **Send a test message to yourself** under Integrations first — if that
does not arrive, no alert will either, and it is a faster thing to debug. Then switch on *A decision
is waiting* by email, and subscribe a device for push. Set quiet hours covering now and confirm
nothing arrives until the window ends; nothing should be lost, only held.
