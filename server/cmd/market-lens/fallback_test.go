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

func TestParseTheFallbackCommands(t *testing.T) {
	now := time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC)
	for _, action := range []string{"status", "enable", "disable", "audit", "reconcile"} {
		command, err := parseMarketDataCommand([]string{"marketdata", "fallback", action}, now)
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		if command.Kind != marketDataFallback || command.Fallback != action || command.Universe != "nordic-liquid-v1" {
			t.Errorf("%s: command = %#v", action, command)
		}
	}
	for _, args := range [][]string{
		{"marketdata", "fallback"},
		{"marketdata", "fallback", "on"},
		{"marketdata", "fallback", "enable", "extra"},
		{"marketdata", "fallback", "audit", "--universe", ""},
	} {
		if _, err := parseMarketDataCommand(args, now); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
}

type fallbackOpsStub struct {
	enabled   bool
	set       []bool
	state     marketdata.FallbackState
	targets   []marketdata.ImportTarget
	observed  int
	targetFor string
}

func (s *fallbackOpsStub) Enabled(context.Context) (bool, error) { return s.enabled, nil }
func (s *fallbackOpsStub) SetEnabled(_ context.Context, enabled bool) error {
	s.set = append(s.set, enabled)
	s.enabled = enabled
	return nil
}
func (s *fallbackOpsStub) State(context.Context) (marketdata.FallbackState, error) {
	return s.state, nil
}
func (s *fallbackOpsStub) Observe(context.Context) (marketdata.FallbackState, bool, error) {
	s.observed++
	return s.state, false, nil
}
func (s *fallbackOpsStub) ReconcileTargets(_ context.Context, primary string) ([]marketdata.ImportTarget, error) {
	s.targetFor = primary
	return s.targets, nil
}

func TestTheFallbackSwitchAndStatus(t *testing.T) {
	ops := &fallbackOpsStub{}
	var output bytes.Buffer
	if err := executeFallbackSwitch(context.Background(), "enable", ops, &output); err != nil {
		t.Fatal(err)
	}
	if len(ops.set) != 1 || !ops.set[0] || !strings.Contains(output.String(), "enabled=true") {
		t.Errorf("enable: set=%v output=%q", ops.set, output.String())
	}
	output.Reset()
	since := marketdata.SessionDate("2026-09-29")
	ops.state = marketdata.FallbackState{Active: true, Since: &since, Instruments: 100}
	if err := executeFallbackSwitch(context.Background(), "status", ops, &output); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"enabled=true", "active=true", "since=2026-09-29", "instruments=100"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("status %q lacks %q", output.String(), want)
		}
	}
	output.Reset()
	if err := executeFallbackSwitch(context.Background(), "disable", ops, &output); err != nil {
		t.Fatal(err)
	}
	if ops.enabled {
		t.Error("disable left the fallback on")
	}
}

// The audit prints one line per instrument with its evidence and a summary, and exits with an
// error if anything is not verified — so it can be run as a check.
func TestTheAuditReportsEveryInstrument(t *testing.T) {
	stored := marketdata.Decimal("45.38")
	entries := []marketdata.FallbackMappingEvidence{
		{Ticker: "TELIA", MIC: "XSTO", Currency: "SEK", Symbol: "TELIA.ST", Session: "2026-09-28", StoredClose: &stored},
		{Ticker: "NEW", MIC: "XSTO", Currency: "SEK"},
	}
	observe := func(_ context.Context, symbol string, session marketdata.SessionDate) (string, *marketdata.Decimal, error) {
		if symbol != "TELIA.ST" || session != "2026-09-28" {
			return "", nil, errors.New("unexpected")
		}
		value := marketdata.Decimal("45.38")
		return "SEK", &value, nil
	}
	var output bytes.Buffer
	err := executeFallbackAudit(context.Background(), entries, observe, &output)
	text := output.String()
	if !strings.Contains(text, "TELIA") || !strings.Contains(text, "verified") ||
		!strings.Contains(text, "NEW") || !strings.Contains(text, "unmapped") ||
		!strings.Contains(text, "verified=1") || !strings.Contains(text, "unmapped=1") {
		t.Errorf("audit output:\n%s", text)
	}
	if err == nil {
		t.Error("an audit with an unmapped instrument reported success")
	}
}

