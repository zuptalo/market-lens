package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"market-lens/server/internal/intents"
	"market-lens/server/internal/risk"
)

type intentsServiceStub struct {
	askedFor       string
	includeSettled bool
	settledID      string
	settledStatus  intents.Status
	refusal        error
	empty          bool
}

func (s *intentsServiceStub) Report(_ context.Context, userID string, includeSettled bool) (intents.Report, error) {
	s.askedFor, s.includeSettled = userID, includeSettled
	report := intents.Report{Intents: []intents.Intent{}, EvaluatedIndependently: true,
		RecordsWhatYouAreConsidering: true}
	if s.empty {
		return report, nil
	}
	value, share, denominator := "117093.200000000000", "0.412300000000", "284000.000000000000"
	settled := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	report.Intents = append(report.Intents,
		intents.Intent{
			ID: intents.UUID("11111111-1111-4111-8111-111111111111"), Ticker: "VOLV-B",
			Name: "Volvo B", Currency: "SEK", Direction: intents.DirectionBuy,
			Quantity: "120.000000000000", Price: "281.400000000000", Costs: "0.000000000000",
			Status: intents.StatusConsidering, RecordedAt: settled,
			Consequence: &intents.Consequence{
				ResultingQuantity: "520.000000000000", ResultingValue: &value,
				ResultingShare: &share, Denominator: &denominator,
				Limits: []risk.Evaluation{{
					Kind: risk.KindInstrumentShare, Threshold: "0.250000000000",
					State: risk.StateExceeded, Measured: &share, Denominator: &denominator,
					Contributions: []risk.Contribution{},
				}},
			},
		},
		intents.Intent{
			ID: intents.UUID("22222222-2222-4222-8222-222222222222"), Ticker: "NOKIA",
			Name: "Nokia", Currency: "EUR", Direction: intents.DirectionSell,
			Quantity: "50.000000000000", Price: "4.100000000000", Costs: "0.000000000000",
			Status: intents.StatusActedOn, RecordedAt: settled, SettledAt: &settled,
		})
	return report, nil
}

func (s *intentsServiceStub) Record(_ context.Context, userID string, _ intents.RecordRequest) (intents.Intent, error) {
	s.askedFor = userID
	return intents.Intent{}, s.refusal
}

func (s *intentsServiceStub) Settle(_ context.Context, userID, intentID string, status intents.Status) error {
	s.askedFor, s.settledID, s.settledStatus = userID, intentID, status
	return s.refusal
}

func intentsRouter(stub *intentsServiceStub) http.Handler {
	return NewRouter(authenticatedDependencies(Dependencies{Intents: stub}))
}

// TestIntentReadsMatchTheContract covers what a client cannot recover on its own: decimals surviving
// as strings, a settled intent arriving with no consequence rather than a zeroed one, and the two
// statements the report always carries.
func TestIntentReadsMatchTheContract(t *testing.T) {
	stub := &intentsServiceStub{}
	response := performRequest(intentsRouter(stub), "/api/v1/order-intents")
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	body := decodeBody(t, response)

	if body["evaluated_independently"] != true || body["records_what_you_are_considering"] != true {
		t.Errorf("the report does not state how it was produced: %#v", body)
	}
	if stub.includeSettled {
		t.Errorf("settled intents were included without being asked for")
	}
	list, _ := body["intents"].([]any)
	if len(list) != 2 {
		t.Fatalf("%d intents", len(list))
	}

	considering, _ := list[0].(map[string]any)
	if considering["quantity"] != "120.000000000000" || considering["price"] != "281.400000000000" {
		t.Errorf("a decimal did not survive as a string: %#v", considering)
	}
	if considering["status"] != "considering" || considering["settled_at"] != nil {
		t.Errorf("the unsettled intent reads %#v", considering)
	}
	consequence, _ := considering["consequence"].(map[string]any)
	if consequence == nil || consequence["resulting_quantity"] != "520.000000000000" {
		t.Fatalf("the consequence is %#v", considering["consequence"])
	}
	if consequence["resulting_share"] != "0.412300000000" || consequence["denominator"] != "284000.000000000000" {
		t.Errorf("the share arrived without the arithmetic behind it: %#v", consequence)
	}
	limits, _ := consequence["limits"].([]any)
	if len(limits) != 1 {
		t.Fatalf("%d limits on the consequence", len(limits))
	}
	if first, _ := limits[0].(map[string]any); first["state"] != "exceeded" {
		t.Errorf("the limit verdict is %#v", limits[0])
	}

	// A settled intent carries no consequence. Sending a zeroed one would read as "it would do
	// nothing", which is a different claim from "the question is no longer asked".
	settled, _ := list[1].(map[string]any)
	if settled["status"] != "acted_on" || settled["settled_at"] == nil {
		t.Errorf("the settled intent reads %#v", settled)
	}
	if _, present := settled["consequence"]; !present {
		t.Errorf("the consequence field is missing rather than null")
	}
	if settled["consequence"] != nil {
		t.Errorf("a settled intent carries a consequence: %#v", settled["consequence"])
	}
}

