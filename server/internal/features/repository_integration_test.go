package features_test

import (
	"context"
	"testing"
	"time"

	"market-lens/server/internal/features"
)

// activeDefinitionNames reads the definitions the way a reader would verify them by hand:
// straight from the table, excluding only the composite, which is a per-universe series
// and never appears in a per-instrument read.
func activeDefinitionNames(t *testing.T, f *engineFixture) []string {
	t.Helper()
	rows, err := f.pool.Query(f.ctx, `SELECT name FROM feature_definitions
		WHERE superseded_at IS NULL AND name <> $1 ORDER BY name`, features.CompositeDefinitionName)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	return names
}

func TestReadingAnInstrumentAsOfASessionReturnsEveryDefinedFeatureWithItsVersion(t *testing.T) {
	f := newEngineFixture(t)
	repository := features.NewRepository(f.pool)
	computeFixture(t, f, 1)

	set, err := repository.ReadAsOf(context.Background(), fixtureA, fixtureAsOf)
	if err != nil {
		t.Fatalf("read as of %s: %v", fixtureAsOf, err)
	}

	expected := activeDefinitionNames(t, f)
	if len(expected) != 24 {
		t.Fatalf("the fixture seeds %d per-instrument definitions, expected 24", len(expected))
	}
	if len(set.NotComputed) != 0 {
		t.Errorf("notComputed should be empty after computation, got %d: %v", len(set.NotComputed), set.NotComputed)
	}
	byName := map[string]features.Value{}
	for _, value := range set.Features {
		byName[value.Name] = value
	}
	if len(byName) != len(expected) {
		t.Errorf("the read carries %d features, expected one per active definition (%d)", len(byName), len(expected))
	}
	for _, name := range expected {
		value, ok := byName[name]
		if !ok {
			t.Errorf("%s: no value and no absence returned", name)
			continue
		}
		if value.DefinitionVersion != 1 {
			t.Errorf("%s: definition version %d, expected 1", name, value.DefinitionVersion)
		}
		settled := 0
		if value.Value != nil {
			settled++
		}
		if value.Label != nil {
			settled++
		}
		if value.AbsenceReason != nil {
			settled++
		}
		if settled != 1 {
			t.Errorf("%s: expected exactly one of value, label and absence reason, got %d", name, settled)
		}
	}
}

func definitionID(t *testing.T, f *engineFixture, name string) features.UUID {
	t.Helper()
	var id string
	if err := f.pool.QueryRow(f.ctx, `SELECT id::text FROM feature_definitions
		WHERE name = $1 AND superseded_at IS NULL`, name).Scan(&id); err != nil {
		t.Fatalf("definition %s: %v", name, err)
	}
	return features.UUID(id)
}

func count(t *testing.T, f *engineFixture, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(f.ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v\n%s", err, sql)
	}
	return n
}

func newFeatureRun(t *testing.T, repository *features.Repository, id features.UUID) features.Run {
	t.Helper()
	run := features.Run{ID: id, Kind: features.RunKindFull, Status: features.RunStatusRunning, UniverseID: fixtureUnivID,
		StartedAt: time.Now().UTC(), AppVersion: "test"}
	if err := repository.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("create run: %v", err)
	}
	return run
}

