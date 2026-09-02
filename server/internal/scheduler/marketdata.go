// Package scheduler owns context-bound in-process background schedules.
package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"market-lens/server/internal/instruments"
	"market-lens/server/internal/marketdata"
)

type MarketDataConfig struct {
	Enabled    bool
	Hour       int
	Minute     int
	Location   *time.Location
	Provider   string
	Universe   string
	AppVersion string
	MaxRetries int
	Workers    int
	// ReobserveSessions is how many recent trading sessions the pass re-asks the source about,
	// so a close restated after the fact is noticed. Zero means one — the behaviour before
	// feature 016 — so a caller that has not been updated keeps working.
	ReobserveSessions int
}

type TargetSource interface {
	TargetsForUniverse(context.Context, string, string) ([]marketdata.ImportTarget, error)
	// ReobservationStarts gives each member the first session this pass should re-ask the
	// source about. It is part of the interface rather than an optional capability the
	// scheduler sniffs for, because a silent fallback is how a feature stops working with
	// nothing failing: the pass would quietly narrow to one session again and the only
	// symptom would be restatements nobody notices.
	ReobservationStarts(ctx context.Context, provider, universe string, sessions int,
		asOf marketdata.SessionDate) (map[instruments.UUID]marketdata.SessionDate, error)
}

type Importer interface {
	Import(context.Context, marketdata.ImportRequest) (marketdata.ImportRun, error)
}

// FeatureComputer recomputes the features the bars of one import run take part in. The
// scheduler treats it as best effort: a computation that fails leaves the import successful
// and its bars stored, and the next pass — scheduled or manual — picks the work up.
type FeatureComputer interface {
	ComputeSinceRun(context.Context, instruments.UUID) error
}

type MarketData struct {
	// Features, when set, recomputes features after each successful import.
	Features    FeatureComputer
	config      MarketDataConfig
	targets     TargetSource
	importer    Importer
	mu          sync.Mutex
	lastSession string
}

// reobserveSessions is the configured window, with zero meaning one: the behaviour before
// feature 016, so a caller that has not been updated keeps working rather than silently
// widening.
func (s *MarketData) reobserveSessions() int {
	if s.config.ReobserveSessions < 1 {
		return 1
	}
	return s.config.ReobserveSessions
}

func NewMarketData(config MarketDataConfig, targets TargetSource, importer Importer) (*MarketData, error) {
	if config.Location == nil || config.Hour < 0 || config.Hour > 23 || config.Minute < 0 || config.Minute > 59 ||
		strings.TrimSpace(config.Provider) == "" || strings.TrimSpace(config.Universe) == "" ||
		strings.TrimSpace(config.AppVersion) == "" || config.MaxRetries < 0 || config.Workers < 1 ||
		config.Workers > 16 || targets == nil || importer == nil {
		return nil, errors.New("market-data scheduler configuration is invalid")
	}
	return &MarketData{config: config, targets: targets, importer: importer}, nil
}

func (s *MarketData) NextRun(after time.Time) time.Time {
	local := after.In(s.config.Location)
	candidate := time.Date(local.Year(), local.Month(), local.Day(), s.config.Hour, s.config.Minute, 0, 0, s.config.Location)
	if !candidate.After(local) {
		candidate = candidate.AddDate(0, 0, 1)
	}
	return candidate.UTC()
}

func (s *MarketData) RunDue(ctx context.Context, now time.Time) error {
	if !s.config.Enabled {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	local := now.In(s.config.Location)
	scheduled := time.Date(local.Year(), local.Month(), local.Day(), s.config.Hour, s.config.Minute, 0, 0, s.config.Location)
	if local.Before(scheduled) {
		return nil
	}
	session := local.Format("2006-01-02")
	s.mu.Lock()
	if s.lastSession == session {
		s.mu.Unlock()
		return nil
	}
	s.lastSession = session
	s.mu.Unlock()

	targets, err := s.targets.TargetsForUniverse(ctx, s.config.Provider, s.config.Universe)
	if err != nil {
		return err
	}
	date, err := marketdata.ParseSessionDate(session)
	if err != nil {
		return err
	}
	// Re-ask the source about a trailing window, not only the session that just closed, so a
	// close restated after the fact is noticed (feature 016). Widening the range costs no extra
	// provider requests — a range is asked for once per instrument whatever its width — and a
	// re-observation that finds nothing changed writes nothing and triggers nothing.
	starts, err := s.targets.ReobservationStarts(ctx, s.config.Provider, s.config.Universe,
		s.reobserveSessions(), date)
	if err != nil {
		return err
	}
	for index := range targets {
		targets[index].To = date
		targets[index].From = date
		if start, known := starts[targets[index].InstrumentID]; known && start < date {
			targets[index].From = start
		}
	}
	run, err := s.importer.Import(ctx, marketdata.ImportRequest{
		Kind: marketdata.ImportDailyUpdate, Provider: s.config.Provider, AppVersion: s.config.AppVersion,
		Targets: targets, MaxRetries: s.config.MaxRetries, Workers: s.config.Workers,
	})
	if err != nil {
		return err
	}
	if s.Features != nil {
		if err := s.Features.ComputeSinceRun(ctx, run.ID); err != nil {
			slog.Default().Error("feature computation after import failed", "import_run_id", run.ID, "error", err)
		}
	}
	return nil
}

func (s *MarketData) Run(ctx context.Context) error {
	if !s.config.Enabled {
		return nil
	}
	for {
		now := time.Now()
		next := s.NextRun(now)
		delay := time.Until(next)
		if delay < 0 {
			delay = 0
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return nil
		case at := <-timer.C:
			if err := s.RunDue(ctx, at); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		}
	}
}
