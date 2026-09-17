package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/httpx"
	"market-lens/server/internal/risk"
)

type riskServiceStub struct {
	askedFor string
	kind     risk.Kind
	refusal  error
	empty    bool
}

func (s *riskServiceStub) Report(_ context.Context, userID string) (risk.Report, error) {
	s.askedFor = userID
	if s.empty {
		return risk.Report{AccountingCurrency: "SEK", Limits: []risk.Evaluation{},
			LimitsAreYourOwn: true}, nil
	}
	measured, denominator := "0.412300000000", "284000.000000000000"
	incomplete := risk.AbsencePortfolioIncomplete
	return risk.Report{
		AccountingCurrency: "SEK",
		Limits: []risk.Evaluation{
			{
				Kind: risk.KindInstrumentShare, Threshold: "0.250000000000",
				State: risk.StateExceeded, Measured: &measured, Denominator: &denominator,
				Contributions: []risk.Contribution{
					{Label: "VOLV-B", Value: "117093.200000000000", Share: "0.412300000000"},
					{Label: "NOKIA", Value: "166906.800000000000", Share: "0.587700000000"},
				},
			},
			{
				Kind: risk.KindSectorShare, Threshold: "0.500000000000",
				State: risk.StateUnevaluable, AbsenceReason: &incomplete,
				Contributions: []risk.Contribution{},
			},
		},
		LimitsAreYourOwn: true,
	}, nil
}

func (s *riskServiceStub) State(_ context.Context, userID string, kind risk.Kind, _ string) (risk.Limit, error) {
	s.askedFor, s.kind = userID, kind
	return risk.Limit{Kind: kind}, s.refusal
}

func (s *riskServiceStub) Remove(_ context.Context, userID string, kind risk.Kind) error {
	s.askedFor, s.kind = userID, kind
	return s.refusal
}

func riskRouter(stub *riskServiceStub) http.Handler {
	return NewRouter(authenticatedDependencies(Dependencies{Risk: stub}))
}

// TestRiskReadsMatchTheContract covers what a client cannot recover on its own: that decimals
// survive as strings, that the three states arrive as stated values, and that an unevaluable limit
// carries its reason rather than an absent field a client would read as zero.
func TestRiskReadsMatchTheContract(t *testing.T) {
	router := riskRouter(&riskServiceStub{})
	response := performRequest(router, "/api/v1/risk-limits")
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	body := decodeBody(t, response)

	if body["limits_are_your_own"] != true {
		t.Errorf("the report does not state that the limits are the person's own")
	}
	limits, _ := body["limits"].([]any)
	if len(limits) != 2 {
		t.Fatalf("%d limits", len(limits))
	}

	breach, _ := limits[0].(map[string]any)
	if breach["state"] != "exceeded" {
		t.Errorf("the state is %#v", breach["state"])
	}
	if breach["measured"] != "0.412300000000" || breach["denominator"] != "284000.000000000000" {
		t.Errorf("a decimal did not survive as a string: %#v", breach)
	}
	// The arithmetic travels with the figure, so the percentage can be checked rather than believed.
	contributions, _ := breach["contributions"].([]any)
	if len(contributions) != 2 {
		t.Fatalf("%d contributions", len(contributions))
	}
	first, _ := contributions[0].(map[string]any)
	for _, field := range []string{"label", "value", "share"} {
		if _, present := first[field]; !present {
			t.Errorf("%s is missing from a contribution", field)
		}
	}

	unevaluable, _ := limits[1].(map[string]any)
	if unevaluable["state"] != "unevaluable" || unevaluable["absence_reason"] != "portfolio_incomplete" {
		t.Errorf("the unevaluable limit reads %#v", unevaluable)
	}
	// And it reports no figure. A measured value beside an unevaluable state would be read as one.
	if unevaluable["measured"] != nil {
		t.Errorf("an unevaluable limit reported a figure: %#v", unevaluable["measured"])
	}
}

// TestNoResponseSaysWhatWouldCloseTheGap. FR-015 asserted on the wire, because a field that exists
// will eventually be rendered.
func TestNoResponseSaysWhatWouldCloseTheGap(t *testing.T) {
	router := riskRouter(&riskServiceStub{})
	response := performRequest(router, "/api/v1/risk-limits")
	body := strings.ToLower(response.Body.String())
	for _, forbidden := range []string{"sell", "reduce", "suggest", "recommend", "excess", "over_by", "action"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the response carries %q: %s", forbidden, response.Body.String())
		}
	}
}

