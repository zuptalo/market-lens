package features_test

import (
	"context"
	"log/slog"
	"testing"

	"market-lens/server/internal/features"
)

func computeKind(t *testing.T, f *engineFixture, request features.ComputeRequest) features.Run {
	t.Helper()
	request.Universe, request.AppVersion = fixtureUniverse, "test"
	if request.Workers == 0 {
		request.Workers = 2
	}
	run, err := features.NewService(features.NewRepository(f.pool), slog.Default()).Compute(context.Background(), request)
	if err != nil {
		t.Fatalf("compute %s: %v", request.Kind, err)
	}
	return run
}

// The longest active window is 251 stored sessions (return_250): a bar at S takes part in
// the windows of [S, S+250]. The composite at S and S+1 both read the bar at S, and the
// longest window that reads the composite is relative_strength_90's 91 sessions, so every
// other instrument's relative strength over [S, S+90] is affected — and nothing else is.
const (
	longestWindowReach   = 250
	compositeWindowReach = 90
)

func TestRevisingOneBarRecomputesExactlyItsWindows(t *testing.T) {
	f := newEngineFixture(t)
	first := computeKind(t, f, features.ComputeRequest{Kind: features.RunKindFull})
	f.snapshot("before_revision")

	// The composite is defined only once E lists, in the last 200 sessions: the revised bar
	// sits there, so the composite moves and the revision reaches every other member. The
	// longest window then runs off the end of the history, so [S, S+250] is clipped to it.
	const position = 150
	revised := f.sessionAtOffset(fixtureASessions - 1 - position)
	next := f.sessionAtOffset(fixtureASessions - 2 - position)
	compositeReach := f.sessionAtOffset(fixtureASessions - 1 - position - compositeWindowReach)
	importRun := f.newImportRun("ffffffff-0013-4000-8000-00000000a002")
	f.reviseBar(fixtureA, importRun, revised, fixtureBar(fixtureSeed(fixtureA), position).closeCents+2500)

	run := computeKind(t, f, features.ComputeRequest{Kind: features.RunKindIncremental, SinceRun: importRun})
	if run.Kind != features.RunKindIncremental || run.Status != features.RunStatusSucceeded ||
		run.TriggerRunID == nil || *run.TriggerRunID != importRun {
		t.Fatalf("run = %+v", run)
	}

	// SC-008, measured as a count: every changed value is one of A's from S on, or a
	// relative-strength value inside [S, S+89].
	outside := f.changedValues("before_revision", `NOT (
		(v.instrument_id = $1 AND v.session_date >= $2)
		OR (d.name LIKE 'relative_strength%' AND v.session_date BETWEEN $2 AND $3))`,
		fixtureA.String(), revised.String(), compositeReach.String())
	if outside != 0 {
		t.Errorf("%d values changed outside the windows the revised bar takes part in", outside)
	}
	if n := f.changedValues("before_revision", `v.instrument_id = $1 AND d.name = 'return_1' AND v.session_date IN ($2, $3)`,
		fixtureA.String(), revised.String(), next.String()); n != 2 {
		t.Errorf("return_1 at S and S+1 changed %d times, expected both", n)
	}
	// A 250-session return reads only its two endpoints, so the reach of the revision is
	// visible in a window that reads every close it spans.
	if n := f.changedValues("before_revision", `v.instrument_id = $1 AND d.name = 'sma_200' AND v.session_date = $2`,
		fixtureA.String(), fixtureAsOf.String()); n != 1 {
		t.Errorf("sma_200 at the last session, whose 200-session window holds the bar, did not change")
	}
	if n := f.changedValues("before_revision", `v.instrument_id <> $1 AND d.name LIKE 'relative_strength%'`, fixtureA.String()); n == 0 {
		t.Errorf("no other instrument's relative strength changed although the composite did")
	}

	// The composite changed at S and S+1 — the two returns that read the bar — and nowhere else.
	if n := f.changedComposites("before_revision", `c.session_date NOT IN ($1, $2)`, revised.String(), next.String()); n != 0 {
		t.Errorf("%d composite sessions outside S and S+1 changed", n)
	}
	if n := f.changedComposites("before_revision", `c.session_date IN ($1, $2)`, revised.String(), next.String()); n != 2 {
		t.Errorf("the composite changed at %d of S and S+1", n)
	}
	if n := count(t, f, `SELECT count(*) FROM universe_composites WHERE session_date >= $1 AND run_id <> $2`,
		revised.String(), run.ID.String()); n != 0 {
		t.Errorf("%d composite sessions inside the range were not recomputed by the incremental run", n)
	}

	// Nothing else was rewritten: A before S, every other member outside relative strength
	// over [S, S+89], and B and C entirely — B's twenty sessions lie past that reach.
	if n := count(t, f, `SELECT count(*) FROM feature_values v JOIN feature_definitions d ON d.id = v.definition_id
		WHERE v.run_id <> $1 AND NOT (
		  (v.instrument_id = $2 AND v.session_date >= $3)
		  OR (d.name LIKE 'relative_strength%' AND v.session_date BETWEEN $3 AND $4))`,
		first.ID.String(), fixtureA.String(), revised.String(), compositeReach.String()); n != 0 {
		t.Errorf("%d values outside the affected windows were rewritten", n)
	}
	if n := count(t, f, `SELECT count(*) FROM feature_values WHERE instrument_id = $1 AND session_date >= $2 AND run_id <> $3`,
		fixtureA.String(), revised.String(), run.ID.String()); n != 0 {
		t.Errorf("%d of A's values from S on were not recomputed", n)
	}
	if n := count(t, f, `SELECT count(*) FROM feature_values WHERE instrument_id = ANY($1::uuid[]) AND run_id <> $2`,
		[]string{fixtureB.String(), fixtureC.String()}, first.ID.String()); n != 0 {
		t.Errorf("%d values of B and C rewritten", n)
	}
	if n := count(t, f, `SELECT count(*) FROM feature_run_items WHERE run_id = $1 AND status = 'succeeded'`, run.ID.String()); n == 0 {
		t.Errorf("the incremental run recorded no succeeded item")
	}

	// The incremental result is the full result: recomputing everything changes nothing.
	f.snapshot("after_incremental")
	computeKind(t, f, features.ComputeRequest{Kind: features.RunKindFull})
	if n := f.changedValues("after_incremental", ""); n != 0 {
		t.Errorf("%d values differ between the incremental pass and a full recomputation", n)
	}
	if n := f.changedComposites("after_incremental", ""); n != 0 {
		t.Errorf("%d composite sessions differ between the incremental pass and a full recomputation", n)
	}
}

