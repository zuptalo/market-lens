# Market Lens roadmap

**Last updated**: 2026-08-29

This is the durable delivery map across sessions. Product intent lives in
[`docs/product-vision.md`](docs/product-vision.md); reviewed requirements/designs live
under [`specs/`](specs/README.md). A roadmap entry does not authorize implementation
without a reviewed feature spec and valid red test.

## Status vocabulary

- **Shipped**: implemented and verified against acceptance criteria.
- **In review**: implementation exists and is being validated/reviewed.
- **In progress**: reviewed spec exists and implementation has started.
- **Planned**: review-ready spec/plan exists; implementation has not started.
- **Backlog**: intended work without a detailed feature spec.
- **Deferred**: intentionally held until prerequisites or evidence exist.

## Delivery roadmap

| Order | Milestone / feature | Status | Governing specification | Depends on | Completion outcome |
|---:|---|---|---|---|---|
| 0 | Application foundation | Shipped | Repository baseline | — | Go/Vue app, migrations, embedded SPA, health/readiness, themes, tests, Docker/Compose, and CI. |
| Ops-A | Optional public k3s deployment | In progress | [`001-k3s-deployment`](specs/001-k3s-deployment/spec.md) | Foundation | Same image runs on target k3s with PostgreSQL, TLS, and image rollout; not a product prerequisite. |
| Release-A | Protected versioned delivery | Shipped | [`003-release-versioning`](specs/003-release-versioning/spec.md) and [plan](specs/003-release-versioning/plan.md) | Foundation | PR-only squash delivery, automatic SemVer/GHCR releases, protected main, and visible runtime version. |
| 1 | Instruments and daily market data | Shipped | [`002-instruments-market-data`](specs/002-instruments-market-data/spec.md) and [plan](specs/002-instruments-market-data/plan.md) | Foundation | 100 Nordic listings, about ten years of daily OHLCV, action context, quality findings, observable imports, read-only inspection, and durable resumable SSE updates. |
| Security-A | Owner bootstrap, authentication, and invitations | Shipped | [`004-owner-access`](specs/004-owner-access/spec.md) | Foundation; required before browser access beyond bootstrap and all private data | Exactly one first owner, secure sessions/recovery, roles, and expiring single-use verified-email invitations with cross-user isolation. |
| Ops-B | Self-provisioned signing key | Shipped | [`009-self-provisioned-keys`](specs/009-self-provisioned-keys/spec.md) and [plan](specs/009-self-provisioned-keys/plan.md) | Security-A | A production deployment needs only `DATABASE_URL`; the signing key is provisioned into the database so a backup restores every session, while `EXTERNAL_CREDENTIAL_KEY` stays outside the data it protects. |
| Ops-C | Owner-correctable configuration | Shipped | [`010-setup-error-clarity`](specs/010-setup-error-clarity/spec.md), [`011-integration-settings`](specs/011-integration-settings/spec.md) | Security-A | Setup names every field to fix and verifies SMTP before storing it; the owner can then see, check and change provider configuration, so an expired key no longer means a new database. |
| Experience-B | One component library everywhere | Shipped | [`012-primevue-consistency`](specs/012-primevue-consistency/spec.md) | Security-A | Every screen uses PrimeVue rather than hand-rolled controls, enforced by a test. |
| Experience-A | Installable PWA and device lifecycle | Backlog | Not yet specified | Security-A; SSE foundation | Chrome/Edge installability on mobile/tablet/desktop, offline/stale behavior, devices, and permission lifecycle. |
| 2 | Instrument exploration and financial charts | Shipped | [`005-instrument-exploration`](specs/005-instrument-exploration/spec.md) | Milestone 1 | Search/browse, instrument detail, responsive candlestick/volume history, overlays, and basic statistics. |
| 3 | Reusable feature engine | Backlog | Not yet specified | Milestone 1; benchmark/FX inputs as needed | Deterministic timestamped returns, trend, momentum, relative strength, volatility, ATR, RSI/MACD, drawdown, volume, and regime features with leakage tests. |
| 4 | Deterministic strategies and signals | Backlog | Not yet specified | Milestone 3 | Versioned momentum/trend strategy, parameters, immutable actions/scores/confidence/explanations and reproducibility. |
| 5 | Reproducible backtesting | Backlog | Not yet specified | Milestone 4; benchmark data | Historical simulation, accounting, brokerage/FX/slippage, benchmarks, metrics, curves, and traceable trades/signals. |
| 6 | Personal tracking, portfolio, and risk engine | Backlog | Not yet specified | Security-A; Milestone 4; preferably Milestone 5 evidence | User-owned holdings/trades/tracking rules, positions, cash/P&L/exposures, independent limits/rejections/modifications, and order intents. |
| 7 | Paper trading | Backlog | Not yet specified | Milestones 1, 4, and 6 | Permanent simulated orders/trades and forward performance using shared strategy/risk/accounting. |
| Notifications-A | Email and Web Push alerts | Backlog | Not yet specified | Security-A; Experience-A; explainable signals and user tracking | Granular consented alerts, quiet/frequency controls, per-device revocation, minimum private payloads, and provider-outage resilience. |
| 8 | Advanced analysis | Deferred | Not yet specified | Trusted Milestones 1–7 | Hourly data, more markets/strategies, comparisons, richer costs, fundamentals, news, notifications. |
| 9 | Machine-learning experimentation | Deferred | Not yet specified | Trusted deterministic platform | Time-aware research and out-of-sample classification/ranking compared with deterministic baselines. |
| Future | Broker execution | Deferred / V1 non-goal | Separate future project/spec | Authentication, risk, paper validation, broker capability | No automated Danske Bank or real-money execution in V1. |

