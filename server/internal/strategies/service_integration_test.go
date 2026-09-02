package strategies_test

import (
	"testing"

	"market-lens/server/internal/strategies"
)

// TestAnUnscorableInstrumentRecordsAStatedAbsence is the constraint that keeps HOLD honest.
//
// The database already refuses a row that is both scored and absent. This asserts the service
// never reaches for the other escape: quietly scoring zero, which would surface as HOLD and be
// indistinguishable, to a reader, from a considered view that the instrument should be held.
func TestAnUnscorableInstrumentRecordsAStatedAbsence(t *testing.T) {
	fixture := newStrategyFixture(t)
	fixture.compute(strategies.ComputeRequest{Kind: strategies.RunKindFull})

	short := fixture.count(`SELECT count(*) FROM signals WHERE instrument_id = $1`, strategyShort.String())
	if short == 0 {
		t.Fatalf("the instrument with too little history has no signals at all, so nothing states why")
	}
	scored := fixture.count(`SELECT count(*) FROM signals WHERE instrument_id = $1 AND score IS NOT NULL`,
		strategyShort.String())
	if scored != 0 {
		t.Fatalf("%d sessions were scored for an instrument below the history minimum", scored)
	}
	reasons := fixture.reasons(strategyShort)
	if len(reasons) != 1 || reasons[0] != string(strategies.AbsenceInsufficientHistory) {
		t.Fatalf("the absence reasons are %v, wanted only %q", reasons, strategies.AbsenceInsufficientHistory)
	}

	// A fully scored instrument still records absences early in its history, for the same
	// reason: the longest factor window has not been reached yet.
	early := fixture.count(`SELECT count(*) FROM signals
		WHERE instrument_id = $1 AND absence_reason = $2`,
		strategyRanked(0).String(), string(strategies.AbsenceInsufficientHistory))
	if early == 0 {
		t.Fatalf("a deep-history instrument recorded no early absence, so the history gate never fired")
	}

	// Every absence names a reason from the published vocabulary, and no absence carries a view.
	if bad := fixture.count(`SELECT count(*) FROM signals
		WHERE absence_reason IS NOT NULL AND (score IS NOT NULL OR action IS NOT NULL OR confidence IS NOT NULL)`); bad != 0 {
		t.Fatalf("%d absent signals also carry a view", bad)
	}
}

// TestAScoredSignalIsAStatedViewWithEveryFactorAccountedFor asserts a scored signal explains
// itself completely: one contribution per factor of the version, whether or not each was
// available, so a reader can see what was missing as well as what counted.
func TestAScoredSignalIsAStatedViewWithEveryFactorAccountedFor(t *testing.T) {
	fixture := newStrategyFixture(t)
	fixture.compute(strategies.ComputeRequest{Kind: strategies.RunKindFull})

	factors := fixture.count(`SELECT jsonb_array_length(parameters -> 'factors') FROM strategies
		WHERE superseded_at IS NULL`)
	if mismatched := fixture.count(`SELECT count(*) FROM signals
		WHERE score IS NOT NULL AND jsonb_array_length(contributions) <> $1`, factors); mismatched != 0 {
		t.Fatalf("%d scored signals do not account for all %d factors", mismatched, factors)
	}
	if unnamed := fixture.count(`SELECT count(*) FROM signals s, jsonb_array_elements(s.contributions) c
		WHERE c ->> 'factor' IS NULL OR c ->> 'feature' IS NULL OR c ->> 'weight' IS NULL`); unnamed != 0 {
		t.Fatalf("%d contributions do not name their factor, feature and weight", unnamed)
	}
	// A contribution that was not available must say so rather than appear as a silent nothing.
	if silent := fixture.count(`SELECT count(*) FROM signals s, jsonb_array_elements(s.contributions) c
		WHERE c -> 'contribution' IS NULL AND c ->> 'unavailable_reason' IS NULL`); silent != 0 {
		t.Fatalf("%d contributions are neither counted nor explained", silent)
	}
	if scored := fixture.count(`SELECT count(*) FROM signals WHERE score IS NOT NULL`); scored == 0 {
		t.Fatalf("nothing was scored at all, so this proves nothing")
	}
}

