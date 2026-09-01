package features_test

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"market-lens/server/internal/features"
)

// goldenSession is one hand-checked session of instrument A from testdata/golden_A.json,
// produced by a script outside the repository from the same integer generator the fixture
// uses (fixtureBar). Comparing the engine to it is comparing two implementations, not the
// engine to itself.
type goldenSession struct {
	Position  int                     `json:"position"`
	Offset    int                     `json:"offset"`
	Note      string                  `json:"note"`
	Composite goldenComposite         `json:"composite_return_1"`
	Features  map[string]goldenResult `json:"features"`
}

type goldenComposite struct {
	Value            *string `json:"value"`
	AbsenceReason    *string `json:"absence_reason"`
	ContributorCount int     `json:"contributor_count"`
}

type goldenResult struct {
	Value         *string `json:"value"`
	Label         *string `json:"label"`
	AbsenceReason *string `json:"absence_reason"`
}

type goldenFile struct {
	AsOf             string                     `json:"as_of"`
	Sessions         map[string]goldenSession   `json:"sessions"`
	CompositeSamples map[string]goldenComposite `json:"composite_samples"`
}

func loadGoldenA(t *testing.T) goldenFile {
	t.Helper()
	raw, err := os.ReadFile("testdata/golden_A.json")
	if err != nil {
		t.Fatalf("read golden values: %v", err)
	}
	var golden goldenFile
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatalf("parse golden values: %v", err)
	}
	return golden
}

// goldenAt returns the golden session at a position of instrument A's series.
func goldenAt(t *testing.T, golden goldenFile, position int) goldenSession {
	t.Helper()
	for _, session := range golden.Sessions {
		if session.Position == position {
			return session
		}
	}
	t.Fatalf("no golden session at position %d", position)
	return goldenSession{}
}

// seriesA rebuilds instrument A's bars in memory from the generator, oldest first, with
// synthetic session dates: the pure definition functions take bars, not a calendar.
func seriesA() []features.Bar {
	bars := make([]features.Bar, 0, fixtureASessions)
	for position := range fixtureASessions {
		bars = append(bars, barAt(fixtureSeed(fixtureA), position))
	}
	return bars
}

func barAt(seed, position int) features.Bar {
	generated := fixtureBar(seed, position)
	volume := generated.volume
	if fixtureASessions-1-position == fixtureZeroVolumeOffset {
		volume = 0
	}
	return features.Bar{
		Session: features.SessionDate(fmt.Sprintf("p%04d", position)),
		Open:    float64(generated.openCents) / 100,
		High:    float64(generated.highCents) / 100,
		Low:     float64(generated.lowCents) / 100,
		Close:   float64(generated.closeCents) / 100,
		Volume:  volume,
	}
}

// window returns the n bars ending at position, the way a satisfied window would.
func window(bars []features.Bar, position, n int) []features.Bar {
	return bars[position-n+1 : position+1]
}

func closesOf(bars []features.Bar) []float64 {
	closes := make([]float64, len(bars))
	for index, bar := range bars {
		closes[index] = bar.Close
	}
	return closes
}

// expectValue asserts a computed number rounds to the golden string.
func expectValue(t *testing.T, name string, got float64, reason features.AbsenceReason, want goldenResult) {
	t.Helper()
	if want.Value == nil {
		t.Errorf("%s: golden has no value (%+v); the test asked for one", name, want)
		return
	}
	if reason != "" {
		t.Errorf("%s: undefined (%s), expected %s", name, reason, *want.Value)
		return
	}
	if rounded := features.Round(got); rounded != *want.Value {
		t.Errorf("%s: %s, expected %s", name, rounded, *want.Value)
	}
}
