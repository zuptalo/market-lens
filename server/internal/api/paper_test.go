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

	"market-lens/server/internal/paper"
)

type paperServiceStub struct {
	askedFor   string
	openedWith paper.OpenRequest
	promoted   string
	cancelled  string
	err        error
	noAccount  bool
}

func (s *paperServiceStub) View(_ context.Context, userID string) (paper.View, error) {
	s.askedFor = userID
	if s.noAccount {
		return paper.View{}, paper.ErrNotFound
	}
	value, unrealised := "272560.000000000000", "32440.000000000000"
	totalReturn := "0.043210000000"
	session := paper.SessionDate("2026-09-17")
	holdingReturn, benchmarkReturn := "0.135600000000", "0.092100000000"
	settled := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	reason := paper.AbsenceInsufficientCash
	return paper.View{
		Account: paper.Account{
			StartingCash: "1000000.000000000000", AccountingCurrency: "SEK",
			Costs: paper.DefaultCostRates, OpenedAt: settled,
		},
		Cash: "727440.000000000000",
		Holdings: []paper.Holding{{
			InstrumentID: "33333333-3333-4333-8333-333333333333", Ticker: "ABB", Name: "ABB Ltd",
			Currency: "SEK", Quantity: "400.000000000000", Cost: "240120.000000000000",
			Value: &value, Unrealised: &unrealised, Session: &session,
			Comparison: paper.Comparison{Series: "OMXS30.INDX",
				HoldingReturn: &holdingReturn, BenchmarkReturn: &benchmarkReturn},
		}},
		Orders: []paper.Order{
			{
				ID: "11111111-1111-4111-8111-111111111111", Ticker: "ABB", Name: "ABB Ltd",
				Currency: "SEK", Direction: paper.DirectionBuy, Quantity: "400.000000000000",
				ExpectedPrice: "600.000000000000", PlacedSession: "2026-09-16",
				State: paper.StateFilled, PlacedAt: settled, SettledAt: &settled,
				Fill: &paper.Fill{
					Session: "2026-09-17", OpenPrice: "600.300000000000",
					Quantity: "400.000000000000", Costs: "252.120000000000",
					CashEffect: "-240372.120000000000", ConversionRate: "1.000000000000",
					BarDiverged: false, FilledAt: settled,
				},
			},
			{
				ID: "22222222-2222-4222-8222-222222222222", Ticker: "NOKIA", Name: "Nokia",
				Currency: "EUR", Direction: paper.DirectionBuy, Quantity: "9000.000000000000",
				ExpectedPrice: "4.100000000000", PlacedSession: "2026-09-16",
				State: paper.StateUnfillable, AbsenceReason: &reason,
				PlacedAt: settled, SettledAt: &settled,
			},
		},
		Totals: paper.Totals{
			Value: &value, Cost: "240120.000000000000", Unrealised: &unrealised,
			Realised: "0.000000000000", TotalReturn: &totalReturn, Complete: true,
		},
		IsASimulation: true,
	}, nil
}

func (s *paperServiceStub) Open(_ context.Context, userID string, request paper.OpenRequest) (paper.Account, error) {
	s.askedFor, s.openedWith = userID, request
	return paper.Account{}, s.err
}

func (s *paperServiceStub) Promote(_ context.Context, userID string, request paper.PromoteRequest) (paper.Order, error) {
	s.askedFor, s.promoted = userID, string(request.IntentID)
	return paper.Order{}, s.err
}

func (s *paperServiceStub) Cancel(_ context.Context, userID, orderID string) error {
	s.askedFor, s.cancelled = userID, orderID
	return s.err
}

func paperRouter(stub *paperServiceStub) http.Handler {
	return NewRouter(authenticatedDependencies(Dependencies{Paper: stub}))
}

// TestPaperReadsMatchTheContract covers what a client cannot recover on its own: decimals surviving
// as strings, a fill arriving with the price it actually used, and the two statements the account
// always carries.
func TestPaperReadsMatchTheContract(t *testing.T) {
	response := performRequest(paperRouter(&paperServiceStub{}), "/api/v1/paper-account")
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	body := decodeBody(t, response)

	if body["is_a_simulation"] != true {
		t.Errorf("the account does not say it is a simulation")
	}
	if body["cash"] != "727440.000000000000" {
		t.Errorf("cash did not survive as a string: %#v", body["cash"])
	}

	totals, _ := body["total"].(map[string]any)
	// The one return figure in the product, and it is present here.
	if totals["total_return"] != "0.043210000000" {
		t.Errorf("the return reads %#v", totals["total_return"])
	}

	orders, _ := body["orders"].([]any)
	if len(orders) != 2 {
		t.Fatalf("%d orders", len(orders))
	}
	filled, _ := orders[0].(map[string]any)
	fill, _ := filled["fill"].(map[string]any)
	if fill == nil || fill["open_price"] != "600.300000000000" {
		t.Fatalf("the fill is %#v", filled["fill"])
	}
	// The costs travel separately from the price, so the price can be checked against the bar.
	if fill["costs"] != "252.120000000000" || fill["cash_effect"] != "-240372.120000000000" {
		t.Errorf("the fill does not carry its arithmetic: %#v", fill)
	}

	// An order that could not be filled says why, and carries no fill at all rather than a zeroed
	// one: "it cost nothing" is a different claim from "it did not happen".
	refused, _ := orders[1].(map[string]any)
	if refused["state"] != "unfillable" || refused["absence_reason"] != "insufficient_cash" {
		t.Errorf("the refused order reads %#v", refused)
	}
	if _, present := refused["fill"]; !present {
		t.Errorf("the fill field is missing rather than null")
	}
	if refused["fill"] != nil {
		t.Errorf("an unfilled order carries a fill: %#v", refused["fill"])
	}
}