## Current focus

Everything specified so far is shipped and running: `v0.5.0` carries features 002, 003, 004
and 009 through 012. Production needs only `DATABASE_URL` plus the credential key it must
retain, and the owner can correct provider configuration from the browser.

The next product feature is **Milestone 2, instrument exploration and financial charts**. Its
specification, plan, research, data model and contracts are written and reviewed on the
`005-instrument-exploration` branch; task breakdown and implementation remain. One decision is
still open and belongs in the specification rather than the implementation: which charting
component is used. The project rule is that no custom UI component is built where the chosen
library provides one, so this must be settled before any chart code is written.

Market data is already delivered behind that boundary: every page, REST snapshot and SSE
stream requires an authenticated session, with cross-user isolation proven by test.

Milestones 2–5 are the next planning sequence. Create separate feature specs so their
acceptance criteria, data ownership, responsive behavior, and test-first proof can be
reviewed independently. Do not combine them into one implementation batch.

## Cross-cutting dependencies

- Authentication ships before browser access beyond bootstrap, browser import controls,
  or private portfolio/trading/tracking data are exposed.
- Every client-visible feature includes a versioned durable authorized SSE change
  contract; REST remains the initial snapshot path and polling is not primary.
- PWA and email/Web Push delivery ship only after identity, device ownership, consent,
  revocation, and cross-user notification/event isolation are specified and tested.
- Benchmark identity/history and FX requirements are specified before dependent tests.
- Corporate-action limitations discovered in Milestone 1 are resolved or disclosed
  before affected return/backtest calculations are declared trustworthy.
- Historical universe/delisting limitations remain visible until point-in-time data is
  available.
- The financial chart library is selected in Milestone 2 based on current maintenance,
  license, performance, responsive/dark behavior, TypeScript/Vue support, and overlays.

## Maintenance rule

When lifecycle, scope, dependencies, or delivered behavior change, update this roadmap
and `specs/README.md` together. When product direction changes, also update
`docs/product-vision.md`. Never use an ignored local Spec Kit pointer or chat transcript
as the only record.
