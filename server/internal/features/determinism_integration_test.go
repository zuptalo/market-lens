package features_test

import (
	"testing"
)

// SC-001: a full recomputation from the same inputs produces exactly the same stored values —
// exact numeric equality at the stored precision, not approximate equality (research R-001).
// The second run uses four workers, so instruments finish in a different order than the
// first single-worker run; a composite or value that depended on processing order would
// differ.
func TestFullRecomputationProducesZeroDifferences(t *testing.T) {
	f := newEngineFixture(t)
	computeFixture(t, f, 1)
	// Ordinary tables rather than TEMP ones: the pool hands the diff query a different
	// connection than the copy, and temp tables are per connection.
	for _, statement := range []string{
		`CREATE TABLE previous_values AS SELECT * FROM feature_values`,
		`CREATE TABLE previous_composites AS SELECT * FROM universe_composites`,
	} {
		if _, err := f.pool.Exec(f.ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	before := count(t, f, `SELECT count(*) FROM previous_values`)
	if before == 0 {
		t.Fatal("the first run stored no values; the diff would be vacuous")
	}

	_, second := computeFixture(t, f, 4)

	if n := count(t, f, `SELECT count(*) FROM feature_values v
		JOIN previous_values p USING (instrument_id, session_date, definition_id)
		WHERE v.value IS DISTINCT FROM p.value
		   OR v.label IS DISTINCT FROM p.label
		   OR v.absence_reason IS DISTINCT FROM p.absence_reason
		   OR v.currency IS DISTINCT FROM p.currency`); n != 0 {
		t.Errorf("%d values differ between two full computations of the same inputs", n)
	}
	if n := count(t, f, `SELECT count(*) FROM feature_values v
		FULL JOIN previous_values p USING (instrument_id, session_date, definition_id)
		WHERE v.run_id IS NULL OR p.run_id IS NULL`); n != 0 {
		t.Errorf("%d values exist in only one of the two computations", n)
	}
	if n := count(t, f, `SELECT count(*) FROM feature_values WHERE run_id <> $1`, second.ID.String()); n != 0 {
		t.Errorf("%d values still carry the first run: the second did not replace them", n)
	}
	if n := count(t, f, `SELECT count(*) FROM universe_composites c
		FULL JOIN previous_composites p USING (universe_id, session_date, definition_id)
		WHERE c.mean_return IS DISTINCT FROM p.mean_return
		   OR c.contributor_count IS DISTINCT FROM p.contributor_count
		   OR c.absence_reason IS DISTINCT FROM p.absence_reason`); n != 0 {
		t.Errorf("%d composite sessions differ between two full computations", n)
	}
}
