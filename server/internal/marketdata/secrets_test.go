package marketdata_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"market-lens/server/internal/api"
	"market-lens/server/internal/config"
	"market-lens/server/internal/instruments"
	"market-lens/server/internal/marketdata"
)

const secretRegressionMarker = "ml-secret-t062-never-expose"

func TestSecretsStayOutOfConfigurationErrorsProviderLogsAndAPIPayloads(t *testing.T) {
	t.Run("configuration error", func(t *testing.T) {
		t.Setenv("EODHD_API_TOKEN", secretRegressionMarker)
		t.Setenv("MARKET_DATA_WORKERS", secretRegressionMarker)
		_, err := config.Load()
		if err == nil || strings.Contains(err.Error(), secretRegressionMarker) {
			t.Fatalf("unsafe configuration error: %v", err)
		}
	})

	unsafe := marketdata.SafeError{
		Code:    "provider_authentication",
		Summary: "unauthorized api_token=" + secretRegressionMarker + " from https://provider.invalid/eod",
	}

	t.Run("structured log", func(t *testing.T) {
		var output bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&output, nil))
		normalized := marketdata.SanitizeError(unsafe.Summary)
		logger.Error("market-data request failed", "code", normalized.Code, "summary", normalized.Summary)
		assertSecretAbsent(t, output.String())
	})

	t.Run("API payload", func(t *testing.T) {
		runID := uuid(t, stockholmInstrument)
		reader := &secretRegressionReader{run: marketdata.ImportRun{
			ID: runID, Kind: marketdata.ImportBackfill, Provider: "fixture", Status: marketdata.ImportFailed,
			StartedAt: time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC), Error: &unsafe,
		}}
		router := api.NewRouter(authenticatedAPIDependencies(api.Dependencies{MarketData: reader}))
		request := httptest.NewRequest(http.MethodGet, "/api/v1/market-data/imports?limit=20", nil)
		addMarketDataTestSession(request)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("API status = %d: %s", response.Code, response.Body.String())
		}
		assertSecretAbsent(t, response.Body.String())
		if !strings.Contains(response.Body.String(), "Market-data provider authentication failed.") {
			t.Fatalf("API omitted safe replacement: %s", response.Body.String())
		}
	})
}

func TestProviderFailurePersistsOnlySanitizedStateAndEvents(t *testing.T) {
	pool := migratedPool(t)
	provider := newScriptedProvider()
	provider.fail("SECRET.ST", &marketdata.ProviderError{
		Code: "provider_authentication", Summary: "api_token=" + secretRegressionMarker,
	})
	service := marketdata.NewImportService(marketdata.NewRepository(pool), provider)
	request := importRequest(t, target(t, stockholmInstrument, "SECRET.ST"))
	run, err := service.Import(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if run.Error == nil || run.Error.Summary != "Market-data provider authentication failed." {
		t.Fatalf("run error = %#v", run.Error)
	}

	var persisted string
	if err := pool.QueryRow(context.Background(), `SELECT jsonb_build_object(
		'run',to_jsonb(r),'items',coalesce(jsonb_agg(to_jsonb(i)),'[]'::jsonb),
		'events',(SELECT coalesce(jsonb_agg(to_jsonb(e)),'[]'::jsonb) FROM client_events e))::text
		FROM import_runs r JOIN import_items i ON i.run_id=r.id WHERE r.id=$1 GROUP BY r.id`, run.ID.String()).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	assertSecretAbsent(t, persisted)
	if !json.Valid([]byte(persisted)) {
		t.Fatalf("persisted audit payload is not JSON: %s", persisted)
	}
}

func assertSecretAbsent(t *testing.T, value string) {
	t.Helper()
	lower := strings.ToLower(value)
	for _, forbidden := range []string{secretRegressionMarker, "api_token=", "https://provider.invalid"} {
		if strings.Contains(lower, strings.ToLower(forbidden)) {
			t.Fatalf("secret-bearing value escaped: %q", value)
		}
	}
}

type secretRegressionReader struct{ run marketdata.ImportRun }

func (r *secretRegressionReader) ListImportRuns(context.Context, marketdata.ImportStatus, int) ([]marketdata.ImportRun, error) {
	return []marketdata.ImportRun{r.run}, nil
}

func (r *secretRegressionReader) GetImportRun(context.Context, instruments.UUID) (marketdata.ImportRun, []marketdata.ImportItem, error) {
	return r.run, nil, nil
}

func (r *secretRegressionReader) ListQualityFindings(context.Context, api.FindingFilter) ([]marketdata.QualityFinding, error) {
	return nil, nil
}
