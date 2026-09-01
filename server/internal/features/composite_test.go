package features_test

import (
	"math/rand/v2"
	"testing"

	"market-lens/server/internal/features"
)

// universeSeries rebuilds every listed fixture instrument's bars in memory, keyed by offset
// back from the as-of session, exactly as the fixture stores them (E after its split, D
// around its gap, B for its twenty sessions).
func universeSeries() map[features.UUID]map[int]features.Bar {
	build := func(seed, count int, skip map[int]bool) map[int]features.Bar {
		out := map[int]features.Bar{}
		for position := range count {
			offset := count - 1 - position
			if skip[offset] {
				continue
			}
			out[offset] = barAt(seed, position)
		}
		return out
	}
	series := map[features.UUID]map[int]features.Bar{
		fixtureA: build(fixtureSeed(fixtureA), fixtureASessions, nil),
		fixtureB: build(fixtureSeed(fixtureB), fixtureBSessions, nil),
		fixtureD: build(fixtureSeed(fixtureD), fixtureDSessions, map[int]bool{
			fixtureDGapStart: true, fixtureDGapStart + 1: true, fixtureDGapStart + 2: true}),
		fixtureE: build(fixtureSeed(fixtureE), fixtureESessions, nil),
	}
	for index := range fixtureFillerCount {
		series[fixtureFiller(index)] = build(fixtureSeed(fixtureFiller(index)), fixtureFillerSessions, nil)
	}
	return series
}

// contributorsAt selects, per the composite's definition, every instrument with a bar at
// the offset and at the one before it. E is read post-split here, which is what the engine
// sees at every offset inside the last thirty sessions.
func contributorsAt(series map[features.UUID]map[int]features.Bar, offset int) []features.Contributor {
	var contributors []features.Contributor
	for id, bars := range series {
		now, ok := bars[offset]
		if !ok {
			continue
		}
		previous, ok := bars[offset+1]
		if !ok {
			continue
		}
		contributors = append(contributors, features.Contributor{InstrumentID: id, Return: now.Close/previous.Close - 1})
	}
	return contributors
}

func TestTheCompositeIsTheEqualWeightedMeanOverContributorsInIdOrder(t *testing.T) {
	golden := loadGoldenA(t)
	series := universeSeries()
	latest := goldenAt(t, golden, 319)
	contributors := contributorsAt(series, 0)
	if len(contributors) != 12 {
		t.Fatalf("%d contributors at the latest session, expected 12", len(contributors))
	}
	mean, count, reason := features.Composite(contributors, 10)
	if reason != "" || count != latest.Composite.ContributorCount || features.Round(mean) != *latest.Composite.Value {
		t.Errorf("composite = %s over %d (%q), expected %s over %d",
			features.Round(mean), count, reason, *latest.Composite.Value, latest.Composite.ContributorCount)
	}

	// Iteration order over a map is random; the composite must not be.
	for attempt := range 5 {
		shuffled := append([]features.Contributor{}, contributors...)
		rand.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		again, _, _ := features.Composite(shuffled, 10)
		if again != mean {
			t.Fatalf("attempt %d: a different contributor order produced %v instead of %v", attempt, again, mean)
		}
	}
}

func TestTheCompositeExcludesInstrumentsMissingEitherBar(t *testing.T) {
	golden := loadGoldenA(t)
	series := universeSeries()
	// B is missing the bar before its first session; D is missing its gap sessions. The
	// golden samples record the resulting counts at those offsets.
	for offset, want := range map[int]int{19: 11, 20: 11, 39: 10, 40: 10, 43: 11, 44: 11} {
		if got := len(contributorsAt(series, offset)); got != want {
			t.Errorf("offset %d: %d contributors, expected %d", offset, got, want)
		}
	}
	// The golden samples are keyed by XSTO session date; these are the offsets back from
	// the as-of session that those dates sit at in the fixture calendar.
	sampleOffsets := map[string]int{
		"2026-06-30": 0, "2026-05-04": 39, "2026-04-30": 40, "2026-04-27": 43, "2026-04-24": 44,
		"2025-09-10": 198, "2025-09-09": 199, "2025-09-08": 200,
	}
	for date, sample := range golden.CompositeSamples {
		offset, ok := sampleOffsets[date]
		if !ok {
			t.Fatalf("golden sample %s has no known offset", date)
		}
		mean, count, reason := features.Composite(contributorsAt(series, offset), 10)
		if count != sample.ContributorCount {
			t.Errorf("%s (offset %d): %d contributors, golden %d", date, offset, count, sample.ContributorCount)
		}
		switch {
		case sample.Value == nil && reason != features.CompositeAbsenceInsufficientContributors:
			t.Errorf("%s (offset %d): %s over %d (%q), golden undefined", date, offset, features.Round(mean), count, reason)
		case sample.Value != nil && (reason != "" || features.Round(mean) != *sample.Value):
			t.Errorf("%s (offset %d): %s (%q), golden %s", date, offset, features.Round(mean), reason, *sample.Value)
		}
	}
	// Ten contributors satisfy a minimum of ten: the edge of D's gap is defined.
	if _, count, reason := features.Composite(contributorsAt(series, 39), 10); reason != "" || count != 10 {
		t.Fatalf("at the edge of D's gap: %d (%q)", count, reason)
	}
}

func TestTheCompositeBelowTheMinimumIsUndefinedWithItsCountRecorded(t *testing.T) {
	series := universeSeries()
	contributors := contributorsAt(series, 219) // before E, D and B list: A and eight fillers
	if len(contributors) != 9 {
		t.Fatalf("%d contributors, expected 9", len(contributors))
	}
	_, count, reason := features.Composite(contributors, 10)
	if reason != features.CompositeAbsenceInsufficientContributors || count != 9 {
		t.Errorf("composite over 9 = (%q, %d), expected %s with the count recorded",
			reason, count, features.CompositeAbsenceInsufficientContributors)
	}
}
