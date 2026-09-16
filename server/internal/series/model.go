// Package series owns the stored index and currency series a backtest compares against and
// converts with.
//
// It exists separately from marketdata for one structural reason: a benchmark is not an
// instrument. Stored as one it would appear in the Markets listing, be computed over by the
// feature engine, enter a universe through a membership row, and end up scored by the very
// strategy it exists to judge — none of which anybody would have decided. Keeping it in its own
// tables means no existing read needs an exclusion, and the absence of exclusions is the proof.
package series

import (
	"time"

	"market-lens/server/internal/instruments"
)

type (
	UUID        = instruments.UUID
	SessionDate = instruments.SessionDate
)

// Benchmark is one index series, and the market it is the benchmark for.
type Benchmark struct {
	ID       UUID
	Code     string
	Name     string
	Currency string
	MIC      string
}

// Point is one session's close for a benchmark. Decimals stay strings, as everywhere else in
// this product: they become numbers only where arithmetic is actually done.
type Point struct {
	SessionDate SessionDate
	Close       string
}

// Rate is one session's rate for one ordered pair, as the provider quotes it. The inverse
// direction divides rather than being stored, so the two can never disagree.
type Rate struct {
	Base        string
	Quote       string
	SessionDate SessionDate
	Rate        string
}

// Coverage is what a stored series actually spans — the fact a result needs in order to say a
// comparison is unavailable rather than quietly truncating its own range.
type Coverage struct {
	FirstSession SessionDate
	LastSession  SessionDate
	Count        int64
}

// ImportOutcome is what one import did, so a command can say something true about it.
type ImportOutcome struct {
	Code      string
	Stored    int64
	Revised   int64
	Unchanged int64
	Coverage  Coverage
	At        time.Time
}
