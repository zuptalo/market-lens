package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/features"
	"market-lens/server/internal/instruments"
)

const featureInstrumentID = "33000000-0000-4000-8000-000000000001"

// featureReaderStub answers the way the repository would for one instrument with a stored
// history ending on 2026-06-30, so the handler's mapping can be tested without a database.
type featureReaderStub struct {
	received    features.ReadRequest
	definitions []features.Definition
	listName    string
	listAll     bool
}

func stringPtr(value string) *string { return &value }

func (s *featureReaderStub) Read(_ context.Context, request features.ReadRequest) (features.FeatureSet, error) {
	s.received = request
	if request.InstrumentID.String() != featureInstrumentID {
		return features.FeatureSet{}, features.ErrNoHistory
	}
	switch request.AsOf {
	case "2026-07-04", "2026-07-05":
		return features.FeatureSet{}, features.ErrClosedDate
	case "2016-01-04":
		return features.FeatureSet{}, features.ErrNoHistory
	}
	known := []string{"regime", "relative_strength_20", "return_20", "sma_20", "volume_ratio_20"}
	for _, name := range request.Features {
		found := false
		for _, candidate := range known {
			found = found || candidate == name
		}
		if !found {
			return features.FeatureSet{}, &features.UnknownFeatureError{Name: name, Known: known}
		}
	}
	session := request.AsOf
	if session == "" || session > "2026-06-30" {
		session = "2026-06-30"
	}
	window20 := 20
	computedAt := time.Date(2026, 7, 1, 3, 0, 0, 0, time.UTC)
	reason := features.AbsenceZeroDenominator
	all := []features.Value{
		{Name: "regime", DefinitionVersion: 1, WindowSessions: &window20, SessionDate: session, Label: stringPtr("range_bound"), ComputedAt: computedAt},
		{Name: "relative_strength_20", DefinitionVersion: 1, WindowSessions: &window20, SessionDate: session, Value: stringPtr("0.012345678901"),
			ComparedTo: &features.CompositeReference{Composite: "universe_equal_weighted", Version: 1, ContributorCount: 12}, ComputedAt: computedAt},
		{Name: "return_20", DefinitionVersion: 1, WindowSessions: &window20, SessionDate: session, Value: stringPtr("0.031400000000"), ComputedAt: computedAt},
		{Name: "sma_20", DefinitionVersion: 1, WindowSessions: &window20, SessionDate: session, Value: stringPtr("123.450000000000"), Currency: stringPtr("EUR"), ComputedAt: computedAt},
		{Name: "volume_ratio_20", DefinitionVersion: 1, WindowSessions: &window20, SessionDate: session, AbsenceReason: &reason, ComputedAt: computedAt},
	}
	set := features.FeatureSet{InstrumentID: request.InstrumentID, SessionDate: session, NotComputed: []string{}}
	for _, value := range all {
		if len(request.Features) == 0 {
			set.Features = append(set.Features, value)
			continue
		}
		for _, name := range request.Features {
			if name == value.Name {
				set.Features = append(set.Features, value)
			}
		}
	}
	if session < "2026-06-30" {
		// An earlier session has no relative strength computed yet.
		set.Features = set.Features[:1]
		set.NotComputed = []string{"relative_strength_20", "return_20", "sma_20", "volume_ratio_20"}
	}
	return set, nil
}

// ListRuns keeps the stub satisfying the reader interface; the operational tests use
// featureRunReaderStub, which answers with runs.
func (s *featureReaderStub) ListRuns(context.Context, int) ([]features.Run, error) {
	return nil, nil
}

func (s *featureReaderStub) ListDefinitions(_ context.Context, name string, includeSuperseded bool) ([]features.Definition, error) {
	s.listName, s.listAll = name, includeSuperseded
	var out []features.Definition
	for _, definition := range s.definitions {
		if (name == "" || definition.Name == name) && (includeSuperseded || definition.SupersededAt == nil) {
			out = append(out, definition)
		}
	}
	return out, nil
}

func featureRouter(stub *featureReaderStub) http.Handler {
	return NewRouter(authenticatedDependencies(Dependencies{Features: stub, Instruments: &instrumentReaderStub{err: instruments.ErrNotFound}}))
}