func TestAPersonWithNoLimitsIsOfferedNone(t *testing.T) {
	router := riskRouter(&riskServiceStub{empty: true})
	body := decodeBody(t, performRequest(router, "/api/v1/risk-limits"))
	limits, _ := body["limits"].([]any)
	if len(limits) != 0 {
		t.Errorf("a person who stated nothing was given %d limits", len(limits))
	}
	// An empty array, not a null: a client distinguishing "none" from "not loaded" needs the shape.
	if body["limits"] == nil {
		t.Errorf("the limits field is null rather than an empty list")
	}
}

func TestRiskLimitsAreAlwaysTheCallersOwn(t *testing.T) {
	stub := &riskServiceStub{}
	router := riskRouter(stub)
	performRequest(router, "/api/v1/risk-limits?user_id=10000000-0000-4000-8000-000000000002")
	if stub.askedFor != aliceUserID {
		t.Errorf("a user_id parameter redirected the read to %q", stub.askedFor)
	}
}

func TestRiskWritesRequireCSRF(t *testing.T) {
	router := riskRouter(&riskServiceStub{})
	for _, route := range []struct{ method, path string }{
		{http.MethodPut, "/api/v1/risk-limits/instrument_share"},
		{http.MethodDelete, "/api/v1/risk-limits/instrument_share"},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, authenticatedAPIRequest(route.method, route.path))
		if recorder.Code != http.StatusForbidden {
			t.Errorf("%s %s answered %d without a CSRF token", route.method, route.path, recorder.Code)
		}
	}
}

func TestARefusedThresholdSaysWhatOneCanBe(t *testing.T) {
	stub := &riskServiceStub{refusal: risk.Refusal{
		Code:    risk.RefusalThresholdOutOfRange,
		Message: "A share is more than 0 and at most 1 — a quarter is 0.25.",
	}}
	router := riskRouter(stub)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, requestWithJSON(http.MethodPut,
		"/api/v1/risk-limits/instrument_share", map[string]any{"threshold": "25"}))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != risk.RefusalThresholdOutOfRange {
		t.Errorf("the refusal code is %q", body.Error.Code)
	}
	// Somebody who typed 25 meaning a quarter needs to be told the form, not that they were wrong.
	if !strings.Contains(body.Error.Message, "0.25") {
		t.Errorf("the refusal does not say what a share looks like: %q", body.Error.Message)
	}
}

func TestRiskIsRefusedWithoutASession(t *testing.T) {
	router := riskRouter(&riskServiceStub{})
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/risk-limits"},
		{http.MethodPut, "/api/v1/risk-limits/instrument_share"},
		{http.MethodDelete, "/api/v1/risk-limits/instrument_share"},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(route.method, route.path, nil))
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s %s answered %d without a session", route.method, route.path, recorder.Code)
		}
	}
}

func TestRiskIsRefusedToADeactivatedAccount(t *testing.T) {
	deps := Dependencies{Risk: &riskServiceStub{}}
	deps.Authenticator = sessionAuthenticatorFunc(func(context.Context, string) (auth.Principal, error) {
		return auth.Principal{}, auth.ErrAuthenticationRequired
	})
	router := NewRouter(deps)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/risk-limits", nil)
	request.AddCookie(&http.Cookie{Name: httpx.SessionCookieName, Value: "test-session"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("a deactivated account read a risk report: %d", recorder.Code)
	}
}

// No route reaches another person's rules, and these must keep not existing.
func TestNoRouteReachesAnotherPersonsLimits(t *testing.T) {
	router := riskRouter(&riskServiceStub{})
	for _, path := range []string{
		"/api/v1/risk-limits/all",
		"/api/v1/users/" + bobUserID + "/risk-limits",
		"/api/v1/owner/risk-limits",
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, authenticatedAPIRequest(http.MethodGet, path))
		if recorder.Code != http.StatusNotFound {
			t.Errorf("GET %s answered %d", path, recorder.Code)
		}
	}
}
