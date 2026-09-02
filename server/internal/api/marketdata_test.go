package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"market-lens/server/internal/instruments"
	"market-lens/server/internal/marketdata"
)

func TestMarketDataImportAndFindingReadContracts(t *testing.T) {
	runID := mustAPIUUID(t, "22000000-0000-4000-8000-000000000001")
	instrumentID := mustAPIUUID(t, "22000000-0000-4000-8000-000000000002")
	finished := time.Date(2026, 8, 29, 8, 0, 0, 0, time.UTC)
	reader := &marketDataReaderStub{
		runs: []marketdata.ImportRun{{
			ID: runID, Kind: marketdata.ImportBackfill, Provider: "fixture", Status: marketdata.ImportPartial,
			StartedAt: finished.Add(-time.Minute), FinishedAt: &finished,
			Counts: marketdata.ImportCounts{Processed: 2, Accepted: 1, Rejected: 1, Flagged: 1},
		}},
		items: []marketdata.ImportItem{{RunID: runID, InstrumentID: instrumentID, Status: marketdata.ImportFailed,
			Attempts: 1, Error: &marketdata.SafeError{Code: "provider_timeout", Summary: "Market-data provider request timed out."}}},
		findings: []marketdata.QualityFinding{{
			ID: runID, InstrumentID: instrumentID, RunID: runID, Rule: "invalid_ohlc",
			Severity: marketdata.SeverityError, Disposition: marketdata.DispositionRejected,
			Detail: "Provider returned inconsistent daily OHLC values.", Status: marketdata.FindingOpen, CreatedAt: finished,
		}},
	}
	router := NewRouter(authenticatedDependencies(Dependencies{Database: databaseStub{}, Version: "test", MarketData: reader}))

	t.Run("recent imports", func(t *testing.T) {
		response := performRequest(router, "/api/v1/market-data/imports?status=partial&limit=25")
		if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("response = %d %s", response.Code, response.Body.String())
		}
		var body struct {
			Items []struct {
				ID     string                  `json:"id"`
				Status marketdata.ImportStatus `json:"status"`
				Counts marketdata.ImportCounts `json:"counts"`
			} `json:"items"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if len(body.Items) != 1 || body.Items[0].ID != runID.String() || body.Items[0].Status != marketdata.ImportPartial {
			t.Fatalf("body = %#v", body)
		}
		if reader.runStatus != marketdata.ImportPartial || reader.runLimit != 25 {
			t.Fatalf("filters = %s/%d", reader.runStatus, reader.runLimit)
		}
	})

	t.Run("run detail and safe retry", func(t *testing.T) {
		response := performRequest(router, "/api/v1/market-data/imports/"+runID.String())
		if response.Code != http.StatusOK {
			t.Fatalf("response = %d %s", response.Code, response.Body.String())
		}
		text := response.Body.String()
		if !strings.Contains(text, `"retry_command":"market-lens marketdata retry --run `+runID.String()+`"`) ||
			!strings.Contains(text, "Market-data provider request timed out.") || strings.Contains(text, "token=") {
			t.Fatalf("unsafe or incomplete detail: %s", text)
		}
	})

	t.Run("quality filters", func(t *testing.T) {
		response := performRequest(router, "/api/v1/market-data/quality-findings?instrument_id="+instrumentID.String()+"&status=open&severity=error&limit=10")
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"rule":"invalid_ohlc"`) {
			t.Fatalf("response = %d %s", response.Code, response.Body.String())
		}
		if reader.findingFilter.InstrumentID == nil || *reader.findingFilter.InstrumentID != instrumentID ||
			reader.findingFilter.Status != marketdata.FindingOpen || reader.findingFilter.Severity != marketdata.SeverityError ||
			reader.findingFilter.Limit != 10 {
			t.Fatalf("finding filter = %#v", reader.findingFilter)
		}
	})
}

