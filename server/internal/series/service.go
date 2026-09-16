package series

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
)

// Provider is the narrow question this package asks: one symbol's daily closes over a range.
//
// Deliberately narrower than the market-data provider interface. An index has no splits and no
// dividends, and a rate has neither; asking for them would turn two requests that cannot fail into
// four that can, and a benchmark import would start failing for reasons that have nothing to do
// with benchmarks.
type Provider interface {
	Name() string
	DailySeries(ctx context.Context, symbol string, from, to SessionDate) ([]Point, error)
}

// earliestSession bounds every import. The provider's own coverage for these series begins in
// 2011, comfortably before this product's stored history, so asking from here asks for all of it
// without needing to know where it starts.
const earliestSession = SessionDate("2000-01-01")

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

// Service imports the series a backtest compares against and converts with.
//
// It runs when somebody asks, not on a schedule. A backtest reads stored data only, so a series
// that is out of date produces a comparison that states its own coverage rather than a wrong
// number — which is a better failure than a nightly job quietly deciding what to fetch.
type Service struct {
	repository *Repository
	provider   Provider
	logger     *slog.Logger
}

func NewService(repository *Repository, provider Provider, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{repository: repository, provider: provider, logger: logger}
}

func (s *Service) ready() error {
	if s == nil || s.repository == nil || s.provider == nil {
		return errors.New("series service is not configured")
	}
	return nil
}

// ImportBenchmark brings one published index series up to date.
func (s *Service) ImportBenchmark(ctx context.Context, code string) (ImportOutcome, error) {
	if err := s.ready(); err != nil {
		return ImportOutcome{}, err
	}
	series, err := s.repository.Benchmark(ctx, code)
	if err != nil {
		return ImportOutcome{}, err
	}
	points, err := s.fetch(ctx, series.Code)
	if err != nil {
		return ImportOutcome{}, err
	}
	outcome, err := s.repository.StorePoints(ctx, series, points)
	if err != nil {
		return ImportOutcome{}, err
	}
	outcome.At = time.Now().UTC()
	s.logger.Info("benchmark imported", "code", series.Code, "mic", series.MIC,
		"stored", outcome.Stored, "revised", outcome.Revised, "unchanged", outcome.Unchanged,
		"from", outcome.Coverage.FirstSession.String(), "to", outcome.Coverage.LastSession.String())
	return outcome, nil
}

// ImportRate brings one currency pair up to date, in the direction the provider quotes it.
func (s *Service) ImportRate(ctx context.Context, base, quote string) (ImportOutcome, error) {
	if err := s.ready(); err != nil {
		return ImportOutcome{}, err
	}
	base, quote = strings.ToUpper(strings.TrimSpace(base)), strings.ToUpper(strings.TrimSpace(quote))
	if !currencyPattern.MatchString(base) || !currencyPattern.MatchString(quote) {
		return ImportOutcome{}, fmt.Errorf("a currency pair is two three-letter codes, not %q and %q", base, quote)
	}
	if base == quote {
		return ImportOutcome{}, errors.New("a currency against itself is one, not a stored fact")
	}
	// The provider names a pair by concatenation, with the forex suffix it uses for every one.
	points, err := s.fetch(ctx, base+quote+".FOREX")
	if err != nil {
		return ImportOutcome{}, err
	}
	outcome, err := s.repository.StoreRates(ctx, base, quote, points)
	if err != nil {
		return ImportOutcome{}, err
	}
	outcome.At = time.Now().UTC()
	s.logger.Info("rate imported", "pair", base+quote, "stored", outcome.Stored,
		"revised", outcome.Revised, "unchanged", outcome.Unchanged,
		"from", outcome.Coverage.FirstSession.String(), "to", outcome.Coverage.LastSession.String())
	return outcome, nil
}

func (s *Service) fetch(ctx context.Context, symbol string) ([]Point, error) {
	to := SessionDate(time.Now().UTC().Format("2006-01-02"))
	points, err := s.provider.DailySeries(ctx, symbol, earliestSession, to)
	if err != nil {
		// Nothing is stored. A partial series would be indistinguishable from a genuine coverage
		// gap, and a result would then report a comparison unavailable for a reason that was not
		// true.
		return nil, fmt.Errorf("read %s from %s: %w", symbol, s.provider.Name(), err)
	}
	return points, nil
}
