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
	if len(result.Bars) != 0 || result.Rejected != 1 || len(result.Issues) != 1 || result.Issues[0].Rule != "provider_gap" || result.Issues[0].Disposition != DispositionRejected {
		t.Fatalf("result = %#v", result)
	}
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

func mustDecimal(t *testing.T, value string) Decimal {
	t.Helper()
	result, err := ParseDecimal(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
