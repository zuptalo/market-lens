package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/backtest"
	"market-lens/server/internal/httpx"
)

const backtestRunID = "55000000-0000-4000-8000-000000000001"

var backtestFinished = time.Date(2026, 9, 16, 4, 0, 0, 0, time.UTC)

func backtestConfiguration() backtest.Configuration {
	return backtest.Configuration{
		ID: "00000000-0021-4000-8000-000000000101", Name: "momentum_trend_equal_weight", Version: 1,
		Title:        "Momentum and trend, equal weight, monthly",
		Intent:       "Hold the ten highest-scoring instruments in equal weight.",
		Caveat:       "This is a simulation over past data, not a prediction and not advice.",
		Strategy:     backtest.StrategyRef{Name: "momentum_trend", Version: 1},
		UniverseCode: "nordic-liquid-v1", StartingCapital: "1000000", AccountingCurrency: "EUR",
		SizingRule: backtest.SizingEqualWeightTopN, SizingN: 10, Rebalance: backtest.RebalanceMonthly,
		Costs: backtest.Costs{BrokerageBasisPoints: "10", BrokerageMinimum: "5",
			SlippageBasisPoints: "5", SpreadBasisPoints: "10"},
		PublishedAt: backtestFinished,
	}
}

func backtestSummary() backtest.Summary {
	finished := backtestFinished
	return backtest.Summary{
		Run: backtest.Run{
			ID: backtestRunID, ConfigurationID: "00000000-0021-4000-8000-000000000101",
			Status: backtest.RunStatusSucceeded, From: "2016-08-31", To: "2026-09-15",
			StrategyID: "00000000-0015-4000-8000-000000000001",
			StartedAt:  backtestFinished, FinishedAt: &finished,
			TradeCount: 312, SkipCount: 11840, RebalanceCount: 121, AppVersion: "0.17.0",
		},
		Configuration: backtestConfiguration(),
	}
}

type backtestReaderStub struct {
	cursor   string
	limit    int
	notFound bool
}

func (s *backtestReaderStub) ListRuns(_ context.Context, limit int) ([]backtest.Summary, error) {
	s.limit = limit
	return []backtest.Summary{backtestSummary()}, nil
}

func (s *backtestReaderStub) Backtest(_ context.Context, id string) (backtest.Detail, error) {
	if s.notFound {
		return backtest.Detail{}, backtest.ErrNotFound
	}
	total, annualised, volatility, drawdown, costs := "0.418200000000", "0.035600000000",
		"0.184300000000", "-0.312400000000", "18849.571698069468"
	count := int64(312)
	from, to := backtest.SessionDate("2016-08-31"), backtest.SessionDate("2026-09-15")
	danish := backtest.MeasureSeriesStartsAfter
	return backtest.Detail{
		Summary: backtestSummary(),
		Measures: []backtest.Measures{
			{Subject: backtest.MeasureSubjectStrategy, From: &from, To: &to,
				TotalReturn: &total, AnnualisedReturn: &annualised, Volatility: &volatility,
				MaxDrawdown: &drawdown, TradeCount: &count, TotalCosts: &costs},
			{Subject: "OMXC25.INDX", AbsenceReason: &danish},
		},
		Skips:   []backtest.SkipTally{{Reason: backtest.SkipNotSelected, Count: 11800}},
		Markets: map[string]string{"OMXC25.INDX": "XCSE"},
	}, nil
}

func (s *backtestReaderStub) Trades(_ context.Context, _, cursor string, limit int) (backtest.TradePage, error) {
	s.cursor, s.limit = cursor, limit
	rate := "9.456700000000"
	total := int64(312)
	page := backtest.TradePage{Items: []backtest.TradeView{{
		Trade: backtest.Trade{
			ID: "66000000-0000-4000-8000-000000000001", RunID: backtestRunID,
			InstrumentID:  "44000000-0000-4000-8000-000000000001",
			StrategyID:    "00000000-0015-4000-8000-000000000001",
			SignalSession: "2026-09-01", ExecutionSession: "2026-09-02",
			Direction: backtest.DirectionBuy, Quantity: "100.000000000000",
			Price: "42.500000000000", PriceCurrency: "SEK", FXRate: &rate,
			Brokerage: "5.000000000000", Slippage: "0.224700000000",
			SpreadCost: "0.449500000000", CashEffect: "-455.000000000000",
		},
		Ticker: "VOLV-B", Name: "Volvo AB",
	}}}
	if cursor == "" {
		page.Total = &total
	}
	return page, nil
}

