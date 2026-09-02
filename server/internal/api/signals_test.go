package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/strategies"
)

const signalInstrumentID = "44000000-0000-4000-8000-000000000001"

var signalComputedAt = time.Date(2026, 7, 1, 4, 0, 0, 0, time.UTC)

func signalStrategy() strategies.Strategy {
	weight := 0.25
	return strategies.Strategy{
		ID: "00000000-0015-4000-8000-000000000001", Name: "momentum_trend", Version: 1,
		Title:  "Momentum and trend",
		Intent: "Ranks the curated universe by medium-term momentum and trend agreement.",
		Caveat: "Its weights are stated rather than fitted, and nothing it produces is advice.",
		Factors: []strategies.Factor{
			{Name: "momentum_90", Feature: "return_90", Mode: strategies.CrossSectional, Weight: weight,
				Description: "Ninety-session return, ranked against the universe."},
		},
		ActionBands: []strategies.ActionBand{{Lower: -1, Upper: 1, Action: "HOLD"}},
		PublishedAt: signalComputedAt,
	}
}

func scoredSignal(id string) strategies.Signal {
	score, confidence, divisor := "0.412500000000", "0.875000000000", "0.250000000000"
	factorScore, contribution := 0.4, 0.1
	action := strategies.ActionWatch
	value := "0.081234000000"
	return strategies.Signal{
		InstrumentID: strategies.UUID(id), SessionDate: "2026-06-30",
		StrategyID: "00000000-0015-4000-8000-000000000001",
		Score:      &score, Action: &action, Confidence: &confidence, Divisor: &divisor,
		ComputedAt: signalComputedAt,
		Contributions: []strategies.Contribution{{
			Factor: "momentum_90", Feature: "return_90", FeatureValue: &value,
			FeatureSession: "2026-06-30", FactorScore: &factorScore, Weight: 0.25,
			Contribution: &contribution,
		}},
	}
}

func absentSignal(id string) strategies.Signal {
	reason := strategies.AbsenceInsufficientHistory
	return strategies.Signal{
		InstrumentID: strategies.UUID(id), SessionDate: "2026-06-30",
		StrategyID: "00000000-0015-4000-8000-000000000001", AbsenceReason: &reason,
		ComputedAt: signalComputedAt, Contributions: []strategies.Contribution{},
	}
}

type signalReaderStub struct {
	rankingRequest strategies.RankingRequest
	includeAll     bool
	runLimit       int
}

func (s *signalReaderStub) SignalAsOf(_ context.Context, id strategies.UUID, asOf strategies.SessionDate,
	_ string, _ int) (strategies.SignalView, error) {
	if id.String() != signalInstrumentID {
		return strategies.SignalView{}, strategies.ErrNotFound
	}
	if asOf == "2016-01-04" {
		return strategies.SignalView{}, strategies.ErrNoSignal
	}
	return strategies.SignalView{Signal: scoredSignal(signalInstrumentID), Strategy: signalStrategy()}, nil
}

func (s *signalReaderStub) Ranking(_ context.Context, request strategies.RankingRequest) (strategies.RankingPage, error) {
	s.rankingRequest = request
	total := int64(2)
	rank := int64(1)
	page := strategies.RankingPage{
		Strategy: signalStrategy(), SessionDate: "2026-06-30", Scored: 1, Unscored: 1,
		NextCursor: "eyJnIjowfQ",
		Items: []strategies.RankedSignal{
			{Signal: scoredSignal(signalInstrumentID), Ticker: "ALFA", Name: "Alfa AB", Rank: &rank},
			{Signal: absentSignal("44000000-0000-4000-8000-000000000002"), Ticker: "BETA", Name: "Beta AB"},
		},
	}
	if request.Cursor == "" {
		page.Total = &total
	}
	return page, nil
}

func (s *signalReaderStub) Strategies(_ context.Context, _ string, includeSuperseded bool) ([]strategies.Strategy, error) {
	s.includeAll = includeSuperseded
	return []strategies.Strategy{signalStrategy()}, nil
}

func (s *signalReaderStub) ListRuns(_ context.Context, limit int) ([]strategies.Run, error) {
	s.runLimit = limit
	finished := signalComputedAt
	return []strategies.Run{{
		ID: "44000000-0000-4000-8000-0000000000aa", StrategyID: "00000000-0015-4000-8000-000000000001",
		Kind: strategies.RunKindIncremental, Status: strategies.RunStatusPartial,
		StartedAt: signalComputedAt, FinishedAt: &finished,
		InstrumentCount: 99, SignalCount: 99, FailedCount: 1, AppVersion: "0.12.0",
	}}, nil
}

