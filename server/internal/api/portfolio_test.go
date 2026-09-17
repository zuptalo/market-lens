package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/httpx"
	"market-lens/server/internal/portfolio"
)

const (
	aliceUserID = "10000000-0000-4000-8000-000000000001"
	bobUserID   = "10000000-0000-4000-8000-000000000002"
	tradeID     = "88000000-0022-4000-8000-000000000001"
)

var portfolioRecordedAt = time.Date(2026, 9, 17, 8, 0, 0, 0, time.UTC)

// portfolioServiceStub keeps one portfolio per user, so a test can assert that a handler asked for
// the caller's own rather than for one it was told about.
type portfolioServiceStub struct {
	askedFor string
	refusal  error
}

func (s *portfolioServiceStub) View(_ context.Context, userID string) (portfolio.View, error) {
	s.askedFor = userID
	if userID != aliceUserID {
		// Everybody else has nothing, which is what an empty portfolio looks like.
		return portfolio.View{Portfolio: portfolio.Portfolio{AccountingCurrency: "EUR"},
			Holdings: []portfolio.Holding{}, Realised: []portfolio.RealisedResult{},
			Totals:                portfolio.Totals{Cost: "0", Realised: "0", Complete: true, ReturnAbsence: portfolio.ReturnAbsence},
			RecordsWhatYouEntered: true}, nil
	}
	value, session := "22586.666666666663", portfolio.SessionDate("2026-09-16")
	rate, unrealised := "1.466666666667", "6586.666666666663"
	from, to := portfolio.SessionDate("2026-03-16"), portfolio.SessionDate("2026-09-16")
	holdingReturn, benchmarkReturn := "0.410000000000", "0.120000000000"
	total, totalUnrealised := "22586.666666666663", "6586.666666666663"
	return portfolio.View{
		Portfolio: portfolio.Portfolio{AccountingCurrency: "SEK"},
		Holdings: []portfolio.Holding{{
			InstrumentID: "44000000-0000-4000-8000-000000000001", Ticker: "VOLV-B",
			Name: "Volvo AB", Currency: "DKK", Quantity: "100.000000000000",
			Cost: "16000.000000000000", Unrealised: &unrealised,
			Valuation: portfolio.Valuation{Value: &value, Session: &session, ConversionRate: &rate},
			Comparison: portfolio.Comparison{Series: "OMXC25.INDX", FromSession: &from, ToSession: &to,
				HoldingReturn: &holdingReturn, BenchmarkReturn: &benchmarkReturn},
		}},
		Realised: []portfolio.RealisedResult{{
			InstrumentID: "44000000-0000-4000-8000-000000000002", Ticker: "NOKIA",
			Quantity: "50.000000000000", Proceeds: "6000.000000000000",
			Cost: "5000.000000000000", Realised: "1000.000000000000",
			CostBasis: portfolio.CostBasisFIFO,
		}},
		Totals: portfolio.Totals{Value: &total, Cost: "16000.000000000000",
			Unrealised: &totalUnrealised, Realised: "1000.000000000000", Complete: true,
			ReturnAbsence: portfolio.ReturnAbsence},
		RecordsWhatYouEntered: true,
	}, nil
}

func (s *portfolioServiceStub) SetCurrency(_ context.Context, userID, currency string) (portfolio.Portfolio, error) {
	s.askedFor = userID
	if s.refusal != nil {
		return portfolio.Portfolio{}, s.refusal
	}
	return portfolio.Portfolio{AccountingCurrency: currency}, nil
}

func (s *portfolioServiceStub) Record(_ context.Context, userID string, request portfolio.RecordRequest) (portfolio.Trade, error) {
	s.askedFor = userID
	if s.refusal != nil {
		return portfolio.Trade{}, s.refusal
	}
	return portfolio.Trade{ID: tradeID, InstrumentID: request.InstrumentID, Ticker: "VOLV-B",
		Name: "Volvo AB", Currency: "SEK", Direction: request.Direction,
		Quantity: request.Quantity, Price: request.Price, Costs: "39.000000000000",
		TradeDate: request.TradeDate, Sequence: 1, Status: portfolio.TradeCurrent,
		RecordedAt: portfolioRecordedAt}, nil
}

func (s *portfolioServiceStub) Correct(_ context.Context, userID, id string, request portfolio.RecordRequest) (portfolio.Trade, error) {
	s.askedFor = userID
	if s.refusal != nil {
		return portfolio.Trade{}, s.refusal
	}
	superseded := portfolio.UUID("88000000-0022-4000-8000-0000000000ff")
	changed := portfolioRecordedAt
	return portfolio.Trade{ID: portfolio.UUID(id), InstrumentID: request.InstrumentID,
		Ticker: "VOLV-B", Name: "Volvo AB", Currency: "SEK", Direction: request.Direction,
		Quantity: request.Quantity, Price: request.Price, Costs: "39.000000000000",
		TradeDate: request.TradeDate, Sequence: 1, Status: portfolio.TradeCurrent,
		Supersedes: &superseded, RecordedAt: portfolioRecordedAt, ChangedAt: &changed}, nil
}

