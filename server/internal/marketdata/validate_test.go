package marketdata

import "testing"

func TestValidateDailyPageAcceptsValidBarSessionAndActions(t *testing.T) {
	session := mustSession(t, "2026-08-24")
	ratio := mustDecimal(t, "2")
	amount := mustDecimal(t, "1.25")
	adjusted := mustDecimal(t, "50.25")
	page := DailyPage{
		Bars: []ProviderBar{{
			SessionDate: session,
			Open:        mustDecimal(t, "100"), High: mustDecimal(t, "110"),
			Low: mustDecimal(t, "90"), Close: mustDecimal(t, "105"),
			AdjustedClose: &adjusted, Volume: 1000, SourceHash: "bar-source",
		}},
		Actions: []ProviderAction{
			{ProviderActionID: "split-1", Type: ActionSplit, ExDate: session, Ratio: &ratio, SourceHash: "split-source"},
			{ProviderActionID: "dividend-1", Type: ActionDividend, ExDate: session, Amount: &amount, Currency: "SEK", SourceHash: "dividend-source"},
		},
	}

	result, err := ValidateDailyPage(page, ValidationOptions{ExpectedSessions: map[SessionDate]struct{}{session: {}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Bars) != 1 || len(result.Actions) != 2 || len(result.Issues) != 0 || result.Rejected != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateDailyPageRejectsBarOutsideExpectedSessions(t *testing.T) {
	expected := mustSession(t, "2026-08-24")
	unexpected := mustSession(t, "2026-08-23")
	page := DailyPage{Bars: []ProviderBar{{
		SessionDate: unexpected,
		Open:        mustDecimal(t, "100"), High: mustDecimal(t, "110"),
		Low: mustDecimal(t, "90"), Close: mustDecimal(t, "105"),
		Volume: 1000, SourceHash: "bar-source",
	}}}

	result, err := ValidateDailyPage(page, ValidationOptions{ExpectedSessions: map[SessionDate]struct{}{expected: {}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Bars) != 0 || result.Rejected != 1 {
		t.Fatalf("result = %#v", result)
	}
	assertIssue(t, result.Issues, "provider_gap", unexpected, DispositionRejected)
	assertIssue(t, result.Issues, "missing_session", expected, DispositionFlagged)
}

func TestValidateDailyPageRejectsMalformedCorporateAction(t *testing.T) {
	session := mustSession(t, "2026-08-24")
	page := DailyPage{Actions: []ProviderAction{{
		ProviderActionID: "dividend-1", Type: ActionDividend, ExDate: session,
		Currency: "SEK", SourceHash: "dividend-source",
	}}}

	result, err := ValidateDailyPage(page, ValidationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Actions) != 0 || result.Rejected != 1 || len(result.Issues) != 1 || result.Issues[0].Rule != "possible_corporate_action_discontinuity" {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateDailyPageRejectsImpossiblePricesAndNegativeVolume(t *testing.T) {
	session := mustSession(t, "2026-08-24")
	tests := []struct {
		name string
		bar  ProviderBar
		rule string
	}{
		{name: "non-positive open", bar: ProviderBar{SessionDate: session, Open: mustDecimal(t, "0"), High: mustDecimal(t, "2"), Low: mustDecimal(t, "1"), Close: mustDecimal(t, "1"), Volume: 1, SourceHash: "zero"}, rule: "non_positive_price"},
		{name: "impossible OHLC", bar: ProviderBar{SessionDate: session, Open: mustDecimal(t, "4"), High: mustDecimal(t, "3"), Low: mustDecimal(t, "1"), Close: mustDecimal(t, "2"), Volume: 1, SourceHash: "ohlc"}, rule: "invalid_ohlc"},
		{name: "negative volume", bar: ProviderBar{SessionDate: session, Open: mustDecimal(t, "2"), High: mustDecimal(t, "3"), Low: mustDecimal(t, "1"), Close: mustDecimal(t, "2"), Volume: -1, SourceHash: "volume"}, rule: "negative_volume"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ValidateDailyPage(DailyPage{Bars: []ProviderBar{tt.bar}}, ValidationOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Bars) != 0 || result.Rejected != 1 || len(result.Issues) != 1 ||
				result.Issues[0].Rule != tt.rule || result.Issues[0].Disposition != DispositionRejected {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestValidateDailyPageClassifiesDuplicateOrderingGapZeroVolumeAndActionDiscontinuity(t *testing.T) {
	dayOne := mustSession(t, "2026-08-24")
	dayTwo := mustSession(t, "2026-08-25")
	dayThree := mustSession(t, "2026-08-26")
	dayFour := mustSession(t, "2026-08-27")
	adjustedOne := mustDecimal(t, "99")
	adjustedTwo := mustDecimal(t, "100")
	adjustedFour := mustDecimal(t, "40")
	ratio := mustDecimal(t, "2")
	page := DailyPage{
		Bars: []ProviderBar{
			{SessionDate: dayTwo, Open: mustDecimal(t, "99"), High: mustDecimal(t, "101"), Low: mustDecimal(t, "98"), Close: mustDecimal(t, "100"), AdjustedClose: &adjustedTwo, Volume: 100, SourceHash: "two"},
			{SessionDate: dayOne, Open: mustDecimal(t, "98"), High: mustDecimal(t, "100"), Low: mustDecimal(t, "97"), Close: mustDecimal(t, "99"), AdjustedClose: &adjustedOne, Volume: 100, SourceHash: "one"},
			{SessionDate: dayTwo, Open: mustDecimal(t, "99"), High: mustDecimal(t, "101"), Low: mustDecimal(t, "98"), Close: mustDecimal(t, "100"), AdjustedClose: &adjustedTwo, Volume: 100, SourceHash: "two"},
			{SessionDate: dayFour, Open: mustDecimal(t, "40"), High: mustDecimal(t, "41"), Low: mustDecimal(t, "39"), Close: mustDecimal(t, "40"), AdjustedClose: &adjustedFour, Volume: 0, SourceHash: "four"},
		},
		Actions: []ProviderAction{{ProviderActionID: "split", Type: ActionSplit, ExDate: dayFour, Ratio: &ratio, SourceHash: "split"}},
	}
	result, err := ValidateDailyPage(page, ValidationOptions{ExpectedSessions: map[SessionDate]struct{}{
		dayOne: {}, dayTwo: {}, dayThree: {}, dayFour: {},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Bars) != 3 || result.Rejected != 1 {
		t.Fatalf("accepted bars/rejections = %d/%d", len(result.Bars), result.Rejected)
	}
	assertIssue(t, result.Issues, "duplicate_source_row", dayTwo, DispositionRejected)
	assertIssue(t, result.Issues, "out_of_order_source_row", dayOne, DispositionFlagged)
	assertIssue(t, result.Issues, "zero_volume", dayFour, DispositionFlagged)
	assertIssue(t, result.Issues, "missing_session", dayThree, DispositionFlagged)
	assertIssue(t, result.Issues, "possible_corporate_action_discontinuity", dayFour, DispositionFlagged)
}

func TestValidateDailyPageFlagsSuspiciousJumpWithoutAction(t *testing.T) {
	dayOne := mustSession(t, "2026-08-24")
	dayTwo := mustSession(t, "2026-08-25")
	page := DailyPage{Bars: []ProviderBar{
		{SessionDate: dayOne, Open: mustDecimal(t, "99"), High: mustDecimal(t, "101"), Low: mustDecimal(t, "98"), Close: mustDecimal(t, "100"), Volume: 100, SourceHash: "one"},
		{SessionDate: dayTwo, Open: mustDecimal(t, "39"), High: mustDecimal(t, "41"), Low: mustDecimal(t, "38"), Close: mustDecimal(t, "40"), Volume: 100, SourceHash: "two"},
	}}
	result, err := ValidateDailyPage(page, ValidationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertIssue(t, result.Issues, "suspicious_jump", dayTwo, DispositionFlagged)
}

func assertIssue(t *testing.T, issues []ValidationIssue, rule string, session SessionDate, disposition FindingDisposition) {
	t.Helper()
	for _, issue := range issues {
		if issue.Rule == rule && issue.SessionDate == session && issue.Disposition == disposition {
			return
		}
	}
	t.Fatalf("missing %s issue for %s with %s: %#v", rule, session, disposition, issues)
}

func mustDecimal(t *testing.T, value string) Decimal {
	t.Helper()
	result, err := ParseDecimal(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// A session before an instrument was listed is not missing data.
//
// The importer requested ten years for every instrument in the universe, and flagged every
// session the exchange was open before a company existed on it. That produced 8,054 of 8,662
// open findings in production — for Metso Outotec's successor, for EQT before its 2019 listing,
// for Kojamo before its 2018 one. The chart then marked years of sessions as gaps in a series
// that had not started.
//
// A gap is a hole *inside* a history. Before the history begins there is no hole, there is no
// history — which coverage and freshness already say, and say once rather than a thousand
// times.
func TestValidateDoesNotFlagSessionsBeforeTheProvidersHistoryBegins(t *testing.T) {
	expected := map[SessionDate]struct{}{}
	for _, day := range []string{"2016-09-01", "2016-09-02", "2019-09-24", "2019-09-25"} {
		expected[mustSession(t, day)] = struct{}{}
	}
	// The provider's history for this instrument starts in 2019: it was not listed before.
	page := DailyPage{Bars: []ProviderBar{
		bar(t, mustSession(t, "2019-09-24"), "100"),
		bar(t, mustSession(t, "2019-09-25"), "101"),
	}}

	result, err := ValidateDailyPage(page, ValidationOptions{ExpectedSessions: expected})
	if err != nil {
		t.Fatal(err)
	}
	for _, issue := range result.Issues {
		if issue.Rule == "missing_session" {
			t.Errorf("session %s before the history begins was flagged missing", issue.SessionDate)
		}
	}
}

// A hole inside the history is still a hole, and still reported.
func TestValidateStillFlagsAGapInsideTheProvidersHistory(t *testing.T) {
	expected := map[SessionDate]struct{}{}
	for _, day := range []string{"2019-09-24", "2019-09-25", "2019-09-26"} {
		expected[mustSession(t, day)] = struct{}{}
	}
	page := DailyPage{Bars: []ProviderBar{
		bar(t, mustSession(t, "2019-09-24"), "100"),
		bar(t, mustSession(t, "2019-09-26"), "102"),
	}}

	result, err := ValidateDailyPage(page, ValidationOptions{ExpectedSessions: expected})
	if err != nil {
		t.Fatal(err)
	}
	var flagged []string
	for _, issue := range result.Issues {
		if issue.Rule == "missing_session" {
			flagged = append(flagged, issue.SessionDate.String())
		}
	}
	if len(flagged) != 1 || flagged[0] != "2019-09-25" {
		t.Fatalf("flagged %v, expected exactly the session inside the history", flagged)
	}
}

// A page with no bars at all reports no gaps. There is nothing for them to be gaps in, and a
// thousand findings would say less than the single fact that no history exists.
func TestValidateFlagsNothingWhenTheProviderReturnedNoHistory(t *testing.T) {
	expected := map[SessionDate]struct{}{}
	for _, day := range []string{"2016-09-01", "2016-09-02"} {
		expected[mustSession(t, day)] = struct{}{}
	}
	result, err := ValidateDailyPage(DailyPage{}, ValidationOptions{ExpectedSessions: expected})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Issues) != 0 {
		t.Fatalf("an empty page produced %d issues: %#v", len(result.Issues), result.Issues)
	}
}
