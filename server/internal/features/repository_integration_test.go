package features_test

import (
	"context"
	"errors"
	"reflect"
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

// read reads an instrument through the request form the API uses.
func read(t *testing.T, f *engineFixture, request features.ReadRequest) (features.FeatureSet, error) {
	t.Helper()
	return features.NewRepository(f.pool).Read(context.Background(), request)
}

func TestReadAsOfIsIdenticalOnRepeatAndReflectsOnlyEarlierData(t *testing.T) {
	f := newEngineFixture(t)
	computeFixture(t, f, 1)
	repository := features.NewRepository(f.pool)
	first, err := repository.ReadAsOf(context.Background(), fixtureA, fixtureAsOf)
	if err != nil {
		t.Fatal(err)
	}
	second, err := repository.ReadAsOf(context.Background(), fixtureA, fixtureAsOf)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("two reads of the same instrument and session differ:\n%+v\n%+v", first, second)
	}
	// The golden holds position 250, 69 sessions before the as-of.
	golden := goldenAt(t, loadGoldenA(t), 250)
	earlier := f.sessionAtOffset(fixtureASessions - 1 - golden.Position)
	previous := readAt(t, repository, fixtureA, earlier)
	latest := readAt(t, repository, fixtureA, fixtureAsOf)
	if previous["return_20"].SessionDate != earlier {
		t.Errorf("sessionDate = %s, expected the earlier session %s", previous["return_20"].SessionDate, earlier)
	}
	if got, want := expectNumber(t, previous, "return_20", "earlier"), golden.Features["return_20"].Value; want == nil || got != *want {
		t.Errorf("return_20 as of %s = %s, expected the golden %v for that session", earlier, got, want)
	}
	if expectNumber(t, previous, "return_20", "earlier") == expectNumber(t, latest, "return_20", "latest") {
		t.Errorf("return_20 as of %s equals the value as of %s; the earlier read did not reflect only earlier data", earlier, fixtureAsOf)
	}
}

func TestReadAsOfBeforeFirstStoredSessionIsNoHistoryNotEmptyValues(t *testing.T) {
	f := newEngineFixture(t)
	computeFixture(t, f, 1)
	// B lists at offset 19; the session before it is a stored session with no bar for B.
	before := f.sessionAtOffset(fixtureBSessions)
	set, err := read(t, f, features.ReadRequest{InstrumentID: fixtureB, AsOf: before})
	if !errors.Is(err, features.ErrNoHistory) {
		t.Fatalf("read B as of %s: set %+v, err %v; expected %v", before, set, err, features.ErrNoHistory)
	}
	// C is a member with no history at all.
	if _, err := read(t, f, features.ReadRequest{InstrumentID: fixtureC}); !errors.Is(err, features.ErrNoHistory) {
		t.Errorf("read C with no history: err %v; expected %v", err, features.ErrNoHistory)
	}
	// D has a gap: a session inside it is a stored session with no bar, but D has history
	// before it, so the read describes the previous stored session rather than refusing.
	inGap := f.sessionAtOffset(fixtureDGapStart + 1)
	set, err = read(t, f, features.ReadRequest{InstrumentID: fixtureD, AsOf: inGap})
	if err != nil {
		t.Fatalf("read D inside its gap: %v", err)
	}
	if want := f.sessionAtOffset(fixtureDGapStart + fixtureDGapLength); set.SessionDate != want {
		t.Errorf("read D as of %s describes %s, expected the latest stored session on or before it, %s", inGap, set.SessionDate, want)
	}
}

func TestReadAsOfAClosedDateIsRefused(t *testing.T) {
	f := newEngineFixture(t)
	computeFixture(t, f, 1)
	var closed string
	if err := f.pool.QueryRow(f.ctx, `SELECT session_date::text FROM exchange_sessions
		WHERE exchange_id = $1 AND status = 'closed' AND session_date <= $2
		ORDER BY session_date DESC LIMIT 1`, f.exchange.String(), fixtureAsOf.String()).Scan(&closed); err != nil {
		t.Fatalf("the fixture calendar holds no closed day: %v", err)
	}
	for _, date := range []features.SessionDate{features.SessionDate(closed), "2026-07-04", "2026-07-05"} {
		if _, err := read(t, f, features.ReadRequest{InstrumentID: fixtureA, AsOf: date}); !errors.Is(err, features.ErrClosedDate) {
			t.Errorf("read as of %s: err %v; expected %v", date, err, features.ErrClosedDate)
		}
	}
}

func TestReadAsOfDefaultsToTheLatestStoredSession(t *testing.T) {
	f := newEngineFixture(t)
	computeFixture(t, f, 1)
	set, err := read(t, f, features.ReadRequest{InstrumentID: fixtureA})
	if err != nil {
		t.Fatal(err)
	}
	if set.SessionDate != fixtureAsOf {
		t.Errorf("default read describes %s, expected the latest stored session %s", set.SessionDate, fixtureAsOf)
	}
	// After the as-of, a stored session with no bar for A yet: the read still describes the
	// latest stored session, and says so.
	set, err = read(t, f, features.ReadRequest{InstrumentID: fixtureA, AsOf: "2026-07-01"})
	if err != nil {
		t.Fatal(err)
	}
	if set.SessionDate != fixtureAsOf {
		t.Errorf("read as of a later session describes %s, expected %s", set.SessionDate, fixtureAsOf)
	}
}

