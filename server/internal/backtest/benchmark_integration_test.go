package backtest_test

import (
	"testing"
)

// TestTheBenchmarkIsMeasuredOverTheIdenticalRange is why a backtest has a second column at all.
//
// A curve that rose during a rising market has demonstrated nothing, and a comparison measured
// over a different window is worse than no comparison: it looks like evidence.
func TestTheBenchmarkIsMeasuredOverTheIdenticalRange(t *testing.T) {
	f := newBacktestFixture(t)
	run := f.run()

	var from, to string
	if err := f.pool.QueryRow(f.ctx, `SELECT from_session::text, to_session::text
		FROM backtest_measures WHERE run_id = $1 AND subject = 'strategy'`, run.ID.String()).
		Scan(&from, &to); err != nil {
		t.Fatalf("read the strategy's range: %v", err)
	}

	compared := f.count(`SELECT count(*) FROM backtest_measures
		WHERE run_id = $1 AND subject <> 'strategy' AND absence_reason IS NULL`, run.ID.String())
	if compared == 0 {
		t.Fatalf("no benchmark was reported, so this proves nothing")
	}
	// The window is the strategy's, measured on the sessions the series actually has. A series
	// whose market was shut on the first session starts on its next one and says so — reporting
	// the strategy's dates while having measured different ones would be the more precise-looking
	// lie of the two.
	if outside := f.count(`SELECT count(*) FROM backtest_measures
		WHERE run_id = $1 AND subject <> 'strategy' AND absence_reason IS NULL
		  AND (from_session < $2::date OR to_session > $3::date)`,
		run.ID.String(), from, to); outside != 0 {
		t.Errorf("%d benchmarks were measured outside the strategy's own range", outside)
	}
	// And never materially inside it: a comparison quietly measured over a shorter window would
	// be the truncation FR-020 exists to forbid.
	if truncated := f.count(`SELECT count(*) FROM backtest_measures m
		WHERE m.run_id = $1 AND m.subject <> 'strategy' AND m.absence_reason IS NULL
		  AND ((SELECT count(*) FROM backtest_equity e WHERE e.run_id = $1
		         AND e.session_date >= $2::date AND e.session_date < m.from_session) > 5
		    OR (SELECT count(*) FROM backtest_equity e WHERE e.run_id = $1
		         AND e.session_date > m.to_session AND e.session_date <= $3::date) > 5)`,
		run.ID.String(), from, to); truncated != 0 {
		t.Errorf("%d benchmark comparisons were silently truncated", truncated)
	}

	// Every market the universe spans is compared against, and no market it does not.
	markets := f.count(`SELECT count(DISTINCT e.mic) FROM universe_memberships m
		JOIN instruments i ON i.id = m.instrument_id
		JOIN exchanges e ON e.id = i.exchange_id
		WHERE m.universe_id = $1 AND m.included_to IS NULL`, backtestUnivID.String())
	reported := f.count(`SELECT count(*) FROM backtest_measures
		WHERE run_id = $1 AND subject <> 'strategy'`, run.ID.String())
	if reported != markets {
		t.Errorf("%d benchmarks reported for %d markets in the universe", reported, markets)
	}

	// The benchmark's six figures are the strategy's six figures, so the two columns read as one
	// comparison. Its trade count and costs are zero rather than absent: an index bears neither,
	// and omitting them would make the rows different shapes.
	if partial := f.count(`SELECT count(*) FROM backtest_measures
		WHERE run_id = $1 AND subject <> 'strategy' AND absence_reason IS NULL
		  AND (trade_count IS DISTINCT FROM 0 OR total_costs IS DISTINCT FROM 0)`,
		run.ID.String()); partial != 0 {
		t.Errorf("%d benchmarks reported trades or costs an index does not bear", partial)
	}
}

// TestAnUncoveredBenchmarkIsStatedUnavailable is the Danish case.
//
// OMXC25.INDX begins 110 days after this product's stored history does. Reporting it over the
// window it happens to cover would compare two different periods and present the result as one
// comparison; splicing in OMXC20, the index it replaced, would show two different things as one
// series. The fixture reproduces the gap, and the answer is a stated absence with a reason.
func TestAnUncoveredBenchmarkIsStatedUnavailable(t *testing.T) {
	f := newBacktestFixture(t)
	run := f.run()

	var reason *string
	if err := f.pool.QueryRow(f.ctx, `SELECT m.absence_reason FROM backtest_measures m
		JOIN benchmark_series b ON b.id = m.benchmark_series_id
		WHERE m.run_id = $1 AND b.code = 'OMXC25.INDX'`, run.ID.String()).Scan(&reason); err != nil {
		t.Fatalf("read the Danish comparison: %v", err)
	}
	if reason == nil || *reason != "series_starts_after_range" {
		t.Fatalf("the Danish comparison reported %v, want a stated absence", reason)
	}
	// And it reports nothing else. A partly filled row would read as a comparison.
	if filled := f.count(`SELECT count(*) FROM backtest_measures m
		JOIN benchmark_series b ON b.id = m.benchmark_series_id
		WHERE m.run_id = $1 AND b.code = 'OMXC25.INDX'
		  AND (m.total_return IS NOT NULL OR m.from_session IS NOT NULL)`,
		run.ID.String()); filled != 0 {
		t.Errorf("an unavailable comparison reported figures anyway")
	}

	// A market holiday at the edge of the range is not a coverage gap. The Swedish series has no
	// point on the first session of the union calendar — Copenhagen was open and Stockholm was
	// not — and it is still reported, because a closed market for a day is not a missing series.
	if absent := f.count(`SELECT count(*) FROM backtest_measures m
		JOIN benchmark_series b ON b.id = m.benchmark_series_id
		WHERE m.run_id = $1 AND b.code = 'OMXS30.INDX' AND m.absence_reason IS NOT NULL`,
		run.ID.String()); absent != 0 {
		t.Errorf("a one-session holiday was reported as a missing benchmark series")
	}
}
