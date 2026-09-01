package features_test

import (
	"testing"
	"time"

	"market-lens/server/internal/features"
)

// SC-006 / research R-008: a full computation from empty over the curated universe must
// finish within ten minutes on the deployment's own hardware. The fixture holds about
// 3,200 bars against production's ~244,000 — roughly 1.3% — so the budget here is that same
// ten minutes scaled down: eight seconds. It is deliberately the plain scaling and not the
// ~1.7 s this actually takes, because `go test ./...` runs the database-heavy packages
// concurrently and a budget pinned to the quiet measurement would fail on contention rather
// than on a defect. What it still catches is what matters: an algorithm that stops being
// linear in bars. The real figure is the production one, recorded against R-008 (T094).
func TestAFullFixtureComputationStaysWithinItsScaledBudget(t *testing.T) {
	const budget = 8 * time.Second
	f := newEngineFixture(t)
	bars := count(t, f, `SELECT count(*) FROM daily_price_bars`)
	started := time.Now()
	_, run := computeFixture(t, f, 4)
	elapsed := time.Since(started)
	t.Logf("full computation: %d bars, %d values, %d instruments in %s (budget %s; production budget 10m over ~244k bars)",
		bars, run.ValueCount, run.InstrumentCount, elapsed.Round(time.Millisecond), budget)
	if elapsed > budget {
		t.Errorf("full fixture computation took %s, over the %s budget", elapsed.Round(time.Millisecond), budget)
	}
}

// SC-007: reading one instrument's features as of a session is bounded by the same two-second
// budget the Markets listing's first page already enforces.
func TestReadingOneInstrumentsFeaturesStaysWithinTheListingsBudget(t *testing.T) {
	const budget = 2 * time.Second
	f := newEngineFixture(t)
	computeFixture(t, f, 4)
	repository := features.NewRepository(f.pool)
	started := time.Now()
	set, err := repository.ReadAsOf(f.ctx, fixtureA, fixtureAsOf)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("read %d features as of %s in %s (budget %s)", len(set.Features), fixtureAsOf, elapsed.Round(time.Microsecond), budget)
	if elapsed > budget {
		t.Errorf("reading one instrument took %s, over the %s budget", elapsed.Round(time.Millisecond), budget)
	}
}

// SC-006's incremental bound is thirty seconds in production. The worst incremental pass is
// a bar revised deep in one instrument's history: that instrument recomputes 24 definitions
// over 251 sessions (6,024 values) and every other member recomputes its two composite-using
// definitions over 91 (182 each), so a ~250-instrument universe writes about 51,000 values
// against this fixture's 7,502 — roughly seven times as many. The fixture's share of the
// thirty seconds is therefore about four seconds, and that scaling is the budget, for the
// same reason as above. The pass itself measures ~350 ms, nearly all of it the database
// write of 6,024 rows (~220 ms; COPY is no faster — the cost is the three foreign keys and
// two indexes), so what this guards is the recomputation scope, which is what a regression
// would widen.
func TestAnIncrementalPassStaysWithinItsScaledBudget(t *testing.T) {
	f := newEngineFixture(t)
	computeKind(t, f, features.ComputeRequest{Kind: features.RunKindFull, Workers: 4})
	const position = 40
	revised := f.sessionAtOffset(fixtureASessions - 1 - position)
	importRun := f.newImportRun("ffffffff-0013-4000-8000-00000000a005")
	f.reviseBar(fixtureA, importRun, revised, fixtureBar(fixtureSeed(fixtureA), position).closeCents+2500)

	started := time.Now()
	run := computeKind(t, f, features.ComputeRequest{Kind: features.RunKindIncremental, SinceRun: importRun, Workers: 4})
	elapsed := time.Since(started)
	const budget = 4 * time.Second
	t.Logf("incremental pass: instruments=%d values=%d elapsed=%s (budget %s; production budget 30s over ~51,000 values)",
		run.InstrumentCount, run.ValueCount, elapsed.Round(time.Millisecond), budget)
	if elapsed > budget {
		t.Errorf("incremental pass took %s, over the %s budget", elapsed.Round(time.Millisecond), budget)
	}
}
