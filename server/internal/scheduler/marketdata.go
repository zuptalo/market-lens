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
	// MaxReachSessions caps how far back a pass may reach on account of an open data quality
	// finding, whatever that finding's age. Zero means the production default.
	MaxReachSessions int
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

// PaperFiller settles the paper orders that now have a price. It hangs off the import because that
// is exactly when new prices exist and never otherwise — a second schedule would have to be kept in
// step with the data, and would drift.
//
// Best effort, like the feature computation beside it: a pass that fails leaves the import
// successful and its bars stored, and the next pass picks the work up. Losing a night's prices
// because one order could not be decided would be the wrong trade.
type PaperFiller interface {
	FillPending(context.Context) (int, error)
}

// NotificationDeliverer sends whatever became due. Best effort like the passes beside it: a
// delivery that fails leaves the notification stored and due again, and the next pass picks it up.
type NotificationDeliverer interface {
	DeliverDue(context.Context) (int, error)
}

// ImportFailureReporter tells whoever asked that market data did not arrive. The owner alone is
// offered that kind, because nobody else can act on it.
type ImportFailureReporter interface {
	RaiseImportFailure(ctx context.Context, provider string) error
}

// Surveyor raises the notification kinds that come from shared reference data changing rather than
// from one transaction: what is now waiting to be decided, and which strategy views moved.
type Surveyor interface {
	Survey(context.Context) error
}

type MarketData struct {
	// Features, when set, recomputes features after each successful import.
	Features FeatureComputer
	// PaperFills, when set, settles pending paper orders after each successful import. A record
	// only means something if it accrues whether or not anybody visits.
	PaperFills PaperFiller
	// Notifications, when set, delivers what the night's work made due.
	Notifications NotificationDeliverer
	// Failures, when set, tells the owner that an import did not complete.
	Failures ImportFailureReporter
	// Surveyor, when set, raises the kinds that come from shared data changing.
	Surveyor    Surveyor
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

// maxReachSessions is the configured floor, with zero meaning the production default.
func (s *MarketData) maxReachSessions() int {
	if s.config.MaxReachSessions < 1 {
		return 2600
	}
	return s.config.MaxReachSessions
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
	// The floor a finding may not push the request past. The narrowing in feature 017 stops a
	// finding the product has already examined from widening anything, but a newly raised one on
	// an old session — or a hundred of them after a backfill — would still make tomorrow night
	// unbounded. This makes the pass's cost predictable whatever the findings say; reaching
	// further is an operator's deliberate backfill.
	floors, err := s.targets.ReobservationStarts(ctx, s.config.Provider, s.config.Universe,
		s.maxReachSessions(), date)
	if err != nil {
		return err
	}
	for index := range targets {
		targets[index].To = date
		targets[index].From = date
		if start, known := starts[targets[index].InstrumentID]; known && start < date {
			targets[index].From = start
		}
		// And far enough back to re-examine anything still open. Only the backfill command
		// applied this, so a finding older than the re-observation window could never be
		// re-examined by the pass that runs every night — the exact trap WidenToUnsettled was
		// written for, left open on the one path nobody has to remember to run.
		targets[index].From = marketdata.WidenToUnsettled(targets[index].From, targets[index].EarliestUnsettled)
		if floor, known := floors[targets[index].InstrumentID]; known && targets[index].From < floor {
			targets[index].From = floor
		}
	}
	run, err := s.importer.Import(ctx, marketdata.ImportRequest{
		Kind: marketdata.ImportDailyUpdate, Provider: s.config.Provider, AppVersion: s.config.AppVersion,
		Targets: targets, MaxRetries: s.config.MaxRetries, Workers: s.config.Workers,
	})
	if err != nil {
		// The owner is the only person who can do anything about this, and they will not find out
		// any other way until a screen shows them older data than it looks like it is showing.
		s.tellTheOwner(ctx, err)
		return err
	}
	if s.Features != nil {
		if err := s.Features.ComputeSinceRun(ctx, run.ID); err != nil {
			slog.Default().Error("feature computation after import failed", "import_run_id", run.ID, "error", err)
		}
	}
	if s.PaperFills != nil {
		filled, err := s.PaperFills.FillPending(ctx)
		if err != nil {
			slog.Default().Error("the paper fill pass after import failed",
				"import_run_id", run.ID, "error", err)
		} else if filled > 0 {
			slog.Default().Info("paper orders filled", "import_run_id", run.ID, "filled", filled)
		}
	}
	// Survey what the passes above changed in shared data — decisions now waiting, and strategy
	// views that moved — before delivering, so the night's work goes out in one round.
	if s.Surveyor != nil {
		if err := s.Surveyor.Survey(ctx); err != nil {
			slog.Default().Error("the notification survey after import failed",
				"import_run_id", run.ID, "error", err)
		}
	}
	// Last, so it carries whatever the passes above raised.
	if s.Notifications != nil {
		sent, err := s.Notifications.DeliverDue(ctx)
		if err != nil {
			slog.Default().Error("the notification pass after import failed",
				"import_run_id", run.ID, "error", err)
		} else if sent > 0 {
			slog.Default().Info("notifications delivered", "import_run_id", run.ID, "sent", sent)
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

// tellTheOwner raises a pipeline-failure notification, if the owner asked for one.
//
// Best effort and deliberately quiet: an import that failed is already returning an error, and
// failing to tell somebody about it must not replace that error with a different one.
func (s *MarketData) tellTheOwner(ctx context.Context, cause error) {
	if s.Failures == nil {
		return
	}
	if err := s.Failures.RaiseImportFailure(ctx, s.config.Provider); err != nil {
		slog.Default().Error("could not raise a notification about the failed import", "error", err)
	}
	_ = cause
}
