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

	response := performRequest(router, "/api/v1/instruments?q=alfa&exchange=XSTO&country=SE&currency=SEK&active=true&cursor=cursor&limit=1")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"mic":"XSTO"`) || !strings.Contains(response.Body.String(), `"next_cursor":"next-page"`) {
		t.Fatalf("search response = %d %s", response.Code, response.Body.String())
	}
	if reader.search.Query != "alfa" || reader.search.MIC != "XSTO" || reader.search.Country != "SE" || reader.search.Currency != "SEK" || reader.search.Active == nil || !*reader.search.Active || reader.search.Cursor != "cursor" || reader.search.Limit != 1 {
		t.Fatalf("search filter = %#v", reader.search)
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
		{path: "/api/v1/instruments?exchange=bad", want: http.StatusBadRequest},
		{path: "/api/v1/instruments?country=SWE", want: http.StatusBadRequest},
		{path: "/api/v1/instruments?currency=SE", want: http.StatusBadRequest},
		{path: "/api/v1/instruments?active=perhaps", want: http.StatusBadRequest},
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
	page         instruments.SearchPage
	inspection   instruments.Inspection
	prices       instruments.PricePage
	err          error
	search       instruments.SearchFilter
	pricesFilter instruments.PriceFilter
}

func (s *instrumentReaderStub) Search(_ context.Context, filter instruments.SearchFilter) (instruments.SearchPage, error) {
	s.search = filter
	return s.page, s.err
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
