package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"market-lens/server/internal/auth"
	"market-lens/server/internal/config"
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

// FR-009 at start: the operator is told which values the installation provisioned for itself
// and which they must retain, and no secret appears in the line. SC-007's "three values down
// to one" is only true if the report says so.
func TestLogInstanceConfigurationNamesRetainedValuesWithoutSecrets(t *testing.T) {
	provisionedKey := bytes.Repeat([]byte{0x5e}, 48)
	suppliedSecret := "an-operator-supplied-auth-secret-value-32b"
	credentialKey := bytes.Repeat([]byte{0x44}, 32)

	tests := []struct {
		name              string
		signingKey        auth.SigningKeyResolution
		external          config.ExternalCredentialConfig
		credentialsStored bool
		wantSigningKey    string
		wantRetain        string
		wantWarning       bool
	}{
		{
			name:           "nothing to retain beyond the database",
			signingKey:     auth.SigningKeyResolution{Key: provisionedKey, Source: auth.SigningKeyProvisioned, Generation: 1},
			external:       config.ExternalCredentialConfig{},
			wantSigningKey: "provisioned",
			wantRetain:     "[]",
			wantWarning:    true,
		},
		{
			name:              "credential key must be retained once credentials are stored",
			signingKey:        auth.SigningKeyResolution{Key: provisionedKey, Source: auth.SigningKeyProvisioned, Generation: 2},
			external:          config.ExternalCredentialConfig{Key: credentialKey, KeyVersion: 1, Configured: true},
			credentialsStored: true,
			wantSigningKey:    "provisioned",
			wantRetain:        "EXTERNAL_CREDENTIAL_KEY",
			wantWarning:       false,
		},
		{
			name:           "an existing deployment still retains both",
			signingKey:     auth.SigningKeyResolution{Key: []byte(suppliedSecret), Source: auth.SigningKeySupplied, Generation: 1},
			external:       config.ExternalCredentialConfig{Key: credentialKey, KeyVersion: 1, Configured: true},
			wantSigningKey: "supplied",
			wantRetain:     "AUTH_SECRET",
			wantWarning:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured bytes.Buffer
			logInstanceConfiguration(slog.New(slog.NewTextHandler(&captured, &slog.HandlerOptions{Level: slog.LevelDebug})),
				tt.signingKey, tt.external, tt.credentialsStored)
			output := captured.String()

			if !strings.Contains(output, "signing_key="+tt.wantSigningKey) {
				t.Errorf("configuration report does not name the signing key source: %s", output)
			}
			if !strings.Contains(output, tt.wantRetain) {
				t.Errorf("configuration report does not name %q: %s", tt.wantRetain, output)
			}
			if warned := strings.Contains(output, "level=WARN"); warned != tt.wantWarning {
				t.Errorf("credential key warning = %t, want %t: %s", warned, tt.wantWarning, output)
			}
			// Nothing secret may appear, in any encoding.
			for _, secret := range []string{
				string(tt.signingKey.Key), string(provisionedKey), suppliedSecret, string(credentialKey),
				base64.StdEncoding.EncodeToString(tt.signingKey.Key),
				base64.StdEncoding.EncodeToString(credentialKey),
				hex.EncodeToString(tt.signingKey.Key),
			} {
				if secret == "" {
					continue
				}
				if strings.Contains(output, secret) {
					t.Fatalf("configuration report disclosed a secret: %s", output)
				}
			}
		})
	}
}

