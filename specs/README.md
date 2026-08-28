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
| 002 | [Instruments and daily market data](002-instruments-market-data/spec.md) | In progress | [Complete](002-instruments-market-data/plan.md) | [In progress](002-instruments-market-data/tasks.md) | Foundation | Current product feature; REST snapshots plus durable resumable SSE are required for client-visible changes. |
| 003 | [Release versioning and protected delivery](003-release-versioning/spec.md) | Shipped | [Complete](003-release-versioning/plan.md) | [In progress](003-release-versioning/tasks.md) | Foundation | Shipped as immutable release `v0.2.0`; lifecycle record will merge with feature 002. |
| 004 | [Owner access and invitations](004-owner-access/spec.md) | Planned | Not generated | Not generated | Foundation | Required before browser access beyond bootstrap and before any private user data or authorized private SSE. |

Feature lifecycle values are `planned → in-progress → in-review → shipped`. Update the
spec and this registry together when lifecycle changes.

## Planned specification backlog

IDs are assigned only when `/speckit-specify` creates a feature; this order does not
reserve directory numbers.

| Order | Feature spec to create | Depends on | Scope retained for the future spec |
|---:|---|---|---|
| 1 | Instrument exploration and financial charts | 002 | Markets/search, instrument detail, daily candles/volume, ranges, overlays, latest-value semantics, responsive dense data, chart-library decision. |
| 2 | Reusable feature engine | 002 | Timestamped return/trend/momentum/relative-strength/volatility/ATR/RSI/MACD/drawdown/volume/regime features; missing data, determinism, leakage tests. |
| 3 | Deterministic strategies and signals | Feature engine | Strategy/config versioning, momentum/trend ranking, action/score/confidence, immutable explanations/snapshots, reproducibility, history. |
| 4 | Reproducible backtesting | Strategies/signals | Simulation, data snapshots, accounting, rebalancing, percentage/minimum brokerage, FX/spread/slippage, benchmarks, metrics/curves/trades/signals, leakage and survivorship disclosure. |
| 5 | Personal tracking, portfolio, and risk engine | Authentication; strategies; backtest evidence | User-owned holdings/trades/tracking rules, cash/positions/P&L/exposure, stock/sector/country/cash/drawdown/liquidity/volatility/trade-size rules, rejection/modification, order intents. |
| 6 | Paper trading | Market data, strategies, risk/portfolio | Virtual cash, latest signals, simulated orders/trades, permanent history, forward P&L, scheduling, shared-logic proof. |
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