// TestTheContributionsStoredWithASignalReconcileWithIt is FR-011 as an integration claim rather
// than a unit one: the arithmetic must survive the round trip through the database, at the
// stored precision, for every signal — not just for the inputs a unit test happened to pick.
func TestTheContributionsStoredWithASignalReconcileWithIt(t *testing.T) {
	fixture := newStrategyFixture(t)
	fixture.compute(strategies.ComputeRequest{Kind: strategies.RunKindFull})

	// The tolerance is one unit in the last stored place, which is what rounding a quotient to
	// twelve decimals can cost. Anything larger is a disagreement, not a rounding artefact.
	mismatched := fixture.count(`SELECT count(*) FROM (
		SELECT s.instrument_id, s.session_date, s.score, s.divisor,
		       sum((c ->> 'contribution')::numeric) AS total
		FROM signals s, jsonb_array_elements(s.contributions) c
		WHERE s.score IS NOT NULL AND c -> 'contribution' IS NOT NULL
		GROUP BY s.instrument_id, s.session_date, s.score, s.divisor
	) reconciled WHERE abs(total / divisor - score) > 1e-12`)
	if mismatched != 0 {
		t.Fatalf("%d signals do not reconcile with their own contributions", mismatched)
	}
	if empty := fixture.count(`SELECT count(*) FROM signals WHERE score IS NOT NULL AND divisor IS NULL`); empty != 0 {
		t.Fatalf("%d scored signals stored no divisor, so their explanation cannot be checked", empty)
	}
}

func (f *strategyFixture) reasons(instrument any) []string {
	f.t.Helper()
	rows, err := f.pool.Query(f.ctx, `SELECT DISTINCT absence_reason FROM signals
		WHERE instrument_id = $1 AND absence_reason IS NOT NULL ORDER BY 1`, instrument)
	if err != nil {
		f.t.Fatalf("read absence reasons: %v", err)
	}
	defer rows.Close()
	var reasons []string
	for rows.Next() {
		var reason string
		if err := rows.Scan(&reason); err != nil {
			f.t.Fatal(err)
		}
		reasons = append(reasons, reason)
	}
	return reasons
}

// TestAFailedInstrumentKeepsItsPreviousSignals is the containment claim.
//
// One instrument the strategy cannot score must not cost the other ninety-nine their run. What
// it keeps is its *previous* signals — stale, but stated by a version, with a run item saying
// which instrument was left behind and why. Silently dropping them would leave a gap that reads
// as "this instrument has no view", which is a different and untrue claim.
func TestAFailedInstrumentKeepsItsPreviousSignals(t *testing.T) {
	fixture := newStrategyFixture(t)
	first := fixture.compute(strategies.ComputeRequest{Kind: strategies.RunKindFull})
	if first.Status != strategies.RunStatusSucceeded {
		t.Fatalf("the first run ended %s", first.Status)
	}
	fixture.snapshot("signals_before_the_failure")
	broken := strategyRanked(2)

	// A regime label outside the strategy's stated vocabulary. An unknown label is an error
	// rather than a zero: scoring it as neutral would invent a view from something nobody
	// defined, which is the failure this containment exists to keep visible.
	fixture.exec(`UPDATE feature_values SET label = 'euphoric'
		WHERE instrument_id = $1 AND label IS NOT NULL
		  AND definition_id = (SELECT id FROM feature_definitions WHERE name = 'regime' AND superseded_at IS NULL)`,
		broken.String())

	second := fixture.compute(strategies.ComputeRequest{Kind: strategies.RunKindFull})
	if second.Status != strategies.RunStatusPartial {
		t.Fatalf("a run with one failed instrument ended %s, wanted partial", second.Status)
	}
	if second.FailedCount != 1 {
		t.Fatalf("the run reported %d failed instruments", second.FailedCount)
	}
	if second.SignalCount == 0 {
		t.Fatalf("no other instrument was written")
	}

	item := fixture.count(`SELECT count(*) FROM strategy_run_items
		WHERE run_id = $1 AND instrument_id = $2 AND status = 'failed' AND error_code IS NOT NULL
		  AND error_summary IS NOT NULL`, second.ID.String(), broken.String())
	if item != 1 {
		t.Fatalf("the failed instrument has %d run items naming a reason", item)
	}

	kept := fixture.count(`SELECT count(*) FROM signals WHERE instrument_id = $1`, broken.String())
	before := fixture.count(`SELECT count(*) FROM signals_before_the_failure WHERE instrument_id = $1`,
		broken.String())
	if kept != before || kept == 0 {
		t.Fatalf("the failed instrument holds %d signals, having held %d", kept, before)
	}
	if changed := fixture.count(`SELECT count(*) FROM signals s JOIN signals_before_the_failure p
		USING (instrument_id, session_date, strategy_id)
		WHERE s.instrument_id = $1 AND s.score IS DISTINCT FROM p.score`, broken.String()); changed != 0 {
		t.Fatalf("%d of the failed instrument's signals were rewritten", changed)
	}

	// And the instruments that succeeded were written by this run, not the previous one.
	written := fixture.count(`SELECT count(*) FROM signals WHERE run_id = $1`, second.ID.String())
	if written == 0 {
		t.Fatalf("the partial run wrote nothing")
	}
}

