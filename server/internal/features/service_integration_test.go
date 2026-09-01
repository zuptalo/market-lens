package features_test

import (
	"context"
	"log/slog"
	"strconv"
	"testing"
	"time"

	"market-lens/server/internal/features"
)

const fixtureMemberCount = 5 + fixtureFillerCount // A, B, C, D, E and the fillers

func computeFixture(t *testing.T, f *engineFixture, workers int) (*features.Service, features.Run) {
	t.Helper()
	service := features.NewService(features.NewRepository(f.pool), slog.Default())
	run, err := service.Compute(context.Background(), features.ComputeRequest{
		Kind: features.RunKindFull, Universe: fixtureUniverse, Workers: workers, AppVersion: "test",
	})
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	return service, run
}

func readAt(t *testing.T, repository *features.Repository, instrument features.UUID, session features.SessionDate) map[string]features.Value {
	t.Helper()
	set, err := repository.ReadAsOf(context.Background(), instrument, session)
	if err != nil {
		t.Fatalf("read %s as of %s: %v", instrument, session, err)
	}
	if len(set.NotComputed) != 0 {
		t.Fatalf("%s as of %s: not computed: %v", instrument, session, set.NotComputed)
	}
	byName := map[string]features.Value{}
	for _, value := range set.Features {
		byName[value.Name] = value
	}
	return byName
}

func expectReason(t *testing.T, values map[string]features.Value, name string, reason features.AbsenceReason, note string) {
	t.Helper()
	value, ok := values[name]
	if !ok {
		t.Errorf("%s (%s): missing", name, note)
		return
	}
	if value.AbsenceReason == nil || *value.AbsenceReason != reason {
		t.Errorf("%s (%s): %+v, expected %s", name, note, describe(value), reason)
	}
}

func expectNumber(t *testing.T, values map[string]features.Value, name string, note string) string {
	t.Helper()
	value, ok := values[name]
	if !ok {
		t.Errorf("%s (%s): missing", name, note)
		return ""
	}
	if value.Value == nil {
		t.Errorf("%s (%s): %s, expected a value", name, note, describe(value))
		return ""
	}
	return *value.Value
}

func describe(value features.Value) string {
	switch {
	case value.Value != nil:
		return "value " + *value.Value
	case value.Label != nil:
		return "label " + *value.Label
	case value.AbsenceReason != nil:
		return "absent: " + string(*value.AbsenceReason)
	}
	return "empty"
}