func TestAnInstrumentsValuesCommitAsOneTransactionWithItsEvent(t *testing.T) {
	f := newEngineFixture(t)
	repository := features.NewRepository(f.pool)
	run := newFeatureRun(t, repository, "ffffffff-0013-4000-8000-0000000000b1")
	from, to := f.sessionAtOffset(1), f.sessionAtOffset(0)
	value, label := "0.012300000000", "trending_up"
	reason := features.AbsenceInsufficientHistory
	rows := []features.ValueRow{
		{DefinitionID: definitionID(t, f, "return_1"), Session: to, Value: &value},
		{DefinitionID: definitionID(t, f, "regime"), Session: to, Label: &label},
		{DefinitionID: definitionID(t, f, "return_1"), Session: from, AbsenceReason: &reason},
	}
	item := features.RunItem{RunID: run.ID, InstrumentID: fixtureA, Status: features.RunItemSucceeded,
		FromSession: &from, ToSession: &to, ValueCount: 3, StartedAt: run.StartedAt}
	change := features.Change{InstrumentID: fixtureA, FromSession: from, ToSession: to, RunID: run.ID}

	t.Run("a forced failure after the values are staged leaves none of the three", func(t *testing.T) {
		scope, err := repository.BeginInstrumentScope(f.ctx, fixtureA)
		if err != nil {
			t.Fatal(err)
		}
		if err := scope.WriteValues(f.ctx, run.ID, from, to, rows); err != nil {
			t.Fatalf("stage values: %v", err)
		}
		if err := scope.WriteItem(f.ctx, item); err != nil {
			t.Fatalf("stage item: %v", err)
		}
		both := features.ValueRow{DefinitionID: rows[0].DefinitionID, Session: from, Value: &value, Label: &label}
		if err := scope.WriteValues(f.ctx, run.ID, from, from, []features.ValueRow{both}); err == nil {
			t.Fatal("a row with both a value and a label must be refused by the store")
		}
		_ = scope.Rollback(f.ctx)
		if n := count(t, f, `SELECT count(*) FROM feature_values WHERE instrument_id = $1`, fixtureA.String()); n != 0 {
			t.Errorf("%d feature_values rows survived the rollback", n)
		}
		if n := count(t, f, `SELECT count(*) FROM feature_run_items WHERE run_id = $1`, run.ID.String()); n != 0 {
			t.Errorf("%d feature_run_items rows survived the rollback", n)
		}
		if n := count(t, f, `SELECT count(*) FROM client_events WHERE event_type = 'feature_values.changed.v1'`); n != 0 {
			t.Errorf("%d events survived the rollback", n)
		}
	})

	t.Run("a committed scope holds the values, the item and exactly one event", func(t *testing.T) {
		scope, err := repository.BeginInstrumentScope(f.ctx, fixtureA)
		if err != nil {
			t.Fatal(err)
		}
		if err := scope.WriteValues(f.ctx, run.ID, from, to, rows); err != nil {
			t.Fatalf("write values: %v", err)
		}
		if err := scope.WriteItem(f.ctx, item); err != nil {
			t.Fatalf("write item: %v", err)
		}
		if err := scope.Commit(f.ctx, change); err != nil {
			t.Fatalf("commit: %v", err)
		}
		if n := count(t, f, `SELECT count(*) FROM feature_values WHERE instrument_id = $1 AND run_id = $2`,
			fixtureA.String(), run.ID.String()); n != 3 {
			t.Errorf("%d feature_values rows, expected 3", n)
		}
		if n := count(t, f, `SELECT count(*) FROM feature_values WHERE instrument_id = $1 AND label = 'trending_up'`,
			fixtureA.String()); n != 1 {
			t.Errorf("%d labelled rows, expected the regime's one", n)
		}
		if n := count(t, f, `SELECT count(*) FROM feature_run_items
			WHERE run_id = $1 AND instrument_id = $2 AND status = 'succeeded' AND value_count = 3`,
			run.ID.String(), fixtureA.String()); n != 1 {
			t.Errorf("%d item rows, expected 1", n)
		}
		var payload map[string]any
		var scope_, entityType, entityID string
		if err := f.pool.QueryRow(f.ctx, `SELECT scope, entity_type, entity_id, payload FROM client_events
			WHERE event_type = 'feature_values.changed.v1' AND version = 1`).Scan(&scope_, &entityType, &entityID, &payload); err != nil {
			t.Fatalf("exactly one feature_values.changed.v1 event: %v", err)
		}
		if scope_ != "shared" || entityType != "instrument" || entityID != fixtureA.String() {
			t.Errorf("event scope/entity = %s %s %s", scope_, entityType, entityID)
		}
		for key, want := range map[string]string{"instrument_id": fixtureA.String(), "from_session": from.String(),
			"to_session": to.String(), "run_id": run.ID.String()} {
			if payload[key] != want {
				t.Errorf("payload[%s] = %v, expected %s", key, payload[key], want)
			}
		}
	})

	t.Run("writing the range again replaces rather than duplicates", func(t *testing.T) {
		scope, err := repository.BeginInstrumentScope(f.ctx, fixtureA)
		if err != nil {
			t.Fatal(err)
		}
		if err := scope.WriteValues(f.ctx, run.ID, from, to, rows[:1]); err != nil {
			t.Fatalf("write values: %v", err)
		}
		if err := scope.Commit(f.ctx, change); err != nil {
			t.Fatalf("commit: %v", err)
		}
		if n := count(t, f, `SELECT count(*) FROM feature_values WHERE instrument_id = $1`, fixtureA.String()); n != 1 {
			t.Errorf("%d rows after rewriting the range with one, expected 1", n)
		}
	})
}

func TestTwoRecomputationsOfOneInstrumentSerialise(t *testing.T) {
	f := newEngineFixture(t)
	repository := features.NewRepository(f.pool)
	first, err := repository.BeginInstrumentScope(f.ctx, fixtureA)
	if err != nil {
		t.Fatal(err)
	}
	acquired := make(chan error, 1)
	go func() {
		second, err := repository.BeginInstrumentScope(f.ctx, fixtureA)
		if err == nil {
			err = second.Rollback(f.ctx)
		}
		acquired <- err
	}()
	select {
	case err := <-acquired:
		t.Fatalf("the second scope was acquired while the first was open (err %v)", err)
	case <-time.After(300 * time.Millisecond):
	}
	// Another instrument is not held up by A's lock.
	other, err := repository.BeginInstrumentScope(f.ctx, fixtureB)
	if err != nil {
		t.Fatalf("scope on B while A is held: %v", err)
	}
	_ = other.Rollback(f.ctx)
	if err := first.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-acquired:
		if err != nil {
			t.Fatalf("second scope: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the second scope never acquired the lock after the first released it")
	}
}
