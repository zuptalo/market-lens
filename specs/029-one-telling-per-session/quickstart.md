# Quickstart: One telling per session

## What arrives now

On a session where eleven instruments changed view, one push and one email arrive.

**Push** — no instrument name, because the payload rests on a third party's server until the
browser collects it:

```text
A strategy changed its view
11 views changed. Open Market Lens to read them.
```

**Email** — subject `11 strategy views changed`:

```text
The momentum_trend strategy changed its view of 11 instruments on its latest session.

  DANSKE: WATCH to BUY
  EQT: REDUCE to HOLD
  NHY: SELL to REDUCE
  ...

The Signals screen has the reasoning behind each one.

This is a strategy output, not advice. …
```

The caveat appears once, not eleven times.

**A quiet session is unchanged.** One instrument changing still produces the message it always did,
naming the instrument in the subject: `NHY: a strategy view changed`. Collapsing must not make the
ordinary case vaguer than the form it replaced.

## What it is about

A telling is about a **session**, not an instrument. The pass claims the session in
`signal_change_sessions` before raising anything, so:

- running the pass twice over one session says nothing twice — a restart, a retry, a second
  invocation;
- somebody who switches the kind on afterwards hears about the *next* session, not that one. The
  claim is recorded whether or not anybody was listening, which is what makes consent stay
  non-retroactive.

```sql
SELECT session_date, changes, told_at FROM signal_change_sessions ORDER BY session_date DESC;
```

An operator asking why a quiet night was quiet gets an answer from that row rather than a guess.

## Turning it off

Unchanged: Account settings → Notifications, "A strategy changed its view", per channel. Or the
unsubscribe link in the email, which needs no sign-in and changes exactly that one kind on that one
channel.

## What did not change

Quiet hours, retry and backoff, per-device revocation, and every other kind. `decision_waiting`
already collapsed with a count — the email reading *"27 decisions waiting in Market Lens"* is the
form this feature copied.
