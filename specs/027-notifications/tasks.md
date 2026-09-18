# Tasks: Consented Email and Web Push Alerts

**Feature**: 027 | **Branch**: `027-notifications` | **Plan**: [plan.md](plan.md)

Tests come first throughout. Every production task names the test that must be observed failing —
on a value, not on a compilation error — before the code that satisfies it is written.

## Phase 1: Setup

- [x] T001 Write the reviewed specification, with the three owner decisions recorded
- [x] T002 Record the quality checklist, naming FR-021/022 as what a reviewer reads first
- [x] T003 Resolve the open questions in `research.md` (R-001..R-009)
- [x] T004 Describe five tables and seven absent columns in `data-model.md`
- [x] T005 Specify the operations and the public unsubscribe path in `contracts/openapi.yaml`
- [x] T006 Write `plan.md` with the constitution gate

## Phase 2: Foundational

- [x] T007 Write `server/internal/db/notifications_migration_test.go` — a clean install, the
      singleton push key, one preference per kind per channel, the sent/sent_at check read from
      both sides, and the seven forbidden columns absent
- [x] T008 Add `server/internal/db/migrations/0030_notifications.sql`
- [x] T009 Update the pinned schema version in `migrate_test.go` and
      `feature_engine_migrations_test.go`

## Phase 3: Web Push, against the specifications

- [x] T010 `TestEncryptionMatchesTheSpecification` — RFC 8291 §5's worked example, with the
      ephemeral key and salt supplied so the output is comparable at all
- [x] T011 `server/internal/notify/push/encrypt.go` — RFC 8291 and RFC 8188
- [x] T012 [P] `TestTheAssertionNamesTheServiceAndExpires` and
      `TestTheAssertionVerifiesAgainstThePublicKey`, then `vapid.go` — RFC 8292
- [x] T013 [P] `TestAGoneSubscriptionSaysSoRatherThanFailing`, then `sender.go`

## Phase 4: US1 — asked for it, and told once

**Goal**: nothing reaches somebody who did not ask, and what does reaches them once.

- [x] T014 [US1] `TestNothingIsSentToSomebodyWhoDidNotAskForIt` — the designated first red
- [x] T015 [US1] `model.go`, `preferences.go`, `repository.go`
- [x] T016 [US1] `raise.go` — the fan-out, on the caller's transaction
- [x] T017 [US1] `templates.go` — what each kind says
- [x] T018 [US1] `deliver.go` — claim, send, retry, abandon
- [x] T019 [P] [US1] `TestSomebodyWhoAskedIsToldOnce`
- [x] T020 [P] [US1] `TestTwoPassesAtOnceSendOnceBetweenThem` (SC-003)

## Phase 5: US2 — told quietly

- [x] T021 [US2] `quiet.go` with tests for a window across midnight and across a daylight-saving
      boundary — the case a stored UTC offset gets wrong for half the year
- [x] T022 [US2] `TestAQuietNotificationIsHeldAndThenDelivered` (SC-002)
- [x] T023 [P] [US2] `subscriptions.go`, including `EnsurePushKey` following migration 0011
- [x] T024 [P] [US2] `TestAPushGoesToEveryDeviceAndAForgottenOneRemovesItself` (SC-004)

## Phase 6: US3 — stopping, from anywhere

- [x] T025 [US3] `unsubscribe.go` — a signed, expiring, single-purpose token
- [x] T026 [US3] `TestAnUnsubscribeLinkStopsExactlyOneThing`
- [x] T027 [P] [US3] `TestAPersonSeesAndChangesOnlyTheirOwn` (SC-006)
- [x] T028 [P] [US3] `TestOneKindIsOfferedToTheOwnerAlone`

## Phase 7: US4 — the provider is down

- [x] T029 [US4] `TestAProviderOutageLosesNothing`
- [x] T030 [US4] `TestSomethingThatCannotBeDeliveredIsAbandonedNotLost`

## Phase 8: Honesty

- [x] T031 `TestNoMessageCarriesWhatSomebodyOwns` (SC-005), and the permitted-key schema in
      `raise.go` that refuses a figure at the source rather than in a template
- [x] T032 `TestASignalMessageStatesTheChangeAndNeverWhatToDo` (FR-021)
- [x] T033 Extend the advice-vocabulary guard to `notify/templates.go` (FR-022, SC-007)

## Phase 9: Transport, schedule and the raises

- [x] T034 `server/internal/api/notifications_test.go`, then `notifications.go` and the routes —
      including the one public route, registered on the root allow-list
- [x] T035 Register the 027 contract in `contract_test.go`
- [x] T036 `TestNotificationsAreDeliveredAfterAnImport`, then `MarketData.Notifications`
- [x] T037 `TestAFailedImportTellsWhoeverAskedToBeTold` and
      `TestAFailureToNotifyDoesNotReplaceTheImportError`, then `MarketData.Failures`
- [x] T038 `TestAFillTellsThePersonIfTheyAskedToBeTold`, then the raise inside the paper fill
- [x] T039 `TestASurveyCollapsesDecisionsAndNamesWhatChanged`, then `Survey` and
      `MarketData.Surveyor`
- [x] T040 The test send, `SendTestEmail`, and its route
- [x] T041 Wire everything in `cmd/market-lens/main.go`, including provisioning the push key on
      first start

## Phase 10: The screens

- [x] T042 `public/sw.js` — receive a push, show it, and nothing else
- [x] T043 `src/types/notifications.ts` and `src/services/notifications.ts`
- [x] T044 `NotificationSettings.test.ts`, then `NotificationSettings.vue`, mounted in Account
- [x] T045 `UnsubscribeView.vue` and its public route
- [x] T046 A test send beside the SMTP settings that already existed
- [x] T047 `e2e/notifications.spec.ts` at three viewports and the 320 floor

## Phase 11: Polish

- [ ] T048 `make verify`, the full Playwright suite, `docker build`, `docker compose config`
- [ ] T049 Update `specs/README.md`, `ROADMAP.md` and `AGENTS.md`
- [ ] T050 Ship, and record production evidence without seeding any data

## Dependencies

T007–T009 block everything. T010–T013 are independent of the database and can proceed alongside.
T014 blocks T015–T018. Phases 5–8 depend on Phase 4's service. Phase 9 depends on it too. Phase 10
depends on Phase 9's contract.

## Implementation strategy

US1 is the MVP and is also the rule the feature rests on: nothing reaches somebody who did not ask.
Phase 8 is not optional and ships in the same change — the owner chose to include signal-change
alerts knowing they edge toward advice, and the constraint on their wording is what makes that safe.
