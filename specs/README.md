# Market Lens specification registry

This is the durable index of reviewed and anticipated feature specs. Product direction
is in [`docs/product-vision.md`](../docs/product-vision.md), and delivery order/status is
in [`ROADMAP.md`](../ROADMAP.md).

Roadmap entries are not implementation authorization. Meaningful production behavior
requires its own reviewed `spec.md`, acceptance criteria, and a valid failing automated
test before production code changes.

## Existing specifications

| ID | Feature | Lifecycle | Plan | Tasks | Dependencies | Notes |
|---|---|---|---|---|---|---|
| 001 | [k3s deployment](001-k3s-deployment/spec.md) | In progress | Not generated in current workflow | Not generated | Foundation | Optional deployment track, not a prerequisite for product milestones. |
| 002 | [Instruments and daily market data](002-instruments-market-data/spec.md) | Shipped | [Complete](002-instruments-market-data/plan.md) | [Complete](002-instruments-market-data/tasks.md) | Foundation | Shipped in `v0.3.0`, currently serving in `v0.5.0`. |
| 003 | [Release versioning and protected delivery](003-release-versioning/spec.md) | Shipped | [Complete](003-release-versioning/plan.md) | [Complete](003-release-versioning/tasks.md) | Foundation | Shipped in `v0.2.0`. Every release since has gone through it. |
| 004 | [Owner access and invitations](004-owner-access/spec.md) | Shipped | [Complete](004-owner-access/plan.md) | [Complete](004-owner-access/tasks.md) | Foundation | Shipped in `v0.4.0`. Protects every market page, REST snapshot, and SSE stream, with cross-user isolation proven. |
| 005 | [Instrument exploration and financial charts](005-instrument-exploration/spec.md) | Shipped | [Complete](005-instrument-exploration/plan.md) | [Complete](005-instrument-exploration/tasks.md) | 002, 004 | Shipped in `v0.6.0`. Browsable universe and daily candlestick/volume history with quality, actions, and gaps surfaced honestly. Added the missing `corporate_action.changed.v1` event, and fixed a shipped defect where the client subscribed to the SSE stream by the wrong event name and received nothing. |
| 009 | [Self-provisioned signing key](009-self-provisioned-keys/spec.md) | Shipped | [Complete](009-self-provisioned-keys/plan.md) | [Complete](009-self-provisioned-keys/tasks.md) | 004 | Shipped in `v0.5.0`. A deployment needs only `DATABASE_URL`; `EXTERNAL_CREDENTIAL_KEY` stays outside the database it protects. |
| 010 | [Actionable owner setup errors](010-setup-error-clarity/spec.md) | Shipped | In spec | In spec | 004 | Shipped in `v0.5.0`. Setup names every field to fix, and verifies SMTP before storing it. |
| 011 | [Owner integration settings](011-integration-settings/spec.md) | Shipped | In spec | In spec | 004, 010 | Shipped in `v0.5.0`. The owner can see, check, and change provider configuration; nothing is stored until it verifies. |
| 012 | [One component library everywhere](012-primevue-consistency/spec.md) | Shipped | In spec | In spec | 004, 010, 011 | Shipped in `v0.5.0`. Enforced by a test; corrected three latent accessibility defects in the theme. |
| 013 | [Reusable feature engine](013-feature-engine/spec.md) | Shipped | [Complete](013-feature-engine/plan.md) | [Complete](013-feature-engine/tasks.md) | 002, 004 | Deterministic, versioned, point-in-time features over stored sessions. Owns the definitions feature 005 displays. Relative strength is measured against an equal-weighted composite of the curated universe, decided 2026-08-31. |
| 014 | [Market data navigation, sector data, continuous listing](014-market-data-navigation/spec.md) | Planned | [Planned](014-market-data-navigation/plan.md) | Pending | 002, 005, 013 | Operational reporting moves off Market Data to its own screen and gains the feature engine's runs; the listing loads on scroll and states its size while keeping keyset pagination; sector becomes curated reference data carried by migration, because the deployment's market-data plan excludes fundamentals. |

Feature lifecycle values are `planned → in-progress → in-review → shipped`. Update the
spec and this registry together when lifecycle changes.

## Planned specification backlog

IDs are assigned only when `/speckit-specify` creates a feature; this order does not
reserve directory numbers.

| Order | Feature spec to create | Depends on | Scope retained for the future spec |
|---:|---|---|---|
| 1 | Reusable feature engine | 002 | Timestamped return/trend/momentum/relative-strength/volatility/ATR/RSI/MACD/drawdown/volume/regime features; missing data, determinism, leakage tests. |
| 2 | Deterministic strategies and signals | Feature engine | Strategy/config versioning, momentum/trend ranking, action/score/confidence, immutable explanations/snapshots, reproducibility, history. |
| 3 | Reproducible backtesting | Strategies/signals | Simulation, data snapshots, accounting, rebalancing, percentage/minimum brokerage, FX/spread/slippage, benchmarks, metrics/curves/trades/signals, leakage and survivorship disclosure. |
| 4 | Personal tracking, portfolio, and risk engine | Authentication; strategies; backtest evidence | User-owned holdings/trades/tracking rules, cash/positions/P&L/exposure, stock/sector/country/cash/drawdown/liquidity/volatility/trade-size rules, rejection/modification, order intents. |
| 5 | Paper trading | Market data, strategies, risk/portfolio | Virtual cash, latest signals, simulated orders/trades, permanent history, forward P&L, scheduling, shared-logic proof. |
| Cross-cutting | Installable PWA and devices | Authentication; SSE foundation | Chrome/Edge mobile/tablet/desktop installation, offline/stale UI, device ownership, push permission lifecycle, and revocation. |
| Cross-cutting | Email and Web Push notifications | Authentication; PWA/devices; explainable signals | Granular consent, quiet/frequency controls, per-device revocation, minimal payloads, provider outage handling, and links to authenticated explanations. |
| Later | Advanced analysis | Trusted core | Hourly data, more strategies/markets, comparisons, costs, fundamentals/news/notifications through separate abstractions. |
| Last | ML experimentation | Trusted deterministic platform | Time-aware export/splits/walk-forward validation, benchmark-relative classification/ranking, portable inference, baselines, leakage controls, no autonomous trading. |

Automatic broker execution is a V1 non-goal and requires a separate future project or
explicitly approved specification.

Every future milestone specification must define functional requirements, independently
testable user scenarios, domain entities, migration needs, API contracts, responsive
pages/components, background jobs, red-green proof, relevant automated suites, measurable
acceptance criteria, dependencies, and decisions that must be resolved before
implementation versus those safe to defer.

## Resuming work in a new session

Read in this order:

1. `AGENTS.md` and `.specify/memory/constitution.md`.
2. `docs/product-vision.md`.
3. `ROADMAP.md` and this registry.
4. The current feature's spec, plan, research, data model, contracts, checklist, and
   `tasks.md` when present.
5. `git status` and relevant code/tests to reconcile documentation with reality.

Then choose one workflow:

- Current feature planned without tasks: `/speckit-tasks`.
- Tasks exist: use `/speckit-analyze` when appropriate, then `/speckit-implement` or
  implement reviewed tasks test-first.
- Next roadmap feature: `/speckit-specify`, review/clarify, `/speckit-plan`, then
  `/speckit-tasks`.
- Missing requirements discovered during implementation: update/review the spec first.

`.specify/feature.json` is ignored local state. It may help Spec Kit in one checkout, but
the roadmap, registry, and committed feature artifacts are authoritative across sessions.
