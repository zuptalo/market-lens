package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"market-lens/server/internal/instruments"
	"market-lens/server/internal/marketdata"
)

func TestMarketDataSchedulerDisabledMode(t *testing.T) {
	location := mustLocation(t, "Europe/Stockholm")
	importer := &recordingImporter{}
	scheduler, err := NewMarketData(MarketDataConfig{
		Enabled: false, Hour: 20, Minute: 0, Location: location,
		Provider: "fixture", Universe: "nordic-liquid-v1", AppVersion: "test", Workers: 1,
	}, staticTargets(t), importer)
	if err != nil {
		t.Fatal(err)
	}
	if err := scheduler.RunDue(context.Background(), time.Date(2026, 7, 1, 20, 0, 0, 0, location)); err != nil {
		t.Fatal(err)
	}
	if importer.calls() != 0 {
		t.Fatalf("disabled scheduler calls = %d", importer.calls())
	}
}

func TestMarketDataSchedulerUsesConfiguredExchangeZoneTime(t *testing.T) {
	location := mustLocation(t, "Europe/Stockholm")
	scheduler, err := NewMarketData(MarketDataConfig{
		Enabled: true, Hour: 20, Minute: 0, Location: location,
		Provider: "fixture", Universe: "nordic-liquid-v1", AppVersion: "test", Workers: 1,
	}, staticTargets(t), &recordingImporter{})
	if err != nil {
		t.Fatal(err)
	}
	before := time.Date(2026, 7, 1, 17, 0, 0, 0, time.UTC)
	if got, want := scheduler.NextRun(before), time.Date(2026, 7, 1, 18, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("next run = %s, want %s", got, want)
	}
	after := time.Date(2026, 7, 1, 18, 30, 0, 0, time.UTC)
	if got, want := scheduler.NextRun(after), time.Date(2026, 7, 2, 18, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("next run after schedule = %s, want %s", got, want)
	}
}

func TestMarketDataSchedulerRunsOncePerLocalSessionThroughSharedService(t *testing.T) {
	location := mustLocation(t, "Europe/Stockholm")
	importer := &recordingImporter{}
	scheduler, err := NewMarketData(MarketDataConfig{
		Enabled: true, Hour: 20, Minute: 0, Location: location,
		Provider: "fixture", Universe: "nordic-liquid-v1", AppVersion: "test",
		MaxRetries: 2, Workers: 3,
	}, staticTargets(t), importer)
	if err != nil {
		t.Fatal(err)
	}
	first := time.Date(2026, 7, 1, 20, 0, 0, 0, location)
	for _, at := range []time.Time{first, first.Add(30 * time.Minute), first.Add(24 * time.Hour)} {
		if err := scheduler.RunDue(context.Background(), at); err != nil {
			t.Fatal(err)
		}
	}
	requests := importer.requests()
	if len(requests) != 2 {
		t.Fatalf("import calls = %d, want 2", len(requests))
	}
	for index, expectedDate := range []string{"2026-07-01", "2026-07-02"} {
		request := requests[index]
		if request.Kind != marketdata.ImportDailyUpdate || request.Provider != "fixture" ||
			request.MaxRetries != 2 || request.Workers != 3 || len(request.Targets) != 1 ||
			request.Targets[0].From.String() != expectedDate || request.Targets[0].To.String() != expectedDate {
			t.Fatalf("request %d = %#v", index, request)
		}
	}
}

func TestMarketDataSchedulerStopsWithContext(t *testing.T) {
	location := mustLocation(t, "Europe/Stockholm")
	scheduler, err := NewMarketData(MarketDataConfig{
		Enabled: true, Hour: 20, Minute: 0, Location: location,
		Provider: "fixture", Universe: "nordic-liquid-v1", AppVersion: "test", Workers: 1,
	}, staticTargets(t), &recordingImporter{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := scheduler.Run(ctx); err != nil {
		t.Fatalf("graceful shutdown returned an error: %v", err)
	}
}

type recordingImporter struct {
	mu       sync.Mutex
	recorded []marketdata.ImportRequest
}

func (i *recordingImporter) Import(ctx context.Context, request marketdata.ImportRequest) (marketdata.ImportRun, error) {
	if err := ctx.Err(); err != nil {
		return marketdata.ImportRun{}, err
	}
	i.mu.Lock()
	i.recorded = append(i.recorded, request)
	i.mu.Unlock()
	id, _ := instruments.NewUUID()
	return marketdata.ImportRun{ID: id, Status: marketdata.ImportSucceeded}, nil
}

func (i *recordingImporter) calls() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return len(i.recorded)
}

func (i *recordingImporter) requests() []marketdata.ImportRequest {
	i.mu.Lock()
	defer i.mu.Unlock()
	return append([]marketdata.ImportRequest(nil), i.recorded...)
}

func staticTargets(t *testing.T) TargetSource {
	t.Helper()
	id, err := instruments.ParseUUID("22000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	return targetSourceFunc(func(context.Context, string, string) ([]marketdata.ImportTarget, error) {
		return []marketdata.ImportTarget{{InstrumentID: id, ProviderSymbol: "INVE-B.ST", Currency: "SEK"}}, nil
	})
}

type targetSourceFunc func(context.Context, string, string) ([]marketdata.ImportTarget, error)

func (f targetSourceFunc) TargetsForUniverse(ctx context.Context, provider, universe string) ([]marketdata.ImportTarget, error) {
	if f == nil {
		return nil, errors.New("target source is nil")
	}
	return f(ctx, provider, universe)
}

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return location
}
