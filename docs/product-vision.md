# Market Lens product vision

- **Status**: Directional product baseline
- **Last reviewed**: 2026-08-28
- **Primary user**: One self-hosting investor initially
- **Primary deployment**: One Market Lens application image plus PostgreSQL
- **Initial market**: Liquid Swedish and Nordic equities purchasable through Danske Bank
- **Initial execution**: Research, backtesting, and paper trading

This is the durable product-level memory for Market Lens. It records intended scope,
architectural invariants, future milestones, and non-goals across development sessions.
It is not a reviewed feature specification and does not authorize production
implementation by itself. Every meaningful behavior change still requires a
specification under `specs/` with independently testable acceptance criteria.

The authoritative engineering rules remain `.specify/memory/constitution.md`. If this
vision conflicts with the constitution or a reviewed feature spec, resolve and document
the conflict rather than silently choosing one source.

## Product purpose

Market Lens is a disciplined, measurable environment for researching securities and
experimenting with investment strategies before risking capital. It should:

1. Maintain a trustworthy, expandable universe of instruments available to the user.
2. Collect historical/current market data with provenance and quality reporting.
3. Calculate reusable features without future-data leakage.
4. Evaluate versioned deterministic strategies and explain their recommendations.
5. Backtest honestly with realistic costs and passive benchmarks.
6. Run the same strategy, risk, and accounting logic through paper trading.
7. Track portfolio performance, exposures, drawdown, and decisions reproducibly.
8. Permit ML-assisted signals only after deterministic foundations are trusted.

The product does not promise profitable trades or certainty about future prices.
Profitability is an experimental result, not a software acceptance criterion.

## Non-negotiable decision pipeline

```text
Market data
    ↓
Reusable feature calculation
    ↓
Versioned strategy
    ↓
Immutable, explainable signal
    ↓
Independent risk evaluation
    ↓
Order intent
    ↓
Execution mode
    ├── historical backtest
    ├── paper trading
    ├── manual real-trade suggestion
    └── future broker execution (separate project/specification)
```

A strategy never executes a trade. Backtesting, live signals, paper trading, and future
execution must consume the same strategy behavior rather than fork implementations.

## Platform baseline

- Backend: Go 1.26 modular monolith, standard-library REST/JSON, pgx, explicit SQL,
  embedded ordered migrations, `slog`, and context-bound in-process jobs.
- Frontend: Vue 3, strict TypeScript, Vite, Vue Router, PrimeVue 4, and reusable
  project-level financial components.
- Storage: plain PostgreSQL, UTC event timestamps, explicit exchange time zones, and
  local trading-session dates. TimescaleDB waits for measured need.
- Production: compiled Vue assets served by Go; one application image plus PostgreSQL.
  Docker Compose remains the required portable deployment.
- `specs/001-k3s-deployment` is an optional target for the same image, not another
  product service or a Kubernetes requirement for users.
- No Kafka, RabbitMQ, Redis, Elasticsearch, microservices, or permanent production
  Python service without a later specification proving the need.

## Instruments and market data

Instrument identity never depends on ticker alone. It supports a stable internal ID,
ISIN, ticker, exchange/MIC, name, currency, country, sector, industry, type, exchange
time zone, lifecycle status, and provider identifiers. Renames, removals, and delistings
preserve historical identity.

The initial universe is 100 user-reviewed common-equity listings: 25 liquid large-cap
constituents from each of Stockholm, Copenhagen, Helsinki, and Oslo. Broker availability
is curated because Danske Bank integration is excluded. The model must later expand to
other Nordic, European, US, and Danske-supported markets without redesign.

Ingestion is provider-independent. A provider may supply instruments, daily/hourly bars,
quotes, actions, and calendars, but downstream domain, feature, strategy, and backtest
code cannot depend on provider payloads.

Initial history is approximately ten years of daily OHLCV where available. Hourly data
is deferred until daily strategies are validated. Raw values, provider adjustments,
provenance, corrections, missing sessions, and limitations remain visible. Splits,
reverse splits, dividends, symbol changes, and delistings must not silently corrupt
returns. FX history eventually supports SEK/USD, SEK/EUR, SEK/DKK, and SEK/NOK; SEK is
the initial portfolio base currency.

Quality detection includes missing/duplicate bars, impossible OHLC, non-positive prices,
negative or unexpected zero volume, out-of-order timestamps, provider gaps, suspicious
jumps, and corporate-action discontinuities. The system never invents observations to
hide defects.

