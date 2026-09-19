# Quickstart: The address a session was last seen from

## Reading the screen

**Account settings → Signed-in devices.** Each row names the device and, beneath it, the address
that device was last seen from:

```text
Safari on iPhone            Current session
203.0.113.12
Last active  2026-09-19 17:17
```

The address is the one the most recent request from that session arrived from — not the one it
signed in from a week ago. A session used from home and then from a hotel shows the hotel.

A row that says **Not recorded** is not an error. Every session that existed before this feature
shipped has no address, and gains one the next time it is used.

The product states the address and nothing more. It does not look it up, map it, or name a country,
and it sends it nowhere.

## Configuring it: `TRUSTED_PROXIES`

**If you run Market Lens behind a reverse proxy — which the k3s deployment does — you must set
this, or every device will show the proxy's address instead of its own.**

It is a comma-separated list of the networks whose `X-Forwarded-For` header the product will
believe. Anything not on the list is treated as a client, and a header it sends is ignored.

```bash
# In the ConfigMap, beside ALLOWED_ORIGINS. It is not a secret.
TRUSTED_PROXIES=10.42.0.0/16
```

Read the right value off the cluster rather than guessing it — it is the pod network Traefik
reaches the container from:

```bash
kubectl -n kube-system get pods -l app.kubernetes.io/name=traefik \
  -o jsonpath='{.items[*].status.podIP}'
# 10.42.0.31  → the /16 that contains it is what to trust
```

A single host is accepted too (`10.42.0.31` means that address alone). **A value that does not parse
stops the process**, with a message naming the variable and the entry that failed. That is
deliberate: a trust list that silently failed to parse produces a screen full of plausible internal
addresses and nobody would ever find out.

Leave it unset in development. Nothing is trusted, the peer address is used, and `127.0.0.1` is the
correct answer there.

## Checking it worked

After a deployment, open the screen from two different networks — a laptop on wifi and a phone on
mobile data is enough. Two different addresses means the header is being read; two identical
internal ones (`10.42.x.x`) means `TRUSTED_PROXIES` is wrong or unset.

From the database, for an operator who wants to see it directly:

```sql
SELECT device_label, last_seen_ip, last_seen_at
FROM sessions
WHERE user_id = $1 AND revoked_at IS NULL
ORDER BY last_seen_at DESC;
```

## What is kept, and for how long

The address stays on the session row for the life of the row, including after the session is revoked
or expires. Sessions are not deleted, so a deployment accumulates one address per sign-in. That is
a deliberate decision — the record of where a past session was signing in from is the evidence that
matters *after* something has gone wrong — and it means a database backup now contains those
addresses. Nothing else about them changes: they go nowhere, they are readable by one person, and
they are joined to nothing.
