# Tasks: Reusable Feature Engine

**Input**: Design documents from `/specs/013-feature-engine/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md),
[data-model.md](./data-model.md), [contracts/openapi.yaml](./contracts/openapi.yaml),
[quickstart.md](./quickstart.md)

**Tests**: MANDATORY. Every production-code task below is preceded by a test task, and each
test task includes *running* the test and recording that it failed for the stated
behavioral reason before any production code is written. A compile error, a missing
fixture, an unavailable database (`TEST_DATABASE_URL` unset makes every integration test
SKIP, silently — export it first), or an already-green test is **not** valid red evidence.

**The designated first red** is T016 — a PostgreSQL-backed readback test asserting that
`Repository.ReadAsOf` returns, for a fixture instrument and a stored session, a value or an
explicit absence for every published definition, each carrying its definition version. It
fails behaviorally: with the definitions seeded and nothing computed, the read returns zero
features and lists every name under `notComputed`. That is the red the spec and plan both
name, and it must be observed before T017 onward.

**Responsive UI**: No new surface. The existing Markets journeys in
`e2e/instrument-exploration.spec.ts` at 360x800, 768x1024, 1440x900 and the 320-pixel floor
must pass **unchanged** (T090). Their needing no edit is the evidence that this feature
changed where a number comes from and nothing else.

**Live Delivery**: One new event, `feature_values.changed.v1`, shared scope, inserted through
`clientevents.Insert` in the same transaction as the values it reports (T035–T036). The
client subscribes to it **by name** (T083) — a listener on `message` receives nothing.

**Identity**: No user-owned data. Isolation evidence is that every read requires an active
session, that an unknown and an unauthorized instrument identifier are indistinguishable
(T049), and that no HTTP route can trigger a computation at all (T053) — triggering is an
owner action performed on the pod, exactly as `marketdata backfill` is today.

**Three design amendments this task list makes**, each discovered while breaking the plan
into testable steps and each recorded back into the design documents by T001–T003:

1. The regime is a *name*, not a number. `feature_values` gains `label text NULL`, and the
   exactly-one-of check becomes exactly one of `value`, `label`, `absence_reason`.
2. A ratio with a zero denominator (a 20-session volume average of zero, say) has no value
   and is none of the three absence reasons. `zero_denominator` is added as a fourth.
3. `price_basis = adjusted` means **engine-applied** adjustment: raw closes divided by the
   ratio of every split whose ex-date lies inside the window *and on or before the session
   being computed*. It never reads the provider's `adjusted_close` column, which is
   back-adjusted for splits that had not happened yet at the session — the exact lookahead
   FR-019 forbids. The three statistics adopted from feature 005 stay `raw`, verbatim.

**Version-1 definitions** (the set T024–T033 implement; `W_max` = 250 sessions):

| Name | Window | Basis | Undefined when |
|---|---|---|---|
| `return_1`, `return_5`, `return_60`, `return_250` | 2, 6, 61, 251 | adjusted | insufficient history, gap, zero denominator |
| `return_20`, `return_90` | 21, 91 | **raw** (feature 005 verbatim) | as above |
| `log_return_1` | 2 | adjusted | as above |
| `sma_20`, `sma_50`, `sma_200` | 20, 50, 200 | adjusted | insufficient history, gap |
| `trend_50_200` = sma_50 / sma_200 − 1 | 200 | adjusted | as sma_200 |
| `momentum_20` = close / sma_20 − 1 | 20 | adjusted | as sma_20 |
| `volatility_20` = stddev_samp of 20 log returns × √252 | 21 | **raw** (feature 005 verbatim) | insufficient history, gap |
| `atr_14` = mean true range over 14 sessions | 15 | adjusted | insufficient history, gap |
| `rsi_14` — Wilder smoothing, SMA-seeded over a fixed 140-session window | 140 | adjusted | insufficient history, gap |
| `macd_12_26`, `macd_signal_9`, `macd_histogram` — SMA-seeded EMAs over a fixed 130-session window | 130 | adjusted | insufficient history, gap |
| `drawdown_250` = close / max(close over 250) − 1 | 250 | adjusted | insufficient history, gap |
| `volume_sma_20`, `volume_ratio_20` | 20 | n/a | insufficient history, gap; ratio also zero denominator |
| `relative_strength_20`, `relative_strength_90` = (1+r_instrument) / (1+r_composite) − 1 over the window | 21, 91 | adjusted | as return; `composite_undefined` when any composite session in the window is undefined |
| `regime` — label, precedence `volatile` → `trending_up` → `trending_down` → `range_bound` | 250 | derived | undefined when any input is undefined |
| `composite_return_1` (universe composite, own table) | 2 | adjusted | `insufficient_contributors` below 10 |

Regime v1 parameters, stored in `parameters` so a later change is a version:
`volatile` when `volatility_20 ≥ 0.40`; else `trending_up` when `trend_50_200 > 0.05` and
`drawdown_250 > −0.10`; else `trending_down` when `trend_50_200 < −0.05`; else `range_bound`.
RSI and MACD use *fixed* windows with SMA seeds rather than smoothing from the start of
history, because an unbounded recursion would make a bar revision at session S affect every
later session and defeat research R-004.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on incomplete work)
- **[Story]**: US1–US5 — maps to the user stories in spec.md

## Path Conventions

Web application, per plan.md: Go modular monolith under `server/internal/`, Vue client
under `src/`, Playwright journeys under `e2e/`. All paths are repository-relative.

---

## Phase 1: Setup

**Purpose**: Record the three design amendments where the plan's readers will look, and
register the new artifacts with the harnesses that reconcile them.

- [ ] T001 Amend `specs/013-feature-engine/data-model.md`: add `label text NULL` to `feature_values`, restate the check as exactly one of `value`, `label`, `absence_reason`; add `zero_denominator` to the absence-reason check; add a paragraph under `feature_definitions.price_basis` stating that `adjusted` is engine-applied from `corporate_actions` with ex-date on or before the session, never the provider's `adjusted_close`
- [ ] T002 [P] Amend `specs/013-feature-engine/contracts/openapi.yaml`: add `label: { type: [string, 'null'] }` to `FeatureValue` with `label` in its `required` list; add `zero_denominator` to the `absenceReason` enum; state in `priceBasis`'s description what `adjusted` means
- [ ] T003 [P] Amend `specs/013-feature-engine/research.md` R-007 with the v1 regime parameters and precedence order above, and add R-009 recording the fixed-window SMA-seeded choice for RSI and MACD and why (R-004)
- [ ] T004 [P] Register `../../../specs/013-feature-engine/contracts/openapi.yaml` in `contractPaths` in `server/internal/api/contract_test.go`, run `TestInstrumentReadContracts`-style reconciliation, and record that it now fails because the two new paths have no handler — this is a harness pre-condition, not the first red
- [ ] T005 [P] Update the expected count in `server/internal/db/migrate_test.go` from 16 to 19 and the last-migration assertion to `0019_markets_adopt_engine_statistics.sql`; run it and record the red (`len(migrations) == 16`)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The schema, the type declarations that let the first red assert on *values*,
and the fixture every story computes over.

**⚠️ CRITICAL**: No user story work begins until this phase is complete.

**Note on TDD**: T010 declares data shapes only — structs and an interface carrying no
computation, query or rounding behavior. Migrations are production code and follow their
own red (T006, T008).

- [ ] T006 Write `server/internal/db/feature_engine_migrations_test.go` with `TestFeatureEngineMigrationsCleanInstall` (all five tables exist with the stated primary keys, the exactly-one-of checks on `feature_values` and `universe_composites` reject a row with both a value and a reason and a row with neither, `absence_reason` rejects an unknown reason, `(name, version)` is unique on `feature_definitions`, the `(definition_id, session_date)` index exists) and `TestFeatureEngineMigrationsUpgradeVersionSixteen` (apply through 0016, insert a bar and an open finding, apply 0017–0019, assert nothing existing changed and every v1 definition from the table above is present with `superseded_at IS NULL`). Run with `TEST_DATABASE_URL` set; record the red: `relation "feature_definitions" does not exist`
- [ ] T007 Write `server/internal/db/migrations/0017_feature_definitions.sql`: `feature_definitions` per data-model.md, then the twenty-five v1 rows with stable literal UUIDs, `published_at` fixed to the migration's date, `parameters` carrying every constant named in the table above (windows, √252, RSI/MACD periods and seed rule, regime thresholds and precedence, composite minimum contributors), and `undefined_conditions` as prose. The three adopted definitions' `parameters` cite `specs/005-instrument-exploration` and `listing.go`'s CTE as their source
- [ ] T008 Write `server/internal/db/migrations/0018_feature_values.sql`: `feature_runs`, `feature_run_items`, `feature_values` (with `label`), `universe_composites`, every check and index from data-model.md as amended by T001, `feature_runs.trigger_run_id` referencing `import_runs(id)`
- [ ] T009 Write `server/internal/db/migrations/0019_markets_adopt_engine_statistics.sql` as a comment-only placeholder stating it is reserved for the index the adopted listing query needs (filled by T080 if measurement demands one); re-run T005 and T006 to green
- [ ] T010 [P] Declare the engine's types in `server/internal/features/model.go`: `Definition` (name, version, windowSessions, priceBasis, parameters, undefinedConditions, sessionLengthSensitive), `Value` (name, version, session, value `*string`, label `*string`, absenceReason, currency, computedAt, comparedTo), `AbsenceReason` constants (`insufficient_history`, `window_gap`, `composite_undefined`, `zero_denominator`), `FeatureSet` (instrumentID, sessionDate, values, notComputed), `Run`, `RunItem`, `RunKind`, `RunStatus`, and `ErrNoHistory`/`ErrUnknownFeature`/`ErrClosedDate` sentinels — declarations only, no behavior
- [ ] T011 [P] Build the shared PostgreSQL fixture in `server/internal/features/fixture_test.go`, applied on top of `testdb` migrations: one exchange calendar with open, half-day and closed sessions; instrument **A** with 320 stored sessions in EUR; **B** with exactly 20 stored sessions; **C** with zero bars; **D** with an interior gap of three open sessions with no bar; **E** with a 2-for-1 split (`ratio = 2`) at a known ex-date inside its history, in SEK; a universe `fixture-v1` containing A, B, D, E; and helpers `insertBar`, `reviseBar`, `extendHistory(instrument, throughSession)` so later suites extend rather than rebuild. Include one session where every instrument has a zero-volume bar
- [ ] T012 [P] Add a golden-value file `server/internal/features/testdata/golden_A.json` holding hand-checked values for instrument A at three sessions — one where every window is satisfied, one inside a warm-up, one right after the gap-free run resumes — computed independently (a spreadsheet or a short throwaway script kept out of the repository) so the engine's suite is not checking the engine against itself

**Checkpoint**: Schema migrated, types declared, fixture loads. No computation exists.

---

## Phase 3: User Story 1 — Compute the feature set over the stored universe (Priority: P1) 🎯 MVP

**Goal**: An owner runs `features compute --universe nordic-liquid-v1` and every instrument
has, for every stored session, a value or an explicit absence per definition, each carrying
its version and computation time — with no value on a closed date and no number invented.

**Independent Test**: Compute over the fixture universe, read back instrument A at a session
where every window is satisfied and instrument B anywhere, and confirm A carries a value per
definition and B carries `insufficient_history` for every windowed feature.

### Failing tests for User Story 1 (MANDATORY) ⚠️

- [ ] T013 [P] [US1] Write `server/internal/features/rounding_test.go`: half-to-even at twelve places on exact ties (`0.0000000000005` → `0.000000000000`, `0.0000000000015` → `0.000000000002`), a negative tie, a value already at twelve places unchanged, and a `float64` that is not exactly representable rounding to the nearest twelfth place. Record the red: `undefined: Round`
- [ ] T014 [P] [US1] Write `server/internal/features/window_test.go`: a window of N sessions ending at session t over a slice of stored sessions returns exactly N bars when all are present; returns `insufficient_history` when fewer than N stored sessions precede t; returns `window_gap` when an exchange-open session inside the window has no bar; a half day counts as one session; a closed date inside the span is not a gap; a session after the instrument's last stored bar yields nothing. Record the red
- [ ] T015 [P] [US1] Write `server/internal/features/adjustment_test.go`: instrument E's closes before its ex-date are divided by the ratio when the window ends on or after the ex-date, are **not** adjusted when the window ends before the ex-date, and a split recorded with an ex-date *after* the window's end has no effect. Record the red
- [ ] T016 [US1] **The designated first red.** Write `TestReadingAnInstrumentAsOfASessionReturnsEveryDefinedFeatureWithItsVersion` in `server/internal/features/repository_integration_test.go`: with the fixture loaded and definitions seeded by 0017, call `Repository.ReadAsOf(ctx, A, session)` and assert that for every row in `feature_definitions` with `superseded_at IS NULL` the result carries either a value/label or an absence reason, each with `definitionVersion == 1`, and that `notComputed` is empty. Run it. Record the red verbatim: the result carries **zero** features and `notComputed` names all twenty-four — nothing is computed or stored. This must be a behavioral failure on returned values; if it fails to compile, T010 is incomplete and the red is invalid
- [ ] T017 [P] [US1] Write `server/internal/features/returns_test.go`: `return_N` against `golden_A.json`; `return_20` and `return_90` on **raw** closes match, to the twelfth place, the numbers `TestListingReturnsLatestCloseChangeAndFreshness` in `server/internal/instruments/listing_test.go` asserts for the same closes; `log_return_1`; a prior close of zero yields `zero_denominator`
- [ ] T018 [P] [US1] Write `server/internal/features/trend_test.go`: `sma_20/50/200`, `trend_50_200`, `momentum_20` against golden values; a window of 20 with 19 sessions is `insufficient_history`
- [ ] T019 [P] [US1] Write `server/internal/features/volatility_test.go`: `volatility_20` reproduces, to the twelfth place, the value `listing.go`'s CTE produces for the same twenty-one closes (compute it in the test with the same `stddev_samp × sqrt(252)` formula over `math.Log` ratios in ascending session order — the *stated evaluation order*); exactly 21 sessions is defined and 20 is `insufficient_history`; `atr_14` against golden values including a session whose true range is governed by the prior close
- [ ] T020 [P] [US1] Write `server/internal/features/oscillators_test.go`: `rsi_14` against golden values for a monotonically rising series (100), a falling series (0) and a mixed one; `macd_12_26`, `macd_signal_9`, `macd_histogram` against golden values; a window one session short of 140 / 130 is `insufficient_history`; the value at session t computed over the fixed window equals the value at t computed over the same window with earlier history removed (the fixed window makes it independent of history start)
- [ ] T021 [P] [US1] Write `server/internal/features/drawdown_volume_test.go`: `drawdown_250` is `0` at a new peak and negative below it; `volume_sma_20`, `volume_ratio_20`; a zero-volume bar is an observation (ratio `0`), an average of zero is `zero_denominator`; a session with no bar inside the window is `window_gap`, not zero
- [ ] T022 [P] [US1] Write `server/internal/features/composite_test.go`: the composite for a session is the equal-weighted mean of `return_1` over every universe instrument with a bar on that session **and** the one before; an instrument missing either bar is excluded and `contributorCount` reflects it; below 10 contributors the composite is `insufficient_contributors` with the count still recorded; the composite is identical regardless of the order instruments are supplied
- [ ] T023 [P] [US1] Write `server/internal/features/relative_strength_test.go`: `relative_strength_20` = (1+r_A) / (1+r_composite) − 1 over the same 20 sessions against golden values; `composite_undefined` when any composite session in the window is undefined — and undefined rather than carried forward from the previous session (spec edge case)
- [ ] T024 [P] [US1] Write `server/internal/features/regime_test.go`: one input series per regime crossing each boundary exactly (`volatility_20 = 0.40` is volatile, `0.399999999999` is not; `trend_50_200 = 0.05` is not trending up, `0.050000000001` is), precedence when both `volatile` and `trending_up` conditions hold, and `absence_reason` propagated when any input is undefined (a regime computed from a missing input must not exist)

### Implementation for User Story 1

- [ ] T025 [US1] Implement `Round(float64) string` in `server/internal/features/rounding.go` returning the twelve-place decimal string (`math/big` or `strconv` — whichever the T013 cases prove exact); green T013
- [ ] T026 [US1] Implement session-counted windows and gap detection in `server/internal/features/window.go`: `Window(bars, sessions, end, n)` returning bars or an `AbsenceReason`, counting half days as sessions and ignoring closed dates; green T014
- [ ] T027 [US1] Implement engine-applied adjustment in `server/internal/features/adjustment.go`: `Adjusted(bars, actions, asOf)` dividing closes/highs/lows before each split's ex-date by its ratio, considering only actions with `ex_date <= asOf`; green T015
- [ ] T028 [US1] Implement the definition registry in `server/internal/features/definition.go`: load rows from `feature_definitions`, map each name to its compute function, expose `WMax()` and `Active()`; a definition in the table with no compute function is a startup error, and a compute function with no definition row is never called
- [ ] T029 [US1] Implement returns, log return and the raw-basis adopted returns in `server/internal/features/returns.go`; green T017
- [ ] T030 [P] [US1] Implement SMA, trend and momentum in `server/internal/features/trend.go`; green T018
- [ ] T031 [P] [US1] Implement volatility (stated order: ascending log ratios, sample standard deviation, × `math.Sqrt(252)`) and ATR in `server/internal/features/volatility.go`; green T019
- [ ] T032 [P] [US1] Implement RSI and MACD with fixed-window SMA-seeded smoothing in `server/internal/features/oscillators.go`; green T020
- [ ] T033 [P] [US1] Implement drawdown and the volume features in `server/internal/features/drawdown.go` and `server/internal/features/volume.go`; green T021
- [ ] T034 [US1] Implement the composite in `server/internal/features/composite.go` (stage one of every run, sorted by instrument id before averaging so order cannot matter), relative strength in `server/internal/features/relative_strength.go`, and the regime in `server/internal/features/regime.go` reading thresholds from `parameters`; green T022–T024
- [ ] T035 [US1] Write `server/internal/features/repository_integration_test.go` additions: `TestAnInstrumentsValuesCommitAsOneTransactionWithItsEvent` (after `WriteInstrument`, `feature_values` rows, the `feature_run_items` row and exactly one `client_events` row with `event_type = 'feature_values.changed.v1'`, `scope = 'shared'`, payload `instrument_id`, `from_session`, `to_session`, `run_id` all exist; a forced failure after the values are staged leaves none of the three), and `TestTwoRecomputationsOfOneInstrumentSerialise` (the second `BeginInstrumentScope` blocks on the advisory lock until the first commits). Record the red
- [ ] T036 [US1] Implement `server/internal/features/repository.go`: `NewRepository`, `Definitions`, `BeginInstrumentScope` (transaction + `pg_advisory_xact_lock(hashtextextended('features:'||instrument_id, 0))`, mirroring `marketdata.BeginImportScope`), `Scope.WriteValues` (delete-then-insert over the affected session range, `label` for the regime, `run_id` on every row), `Scope.Commit` inserting the event through `clientevents.Insert` in the same `tx`, `WriteComposite`, `CreateRun`/`FinishRun`/`WriteItem`, and `ReadAsOf`; green T016 and T035
- [ ] T037 [US1] Write `server/internal/features/service_integration_test.go`: `TestAFullRunComputesEveryInstrumentInTheUniverse` (A has a value for every definition where its window is satisfied and each carries version 1; B has `insufficient_history` for every windowed definition; **C produces no rows and no error**; D has `window_gap` for exactly the sessions whose windows span the gap and values again once a full window exists after it; E's adjusted-basis features differ from raw across its ex-date and its currency reads `SEK`; the run finishes `succeeded` with `instrument_count = 4`), `TestNoValueExistsForAClosedDate`, `TestAHalfDayCountsAsOneSession` (a 20-window spanning a half day is satisfied by 20 sessions, and `volatility_20`'s definition reports `sessionLengthSensitive = true`), and `TestTheCompositeIsFinishedBeforeAnyInstrumentBegins`. Record the red
- [ ] T038 [US1] Implement `server/internal/features/service.go`: `Service.Compute(ctx, ComputeRequest{Kind, Universe, Workers, AppVersion})` — create the run, stage one composite over the whole universe, stage two per instrument (each in its own scope; a failure records the item as `failed` with `error_code`/`error_summary` and continues), finish the run as `succeeded`/`partial`/`failed`, `slog` one line per item and one per run; green T037
- [ ] T039 [US1] Write `TestFullRecomputationProducesZeroDifferences` in `server/internal/features/determinism_integration_test.go`: compute the fixture universe, copy `feature_values` and `universe_composites` to temp tables, compute again with `Workers = 4` and a shuffled instrument order, and assert the SC-001 and composite diff queries from quickstart.md both return 0. Record the red only if T038's implementation is non-deterministic; an immediate green here is acceptable and must be recorded as such, because the behavior is already forced by T035–T038
- [ ] T040 [US1] Write `TestFeaturesComputeReportsTheRunLikeMarketDataDoes` in `server/cmd/market-lens/main_test.go`: `features compute --universe fixture-v1` prints `run_id=… status=succeeded instruments=4 values=N` and exits 0; `features compute` with no universe defaults to `nordic-liquid-v1`; an unknown subcommand prints usage and exits 2. Record the red (`unknown command "features"`)
- [ ] T041 [US1] Add the `features` command to `server/cmd/market-lens/main.go`: `parseFeaturesCommand`, `executeFeaturesCommand`, wired to `features.Service` with the pool from `config`; green T040
- [ ] T042 [US1] Write `TestAFullFixtureComputationStaysWithinItsScaledBudget` in `server/internal/features/budget_integration_test.go`: the fixture (~700 bars) computes within 3 seconds, which scales to the 10-minute production budget with ~30× headroom; record the elapsed figure in the test's log output so the production figure recorded in T094 has something to compare to

**Checkpoint**: A stored, versioned, deterministic feature history exists and is
operable from the CLI. Nothing reads it over HTTP yet.

---

## Phase 4: User Story 2 — Read one instrument's features as of a session (Priority: P1)

**Goal**: `GET /api/v1/instruments/{id}/features?asOf=…&feature=…` and
`GET /api/v1/feature-definitions` per the contract, behind the existing session boundary.

**Independent Test**: Read A twice as of one session and confirm identical bodies; read A as
of an earlier session and confirm the values differ and `sessionDate` is the earlier one.

### Failing tests for User Story 2 (MANDATORY) ⚠️

- [ ] T043 [P] [US2] Write `TestReadAsOfIsIdenticalOnRepeatAndReflectsOnlyEarlierData` in `server/internal/features/repository_integration_test.go`: two reads of (A, s) are `reflect.DeepEqual`; a read of (A, s−40) differs and `return_20` at s−40 equals the golden value for that session, not s. Record the red
- [ ] T044 [P] [US2] Write `TestReadAsOfBeforeFirstStoredSessionIsNoHistoryNotEmptyValues` (returns `ErrNoHistory`, not a set with all absences) and `TestReadAsOfAClosedDateIsRefused` (`ErrClosedDate`) and `TestReadAsOfDefaultsToTheLatestStoredSession` in the same file. Record the red
- [ ] T045 [P] [US2] Write `TestReadAsOfAnUnknownFeatureNamesTheOnesThatExist` (`ErrUnknownFeature` carrying the sorted list of active names) and `TestCurrencyDenominatedFeaturesStateTheirCurrency` (E's `atr_14` and `sma_*` carry `SEK`, A's carry `EUR`, `return_*`/`rsi_14`/`regime` carry none) in the same file. Record the red
- [ ] T046 [P] [US2] Write `server/internal/api/features_test.go`: `TestInstrumentFeaturesEndpointReturnsTheContractsShape` (every `FeatureValue` field present, `value` is a **string** or null, `label` present, `absenceReason` non-null exactly when `value` and `label` are null, `comparedTo` present only on relative-strength values with `composite = "universe_equal_weighted"` and `contributorCount`), `TestInstrumentFeaturesEndpointHonoursAsOfAndFeatureFilters`, `TestInstrumentFeaturesEndpointRefusesAnUnknownFeatureWithTheKnownList` (400, `error.knownFeatures`), `TestInstrumentFeaturesEndpointRefusesAClosedDate` (400), `TestInstrumentFeaturesEndpointReportsNoHistoryAsNotFound` (404 with a message saying there is no history). Record the red (404 from the router for every case)
- [ ] T047 [P] [US2] Write `TestFeatureDefinitionsEndpointListsEveryVersionIncludingSuperseded` and `TestFeatureDefinitionsEndpointFiltersByName` in `server/internal/api/features_test.go`; insert a superseded v1 and a v2 of one definition directly in the test to prove `includeSuperseded=false` hides the former. Record the red
- [ ] T048 [P] [US2] Write `TestFeatureEndpointsRequireAnActiveSession` (401 for both routes with no cookie; 401 after the session is revoked; 401 after the user is deactivated — reuse the helpers `TestHistoryEndpointRequiresAnActiveSession` uses) in `server/internal/api/features_test.go`. Record the red
- [ ] T049 [P] [US2] Write `TestFeaturesEndpointAnswersUnknownAndUnauthorizedIdentically` in `server/internal/api/features_test.go` modelled on `TestHistoryEndpointAnswersUnknownAndUnauthorizedIdentically`: a random UUID and a malformed id yield byte-identical 404 bodies and identical headers. Record the red
- [ ] T050 [P] [US2] Write `TestReadingOneInstrumentsFeaturesStaysWithinTheListingsBudget` in `server/internal/features/budget_integration_test.go`: after a full fixture computation, `ReadAsOf` for A returns within the 2-second budget `TestTheFirstPageOfTheUniverseStaysWithinItsBudget` already enforces (SC-007). Immediate green is acceptable and must be recorded

### Implementation for User Story 2

- [ ] T051 [US2] Extend `ReadAsOf` in `server/internal/features/repository.go`: default `asOf` to the latest stored bar, refuse a date with no `exchange_sessions` row or `status = 'closed'`, return `ErrNoHistory` when no bar exists on or before `asOf`, accept a feature-name filter and return `ErrUnknownFeature` with the active names, attach currency from the bar for currency-denominated definitions and `comparedTo` from `universe_composites` for relative strength; green T043–T045
- [ ] T052 [US2] Add `ListDefinitions(ctx, name string, includeSuperseded bool)` to `server/internal/features/repository.go`; green the repository half of T047
- [ ] T053 [US2] Write `TestNoHTTPRouteTriggersAComputation` in `server/internal/api/router_test.go`: `POST`/`PUT` to `/api/v1/features/compute`, `/api/v1/feature-runs` and `/api/v1/instruments/{id}/features` as owner return 404 or 405 and create no `feature_runs` row. Immediate green is expected — record it — and the test stays as the guard the spec's security evidence asks for
- [ ] T054 [US2] Implement the two thin handlers in `server/internal/api/features.go` (parse, call the repository, map `ErrNoHistory` → 404 using the same not-found body the history endpoint uses, `ErrClosedDate`/`ErrUnknownFeature` → 400 via `httpx`, serialise `value` as a decimal string) and register them in `server/internal/api/router.go` behind the authenticated group; green T046–T049 and T004's contract reconciliation

**Checkpoint**: The store is readable point-in-time over REST, with the same boundary the
rest of the API has.

---

## Phase 5: User Story 3 — Trust that no feature saw the future (Priority: P2)

**Goal**: The leakage suite. Deliberately after computation so there is something to test,
deliberately before adoption so nothing consumes a value not yet proven honest.

**Independent Test**: Compute over history truncated at session N, snapshot, extend with
N+1…N+60 using the fixture's `extendHistory`, recompute, and assert zero changes ≤ N.

### Failing tests for User Story 3 (MANDATORY) ⚠️

- [x] T055 [P] [US3] Write `server/internal/features/leakage_integration_test.go`: `TestExtendingHistoryChangesNoEarlierValue` — load the fixture with A truncated 60 sessions early, compute, copy `feature_values` and `universe_composites` to temp tables, `extendHistory(A, full)`, recompute in full, run the SC-002 diff from quickstart.md at the truncation point and assert 0 for values **and** for composites. Run it; if it is green immediately, record that, and then **deliberately break it** by pointing one definition at the provider's `adjusted_close` column in a throwaway branch to prove the test can fail — record that observation too, because a leakage test that has never failed is the risk plan.md names
- [x] T056 [P] [US3] Write `TestNoDefinitionReadsABarAfterItsSession` in the same file: wrap the bar source in a double that records the maximum `session_date` requested per (instrument, session) and fails the test on the first read past the session; compute A and D in full. Record the outcome as in T055
- [x] T057 [P] [US3] Write `TestACorporateActionAffectsOnlySessionsOnOrAfterItsExDate` in the same file: compute E; snapshot; insert a second split for E with an ex-date **after** the last stored session; recompute; assert 0 differences. Then insert a split with an ex-date inside the history and assert that exactly the sessions whose adjusted windows span it changed and no session before the ex-date did. Record the red — the second half fails until T027's adjustment considers `asOf`, which it should already; if it is green, record why
- [x] T058 [P] [US3] Write `TestTheCompositeUsesOnlyBarsOnOrBeforeEachSession` in the same file: extend the universe with a new instrument whose history begins after N; recompute; composite rows ≤ N are unchanged, including `contributor_count`

### Implementation for User Story 3

- [x] T059 [US3] Fix whatever T055–T058 expose in `server/internal/features/*.go`. If they expose nothing, this task records that explicitly in `quickstart.md`'s verification section with the deliberate-break evidence from T055, and no production code changes

**Checkpoint**: Lookahead is proven absent by recomputation, not asserted.

---

## Phase 6: User Story 4 — Recompute when a definition or the data changes (Priority: P2)

**Goal**: A revised bar recomputes exactly the sessions whose windows include it; a
superseded definition leaves its old values readable and labelled; a failure partway
leaves the previous values in force.

**Independent Test**: Revise one of A's bars at session S, run the incremental pass, and
count changed values — exactly the sessions `[S, S + W_max − 1]` in stored sessions, and no
other instrument.

### Failing tests for User Story 4 (MANDATORY) ⚠️

- [ ] T060 [P] [US4] Write `server/internal/features/incremental_test.go` (unit): `AffectedRange(sessions, S, wMax)` returns `[S, S + wMax − 1]` counted in **stored sessions**, is clipped at the last stored session, and treats a closed date between two sessions as no session. Record the red
- [ ] T061 [P] [US4] Write `server/internal/features/recompute_integration_test.go`: `TestRevisingOneBarRecomputesExactlyItsWindows` — compute; snapshot; `reviseBar(A, S)`; `Service.Compute(Kind: incremental, SinceRun: <the import run that revised it>)`; assert the set of changed `(instrument, session, definition)` rows is a subset of A × `[S, S+249]` and that at least `return_1` at S+1 changed (SC-008 measured as a count); assert D, E and the composite rows outside `[S, S+249]` are untouched, and that composite rows *inside* the range **did** recompute (a revised bar changes the composite too). Record the red (`incremental` is not a run kind yet)
- [ ] T062 [P] [US4] Write `TestASupersededDefinitionLeavesItsValuesReadableAndLabelled` in the same file: insert `rsi_14` v2 (changed period) and set v1's `superseded_at`; `Service.Compute(Kind: definition, Name: "rsi_14")`; assert v2 rows exist for every session, v1 rows still exist with version 1, `ReadAsOf` returns v2 by default, and `ListDefinitions` explains both. Record the red
- [ ] T063 [P] [US4] Write `TestAFailedInstrumentKeepsItsPreviousValues` in the same file: compute; inject a failure (a compute function that panics for instrument D at one session); recompute in full; assert D's rows are byte-identical to before, D's `feature_run_items` row is `failed` with an `error_code`, A and E are recomputed, and the run is `partial`. Record the red
- [ ] T064 [P] [US4] Write `TestAnImportTriggersTheIncrementalPass` in `server/internal/scheduler/marketdata_test.go`: after `RunDue` imports one session, the `Features` collaborator is called once with that import's run id; when the import fails it is not called. Record the red (`MarketData` has no `Features` collaborator)
- [ ] T065 [P] [US4] Write `TestFeaturesComputeAcceptsSinceRunAndDefinition` in `server/cmd/market-lens/main_test.go`: `features compute --since-run <uuid>` and `features compute --definition rsi_14` parse to the right kinds, `--since-run` with a malformed id exits 2. Record the red
- [ ] T066 [P] [US4] Write `TestAnIncrementalPassStaysWithinItsScaledBudget` in `server/internal/features/budget_integration_test.go`: one revised session over the fixture recomputes within 300 ms (scales to the 30-second production budget). Record the red or the immediate green

### Implementation for User Story 4

- [ ] T067 [US4] Implement `AffectedRange` in `server/internal/features/incremental.go` and `Repository.SessionsTouchedByRun(ctx, importRunID)` (from `daily_price_bars.import_run_id` and `price_bar_revisions.superseding_run_id`) in `server/internal/features/repository.go`; green T060
- [ ] T068 [US4] Extend `Service.Compute` in `server/internal/features/service.go` with `RunKind` `incremental` (scope each touched instrument to its affected range, recompute the composite for the union of ranges first) and `definition` (one definition, full history, every instrument), recording `trigger_run_id`; green T061–T062
- [ ] T069 [US4] Make per-instrument failure containment explicit in `server/internal/features/service.go`: recover a panic inside one instrument's scope, roll the scope back, record the item, continue; green T063
- [ ] T070 [US4] Add a `Features` collaborator to `server/internal/scheduler/marketdata.go` (an interface with `ComputeSinceRun(ctx, runID)`), call it after a successful `Import`, log and swallow its error so a feature failure never marks the import failed; wire it in `server/cmd/market-lens/main.go` for both the scheduler and the `marketdata backfill`/`update` commands; green T064
- [ ] T071 [US4] Add `--since-run` and `--definition` to `parseFeaturesCommand` in `server/cmd/market-lens/main.go`; green T065–T066

**Checkpoint**: The store keeps itself current and honest under data revision, definition
change and failure.

---

## Phase 7: User Story 5 — The Markets table shows the engine's numbers (Priority: P3)

**Goal**: `return_20`, `return_90` and `volatility` on the Markets list are read from
`feature_values` at each instrument's latest stored session; the derived CTE is deleted;
an instrument without engine values shows the three as absent.

**Release gate**: This phase ships in a **separate release** after Phases 3–6 have run in
production and `features compute --universe nordic-liquid-v1` has succeeded there (T092).
Shipping it in the same release would blank three columns for every user until the first
computation finished — the risk plan.md's R-006 accepts only because adoption lands once
values exist.

**Independent Test**: With engine values present for A and absent for a freshly inserted
instrument F that has 300 bars, the listing shows A's three statistics equal to
`feature_values` and F's as null.

### Failing tests for User Story 5 (MANDATORY) ⚠️

- [ ] T072 [P] [US5] Write `TestListingStatisticsComeFromTheEngineNotTheQuery` in `server/internal/instruments/listing_test.go`: an instrument with 300 stored bars and **no** `feature_values` rows lists `Return20`, `Return90` and `Volatility` as nil. Record the red: the CTE computes a number for it
- [ ] T073 [P] [US5] Write `TestListingStatisticsEqualTheEnginesStoredValues` in the same file: seed `feature_values` for the instrument at its latest session with three known `numeric(24,12)` values (one of them an absence with `insufficient_history`); the listing shows the two values exactly (compare as decimal strings, not `float64`) and the third as nil. Record the red
- [ ] T074 [P] [US5] Write `TestMarketsAgreesWithTheEngineForEveryInstrument` in `server/internal/features/adoption_integration_test.go`: full fixture computation, then walk the listing page by page and assert each row's three statistics equal `ReadAsOf(row, latest)` (SC-010). Record the red
- [ ] T075 [P] [US5] Write `TestUpgradeLeavesTheMarketsStatisticsReadableUntilEngineValuesExist` in `server/internal/db/feature_engine_migrations_test.go`: apply through 0019 with no `feature_values`; the listing query in `listing.go` runs without error and returns the instrument with the three statistics nil, and `return_20` sorting still orders nulls last. Record the red or the immediate green
- [ ] T076 [P] [US5] Write `TestSortingByAnAdoptedStatisticUsesTheEngineColumn` in `server/internal/instruments/listing_test.go`: sort by `return_20` orders by `feature_values.value`, nulls last, keyset pagination still neither repeats nor skips (reuse the shape of `TestListingPagesWithoutRepeatingOrSkippingARow`). Record the red
- [ ] T077 [P] [US5] Write `TestAFeatureChangeRefreshesOnlyTheAffectedRow` in `src/views/MarketsView.test.ts`: dispatch a `feature_values.changed.v1` event (by name) for a listed instrument and assert one row re-read with filters, sort, page and scroll untouched; for an instrument outside the current filters, no request; the same event id twice, one request. Record the red (`feature_values.changed.v1` is not in `MARKET_DATA_EVENT_TYPES`, so the double dispatches into nothing)
- [ ] T078 [P] [US5] Write `TestFeatureValuesEventsAreSharedScopeAndInvisibleUnauthenticated` in `server/internal/events/isolation_integration_test.go`: a `feature_values.changed.v1` row is replayed to a member and to the owner, is not replayed to a deactivated user, and an unauthenticated stream request is refused before any replay (reuse the harness of `TestAuthorizedEventReplayReturnsSharedAndOnlyPermittedAccountScopes`). Record the red or immediate green
- [ ] T079 [P] [US5] Write `TestReconnectionReplaysOnlyTheMissedFeatureEvents` in `src/services/marketData.test.ts`: after a drop, the reconnect carries `Last-Event-ID` and only later `feature_values.changed.v1` events are applied. Record the red or immediate green

### Implementation for User Story 5

- [ ] T080 [US5] Rewrite the statistics source in `server/internal/instruments/listing.go`: **delete** the `recent`, `log_returns`, `volatility` CTEs and the `close_20`/`close_90` aggregates; join `feature_values` for the three definitions at `latest_session` (definition ids resolved by `(name, version)` where `superseded_at IS NULL`); keep `change_absolute`, `change_percent`, `latest_close`, `sessions_behind` derived as before; update `sortColumns` to the joined columns; measure `TestTheFirstPageOfTheUniverseStaysWithinItsBudget` and, only if it needs one, add the index to `0019_markets_adopt_engine_statistics.sql`; green T072–T076
- [ ] T081 [US5] Serialise the three adopted statistics as decimal strings end to end: confirm `ListingRow` (`server/internal/instruments/model.go`) and `InstrumentListingRow` (`src/types/marketData.ts`) already carry them as strings or nullable strings; if either is `float64`/`number`, change it with a red Vitest in `src/components/finance/InstrumentTable.test.ts` first, so no value is re-rounded through a JavaScript number
- [ ] T082 [US5] Record the adoption in `specs/013-feature-engine/quickstart.md` under a new *Adopted definitions* heading: the three names, their versions, the statement that no definition changed, and where the previous CTE lived (US5-3)
- [ ] T083 [US5] Add `'feature_values.changed.v1'` to `MARKET_DATA_EVENT_TYPES` in `src/services/marketData.ts` and, if `applyLiveChange` in `src/views/MarketsView.vue` needs an `entity_type` branch, add it; green T077 and T079
- [ ] T084 [US5] Green T078 — expected to need no production change; if it does, the fix is in `server/internal/events/repository.go`'s scope handling and must be recorded

**Checkpoint**: Every consumer of the three statistics now agrees on their definition, and
the number a person sees did not move.

---

## Phase 8: Polish & Cross-Cutting Concerns

- [ ] T085 [P] Write `TestEveryPublishedDefinitionHasAUnitTestAtItsBoundaries` in `server/internal/features/definition_test.go`: for each active definition name, a test file under `server/internal/features/` references it — the guard that a future definition cannot be added without a boundary test
- [ ] T086 [P] Write `TestNoSurfaceCallsTheCompositeAnIndexOrABenchmark` in `server/internal/features/vocabulary_test.go`: grep `server/internal/features`, `server/internal/api/features.go`, `src/`, the migrations 0017–0019 and `specs/013-feature-engine/` for `\bindex\b` (outside SQL `CREATE INDEX`/`index` in Go identifiers) and `\bbenchmark\b`, and fail on any match in user-facing strings, definitions or documentation (SC-003a)
- [ ] T087 [P] Write `TestEveryFeatureValueResolvesToAPublishedDefinition` and `TestNoFeatureValueExistsForAClosedOrGappedSession` in `server/internal/features/invariants_integration_test.go` running the SC-003 and SC-004 queries from quickstart.md over the computed fixture
- [ ] T088 [P] Write `TestAnUncomputableStatisticIsAbsentInEverySurface` in `server/internal/features/invariants_integration_test.go`: for instrument B, `ReadAsOf`, the API body and the listing row all carry null (never `"0"`, never `0`) for every windowed feature (SC-005)
- [ ] T089 Run `make verify` and fix anything it reports without weakening a test
- [ ] T090 Run `npm run test:e2e` across `mobile-chromium`, `tablet-chromium` and `desktop-chromium` and confirm `e2e/instrument-exploration.spec.ts` passes **with no edits**, including `never shows an uncomputable statistic as a zero move` and the 320-pixel and stacked-card journeys
- [ ] T091 Run `docker build -t market-lens:local .` and `docker compose config`
- [ ] T092 Ship Phases 1–6 as one PR from branch `013-feature-engine` (`feat(features): …`), wait for Keel to roll it, and run `features compute --universe nordic-liquid-v1` on the pod per quickstart.md
- [ ] T093 Run the five quickstart verification queries against production and record each result in `specs/013-feature-engine/quickstart.md` under *Recorded evidence*, with the date and app version
- [ ] T094 Record the production figures against R-008's budgets in `specs/013-feature-engine/research.md` (full computation elapsed, incremental pass elapsed after the next scheduled import, value-row count) — the budgets become measurements
- [ ] T095 Ship Phase 7 as a second PR (`feat(markets): read the three statistics from the feature engine`), confirm on production that a Markets row's three statistics equal `GET /instruments/{id}/features` for the same instrument (SC-010 by hand, as quickstart.md describes)
- [ ] T096 [P] Update `specs/013-feature-engine/spec.md` status to `shipped`, the 013 row in `specs/README.md`, `docs/roadmap.md` (the reusable-feature-engine milestone is done; the next milestone may now begin), and the SPECKIT block in `AGENTS.md`

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies; T002–T005 in parallel
- **Foundational (Phase 2)**: T006 → T007 → T008 → T009 strictly (each migration follows its red); T010–T012 in parallel with the migration chain
- **US1 (Phase 3)**: after Phase 2. T013–T015 and T017–T024 are all parallel reds; T016 is written after T010 and **observed red before anything in T025 onward is written**. Implementation order T025 → T026 → T027 → T028 → T029–T033 (parallel) → T034 → T035 → T036 → T037 → T038 → T039 → T040 → T041 → T042
- **US2 (Phase 4)**: after US1 (needs stored values to read). T043–T050 parallel reds; T051 → T052 → T053 → T054
- **US3 (Phase 5)**: after US1; independent of US2. T055–T058 parallel; T059 last
- **US4 (Phase 6)**: after US1 and US3 (nothing incremental should exist before leakage is proven). T060–T066 parallel reds; T067 → T068 → T069 → T070 → T071
- **US5 (Phase 7)**: after US2, US3 **and** US4, and after T092–T094 have run in production. T072–T079 parallel reds; T080 → T081 → T082 → T083 → T084
- **Polish (Phase 8)**: T085–T088 any time after US1; T089–T091 before each release; T092–T094 between the two releases; T095–T096 last

### Story Dependencies

- **US1** — foundation for all; MVP on its own (a stored feature history)
- **US2** — reads US1's store; no other dependency
- **US3** — proves US1's store; no dependency on US2
- **US4** — depends on US1 and on US3's proof
- **US5** — depends on all four, and on a production computation having completed

### Parallel Opportunities

- Phase 1: T002, T003, T004, T005 together
- Phase 2: T010, T011, T012 while T006–T009 proceed
- US1: eleven red tests at once (T013–T015, T017–T024); five definition implementations at once (T029–T033)
- US2: eight red tests at once (T043–T050)
- US3: four red tests at once (T055–T058)
- US4: seven red tests at once (T060–T066)
- US5: eight red tests at once (T072–T079)

---

## Parallel Example: User Story 1

```bash
# Reds, all at once, each observed failing for its stated reason before any implementation:
Task: "rounding_test.go — half-to-even at twelve places"
Task: "window_test.go — session-counted windows, gaps, half days"
Task: "adjustment_test.go — engine-applied split adjustment honours asOf"
Task: "returns_test.go / trend_test.go / volatility_test.go / oscillators_test.go /
       drawdown_volume_test.go / composite_test.go / relative_strength_test.go / regime_test.go"

# Then the designated first red, on its own, and record its output:
TEST_DATABASE_URL='postgres://market_lens:market_lens@127.0.0.1:5432/market_lens_test?sslmode=disable' \
  go test ./server/internal/features -run TestReadingAnInstrumentAsOfASessionReturnsEveryDefinedFeatureWithItsVersion -v

# Implementation, definitions in parallel once window/rounding/adjustment are green:
Task: "returns.go"  Task: "trend.go"  Task: "volatility.go"  Task: "oscillators.go"  Task: "drawdown.go + volume.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Phase 1 and Phase 2 — schema, types, fixture
2. Phase 3 — observe T016 red, then build to green through T042
3. **Stop and validate**: run T039's determinism test twice and T042's budget test; compute the real universe locally against a copy of production data if available
4. This alone is shippable: a stored, versioned feature history nothing yet reads

### Incremental Delivery

1. US1 → US2 → US3 → US4 in one release (`feat(features)`), computed in production, figures recorded (T092–T094)
2. US5 in a second release (`feat(markets)`), only once production holds values (T095)
3. Each release passes `make verify`, the unchanged Playwright journeys, and the Docker build

### What is deliberately not here

No `src/types/features.ts` and no client read of `/instruments/{id}/features`: plan.md
sketched one, but no client surface consumes the feature set in this feature, and a type
file with no consumer is dead code. The first client consumer — a later milestone — adds it
with its own red test. No strategy, signal, score, ranking or backtest task appears
anywhere above, and none may be added to this list.

---

## Notes

- [P] tasks = different files, no dependency on incomplete work
- Every red must be *run*; "it would fail" is not evidence. Paste the failing assertion into
  the commit message or quickstart.md's evidence section
- Integration tests SKIP silently without `TEST_DATABASE_URL`; a skipped suite is not green
- Never anchor an edit on a non-unique string; anchors must be unique in the file
- Commit after each green; the branch is `013-feature-engine`, and the PR title's prefix
  (`feat`) selects the minor version bump