func (s *backtestReaderStub) Equity(_ context.Context, _ string) ([]backtest.EquityPoint, error) {
	positions, total := "912345.000000000000", "1418200.000000000000"
	reason := backtest.EquityPositionUnvalued
	return []backtest.EquityPoint{
		{SessionDate: "2026-09-14", Cash: "505855.000000000000", PositionValue: &positions, Total: &total},
		{SessionDate: "2026-09-15", Cash: "505855.000000000000", AbsenceReason: &reason},
	}, nil
}

func backtestRouter(stub *backtestReaderStub) http.Handler {
	return NewRouter(authenticatedDependencies(Dependencies{Backtests: stub}))
}

// TestBacktestReadsMatchTheContract covers the three things a client cannot recover on its own:
// that decimals survive as strings, that all six measures are present whether or not they have
// values, and that the response says a result is a simulation.
func TestBacktestReadsMatchTheContract(t *testing.T) {
	router := backtestRouter(&backtestReaderStub{})

	response := performRequest(router, "/api/v1/backtests")
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	var list struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("listed %d backtests", len(list.Items))
	}
	if list.Items[0]["is_simulation"] != true {
		t.Errorf("a listed result does not say it is a simulation")
	}

	response = performRequest(router, "/api/v1/backtests/"+backtestRunID)
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	body := decodeBody(t, response)
	if body["is_simulation"] != true {
		t.Errorf("the result does not state that it is a simulation")
	}
	configuration, _ := body["configuration"].(map[string]any)
	if configuration["caveat"] == "" || configuration["caveat"] == nil {
		t.Errorf("the configuration's caveat is not on the response, so no screen can show it")
	}
	if configuration["starting_capital"] != "1000000" {
		t.Errorf("starting capital is %#v; a decimal must stay a string", configuration["starting_capital"])
	}

	measures, _ := body["measures"].(map[string]any)
	// All six, always. A client free to receive a subset would eventually show the flattering
	// half, and maximum drawdown is the half that goes missing.
	for _, field := range []string{"total_return", "annualised_return", "volatility",
		"maximum_drawdown", "trade_count", "total_costs"} {
		if _, present := measures[field]; !present {
			t.Errorf("%s is missing from the reported measures", field)
		}
	}
	if measures["maximum_drawdown"] != "-0.312400000000" {
		t.Errorf("maximum drawdown is %#v", measures["maximum_drawdown"])
	}

	benchmarks, _ := body["benchmarks"].([]any)
	if len(benchmarks) != 1 {
		t.Fatalf("%d benchmarks reported", len(benchmarks))
	}
	comparison, _ := benchmarks[0].(map[string]any)
	if comparison["absence_reason"] != "series_starts_after_range" || comparison["mic"] != "XCSE" {
		t.Errorf("the unavailable comparison reads %#v", comparison)
	}
}

func TestBacktestTradesPageAndNameTheirSignal(t *testing.T) {
	stub := &backtestReaderStub{}
	router := backtestRouter(stub)

	response := performRequest(router, "/api/v1/backtests/"+backtestRunID+"/trades")
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	body := decodeBody(t, response)
	if body["total"] == nil {
		t.Errorf("a cursor-less request did not report the total")
	}
	items, _ := body["items"].([]any)
	trade, _ := items[0].(map[string]any)
	// The trade names the signal that caused it, so a reader reaches the strategy's own
	// contributions rather than being asked to take the trade on trust.
	want := "44000000-0000-4000-8000-000000000001/2026-09-01/00000000-0015-4000-8000-000000000001"
	if trade["signal_id"] != want {
		t.Errorf("signal_id is %#v, want %s", trade["signal_id"], want)
	}
	if trade["price"] != "42.500000000000" || trade["conversion_rate"] != "9.456700000000" {
		t.Errorf("a decimal did not survive as a string: %#v", trade)
	}
	for _, field := range []string{"brokerage", "slippage", "currency_spread", "cash_effect"} {
		if _, present := trade[field]; !present {
			t.Errorf("%s is missing from a trade, so its costs cannot be read", field)
		}
	}

	// Paging counts only on the first request. Counting on every page would defeat the early
	// termination keyset paging exists for.
	response = performRequest(router, "/api/v1/backtests/"+backtestRunID+"/trades?cursor=abc&limit=10")
	body = decodeBody(t, response)
	if body["total"] != nil {
		t.Errorf("a paged request counted the whole set")
	}
	if stub.cursor != "abc" || stub.limit != 10 {
		t.Errorf("the cursor and limit did not reach the reader: %+v", stub)
	}
}