type recordingImporterStub struct {
	requests []marketdata.ImportRequest
}

func (s *recordingImporterStub) Import(_ context.Context, request marketdata.ImportRequest) (marketdata.ImportRun, error) {
	s.requests = append(s.requests, request)
	id, _ := instruments.NewUUID()
	return marketdata.ImportRun{ID: id, Status: marketdata.ImportSucceeded}, nil
}

// Reconciliation asks the primary for exactly the fallback span, then observes the state so the
// banner and the owner hear that it ended.
func TestReconcileAsksThePrimaryThenObserves(t *testing.T) {
	id, _ := instruments.NewUUID()
	ops := &fallbackOpsStub{targets: []marketdata.ImportTarget{{InstrumentID: id, ProviderSymbol: "INVE-B.ST",
		Currency: "SEK", From: "2026-09-29", To: "2026-10-02"}}}
	importer := &recordingImporterStub{}
	var output bytes.Buffer
	differences := func(context.Context, instruments.UUID) ([]marketdata.ReconciliationDifference, error) {
		return []marketdata.ReconciliationDifference{{Ticker: "INVE-B", Session: "2026-09-29",
			FallbackClose: "53.1", PrimaryClose: "54", BeyondTolerance: true}}, nil
	}
	if err := executeFallbackReconcile(context.Background(), ops, importer, differences, &output,
		"eodhd", "test", 0, 2); err != nil {
		t.Fatal(err)
	}
	if ops.targetFor != "eodhd" || len(importer.requests) != 1 || ops.observed != 1 {
		t.Fatalf("targets for %q, %d imports, %d observations", ops.targetFor, len(importer.requests), ops.observed)
	}
	request := importer.requests[0]
	if request.Kind != marketdata.ImportBackfill || request.Provider != "eodhd" ||
		request.Targets[0].From != "2026-09-29" || request.Targets[0].To != "2026-10-02" {
		t.Errorf("request = %#v", request)
	}
	if !strings.Contains(output.String(), "INVE-B") || !strings.Contains(output.String(), "beyond_tolerance") {
		t.Errorf("output = %q", output.String())
	}
}

func TestReconcileWithNothingToDoSaysSo(t *testing.T) {
	ops := &fallbackOpsStub{}
	importer := &recordingImporterStub{}
	var output bytes.Buffer
	if err := executeFallbackReconcile(context.Background(), ops, importer, nil, &output, "eodhd", "test", 0, 2); err != nil {
		t.Fatal(err)
	}
	if len(importer.requests) != 0 || !strings.Contains(output.String(), "nothing to reconcile") {
		t.Errorf("imports=%d output=%q", len(importer.requests), output.String())
	}
}

// A fallback period is named by the session it began on — for an ending as well, whose state no
// longer has one — so its beginning and its end are told about the same period.
func TestAFallbackPeriodIsNamedByItsFirstSession(t *testing.T) {
	began := marketdata.SessionDate("2026-09-29")
	entered := marketdata.FallbackChange{Entered: true, State: marketdata.FallbackState{Active: true, Since: &began}}
	ended := marketdata.FallbackChange{Ended: true, State: marketdata.FallbackState{
		Recorded: marketdata.RecordedFallbackState{Active: true, Since: &began}}}
	if got := fallbackPeriod(entered); got != "2026-09-29" {
		t.Errorf("entered names %q", got)
	}
	if got := fallbackPeriod(ended); got != "2026-09-29" {
		t.Errorf("ended names %q", got)
	}
}
