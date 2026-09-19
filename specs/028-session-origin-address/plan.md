# Implementation Plan: The address a session was last seen from

**Branch**: `038-session-origin-address` | **Date**: 2026-09-19 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/028-session-origin-address/spec.md`

## Summary

Record, on each session row, the client address that created it and the address of the most recent
request that authenticated it; show the second one — in full — to the person who owns the session,
beside the device name.

Two things make this more than a column. The first is that the product does not currently know the
client's address at all: behind Traefik, `http.Request.RemoteAddr` is the ingress, so today every
session on the production deployment carries the *same* origin (D1). The second is that the write
has to happen on the hottest path in the application — every authenticated request updates
`last_seen_at` — so the address must ride in that existing statement and cost nothing extra.

The approach: one trusted-proxy resolver in `httpx`, one configuration value, two nullable `inet`
columns added by an ordered migration, the client address threaded through the two service calls
that already carry an origin, and one more fact per row in the session list.

## Technical Context

**Language/Version**: Go 1.26 (server), TypeScript 5 / Vue 3.5 (client)

**Primary Dependencies**: standard library `net/netip` and `net/http`; pgx v5 (`netip.Addr` maps to
PostgreSQL `inet` natively, so no conversion layer and no string parsing in the repository);
PrimeVue 4 `DataTable` on the client. Nothing new is added to either dependency set.

**Storage**: PostgreSQL. Two nullable `inet` columns on `sessions`, added by migration `0033`.

**Testing**: Go integration tests against `TEST_DATABASE_URL` (`server/internal/testdb`), Go unit
tests for the address resolver, Vitest for the client, Playwright for the rendered list.

**Responsive UI Verification**: 360×800 — the stacked Device cell carries the device name with the
address beneath it, wrapping rather than widening; 768×1024 — the same, in table layout; 1440×900 —
the address in its own `Last seen from` column. 320 CSS px is checked with a full-length IPv6
(39 characters) for page-level horizontal scrolling. Playwright covers all three in
`e2e/owner-access.spec.ts` and `e2e/mobile-layout.spec.ts`.

**Live Delivery**: No new event type and no payload change. The list already refetches its REST
snapshot on the existing user-scoped `session.created.v1` / `session.revoked.v1` events; the address
travels only in that authenticated snapshot (FR-011). An added assertion proves no session event
payload carries an address.

**Identity and Ownership**: The address is user-private. `ListSessions` is already scoped by
`user_id` in the query; the new columns are returned through the same scoped path and nowhere else.
The owner's member administration view keeps its device count and gains nothing (FR-009, FR-017).
Cross-user tests assert a member cannot read another member's address, and an owner cannot read a
member's, on every path that returns a session.

**PWA and Notifications**: N/A — nothing is sent anywhere.

**Red-Green-Refactor Proof**:

1. `TestClientAddressPrefersTrustedForwardedHop` (unit, `server/internal/httpx`) — asserts the
   resolver returns the forwarded client address behind a trusted peer and the peer's own address
   otherwise. Red because the resolver does not exist; this is the first test written.
2. `TestSessionRecordsClientAddress` (integration, `server/internal/auth`) — authenticates a session
   from one address, then a second, and asserts the stored `last_seen_ip` follows. Red on a missing
   value, not a compile error, once the column exists.
3. `TestSessionListReportsLastSeenAddress` (integration, `server/internal/api`) — asserts
   `last_seen_from` appears in the session-list response for the caller's own sessions.
4. `SessionList.test.ts` — asserts the address renders in the device row. Red because nothing
   renders it.
5. Green: `make verify` plus `npm run test:e2e`.

**Database Evolution**: One ordered migration, `0033_session_origin_address.sql`, adding
`created_ip inet` and `last_seen_ip inet`, both nullable. No backfill: a pre-existing session has
no address and gains one on its next authenticated request (FR-004). `0007_identity_access.sql` is
applied and is not edited. A migration test proves a clean database upgrades and that an existing
session row with a null address stays valid and authenticable.

**Target Platform**: Linux container behind Traefik on k3s; Chrome, Edge and Safari clients.

**Project Type**: Web application — Go modular monolith serving a Vue SPA.

**Performance Goals**: No additional query, statement, or round trip on the authenticated request
path. The address is one more assignment in the `UPDATE sessions SET last_seen_at=…` that already
runs per request, and one more column in the `SELECT` the session list already runs.

**Constraints**: The resolver must be allocation-light and must never trust a header from an
untrusted peer. A malformed configuration value must stop the process rather than quietly disable
the trust list — a silently empty trust list looks identical, on screen, to a working one.

**Scale/Scope**: A self-hosted deployment: single-digit users, a few hundred session rows. The
retention decision (D2) means the row count is the address count.

## Constitution Check

*GATE: passed before Phase 0, re-checked after Phase 1.*

| Principle | How this plan satisfies it |
|---|---|
| I. Specification-driven | `spec.md` is reviewed with both owner decisions resolved; this plan adds no behaviour the spec does not state. The one amendment made during planning (FR-006, malformed configuration) is recorded in `research.md` and written back into the spec. |
| II. Modular monolith | No new service, process, or dependency. One helper in `httpx`, one field in `config`, existing packages otherwise. |
| III. Migration-only evolution | One ordered migration; `0007` untouched; no manual SQL; no backfill to get wrong. |
| IV. Explicit versioned contracts | `last_seen_from` and `created_from` are added fields on an existing response; no event payload changes; `contracts/openapi.yaml` records the shape. |
| V. Correctness and reproducibility | `inet` storage makes an unparseable address unstorable; the resolver is a pure function with table-driven tests over forged, malformed, IPv6, and multi-hop chains. |
| VI. Test-driven development | Five named red tests, in order, above. The resolver's test is written before the resolver exists. |
| VII. PrimeVue-first accessible responsive UI | Existing `DataTable`; the address is plain selectable text, not a tooltip and not hover-revealed; three viewports plus 320 px verified. |
| VIII. Self-hosted simplicity | One optional environment variable, empty by default, which a development machine never needs to set. |
| IX. Secure identity, ownership, isolation | The whole feature is a disclosure boundary: one person, their own sessions, enforced in the query. Cross-user tests on every path that returns a session. |
| X. Live updates and consented notifications | No event payload gains the address, asserted by test. Nothing is notified. |

**Gate result: pass.** One judgement is recorded rather than waved through: this feature stores a
plaintext network address where feature 004 deliberately stored a digest. That is an owner decision
(D2), it is narrowed to the session row alone, and every requirement in *Not disclosing more than
the one fact* exists to bound it.

## Project Structure

### Documentation (this feature)

```text
specs/028-session-origin-address/
├── plan.md              # This file
├── spec.md              # Reviewed specification
├── research.md          # Phase 0 decisions
├── data-model.md        # Phase 1 schema and entities
├── quickstart.md        # Phase 1 operator and reader guide
├── contracts/
│   └── openapi.yaml     # Session list response shape
├── checklists/
│   └── requirements.md
└── tasks.md             # /speckit-tasks output, not created here
```

### Source code

```text
server/
├── internal/
│   ├── httpx/
│   │   ├── clientaddr.go           # NEW: trusted-proxy address resolution
│   │   └── clientaddr_test.go      # NEW: the first failing test
│   ├── config/
│   │   └── config.go               # TRUSTED_PROXIES parsed into []netip.Prefix
│   ├── db/migrations/
│   │   └── 0033_session_origin_address.sql   # NEW
│   ├── auth/
│   │   ├── model.go                # Session/SessionSummary gain the two addresses
│   │   ├── repository.go           # INSERT, SELECT, and the activity UPDATE
│   │   └── service.go              # AuthenticateSession takes the client address
│   ├── identity/
│   │   ├── repository.go           # bootstrap and invitation session inserts
│   │   └── service.go              # the same address travels with the origin
│   └── api/
│       ├── router.go               # resolver wired into the authentication middleware
│       ├── auth.go                 # authenticationClientMetadata returns the client address
│       └── sessions.go             # last_seen_from / created_from in the response
└── cmd/market-lens/main.go         # configuration threaded to the router

src/
├── types/auth.ts                   # Session gains lastSeenFrom / createdFrom
├── services/auth.ts                # wire mapping and validation
├── utils/device.ts                 # (already shipped) device naming
└── components/account/SessionList.vue

e2e/
├── owner-access.spec.ts            # the address renders, and only for the caller
└── mobile-layout.spec.ts           # 320 px with a full-length IPv6
```

**Structure Decision**: The existing web-application layout. This feature adds exactly one new file
pair to the server (`httpx/clientaddr.go` and its test) and one migration; everything else is an
edit to a file that already owns the concern.

## Complexity Tracking

No constitution violations to justify. The one thing that could look like added complexity — a
configurable trusted-proxy list rather than reading a header directly — is the requirement, not an
embellishment: reading the header unconditionally is the forgeable alternative the owner rejected
in D1.
