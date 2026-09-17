package risk

import (
	"strconv"
	"testing"

	"market-lens/server/internal/portfolio"
)

// The measurement is tested against answers worked out by hand, because a percentage is the easiest
// thing in this feature to compute plausibly and wrongly, and a test that agreed with the
// implementation would catch nothing.

func holding(ticker, sector, mic, value string) portfolio.Holding {
	amount := value
	return portfolio.Holding{
		Ticker: ticker, Name: ticker + " AB", Sector: sector, SectorName: sector, MIC: mic,
		Quantity: "1", Cost: value,
		Valuation: portfolio.Valuation{Value: &amount},
	}
}

// completeView is a portfolio whose total is stated, which is what every share limit needs.
func completeView(total string, holdings ...portfolio.Holding) portfolio.View {
	value := total
	return portfolio.View{
		Portfolio: portfolio.Portfolio{AccountingCurrency: "EUR"},
		Holdings:  holdings,
		Totals:    portfolio.Totals{Value: &value, Complete: true, ReturnAbsence: portfolio.ReturnAbsence},
	}
}

func limitOf(kind Kind, threshold string) Limit {
	return Limit{Kind: kind, Threshold: threshold}
}

// TestEachKindMeasuresWhatItSays. Four holdings worth 400 in total: 200, 100, 60 and 40.
//
//	ALFA  industrials  XSTO  200  → 50%
//	BETA  industrials  XHEL  100  → 25%
//	GAMMA financials   XSTO   60  → 15%
//	DELTA financials   XCSE   40  → 10%
//
// Instrument: the largest single holding is 50%. Sector: industrials is 300 of 400, 75%.
// Market: XSTO is 260 of 400, 65%. Count: four.
func TestEachKindMeasuresWhatItSays(t *testing.T) {
	view := completeView("400",
		holding("ALFA", "industrials", "XSTO", "200"),
		holding("BETA", "industrials", "XHEL", "100"),
		holding("GAMMA", "financials", "XSTO", "60"),
		holding("DELTA", "financials", "XCSE", "40"),
	)

	for _, expected := range []struct {
		kind     Kind
		measured string
		groups   int
	}{
		{KindInstrumentShare, "0.500000000000", 4},
		{KindSectorShare, "0.750000000000", 2},
		{KindMarketShare, "0.650000000000", 3},
	} {
		evaluation, err := evaluate(limitOf(expected.kind, "0.9"), view)
		if err != nil {
			t.Fatalf("%s: %v", expected.kind, err)
		}
		if evaluation.Measured == nil || *evaluation.Measured != expected.measured {
			t.Errorf("%s measured %v, want %s", expected.kind, evaluation.Measured, expected.measured)
		}
		if len(evaluation.Contributions) != expected.groups {
			t.Errorf("%s produced %d groups, want %d: %+v",
				expected.kind, len(evaluation.Contributions), expected.groups, evaluation.Contributions)
		}
		// Largest first, and the largest is what was measured.
		if len(evaluation.Contributions) > 0 && evaluation.Contributions[0].Share != expected.measured {
			t.Errorf("%s reports %s as its largest group, measured %s",
				expected.kind, evaluation.Contributions[0].Share, expected.measured)
		}
	}

	count, err := evaluate(limitOf(KindHoldingCount, "10"), view)
	if err != nil {
		t.Fatal(err)
	}
	if count.Measured == nil || *count.Measured != "4.000000000000" {
		t.Errorf("the holding count measured %v, want 4", count.Measured)
	}
	// A count divides by nothing, so there is nothing to show as a denominator.
	if count.Denominator != nil {
		t.Errorf("a count reported a denominator: %v", *count.Denominator)
	}
}

func TestTheStateIsOneOfThreeAndNeverDefaults(t *testing.T) {
	view := completeView("400",
		holding("ALFA", "industrials", "XSTO", "200"),
		holding("BETA", "industrials", "XHEL", "200"),
	)

	within, err := evaluate(limitOf(KindInstrumentShare, "0.6"), view)
	if err != nil {
		t.Fatal(err)
	}
	if within.State != StateWithin {
		t.Errorf("50%% against a 60%% limit is %q", within.State)
	}

	exceeded, err := evaluate(limitOf(KindInstrumentShare, "0.4"), view)
	if err != nil {
		t.Fatal(err)
	}
	if exceeded.State != StateExceeded {
		t.Errorf("50%% against a 40%% limit is %q", exceeded.State)
	}

	// Exactly at the threshold is within: a limit says "no more than", not "less than".
	exact, err := evaluate(limitOf(KindInstrumentShare, "0.5"), view)
	if err != nil {
		t.Fatal(err)
	}
	if exact.State != StateWithin {
		t.Errorf("50%% against a 50%% limit is %q; a limit is a maximum, not a bound to stay under", exact.State)
	}
}

// TestNothingHeldIsNotCompliance. An empty portfolio satisfies nothing; there is nothing to
// measure, and saying "within" would be the product reporting a check it did not make.
func TestNothingHeldIsNotCompliance(t *testing.T) {
	empty := completeView("0")
	for _, kind := range Kinds {
		evaluation, err := evaluate(limitOf(kind, thresholdFor(kind)), empty)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if evaluation.State != StateUnevaluable {
			t.Errorf("%s on an empty portfolio is %q, want unevaluable", kind, evaluation.State)
		}
		if evaluation.AbsenceReason == nil || *evaluation.AbsenceReason != AbsenceNothingHeld {
			t.Errorf("%s gives reason %v, want nothing_held", kind, evaluation.AbsenceReason)
		}
		if evaluation.Measured != nil {
			t.Errorf("%s reported a figure for an empty portfolio", kind)
		}
	}
}