// Before E lists the composite has nine contributors and is undefined, so a revision there
// reaches nothing through it: only A's own windows recompute, and the far end of the range
// is visible because 251 sessions do not reach the end of A's history.
func TestARevisionReachesExactlyTheLongestWindow(t *testing.T) {
	f := newEngineFixture(t)
	first := computeKind(t, f, features.ComputeRequest{Kind: features.RunKindFull})
	f.snapshot("before_revision")
	const position = 40
	revised := f.sessionAtOffset(fixtureASessions - 1 - position)
	reach := f.sessionAtOffset(fixtureASessions - 1 - position - longestWindowReach)
	beyond := f.sessionAtOffset(fixtureASessions - 2 - position - longestWindowReach)
	importRun := f.newImportRun("ffffffff-0013-4000-8000-00000000a006")
	f.reviseBar(fixtureA, importRun, revised, fixtureBar(fixtureSeed(fixtureA), position).closeCents+2500)
	run := computeKind(t, f, features.ComputeRequest{Kind: features.RunKindIncremental, SinceRun: importRun})

	if n := f.changedValues("before_revision", `NOT (v.instrument_id = $1 AND v.session_date BETWEEN $2 AND $3)`,
		fixtureA.String(), revised.String(), reach.String()); n != 0 {
		t.Errorf("%d values changed outside A × [S, S+250]", n)
	}
	if n := f.changedValues("before_revision", `v.instrument_id = $1 AND d.name = 'return_250' AND v.session_date = $2`,
		fixtureA.String(), reach.String()); n != 1 {
		t.Errorf("return_250 at S+250, whose window starts at the revised bar, did not change")
	}
	if n := f.changedComposites("before_revision", ""); n != 0 {
		t.Errorf("%d composite sessions changed while the composite is undefined", n)
	}
	// The other members are still visited, because a revision usually moves the composite;
	// here it does not, so their rewritten relative strength is identical to what it replaced.
	compositeReach := f.sessionAtOffset(fixtureASessions - 1 - position - compositeWindowReach)
	if n := count(t, f, `SELECT count(*) FROM feature_values v JOIN feature_definitions d ON d.id = v.definition_id
		WHERE v.run_id <> $1 AND NOT (
		  (v.instrument_id = $2 AND v.session_date BETWEEN $3 AND $4)
		  OR (d.name LIKE 'relative_strength%' AND v.session_date BETWEEN $3 AND $5))`,
		first.ID.String(), fixtureA.String(), revised.String(), reach.String(), compositeReach.String()); n != 0 {
		t.Errorf("%d values outside A × [S, S+250] and relative strength over [S, S+90] were rewritten", n)
	}
	if n := count(t, f, `SELECT count(*) FROM feature_values WHERE instrument_id = $1 AND session_date = $2 AND run_id <> $3`,
		fixtureA.String(), beyond.String(), first.ID.String()); n != 0 {
		t.Errorf("A at S+251 was rewritten")
	}
	if n := count(t, f, `SELECT count(*) FROM feature_values WHERE instrument_id = $1
		AND session_date BETWEEN $2 AND $3 AND run_id <> $4`,
		fixtureA.String(), revised.String(), reach.String(), run.ID.String()); n != 0 {
		t.Errorf("%d of A's values inside [S, S+250] were not recomputed", n)
	}
}

