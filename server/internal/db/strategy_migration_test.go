package db

import (
	"context"
	"encoding/json"
	"testing"

	"market-lens/server/internal/testdb"
)

// Feature 015. A strategy version is reference data published by migration: its factors must
// name features that exist, its action bands must cover the scoring range without gaps or
// overlaps, and the signal table must refuse a row that is neither a view nor a stated absence.
func TestStrategyMigrationPublishesOneUsableVersion(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	var name, title, intent, caveat string
	var version int
	var parameters []byte
	if err := pool.QueryRow(ctx, `SELECT name, version, title, intent, caveat, parameters
		FROM strategies WHERE superseded_at IS NULL`).
		Scan(&name, &version, &title, &intent, &caveat, &parameters); err != nil {
		t.Fatalf("read the current strategy: %v", err)
	}
	if name == "" || version < 1 || title == "" || intent == "" || caveat == "" {
		t.Fatalf("the published version is incomplete: %s v%d %q %q %q", name, version, title, intent, caveat)
	}

	var document struct {
		Factors []struct {
			Name    string `json:"name"`
			Feature string `json:"feature"`
			Mode    string `json:"mode"`
			Weight  string `json:"weight"`
		} `json:"factors"`
		ActionBands []struct {
			Lower  string `json:"lower"`
			Upper  string `json:"upper"`
			Action string `json:"action"`
		} `json:"action_bands"`
	}
	if err := json.Unmarshal(parameters, &document); err != nil {
		t.Fatalf("the version's parameters are not readable: %v", err)
	}
	if len(document.Factors) < 5 {
		t.Errorf("the strategy has %d factors; the specification names seven candidates", len(document.Factors))
	}

	// Every factor must read a definition that exists and is current — a strategy quietly
	// reading a retired definition would produce absences nobody could explain.
	for _, factor := range document.Factors {
		var published bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM feature_definitions
			WHERE name = $1 AND superseded_at IS NULL)`, factor.Feature).Scan(&published); err != nil {
			t.Fatal(err)
		}
		if !published {
			t.Errorf("factor %q reads %q, which is not a published feature definition", factor.Name, factor.Feature)
		}
		if factor.Mode != "cross_sectional" && factor.Mode != "absolute" {
			t.Errorf("factor %q has mode %q", factor.Name, factor.Mode)
		}
	}

	// Bands must be contiguous and cover [-1, 1], so every score maps to exactly one action.
	if len(document.ActionBands) < 2 {
		t.Fatalf("the strategy declares %d action bands", len(document.ActionBands))
	}
	if document.ActionBands[0].Lower != "-1" {
		t.Errorf("the lowest band starts at %q, expected -1", document.ActionBands[0].Lower)
	}
	if last := document.ActionBands[len(document.ActionBands)-1]; last.Upper != "1" {
		t.Errorf("the highest band ends at %q, expected 1", last.Upper)
	}
	for index := 1; index < len(document.ActionBands); index++ {
		if document.ActionBands[index].Lower != document.ActionBands[index-1].Upper {
			t.Errorf("bands %d and %d neither meet nor overlap: %q then %q", index-1, index,
				document.ActionBands[index-1].Upper, document.ActionBands[index].Lower)
		}
	}
}

// The constraint, not the convention, is what stops a HOLD standing in for missing data.
func TestASignalIsEitherAViewOrAStatedAbsence(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var strategyID, instrumentID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM strategies WHERE superseded_at IS NULL`).Scan(&strategyID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT id::text FROM instruments ORDER BY id LIMIT 1`).Scan(&instrumentID); err != nil {
		t.Fatal(err)
	}
	runID := mustNewUUID(t)
	var universeID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM research_universes ORDER BY code LIMIT 1`).Scan(&universeID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO strategy_runs
		(id, strategy_id, kind, status, universe_id, started_at, app_version)
		VALUES ($1,$2,'full','running',$3,now(),'test')`, runID, strategyID, universeID); err != nil {
		t.Fatalf("insert strategy run: %v", err)
	}

	insert := func(score, action, confidence, absence any) error {
		_, err := pool.Exec(ctx, `INSERT INTO signals
			(instrument_id, session_date, strategy_id, score, action, confidence, absence_reason,
			 contributions, divisor, computed_at, run_id)
			VALUES ($1,'2026-06-30',$2,$3,$4,$5,$6,'[]'::jsonb,NULL,now(),$7)
			ON CONFLICT (instrument_id, session_date, strategy_id) DO UPDATE
			SET score = excluded.score, action = excluded.action,
			    confidence = excluded.confidence, absence_reason = excluded.absence_reason`,
			instrumentID, strategyID, score, action, confidence, absence, runID)
		return err
	}

	if err := insert("0.25", "BUY", "0.80", nil); err != nil {
		t.Errorf("a scored signal was refused: %v", err)
	}
	if err := insert(nil, nil, nil, "insufficient_history"); err != nil {
		t.Errorf("a stated absence was refused: %v", err)
	}
	// The two states this feature exists to keep apart.
	if err := insert(nil, "HOLD", nil, nil); err == nil {
		t.Error("a HOLD with no score and no reason was stored — that is an absence wearing a view")
	}
	if err := insert("0.25", "BUY", "0.80", "insufficient_history"); err == nil {
		t.Error("a signal was stored as both scored and absent")
	}
	if err := insert("1.5", "BUY", "0.80", nil); err == nil {
		t.Error("a score outside the scoring range was stored")
	}
	if err := insert("0.25", "MAYBE", "0.80", nil); err == nil {
		t.Error("an action outside the stated vocabulary was stored")
	}
}
