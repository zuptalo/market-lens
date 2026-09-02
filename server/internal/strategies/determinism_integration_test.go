package strategies_test

import (
	"testing"

	"market-lens/server/internal/strategies"
)

// TestTheSameStrategyVersionScoresAnInstrumentIdentically is the claim the whole feature rests
// on: a signal recorded once is the signal you get when you ask again.
//
// It compares every stored field a reader can see — the score, the action, the confidence, the
// absence reason, the divisor and every contribution — because a recomputation that agreed on
// the score while disagreeing on the reasons would be the more damaging failure of the two: it
// would leave the product still able to show a number, but no longer able to justify it.
func TestTheSameStrategyVersionScoresAnInstrumentIdentically(t *testing.T) {
	fixture := newStrategyFixture(t)

	first := fixture.compute(strategies.ComputeRequest{Kind: strategies.RunKindFull})
	if first.SignalCount == 0 {
		t.Fatalf("the run recorded no signals, so there is nothing to reproduce")
	}
	fixture.snapshot("signals_first_pass")

	second := fixture.compute(strategies.ComputeRequest{Kind: strategies.RunKindFull})
	if second.SignalCount != first.SignalCount {
		t.Fatalf("the second pass recorded %d signals, the first %d", second.SignalCount, first.SignalCount)
	}
	if changed := fixture.changedSignals("signals_first_pass"); changed != 0 {
		t.Fatalf("%d signals differ between two passes of the same strategy version", changed)
	}
	missing := fixture.count(`SELECT count(*) FROM signals_first_pass p
		WHERE NOT EXISTS (SELECT 1 FROM signals s
			WHERE s.instrument_id = p.instrument_id AND s.session_date = p.session_date
			  AND s.strategy_id = p.strategy_id)`)
	if missing != 0 {
		t.Fatalf("the second pass dropped %d signals the first recorded", missing)
	}
}

// TestRecomputingTheWholeHistoryChangesNothing is the same claim as the one above at the scale
// it actually matters: not one instrument and one session, but every stored signal.
func TestRecomputingTheWholeHistoryChangesNothing(t *testing.T) {
	fixture := newStrategyFixture(t)
	fixture.compute(strategies.ComputeRequest{Kind: strategies.RunKindFull})
	before := fixture.count(`SELECT count(*) FROM signals`)
	fixture.snapshot("signals_before_recompute")

	fixture.compute(strategies.ComputeRequest{Kind: strategies.RunKindFull})

	if after := fixture.count(`SELECT count(*) FROM signals`); after != before {
		t.Fatalf("the history holds %d signals after recomputing and %d before", after, before)
	}
	if changed := fixture.changedSignals("signals_before_recompute"); changed != 0 {
		t.Fatalf("%d of %d signals moved when nothing about the data moved", changed, before)
	}
}

// TestASupersededVersionKeepsItsSignals is what makes a recorded view a record.
//
// Publishing a new version must not rewrite what the old one said, and must not leave the old
// signals unattributed either: a stored score with no version behind it is a number nobody can
// argue with, which is precisely what the product must not produce.
func TestASupersededVersionKeepsItsSignals(t *testing.T) {
	fixture := newStrategyFixture(t)
	fixture.compute(strategies.ComputeRequest{Kind: strategies.RunKindFull})
	fixture.snapshot("signals_version_one")
	firstVersion := fixture.count(`SELECT count(*) FROM signals s
		JOIN strategies st ON st.id = s.strategy_id WHERE st.version = 1`)
	if firstVersion == 0 {
		t.Fatalf("the first version recorded no signals")
	}

	fixture.publishSecondVersion()
	fixture.compute(strategies.ComputeRequest{Kind: strategies.RunKindStrategy, Strategy: "momentum_trend", Version: 2})

	if changed := fixture.changedSignals("signals_version_one"); changed != 0 {
		t.Fatalf("computing version 2 changed %d of version 1's signals", changed)
	}
	if lost := fixture.count(`SELECT count(*) FROM signals s
		JOIN strategies st ON st.id = s.strategy_id WHERE st.version = 1`); lost != firstVersion {
		t.Fatalf("version 1 now holds %d signals, not the %d it recorded", lost, firstVersion)
	}
	secondVersion := fixture.count(`SELECT count(*) FROM signals s
		JOIN strategies st ON st.id = s.strategy_id WHERE st.version = 2`)
	if secondVersion == 0 {
		t.Fatalf("version 2 recorded no signals of its own")
	}
	// The two versions differ in their weights, so at least some sessions must differ in view.
	// If they agreed everywhere, the test would pass while proving only that both ran.
	differing := fixture.count(`SELECT count(*) FROM signals a
		JOIN strategies sa ON sa.id = a.strategy_id AND sa.version = 1
		JOIN signals b ON b.instrument_id = a.instrument_id AND b.session_date = a.session_date
		JOIN strategies sb ON sb.id = b.strategy_id AND sb.version = 2
		WHERE a.score IS DISTINCT FROM b.score`)
	if differing == 0 {
		t.Fatalf("the two versions agree on every signal, so version keying is untested here")
	}
	if orphaned := fixture.count(`SELECT count(*) FROM signals s
		WHERE NOT EXISTS (SELECT 1 FROM strategies st WHERE st.id = s.strategy_id)`); orphaned != 0 {
		t.Fatalf("%d signals name no strategy version", orphaned)
	}
}

// publishSecondVersion supersedes the first the way a migration would: forward only, never by
// editing what was published.
func (f *strategyFixture) publishSecondVersion() {
	f.t.Helper()
	f.exec(`UPDATE strategies SET superseded_at = now() WHERE name = 'momentum_trend' AND version = 1`)
	f.exec(`INSERT INTO strategies (id, name, version, title, intent, caveat, parameters, published_at)
		SELECT '00000000-0015-4000-8000-000000000002', name, 2, title, intent, caveat,
		       jsonb_set(parameters, '{factors,0,weight}', '"0.45"'), now()
		FROM strategies WHERE name = 'momentum_trend' AND version = 1`)
}