func (s *portfolioServiceStub) Withdraw(_ context.Context, userID, _ string) error {
	s.askedFor = userID
	return s.refusal
}

func (s *portfolioServiceStub) Trades(_ context.Context, userID string, query portfolio.TradeQuery) (portfolio.TradePage, error) {
	s.askedFor = userID
	total := int64(1)
	page := portfolio.TradePage{Items: []portfolio.Trade{{
		ID: tradeID, InstrumentID: "44000000-0000-4000-8000-000000000001", Ticker: "VOLV-B",
		Name: "Volvo AB", Currency: "SEK", Direction: portfolio.DirectionBuy,
		Quantity: "100.000000000000", Price: "245.800000000000", Costs: "39.000000000000",
		TradeDate: "2026-03-16", Sequence: 1, Status: portfolio.TradeCurrent,
		RecordedAt: portfolioRecordedAt,
	}}}
	if query.Cursor == "" {
		page.Total = &total
	}
	return page, nil
}

func portfolioRouter(stub *portfolioServiceStub) http.Handler {
	return NewRouter(authenticatedDependencies(Dependencies{Portfolio: stub}))
}

// TestPortfolioReadsMatchTheContract covers what a client cannot recover on its own: that decimals
// survive as strings, that the two statements the product is required to make are always present,
// and that a realised figure never appears without the basis that produced it.
func TestPortfolioReadsMatchTheContract(t *testing.T) {
	router := portfolioRouter(&portfolioServiceStub{})

	response := performRequest(router, "/api/v1/portfolio")
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	body := decodeBody(t, response)

	if body["records_what_you_entered"] != true {
		t.Errorf("the portfolio does not state that it records what a person entered")
	}
	totals, _ := body["total"].(map[string]any)
	if totals["return_absence"] != portfolio.ReturnAbsence {
		t.Errorf("the total does not state why there is no return: %#v", totals["return_absence"])
	}
	if _, present := totals["return"]; present {
		t.Errorf("a portfolio return was reported; the product does not know what was paid in")
	}
	if totals["value"] != "22586.666666666663" {
		t.Errorf("a total did not survive as a string: %#v", totals["value"])
	}

	holdings, _ := body["holdings"].([]any)
	holding, _ := holdings[0].(map[string]any)
	valuation, _ := holding["valuation"].(map[string]any)
	if valuation["session"] != "2026-09-16" {
		t.Errorf("the holding does not say which session priced it: %#v", valuation["session"])
	}
	if valuation["conversion_rate"] != "1.466666666667" {
		t.Errorf("the conversion rate is %#v; a reader cannot reproduce the figure without it", valuation["conversion_rate"])
	}
	comparison, _ := holding["comparison"].(map[string]any)
	for _, field := range []string{"series", "from_session", "to_session", "holding_return", "benchmark_return"} {
		if _, present := comparison[field]; !present {
			t.Errorf("%s is missing from the comparison", field)
		}
	}

	realised, _ := body["realised"].([]any)
	first, _ := realised[0].(map[string]any)
	if first["cost_basis"] != portfolio.CostBasisFIFO {
		t.Errorf("a realised figure appeared without its cost basis: %#v", first)
	}
}

// TestAPortfolioIsAlwaysTheCallersOwn. The user identifier comes from the persisted principal and
// never from the request, which is the difference between a private record and a guessable one.
func TestAPortfolioIsAlwaysTheCallersOwn(t *testing.T) {
	stub := &portfolioServiceStub{}
	router := portfolioRouter(stub)

	// There is no route that takes a user identifier at all, so the only thing to assert is that
	// the handler asked for the authenticated caller.
	performRequest(router, "/api/v1/portfolio")
	if stub.askedFor != aliceUserID {
		t.Errorf("the handler asked for %q, want the authenticated caller", stub.askedFor)
	}

	// And a query parameter that looks like one changes nothing.
	performRequest(router, "/api/v1/portfolio?user_id="+bobUserID)
	if stub.askedFor != aliceUserID {
		t.Errorf("a user_id parameter redirected the read to %q", stub.askedFor)
	}
}

