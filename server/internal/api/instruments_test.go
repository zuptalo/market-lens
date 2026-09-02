package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"market-lens/server/internal/instruments"
)

func TestInstrumentReadContracts(t *testing.T) {
	id := mustAPIUUID(t, "33000000-0000-4000-8000-000000000001")
	first, _ := instruments.ParseSessionDate("2026-08-27")
	last, _ := instruments.ParseSessionDate("2026-08-28")
	reader := &instrumentReaderStub{
		page: instruments.SearchPage{Items: []instruments.SearchResult{{
			Instrument: instruments.Instrument{ID: id, ISIN: "SE0000000100", Ticker: "ALFA", Name: "Alpha AB", Currency: "SEK", Country: "SE", Type: instruments.InstrumentTypeCommonStock, Active: true, PurchasabilityStatus: instruments.PurchasabilityUnverified},
			Exchange:   instruments.Exchange{MIC: "XSTO", Name: "Nasdaq Stockholm", Timezone: "Europe/Stockholm"},
		}}, NextCursor: "next-page"},
		inspection: instruments.Inspection{
			Identity: instruments.SearchResult{Instrument: instruments.Instrument{ID: id, ISIN: "SE0000000100", Ticker: "ALFA", Name: "Alpha AB", Currency: "SEK", Country: "SE", Type: instruments.InstrumentTypeCommonStock, Active: true, PurchasabilityStatus: instruments.PurchasabilityUnverified}, Exchange: instruments.Exchange{MIC: "XSTO", Name: "Nasdaq Stockholm", Timezone: "Europe/Stockholm"}},
			MarketDataSummary: instruments.MarketDataSummary{
				LatestBar: &instruments.DailyBar{SessionDate: last, Open: "100.125", High: "102.5", Low: "99.75", Close: "101.25", AdjustedClose: stringPointer("101.125"), Volume: 1234, Currency: "SEK", Provider: "fixture", ObservedAt: time.Date(2026, 8, 29, 18, 30, 0, 0, time.UTC)},
				Freshness: time.Date(2026, 8, 29, 18, 30, 0, 0, time.UTC), Coverage: instruments.HistoryCoverage{FirstSession: first, LastSession: last, BarCount: 2}, Quality: instruments.QualitySummary{OpenWarnings: 1},
			},
		},
		prices: instruments.PricePage{Items: []instruments.DailyBar{{SessionDate: last, Open: "100.125", High: "102.5", Low: "99.75", Close: "101.25", Volume: 1234, Currency: "SEK", Provider: "fixture", ObservedAt: time.Date(2026, 8, 29, 18, 30, 0, 0, time.UTC)}}, NextCursor: "older"},
	}
	router := NewRouter(authenticatedDependencies(Dependencies{Instruments: reader}))

	response := performRequest(router, "/api/v1/instruments?q=alfa&mic=XSTO&country=SE&currency=SEK&status=active&cursor=cursor&limit=1")
	if response.Code != http.StatusOK {
		t.Fatalf("listing response = %d %s", response.Code, response.Body.String())
	}
	if reader.listingFilter.Query != "alfa" || reader.listingFilter.MIC != "XSTO" ||
		reader.listingFilter.Country != "SE" || reader.listingFilter.Currency != "SEK" ||
		reader.listingFilter.Status != "active" || reader.listingFilter.Cursor != "cursor" ||
		reader.listingFilter.Limit != 1 {
		t.Fatalf("listing filter = %#v", reader.listingFilter)
	}

	response = performRequest(router, "/api/v1/instruments/"+id.String())
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"latest_bar"`) || !strings.Contains(response.Body.String(), `"open_warnings":1`) {
		t.Fatalf("detail response = %d %s", response.Code, response.Body.String())
	}

	response = performRequest(router, "/api/v1/instruments/"+id.String()+"/prices?from=2026-08-01&to=2026-08-28&cursor=2026-08-28&limit=50")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"close":"101.25"`) || strings.Contains(response.Body.String(), `101.25,`) {
		t.Fatalf("price response lost decimal JSON strings = %d %s", response.Code, response.Body.String())
	}
	if reader.pricesFilter.From.String() != "2026-08-01" || reader.pricesFilter.To.String() != "2026-08-28" || reader.pricesFilter.Cursor != "2026-08-28" || reader.pricesFilter.Limit != 50 {
		t.Fatalf("price filter = %#v", reader.pricesFilter)
	}

	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
}

