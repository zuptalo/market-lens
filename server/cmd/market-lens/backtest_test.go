package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"market-lens/server/internal/backtest"
	"market-lens/server/internal/series"
)

type stubSeriesImporter struct {
	benchmark string
	base      string
	quote     string
	outcome   series.ImportOutcome
	err       error
}

func (s *stubSeriesImporter) ImportBenchmark(_ context.Context, code string) (series.ImportOutcome, error) {
	s.benchmark = code
	return s.outcome, s.err
}

func (s *stubSeriesImporter) ImportRate(_ context.Context, base, quote string) (series.ImportOutcome, error) {
	s.base, s.quote = base, quote
	return s.outcome, s.err
}

type stubBacktestRunner struct {
	request backtest.RunRequest
	run     backtest.Run
	err     error
}

func (s *stubBacktestRunner) Run(_ context.Context, request backtest.RunRequest) (backtest.Run, error) {
	s.request = request
	return s.run, s.err
}

func TestSeriesImportTakesExactlyOneSubject(t *testing.T) {
	for _, args := range [][]string{
		{"series", "import"},
		{"series", "import", "--benchmark", "OMXS30.INDX", "--rate", "EURSEK"},
		{"series", "import", "--rate", "EUR"},
		{"series", "import", "--benchmark", "OMXS30.INDX", "extra"},
		{"series", "update", "--benchmark", "OMXS30.INDX"},
	} {
		if _, err := parseSeriesCommand(args); err == nil {
			t.Errorf("%v was accepted", args)
		}
	}

	command, err := parseSeriesCommand([]string{"series", "import", "--benchmark", "OMXS30.INDX"})
	if err != nil || command.Benchmark != "OMXS30.INDX" {
		t.Errorf("benchmark import parsed as %+v (%v)", command, err)
	}
	// A pair is one symbol to the provider and two currencies to this product; the command reads
	// it the way a person writes it and stores it the way the schema holds it.
	command, err = parseSeriesCommand([]string{"series", "import", "--rate", "eursek"})
	if err != nil || command.Base != "EUR" || command.Quote != "SEK" {
		t.Errorf("rate import parsed as %+v (%v)", command, err)
	}
}

// The printed coverage is the point of the command. A series that begins after the range a
// backtest asks about produces a stated absence rather than a comparison, and this is where an
// operator learns that before running anything.
func TestSeriesImportPrintsWhatTheSeriesActuallyCovers(t *testing.T) {
	importer := &stubSeriesImporter{outcome: series.ImportOutcome{
		Code: "OMXC25.INDX", Stored: 2434, Revised: 0, Unchanged: 0,
		Coverage: series.Coverage{FirstSession: "2016-12-19", LastSession: "2026-09-15", Count: 2434},
	}}
	var output bytes.Buffer
	if err := executeSeriesCommand(context.Background(),
		seriesCommand{Benchmark: "OMXC25.INDX"}, importer, &output); err != nil {
		t.Fatal(err)
	}
	printed := output.String()
	for _, want := range []string{"series=OMXC25.INDX", "stored=2434", "sessions=2434",
		"first=2016-12-19", "last=2026-09-15"} {
		if !strings.Contains(printed, want) {
			t.Errorf("%q is missing from %q", want, printed)
		}
	}
}

func TestSeriesImportReportsAnEmptySeriesPlainly(t *testing.T) {
	importer := &stubSeriesImporter{outcome: series.ImportOutcome{Code: "OBX.INDX"}}
	var output bytes.Buffer
	if err := executeSeriesCommand(context.Background(),
		seriesCommand{Benchmark: "OBX.INDX"}, importer, &output); err != nil {
		t.Fatal(err)
	}
	// A series with no sessions must not print blank dates that read like a success.
	if !strings.Contains(output.String(), "sessions=0 first=- last=-") {
		t.Errorf("an empty series printed %q", output.String())
	}
}

func TestBacktestRunNamesAConfigurationAndNothingElse(t *testing.T) {
	for _, args := range [][]string{
		{"backtest", "run"},
		{"backtest", "run", "--configuration", ""},
		{"backtest", "run", "--configuration", "x", "--version", "0"},
		{"backtest", "run", "--configuration", "x", "extra"},
		// Nothing may vary the rules from the command line. A caller free to change the capital,
		// the sizing or the costs is a caller free to search for the flattering combination.
		{"backtest", "run", "--configuration", "x", "--sizing-n", "3"},
		{"backtest", "run", "--configuration", "x", "--brokerage-bps", "0"},
	} {
		if _, err := parseBacktestCommand(args); err == nil {
			t.Errorf("%v was accepted", args)
		}
	}

	command, err := parseBacktestCommand([]string{"backtest", "run",
		"--configuration", "momentum_trend_equal_weight", "--version", "2"})
	if err != nil || command.Configuration != "momentum_trend_equal_weight" || command.Version != 2 {
		t.Errorf("parsed as %+v (%v)", command, err)
	}
}

func TestBacktestRunReportsTheRunLikeEveryOtherCommand(t *testing.T) {
	runner := &stubBacktestRunner{run: backtest.Run{
		ID: "11111111-2222-4333-8444-555555555555", Status: backtest.RunStatusSucceeded,
		From: "2016-08-31", To: "2026-09-15", TradeCount: 312, SkipCount: 11840, RebalanceCount: 121,
	}}
	var output bytes.Buffer
	if err := executeBacktestCommand(context.Background(),
		backtestCommand{Configuration: "momentum_trend_equal_weight"}, runner, &output, "0.17.0"); err != nil {
		t.Fatal(err)
	}
	if runner.request.AppVersion != "0.17.0" {
		t.Errorf("the run was not attributed to the application version: %+v", runner.request)
	}
	for _, want := range []string{"status=succeeded", "trades=312", "skipped=11840", "rebalances=121"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("%q is missing from %q", want, output.String())
		}
	}
}

func TestBacktestRunSurfacesAFailureRatherThanPrintingASuccess(t *testing.T) {
	runner := &stubBacktestRunner{err: errors.New("no stored data in range")}
	var output bytes.Buffer
	if err := executeBacktestCommand(context.Background(),
		backtestCommand{Configuration: "x"}, runner, &output, "test"); err == nil {
		t.Errorf("a failed backtest returned no error")
	}
	if output.Len() != 0 {
		t.Errorf("a failed backtest printed %q", output.String())
	}
}