// The resolve command reports what the provider has, rather than changing anything.
//
// A stale ticker is invisible today: the instrument imports nothing and the provider says it
// has no data, which reads as a provider problem rather than as an identifier of ours that
// went out of date. Correcting one is a migration, and a migration must not be written from a
// guess, so the first thing needed is a way to ask.
func TestParseMarketDataResolveCommand(t *testing.T) {
	now := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)

	command, err := parseMarketDataCommand([]string{"marketdata", "resolve"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if command.Kind != marketDataResolve || command.Universe != "nordic-liquid-v1" {
		t.Fatalf("command = %#v", command)
	}

	command, err = parseMarketDataCommand(
		[]string{"marketdata", "resolve", "--universe", "other-universe"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if command.Universe != "other-universe" {
		t.Fatalf("universe = %q", command.Universe)
	}

	if _, err := parseMarketDataCommand([]string{"marketdata", "resolve", "--universe", ""}, now); err == nil {
		t.Fatal("an empty universe was accepted")
	}
}

func TestResolveReportsRenamedSymbolsAndChangesNothing(t *testing.T) {
	universe := []marketdata.UniverseEntry{
		{Ticker: "MOCORP", ISIN: "FI0009014575", Name: "Metso Oyj", MIC: "XHEL", ProviderSymbol: "MOCORP.HE"},
		{Ticker: "NOKIA", ISIN: "FI0009000681", Name: "Nokia Oyj", MIC: "XHEL", ProviderSymbol: "NOKIA.HE"},
		{Ticker: "GONE", ISIN: "SE0000000999", Name: "Delisted AB", MIC: "XSTO", ProviderSymbol: "GONE.ST"},
	}
	catalog := map[string][]marketdata.CatalogEntry{
		"XHEL": {
			{ProviderSymbol: "METSO.HE", ISIN: "FI0009014575", Ticker: "METSO", Name: "Metso Oyj"},
			{ProviderSymbol: "NOKIA.HE", ISIN: "FI0009000681", Ticker: "NOKIA", Name: "Nokia Oyj"},
		},
		"XSTO": {{ProviderSymbol: "ERIC-B.ST", ISIN: "SE0000108656", Ticker: "ERIC-B", Name: "Ericsson"}},
	}

	var output strings.Builder
	if err := reportSymbolAudit(&output, universe, catalog); err != nil {
		t.Fatal(err)
	}
	report := output.String()

	for _, want := range []string{
		"MOCORP.HE", "METSO.HE", "renamed",
		"GONE.ST", "absent",
		"checked=3",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("the report does not mention %q:\n%s", want, report)
		}
	}
	// An instrument that is fine must not be listed line by line; a hundred correct rows
	// would bury the two that are not.
	if strings.Contains(report, "NOKIA.HE") {
		t.Errorf("a correct symbol was listed individually:\n%s", report)
	}
}

func TestResolveSaysSoWhenEverythingIsCorrect(t *testing.T) {
	universe := []marketdata.UniverseEntry{
		{Ticker: "NOKIA", ISIN: "FI0009000681", Name: "Nokia Oyj", MIC: "XHEL", ProviderSymbol: "NOKIA.HE"},
	}
	catalog := map[string][]marketdata.CatalogEntry{
		"XHEL": {{ProviderSymbol: "NOKIA.HE", ISIN: "FI0009000681", Ticker: "NOKIA", Name: "Nokia Oyj"}},
	}
	var output strings.Builder
	if err := reportSymbolAudit(&output, universe, catalog); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "checked=1") {
		t.Errorf("report = %s", output.String())
	}
}

// The report has to separate the instrument that needs fixing from the one that only looks
// like it does, and say plainly how much history the working one already holds — otherwise the
// reader has no way to tell the two apart and will "correct" a symbol that works.
func TestResolveDistinguishesUncataloguedFromBrokenAndNamesTheEvidence(t *testing.T) {
	universe := []marketdata.UniverseEntry{
		{Ticker: "KOJAMO", ISIN: "FI4000312251", Name: "Kojamo Oyj", MIC: "XHEL",
			ProviderSymbol: "KOJAMO.HE", StoredBars: 2035},
		{Ticker: "GONE", ISIN: "FI0000000999", Name: "Delisted Oyj", MIC: "XHEL",
			ProviderSymbol: "GONE.HE", StoredBars: 0},
		{Ticker: "MOCORP", ISIN: "FI0009014575", Name: "Metso Oyj", MIC: "XHEL",
			ProviderSymbol: "MOCORP.HE", StoredBars: 500},
	}
	catalog := map[string][]marketdata.CatalogEntry{"XHEL": {
		{ProviderSymbol: "METSO.HE", ISIN: "FI0009014575", Ticker: "METSO", Name: "Metso Oyj"},
	}}

	var output strings.Builder
	if err := reportSymbolAudit(&output, universe, catalog); err != nil {
		t.Fatal(err)
	}
	report := output.String()

	for _, want := range []string{
		"KOJAMO.HE", "uncatalogued", "stored_bars=2035",
		"GONE.HE", "absent",
		"matched_on=isin",
		"uncatalogued=1", "absent=1",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("the report does not mention %q:\n%s", want, report)
		}
	}
}
