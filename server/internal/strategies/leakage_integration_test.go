package strategies_test

import (
	"testing"

	"market-lens/server/internal/strategies"
)

// TestNoSignalReadsAFeatureFromALaterSession is the engine's no-lookahead rule carried into the
// strategy layer, where it is easier to break and harder to notice: the engine reads bars, which
// obviously have dates, while a strategy reads a table of values that a careless query could
// join without a date bound at all and still return plausible numbers.
func TestNoSignalReadsAFeatureFromALaterSession(t *testing.T) {
	// Reserve sixty sessions so the history can be extended with data the first computation
	// could not have known.
	fixture := newStrategyFixtureEndingBefore(t, 60)
	fixture.compute(strategies.ComputeRequest{Kind: strategies.RunKindFull})

	if ahead := fixture.count(`SELECT count(*) FROM signals s, jsonb_array_elements(s.contributions) c
		WHERE (c ->> 'feature_session')::date > s.session_date`); ahead != 0 {
		t.Fatalf("%d contributions read a feature from a session after their own", ahead)
	}

	fixture.snapshot("signals_before_the_future")
	lastKnown := fixture.count(`SELECT count(*) FROM signals`)

	// Sixty sessions arrive. Nothing that was said before them may move.
	fixture.extendHistory(60)
	fixture.computeFeatures()
	fixture.compute(strategies.ComputeRequest{Kind: strategies.RunKindFull})

	changed := fixture.count(`SELECT count(*) FROM signals s JOIN signals_before_the_future p
		USING (instrument_id, session_date, strategy_id)
		WHERE s.score IS DISTINCT FROM p.score
		   OR s.action IS DISTINCT FROM p.action
		   OR s.confidence IS DISTINCT FROM p.confidence
		   OR s.absence_reason IS DISTINCT FROM p.absence_reason
		   OR s.contributions::text IS DISTINCT FROM p.contributions::text`)
	if changed != 0 {
		t.Fatalf("%d of %d earlier signals changed once later sessions existed", changed, lastKnown)
	}
	if grown := fixture.count(`SELECT count(*) FROM signals`); grown <= lastKnown {
		t.Fatalf("the history holds %d signals after sixty more sessions and %d before", grown, lastKnown)
	}
	if ahead := fixture.count(`SELECT count(*) FROM signals s, jsonb_array_elements(s.contributions) c
		WHERE (c ->> 'feature_session')::date > s.session_date`); ahead != 0 {
		t.Fatalf("%d contributions read a feature from a session after their own", ahead)
	}
}