func TestReadAsOfAnUnknownFeatureNamesTheOnesThatExist(t *testing.T) {
	f := newEngineFixture(t)
	computeFixture(t, f, 1)
	_, err := read(t, f, features.ReadRequest{InstrumentID: fixtureA, Features: []string{"return_20", "sharpe_ratio"}})
	if !errors.Is(err, features.ErrUnknownFeature) {
		t.Fatalf("err %v; expected %v", err, features.ErrUnknownFeature)
	}
	var unknown *features.UnknownFeatureError
	if !errors.As(err, &unknown) {
		t.Fatalf("err %T does not carry the known features", err)
	}
	if unknown.Name != "sharpe_ratio" || !reflect.DeepEqual(unknown.Known, activeDefinitionNames(t, f)) {
		t.Errorf("unknown = %+v, expected sharpe_ratio with the sorted active names", unknown)
	}
	set, err := read(t, f, features.ReadRequest{InstrumentID: fixtureA, Features: []string{"sma_20", "return_20"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Features) != 2 || set.Features[0].Name != "return_20" || set.Features[1].Name != "sma_20" {
		t.Errorf("filtered read returned %d features %+v, expected return_20 and sma_20 in name order", len(set.Features), set.Features)
	}
	if _, err := read(t, f, features.ReadRequest{InstrumentID: fixtureA, Features: []string{features.CompositeDefinitionName}}); !errors.Is(err, features.ErrUnknownFeature) {
		t.Errorf("the composite is not a per-instrument feature: err %v", err)
	}
}

func TestCurrencyDenominatedFeaturesStateTheirCurrency(t *testing.T) {
	f := newEngineFixture(t)
	computeFixture(t, f, 1)
	repository := features.NewRepository(f.pool)
	a := readAt(t, repository, fixtureA, fixtureAsOf)
	e := readAt(t, repository, fixtureE, fixtureAsOf)
	for name, want := range map[string]string{"atr_14": "SEK", "sma_20": "SEK", "sma_50": "SEK", "sma_200": "SEK"} {
		if got := e[name].Currency; got == nil || *got != want {
			t.Errorf("E %s currency = %v, expected %s", name, got, want)
		}
		if got := a[name].Currency; got == nil || *got != "EUR" {
			t.Errorf("A %s currency = %v, expected EUR", name, got)
		}
	}
	for _, name := range []string{"return_1", "return_20", "rsi_14", "regime", "volatility_20", "relative_strength_20", "volume_sma_20"} {
		if got := a[name].Currency; got != nil {
			t.Errorf("A %s carries currency %s; it is not currency-denominated", name, *got)
		}
	}
	for _, name := range []string{"relative_strength_20", "relative_strength_90"} {
		value := a[name]
		if value.ComparedTo == nil {
			t.Errorf("%s: no comparedTo; a relative-strength value names the composite it was measured against", name)
			continue
		}
		if value.ComparedTo.Composite != "universe_equal_weighted" || value.ComparedTo.Version != 1 ||
			value.ComparedTo.ContributorCount != fixtureMemberCount-1 {
			t.Errorf("%s comparedTo = %+v", name, value.ComparedTo)
		}
	}
	if a["return_20"].ComparedTo != nil {
		t.Errorf("return_20 carries comparedTo; only relative strength is measured against the composite")
	}
}

func TestListDefinitionsIncludesSupersededVersionsUnlessAsked(t *testing.T) {
	f := newEngineFixture(t)
	repository := features.NewRepository(f.pool)
	// Publish a second version of return_20 and supersede the first, the way a migration would.
	if _, err := f.pool.Exec(f.ctx, `UPDATE feature_definitions SET superseded_at = now() WHERE name = 'return_20' AND version = 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO feature_definitions
		(id, name, version, window_sessions, price_basis, parameters, undefined_conditions, session_length_sensitive, published_at)
		SELECT gen_random_uuid(), name, 2, window_sessions, price_basis, parameters, undefined_conditions, session_length_sensitive, now()
		FROM feature_definitions WHERE name = 'return_20' AND version = 1`); err != nil {
		t.Fatal(err)
	}
	all, err := repository.ListDefinitions(f.ctx, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 26 { // 25 seeded, including the composite, plus the new version
		t.Errorf("listed %d definitions, expected 26 including the superseded one", len(all))
	}
	for index := 1; index < len(all); index++ {
		previous, current := all[index-1], all[index]
		if previous.Name > current.Name || (previous.Name == current.Name && previous.Version >= current.Version) {
			t.Errorf("definitions are not ordered by name then version at %d: %s v%d after %s v%d",
				index, current.Name, current.Version, previous.Name, previous.Version)
		}
	}
	byName, err := repository.ListDefinitions(f.ctx, "return_20", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(byName) != 2 || byName[0].Version != 1 || byName[0].SupersededAt == nil || byName[1].Version != 2 || byName[1].SupersededAt != nil {
		t.Errorf("return_20 versions = %+v, expected v1 superseded and v2 current", byName)
	}
	active, err := repository.ListDefinitions(f.ctx, "return_20", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].Version != 2 {
		t.Errorf("active return_20 = %+v, expected only v2", active)
	}
	none, err := repository.ListDefinitions(f.ctx, "sharpe_ratio", true)
	if err != nil || len(none) != 0 {
		t.Errorf("unknown name: %d definitions, err %v; expected an empty list", len(none), err)
	}
}
