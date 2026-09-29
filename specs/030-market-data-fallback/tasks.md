# Tasks: Market-data fallback provider

Every task starts with a failing test run for its behavioural reason.

## Phase 1 — Tell the owner (FR-004a)
- [ ] T001 scheduler: a run that ends `failed` raises the import-failure notice (red: no raise today)

## Phase 2 — Schema and provenance guards
- [ ] T002 migration 0036 + migration test (settings, state, kind `fallback`, notice kind, yahoo mappings)
- [ ] T003 upsertBar keeps a primary bar against a fallback candidate; primary replaces fallback with a revision (FR-006/007)
- [ ] T004 import scope lock is shared across providers (FR-010a)
- [ ] T005 `ImportFallback` kind requires a parent run and the fallback provider stores no corporate actions (FR-008a)

## Phase 3 — Yahoo client
- [ ] T006 Daily maps bars, rounds to priceHint, dates in the exchange zone, adjusted = close
- [ ] T007 Daily fails `fallback_action_in_window` on a split/dividend; 401/403/429/5xx classified; UA sent
- [ ] T008 Resolve returns currency and exchange zone

## Phase 4 — Fallback service
- [ ] T009 Cover: disabled → nothing; only auth-failed items; window after newest primary bar; child run
- [ ] T010 Observe: entered/ended/pending transitions recorded once, event emitted, notice raised once
- [ ] T011 ClassifyFallbackMapping verified/mismatched/unmapped/unverified
- [ ] T012 Reconcile targets = instruments with fallback bars over their fallback span

## Phase 5 — Downstream
- [ ] T013 paper: a fallback next bar does not fill and is not given up on (FR-026)
- [ ] T014 backtest: fallback bars are not read (FR-027)
- [ ] T015 scheduler wiring: Cover after an auth-failed run, features since the fallback run, Observe

## Phase 6 — Surface
- [ ] T016 notify kind + wording + privacy guards
- [ ] T017 API snapshot endpoint + event type allow-list
- [ ] T018 CLI `marketdata fallback …`
- [ ] T019 FallbackBanner (Vitest) + AppShell + settings copy
- [ ] T020 Playwright banner at 360x800, 768x1024, 1440x900 and 320px tolerance

## Phase 7 — Verify and ship
- [ ] T021 make verify, e2e, docker build, compose config
- [ ] T022 PR, checks, squash-merge, release, confirm rollout, run the audit in production
