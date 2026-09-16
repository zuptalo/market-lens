package series

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound distinguishes "no such published series" from a read failure. A benchmark arrives by
// migration, attached to the market it is the benchmark for; letting an import invent one would
// let a result be compared against a series nobody chose.
var ErrNotFound = errors.New("not found")

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) ready() error {
	if r == nil || r.pool == nil {
		return errors.New("series repository is not configured")
	}
	return nil
}

// Benchmark reads one published series by the provider's own code.
func (r *Repository) Benchmark(ctx context.Context, code string) (Benchmark, error) {
	if err := r.ready(); err != nil {
		return Benchmark{}, err
	}
	var series Benchmark
	if err := r.pool.QueryRow(ctx, `SELECT id::text, code, name, currency, mic
		FROM benchmark_series WHERE code = $1`, code).Scan(
		(*string)(&series.ID), &series.Code, &series.Name, &series.Currency, &series.MIC); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Benchmark{}, fmt.Errorf("%w: benchmark %q", ErrNotFound, code)
		}
		return Benchmark{}, fmt.Errorf("read the benchmark: %w", err)
	}
	return series, nil
}

// Benchmarks lists the published series, one per market.
func (r *Repository) Benchmarks(ctx context.Context) ([]Benchmark, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT id::text, code, name, currency, mic
		FROM benchmark_series ORDER BY code`)
	if err != nil {
		return nil, fmt.Errorf("list the benchmarks: %w", err)
	}
	defer rows.Close()
	var all []Benchmark
	for rows.Next() {
		var series Benchmark
		if err := rows.Scan((*string)(&series.ID), &series.Code, &series.Name,
			&series.Currency, &series.MIC); err != nil {
			return nil, err
		}
		all = append(all, series)
	}
	return all, rows.Err()
}

// StorePoints writes a whole series in one transaction, distinguishing what was new from what was
// restated.
//
// The distinction is not bookkeeping. A restatement means every stored result that read the old
// close was computed from data that has since changed, and a count of them is how an operator
// learns that without reading the rows.
func (r *Repository) StorePoints(ctx context.Context, series Benchmark, points []Point) (ImportOutcome, error) {
	if err := r.ready(); err != nil {
		return ImportOutcome{}, err
	}
	outcome := ImportOutcome{Code: series.Code}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ImportOutcome{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, point := range points {
		var state string
		if err := tx.QueryRow(ctx, `INSERT INTO benchmark_points (series_id, session_date, close)
			VALUES ($1, $2::date, $3::numeric)
			ON CONFLICT (series_id, session_date) DO UPDATE
			  SET close = excluded.close, last_observed_at = now()
			  WHERE benchmark_points.close IS DISTINCT FROM excluded.close
			RETURNING CASE WHEN xmax = 0 THEN 'inserted' ELSE 'revised' END`,
			series.ID.String(), point.SessionDate.String(), point.Close).Scan(&state); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// The conflict clause declined to write because nothing changed. An unchanged
				// re-observation performs no write at all, which is what keeps re-importing a
				// series cheap enough to do whenever coverage is in doubt.
				outcome.Unchanged++
				continue
			}
			return ImportOutcome{}, fmt.Errorf("store %s on %s: %w", series.Code, point.SessionDate, err)
		}
		if state == "inserted" {
			outcome.Stored++
		} else {
			outcome.Revised++
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ImportOutcome{}, fmt.Errorf("commit: %w", err)
	}
	if outcome.Coverage, err = r.BenchmarkCoverage(ctx, series.Code); err != nil {
		return ImportOutcome{}, err
	}
	return outcome, nil
}

// StoreRates writes a currency pair in one direction. The inverse is derived by dividing, never
// stored, so the two directions can never disagree.
func (r *Repository) StoreRates(ctx context.Context, base, quote string, points []Point) (ImportOutcome, error) {
	if err := r.ready(); err != nil {
		return ImportOutcome{}, err
	}
	outcome := ImportOutcome{Code: base + quote}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ImportOutcome{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, point := range points {
		var state string
		if err := tx.QueryRow(ctx, `INSERT INTO fx_rates (base, quote, session_date, rate)
			VALUES ($1, $2, $3::date, $4::numeric)
			ON CONFLICT (base, quote, session_date) DO UPDATE
			  SET rate = excluded.rate, last_observed_at = now()
			  WHERE fx_rates.rate IS DISTINCT FROM excluded.rate
			RETURNING CASE WHEN xmax = 0 THEN 'inserted' ELSE 'revised' END`,
			base, quote, point.SessionDate.String(), point.Close).Scan(&state); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				outcome.Unchanged++
				continue
			}
			return ImportOutcome{}, fmt.Errorf("store %s%s on %s: %w", base, quote, point.SessionDate, err)
		}
		if state == "inserted" {
			outcome.Stored++
		} else {
			outcome.Revised++
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ImportOutcome{}, fmt.Errorf("commit: %w", err)
	}
	if outcome.Coverage, err = r.RateCoverage(ctx, base, quote); err != nil {
		return ImportOutcome{}, err
	}
	return outcome, nil
}

// BenchmarkCoverage is what the stored series actually spans — the fact a result needs in order to
// state a comparison unavailable rather than quietly truncating its own range.
func (r *Repository) BenchmarkCoverage(ctx context.Context, code string) (Coverage, error) {
	if err := r.ready(); err != nil {
		return Coverage{}, err
	}
	var coverage Coverage
	var first, last *string
	if err := r.pool.QueryRow(ctx, `SELECT min(p.session_date)::text, max(p.session_date)::text, count(*)
		FROM benchmark_points p JOIN benchmark_series b ON b.id = p.series_id
		WHERE b.code = $1`, code).Scan(&first, &last, &coverage.Count); err != nil {
		return Coverage{}, fmt.Errorf("read the coverage of %s: %w", code, err)
	}
	if first != nil {
		coverage.FirstSession = SessionDate(*first)
	}
	if last != nil {
		coverage.LastSession = SessionDate(*last)
	}
	return coverage, nil
}

func (r *Repository) RateCoverage(ctx context.Context, base, quote string) (Coverage, error) {
	if err := r.ready(); err != nil {
		return Coverage{}, err
	}
	var coverage Coverage
	var first, last *string
	if err := r.pool.QueryRow(ctx, `SELECT min(session_date)::text, max(session_date)::text, count(*)
		FROM fx_rates WHERE base = $1 AND quote = $2`, base, quote).
		Scan(&first, &last, &coverage.Count); err != nil {
		return Coverage{}, fmt.Errorf("read the coverage of %s%s: %w", base, quote, err)
	}
	if first != nil {
		coverage.FirstSession = SessionDate(*first)
	}
	if last != nil {
		coverage.LastSession = SessionDate(*last)
	}
	return coverage, nil
}