func TestANewSessionRecomputesOnlyItself(t *testing.T) {
	f := newEngineFixture(t)
	cut := f.sessionAtOffset(1)
	f.truncateHistory(fixtureA, cut)
	first := computeKind(t, f, features.ComputeRequest{Kind: features.RunKindFull})
	f.snapshot("before_new_session")
	importRun := f.newImportRun("ffffffff-0013-4000-8000-00000000a003")
	if added := f.extendHistory(fixtureA, importRun, fixtureAsOf); len(added) != 1 {
		t.Fatalf("extended by %d sessions", len(added))
	}
	run := computeKind(t, f, features.ComputeRequest{Kind: features.RunKindIncremental, SinceRun: importRun})
	// The composite at the new session gained A as a contributor, so the other members'
	// relative strength there follows; no other existing value moves.
	if n := f.changedValues("before_new_session", `NOT (d.name LIKE 'relative_strength%' AND v.session_date = $1)`, fixtureAsOf.String()); n != 0 {
		t.Errorf("%d existing values changed when a session was added", n)
	}
	if n := f.changedValues("before_new_session", `d.name LIKE 'relative_strength%' AND v.session_date = $1`, fixtureAsOf.String()); n == 0 {
		t.Errorf("no relative strength at the new session followed the composite")
	}
	if n := count(t, f, `SELECT count(*) FROM feature_values WHERE instrument_id = $1 AND session_date = $2 AND run_id = $3`,
		fixtureA.String(), fixtureAsOf.String(), run.ID.String()); n != 24 {
		t.Errorf("%d values computed for the new session, expected 24", n)
	}
	if n := count(t, f, `SELECT count(*) FROM feature_values v JOIN feature_definitions d ON d.id = v.definition_id
		WHERE v.run_id <> $1 AND NOT (v.session_date = $2 AND (v.instrument_id = $3 OR d.name LIKE 'relative_strength%'))`,
		first.ID.String(), fixtureAsOf.String(), fixtureA.String()); n != 0 {
		t.Errorf("%d values away from the new session were rewritten", n)
	}
	// The composite at the new session gained A as a contributor; earlier sessions did not move.
	if n := f.changedComposites("before_new_session", `c.session_date < $1`, fixtureAsOf.String()); n != 0 {
		t.Errorf("%d earlier composite sessions changed", n)
	}
	if n := f.changedComposites("before_new_session", `c.session_date = $1`, fixtureAsOf.String()); n != 1 {
		t.Errorf("the composite at the new session did not change (%d)", n)
	}
}

