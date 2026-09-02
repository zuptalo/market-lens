package strategies_test

import (
	"testing"

	"market-lens/server/internal/strategies"
)

// TestEveryInstrumentSessionHasASignal is FR-008a: the strategy has something to say about every
// instrument at every session it has data for, even when what it has to say is "I cannot score
// this, and here is why". A missing row and a stated absence look the same on a screen only if
// the screen is careless; in the database they are entirely different claims, and only one of
// them is honest about a gap.
func TestEveryInstrumentSessionHasASignal(t *testing.T) {
	fixture := newStrategyFixture(t)
	fixture.compute(strategies.ComputeRequest{Kind: strategies.RunKindFull})

	missing := fixture.count(`SELECT count(*) FROM (
		SELECT DISTINCT v.instrument_id, v.session_date
		FROM feature_values v
		JOIN universe_memberships m ON m.instrument_id = v.instrument_id
		JOIN research_universes u ON u.id = m.universe_id AND u.code = $1
	) stored
	WHERE NOT EXISTS (
		SELECT 1 FROM signals s
		JOIN strategies st ON st.id = s.strategy_id AND st.superseded_at IS NULL
		WHERE s.instrument_id = stored.instrument_id AND s.session_date = stored.session_date)`,
		strategyUniverse)
	if missing != 0 {
		t.Fatalf("%d instrument-sessions have feature values but no signal at all", missing)
	}

	// Every signal settles exactly one of the two. The database enforces this, so a failure here
	// means the check was weakened rather than that the service misbehaved — which is worth
	// catching separately, because a weakened check is the kind of change that looks harmless.
	if neither := fixture.count(`SELECT count(*) FROM signals
		WHERE score IS NULL AND absence_reason IS NULL`); neither != 0 {
		t.Fatalf("%d signals are neither a view nor a stated absence", neither)
	}
	if both := fixture.count(`SELECT count(*) FROM signals
		WHERE score IS NOT NULL AND absence_reason IS NOT NULL`); both != 0 {
		t.Fatalf("%d signals are both a view and an absence", both)
	}
	if orphaned := fixture.count(`SELECT count(*) FROM signals s
		WHERE NOT EXISTS (SELECT 1 FROM strategy_runs r WHERE r.id = s.run_id)`); orphaned != 0 {
		t.Fatalf("%d signals name no run, so nothing records when or by what they were produced", orphaned)
	}
}

// TestEveryPublishedStrategyFactorNamesALivingFeature guards the seam between the two versioned
// vocabularies. A feature definition can be superseded independently of the strategies that read
// it; when that happens the strategy does not break loudly, it quietly starts recording an
// unavailable factor for every instrument and every session, and its scores drift while every
// individual row still looks well formed.
func TestEveryPublishedStrategyFactorNamesALivingFeature(t *testing.T) {
	fixture := newStrategyFixture(t)

	dangling := fixture.count(`SELECT count(*) FROM strategies s,
		jsonb_array_elements(s.parameters -> 'factors') f
		WHERE NOT EXISTS (
			SELECT 1 FROM feature_definitions d
			WHERE d.name = f ->> 'feature' AND d.superseded_at IS NULL)`)
	if dangling != 0 {
		t.Fatalf("%d published strategy factors name a feature that is retired or does not exist", dangling)
	}

	// And the reverse mistake: a factor with no mode, no weight, or a mode nothing implements.
	if malformed := fixture.count(`SELECT count(*) FROM strategies s,
		jsonb_array_elements(s.parameters -> 'factors') f
		WHERE f ->> 'name' IS NULL OR f ->> 'weight' IS NULL
		   OR f ->> 'mode' NOT IN ('cross_sectional', 'absolute')
		   OR f ->> 'description' IS NULL`); malformed != 0 {
		t.Fatalf("%d published strategy factors are malformed", malformed)
	}

	// An absolute factor needs a transform; without one it has no way to reach the scoring range.
	if untransformed := fixture.count(`SELECT count(*) FROM strategies s,
		jsonb_array_elements(s.parameters -> 'factors') f
		WHERE f ->> 'mode' = 'absolute' AND f -> 'transform' IS NULL`); untransformed != 0 {
		t.Fatalf("%d absolute factors have no transform", untransformed)
	}
}