// TestAnIncompleteTotalMakesAShareUnevaluableButNotACount is FR-011 and FR-012 together. The split
// falls out of the arithmetic: a share divides by a total the product declines to state, and a
// count divides by nothing.
func TestAnIncompleteTotalMakesAShareUnevaluableButNotACount(t *testing.T) {
	reason := "at least one holding could not be valued, so no total is stated"
	view := portfolio.View{
		Portfolio: portfolio.Portfolio{AccountingCurrency: "EUR"},
		Holdings: []portfolio.Holding{
			holding("ALFA", "industrials", "XSTO", "200"),
			{Ticker: "BETA", Sector: "financials", MIC: "XHEL", Quantity: "1", Cost: "100"},
		},
		Totals: portfolio.Totals{Complete: false, IncompleteReason: &reason,
			ReturnAbsence: portfolio.ReturnAbsence},
	}

	for _, kind := range []Kind{KindInstrumentShare, KindSectorShare, KindMarketShare} {
		evaluation, err := evaluate(limitOf(kind, "0.5"), view)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if evaluation.State != StateUnevaluable {
			t.Errorf("%s with an unpriceable holding is %q, want unevaluable", kind, evaluation.State)
		}
		if evaluation.AbsenceReason == nil || *evaluation.AbsenceReason != AbsencePortfolioIncomplete {
			t.Errorf("%s gives reason %v, want portfolio_incomplete", kind, evaluation.AbsenceReason)
		}
		if evaluation.Measured != nil {
			t.Errorf("%s reported a figure from an incomplete total", kind)
		}
	}

	// Counting needs no price. Silencing this too would be its own dishonesty.
	count, err := evaluate(limitOf(KindHoldingCount, "5"), view)
	if err != nil {
		t.Fatal(err)
	}
	if count.State != StateWithin || count.Measured == nil || *count.Measured != "2.000000000000" {
		t.Errorf("the holding count is %q measuring %v; it needs no price", count.State, count.Measured)
	}
}

// TestUnclassifiedIsASectorAndIsStated. Dropping those holdings would shrink the denominator while
// leaving every other numerator alone, understating every sector's share — and hiding the gap from
// somebody who could go and classify it.
func TestUnclassifiedIsASectorAndIsStated(t *testing.T) {
	view := completeView("300",
		holding("ALFA", "industrials", "XSTO", "200"),
		holding("BETA", "unclassified", "XHEL", "100"),
	)

	evaluation, err := evaluate(limitOf(KindSectorShare, "0.9"), view)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	var summed float64
	for _, contribution := range evaluation.Contributions {
		if contribution.Label == "unclassified" {
			found = true
		}
		summed += mustFloat(t, contribution.Share)
	}
	if !found {
		t.Errorf("the unclassified holding is missing from %+v", evaluation.Contributions)
	}
	// It is inside the denominator too: the shares still sum to one.
	if summed < 0.999999 || summed > 1.000001 {
		t.Errorf("the sector shares sum to %v, so something was dropped from the denominator", summed)
	}
}

// TestTheContributionsReconcileWithTheMeasurement is the property behind every worked example:
// whatever the holdings, each group's value over the denominator is its share, the shares sum to
// one, and the largest is the figure reported.
func TestTheContributionsReconcileWithTheMeasurement(t *testing.T) {
	view := completeView("1000",
		holding("A", "industrials", "XSTO", "437"),
		holding("B", "financials", "XSTO", "263"),
		holding("C", "industrials", "XHEL", "191"),
		holding("D", "energy", "XCSE", "109"),
	)

	for _, kind := range []Kind{KindInstrumentShare, KindSectorShare, KindMarketShare} {
		evaluation, err := evaluate(limitOf(kind, "0.99"), view)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		denominator := mustFloat(t, *evaluation.Denominator)
		var summedShares, summedValues float64
		for _, contribution := range evaluation.Contributions {
			value := mustFloat(t, contribution.Value)
			share := mustFloat(t, contribution.Share)
			if difference := share - value/denominator; difference > 1e-9 || difference < -1e-9 {
				t.Errorf("%s: %s reports share %v against value/total %v",
					kind, contribution.Label, share, value/denominator)
			}
			summedShares += share
			summedValues += value
		}
		if summedShares < 0.999999 || summedShares > 1.000001 {
			t.Errorf("%s: shares sum to %v", kind, summedShares)
		}
		if difference := summedValues - denominator; difference > 1e-6 || difference < -1e-6 {
			t.Errorf("%s: values sum to %v against a denominator of %v", kind, summedValues, denominator)
		}
		if *evaluation.Measured != evaluation.Contributions[0].Share {
			t.Errorf("%s: measured %s, largest contribution %s",
				kind, *evaluation.Measured, evaluation.Contributions[0].Share)
		}
	}
}

func thresholdFor(kind Kind) string {
	if kind == KindHoldingCount {
		return "10"
	}
	return "0.5"
}

func mustFloat(t *testing.T, value string) float64 {
	t.Helper()
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		t.Fatalf("parse %s: %v", value, err)
	}
	return parsed
}