func TestAFullRunComputesEveryInstrumentInTheUniverse(t *testing.T) {
	f := newEngineFixture(t)
	_, run := computeFixture(t, f, 1)
	repository := features.NewRepository(f.pool)
	if run.Status != features.RunStatusSucceeded || run.InstrumentCount != fixtureMemberCount || run.FinishedAt == nil {
		t.Fatalf("run = %+v, expected succeeded over %d instruments", run, fixtureMemberCount)
	}
	if n := count(t, f, `SELECT count(*) FROM feature_values WHERE run_id = $1`, run.ID.String()); int64(n) != run.ValueCount || n == 0 {
		t.Errorf("run reports %d values, the store holds %d", run.ValueCount, n)
	}

	t.Run("A matches the golden values at every golden session", func(t *testing.T) {
		golden := loadGoldenA(t)
		for date, session := range golden.Sessions {
			values := readAt(t, repository, fixtureA, features.SessionDate(date))
			for name, want := range session.Features {
				got, ok := values[name]
				if !ok {
					t.Errorf("%s@%s: missing", name, date)
					continue
				}
				if got.DefinitionVersion != 1 {
					t.Errorf("%s@%s: version %d", name, date, got.DefinitionVersion)
				}
				switch {
				case want.Value != nil && (got.Value == nil || *got.Value != *want.Value):
					t.Errorf("%s@%s: %s, golden %s", name, date, describe(got), *want.Value)
				case want.Label != nil && (got.Label == nil || *got.Label != *want.Label):
					t.Errorf("%s@%s: %s, golden label %s", name, date, describe(got), *want.Label)
				case want.AbsenceReason != nil && (got.AbsenceReason == nil || string(*got.AbsenceReason) != *want.AbsenceReason):
					t.Errorf("%s@%s: %s, golden %s", name, date, describe(got), *want.AbsenceReason)
				}
			}
			var mean *string
			var contributors int
			if err := f.pool.QueryRow(f.ctx, `SELECT mean_return::text, contributor_count FROM universe_composites
				WHERE universe_id = $1 AND session_date = $2`, fixtureUnivID.String(), date).Scan(&mean, &contributors); err != nil {
				t.Fatalf("composite at %s: %v", date, err)
			}
			if contributors != session.Composite.ContributorCount ||
				(session.Composite.Value == nil) != (mean == nil) || (mean != nil && *mean != *session.Composite.Value) {
				t.Errorf("composite@%s: %v over %d, golden %v over %d", date, mean, contributors,
					session.Composite.Value, session.Composite.ContributorCount)
			}
		}
		for date, sample := range golden.CompositeSamples {
			var mean *string
			var contributors int
			if err := f.pool.QueryRow(f.ctx, `SELECT mean_return::text, contributor_count FROM universe_composites
				WHERE universe_id = $1 AND session_date = $2`, fixtureUnivID.String(), date).Scan(&mean, &contributors); err != nil {
				t.Fatalf("composite sample at %s: %v", date, err)
			}
			if contributors != sample.ContributorCount || (sample.Value == nil) != (mean == nil) || (mean != nil && *mean != *sample.Value) {
				t.Errorf("composite sample@%s: %v over %d, golden %v over %d", date, mean, contributors, sample.Value, sample.ContributorCount)
			}
		}
	})

	t.Run("B is too young for every window longer than its history", func(t *testing.T) {
		values := readAt(t, repository, fixtureB, fixtureAsOf)
		for name, value := range values {
			if value.WindowSessions == nil {
				continue
			}
			if *value.WindowSessions > fixtureBSessions {
				expectReason(t, values, name, features.AbsenceInsufficientHistory, "B, latest")
			} else if name != "regime" {
				expectNumber(t, values, name, "B, latest")
			}
		}
		first := readAt(t, repository, fixtureB, f.sessionAtOffset(fixtureBSessions-1))
		expectReason(t, first, "return_1", features.AbsenceInsufficientHistory, "B, first session")
	})

	t.Run("C produces no rows and no error", func(t *testing.T) {
		if n := count(t, f, `SELECT count(*) FROM feature_values WHERE instrument_id = $1`, fixtureC.String()); n != 0 {
			t.Errorf("%d rows for an instrument with no history", n)
		}
		var status string
		var code *string
		if err := f.pool.QueryRow(f.ctx, `SELECT status, error_code FROM feature_run_items
			WHERE run_id = $1 AND instrument_id = $2`, run.ID.String(), fixtureC.String()).Scan(&status, &code); err != nil {
			t.Fatalf("C's item: %v", err)
		}
		if status != string(features.RunItemSkipped) || code != nil {
			t.Errorf("C's item = %s (%v), expected skipped without an error", status, code)
		}
	})

	t.Run("D reports a gap exactly where a window spans it", func(t *testing.T) {
		afterGap := readAt(t, repository, fixtureD, f.sessionAtOffset(fixtureDGapStart-1))
		expectReason(t, afterGap, "return_1", features.AbsenceWindowGap, "D, first bar after the gap")
		expectReason(t, afterGap, "sma_20", features.AbsenceWindowGap, "D, first bar after the gap")
		beforeGap := readAt(t, repository, fixtureD, f.sessionAtOffset(fixtureDGapStart+fixtureDGapLength))
		expectNumber(t, beforeGap, "return_60", "D, last bar before the gap")
		expectNumber(t, beforeGap, "sma_20", "D, last bar before the gap")
		latest := readAt(t, repository, fixtureD, fixtureAsOf)
		expectNumber(t, latest, "sma_20", "D, latest: a full window after the gap")
		expectNumber(t, latest, "return_20", "D, latest")
		expectReason(t, latest, "sma_50", features.AbsenceWindowGap, "D, latest: 50 sessions span the gap")
		expectReason(t, latest, "return_60", features.AbsenceWindowGap, "D, latest")
		expectReason(t, latest, "return_250", features.AbsenceInsufficientHistory, "D, latest: 251 sessions reach before D listed")
		expectReason(t, latest, "rsi_14", features.AbsenceInsufficientHistory, "D, latest: 140 sessions reach before D listed")
		// The listing's CTE reported a number here by ignoring the gap; the engine does not.
		if n := count(t, f, `SELECT count(*) FROM feature_values v JOIN feature_definitions d ON d.id = v.definition_id
			WHERE v.instrument_id = $1 AND d.name = 'return_20' AND v.absence_reason = 'window_gap'`, fixtureD.String()); n != 20 {
			t.Errorf("%d sessions of D have return_20 undefined by the gap, expected the 20 whose 21-session window spans it", n)
		}
	})

	t.Run("E is adjusted for its split only on the adjusted basis and states its currency", func(t *testing.T) {
		exDate := readAt(t, repository, fixtureE, f.sessionAtOffset(fixtureESplitOffset))
		position := fixtureESessions - 1 - fixtureESplitOffset
		wantAdjusted := features.Round(barAt(fixtureSeed(fixtureE), position).Close/barAt(fixtureSeed(fixtureE), position-1).Close - 1)
		if got := expectNumber(t, exDate, "return_1", "E, ex-date"); got != wantAdjusted {
			t.Errorf("E's adjusted return_1 across the ex-date = %s, expected %s", got, wantAdjusted)
		}
		raw := expectNumber(t, exDate, "return_20", "E, ex-date")
		if value, err := strconv.ParseFloat(raw, 64); err != nil || value > -0.4 {
			t.Errorf("E's raw return_20 across the ex-date = %s; the raw basis must show the halving", raw)
		}
		if currency := exDate["sma_20"].Currency; currency == nil || *currency != "SEK" {
			t.Errorf("E's sma_20 currency = %v, expected SEK", currency)
		}
		if currency := exDate["return_1"].Currency; currency != nil {
			t.Errorf("E's return_1 carries currency %s; a ratio has none", *currency)
		}
		a := readAt(t, repository, fixtureA, fixtureAsOf)
		if currency := a["atr_14"].Currency; currency == nil || *currency != "EUR" {
			t.Errorf("A's atr_14 currency = %v, expected EUR", currency)
		}
	})
}