func TestInstrumentReadContractsValidateAndReturnNotFound(t *testing.T) {
	reader := &instrumentReaderStub{err: instruments.ErrNotFound}
	router := NewRouter(authenticatedDependencies(Dependencies{Instruments: reader}))
	validID := "33000000-0000-4000-8000-000000000001"
	tests := []struct {
		path string
		want int
	}{
		{path: "/api/v1/instruments?q=" + strings.Repeat("a", 121), want: http.StatusBadRequest},
		{path: "/api/v1/instruments?mic=bad", want: http.StatusBadRequest},
		{path: "/api/v1/instruments?country=SWE", want: http.StatusBadRequest},
		{path: "/api/v1/instruments?currency=SE", want: http.StatusBadRequest},
		{path: "/api/v1/instruments?status=perhaps", want: http.StatusBadRequest},
		{path: "/api/v1/instruments?limit=201", want: http.StatusBadRequest},
		{path: "/api/v1/instruments/not-a-uuid", want: http.StatusBadRequest},
		{path: "/api/v1/instruments/" + validID, want: http.StatusNotFound},
		{path: "/api/v1/instruments/" + validID + "/prices?from=bad", want: http.StatusBadRequest},
		{path: "/api/v1/instruments/" + validID + "/prices?from=2026-08-29&to=2026-08-28", want: http.StatusBadRequest},
		{path: "/api/v1/instruments/" + validID + "/prices", want: http.StatusNotFound},
	}
	for _, test := range tests {
		response := performRequest(router, test.path)
		if response.Code != test.want || response.Header().Get("Content-Type") != "application/json" || !strings.Contains(response.Body.String(), `"error":`) {
			t.Fatalf("%s response = %d %s", test.path, response.Code, response.Body.String())
		}
	}
}

type instrumentReaderStub struct {
	listing       instruments.ListingPage
	listingFilter instruments.ListingFilter
	history       instruments.HistoryWindow
	historyFilter instruments.HistoryFilter
	page          instruments.SearchPage
	inspection    instruments.Inspection
	prices        instruments.PricePage
	err           error
	search        instruments.SearchFilter
	pricesFilter  instruments.PriceFilter
}

func (s *instrumentReaderStub) Listing(_ context.Context, filter instruments.ListingFilter) (instruments.ListingPage, error) {
	s.listingFilter = filter
	return s.listing, s.err
}

func (s *instrumentReaderStub) History(_ context.Context, _ instruments.UUID,
	filter instruments.HistoryFilter) (instruments.HistoryWindow, error) {
	s.historyFilter = filter
	return s.history, s.err
}

func (s *instrumentReaderStub) Inspect(context.Context, instruments.UUID) (instruments.Inspection, error) {
	return s.inspection, s.err
}

func (s *instrumentReaderStub) Prices(_ context.Context, _ instruments.UUID, filter instruments.PriceFilter) (instruments.PricePage, error) {
	s.pricesFilter = filter
	return s.prices, s.err
}

func stringPointer(value string) *string { return &value }

var _ = errors.Is