func decodeBody(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q is not a JSON object: %v", response.Body.String(), err)
	}
	return body
}

func TestInstrumentFeaturesEndpointReturnsTheContractsShape(t *testing.T) {
	stub := &featureReaderStub{}
	response := performRequest(featureRouter(stub), "/api/v1/instruments/"+featureInstrumentID+"/features")
	if response.Code != http.StatusOK {
		t.Fatalf("status %d %s", response.Code, response.Body.String())
	}
	body := decodeBody(t, response)
	for _, key := range []string{"instrumentId", "sessionDate", "features", "notComputed"} {
		if _, ok := body[key]; !ok {
			t.Errorf("response lacks %s", key)
		}
	}
	if body["instrumentId"] != featureInstrumentID || body["sessionDate"] != "2026-06-30" {
		t.Errorf("identity = %v as of %v", body["instrumentId"], body["sessionDate"])
	}
	if notComputed, ok := body["notComputed"].([]any); !ok || len(notComputed) != 0 {
		t.Errorf("notComputed = %v, expected an empty array, never null", body["notComputed"])
	}
	values, _ := body["features"].([]any)
	if len(values) != 5 {
		t.Fatalf("features = %v", body["features"])
	}
	for _, raw := range values {
		value := raw.(map[string]any)
		name, _ := value["name"].(string)
		for _, key := range []string{"name", "definitionVersion", "windowSessions", "value", "label", "absenceReason", "computedAt"} {
			if _, ok := value[key]; !ok {
				t.Errorf("%s lacks %s", name, key)
			}
		}
		if _, isNumber := value["value"].(float64); isNumber {
			t.Errorf("%s value is a JSON number %v; the contract requires a decimal string", name, value["value"])
		}
		settled := 0
		for _, key := range []string{"value", "label", "absenceReason"} {
			if value[key] != nil {
				settled++
			}
		}
		if settled != 1 {
			t.Errorf("%s settles %d of value/label/absenceReason, expected exactly one: %v", name, settled, value)
		}
		compared, hasCompared := value["comparedTo"]
		if name == "relative_strength_20" {
			reference, _ := compared.(map[string]any)
			if reference["composite"] != "universe_equal_weighted" || reference["version"] != float64(1) || reference["contributorCount"] != float64(12) {
				t.Errorf("relative_strength_20 comparedTo = %v", compared)
			}
		} else if hasCompared && compared != nil {
			t.Errorf("%s carries comparedTo %v; only relative strength does", name, compared)
		}
		switch name {
		case "regime":
			if value["label"] != "range_bound" {
				t.Errorf("regime = %v", value)
			}
		case "sma_20":
			if value["value"] != "123.450000000000" || value["currency"] != "EUR" {
				t.Errorf("sma_20 = %v", value)
			}
		case "volume_ratio_20":
			if value["absenceReason"] != "zero_denominator" {
				t.Errorf("volume_ratio_20 = %v", value)
			}
		case "return_20":
			if currency, ok := value["currency"]; ok && currency != nil {
				t.Errorf("return_20 carries currency %v", currency)
			}
		}
	}
}

func TestInstrumentFeaturesEndpointHonoursAsOfAndFeatureFilters(t *testing.T) {
	stub := &featureReaderStub{}
	router := featureRouter(stub)
	response := performRequest(router, "/api/v1/instruments/"+featureInstrumentID+"/features?asOf=2026-05-04&feature=return_20&feature=regime")
	if response.Code != http.StatusOK {
		t.Fatalf("status %d %s", response.Code, response.Body.String())
	}
	if stub.received.AsOf != "2026-05-04" || strings.Join(stub.received.Features, ",") != "return_20,regime" {
		t.Errorf("reader received %+v", stub.received)
	}
	body := decodeBody(t, response)
	if body["sessionDate"] != "2026-05-04" {
		t.Errorf("sessionDate = %v, expected the earlier session", body["sessionDate"])
	}
	if notComputed, _ := body["notComputed"].([]any); len(notComputed) != 4 {
		t.Errorf("notComputed = %v", body["notComputed"])
	}
	if code := performRequest(router, "/api/v1/instruments/"+featureInstrumentID+"/features?asOf=yesterday").Code; code != http.StatusBadRequest {
		t.Errorf("a malformed asOf returned %d, expected 400", code)
	}
}

