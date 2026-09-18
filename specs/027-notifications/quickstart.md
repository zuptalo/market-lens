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