// The listing endpoint is the one feature 005 changes most: it grows price, derived
// statistics and freshness, and its query vocabulary moves to the words the contract uses.
func TestListingEndpointAcceptsTheContractsQueryVocabulary(t *testing.T) {
	id := mustAPIUUID(t, "33000000-0000-4000-8000-000000000001")
	behind := 3
	changePercent := 0.0125
	return20 := instruments.Decimal("0.041200000000")
	latestClose := instruments.Decimal("101.25")
	changeAbsolute := instruments.Decimal("1.25")
	reader := &instrumentReaderStub{
		listing: instruments.ListingPage{Items: []instruments.ListingRow{{
			Instrument: instruments.Instrument{ID: id, ISIN: "SE0000000100", Ticker: "ALFA",
				Name: "Alpha AB", Currency: "SEK", Country: "SE", Sector: "Technology",
				Industry: "Software", Type: instruments.InstrumentTypeCommonStock, Active: true,
				PurchasabilityStatus: instruments.PurchasabilityUnverified},
			Exchange:       instruments.Exchange{MIC: "XSTO", Name: "Nasdaq Stockholm", Timezone: "Europe/Stockholm"},
			LatestSession:  instruments.SessionDate("2026-06-30"),
			LatestClose:    &latestClose,
			ChangeAbsolute: &changeAbsolute,
			ChangePercent:  &changePercent,
			Return20:       &return20,
			StoredSessions: 42,
			Freshness:      instruments.Freshness{State: instruments.FreshnessStale, SessionsBehind: &behind},
		}}, NextCursor: "next-page"},
	}
	router := NewRouter(authenticatedDependencies(Dependencies{Instruments: reader}))

	response := performRequest(router,
		"/api/v1/instruments?q=alfa&mic=XSTO&country=SE&sector=Technology&status=active"+
			"&sort=return_20&order=desc&cursor=cursor&limit=25")
	if response.Code != http.StatusOK {
		t.Fatalf("listing response = %d %s", response.Code, response.Body.String())
	}
	if reader.listingFilter.Query != "alfa" || reader.listingFilter.MIC != "XSTO" ||
		reader.listingFilter.Country != "SE" || reader.listingFilter.Sector != "Technology" ||
		reader.listingFilter.Status != "active" || reader.listingFilter.Sort != instruments.SortReturn20 ||
		!reader.listingFilter.Descending || reader.listingFilter.Cursor != "cursor" ||
		reader.listingFilter.Limit != 25 {
		t.Fatalf("the handler did not pass the contract's parameters through: %#v", reader.listingFilter)
	}

	var body struct {
		Items []struct {
			LatestClose    *string  `json:"latest_close"`
			ChangeAbsolute *string  `json:"change_absolute"`
			ChangePercent  *float64 `json:"change_percent"`
			Return20       *string  `json:"return_20"`
			Return90       *string  `json:"return_90"`
			Volatility     *string  `json:"volatility"`
			StoredSessions int64    `json:"stored_sessions"`
			Status         string   `json:"status"`
			Sector         string   `json:"sector"`
			Freshness      struct {
				State          string `json:"state"`
				SessionsBehind *int   `json:"sessions_behind"`
			} `json:"freshness"`
		} `json:"items"`
		NextCursor *string `json:"next_cursor"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode listing response: %v — %s", err, response.Body.String())
	}
	if len(body.Items) != 1 {
		t.Fatalf("listing returned %d rows", len(body.Items))
	}
	row := body.Items[0]
	if row.LatestClose == nil || *row.LatestClose != "101.25" {
		t.Errorf("latest close was %v; money must stay a decimal string", row.LatestClose)
	}
	// The engine's statistics are decimal strings for the same reason money is: a JSON number
	// would round them on the way out (feature 013, US5-2).
	if row.Return20 == nil || *row.Return20 != "0.041200000000" {
		t.Errorf("return_20 was %v; the engine's decimal must survive the response", row.Return20)
	}
	if row.Return90 != nil || row.Volatility != nil {
		t.Errorf("an absent statistic was serialised as %v / %v", row.Return90, row.Volatility)
	}
	if row.ChangeAbsolute == nil || *row.ChangeAbsolute != "1.25" {
		t.Errorf("change was %v", row.ChangeAbsolute)
	}
	if row.Status != "active" {
		t.Errorf("status was %q, expected the contract's active/inactive enumeration", row.Status)
	}
	if row.Sector != "Technology" || row.StoredSessions != 42 {
		t.Errorf("sector or stored-session count was lost: %#v", row)
	}
	if row.Freshness.State != "stale" || row.Freshness.SessionsBehind == nil || *row.Freshness.SessionsBehind != 3 {
		t.Errorf("freshness was not reported as the contract defines it: %#v", row.Freshness)
	}
	// An absent statistic must serialize as null. Rendering it as 0 would turn "we could not
	// compute this" into "this instrument did not move" (FR-007).
	if row.Return90 != nil || row.Volatility != nil {
		t.Errorf("an uncomputed statistic serialized as a value: return_90=%v volatility=%v",
			row.Return90, row.Volatility)
	}
	if !strings.Contains(response.Body.String(), `"return_90":null`) {
		t.Errorf("an absent statistic was omitted rather than sent as null: %s", response.Body.String())
	}
}

func TestListingEndpointRejectsParametersOutsideTheContract(t *testing.T) {
	reader := &instrumentReaderStub{}
	router := NewRouter(authenticatedDependencies(Dependencies{Instruments: reader}))
	for _, path := range []string{
		"/api/v1/instruments?sort=whatever",
		"/api/v1/instruments?order=sideways",
		"/api/v1/instruments?status=maybe",
		"/api/v1/instruments?limit=201",
		"/api/v1/instruments?mic=bad",
		"/api/v1/instruments?country=SWE",
	} {
		response := performRequest(router, path)
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s was accepted with %d %s", path, response.Code, response.Body.String())
		}
	}
}

func TestListingEndpointRequiresAnActiveSession(t *testing.T) {
	router := NewRouter(Dependencies{Instruments: &instrumentReaderStub{}})
	response := performRequest(router, "/api/v1/instruments?limit=10")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("an anonymous listing request returned %d %s", response.Code, response.Body.String())
	}
}

func TestHistoryEndpointReturnsTheChartsPayload(t *testing.T) {
	id := mustAPIUUID(t, "33000000-0000-4000-8000-000000000001")
	observed := time.Date(2026, 6, 30, 17, 30, 0, 0, time.UTC)
	provider := "fixture"
	reader := &instrumentReaderStub{
		history: instruments.HistoryWindow{
			Instrument: instruments.ListingRow{
				Instrument: instruments.Instrument{ID: id, Ticker: "GAPPY", Name: "Interrupted History AB",
					Currency: "SEK", Country: "SE", Active: true},
				Exchange: instruments.Exchange{MIC: "XSTO", Name: "Nasdaq Stockholm"},
			},
			Coverage:      instruments.HistoryCoverage{FirstSession: "2026-01-05", LastSession: "2026-06-30", BarCount: 120},
			RequestedFrom: "2026-05-18",
			RequestedTo:   "2026-06-30",
			Bars: []instruments.DailyBar{{SessionDate: "2026-05-18", Open: "100.00", High: "101.50",
				Low: "98.50", Close: "100.50", Volume: 1000}},
			MissingSessions: []instruments.SessionDate{"2026-06-03", "2026-06-04"},
			SeriesBasis:     instruments.SeriesProviderAdjusted,
			Provider:        &provider,
			ObservedAt:      &observed,
			Actions: []instruments.ChartAction{{ID: id, Type: "split", ExDate: "2026-05-28",
				Ratio: func() *instruments.Decimal { d := instruments.Decimal("2"); return &d }()}},
			Findings: []instruments.ChartFinding{{ID: id, Rule: "suspicious_jump", Status: "open",
				Severity: "warning"}},
		},
	}
	router := NewRouter(authenticatedDependencies(Dependencies{Instruments: reader}))

	response := performRequest(router, "/api/v1/instruments/"+id.String()+"/history?sessions=60&to=2026-06-30")
	if response.Code != http.StatusOK {
		t.Fatalf("history response = %d %s", response.Code, response.Body.String())
	}
	if reader.historyFilter.Sessions != 60 || reader.historyFilter.To.String() != "2026-06-30" {
		t.Fatalf("history filter = %#v", reader.historyFilter)
	}

	body := response.Body.String()
	for _, want := range []string{
		`"missing_sessions":["2026-06-03","2026-06-04"]`,
		`"series_basis":"provider_adjusted"`,
		`"provider":"fixture"`,
		`"stored_sessions":120`,
		`"action_type":"split"`,
		`"rule":"suspicious_jump"`,
		`"close":"100.50"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("history response is missing %s: %s", want, body)
		}
	}
	// Money must survive as a decimal string; a float would quietly change the number.
	if strings.Contains(body, `"close":100.5`) {
		t.Errorf("a price was serialized as a number: %s", body)
	}
}

