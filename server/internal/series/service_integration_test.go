package series_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"market-lens/server/internal/db"
	"market-lens/server/internal/series"
	"market-lens/server/internal/testdb"
)

// A provider that answers from a script. Benchmarks and rates are imported deliberately rather
// than nightly, so what matters here is that re-asking the same question changes nothing, and that
// a restated close is corrected rather than added beside the old one.
type scriptedProvider struct {
	points  []series.Point
	entered int
	fail    error
}

func (p *scriptedProvider) Name() string { return "scripted" }

func (p *scriptedProvider) DailySeries(_ context.Context, _ string, _, _ series.SessionDate) ([]series.Point, error) {
	p.entered++
	if p.fail != nil {
		return nil, p.fail
	}
	return p.points, nil
}

func newSeriesService(t *testing.T, provider series.Provider) (*series.Service, *series.Repository) {
	t.Helper()
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repository := series.NewRepository(pool)
	return series.NewService(repository, provider, slog.Default()), repository
}

func TestSeriesImportStoresABenchmarkAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	provider := &scriptedProvider{points: []series.Point{
		{SessionDate: "2026-01-02", Close: "1432.25"},
		{SessionDate: "2026-01-05", Close: "1440.10"},
		{SessionDate: "2026-01-07", Close: "1428.00"},
	}}
	service, repository := newSeriesService(t, provider)

	first, err := service.ImportBenchmark(ctx, "OMXS30.INDX")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if first.Stored != 3 || first.Revised != 0 {
		t.Errorf("first import stored %d and revised %d, want 3 and 0", first.Stored, first.Revised)
	}
	if first.Coverage.FirstSession != "2026-01-02" || first.Coverage.LastSession != "2026-01-07" ||
		first.Coverage.Count != 3 {
		t.Errorf("coverage is %+v", first.Coverage)
	}

	// Asking again changes nothing. A benchmark that rewrote itself on every import would make
	// every stored result's comparison quietly unstable.
	second, err := service.ImportBenchmark(ctx, "OMXS30.INDX")
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if second.Stored != 0 || second.Revised != 0 || second.Unchanged != 3 {
		t.Errorf("re-import stored %d, revised %d, left %d unchanged; want 0, 0 and 3",
			second.Stored, second.Revised, second.Unchanged)
	}

	// A restated close is a correction, counted as one, not a second row nobody can choose
	// between.
	provider.points[1].Close = "1441.50"
	third, err := service.ImportBenchmark(ctx, "OMXS30.INDX")
	if err != nil {
		t.Fatalf("third import: %v", err)
	}
	if third.Revised != 1 || third.Stored != 0 {
		t.Errorf("a restated close stored %d and revised %d, want 0 and 1", third.Stored, third.Revised)
	}
	coverage, err := repository.BenchmarkCoverage(ctx, "OMXS30.INDX")
	if err != nil {
		t.Fatalf("coverage: %v", err)
	}
	if coverage.Count != 3 {
		t.Errorf("the correction left %d points, want 3", coverage.Count)
	}
}

func TestSeriesImportRefusesAnUnpublishedBenchmark(t *testing.T) {
	ctx := context.Background()
	service, _ := newSeriesService(t, &scriptedProvider{})
	// A benchmark arrives by migration, named and attached to the market it is the benchmark for.
	// Letting an import invent one would let a result be compared against something nobody chose.
	if _, err := service.ImportBenchmark(ctx, "SPY.US"); !errors.Is(err, series.ErrNotFound) {
		t.Errorf("importing an unpublished series returned %v, want not found", err)
	}
}

func TestRateImportStoresOneDirectionOnly(t *testing.T) {
	ctx := context.Background()
	provider := &scriptedProvider{points: []series.Point{
		{SessionDate: "2026-01-02", Close: "9.4567"},
		{SessionDate: "2026-01-05", Close: "9.4612"},
	}}
	service, repository := newSeriesService(t, provider)

	outcome, err := service.ImportRate(ctx, "EUR", "SEK")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if outcome.Stored != 2 {
		t.Errorf("stored %d rates, want 2", outcome.Stored)
	}
	// The inverse is derived by dividing, never stored. Two stored directions could disagree, and
	// nothing in the product would be able to say which one was right.
	coverage, err := repository.RateCoverage(ctx, "SEK", "EUR")
	if err != nil {
		t.Fatalf("coverage: %v", err)
	}
	if coverage.Count != 0 {
		t.Errorf("the inverse direction was stored as %d rows", coverage.Count)
	}
}

func TestSeriesImportStoresNothingWhenTheProviderFails(t *testing.T) {
	ctx := context.Background()
	provider := &scriptedProvider{fail: errors.New("the provider is unavailable")}
	service, repository := newSeriesService(t, provider)

	if _, err := service.ImportBenchmark(ctx, "OMXS30.INDX"); err == nil {
		t.Fatalf("a failing provider produced a successful import")
	}
	coverage, err := repository.BenchmarkCoverage(ctx, "OMXS30.INDX")
	if err != nil {
		t.Fatalf("coverage: %v", err)
	}
	if coverage.Count != 0 {
		t.Errorf("a failed import left %d points behind", coverage.Count)
	}
}
