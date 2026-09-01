package features_test

import (
	"testing"

	"market-lens/server/internal/features"
)

// SC-002: no feature saw the future. Every test here proves it the same way — compute, keep
// the result, change only what lies after a point in time, recompute, and show that nothing
// on or before that point moved. A value that had read past its session would move.

func TestExtendingHistoryChangesNoEarlierValue(t *testing.T) {
	f := newEngineFixture(t)
	const truncated = 60
	cut := f.sessionAtOffset(truncated)
	f.truncateHistory(fixtureA, cut)
	computeFixture(t, f, 2)
	if n := count(t, f, `SELECT count(*) FROM feature_values WHERE instrument_id = $1 AND session_date > $2`, fixtureA.String(), cut.String()); n != 0 {
		t.Fatalf("%d values exist past the truncation point before extension", n)
	}
	f.snapshot("before_extension")

	extended := f.extendHistory(fixtureA, f.newImportRun("ffffffff-0013-4000-8000-00000000a001"), fixtureAsOf)
	if len(extended) != truncated {
		t.Fatalf("extended by %d sessions, expected %d", len(extended), truncated)
	}
	computeFixture(t, f, 2)

	if n := f.changedValues("before_extension", `v.session_date <= $1`, cut.String()); n != 0 {
		t.Errorf("%d values on or before %s changed when later history arrived", n, cut)
	}
	if n := f.changedComposites("before_extension", `c.session_date <= $1`, cut.String()); n != 0 {
		t.Errorf("%d composite sessions on or before %s changed when later history arrived", n, cut)
	}
	// The extension did compute: A now has values past the cut, and those match the golden
	// values for the full series, so the recomputation is the real one.
	if n := count(t, f, `SELECT count(*) FROM feature_values WHERE instrument_id = $1 AND session_date > $2`, fixtureA.String(), cut.String()); n != truncated*24 {
		t.Errorf("%d values past the cut, expected %d", n, truncated*24)
	}
	golden := goldenAt(t, loadGoldenA(t), fixtureASessions-1)
	latest := readAt(t, features.NewRepository(f.pool), fixtureA, fixtureAsOf)
	for _, name := range []string{"return_250", "sma_200", "relative_strength_90", "rsi_14"} {
		want := golden.Features[name].Value
		if want == nil {
			t.Fatalf("golden has no value for %s", name)
		}
		if got := expectNumber(t, latest, name, "after extension"); got != *want {
			t.Errorf("%s after extension = %s, expected the golden %s", name, got, *want)
		}
	}
}