func TestInstrumentFeaturesEndpointRefusesAnUnknownFeatureWithTheKnownList(t *testing.T) {
	response := performRequest(featureRouter(&featureReaderStub{}), "/api/v1/instruments/"+featureInstrumentID+"/features?feature=sharpe_ratio")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status %d %s", response.Code, response.Body.String())
	}
	body := decodeBody(t, response)
	errorBody, _ := body["error"].(map[string]any)
	known, _ := errorBody["knownFeatures"].([]any)
	if errorBody["code"] != "unknown_feature" || len(known) != 5 || known[0] != "regime" {
		t.Errorf("error = %v, expected unknown_feature with the known features", body["error"])
	}
	if message, _ := errorBody["message"].(string); !strings.Contains(message, "sharpe_ratio") {
		t.Errorf("message %q does not name the unknown feature", message)
	}
}

func TestInstrumentFeaturesEndpointRefusesAClosedDate(t *testing.T) {
	response := performRequest(featureRouter(&featureReaderStub{}), "/api/v1/instruments/"+featureInstrumentID+"/features?asOf=2026-07-04")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status %d %s", response.Code, response.Body.String())
	}
	errorBody, _ := decodeBody(t, response)["error"].(map[string]any)
	if errorBody["code"] != "closed_date" {
		t.Errorf("error = %v", errorBody)
	}
}

func TestInstrumentFeaturesEndpointReportsNoHistoryAsNotFound(t *testing.T) {
	response := performRequest(featureRouter(&featureReaderStub{}), "/api/v1/instruments/"+featureInstrumentID+"/features?asOf=2016-01-04")
	if response.Code != http.StatusNotFound {
		t.Fatalf("status %d %s", response.Code, response.Body.String())
	}
	errorBody, _ := decodeBody(t, response)["error"].(map[string]any)
	if message, _ := errorBody["message"].(string); !strings.Contains(strings.ToLower(message), "no history") {
		t.Errorf("message %q does not say there is no history", message)
	}
}

func TestFeatureDefinitionsEndpointListsEveryVersionIncludingSuperseded(t *testing.T) {
	published := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	superseded := published.Add(time.Hour)
	window := 20
	stub := &featureReaderStub{definitions: []features.Definition{
		{ID: "44000000-0000-4000-8000-000000000001", Name: "return_20", Version: 1, WindowSessions: &window, PriceBasis: features.PriceBasisRaw,
			Parameters: map[string]any{}, UndefinedConditions: "fewer than 21 stored sessions", PublishedAt: published, SupersededAt: &superseded},
		{ID: "44000000-0000-4000-8000-000000000002", Name: "return_20", Version: 2, WindowSessions: &window, PriceBasis: features.PriceBasisRaw,
			Parameters: map[string]any{"basis": "log"}, UndefinedConditions: "fewer than 21 stored sessions", PublishedAt: superseded},
		{ID: "44000000-0000-4000-8000-000000000003", Name: "volatility_20", Version: 1, WindowSessions: &window, PriceBasis: features.PriceBasisRaw,
			Parameters: map[string]any{}, UndefinedConditions: "fewer than 21 stored sessions", SessionLengthSensitive: true, PublishedAt: published},
	}}
	router := featureRouter(stub)
	response := performRequest(router, "/api/v1/feature-definitions")
	if response.Code != http.StatusOK {
		t.Fatalf("status %d %s", response.Code, response.Body.String())
	}
	items, _ := decodeBody(t, response)["items"].([]any)
	if len(items) != 3 || !stub.listAll {
		t.Fatalf("items = %v (includeSuperseded received %v); the default lists every version", items, stub.listAll)
	}
	first := items[0].(map[string]any)
	for _, key := range []string{"name", "version", "windowSessions", "priceBasis", "parameters", "undefinedConditions", "sessionLengthSensitive", "publishedAt", "supersededAt"} {
		if _, ok := first[key]; !ok {
			t.Errorf("definition lacks %s: %v", key, first)
		}
	}
	if first["name"] != "return_20" || first["version"] != float64(1) || first["supersededAt"] == nil || first["priceBasis"] != "raw" {
		t.Errorf("first = %v", first)
	}
	if second := items[1].(map[string]any); second["supersededAt"] != nil || second["parameters"].(map[string]any)["basis"] != "log" {
		t.Errorf("second = %v", second)
	}
	if third := items[2].(map[string]any); third["sessionLengthSensitive"] != true {
		t.Errorf("third = %v", third)
	}
	response = performRequest(router, "/api/v1/feature-definitions?includeSuperseded=false")
	if items, _ := decodeBody(t, response)["items"].([]any); response.Code != http.StatusOK || len(items) != 2 {
		t.Errorf("includeSuperseded=false: %d items, status %d", len(items), response.Code)
	}
	if code := performRequest(router, "/api/v1/feature-definitions?includeSuperseded=maybe").Code; code != http.StatusBadRequest {
		t.Errorf("a malformed includeSuperseded returned %d", code)
	}
}