func signalRouter(stub *signalReaderStub) http.Handler {
	return NewRouter(authenticatedDependencies(Dependencies{Signals: stub}))
}

// TestInstrumentSignalEndpointReturnsTheContractsShape checks the two things a client cannot
// recover on its own: that decimals survive as strings, and that the caveat is on the response.
// A caveat the API omits is a caveat no interface can show.
func TestInstrumentSignalEndpointReturnsTheContractsShape(t *testing.T) {
	router := signalRouter(&signalReaderStub{})
	response := performRequest(router, "/api/v1/instruments/"+signalInstrumentID+"/signal")
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	body := decodeBody(t, response)
	if body["score"] != "0.412500000000" {
		t.Errorf("score is %#v; a decimal must stay a string", body["score"])
	}
	if body["confidence"] != "0.875000000000" || body["divisor"] != "0.250000000000" {
		t.Errorf("confidence %#v divisor %#v", body["confidence"], body["divisor"])
	}
	if body["action"] != "WATCH" || body["absence_reason"] != nil {
		t.Errorf("a scored signal settled both sides: action %#v absence %#v", body["action"], body["absence_reason"])
	}
	strategy, _ := body["strategy"].(map[string]any)
	if strategy == nil || strategy["caveat"] == "" || strategy["caveat"] == nil {
		t.Fatalf("the response carries no caveat: %#v", body["strategy"])
	}
	if strategy["name"] != "momentum_trend" || strategy["version"] != float64(1) || strategy["superseded"] != false {
		t.Errorf("strategy reference is %#v", strategy)
	}
	contributions, _ := body["contributions"].([]any)
	if len(contributions) != 1 {
		t.Fatalf("contributions are %#v", body["contributions"])
	}
	first, _ := contributions[0].(map[string]any)
	for _, field := range []string{"factor", "feature", "feature_session", "factor_score", "weight", "contribution"} {
		if first[field] == nil {
			t.Errorf("contribution field %q is absent", field)
		}
	}
	if first["weight"] != "0.250000000000" || first["contribution"] != "0.100000000000" {
		t.Errorf("contribution decimals are %#v and %#v", first["weight"], first["contribution"])
	}
}

// TestASignalEndpointAnswersAnUnknownInstrumentAndAnEmptyHistoryIdentically: both are 404 with
// the same code, so a caller cannot use the endpoint to learn which identifiers exist.
func TestASignalEndpointAnswersAnUnknownInstrumentAndAnEmptyHistoryIdentically(t *testing.T) {
	router := signalRouter(&signalReaderStub{})
	unknown := performRequest(router, "/api/v1/instruments/44000000-0000-4000-8000-000000000999/signal")
	empty := performRequest(router, "/api/v1/instruments/"+signalInstrumentID+"/signal?as_of=2016-01-04")
	if unknown.Code != http.StatusNotFound || empty.Code != http.StatusNotFound {
		t.Fatalf("unknown %d, empty %d", unknown.Code, empty.Code)
	}
	if unknown.Body.String() != empty.Body.String() {
		t.Fatalf("the two answers differ:\n%s\n%s", unknown.Body.String(), empty.Body.String())
	}
	bad := performRequest(router, "/api/v1/instruments/"+signalInstrumentID+"/signal?as_of=notadate")
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("a malformed date returned %d", bad.Code)
	}
}

// TestSignalsEndpointRanksTheUniverse asserts the ranking's shape, including the separation of
// scored from unscored — the field an interface needs in order not to present an absence as the
// weakest instrument in the list.
func TestSignalsEndpointRanksTheUniverse(t *testing.T) {
	stub := &signalReaderStub{}
	router := signalRouter(stub)
	response := performRequest(router, "/api/v1/signals")
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	body := decodeBody(t, response)
	if body["session_date"] != "2026-06-30" || body["scored"] != float64(1) || body["unscored"] != float64(1) {
		t.Errorf("session %#v scored %#v unscored %#v", body["session_date"], body["scored"], body["unscored"])
	}
	if body["total"] != float64(2) {
		t.Errorf("a cursor-less request reported total %#v", body["total"])
	}
	if body["next_cursor"] != "eyJnIjowfQ" {
		t.Errorf("next_cursor is %#v", body["next_cursor"])
	}
	strategy, _ := body["strategy"].(map[string]any)
	if strategy == nil || strategy["caveat"] == nil {
		t.Fatalf("the ranking carries no caveat")
	}
	items, _ := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items are %#v", body["items"])
	}
	first, _ := items[0].(map[string]any)
	second, _ := items[1].(map[string]any)
	if first["rank"] != float64(1) || first["ticker"] != "ALFA" {
		t.Errorf("the first row is %#v", first)
	}
	if second["rank"] != nil {
		t.Errorf("an unscored instrument carries rank %#v", second["rank"])
	}
	if second["absence_reason"] != "insufficient_history" || second["action"] != nil {
		t.Errorf("the unscored row is %#v", second)
	}

	// A paged request must not ask for the count again.
	performRequest(router, "/api/v1/signals?cursor=abc&limit=10&as_of=2026-06-29&version=2&strategy=momentum_trend")
	if stub.rankingRequest.Cursor != "abc" || stub.rankingRequest.Limit != 10 ||
		stub.rankingRequest.AsOf != "2026-06-29" || stub.rankingRequest.Version != 2 ||
		stub.rankingRequest.Strategy != "momentum_trend" {
		t.Errorf("the request forwarded was %#v", stub.rankingRequest)
	}
	if bad := performRequest(router, "/api/v1/signals?limit=500"); bad.Code != http.StatusBadRequest {
		t.Errorf("an out-of-range limit returned %d", bad.Code)
	}
}

