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

	"market-lens/server/internal/marketdata"
)

type fallbackReaderStub struct {
	state marketdata.FallbackState
	err   error
}

func (s fallbackReaderStub) State(context.Context) (marketdata.FallbackState, error) {
	return s.state, s.err
}

// Feature 030. The banner's snapshot: what was last recorded and published, for anybody signed in.
func TestTheFallbackSnapshot(t *testing.T) {
	since := marketdata.SessionDate("2026-09-29")
	changed := time.Date(2026, 9, 29, 18, 5, 0, 0, time.UTC)
	router := NewRouter(authenticatedDependencies(Dependencies{Fallback: fallbackReaderStub{state: marketdata.FallbackState{
		Active: true, Since: &since, Instruments: 100, PendingReconciliation: false,
		Recorded: marketdata.RecordedFallbackState{Active: true, Since: &since, Instruments: 100, ChangedAt: changed},
	}}}))

	response := performRequest(router, "/api/v1/market-data/fallback")
	if response.Code != http.StatusOK {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["active"] != true || body["since"] != "2026-09-29" || body["instruments"] != float64(100) ||
		body["pending_reconciliation"] != false || body["changed_at"] != "2026-09-29T18:05:00Z" ||
		body["provider"] != marketdata.FallbackProvider {
		t.Errorf("body = %s", response.Body.String())
	}
}

func TestAQuietFallbackSnapshotSaysSo(t *testing.T) {
	router := NewRouter(authenticatedDependencies(Dependencies{Fallback: fallbackReaderStub{}}))
	response := performRequest(router, "/api/v1/market-data/fallback")
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || body["active"] != false || body["since"] != nil {
		t.Errorf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestTheFallbackSnapshotNeedsASession(t *testing.T) {
	router := NewRouter(authenticatedDependencies(Dependencies{Fallback: fallbackReaderStub{}}))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/market-data/fallback", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("an anonymous request got %d", recorder.Code)
	}
}

func TestAFallbackSnapshotFailureSaysNothingInternal(t *testing.T) {
	router := NewRouter(authenticatedDependencies(Dependencies{Fallback: fallbackReaderStub{err: errors.New("pq: secret detail")}}))
	response := performRequest(router, "/api/v1/market-data/fallback")
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "secret") {
		t.Errorf("response = %d %s", response.Code, response.Body.String())
	}
}
