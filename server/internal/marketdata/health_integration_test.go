package marketdata_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"market-lens/server/internal/marketdata"
)

func TestImportHealthRetainsLastValidBarAndCouplesFindingsAndEvents(t *testing.T) {
	pool := migratedPool(t)
	provider := newScriptedProvider()
	provider.set("NORD.ST", "", marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-04-02", "51.75", "51.75", 180000, "valid"),
	}})
	service := marketdata.NewImportService(marketdata.NewRepository(pool), provider)
	request := importRequest(t, target(t, stockholmInstrument, "NORD.ST"))
	request.Targets[0].From = session(t, "2024-04-02")
	request.Targets[0].To = session(t, "2024-04-02")
	validRun, err := service.Import(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}

	invalid := bar(t, "2024-04-02", "51.75", "51.75", 180000, "invalid-correction")
	invalid.Low = decimal(t, "60")
	provider.set("NORD.ST", "", marketdata.DailyPage{Bars: []marketdata.ProviderBar{invalid}})
	partialRun, err := service.Import(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if partialRun.Status != marketdata.ImportPartial || partialRun.Counts.Processed != 1 ||
		partialRun.Counts.Accepted != 0 || partialRun.Counts.Rejected != 1 || partialRun.Counts.Flagged != 1 {
		t.Fatalf("partial run = %#v", partialRun)
	}
	assertCurrentBar(t, pool, stockholmInstrument, "2024-04-02", "51.75000000", validRun.ID.String())

	var findingStatus, rule, detail string
	if err := pool.QueryRow(context.Background(), `SELECT status,rule,detail FROM data_quality_findings
		WHERE run_id=$1 AND instrument_id=$2`, partialRun.ID.String(), stockholmInstrument).
		Scan(&findingStatus, &rule, &detail); err != nil {
		t.Fatal(err)
	}
	if findingStatus != "open" || rule != "invalid_ohlc" || strings.Contains(strings.ToLower(detail), "token") {
		t.Fatalf("finding = %s %s %q", findingStatus, rule, detail)
	}

	for _, eventType := range []string{"daily_bar.changed.v1", "import_run.changed.v1", "import_item.changed.v1", "quality_finding.changed.v1"} {
		var events int
		if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM client_events
			WHERE event_type=$1 AND scope='shared'`, eventType).Scan(&events); err != nil {
			t.Fatal(err)
		}
		if events == 0 {
			t.Fatalf("no durable %s event was committed", eventType)
		}
	}

	repository := marketdata.NewRepository(pool)
	runs, err := repository.ListImportRuns(context.Background(), marketdata.ImportPartial, 10)
	if err != nil || len(runs) != 1 || runs[0].ID != partialRun.ID {
		t.Fatalf("partial run snapshot = %#v, error %v", runs, err)
	}
	runDetail, items, err := repository.GetImportRun(context.Background(), partialRun.ID)
	if err != nil || runDetail.ID != partialRun.ID || len(items) != 1 || items[0].Status != marketdata.ImportPartial {
		t.Fatalf("run detail = %#v/%#v, error %v", runDetail, items, err)
	}
	findings, err := repository.ListQualityFindings(context.Background(), marketdata.FindingFilter{
		InstrumentID: &request.Targets[0].InstrumentID, Status: marketdata.FindingOpen,
		Severity: marketdata.SeverityError, Limit: 10,
	})
	if err != nil || len(findings) != 1 || findings[0].Rule != "invalid_ohlc" {
		t.Fatalf("quality finding snapshot = %#v, error %v", findings, err)
	}
}

func TestImportHealthRetainsSanitizedFailedRunAndAtomicCounts(t *testing.T) {
	pool := migratedPool(t)
	provider := newScriptedProvider()
	provider.fail("BAD.ST", errors.New("unauthorized api_token=must-not-survive"))
	service := marketdata.NewImportService(marketdata.NewRepository(pool), provider)
	request := importRequest(t, target(t, stockholmInstrument, "BAD.ST"))
	request.Targets[0].From = session(t, "2024-04-02")
	request.Targets[0].To = session(t, "2024-04-02")
	run, err := service.Import(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != marketdata.ImportFailed || run.Counts != (marketdata.ImportCounts{}) || run.Error == nil ||
		run.Error.Code != "provider_authentication" || run.Error.Summary != "Market-data provider authentication failed." {
		t.Fatalf("failed run = %#v", run)
	}
	var status, code, summary string
	var processed, accepted, rejected, flagged int64
	if err := pool.QueryRow(context.Background(), `SELECT status,processed_count,accepted_count,rejected_count,
		flagged_count,error_code,error_summary FROM import_items WHERE run_id=$1 AND instrument_id=$2`,
		run.ID.String(), stockholmInstrument).Scan(&status, &processed, &accepted, &rejected, &flagged, &code, &summary); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || processed != 0 || accepted != 0 || rejected != 0 || flagged != 0 ||
		code != run.Error.Code || summary != run.Error.Summary {
		t.Fatalf("failed item = %s %d/%d/%d/%d %s %q", status, processed, accepted, rejected, flagged, code, summary)
	}
}

func TestImportHealthRetryReconstructsOnlyFailedScopesWithParentLineage(t *testing.T) {
	pool := migratedPool(t)
	if _, err := pool.Exec(context.Background(), `INSERT INTO provider_instruments
		(provider,provider_symbol,instrument_id) VALUES
		('fixture','GOOD.ST',$1),('fixture','BAD.ST',$2)`, stockholmInstrument, stockholmSecond); err != nil {
		t.Fatal(err)
	}
	provider := newScriptedProvider()
	provider.set("GOOD.ST", "", marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-04-02", "51.75", "51.75", 100, "good"),
	}})
	provider.fail("BAD.ST", errors.New("provider unavailable"))
	service := marketdata.NewImportService(marketdata.NewRepository(pool), provider)
	request := importRequest(t,
		target(t, stockholmInstrument, "GOOD.ST"),
		target(t, stockholmSecond, "BAD.ST"),
	)
	for index := range request.Targets {
		request.Targets[index].From = session(t, "2024-04-02")
		request.Targets[index].To = session(t, "2024-04-02")
	}
	parent, err := service.Import(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Status != marketdata.ImportPartial {
		t.Fatalf("parent status = %s", parent.Status)
	}

	provider.recover("BAD.ST")
	provider.set("BAD.ST", "", marketdata.DailyPage{Bars: []marketdata.ProviderBar{
		bar(t, "2024-04-02", "52", "52", 100, "recovered"),
	}})
	retry, err := service.Retry(context.Background(), marketdata.RetryRequest{
		ParentRunID: parent.ID, AppVersion: "test-retry", MaxRetries: 1, Workers: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if retry.Kind != marketdata.ImportRetry || retry.ParentRunID == nil || *retry.ParentRunID != parent.ID ||
		retry.Status != marketdata.ImportSucceeded {
		t.Fatalf("retry run = %#v", retry)
	}
	var items int
	var instrumentID string
	if err := pool.QueryRow(context.Background(), `SELECT count(*),min(instrument_id::text)
		FROM import_items WHERE run_id=$1`, retry.ID.String()).Scan(&items, &instrumentID); err != nil {
		t.Fatal(err)
	}
	if items != 1 || instrumentID != stockholmSecond {
		t.Fatalf("retry scope = %d item(s), instrument %s", items, instrumentID)
	}
}