func TestStrategiesEndpointCarriesIntentAndBands(t *testing.T) {
	stub := &signalReaderStub{}
	router := signalRouter(stub)
	response := performRequest(router, "/api/v1/strategies")
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	items, _ := decodeBody(t, response)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items are %#v", items)
	}
	strategy, _ := items[0].(map[string]any)
	if strategy["intent"] == nil || strategy["caveat"] == nil {
		t.Fatalf("a strategy without its intent or caveat: %#v", strategy)
	}
	factors, _ := strategy["factors"].([]any)
	if len(factors) != 1 {
		t.Fatalf("factors are %#v", strategy["factors"])
	}
	factor, _ := factors[0].(map[string]any)
	if factor["weight"] != "0.250000000000" || factor["mode"] != "cross_sectional" {
		t.Errorf("factor is %#v", factor)
	}
	bands, _ := strategy["action_bands"].([]any)
	if len(bands) != 1 {
		t.Fatalf("action bands are %#v", strategy["action_bands"])
	}
	if !stub.includeAll {
		t.Errorf("superseded versions are excluded by default; they must be readable")
	}
	performRequest(router, "/api/v1/strategies?include_superseded=false")
	if stub.includeAll {
		t.Errorf("include_superseded=false was not forwarded")
	}
}

func TestListingStrategyRunsReturnsTheMostRecentFirst(t *testing.T) {
	stub := &signalReaderStub{}
	router := signalRouter(stub)
	response := performRequest(router, "/api/v1/strategy-runs?limit=5")
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	if stub.runLimit != 5 {
		t.Errorf("limit forwarded was %d", stub.runLimit)
	}
	items, _ := decodeBody(t, response)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items are %#v", items)
	}
	run, _ := items[0].(map[string]any)
	// The failed count is what makes a partial run legible: it left the previous signals
	// standing for the instruments it could not compute, which is correct and invisible.
	if run["status"] != "partial" || run["failed_count"] != float64(1) {
		t.Errorf("run is %#v", run)
	}
	if run["kind"] != "incremental" || run["signal_count"] != float64(99) {
		t.Errorf("run is %#v", run)
	}
	if bad := performRequest(router, "/api/v1/strategy-runs?limit=0"); bad.Code != http.StatusBadRequest {
		t.Errorf("an out-of-range limit returned %d", bad.Code)
	}
}

func TestSignalEndpointsRequireAnActiveSession(t *testing.T) {
	paths := []string{
		"/api/v1/instruments/" + signalInstrumentID + "/signal",
		"/api/v1/signals", "/api/v1/strategies", "/api/v1/strategy-runs",
	}
	t.Run("no cookie", func(t *testing.T) {
		router := NewRouter(Dependencies{Signals: &signalReaderStub{}})
		for _, path := range paths {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
			if recorder.Code != http.StatusUnauthorized {
				t.Errorf("%s anonymous: %d %s", path, recorder.Code, recorder.Body.String())
			}
		}
	})
	for name, err := range map[string]error{
		"revoked session":    auth.ErrAuthenticationRequired,
		"deactivated member": auth.ErrMemberLocked,
	} {
		t.Run(name, func(t *testing.T) {
			deps := Dependencies{Signals: &signalReaderStub{}}
			deps.Authenticator = sessionAuthenticatorFunc(func(context.Context, string) (auth.Principal, error) {
				return auth.Principal{}, err
			})
			router := NewRouter(deps)
			for _, path := range paths {
				response := performRequest(router, path)
				if response.Code != http.StatusUnauthorized {
					t.Errorf("%s with a %s: %d %s", path, name, response.Code, response.Body.String())
				}
			}
		})
	}
}

var _ = json.Marshal