func TestPortfolioWritesRefuseWithSomethingActionable(t *testing.T) {
	stub := &portfolioServiceStub{refusal: portfolio.Refusal{
		Code:    portfolio.RefusalSaleExceedsPosition,
		Message: "That is more than you hold. You hold 100.",
		Held:    "100.000000000000",
	}}
	router := portfolioRouter(stub)

	recorder := httptest.NewRecorder()
	request := requestWithJSON(http.MethodPost, "/api/v1/portfolio/trades", map[string]any{
		"instrument_id": "44000000-0000-4000-8000-000000000001", "direction": "sell",
		"quantity": "150", "price": "110", "trade_date": "2026-03-16"})
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Held    string `json:"held_quantity"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != portfolio.RefusalSaleExceedsPosition {
		t.Errorf("the refusal code is %q", body.Error.Code)
	}
	// The refusal names what is actually held. "That is more than you hold" is only useful with the
	// quantity attached.
	if body.Error.Held != "100.000000000000" {
		t.Errorf("the refusal does not say what is held: %q", body.Error.Held)
	}
}

func TestAnotherPersonsTradeIsNotFound(t *testing.T) {
	stub := &portfolioServiceStub{refusal: portfolio.ErrNotFound}
	router := portfolioRouter(stub)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, requestWithJSON(http.MethodPatch, "/api/v1/portfolio/trades/"+tradeID,
		map[string]any{"instrument_id": "44000000-0000-4000-8000-000000000001",
			"direction": "buy", "quantity": "1", "price": "1", "trade_date": "2026-03-16"}))
	// The same answer a trade that does not exist gets, so the response never confirms which
	// identifiers are real.
	if recorder.Code != http.StatusNotFound {
		t.Errorf("correcting somebody else's trade answered %d", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	withdrawal := authenticatedAPIRequest(http.MethodDelete, "/api/v1/portfolio/trades/"+tradeID)
	withdrawal.Header.Set("X-CSRF-Token", "test-csrf")
	router.ServeHTTP(recorder, withdrawal)
	if recorder.Code != http.StatusNotFound {
		t.Errorf("withdrawing somebody else's trade answered %d", recorder.Code)
	}
}

func TestPortfolioIsRefusedWithoutASession(t *testing.T) {
	router := portfolioRouter(&portfolioServiceStub{})
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/portfolio"},
		{http.MethodPut, "/api/v1/portfolio"},
		{http.MethodGet, "/api/v1/portfolio/trades"},
		{http.MethodPost, "/api/v1/portfolio/trades"},
		{http.MethodPatch, "/api/v1/portfolio/trades/" + tradeID},
		{http.MethodDelete, "/api/v1/portfolio/trades/" + tradeID},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(route.method, route.path, nil))
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s %s answered %d without a session", route.method, route.path, recorder.Code)
		}
	}
}

// A deactivated account holds a session token that has not expired. The persisted record decides.
func TestPortfolioIsRefusedToADeactivatedAccount(t *testing.T) {
	deps := Dependencies{Portfolio: &portfolioServiceStub{}}
	deps.Authenticator = sessionAuthenticatorFunc(func(context.Context, string) (auth.Principal, error) {
		return auth.Principal{}, auth.ErrAuthenticationRequired
	})
	router := NewRouter(deps)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/portfolio", nil)
	request.AddCookie(&http.Cookie{Name: httpx.SessionCookieName, Value: "test-session"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("a deactivated account read a portfolio: %d", recorder.Code)
	}
}

// No route lets one person reach another's portfolio, and these must keep not existing.
func TestNoRouteReachesAnotherPersonsPortfolio(t *testing.T) {
	router := portfolioRouter(&portfolioServiceStub{})
	for _, path := range []string{
		"/api/v1/portfolios",
		"/api/v1/users/" + bobUserID + "/portfolio",
		"/api/v1/owner/portfolios",
		"/api/v1/owner/users/" + bobUserID + "/portfolio",
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, authenticatedAPIRequest(http.MethodGet, path))
		if recorder.Code != http.StatusNotFound {
			t.Errorf("GET %s answered %d; ownership here is not administrative", path, recorder.Code)
		}
	}
}

// requestWithJSON is an authenticated write, carrying the CSRF token every state change in this
// product requires. The token is asserted separately below: it is not a formality here.
func requestWithJSON(method, path string, body map[string]any) *http.Request {
	encoded, _ := json.Marshal(body)
	request := authenticatedAPIRequest(method, path)
	request.Body = io.NopCloser(bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "test-csrf")
	return request
}

// TestPortfolioWritesRequireCSRF. Recording a trade changes stored state, so it carries the same
// cross-site protection every other mutation does — a portfolio nobody else can read is still a
// portfolio somebody else's page could write to.
func TestPortfolioWritesRequireCSRF(t *testing.T) {
	router := portfolioRouter(&portfolioServiceStub{})
	for _, route := range []struct{ method, path string }{
		{http.MethodPut, "/api/v1/portfolio"},
		{http.MethodPost, "/api/v1/portfolio/trades"},
		{http.MethodPatch, "/api/v1/portfolio/trades/" + tradeID},
		{http.MethodDelete, "/api/v1/portfolio/trades/" + tradeID},
	} {
		recorder := httptest.NewRecorder()
		// Authenticated, but with no token.
		router.ServeHTTP(recorder, authenticatedAPIRequest(route.method, route.path))
		if recorder.Code != http.StatusForbidden {
			t.Errorf("%s %s answered %d without a CSRF token", route.method, route.path, recorder.Code)
		}
	}
}