func TestAnIncrementalRunWithNothingTouchedComputesNothing(t *testing.T) {
	f := newEngineFixture(t)
	first := computeKind(t, f, features.ComputeRequest{Kind: features.RunKindFull})
	run := computeKind(t, f, features.ComputeRequest{Kind: features.RunKindIncremental, SinceRun: f.newImportRun("ffffffff-0013-4000-8000-00000000a004")})
	if run.Status != features.RunStatusSucceeded || run.InstrumentCount != 0 || run.ValueCount != 0 {
		t.Errorf("run = %+v", run)
	}
	if n := count(t, f, `SELECT count(*) FROM feature_values WHERE run_id <> $1`, first.ID.String()); n != 0 {
		t.Errorf("%d values rewritten", n)
	}
}

func TestASupersededDefinitionLeavesItsValuesReadableAndLabelled(t *testing.T) {
	f := newEngineFixture(t)
	repository := features.NewRepository(f.pool)
	first := computeKind(t, f, features.ComputeRequest{Kind: features.RunKindFull})
	before := readAt(t, repository, fixtureA, fixtureAsOf)
	v1 := definitionID(t, f, "rsi_14")

	f.exec(`INSERT INTO feature_definitions
		(id, name, version, window_sessions, price_basis, parameters, undefined_conditions, session_length_sensitive, published_at)
		SELECT gen_random_uuid(), name, 2, window_sessions, price_basis, parameters || '{"period": 10}', undefined_conditions, session_length_sensitive, now()
		FROM feature_definitions WHERE name = 'rsi_14' AND version = 1`)
	f.exec(`UPDATE feature_definitions SET superseded_at = now() WHERE name = 'rsi_14' AND version = 1`)
	var v2 features.UUID
	if err := f.pool.QueryRow(f.ctx, `SELECT id::text FROM feature_definitions WHERE name = 'rsi_14' AND version = 2`).Scan((*string)(&v2)); err != nil {
		t.Fatal(err)
	}

	run := computeKind(t, f, features.ComputeRequest{Kind: features.RunKindDefinition, Definition: "rsi_14"})
	if run.Kind != features.RunKindDefinition || run.Status != features.RunStatusSucceeded ||
		run.DefinitionName == nil || *run.DefinitionName != "rsi_14" || run.InstrumentCount != fixtureMemberCount {
		t.Fatalf("run = %+v", run)
	}
	bars := count(t, f, `SELECT count(*) FROM daily_price_bars b JOIN universe_memberships m USING (instrument_id) WHERE m.universe_id = $1`, fixtureUnivID.String())
	if n := count(t, f, `SELECT count(*) FROM feature_values WHERE definition_id = $1 AND run_id = $2`, v2.String(), run.ID.String()); n != bars {
		t.Errorf("version 2 has %d rows, expected one per stored bar (%d)", n, bars)
	}
	if n := count(t, f, `SELECT count(*) FROM feature_values WHERE definition_id = $1 AND run_id = $2`, v1.String(), first.ID.String()); n != bars {
		t.Errorf("version 1 keeps %d rows, expected all %d", n, bars)
	}
	if n := count(t, f, `SELECT count(*) FROM feature_values WHERE definition_id NOT IN ($1, $2) AND run_id <> $3`,
		v1.String(), v2.String(), first.ID.String()); n != 0 {
		t.Errorf("%d values of other definitions were rewritten by a one-definition run", n)
	}

	after := readAt(t, repository, fixtureA, fixtureAsOf)
	if got := after["rsi_14"]; got.DefinitionVersion != 2 || got.Value == nil || before["rsi_14"].Value == nil || *got.Value == *before["rsi_14"].Value {
		t.Errorf("rsi_14 read as %s; before %s", describe(got), describe(before["rsi_14"]))
	}
	for name, value := range after {
		if name != "rsi_14" && describe(value) != describe(before[name]) {
			t.Errorf("%s changed from %s to %s", name, describe(before[name]), describe(value))
		}
	}
	both, err := repository.ListDefinitions(f.ctx, "rsi_14", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(both) != 2 || both[0].Version != 1 || both[0].SupersededAt == nil || both[1].Version != 2 || both[1].SupersededAt != nil {
		t.Errorf("definitions = %+v", both)
	}
}

func TestAFailedInstrumentKeepsItsPreviousValues(t *testing.T) {
	f := newEngineFixture(t)
	first := computeKind(t, f, features.ComputeRequest{Kind: features.RunKindFull})
	f.snapshot("before_failure")

	// D at offset 50 is the only bar of any instrument with this close at this session.
	target := f.sessionAtOffset(50)
	targetClose := float64(fixtureBar(fixtureSeed(fixtureD), fixtureDSessions-1-50).closeCents) / 100
	restore := features.OverrideComputeForTest("return_1", func(definition features.Definition, in features.Input) features.Result {
		last := in.Bars[len(in.Bars)-1]
		if last.Session == target && last.Close == targetClose {
			panic("injected: return_1 for D at " + target.String())
		}
		return features.Result{Reason: features.AbsenceInsufficientHistory}
	})
	defer restore()

	run := computeKind(t, f, features.ComputeRequest{Kind: features.RunKindFull})
	if run.Status != features.RunStatusPartial {
		t.Errorf("run status = %s, expected partial", run.Status)
	}
	if n := count(t, f, `SELECT count(*) FROM feature_values WHERE instrument_id = $1 AND run_id <> $2`, fixtureD.String(), first.ID.String()); n != 0 {
		t.Errorf("%d of D's values were rewritten by the run that failed on it", n)
	}
	if n := f.changedValues("before_failure", `v.instrument_id = $1`, fixtureD.String()); n != 0 {
		t.Errorf("%d of D's values changed", n)
	}
	var status string
	var code *string
	if err := f.pool.QueryRow(f.ctx, `SELECT status, error_code FROM feature_run_items WHERE run_id = $1 AND instrument_id = $2`,
		run.ID.String(), fixtureD.String()).Scan(&status, &code); err != nil {
		t.Fatalf("D's item: %v", err)
	}
	if status != "failed" || code == nil || *code == "" {
		t.Errorf("D's item = %s / %v", status, code)
	}
	for _, instrument := range []features.UUID{fixtureA, fixtureE} {
		if n := count(t, f, `SELECT count(*) FROM feature_values WHERE instrument_id = $1 AND run_id <> $2`, instrument.String(), run.ID.String()); n != 0 {
			t.Errorf("%s: %d values not recomputed", instrument, n)
		}
	}
	// The override was in force for everyone: return_1 is now an absence for A.
	if got := readAt(t, features.NewRepository(f.pool), fixtureA, fixtureAsOf)["return_1"]; got.AbsenceReason == nil {
		t.Errorf("return_1 for A = %s, expected the override's absence", describe(got))
	}
}
