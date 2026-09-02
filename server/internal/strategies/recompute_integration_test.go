package strategies_test

import (
	"testing"

	"market-lens/server/internal/features"
	"market-lens/server/internal/strategies"
)

// TestARevisedBarRewritesOnlyTheSessionsItAffects is the incremental pass's whole claim, and it
// has two halves that pull against each other.
//
// The narrow half: a correction deep in history must not rewrite signals outside the sessions it
// can reach. The wide half: within those sessions it must rewrite *every* instrument, not only
// the corrected one — because the factors are cross-sectional, so one instrument's return moving
// changes where every other instrument ranks for the same session. Recomputing only the touched
// instrument would leave the rest quietly wrong, which is the trap the feature engine's composite
// already sprang once.
func TestARevisedBarRewritesOnlyTheSessionsItAffects(t *testing.T) {
	fixture := newStrategyFixture(t)
	fixture.compute(strategies.ComputeRequest{Kind: strategies.RunKindFull})
	fixture.snapshot("signals_before_the_revision")

	revised := strategyRanked(0)
	importRun, session := fixture.reviseBar(revised, 60, 250000)
	featureRun := fixture.computeFeatureRun(features.ComputeRequest{
		Kind: features.RunKindIncremental, SinceRun: importRun,
	})

	run := fixture.compute(strategies.ComputeRequest{
		Kind: strategies.RunKindIncremental, SinceFeatureRun: strategies.UUID(featureRun.ID),
	})
	if run.SignalCount == 0 {
		t.Fatalf("the incremental pass recorded no signals at all")
	}

	from, to := fixture.runRange(featureRun.ID)
	outside := fixture.count(`SELECT count(*) FROM signals s JOIN signals_before_the_revision p
		USING (instrument_id, session_date, strategy_id)
		WHERE s.session_date NOT BETWEEN $1::date AND $2::date
		  AND (s.score IS DISTINCT FROM p.score OR s.action IS DISTINCT FROM p.action
		    OR s.confidence IS DISTINCT FROM p.confidence
		    OR s.absence_reason IS DISTINCT FROM p.absence_reason
		    OR s.contributions::text IS DISTINCT FROM p.contributions::text)`, from, to)
	if outside != 0 {
		t.Fatalf("%d signals outside %s..%s changed, though nothing there was revised", outside, from, to)
	}

	if session.String() < from || session.String() > to {
		t.Fatalf("the revised session %s is not inside the recomputed range %s..%s", session, from, to)
	}
	movedItself := fixture.count(`SELECT count(*) FROM signals s JOIN signals_before_the_revision p
		USING (instrument_id, session_date, strategy_id)
		WHERE s.instrument_id = $1 AND s.score IS DISTINCT FROM p.score`, revised.String())
	if movedItself == 0 {
		t.Fatalf("the revised instrument's own signals did not move at all")
	}

	// The wide half. Another instrument's view must have moved even though its own data did not.
	movedOthers := fixture.count(`SELECT count(DISTINCT s.instrument_id) FROM signals s
		JOIN signals_before_the_revision p USING (instrument_id, session_date, strategy_id)
		WHERE s.instrument_id <> $1 AND s.score IS DISTINCT FROM p.score`, revised.String())
	if movedOthers == 0 {
		t.Fatalf("no other instrument's rank moved, so the pass recomputed only the revised one")
	}
}

// runRange is the session span a feature run wrote, which is the scope the incremental strategy
// pass derives from it.
func (f *strategyFixture) runRange(runID features.UUID) (string, string) {
	f.t.Helper()
	var from, to string
	if err := f.pool.QueryRow(f.ctx, `SELECT min(from_session)::text, max(to_session)::text
		FROM feature_run_items WHERE run_id = $1 AND status = 'succeeded'`,
		runID.String()).Scan(&from, &to); err != nil {
		f.t.Fatalf("read the feature run range: %v", err)
	}
	return from, to
}