func TestNoValueExistsForAClosedDate(t *testing.T) {
	f := newEngineFixture(t)
	computeFixture(t, f, 2)
	if n := count(t, f, `SELECT count(*) FROM feature_values v JOIN instruments i ON i.id = v.instrument_id
		LEFT JOIN exchange_sessions s ON s.exchange_id = i.exchange_id AND s.session_date = v.session_date
		WHERE s.session_date IS NULL OR s.status = 'closed'`); n != 0 {
		t.Errorf("%d values describe a date the exchange was closed", n)
	}
	if n := count(t, f, `SELECT count(*) FROM feature_values v
		LEFT JOIN daily_price_bars b ON b.instrument_id = v.instrument_id AND b.session_date = v.session_date
		WHERE b.session_date IS NULL`); n != 0 {
		t.Errorf("%d values describe a session with no stored bar", n)
	}
}

func TestAHalfDayCountsAsOneSession(t *testing.T) {
	f := newEngineFixture(t)
	computeFixture(t, f, 1)
	repository := features.NewRepository(f.pool)
	var halfDay string
	if err := f.pool.QueryRow(f.ctx, `SELECT session_date::text FROM exchange_sessions
		WHERE exchange_id = $1 AND status = 'half_day' AND session_date <= $2
		ORDER BY session_date DESC LIMIT 1`, f.exchange.String(), fixtureAsOf.String()).Scan(&halfDay); err != nil {
		t.Fatalf("the fixture calendar holds no half day: %v", err)
	}
	// Twenty sessions ending at the half day: the window is satisfied with the half day as
	// one of them, not one short.
	values := readAt(t, repository, fixtureA, features.SessionDate(halfDay))
	expectNumber(t, values, "sma_20", "A, half day")
	// The half day's position in A's series: as many sessions before the as-of as the
	// calendar holds between them.
	after := len(f.openSessions("2016-01-01", fixtureAsOf)) - len(f.openSessions("2016-01-01", features.SessionDate(halfDay)))
	want := features.Round(features.SMA(closesOf(window(seriesA(), fixtureASessions-1-after, 20))))
	if got := *values["sma_20"].Value; got != want {
		t.Errorf("sma_20 at the half day = %s, expected %s over the 20 sessions ending there", got, want)
	}
	definitions, err := repository.Definitions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sensitive := map[string]bool{}
	for _, definition := range definitions {
		sensitive[definition.Name] = definition.SessionLengthSensitive
	}
	for name, want := range map[string]bool{"volatility_20": true, "atr_14": true, "volume_sma_20": true, "return_20": false, "sma_20": false} {
		if sensitive[name] != want {
			t.Errorf("%s sessionLengthSensitive = %v, expected %v", name, sensitive[name], want)
		}
	}
}

func TestTheCompositeIsFinishedBeforeAnyInstrumentBegins(t *testing.T) {
	f := newEngineFixture(t)
	_, run := computeFixture(t, f, 4)
	var compositeDone, firstInstrument time.Time
	if err := f.pool.QueryRow(f.ctx, `SELECT max(computed_at) FROM universe_composites WHERE run_id = $1`,
		run.ID.String()).Scan(&compositeDone); err != nil {
		t.Fatalf("composite: %v", err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT min(started_at) FROM feature_run_items WHERE run_id = $1`,
		run.ID.String()).Scan(&firstInstrument); err != nil {
		t.Fatalf("items: %v", err)
	}
	if firstInstrument.Before(compositeDone) {
		t.Errorf("an instrument began at %s, before the composite was finished at %s", firstInstrument, compositeDone)
	}
	if n := count(t, f, `SELECT count(*) FROM universe_composites WHERE run_id = $1`, run.ID.String()); n != fixtureASessions {
		t.Errorf("%d composite sessions, expected one per session any member traded (%d)", n, fixtureASessions)
	}
}
