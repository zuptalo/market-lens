package features_test

import (
	"testing"

	"market-lens/server/internal/features"
	"market-lens/server/internal/instruments"
)

// SC-003: every stored value names a definition that exists. The value table carries the
// definition id, not the name, so an orphan row would be a number nobody can interpret.
// This is the quickstart's SC-003 query, run over the computed fixture.
func TestEveryFeatureValueResolvesToAPublishedDefinition(t *testing.T) {
	f := newEngineFixture(t)
	computeFixture(t, f, 4)
	if n := count(t, f, `SELECT count(*) FROM feature_values v
		LEFT JOIN feature_definitions d ON d.id = v.definition_id
		WHERE d.id IS NULL`); n != 0 {
		t.Errorf("%d values name a definition that does not exist", n)
	}
	if n := count(t, f, `SELECT count(*) FROM feature_values v
		JOIN feature_definitions d ON d.id = v.definition_id
		WHERE d.published_at IS NULL`); n != 0 {
		t.Errorf("%d values name an unpublished definition", n)
	}
	// The same holds for the composite, which is a definition like any other.
	if n := count(t, f, `SELECT count(*) FROM universe_composites c
		LEFT JOIN feature_definitions d ON d.id = c.definition_id
		WHERE d.id IS NULL OR d.published_at IS NULL`); n != 0 {
		t.Errorf("%d composite sessions name no published definition", n)
	}
}

// SC-004: a value exists only where the exchange was open and the instrument has a stored
// bar. A row on a closed date would be an observation of a day that never traded; a row on a
// gapped session would be an observation with nothing behind it.
func TestNoFeatureValueExistsForAClosedOrGappedSession(t *testing.T) {
	f := newEngineFixture(t)
	computeFixture(t, f, 4)
	// The quickstart's SC-004 query, with the half day counted as the session it is.
	if n := count(t, f, `SELECT count(*) FROM feature_values v
		JOIN instruments i ON i.id = v.instrument_id
		LEFT JOIN exchange_sessions s
		  ON s.exchange_id = i.exchange_id AND s.session_date = v.session_date
		WHERE s.session_date IS NULL OR s.status NOT IN ('open', 'half_day')`); n != 0 {
		t.Errorf("%d values fall on a date the exchange did not trade", n)
	}
	if n := count(t, f, `SELECT count(*) FROM feature_values v
		LEFT JOIN daily_price_bars b
		  ON b.instrument_id = v.instrument_id AND b.session_date = v.session_date
		WHERE b.instrument_id IS NULL`); n != 0 {
		t.Errorf("%d values fall on a session with no stored bar", n)
	}
	// D's three-session gap is the case that matters: sessions the exchange was open for,
	// inside an instrument's history, with no bar of its own.
	for _, offset := range []int{40, 41, 42} {
		session := f.sessionAtOffset(offset)
		if n := count(t, f, `SELECT count(*) FROM feature_values WHERE instrument_id = $1 AND session_date = $2`,
			fixtureD.String(), session.String()); n != 0 {
			t.Errorf("D carries %d values at %s, which is inside its gap", n, session)
		}
	}
	// A composite session, by contrast, exists for every session the universe traded.
	if n := count(t, f, `SELECT count(*) FROM universe_composites c
		LEFT JOIN exchange_sessions s ON s.session_date = c.session_date AND s.exchange_id = $1
		WHERE s.session_date IS NULL OR s.status NOT IN ('open', 'half_day')`, f.exchange.String()); n != 0 {
		t.Errorf("%d composite sessions fall on a date the exchange did not trade", n)
	}
}

// SC-005: an unsatisfied window is absent, everywhere, and never a zero. Instrument B has
// twenty stored sessions, so every definition needing twenty-one or more must say why it has
// no number rather than print one. A zero here would read as "no movement" — a claim about
// the market rather than about the history — which is the failure this feature exists to end.
func TestAnUncomputableStatisticIsAbsentInEverySurface(t *testing.T) {
	f := newEngineFixture(t)
	computeFixture(t, f, 4)

	set, err := features.NewRepository(f.pool).ReadAsOf(f.ctx, fixtureB, fixtureAsOf)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Features) == 0 {
		t.Fatal("B has no features at all")
	}
	absent := 0
	for _, value := range set.Features {
		if value.WindowSessions == nil || *value.WindowSessions <= fixtureBSessions {
			continue
		}
		absent++
		if value.AbsenceReason == nil || *value.AbsenceReason != features.AbsenceInsufficientHistory {
			t.Errorf("%s over %d sessions: absence reason %v, expected insufficient_history",
				value.Name, *value.WindowSessions, value.AbsenceReason)
		}
		if value.Value != nil || value.Label != nil {
			t.Errorf("%s carries value %v label %v although its window is unsatisfied", value.Name, value.Value, value.Label)
		}
	}
	if absent < 15 {
		t.Fatalf("only %d of B's features have an unsatisfied window; the fixture no longer proves the point", absent)
	}

	// The Markets listing is the surface a person actually reads first.
	page, err := instruments.NewRepository(f.pool).Listing(f.ctx, instruments.ListingFilter{ID: instruments.UUID(fixtureB), Limit: 1, AsOf: instruments.SessionDate(fixtureAsOf)})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("listing returned %d rows for B", len(page.Items))
	}
	row := page.Items[0]
	if row.Return20 != nil || row.Return90 != nil || row.Volatility != nil {
		t.Errorf("B lists return_20 %v, return_90 %v, volatility %v; each must be absent",
			row.Return20, row.Return90, row.Volatility)
	}
	if row.StoredSessions != int64(fixtureBSessions) {
		t.Errorf("B lists %d stored sessions, expected %d — the reader must be able to see why", row.StoredSessions, fixtureBSessions)
	}
}
