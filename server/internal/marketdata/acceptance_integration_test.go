package marketdata_test

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"market-lens/server/internal/api"
	clientevents "market-lens/server/internal/events"
	"market-lens/server/internal/marketdata"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestFixtureImportAcceptanceAtCuratedUniverseScale(t *testing.T) {
	pool := migratedPool(t)
	repository := marketdata.NewRepository(pool)
	if _, err := pool.Exec(context.Background(), `INSERT INTO provider_instruments
		(provider,provider_symbol,instrument_id)
		SELECT 'fixture','FIXTURE-' || i.id::text,i.id
		FROM research_universes u
		JOIN universe_memberships m ON m.universe_id=u.id AND m.included_to IS NULL
		JOIN instruments i ON i.id=m.instrument_id AND i.active
		WHERE u.code='nordic-liquid-v1' AND u.active`); err != nil {
		t.Fatal(err)
	}
	targets, err := repository.TargetsForUniverse(context.Background(), "fixture", "nordic-liquid-v1")
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 100 {
		t.Fatalf("curated import targets = %d, want 100", len(targets))
	}
	from, to := session(t, "2024-04-02"), session(t, "2024-04-03")
	for index := range targets {
		targets[index].From = from
		targets[index].To = to
	}

	provider := &acceptanceProvider{
		actionSymbol:  targets[0].ProviderSymbol,
		warningSymbol: targets[1].ProviderSymbol,
	}
	service := marketdata.NewImportService(repository, provider)
	request := marketdata.ImportRequest{
		Kind: marketdata.ImportBackfill, Provider: provider.Name(), AppVersion: "fixture-acceptance",
		Targets: targets, Workers: 8,
	}

	first, err := service.Import(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	assertAcceptanceRun(t, first, 201, 201, 1)
	assertAcceptanceStorage(t, pool, 200, 0, 1)

	replayed, err := service.Import(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	assertAcceptanceRun(t, replayed, 201, 201, 1)
	assertAcceptanceStorage(t, pool, 200, 0, 1)

	var resumeID int64
	if err := pool.QueryRow(context.Background(), `SELECT max(id) FROM client_events`).Scan(&resumeID); err != nil {
		t.Fatal(err)
	}
	provider.corrected.Store(true)
	corrected, err := service.Import(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	assertAcceptanceRun(t, corrected, 201, 201, 1)
	assertAcceptanceStorage(t, pool, 200, 1, 1)

	var findings, dailyEvents int
	if err := pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM data_quality_findings WHERE rule='zero_volume' AND status='open'),
		(SELECT count(*) FROM client_events WHERE event_type='daily_bar.changed.v1' AND scope='shared')`).
		Scan(&findings, &dailyEvents); err != nil {
		t.Fatal(err)
	}
	if findings < 1 {
		t.Fatal("fixture import did not retain its visible zero-volume quality summary")
	}
	if dailyEvents != 201 {
		t.Fatalf("daily-bar events = %d, want 200 initial changes plus one correction", dailyEvents)
	}

	seedMarketDataTestOwner(t, pool)
	assertResumedSSE(t, clientevents.NewRepository(pool), resumeID)
}

type acceptanceProvider struct {
	corrected     atomic.Bool
	actionSymbol  string
	warningSymbol string
}

func (p *acceptanceProvider) Name() string { return "fixture" }

func (p *acceptanceProvider) Resolve(context.Context, marketdata.ResolveRequest) (marketdata.ResolvedInstrument, error) {
	return marketdata.ResolvedInstrument{}, errors.New("resolve is not used by the fixture acceptance import")
}

func (p *acceptanceProvider) Daily(_ context.Context, request marketdata.DailyRequest) (marketdata.DailyPage, error) {
	if request.Cursor != "" {
		return marketdata.DailyPage{}, nil
	}
	first := barWithoutTest("2024-04-02", request.ProviderSymbol+":2024-04-02")
	second := barWithoutTest("2024-04-03", request.ProviderSymbol+":2024-04-03")
	if request.ProviderSymbol == p.warningSymbol {
		second.Volume = 0
	}
	if request.ProviderSymbol == p.actionSymbol && p.corrected.Load() {
		first.Close, _ = marketdata.ParseDecimal("51.90")
		first.SourceHash += ":corrected"
	}
	page := marketdata.DailyPage{Bars: []marketdata.ProviderBar{first, second}}
	if request.ProviderSymbol == p.actionSymbol {
		ratio, _ := marketdata.ParseDecimal("2")
		page.Actions = []marketdata.ProviderAction{{
			ProviderActionID: "fixture-split-1", Type: marketdata.ActionSplit,
			ExDate: first.SessionDate, Ratio: &ratio, SourceHash: "fixture-split-1:v1",
		}}
	}
	return page, nil
}

func assertAcceptanceRun(t *testing.T, run marketdata.ImportRun, processed, accepted, flagged int64) {
	t.Helper()
	if run.Status != marketdata.ImportSucceeded || run.Counts.Processed != processed ||
		run.Counts.Accepted != accepted || run.Counts.Rejected != 0 || run.Counts.Flagged != flagged {
		t.Fatalf("acceptance run = %#v", run)
	}
}

func assertAcceptanceStorage(t *testing.T, pool *pgxpool.Pool, bars, revisions, actions int) {
	t.Helper()
	var gotBars, gotRevisions, gotActions int
	if err := pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM daily_price_bars),
		(SELECT count(*) FROM price_bar_revisions),
		(SELECT count(*) FROM corporate_actions)`).Scan(&gotBars, &gotRevisions, &gotActions); err != nil {
		t.Fatal(err)
	}
	if gotBars != bars || gotRevisions != revisions || gotActions != actions {
		t.Fatalf("acceptance storage = bars %d, revisions %d, actions %d", gotBars, gotRevisions, gotActions)
	}
}

type cancellingEventReader struct {
	repository *clientevents.Repository
	cancel     context.CancelFunc
}

func (r *cancellingEventReader) Audience(ctx context.Context, userID string) (clientevents.Audience, error) {
	return r.repository.Audience(ctx, userID)
}

func (r *cancellingEventReader) Head(ctx context.Context) (int64, error) {
	return r.repository.Head(ctx)
}

func (r *cancellingEventReader) ListAuthorized(ctx context.Context, audience clientevents.Audience, after int64, limit int) ([]clientevents.Event, error) {
	events, err := r.repository.ListAuthorized(ctx, audience, after, limit)
	if err == nil && len(events) == 0 {
		r.cancel()
	}
	return events, err
}

func assertResumedSSE(t *testing.T, repository *clientevents.Repository, resumeID int64) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	reader := &cancellingEventReader{repository: repository, cancel: cancel}
	router := api.NewRouter(authenticatedAPIDependencies(api.Dependencies{
		Events: reader, EventHeartbeat: time.Hour, EventBatchLimit: 37,
	}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil).WithContext(ctx)
	addMarketDataTestSession(request)
	request.Header.Set("Last-Event-ID", strconv.FormatInt(resumeID, 10))
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("resumed SSE status = %d, body %s", recorder.Code, recorder.Body.String())
	}

	seen := make(map[int64]struct{})
	sharedEnvelopes := 0
	lastID := resumeID
	scanner := bufio.NewScanner(strings.NewReader(recorder.Body.String()))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			if !strings.Contains(line, `"scope":"shared"`) {
				t.Fatalf("non-shared envelope entered fixture stream: %s", line)
			}
			sharedEnvelopes++
		}
		if !strings.HasPrefix(line, "id: ") {
			continue
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(line, "id: "), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		if id <= lastID {
			t.Fatalf("resumed SSE ID = %d after %d", id, lastID)
		}
		if _, duplicate := seen[id]; duplicate {
			t.Fatalf("resumed SSE delivered duplicate ID %d", id)
		}
		seen[id] = struct{}{}
		lastID = id
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(seen) == 0 {
		t.Fatal("resumed SSE delivered no correction-run events")
	}
	if sharedEnvelopes != len(seen) {
		t.Fatalf("shared envelopes = %d for %d event IDs", sharedEnvelopes, len(seen))
	}
}