func TestFeatureDefinitionsEndpointFiltersByName(t *testing.T) {
	stub := &featureReaderStub{definitions: []features.Definition{
		{Name: "return_20", Version: 1, PriceBasis: features.PriceBasisRaw, Parameters: map[string]any{}},
		{Name: "sma_20", Version: 1, PriceBasis: features.PriceBasisAdjusted, Parameters: map[string]any{}},
	}}
	router := featureRouter(stub)
	response := performRequest(router, "/api/v1/feature-definitions?name=sma_20")
	items, _ := decodeBody(t, response)["items"].([]any)
	if response.Code != http.StatusOK || len(items) != 1 || items[0].(map[string]any)["name"] != "sma_20" || stub.listName != "sma_20" {
		t.Errorf("name filter: status %d items %v received %q", response.Code, items, stub.listName)
	}
	response = performRequest(router, "/api/v1/feature-definitions?name=sharpe_ratio")
	if items, ok := decodeBody(t, response)["items"].([]any); response.Code != http.StatusOK || !ok || len(items) != 0 {
		t.Errorf("an unknown name: status %d body %s; expected 200 with an empty array", response.Code, response.Body.String())
	}
}

func TestFeatureEndpointsRequireAnActiveSession(t *testing.T) {
	paths := []string{"/api/v1/instruments/" + featureInstrumentID + "/features", "/api/v1/feature-definitions"}
	t.Run("no cookie", func(t *testing.T) {
		router := NewRouter(Dependencies{Features: &featureReaderStub{}})
		for _, path := range paths {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
			if recorder.Code != http.StatusUnauthorized {
				t.Errorf("%s anonymous: %d %s", path, recorder.Code, recorder.Body.String())
			}
		}
	})
	// A revoked session and a deactivated member both surface from the authenticator as
	// "authentication required"; the routes must not answer either.
	for name, err := range map[string]error{"revoked session": auth.ErrAuthenticationRequired, "deactivated member": auth.ErrMemberLocked} {
		t.Run(name, func(t *testing.T) {
			deps := Dependencies{Features: &featureReaderStub{}}
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

func TestFeaturesEndpointAnswersUnknownAndUnauthorizedIdentically(t *testing.T) {
	router := featureRouter(&featureReaderStub{})
	unknown := performRequest(router, "/api/v1/instruments/33000000-0000-4000-8000-000000000999/features")
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown instrument returned %d %s", unknown.Code, unknown.Body.String())
	}
	// A second well-formed identifier that does not exist either. If the two answers ever
	// diverge, a prober could enumerate which identifiers are real.
	other := performRequest(router, "/api/v1/instruments/33000000-0000-4000-8000-000000000998/features")
	if other.Code != http.StatusNotFound || other.Body.String() != unknown.Body.String() ||
		other.Header().Get("Content-Type") != unknown.Header().Get("Content-Type") {
		t.Errorf("a second unknown identifier answered differently: %d %s", other.Code, other.Body.String())
	}
	// The no-history answer for an instrument that exists is the same body as for one that
	// does not: the response must not reveal which identifiers are real.
	noHistory := performRequest(router, "/api/v1/instruments/"+featureInstrumentID+"/features?asOf=2016-01-04")
	if noHistory.Code != http.StatusNotFound || noHistory.Body.String() != unknown.Body.String() {
		t.Errorf("no history answered differently from unknown: %d %s vs %s", noHistory.Code, noHistory.Body.String(), unknown.Body.String())
	}
	if code := performRequest(router, "/api/v1/instruments/not-a-uuid/features").Code; code != http.StatusBadRequest {
		t.Errorf("a malformed identifier returned %d, expected 400 as the history endpoint answers", code)
	}
}

// absentReaderStub answers for an instrument whose history is too short for any window: the
// case a person meets on a newly listed company.
type absentReaderStub struct{ featureReaderStub }

func (s *absentReaderStub) Read(_ context.Context, request features.ReadRequest) (features.FeatureSet, error) {
	window := 21
	reason := features.AbsenceInsufficientHistory
	set := features.FeatureSet{InstrumentID: request.InstrumentID, SessionDate: "2026-06-30", NotComputed: []string{}}
	for _, name := range []string{"return_20", "return_90", "volatility_20", "sma_200", "regime"} {
		set.Features = append(set.Features, features.Value{
			Name: name, DefinitionVersion: 1, WindowSessions: &window, SessionDate: "2026-06-30",
			AbsenceReason: &reason, ComputedAt: time.Date(2026, 7, 1, 3, 0, 0, 0, time.UTC),
		})
	}
	return set, nil
}

// SC-005 at the API surface: an unsatisfied window serialises as an absence with a reason,
// never as a zero. A zero would be read as "the price did not move", which is a claim about
// the market rather than about the history behind it.
func TestAnUncomputableFeatureIsNullInTheBodyNeverZero(t *testing.T) {
	response := performRequest(NewRouter(authenticatedDependencies(Dependencies{
		Features: &absentReaderStub{}, Instruments: &instrumentReaderStub{err: instruments.ErrNotFound},
	})), "/api/v1/instruments/"+featureInstrumentID+"/features")
	if response.Code != http.StatusOK {
		t.Fatalf("status %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), `"value":0`) || strings.Contains(response.Body.String(), `"value":"0`) {
		t.Errorf("the body prints a zero for an uncomputable feature: %s", response.Body.String())
	}
	values, _ := decodeBody(t, response)["features"].([]any)
	if len(values) != 5 {
		t.Fatalf("features = %v", values)
	}
	for _, raw := range values {
		value := raw.(map[string]any)
		if value["value"] != nil || value["label"] != nil {
			t.Errorf("%v carries a value or label although its window is unsatisfied", value)
		}
		if value["absenceReason"] != "insufficient_history" {
			t.Errorf("%v: absenceReason = %v", value["name"], value["absenceReason"])
		}
	}
}

// featureRunReaderStub answers the way the repository would for an operational screen.
type featureRunReaderStub struct {
	featureReaderStub
	requestedLimit int
	runs           []features.Run
	err            error
}

func (s *featureRunReaderStub) ListRuns(_ context.Context, limit int) ([]features.Run, error) {
	s.requestedLimit = limit
	if s.err != nil {
		return nil, s.err
	}
	return s.runs, nil
}

func seededFeatureRuns() []features.Run {
	started := time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC)
	finished := started.Add(4 * time.Minute)
	trigger := features.UUID("ffffffff-0013-4000-8000-00000000a002")
	name := "rsi_14"
	return []features.Run{
		{ID: "eeeeeeee-0014-4000-8000-000000000003", Kind: features.RunKindDefinition,
			Status: features.RunStatusFailed, StartedAt: started, FinishedAt: &finished,
			InstrumentCount: 13, ValueCount: 0, FailedCount: 13, DefinitionName: &name, AppVersion: "0.10.1"},
		{ID: "eeeeeeee-0014-4000-8000-000000000002", Kind: features.RunKindIncremental,
			Status: features.RunStatusPartial, StartedAt: started, FinishedAt: &finished,
			InstrumentCount: 13, ValueCount: 7502, FailedCount: 1, TriggerRunID: &trigger, AppVersion: "0.10.1"},
	}
}

// Feature 014 US1: the operational screen reports the engine's runs, which have had no
// interface at all since the engine shipped.
func TestFeatureRunsEndpointReportsRecentRuns(t *testing.T) {
	stub := &featureRunReaderStub{runs: seededFeatureRuns()}
	router := NewRouter(authenticatedDependencies(Dependencies{
		Features: stub, Instruments: &instrumentReaderStub{err: instruments.ErrNotFound},
	}))
	response := performRequest(router, "/api/v1/feature-runs")
	if response.Code != http.StatusOK {
		t.Fatalf("status %d %s", response.Code, response.Body.String())
	}
	if stub.requestedLimit != 10 {
		t.Errorf("the endpoint asked for %d runs by default, expected 10", stub.requestedLimit)
	}
	body := decodeBody(t, response)
	items, _ := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items = %v", body["items"])
	}
	first, _ := items[0].(map[string]any)
	for _, key := range []string{"id", "kind", "status", "started_at", "finished_at",
		"instrument_count", "value_count", "failed_count"} {
		if _, ok := first[key]; !ok {
			t.Errorf("a run lacks %s: %v", key, first)
		}
	}
	if first["kind"] != "definition" || first["status"] != "failed" || first["definition_name"] != "rsi_14" {
		t.Errorf("the definition run reads %v", first)
	}
	if first["failed_count"] != float64(13) {
		t.Errorf("failed_count = %v, expected 13", first["failed_count"])
	}
	second, _ := items[1].(map[string]any)
	if second["trigger_run_id"] != "ffffffff-0013-4000-8000-00000000a002" {
		t.Errorf("the incremental run lost the import it followed: %v", second)
	}

	t.Run("the limit is honoured and bounded", func(t *testing.T) {
		router := NewRouter(authenticatedDependencies(Dependencies{
			Features: stub, Instruments: &instrumentReaderStub{err: instruments.ErrNotFound},
		}))
		if response := performRequest(router, "/api/v1/feature-runs?limit=3"); response.Code != http.StatusOK ||
			stub.requestedLimit != 3 {
			t.Errorf("limit=3 gave status %d and asked for %d", response.Code, stub.requestedLimit)
		}
		for _, raw := range []string{"0", "51", "-1", "many"} {
			if response := performRequest(router, "/api/v1/feature-runs?limit="+raw); response.Code != http.StatusBadRequest {
				t.Errorf("limit=%s gave status %d, expected 400", raw, response.Code)
			}
		}
	})
}

// The operational screen is shared, not private, but it sits behind the authenticated boundary
// like everything else, and neither a revoked session nor a deactivated member may read it.
func TestFeatureRunsEndpointRequiresAnActiveSession(t *testing.T) {
	const path = "/api/v1/feature-runs"
	t.Run("no cookie", func(t *testing.T) {
		stub := &featureRunReaderStub{runs: seededFeatureRuns()}
		response := performRequest(NewRouter(Dependencies{Features: stub}), path)
		if response.Code != http.StatusUnauthorized {
			t.Errorf("anonymous: %d %s", response.Code, response.Body.String())
		}
		if stub.requestedLimit != 0 {
			t.Errorf("the reader was consulted for an anonymous request")
		}
	})
	for name, err := range map[string]error{
		"revoked session":    auth.ErrAuthenticationRequired,
		"deactivated member": auth.ErrMemberLocked,
	} {
		t.Run(name, func(t *testing.T) {
			stub := &featureRunReaderStub{runs: seededFeatureRuns()}
			deps := Dependencies{Features: stub}
			deps.Authenticator = sessionAuthenticatorFunc(func(context.Context, string) (auth.Principal, error) {
				return auth.Principal{}, err
			})
			response := performRequest(NewRouter(deps), path)
			if response.Code != http.StatusUnauthorized {
				t.Errorf("%s: %d %s", name, response.Code, response.Body.String())
			}
			if stub.requestedLimit != 0 {
				t.Errorf("%s: the reader was consulted anyway", name)
			}
		})
	}
}