Anticipated domain concepts include exchange, instrument, provider mapping, price bar,
corporate action, FX rate, benchmark, feature definition/snapshot, strategy/version/
parameter, signal/reason, portfolio/position, order intent, paper order, trade,
backtest/run/trade/snapshot, watchlist/membership, market-data import, and job execution.
Each feature specification decides the final ownership and schema; this list prevents
future capabilities from disappearing from the product backlog.

## Features and temporal correctness

Features are timestamped reusable facts independent of strategies. Initial candidates:

- returns over 1, 5, 20, 60, 90, and 200 trading days;
- SMA 20/50/100/200 and useful EMA variants;
- momentum and relative strength;
- volatility, average true range, and drawdown;
- volume trend and anomalies;
- RSI, MACD, and distance from moving averages;
- distance from the 52-week high;
- benchmark-relative and sector-relative returns;
- market-regime indicators.

At historical time T, calculations use only information genuinely available by T. This
applies to prices/actions, features, rankings, universe membership, strategies,
benchmarks, and ML datasets. Tests target look-ahead/data leakage, sessions/time zones,
missing data, and survivorship bias. Missing delisted or point-in-time universe data is
disclosed in results rather than hidden.

## Strategies and signals

Strategies and configuration are immutable versions. A strategy receives time,
instrument, features, market context, and optional portfolio context and emits a signal,
not an order. Initial actions are `BUY`, `HOLD`, `REDUCE`, `SELL`, and `WATCH`; a strategy
need not recommend a trade.

Each signal persists instrument, market timestamp, strategy/version, parameters, score,
action, confidence, relevant feature snapshot, reason contributions, and enough
application/data context to reproduce it. Historical signals never silently change.

The first strategy is deterministic multi-factor momentum/trend ranking using candidates
such as momentum, trend, relative strength, volume confirmation, volatility penalty,
market regime, and sector strength. Weights, liquidity rules, and thresholds are
versioned/configurable. Its purpose is validating the platform, not claiming optimality.

## Backtesting and benchmarks

A run identifies its immutable data snapshot, universe, strategy/version, parameters,
dates, capital, rebalance frequency, benchmark, costs, and portfolio/risk rules.
Identical inputs produce identical results.

Outputs include final value, total/annualized return, maximum drawdown, volatility,
Sharpe ratio where appropriate, trade count, meaningful win/loss statistics, average
holding period, turnover, transaction costs, benchmark/excess return, equity/drawdown
curves, trades, and associated signals.

Costs are never zero by assumption. Configuration supports percentage and minimum
brokerage fees, FX cost, and spread/slippage outside strategy logic. Every strategy is
compared with appropriate OMX Stockholm/Nordic and buy-and-hold baselines; broad global
benchmarks may follow.

## Risk, portfolio, and order intent

Signals pass through a separate risk engine. Initial controls include maximum stock,
sector, and country allocations; cash reserve; position count and portfolio drawdown;
liquidity/volatility limits; minimum trade size; and no blind averaging down. Risk may
reject or modify a recommendation and must explain why.

An order intent records instrument, action, allocation/quantity, strategy, signal,
rationale, and status. Execution mode decides whether it becomes a backtest trade, paper
order, manual suggestion, or future broker order.

Portfolio accounting tracks cash, positions, quantity, average cost, current price,
market value, realized/unrealized P&L, total return, allocation, and currency/sector/
country exposure in an initial SEK base currency.

## Paper and real-money trading

Paper trading uses current data and simulated cash—initially exemplified by 10,000 SEK—
through exactly the same feature, strategy, signal, risk, order-intent, and accounting
logic used by backtests/future execution. Paper signals and trades are immutable and
permanently recorded.

V1 never automatically trades through Danske Bank. It may show a suggested action,
allocation/quantity, approximate amount, and explanation for manual execution, and later
record that manual trade. Broker automation is a separate future project and spec.

## User experience

Planned views: Overview, Markets, Screener, Signals, Portfolio, Instrument Detail,
Backtests, Strategies, Watchlists, and Settings. Reusable PrimeVue-first components cover
headers, stats, filters, instrument search, signal/risk badges, money/percent formatting,
scores, charts, and empty/loading/error/confirmation states.

Expected view outcomes retained for future specifications:

- **Overview**: portfolio value, daily/total change, cash/invested amount, drawdown,
  positions, latest signals/opportunities/movers, performance and benchmark comparison,
  allocation, and recent data/system status.
- **Markets and Screener**: sortable, filterable, paginated, searchable, selectable
  columns for identity/exchange, price/change, 20/90-day return, volatility, relative
  strength, signal/score, sector/country, and freshness; selection opens the instrument.
- **Instrument Detail**: exchange-qualified identity, native price/change, sector/country,
  signal/score/confidence/explanation, financial and volume charts, overlays/indicators,
  recent features, signal history, and applicable backtest context.