// TestNoResponseFieldCouldBeSentToABroker asserts FR-006 at the boundary the outside world sees.
func TestNoResponseFieldCouldBeSentToABroker(t *testing.T) {
	response := performRequest(intentsRouter(&intentsServiceStub{}), "/api/v1/order-intents")
	body := strings.ToLower(response.Body.String())
	for _, forbidden := range []string{
		"venue", "order_type", "time_in_force", "destination", "broker", "expires",
		"limit_price", "stop_price", "submit", "route", "recommend", "should",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the response carries %q: %s", forbidden, response.Body.String())
		}
	}
}

func TestIntentWritesRequireTheCSRFHeader(t *testing.T) {
	router := intentsRouter(&intentsServiceStub{})
	for _, write := range []struct {
		method, path, body string
	}{
		{http.MethodPost, "/api/v1/order-intents", `{"instrument_id":"x","direction":"buy","quantity":"1","price":"1"}`},
		{http.MethodPatch, "/api/v1/order-intents/11111111-1111-4111-8111-111111111111", `{"status":"withdrawn"}`},
	} {
		request := authenticatedAPIRequest(write.method, write.path)
		request.Body = io.NopCloser(strings.NewReader(write.body))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden {
			t.Errorf("%s %s without the CSRF header returned %d", write.method, write.path, recorder.Code)
		}
	}
}

func TestSettlingReachesTheServiceWithTheCallerAndStatus(t *testing.T) {
	stub := &intentsServiceStub{}
	response := performIntentWrite(t, intentsRouter(stub), http.MethodPatch,
		"/api/v1/order-intents/11111111-1111-4111-8111-111111111111", `{"status":"acted_on"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	if stub.settledID != "11111111-1111-4111-8111-111111111111" {
		t.Errorf("the service was asked to settle %q", stub.settledID)
	}
	if stub.settledStatus != intents.StatusActedOn {
		t.Errorf("the service was asked for status %q", stub.settledStatus)
	}
	if stub.askedFor == "" {
		t.Errorf("the service was not told whose intent it is")
	}
	// A settle answers with the whole report, because every consequence moves when the portfolio
	// context does and a client patching one row would show stale arithmetic beside fresh.
	if body := decodeBody(t, response); body["intents"] == nil {
		t.Errorf("the settle response is not a report: %s", response.Body.String())
	}
}

func TestARefusalArrivesWithACodeAPersonCanActOn(t *testing.T) {
	stub := &intentsServiceStub{refusal: intents.Refusal{
		Code: intents.RefusalInstrumentNotCarried, Message: "This product does not carry that instrument."}}
	response := performIntentWrite(t, intentsRouter(stub), http.MethodPost, "/api/v1/order-intents",
		`{"instrument_id":"00000000-0000-4000-8000-00000000dead","direction":"buy","quantity":"1","price":"1"}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	body := decodeBody(t, response)
	failure, _ := body["error"].(map[string]any)
	if failure["code"] != string(intents.RefusalInstrumentNotCarried) {
		t.Errorf("the refusal reads %#v", body)
	}

	notFound := &intentsServiceStub{refusal: intents.ErrNotFound}
	missing := performIntentWrite(t, intentsRouter(notFound), http.MethodPatch,
		"/api/v1/order-intents/11111111-1111-4111-8111-111111111111", `{"status":"withdrawn"}`)
	if missing.Code != http.StatusNotFound {
		t.Errorf("settling somebody else's intent returned %d, want 404", missing.Code)
	}
}

func TestIntentsAreRefusedWithoutASession(t *testing.T) {
	router := intentsRouter(&intentsServiceStub{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/order-intents", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("an unauthenticated read returned %d", recorder.Code)
	}
}

func TestSettledIntentsAreIncludedOnlyWhenAsked(t *testing.T) {
	stub := &intentsServiceStub{}
	performRequest(intentsRouter(stub), "/api/v1/order-intents?include_settled=true")
	if !stub.includeSettled {
		t.Errorf("include_settled=true did not reach the service")
	}
}

func performIntentWrite(t *testing.T, router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := authenticatedAPIRequest(method, path)
	request.Body = io.NopCloser(bytes.NewReader([]byte(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "test-csrf")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}