// The service has exactly one path to a bar: History.Window on the instrument's stored bars,
// on the raw basis or the basis adjusted as of the session. This test walks that path for
// every session and every active window of A (complete) and D (with a gap) and fails on the
// first bar returned past the session it was asked for, or the first split applied before
// its ex-date.
func TestNoDefinitionReadsABarAfterItsSession(t *testing.T) {
	f := newEngineFixture(t)
	repository := features.NewRepository(f.pool)
	definitions, err := repository.Definitions(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := features.NewRegistry(definitions)
	if err != nil {
		t.Fatal(err)
	}
	calendar, err := repository.Calendar(f.ctx, f.exchange)
	if err != nil {
		t.Fatal(err)
	}
	for _, instrument := range []features.UUID{fixtureA, fixtureD, fixtureE} {
		bars, err := repository.Bars(f.ctx, instrument)
		if err != nil {
			t.Fatal(err)
		}
		splits, err := repository.Splits(f.ctx, instrument)
		if err != nil {
			t.Fatal(err)
		}
		raw := features.NewHistory(bars, calendar)
		reads, violations := 0, 0
		for _, session := range raw.Sessions() {
			if _, ok := raw.Bar(session); !ok {
				continue
			}
			adjusted := features.NewHistory(features.Adjusted(bars, splits, session), calendar)
			for _, definition := range registry.Active() {
				view := raw
				if definition.PriceBasis == features.PriceBasisAdjusted {
					view = adjusted
				}
				window, reason := view.Window(session, *definition.WindowSessions)
				if reason != "" {
					continue
				}
				reads++
				for _, bar := range window {
					if bar.Session > session {
						violations++
						t.Errorf("%s at %s read a bar at %s, after its session", definition.Name, session, bar.Session)
						break
					}
				}
				if window[len(window)-1].Session != session {
					t.Errorf("%s at %s: the window ends at %s", definition.Name, session, window[len(window)-1].Session)
				}
			}
			// A split with an ex-date after the session must not touch the adjusted view.
			for _, split := range splits {
				if split.ExDate > session {
					if a, ok := adjusted.Bar(bars[0].Session); ok && a.Close != bars[0].Close {
						t.Errorf("as of %s the split at %s was already applied", session, split.ExDate)
					}
				}
			}
		}
		if reads == 0 || violations != 0 {
			t.Errorf("%s: %d satisfied windows read, %d violations", instrument, reads, violations)
		}
	}
}

func TestACorporateActionAffectsOnlySessionsOnOrAfterItsExDate(t *testing.T) {
	f := newEngineFixture(t)
	computeFixture(t, f, 2)
	f.snapshot("before_future_split")

	// A split whose ex-date is after the last stored session has not happened as of any
	// stored session: nothing may change.
	f.exec(`INSERT INTO corporate_actions
		(id, instrument_id, provider, provider_action_id, action_type, ex_date, ratio,
		 source_hash, import_run_id, first_observed_at, last_observed_at)
		VALUES ($1, $2, 'fixture', 'split-e-future', 'split', '2026-07-06', 3, 'split-hash-2', $3, now(), now())`,
		"ffffffff-0013-4000-8000-0000000000e2", fixtureE.String(), fixtureRunID.String())
	computeFixture(t, f, 2)
	if n := f.changedValues("before_future_split", ""); n != 0 {
		t.Errorf("%d values changed for a split that has not happened yet", n)
	}
	if n := f.changedComposites("before_future_split", ""); n != 0 {
		t.Errorf("%d composite sessions changed for a split that has not happened yet", n)
	}

	// A split inside the history: only sessions on or after its ex-date can see it, and of
	// those only the ones whose window reaches back across it.
	const exOffset = 100
	exDate := f.sessionAtOffset(exOffset)
	f.snapshot("before_past_split")
	f.exec(`INSERT INTO corporate_actions
		(id, instrument_id, provider, provider_action_id, action_type, ex_date, ratio,
		 source_hash, import_run_id, first_observed_at, last_observed_at)
		VALUES ($1, $2, 'fixture', 'split-e-past', 'split', $3, 3, 'split-hash-3', $4, now(), now())`,
		"ffffffff-0013-4000-8000-0000000000e3", fixtureE.String(), exDate.String(), fixtureRunID.String())
	computeFixture(t, f, 2)

	if n := f.changedValues("before_past_split", `v.session_date < $1`, exDate.String()); n != 0 {
		t.Errorf("%d values before the ex-date %s changed", n, exDate)
	}
	if n := f.changedValues("before_past_split", `d.price_basis = 'raw' AND d.name NOT LIKE 'relative_strength%'`); n != 0 {
		t.Errorf("%d raw-basis values changed; a split adjusts only the adjusted basis", n)
	}
	if n := f.changedValues("before_past_split", `v.instrument_id = $1 AND v.session_date >= $2 AND d.price_basis = 'adjusted'`,
		fixtureE.String(), exDate.String()); n == 0 {
		t.Errorf("no adjusted value of E on or after %s changed; the split was not applied", exDate)
	}
	// The ex-date session's session-over-session return spans the split, so the composite
	// changes there and only there — and through it, relative strength for the sessions whose
	// composite window covers the ex-date.
	if n := f.changedComposites("before_past_split", `c.session_date <> $1`, exDate.String()); n != 0 {
		t.Errorf("%d composite sessions other than the ex-date changed", n)
	}
	if n := f.changedComposites("before_past_split", `c.session_date = $1`, exDate.String()); n != 1 {
		t.Errorf("the composite at the ex-date did not change (%d)", n)
	}
	last := f.sessionAtOffset(exOffset - 89)
	if n := f.changedValues("before_past_split", `v.instrument_id <> $1 AND NOT (d.name LIKE 'relative_strength%' AND v.session_date BETWEEN $2 AND $3)`,
		fixtureE.String(), exDate.String(), last.String()); n != 0 {
		t.Errorf("%d values of other instruments changed outside relative strength over [%s, %s]", n, exDate, last)
	}
	// Beyond the longest window past the ex-date, nothing of E's window reaches back across
	// it (offset 100 is the ex-date; return_250 reads 251 sessions, so every session with an
	// offset below 100-250 is unaffected — none in this fixture — and the check is that no
	// session before the ex-date moved, made above).
}

func TestTheCompositeUsesOnlyBarsOnOrBeforeEachSession(t *testing.T) {
	f := newEngineFixture(t)
	computeFixture(t, f, 2)
	f.snapshot("before_new_member")
	// The newcomer's first bar is at offset newSessions-1. It has no prior bar there, so it
	// cannot contribute a return until the session after: the composite is unchanged through
	// the first bar and counts one more contributor from the second.
	const newSessions = 30
	cut := f.sessionAtOffset(newSessions - 1)
	f.addInstrument(fixtureF, "SE0000000106", "FXF", "Fixture Newcomer AB", "SEK")
	f.addBars(fixtureF, newSessions, nil, 0)
	f.exec(`INSERT INTO universe_memberships (universe_id, instrument_id, included_from, curation_source)
		VALUES ($1, $2, '2016-01-01', 'fixture')`, fixtureUnivID.String(), fixtureF.String())
	_, run := computeFixture(t, f, 2)
	if run.InstrumentCount != fixtureMemberCount+1 {
		t.Errorf("the run covered %d instruments, expected %d", run.InstrumentCount, fixtureMemberCount+1)
	}
	if n := f.changedComposites("before_new_member", `c.session_date <= $1`, cut.String()); n != 0 {
		t.Errorf("%d composite sessions on or before %s changed when a member listed after it", n, cut)
	}
	if n := count(t, f, `SELECT count(*) FROM universe_composites c JOIN before_new_member_composites b USING (universe_id, session_date, definition_id)
		WHERE c.session_date > $1 AND c.contributor_count <> b.contributor_count + 1`, cut.String()); n != 0 {
		t.Errorf("%d composite sessions after %s do not count the newcomer", n, cut)
	}
	if n := f.changedValues("before_new_member", `v.session_date <= $1`, cut.String()); n != 0 {
		t.Errorf("%d values on or before %s changed when a member listed after it", n, cut)
	}
}
