# Phase 1 Data Model: Consented Email and Web Push Alerts

One migration, `0030_notifications.sql`: five tables and one seeded-by-code singleton.

## `instance_push_key`

The VAPID key pair, self-provisioned on first start. Shaped after `instance_signing_key` (migration
0011) deliberately, so there is one pattern for "a secret this instance owns" rather than two.

| Column | Type | Notes |
|---|---|---|
| `id` | uuid pk | |
| `private_key` | bytea not null | P-256 private scalar, 32 bytes |
| `public_key` | bytea not null | uncompressed P-256 point, 65 bytes |
| `subject` | text not null | the `sub` claim: a `mailto:` for this instance |
| `created_at` | timestamptz not null default now() | |

```sql
CREATE UNIQUE INDEX instance_push_key_singleton ON instance_push_key ((true));
```

One forever. Provisioning is `INSERT ... ON CONFLICT DO NOTHING` then an unconditional `SELECT`, so
two instances starting together converge without an advisory lock. **Rotating it invalidates every
existing subscription**, which is why there is no rotation path in this feature: it would need a
re-subscription flow to be anything but a silent outage.

## `notification_preferences`

| Column | Type | Notes |
|---|---|---|
| `user_id` | uuid not null | |
| `kind` | text not null | `CHECK (kind IN ('decision_waiting','paper_fill','pipeline_failure','signal_change'))` |
| `channel` | text not null | `CHECK (channel IN ('email','web_push'))` |
| `enabled` | boolean not null default false | |
| `updated_at` | timestamptz not null default now() | |
| | | `PRIMARY KEY (user_id, kind, channel)` |

**No seed data.** A row's absence and `enabled = false` mean the same thing, and both mean nothing is
sent. Seeding eight rows per account would look like a decision had been made.

## `notification_quiet_hours`

| Column | Type | Notes |
|---|---|---|
| `user_id` | uuid pk | one window per person |
| `starts_at` | time not null | local wall clock |
| `ends_at` | time not null | |
| `timezone` | text not null | IANA name; a person means "while I am asleep" |
| `updated_at` | timestamptz not null default now() | |

A window whose `ends_at` is before its `starts_at` crosses midnight. Absent means no quiet hours,
which is the default: a default window would be a decision about somebody's sleep.

## `push_subscriptions`

| Column | Type | Notes |
|---|---|---|
| `id` | uuid pk | |
| `user_id` | uuid not null | |
| `endpoint` | text not null | the browser's own URL |
| `p256dh` | bytea not null | the browser's public key, 65 bytes |
| `auth` | bytea not null | the browser's shared secret, 16 bytes |
| `label` | text not null | what the person will recognise it by |
| `created_at`, `last_used_at` | timestamptz | |
| | | `UNIQUE (user_id, endpoint)` |

Nothing about the device beyond the label. A user agent string is a fingerprint and is not needed to
send a push.

## `notifications`

One row per person per kind per event — never one shared row with a recipient list.

| Column | Type | Notes |
|---|---|---|
| `id` | uuid pk | |
| `user_id` | uuid not null | |
| `kind` | text not null | the four |
| `channel` | text not null | `email` or `web_push` |
| `subject_key` | text not null | what it is about, for collapsing |
| `count` | integer not null default 1 | how many of that thing |
| `detail` | jsonb not null default '{}' | the *minimum* the template needs |
| `state` | text not null default 'pending' | `pending`, `sent`, `failed`, `abandoned` |
| `available_at` | timestamptz not null default now() | quiet hours and backoff both move this |
| `attempts` | integer not null default 0 | |
| `last_error` | text | kept, so a person can see why |
| `created_at`, `sent_at` | timestamptz | `CHECK ((state = 'sent') = (sent_at IS NOT NULL))` |

```sql
CREATE INDEX notifications_due_idx ON notifications (available_at)
    WHERE state = 'pending';
CREATE INDEX notifications_owner_idx ON notifications (user_id, created_at DESC);
```

**At most once per channel** comes from the state machine, not from the pass being careful:
`UPDATE ... SET state = 'sending' WHERE id = $1 AND state = 'pending'` returning zero rows means
another instance took it.

**`detail` holds the minimum.** For a push that is a kind and a count; the payload is built from it
and never stored. An email may add an instrument's ticker. Neither may hold a figure, and a test
asserts what keys are permitted per kind rather than trusting each template.

## Deliberately absent

A migration test asserts these columns do **not** exist anywhere in the feature:

`opened_at`, `clicked_at`, `read_at`, `tracking_id`, `campaign`, `user_agent`, `ip_address`.

The first five are engagement tracking, excluded by FR-027. The last two are device fingerprinting,
which a push does not need and which a subscription list would otherwise quietly accumulate.

## Event

None. A notification is the thing that tells somebody about a change; publishing an event about it
would be a notification about a notification, and the live stream already covers an open page.
