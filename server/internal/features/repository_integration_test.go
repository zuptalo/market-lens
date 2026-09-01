package features_test

import (
	"context"
	"testing"

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
