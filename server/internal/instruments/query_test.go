package instruments_test

import (
	"context"
	"testing"
	"time"

	"market-lens/server/internal/db"
	"market-lens/server/internal/instruments"
	"market-lens/server/internal/marketdata"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestInstrumentQuerySearchesFiltersAndKeepsCursorStable(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	stockholmID := exchangeID(t, ctx, pool, "XSTO")
	copenhagenID := exchangeID(t, ctx, pool, "XCSE")
	activeAlpha := mustUUID(t)
	activeBeta := mustUUID(t)
	inactiveAlpha := mustUUID(t)
	insertInstrument(t, ctx, pool, activeAlpha, stockholmID, "SE0000000100", "QALFA", "Alpha Industries", "SEK", "SE")
	insertInstrument(t, ctx, pool, activeBeta, copenhagenID, "DK0000000200", "QBETA", "Beta Holdings", "DKK", "DK")
	insertInstrument(t, ctx, pool, inactiveAlpha, copenhagenID, "DK0000000300", "QOLD", "Former Alpha", "DKK", "DK")
	if _, err := pool.Exec(ctx, `UPDATE instruments SET active=false WHERE id=$1`, inactiveAlpha.String()); err != nil {
		t.Fatal(err)
	}

	repository := instruments.NewRepository(pool)
	active := true
	inactive := false
	tests := []struct {
		name   string
		filter instruments.SearchFilter
		want   instruments.UUID
	}{
		{name: "ticker is case insensitive", filter: instruments.SearchFilter{Query: "alfa", Active: &active, Limit: 20}, want: activeAlpha},
		{name: "name is case insensitive", filter: instruments.SearchFilter{Query: "HOLDINGS", Active: &active, Limit: 20}, want: activeBeta},
		{name: "ISIN is case insensitive", filter: instruments.SearchFilter{Query: "dk0000000200", Active: &active, Limit: 20}, want: activeBeta},
		{name: "MIC", filter: instruments.SearchFilter{MIC: "XSTO", Active: &active, Limit: 200}, want: activeAlpha},
		{name: "country", filter: instruments.SearchFilter{Country: "SE", Active: &active, Limit: 200}, want: activeAlpha},
		{name: "currency", filter: instruments.SearchFilter{Currency: "DKK", Active: &active, Limit: 200}, want: activeBeta},
		{name: "inactive", filter: instruments.SearchFilter{Active: &inactive, Limit: 20}, want: inactiveAlpha},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			page, err := repository.SearchPage(ctx, test.filter)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, item := range page.Items {
				found = found || item.ID == test.want
			}
			if !found {
				t.Fatalf("items did not contain %s: %#v", test.want, page.Items)
			}
			if page.Items[0].Exchange.MIC == "" || page.Items[0].Exchange.Name == "" {
				t.Fatalf("exchange-qualified identity missing: %#v", page.Items[0])
			}
		})
	}

	cursorAlpha := mustUUID(t)
	cursorBeta := mustUUID(t)
	insertInstrument(t, ctx, pool, cursorAlpha, stockholmID, "ZZ0000000100", "CALFA", "Cursor Fixture Alpha", "ZZZ", "ZZ")
	insertInstrument(t, ctx, pool, cursorBeta, stockholmID, "ZZ0000000200", "CBETA", "Cursor Fixture Beta", "ZZZ", "ZZ")
	first, err := repository.SearchPage(ctx, instruments.SearchFilter{Query: "cursor fixture", Active: &active, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatalf("first page = %#v", first)
	}
	insertInstrument(t, ctx, pool, mustUUID(t), stockholmID, "ZZ0000000001", "CAAAA", "Cursor Fixture Inserted Before", "ZZZ", "ZZ")
	second, err := repository.SearchPage(ctx, instruments.SearchFilter{Query: "cursor fixture", Active: &active, Cursor: first.NextCursor, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != cursorBeta {
		t.Fatalf("cursor page changed after earlier insertion: %#v", second)
	}
}

func TestInstrumentInspectionSummarizesHistoryFreshnessWarningsAndEmptyState(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	exchange := exchangeID(t, ctx, pool, "XSTO")
	withHistory := mustUUID(t)
	withoutHistory := mustUUID(t)
	insertInstrument(t, ctx, pool, withHistory, exchange, "SE0000000400", "HIST", "History AB", "SEK", "SE")
	insertInstrument(t, ctx, pool, withoutHistory, exchange, "SE0000000500", "EMPTY", "Empty AB", "SEK", "SE")
	runID := insertSucceededRun(t, ctx, pool)
	observedAt := time.Date(2026, 8, 29, 18, 30, 0, 0, time.UTC)
	insertQueryBar(t, ctx, pool, runID, withHistory, "2026-08-27", "100.12500000", observedAt.Add(-24*time.Hour))
	insertQueryBar(t, ctx, pool, runID, withHistory, "2026-08-28", "101.25000000", observedAt)
	insertQueryFinding(t, ctx, pool, runID, withHistory, "suspicious_jump", "warning", "open")
	insertQueryFinding(t, ctx, pool, runID, withHistory, "invalid_ohlc", "error", "open")
	insertQueryFinding(t, ctx, pool, runID, withHistory, "zero_volume", "warning", "resolved")

	service := instruments.NewQueryService(
		instruments.NewRepository(pool),
		marketdata.NewRepository(pool),
	)
	inspection, err := service.Inspect(ctx, withHistory)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.LatestBar == nil || inspection.LatestBar.SessionDate.String() != "2026-08-28" ||
		inspection.LatestBar.Close.String() != "101.25" || !inspection.Freshness.Equal(observedAt) {
		t.Fatalf("latest/freshness = %#v / %s", inspection.LatestBar, inspection.Freshness)
	}
	if inspection.Coverage.FirstSession.String() != "2026-08-27" ||
		inspection.Coverage.LastSession.String() != "2026-08-28" || inspection.Coverage.BarCount != 2 {
		t.Fatalf("coverage = %#v", inspection.Coverage)
	}
	if inspection.Quality.OpenWarnings != 1 || inspection.Quality.OpenErrors != 1 {
		t.Fatalf("quality = %#v", inspection.Quality)
	}

	empty, err := service.Inspect(ctx, withoutHistory)
	if err != nil {
		t.Fatal(err)
	}
	if empty.LatestBar != nil || !empty.Freshness.IsZero() || empty.Coverage.BarCount != 0 ||
		empty.Coverage.FirstSession != "" || empty.Coverage.LastSession != "" ||
		empty.Quality.OpenWarnings != 0 || empty.Quality.OpenErrors != 0 {
		t.Fatalf("empty history fabricated values: %#v", empty)
	}
}

func insertSucceededRun(t *testing.T, ctx context.Context, pool *pgxpool.Pool) instruments.UUID {
	t.Helper()
	runID := mustUUID(t)
	finishedAt := time.Date(2026, 8, 29, 18, 31, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `INSERT INTO import_runs
		(id,kind,provider,status,started_at,finished_at,processed_count,accepted_count,app_version)
		VALUES ($1,'backfill','fixture','succeeded',$2,$2,2,2,'test')`, runID.String(), finishedAt); err != nil {
		t.Fatal(err)
	}
	return runID
}

func insertQueryBar(t *testing.T, ctx context.Context, pool *pgxpool.Pool, runID, instrumentID instruments.UUID, sessionDate, close string, observedAt time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO daily_price_bars
		(instrument_id,session_date,open,high,low,close,adjusted_close,volume,currency,provider,source_hash,import_run_id,first_observed_at,last_observed_at)
		VALUES ($1,$2,$3,$3,$3,$3,$3,100,'SEK','fixture',$4,$5,$6,$6)`,
		instrumentID.String(), sessionDate, close, "hash-"+sessionDate, runID.String(), observedAt); err != nil {
		t.Fatal(err)
	}
}

// insertQueryFinding takes the rule because an instrument may hold only one open finding per
// condition. Two open findings for the same instrument have to differ in their rule or their
// session, which is the invariant migration 0013 enforces.
func insertQueryFinding(t *testing.T, ctx context.Context, pool *pgxpool.Pool, runID, instrumentID instruments.UUID, rule, severity, status string) {
	t.Helper()
	resolvedAt := any(nil)
	if status == "resolved" {
		resolvedAt = time.Date(2026, 8, 29, 18, 32, 0, 0, time.UTC)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO data_quality_findings
		(id,instrument_id,run_id,rule,severity,disposition,detail,status,created_at,resolved_at)
		VALUES ($1,$2,$3,$4,$5,'flagged','safe fixture finding',$6,$7,$8)`,
		mustUUID(t).String(), instrumentID.String(), runID.String(), rule, severity, status,
		time.Date(2026, 8, 29, 18, 31, 0, 0, time.UTC), resolvedAt); err != nil {
		t.Fatal(err)
	}
}