func TestHistoryEndpointAnswersUnknownAndUnauthorizedIdentically(t *testing.T) {
	id := "33000000-0000-4000-8000-000000000001"
	reader := &instrumentReaderStub{err: instruments.ErrNotFound}
	router := NewRouter(authenticatedDependencies(Dependencies{Instruments: reader}))

	response := performRequest(router, "/api/v1/instruments/"+id+"/history")
	if response.Code != http.StatusNotFound {
		t.Fatalf("unknown instrument returned %d", response.Code)
	}
	unknownBody := response.Body.String()

	// The identifier below is well-formed and simply does not exist. If the two answers ever
	// diverge, an anonymous prober could enumerate which identifiers are real (SC-010).
	response = performRequest(router, "/api/v1/instruments/33000000-0000-4000-8000-000000000999/history")
	if response.Code != http.StatusNotFound || response.Body.String() != unknownBody {
		t.Fatalf("a second unknown identifier answered differently: %d %s",
			response.Code, response.Body.String())
	}

	for _, path := range []string{
		"/api/v1/instruments/not-a-uuid/history",
		"/api/v1/instruments/" + id + "/history?sessions=1",
		"/api/v1/instruments/" + id + "/history?sessions=5001",
		"/api/v1/instruments/" + id + "/history?to=yesterday",
	} {
		if code := performRequest(router, path).Code; code != http.StatusBadRequest {
			t.Errorf("%s returned %d, expected 400", path, code)
		}
	}
}

