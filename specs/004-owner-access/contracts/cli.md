# CLI Contract: Owner Access Operations

The commands are subcommands of the production `market-lens` binary and use the same
configuration, migrations, repositories, audit records, and durable client-event outbox
as the HTTP server. They never mutate PostgreSQL outside application services.

## Generate initial setup link

```text
market-lens auth setup-link
```

- Available only while bootstrap is open.
- Creates one 15-minute, single-use setup capability and supersedes an older unused
  setup capability.
- Writes the URL once to stdout with the capability in the fragment.
- Structured logs contain only a safe capability ID and expiry.
- A closed setup returns a safe non-success result and emits no URL.

## Reset the owner's password

```text
market-lens auth owner-password reset
```

Preconditions and input:

- stdin must be a terminal; piped/redirected input is rejected before any mutation;
- the database must contain the single active owner;
- the command prompts `New owner password:` and `Repeat new owner password:` using
  no-echo terminal reads;
- it accepts no email, user ID, password flag, password environment variable, or public
  capability;
- mismatch, EOF, cancellation, weak input, database failure, or audit/event failure
  changes nothing and returns nonzero.

Successful transaction:

1. Replace the owner's self-describing Argon2id password hash.
2. Revoke every owner session with reason `owner_password_reset`.
3. Append a safe `owner_password_reset` security audit event.
4. Append `owner.password_reset.v1` and session-revocation invalidation events scoped to
   the owner.
5. Commit all effects together, print one safe success line, and return zero.

The command never prints or logs the owner email, plaintext password, password hash,
session/cookie material, credential configuration, or event payload secrets. Existing
clients lose access when their next request or periodic SSE authorization check observes
the revoked session.

## Rotate the external credential encryption key

```text
EXTERNAL_CREDENTIAL_KEY=<old-base64-key> \
EXTERNAL_CREDENTIAL_KEY_VERSION=<old-version> \
market-lens auth credential-key rotate --new-version <higher-version>
```

- stdin must be a terminal. The new 32-byte base64 key is entered twice with no echo;
  no command flag, process environment variable, file, or database row carries it.
- `--new-version` is a positive integer greater than every stored/current version and
  is non-secret.
- The command decrypts and authenticates every credential before opening the write
  transaction. It then re-encrypts all rows with fresh random nonces and the new version,
  writes a safe audit/event record, and commits atomically.
- Any missing row, old-key mismatch, authentication failure, input mismatch, or write
  failure leaves every row unchanged and returns nonzero.
- After success, the operator updates the deployment secret and
  `EXTERNAL_CREDENTIAL_KEY_VERSION` together before restarting the normal server.
- Startup fails closed if the configured version does not match persisted rows.

CLI tests inject a terminal abstraction, clock, random source, and database fixture;
they inspect stdout/stderr/log captures and persistence for secret leakage.