- **Signals**: immutable time, instrument, strategy, action, score, confidence, price,
  reason summary, risk outcome, and paper-trade outcome.
- **Strategies**: versions, parameters, scoring rules, backtest launch, clone/new-version
  workflow, and performance comparison; no arbitrary embedded programming language in V1.
- **Backtests**: strategy/version, universe, capital, dates, benchmark, costs, risk, and
  rebalance configuration followed by metrics, curves, fees, trades, and linked signals.
- **Watchlists**: user-created named lists of instruments with optional notes, including
  examples such as Swedish Large Caps, Momentum Candidates, Possible Buys, and Owned
  Elsewhere.

Initial language is English, base currency SEK, with consistent formatting such as
`154 321,50 SEK`, `+4,21%`, and `-2,18%`. System, light, and dark themes are first-class.

General allocation/performance/P&L/drawdown/strategy charts may use Chart.js. Candles,
OHLC, volume, overlays, indicator panels, zoom/pan, and large series require a dedicated
financial library selected during its feature plan for maintenance, license, Vue/
TypeScript support, responsiveness, dark mode, performance, and avoidable lock-in.

Every UI spec defines mobile/tablet/desktop behavior and tests representative 360x800,
768x1024, and 1440x900 viewports plus usable 320 CSS-pixel width. Tables, charts,
dialogs, menus, and navigation get intentional small-screen treatment; nothing depends
on hover.

## Operations, security, and observability

REST/JSON remains under `/api/v1`, with validation, consistent errors, pagination, and
no database details. `/api/v1/health` is liveness and `/api/v1/ready` readiness.
Production serves `/api/*` from Go and falls back to embedded `index.html` for SPA routes.

In-process observable work includes instrument sync, daily and future hourly/FX imports,
features, signals, paper evaluation, and maintenance. Runs record job name, timing,
status, counts, and sanitized errors and stop with application context.

The app is private/self-hosted and should not be public by default. Credentials come
from secret mechanisms and never enter source, logs, images, build arguments, or browser
code. The optional public k3s deployment makes reviewed single-user authentication a
prerequisite for browser mutations and private portfolio/trading data. No custom crypto.

## Testing and release evidence

Backend coverage includes domain/unit, migration/repository integration, provider
contract, features/strategies, leakage, backtest determinism, risk, accounting, costs,
actions, and API behavior. Frontend coverage includes valuable components, API
integration, accessibility/responsiveness, and critical end-to-end flows.

All implementation is strict red-green-refactor. Relevant changes run `make verify`,
Playwright, production builds, Docker builds, and Compose validation proportionately.
Checks and tests are never weakened or disabled to obtain green.

## Machine-learning boundary

ML starts only after market data, deterministic signals, honest backtesting, costs, and
accounting are trusted. A suitable first target is the probability an instrument
outperforms its benchmark over the next N trading days (for example 20), not exact price.

Python research uses time-aware train/validation/test splits and walk-forward validation;
random future-leaking splits are prohibited. Production remains Go/PostgreSQL with a
portable model such as ONNX preferred over a Python service. ML is an additional signal,
never autonomous authority, and must beat deterministic baselines out of sample.

## Deferred areas and explicit V1 non-goals

Fundamentals, earnings, valuation, statements, estimates, news, sentiment, macro/sector
feeds, and email/push/messaging notifications enter through later provider abstractions
and specs.

V1 excludes high-frequency/intraday scalping; options; crypto; margin/leverage; short
selling; automatic Danske execution; social trading; multi-user SaaS; mandatory
Kubernetes; microservices/distributed messaging; LLM autonomous trades; deep neural
networks; and automatic investment of the user's primary savings.

## Product success checkpoint

The first end-to-end checkpoint uses about 100 Nordic equities, up to ten years of daily
history, a hypothetical 10,000 SEK portfolio, a deterministic strategy, realistic costs,
and a passive benchmark, then runs the same strategy in paper mode.

Success means reproducible results, explainable recommendations, no known look-ahead
leakage, correct accounting/costs, shared paper/backtest logic, and objective benchmark
comparison. Profit is not required.

## Durable planning hierarchy

When resuming work, read in order:

1. `.specify/memory/constitution.md` — binding engineering rules.
2. This document — long-term intent and boundaries.
3. `ROADMAP.md` — milestone status, dependencies, and recommended next work.
4. `specs/README.md` — feature registry and lifecycle status.
5. The selected feature's spec, plan, tasks, and supporting artifacts.
6. Tests and production code — implementation evidence, not substitutes for requirements.

When a reviewed feature changes direction, update this vision, roadmap, and registry in
the same change so future sessions never depend on conversation history.
