# Tasks: Market-data fallback provider

Every task starts with a failing test run for its behavioural reason.

## Phase 1 — Tell the owner (FR-004a)
- [x] T001 scheduler: a run that ends `failed` raises the import-failure notice (red: no raise today)

## Phase 2 — Schema and provenance guards
- [x] T002 migration 0036 + migration test (settings, state, kind `fallback`, notice kind, yahoo mappings)
- [x] T003 upsertBar keeps a primary bar against a fallback candidate; primary replaces fallback with a revision (FR-006/007)
- [x] T004 import scope lock is shared across providers (FR-010a)
- [x] T005 `ImportFallback` kind requires a parent run and the fallback provider stores no corporate actions (FR-008a)

## Phase 3 — Yahoo client
- [x] T006 Daily maps bars, rounds to priceHint, dates in the exchange zone, adjusted = close
- [x] T007 Daily fails `fallback_action_in_window` on a split/dividend; 401/403/429/5xx classified; UA sent
- [x] T008 Resolve returns currency and exchange zone

## Phase 4 — Fallback service
- [x] T009 Cover: disabled → nothing; only auth-failed items; window after newest primary bar; child run
- [x] T010 Observe: entered/ended/pending transitions recorded once, event emitted, notice raised once
- [x] T011 ClassifyFallbackMapping verified/mismatched/unmapped/unverified
- [x] T012 Reconcile targets = instruments with fallback bars over their fallback span

## Phase 5 — Downstream
- [x] T013 paper: a fallback next bar does not fill and is not given up on (FR-026)
- [x] T014 backtest: fallback bars are not read (FR-027)
- [x] T015 scheduler wiring: Cover after an auth-failed run, features since the fallback run, Observe

## Phase 6 — Surface
- [x] T016 notify kind + wording + privacy guards
- [x] T017 API snapshot endpoint + event type allow-list
- [x] T018 CLI `marketdata fallback …`
- [x] T019 FallbackBanner (Vitest) + AppShell + settings copy
- [x] T020 Playwright banner at 360x800, 768x1024, 1440x900 and 320px tolerance

## Phase 7 — Verify and ship
- [ ] T021 make verify, e2e, docker build, compose config
- [ ] T022 PR, checks, squash-merge, release, confirm rollout, run the audit in production
