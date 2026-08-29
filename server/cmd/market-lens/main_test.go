package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"market-lens/server/internal/instruments"
	"market-lens/server/internal/marketdata"
)

func TestParseMarketDataCommandsBoundsRequestedScope(t *testing.T) {
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		args []string
		kind marketdata.ImportKind
		from string
		to   string
	}{
		{name: "backfill years", args: []string{"marketdata", "backfill", "--universe", "nordic-liquid-v1", "--years", "10"},
			kind: marketdata.ImportBackfill, from: "2016-08-29", to: "2026-08-29"},
		{name: "bounded update", args: []string{"marketdata", "update", "--universe", "nordic-liquid-v1", "--days", "5"},
			kind: marketdata.ImportDailyUpdate, from: "2026-08-25", to: "2026-08-29"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command, err := parseMarketDataCommand(tt.args, now)
			if err != nil {
				t.Fatal(err)
			}
			if command.Kind != tt.kind || command.Universe != "nordic-liquid-v1" ||
				command.From.String() != tt.from || command.To.String() != tt.to {
				t.Fatalf("command = %#v", command)
			}
		})
	}

	for _, args := range [][]string{
		{"marketdata", "backfill", "--universe", "nordic-liquid-v1", "--years", "0"},
		{"marketdata", "backfill", "--universe", "nordic-liquid-v1", "--years", "31"},
		{"marketdata", "update", "--universe", "nordic-liquid-v1", "--days", "0"},
		{"marketdata", "update", "--universe", "nordic-liquid-v1", "--days", "32"},
		{"marketdata", "backfill", "--universe", "", "--years", "10"},
	} {
		if _, err := parseMarketDataCommand(args, now); err == nil {
			t.Fatalf("invalid command was accepted: %v", args)
		}
	}
}

func TestExecuteMarketDataCommandsUseSharedServiceAndPrintSafeTotals(t *testing.T) {
	instrumentID := mustCommandUUID(t, "22000000-0000-4000-8000-000000000001")
	targets := []marketdata.ImportTarget{{
		InstrumentID: instrumentID, ProviderSymbol: "INVE-B.ST", Currency: "SEK",
	}}
	for _, kind := range []marketdata.ImportKind{marketdata.ImportBackfill, marketdata.ImportDailyUpdate} {
		t.Run(string(kind), func(t *testing.T) {
			var received marketdata.ImportRequest
			importer := importerFunc(func(_ context.Context, request marketdata.ImportRequest) (marketdata.ImportRun, error) {
				received = request
				return marketdata.ImportRun{
					ID: instrumentID, Status: marketdata.ImportSucceeded,
					Counts: marketdata.ImportCounts{Processed: 3, Accepted: 2, Rejected: 1, Flagged: 1},
					Error:  &marketdata.SafeError{Code: "provider_authentication", Summary: "token=secret raw provider failure"},
				}, nil
			})
			from, _ := marketdata.ParseSessionDate("2026-08-25")
			to, _ := marketdata.ParseSessionDate("2026-08-29")
			command := marketDataCommand{Kind: kind, Universe: "nordic-liquid-v1", From: from, To: to}
			var output bytes.Buffer
			if err := executeMarketDataCommand(context.Background(), command, targets, importer, &output,
				"fixture", "test-version", 2, 3); err != nil {
				t.Fatal(err)
			}
			if received.Kind != kind || received.Provider != "fixture" || received.AppVersion != "test-version" ||
				received.MaxRetries != 2 || received.Workers != 3 || len(received.Targets) != 1 ||
				received.Targets[0].From != from || received.Targets[0].To != to {
				t.Fatalf("shared service request = %#v", received)
			}
			text := output.String()
			for _, safe := range []string{"run_id=" + instrumentID.String(), "status=succeeded", "processed=3", "accepted=2", "rejected=1", "flagged=1"} {
				if !strings.Contains(text, safe) {
					t.Fatalf("output %q does not contain %q", text, safe)
				}
			}
			if strings.Contains(text, "secret") || strings.Contains(text, "provider failure") {
				t.Fatalf("unsafe provider detail reached command output: %q", text)
			}
		})
	}
}

func TestExecuteMarketDataCommandRespectsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	importer := importerFunc(func(ctx context.Context, _ marketdata.ImportRequest) (marketdata.ImportRun, error) {
		return marketdata.ImportRun{}, ctx.Err()
	})
	from, _ := marketdata.ParseSessionDate("2026-08-25")
	to, _ := marketdata.ParseSessionDate("2026-08-29")
	command := marketDataCommand{Kind: marketdata.ImportDailyUpdate, Universe: "nordic-liquid-v1", From: from, To: to}
	var output bytes.Buffer
	err := executeMarketDataCommand(ctx, command, []marketdata.ImportTarget{{
		InstrumentID:   mustCommandUUID(t, "22000000-0000-4000-8000-000000000001"),
		ProviderSymbol: "INVE-B.ST", Currency: "SEK",
	}}, importer, &output, "fixture", "test-version", 0, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
	if output.Len() != 0 {
		t.Fatalf("cancelled command printed output: %q", output.String())
	}
}

func TestParseAndExecuteMarketDataRetryUsesOnlySafeParentScope(t *testing.T) {
	parentID := mustCommandUUID(t, "22000000-0000-4000-8000-000000000001")
	command, err := parseMarketDataCommand([]string{"marketdata", "retry", "--run", parentID.String()}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if command.Kind != marketdata.ImportRetry || command.RunID != parentID {
		t.Fatalf("retry command = %#v", command)
	}
	for _, args := range [][]string{
		{"marketdata", "retry"},
		{"marketdata", "retry", "--run", "not-a-uuid"},
		{"marketdata", "retry", "--run", parentID.String(), "unexpected"},
	} {
		if _, err := parseMarketDataCommand(args, time.Now()); err == nil {
			t.Fatalf("invalid retry command was accepted: %v", args)
		}
	}

	var received marketdata.RetryRequest
	retrier := retrierFunc(func(_ context.Context, request marketdata.RetryRequest) (marketdata.ImportRun, error) {
		received = request
		return marketdata.ImportRun{
			ID: parentID, Kind: marketdata.ImportRetry, Status: marketdata.ImportSucceeded,
			Counts: marketdata.ImportCounts{Processed: 2, Accepted: 2},
			Error:  &marketdata.SafeError{Summary: "api_token=must-not-print raw provider failure"},
		}, nil
	})
	var output bytes.Buffer
	if err := executeMarketDataRetry(context.Background(), command, retrier, &output, "test-version", 2, 3); err != nil {
		t.Fatal(err)
	}
	if received.ParentRunID != parentID || received.AppVersion != "test-version" || received.MaxRetries != 2 || received.Workers != 3 {
		t.Fatalf("retry request = %#v", received)
	}
	text := output.String()
	if !strings.Contains(text, "run_id="+parentID.String()) || !strings.Contains(text, "accepted=2") ||
		strings.Contains(text, "token") || strings.Contains(text, "provider failure") {
		t.Fatalf("retry output = %q", text)
	}
}

type importerFunc func(context.Context, marketdata.ImportRequest) (marketdata.ImportRun, error)

func (f importerFunc) Import(ctx context.Context, request marketdata.ImportRequest) (marketdata.ImportRun, error) {
	return f(ctx, request)
}

type retrierFunc func(context.Context, marketdata.RetryRequest) (marketdata.ImportRun, error)

func (f retrierFunc) Retry(ctx context.Context, request marketdata.RetryRequest) (marketdata.ImportRun, error) {
	return f(ctx, request)
}

func mustCommandUUID(t *testing.T, value string) instruments.UUID {
	t.Helper()
	id, err := instruments.ParseUUID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