// TestNoPaperResponseFieldCouldBeSentToABroker asserts FR-003 and FR-023 at the boundary the
// outside world sees.
func TestNoPaperResponseFieldCouldBeSentToABroker(t *testing.T) {
	response := performRequest(paperRouter(&paperServiceStub{}), "/api/v1/paper-account")
	body := strings.ToLower(response.Body.String())
	for _, forbidden := range []string{
		"venue", "order_type", "time_in_force", "destination", "broker_reference", "broker_id",
		"external_id", "expires", "limit_price", "stop_price", "submit", "route",
		"leverage", "margin",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the response carries %q: %s", forbidden, response.Body.String())
		}
	}
	// "brokerage" is the fee a trade costs, which the account states rather than hides. It is not a
	// broker connection, and the check above is narrowed to the fields that would be one.
	if !strings.Contains(body, "brokerage_bps") {
		t.Errorf("the account does not state what trading costs it")
	}
}

func TestPaperWritesRequireTheCSRFHeader(t *testing.T) {
	router := paperRouter(&paperServiceStub{})
	for _, write := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/paper-account", `{"starting_cash":"1000000","accounting_currency":"SEK"}`},
		{http.MethodPost, "/api/v1/paper-account/orders", `{"intent_id":"11111111-1111-4111-8111-111111111111"}`},
		{http.MethodDelete, "/api/v1/paper-account/orders/11111111-1111-4111-8111-111111111111", ``},
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

func TestPromotingReachesTheServiceWithTheCallerAndIntent(t *testing.T) {
	stub := &paperServiceStub{}
	response := performPaperWrite(t, paperRouter(stub), http.MethodPost, "/api/v1/paper-account/orders",
		`{"intent_id":"11111111-1111-4111-8111-111111111111"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	if stub.promoted != "11111111-1111-4111-8111-111111111111" {
		t.Errorf("the service was asked to promote %q", stub.promoted)
	}
	if stub.askedFor == "" {
		t.Errorf("the service was not told whose account it is")
	}
	// A promotion answers with the whole account: a new order moves what is pending and what the
	// cash will be, and a client patching one row would show stale arithmetic beside fresh.
	if body := decodeBody(t, response); body["orders"] == nil {
		t.Errorf("the promote response is not an account: %s", response.Body.String())
	}
}

func TestOpeningAnAccountTwiceIsAConflict(t *testing.T) {
	stub := &paperServiceStub{err: paper.Refusal{
		Code: paper.RefusalAccountAlreadyOpen, Message: "You already have a paper account."}}
	response := performPaperWrite(t, paperRouter(stub), http.MethodPost, "/api/v1/paper-account",
		`{"starting_cash":"1000000","accounting_currency":"SEK"}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	failure, _ := decodeBody(t, response)["error"].(map[string]any)
	if failure["code"] != string(paper.RefusalAccountAlreadyOpen) {
		t.Errorf("the refusal reads %#v", failure)
	}
}

func TestAPersonWithoutAnAccountIsToldSo(t *testing.T) {
	response := performRequest(paperRouter(&paperServiceStub{noAccount: true}), "/api/v1/paper-account")
	if response.Code != http.StatusNotFound {
		t.Errorf("reading an unopened account returned %d", response.Code)
	}
}

func TestPaperIsRefusedWithoutASession(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/paper-account", nil)
	paperRouter(&paperServiceStub{}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("an unauthenticated read returned %d", recorder.Code)
	}
}

// No route creates an order from anything but a promotion, and these must keep not existing.
func TestNoRouteCreatesAPaperOrderFromAStrategy(t *testing.T) {
	router := paperRouter(&paperServiceStub{})
	for _, path := range []string{
		"/api/v1/paper-account/orders/from-signal",
		"/api/v1/paper-account/run",
		"/api/v1/paper-account/fill",
		"/api/v1/paper-accounts",
	} {
		for _, method := range []string{http.MethodPost, http.MethodPut} {
			request := authenticatedAPIRequest(method, path)
			request.Header.Set("X-CSRF-Token", "test-csrf")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusNotFound && recorder.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s answered %d; no route may make the product place an order",
					method, path, recorder.Code)
			}
		}
	}
}

func performPaperWrite(t *testing.T, router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := authenticatedAPIRequest(method, path)
	request.Body = io.NopCloser(bytes.NewReader([]byte(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "test-csrf")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}