func TestHistoryEndpointRequiresAnActiveSession(t *testing.T) {
	router := NewRouter(Dependencies{Instruments: &instrumentReaderStub{}})
	response := performRequest(router, "/api/v1/instruments/33000000-0000-4000-8000-000000000001/history")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("an anonymous history request returned %d %s", response.Code, response.Body.String())
	}
}

// Feature 014 US2: a reader scrolling the universe is told how large it is. The total rides
// with the first page, because that is the request that counts it; a later page reports null,
// which means "you already have it" and never "zero".
func TestListingEndpointReportsTheTotalOnTheFirstPageOnly(t *testing.T) {
	id := instruments.UUID("33000000-0000-4000-8000-000000000001")
	total := int64(342)
	reader := &instrumentReaderStub{
		listing: instruments.ListingPage{
			Items:      []instruments.ListingRow{{Instrument: instruments.Instrument{ID: id, Ticker: "ALFA", Name: "Alpha AB"}}},
			NextCursor: "next-page",
			Total:      &total,
		},
	}
	router := NewRouter(authenticatedDependencies(Dependencies{Instruments: reader}))

	first := performRequest(router, "/api/v1/instruments?limit=1")
	if first.Code != http.StatusOK {
		t.Fatalf("status %d %s", first.Code, first.Body.String())
	}
	var firstBody struct {
		Total      *int64  `json:"total"`
		NextCursor *string `json:"next_cursor"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &firstBody); err != nil {
		t.Fatalf("decode: %v — %s", err, first.Body.String())
	}
	if firstBody.Total == nil || *firstBody.Total != 342 {
		t.Errorf("total = %v, expected 342", firstBody.Total)
	}

	// A page the repository did not count reports null rather than zero: the difference is
	// "unchanged" against "there is nothing", and a reader is shown one of them.
	reader.listing.Total = nil
	later := performRequest(router, "/api/v1/instruments?limit=1&cursor=next-page")
	var laterBody map[string]any
	if err := json.Unmarshal(later.Body.Bytes(), &laterBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, present := laterBody["total"]; !present {
		t.Errorf("the response omitted total entirely; the contract requires the key")
	}
	if laterBody["total"] != nil {
		t.Errorf("a later page reported total %v, expected null", laterBody["total"])
	}
}
