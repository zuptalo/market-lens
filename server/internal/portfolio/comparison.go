package portfolio

import (
	"context"
	"fmt"
)

// Comparing a holding with its own market, over its own window.
//
// A profit means little on its own: a holding that gained 8% while its market gained 20% lost money
// in every sense that matters. This is what stops a green number reading as a success.
//
// Two things it deliberately is not. It is not a portfolio return — the product does not track what
// was paid in, so there is no denominator (FR-016a). And it is not a money-weighted return of the
// holding either: a position added to over time has no single honest return figure without cash
// flows. It is value against cost over one stated window, and the window is stated so a reader can
// check it rather than take it on trust.

// compare measures the holding and its market between the same two sessions.
//
// The window opens at the oldest purchase still contributing to the remaining cost, not at the
// first purchase ever made. A window starting at a purchase whose shares are already sold would
// compare a period the holding no longer represents.
func (s *Service) compare(ctx context.Context, holding Holding, folded fold, latest *priced) (Comparison, error) {
	if latest == nil || holding.Valuation.Value == nil {
		reason := ComparisonNotValued
		return Comparison{AbsenceReason: &reason}, nil
	}

	series, coverage, err := s.repository.benchmarkFor(ctx, holding.InstrumentID)
	if err != nil {
		return Comparison{}, err
	}
	if series == "" {
		reason := ComparisonNoBenchmark
		return Comparison{AbsenceReason: &reason}, nil
	}

	from, to := folded.costOpenedAt, latest.session
	comparison := Comparison{Series: series, FromSession: &from, ToSession: &to}
	if coverage.first == "" || coverage.first > from || coverage.last < to {
		// Stated, never truncated. Measuring over the window the series happens to cover would look
		// like a comparison and would not be one — the rule feature 021 settled for the Danish
		// index, applied here for the same reason.
		reason := ComparisonSeriesUncovered
		comparison.AbsenceReason = &reason
		comparison.FromSession, comparison.ToSession = nil, nil
		return comparison, nil
	}

	opening, closing, err := s.repository.benchmarkBetween(ctx, series, from, to)
	if err != nil {
		return Comparison{}, err
	}
	if opening.Sign() <= 0 || closing.Sign() <= 0 {
		reason := ComparisonSeriesUncovered
		comparison.AbsenceReason = &reason
		comparison.FromSession, comparison.ToSession = nil, nil
		return comparison, nil
	}
	benchmarkReturn := closing.Div(opening).Sub(decOne).String()
	comparison.BenchmarkReturn = &benchmarkReturn

	// The holding's own return is what it is worth against what it cost, both in the same currency
	// and at the same session — which is exactly what the unrealised figure already compares.
	if holding.Unrealised != nil && folded.cost.Sign() > 0 {
		unrealised, err := parseDec(*holding.Unrealised)
		if err != nil {
			return Comparison{}, err
		}
		value, err := parseDec(*holding.Valuation.Value)
		if err != nil {
			return Comparison{}, err
		}
		cost := value.Sub(unrealised)
		if cost.Sign() > 0 {
			holdingReturn := value.Div(cost).Sub(decOne).String()
			comparison.HoldingReturn = &holdingReturn
		}
	}
	return comparison, nil
}

// seriesCoverage is what a stored benchmark actually spans.
type seriesCoverage struct {
	first SessionDate
	last  SessionDate
}

// benchmarkFor is the series for the market an instrument is listed on. A Swedish holding compared
// against a Norwegian index would be a claim about a relationship that does not exist.
func (r *Repository) benchmarkFor(ctx context.Context, instrumentID UUID) (string, seriesCoverage, error) {
	if err := r.ready(); err != nil {
		return "", seriesCoverage{}, err
	}
	var code string
	if err := r.pool.QueryRow(ctx, `SELECT b.code FROM instruments i
		JOIN exchanges e ON e.id = i.exchange_id
		JOIN benchmark_series b ON b.mic = e.mic
		WHERE i.id = $1`, instrumentID.String()).Scan(&code); err != nil {
		// No benchmark for this market is a stated absence, not a failure.
		return "", seriesCoverage{}, nil
	}
	var first, last *string
	if err := r.pool.QueryRow(ctx, `SELECT min(p.session_date)::text, max(p.session_date)::text
		FROM benchmark_points p JOIN benchmark_series b ON b.id = p.series_id
		WHERE b.code = $1`, code).Scan(&first, &last); err != nil {
		return "", seriesCoverage{}, fmt.Errorf("read the coverage of %s: %w", code, err)
	}
	coverage := seriesCoverage{}
	if first != nil {
		coverage.first = SessionDate(*first)
	}
	if last != nil {
		coverage.last = SessionDate(*last)
	}
	return code, coverage, nil
}

// benchmarkBetween reads the series at both ends of the window. The opening point is the first on
// or after the window opens — a market shut on the day somebody bought is a holiday, not a gap —
// and the closing point is the last on or before it closes.
func (r *Repository) benchmarkBetween(ctx context.Context, code string, from, to SessionDate) (dec, dec, error) {
	if err := r.ready(); err != nil {
		return decZero, decZero, err
	}
	var opening, closing *string
	if err := r.pool.QueryRow(ctx, `SELECT
		(SELECT p.close::text FROM benchmark_points p JOIN benchmark_series b ON b.id = p.series_id
		 WHERE b.code = $1 AND p.session_date >= $2::date ORDER BY p.session_date LIMIT 1),
		(SELECT p.close::text FROM benchmark_points p JOIN benchmark_series b ON b.id = p.series_id
		 WHERE b.code = $1 AND p.session_date <= $3::date ORDER BY p.session_date DESC LIMIT 1)`,
		code, from.String(), to.String()).Scan(&opening, &closing); err != nil {
		return decZero, decZero, fmt.Errorf("read %s between %s and %s: %w", code, from, to, err)
	}
	if opening == nil || closing == nil {
		return decZero, decZero, nil
	}
	first, err := parseDec(*opening)
	if err != nil {
		return decZero, decZero, err
	}
	last, err := parseDec(*closing)
	if err != nil {
		return decZero, decZero, err
	}
	return first, last, nil
}