// TestTheEquityCurveIsAvailableAsFigures is the accessibility requirement expressed where it can
// actually be guaranteed. A reader who cannot see a canvas receives the same information, not a
// poorer version of the result.
func TestTheEquityCurveIsAvailableAsFigures(t *testing.T) {
	router := backtestRouter(&backtestReaderStub{})
	response := performRequest(router, "/api/v1/backtests/"+backtestRunID+"/equity")
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	body := decodeBody(t, response)
	if body["currency"] != "EUR" {
		t.Errorf("the curve does not say what currency it is in: %#v", body["currency"])
	}
	items, _ := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("%d points", len(items))
	}
	valued, _ := items[0].(map[string]any)
	if valued["total"] != "1418200.000000000000" {
		t.Errorf("a total did not survive as a string: %#v", valued["total"])
	}
	// A session that could not be valued says so rather than repeating the previous total.
	absent, _ := items[1].(map[string]any)
	if absent["total"] != nil || absent["absence_reason"] != "position_unvalued" {
		t.Errorf("an unvalued session reads %#v", absent)
	}
}

func TestBacktestReadsAreRefusedWithoutASession(t *testing.T) {
	router := backtestRouter(&backtestReaderStub{})
	for _, path := range []string{
		"/api/v1/backtests",
		"/api/v1/backtests/" + backtestRunID,
		"/api/v1/backtests/" + backtestRunID + "/trades",
		"/api/v1/backtests/" + backtestRunID + "/equity",
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s answered %d without a session", path, recorder.Code)
		}
	}
}

// A deactivated account holds a session token that has not expired. The persisted record decides,
// not the token, which is why this is asserted here rather than trusted to the session's lifetime.
func TestBacktestReadsAreRefusedToADeactivatedAccount(t *testing.T) {
	deps := Dependencies{Backtests: &backtestReaderStub{}}
	deps.Authenticator = sessionAuthenticatorFunc(func(context.Context, string) (auth.Principal, error) {
		return auth.Principal{}, auth.ErrAuthenticationRequired
	})
	router := NewRouter(deps)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/backtests", nil)
	request.AddCookie(&http.Cookie{Name: httpx.SessionCookieName, Value: "test-session"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("a deactivated account read the backtests: %d", recorder.Code)
	}
}

// No request may make the product compute a result. Running a backtest is an owner action at the
// command line, and these routes must keep not existing.
func TestNoRouteRunsABacktest(t *testing.T) {
	router := backtestRouter(&backtestReaderStub{})
	for _, route := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/backtests"},
		{http.MethodPost, "/api/v1/backtests/run"},
		{http.MethodPost, "/api/v1/backtests/" + backtestRunID + "/rerun"},
		{http.MethodDelete, "/api/v1/backtests/" + backtestRunID},
		{http.MethodPost, "/api/v1/series/import"},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, authenticatedAPIRequest(route.method, route.path))
		if recorder.Code != http.StatusNotFound && recorder.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s answered %d; no request may make the product compute a result",
				route.method, route.path, recorder.Code)
		}
	}
}

func TestAMissingBacktestIsNotFound(t *testing.T) {
	router := backtestRouter(&backtestReaderStub{notFound: true})
	response := performRequest(router, "/api/v1/backtests/"+backtestRunID)
	if response.Code != http.StatusNotFound {
		t.Errorf("status %d for an unknown backtest", response.Code)
	}
}