// TestAnInstrumentThatFailsPartWayThroughKeepsAllOfItsPreviousSignals is the containment claim
// again, at the point where it is actually hard.
//
// A long history is computed in blocks. An instrument that scores fine for the first blocks and
// fails in a later one must not be left holding a mixture — some sessions rewritten by the run
// that failed, the rest from an earlier one. Every row would look well formed and the series as
// a whole would be from no single computation, which is precisely the kind of wrongness nothing
// downstream can detect.
func TestAnInstrumentThatFailsPartWayThroughKeepsAllOfItsPreviousSignals(t *testing.T) {
	fixture := newStrategyFixture(t)
	fixture.compute(strategies.ComputeRequest{Kind: strategies.RunKindFull})
	fixture.snapshot("signals_before_the_late_failure")
	broken := strategyRanked(3)

	// The label is corrupted only for the most recent sessions, so the instrument scores
	// normally through the earlier blocks and fails in the last one.
	fixture.exec(`UPDATE feature_values SET label = 'euphoric'
		WHERE instrument_id = $1 AND label IS NOT NULL AND session_date > $2::date
		  AND definition_id = (SELECT id FROM feature_definitions WHERE name = 'regime' AND superseded_at IS NULL)`,
		broken.String(), "2026-04-01")

	run := fixture.compute(strategies.ComputeRequest{Kind: strategies.RunKindFull})
	if run.Status != strategies.RunStatusPartial {
		t.Fatalf("the run ended %s, wanted partial", run.Status)
	}

	changed := fixture.count(`SELECT count(*) FROM signals s JOIN signals_before_the_late_failure p
		USING (instrument_id, session_date, strategy_id)
		WHERE s.instrument_id = $1
		  AND (s.score IS DISTINCT FROM p.score OR s.run_id IS DISTINCT FROM p.run_id)`, broken.String())
	if changed != 0 {
		t.Fatalf("%d of the failed instrument's earlier signals were rewritten by the run that failed", changed)
	}
	if written := fixture.count(`SELECT count(*) FROM signals WHERE instrument_id = $1 AND run_id = $2`,
		broken.String(), run.ID.String()); written != 0 {
		t.Fatalf("the failed run wrote %d signals for the instrument it could not finish", written)
	}
	// The instruments that succeeded were fully rewritten by this run.
	if stale := fixture.count(`SELECT count(*) FROM signals WHERE instrument_id = $1 AND run_id <> $2`,
		strategyRanked(0).String(), run.ID.String()); stale != 0 {
		t.Fatalf("%d of a successful instrument's signals are from an earlier run", stale)
	}
}