func TestMarketDataReadContractsReturnConsistentErrors(t *testing.T) {
	router := NewRouter(authenticatedDependencies(Dependencies{MarketData: &marketDataReaderStub{err: ErrNotFound}}))
	tests := []struct {
		path string
		want int
	}{
		{path: "/api/v1/market-data/imports?status=unknown", want: http.StatusBadRequest},
		{path: "/api/v1/market-data/imports?limit=0", want: http.StatusBadRequest},
		{path: "/api/v1/market-data/imports/not-a-uuid", want: http.StatusBadRequest},
		{path: "/api/v1/market-data/imports/22000000-0000-4000-8000-000000000001", want: http.StatusNotFound},
		{path: "/api/v1/market-data/quality-findings?severity=critical", want: http.StatusBadRequest},
		{path: "/api/v1/market-data/quality-findings?status=unknown", want: http.StatusBadRequest},
	}
	for _, tt := range tests {
		response := performRequest(router, tt.path)
		if response.Code != tt.want || response.Header().Get("Content-Type") != "application/json" ||
			!strings.Contains(response.Body.String(), `"error":`) {
			t.Fatalf("%s response = %d %s", tt.path, response.Code, response.Body.String())
		}
	}
}

type marketDataReaderStub struct {
	runs          []marketdata.ImportRun
	items         []marketdata.ImportItem
	findings      []marketdata.QualityFinding
	err           error
	runStatus     marketdata.ImportStatus
	runLimit      int
	findingFilter FindingFilter
}

func (s *marketDataReaderStub) ListImportRuns(_ context.Context, status marketdata.ImportStatus, limit int) ([]marketdata.ImportRun, error) {
	s.runStatus, s.runLimit = status, limit
	return s.runs, s.err
}

func (s *marketDataReaderStub) GetImportRun(context.Context, instruments.UUID) (marketdata.ImportRun, []marketdata.ImportItem, error) {
	if s.err != nil {
		return marketdata.ImportRun{}, nil, s.err
	}
	return s.runs[0], s.items, nil
}

func (s *marketDataReaderStub) ListQualityFindings(_ context.Context, filter FindingFilter) ([]marketdata.QualityFinding, error) {
	s.findingFilter = filter
	return s.findings, s.err
}

func performRequest(handler http.Handler, path string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authenticatedAPIRequest(http.MethodGet, path))
	return recorder
}

func mustAPIUUID(t *testing.T, value string) instruments.UUID {
	t.Helper()
	id, err := instruments.ParseUUID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

var _ = errors.Is

// TestImportRunResponseCarriesTheRevisedCount: a run that corrected history and one that merely
// extended it must be distinguishable by a client, and the field must be present either way —
// omitting it on a quiet run would make "no corrections" and "an older server" look the same.
func TestImportRunResponseCarriesTheRevisedCount(t *testing.T) {
	corrected := marketdata.ImportRun{
		ID: mustAPIUUID(t, "22000000-0000-4000-8000-0000000000a1"), Kind: marketdata.ImportDailyUpdate,
		Provider: "fixture", Status: marketdata.ImportSucceeded, StartedAt: time.Now().UTC(),
		Counts: marketdata.ImportCounts{Processed: 5, Accepted: 5, Revised: 2},
	}
	quiet := corrected
	quiet.ID = mustAPIUUID(t, "22000000-0000-4000-8000-0000000000a2")
	quiet.Counts.Revised = 0

	router := NewRouter(authenticatedDependencies(Dependencies{
		MarketData: &marketDataReaderStub{runs: []marketdata.ImportRun{corrected, quiet}},
	}))
	response := performRequest(router, "/api/v1/market-data/imports?limit=5")
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	items, _ := decodeBody(t, response)["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items are %#v", items)
	}
	first, _ := items[0].(map[string]any)
	counts, _ := first["counts"].(map[string]any)
	if counts["revised"] != float64(2) {
		t.Errorf("a run that corrected two sessions reports revised %#v", counts["revised"])
	}
	if counts["accepted"] != float64(5) {
		t.Errorf("corrections must not replace the accepted count: %#v", counts["accepted"])
	}
	second, _ := items[1].(map[string]any)
	quietCounts, _ := second["counts"].(map[string]any)
	value, present := quietCounts["revised"]
	if !present {
		t.Fatalf("a run that corrected nothing omits the field entirely: %#v", quietCounts)
	}
	if value != float64(0) {
		t.Errorf("a quiet run reports revised %#v", value)
	}
}
