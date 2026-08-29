package marketdata

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"market-lens/server/internal/instruments"
)

var ErrInvalidImport = errors.New("market-data import request is invalid")

type RetryRequest struct {
	ParentRunID instruments.UUID
	AppVersion  string
	MaxRetries  int
	Workers     int
}

type ImportTarget struct {
	InstrumentID   instruments.UUID
	ProviderSymbol string
	Currency       string
	From           SessionDate
	To             SessionDate
}

type ImportRequest struct {
	Kind        ImportKind
	Provider    string
	AppVersion  string
	ParentRunID *instruments.UUID
	Targets     []ImportTarget
	MaxRetries  int
	Workers     int
}

type ImportService struct {
	repository *Repository
	provider   Provider
	now        func() time.Time
}

func NewImportService(repository *Repository, provider Provider) *ImportService {
	return &ImportService{repository: repository, provider: provider, now: func() time.Time { return time.Now().UTC() }}
}

func (s *ImportService) Retry(ctx context.Context, request RetryRequest) (ImportRun, error) {
	if s == nil || s.repository == nil || s.provider == nil || !request.ParentRunID.Valid() ||
		strings.TrimSpace(request.AppVersion) == "" || request.MaxRetries < 0 ||
		request.Workers < 1 || request.Workers > 16 {
		return ImportRun{}, ErrInvalidImport
	}
	provider, targets, err := s.repository.retryTargets(ctx, request.ParentRunID)
	if err != nil {
		return ImportRun{}, err
	}
	return s.Import(ctx, ImportRequest{
		Kind: ImportRetry, Provider: provider, AppVersion: request.AppVersion,
		ParentRunID: &request.ParentRunID, Targets: targets,
		MaxRetries: request.MaxRetries, Workers: request.Workers,
	})
}

func (s *ImportService) Import(ctx context.Context, request ImportRequest) (ImportRun, error) {
	if err := s.validate(request); err != nil {
		return ImportRun{}, err
	}
	runID, err := instruments.NewUUID()
	if err != nil {
		return ImportRun{}, err
	}
	from, to := importRange(request.Targets)
	run := ImportRun{
		ID: runID, Kind: request.Kind, Provider: request.Provider, RequestedFrom: &from, RequestedTo: &to,
		Status: ImportRunning, ParentRunID: request.ParentRunID, StartedAt: s.now(), AppVersion: request.AppVersion,
	}
	if err := s.repository.createRun(ctx, run, request.Targets); err != nil {
		return ImportRun{}, err
	}

	jobs := make(chan ImportTarget)
	results := make(chan error, len(request.Targets))
	workers := request.Workers
	if workers > len(request.Targets) {
		workers = len(request.Targets)
	}
	for range workers {
		go func() {
			for target := range jobs {
				results <- s.importTarget(ctx, runID, request, target)
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, target := range request.Targets {
			jobs <- target
		}
	}()

	var terminalErr error
	for range request.Targets {
		err := <-results
		switch {
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			terminalErr = err
		case errors.Is(err, ErrImportConflict):
			if terminalErr == nil {
				terminalErr = ErrImportConflict
			}
		}
	}
	finished, err := s.repository.finishRun(context.WithoutCancel(ctx), runID, s.now())
	if err != nil {
		return ImportRun{}, err
	}
	if terminalErr != nil {
		return finished, terminalErr
	}
	return finished, nil
}

func (s *ImportService) validate(request ImportRequest) error {
	if s == nil || s.repository == nil || s.provider == nil || request.MaxRetries < 0 || request.Workers < 1 || request.Workers > 16 ||
		strings.TrimSpace(request.Provider) == "" || strings.TrimSpace(request.AppVersion) == "" ||
		request.Provider != s.provider.Name() || len(request.Targets) == 0 {
		return ErrInvalidImport
	}
	switch request.Kind {
	case ImportBackfill, ImportDailyUpdate:
		if request.ParentRunID != nil {
			return ErrInvalidImport
		}
	case ImportRetry:
		if request.ParentRunID == nil || !request.ParentRunID.Valid() {
			return ErrInvalidImport
		}
	default:
		return ErrInvalidImport
	}
	seen := make(map[instruments.UUID]struct{}, len(request.Targets))
	for _, target := range request.Targets {
		if !target.InstrumentID.Valid() || strings.TrimSpace(target.ProviderSymbol) == "" ||
			len(target.Currency) != 3 || target.From == "" || target.To == "" || target.To < target.From {
			return ErrInvalidImport
		}
		if _, duplicate := seen[target.InstrumentID]; duplicate {
			return ErrInvalidImport
		}
		seen[target.InstrumentID] = struct{}{}
	}
	return nil
}

func (s *ImportService) importTarget(ctx context.Context, runID instruments.UUID, request ImportRequest, target ImportTarget) error {
	if err := ctx.Err(); err != nil {
		_ = s.repository.cancelItem(context.WithoutCancel(ctx), runID, target.InstrumentID, s.now())
		return err
	}
	startedAt := s.now()
	if err := s.repository.markItemRunning(ctx, runID, target.InstrumentID, startedAt); err != nil {
		return err
	}
	dataset, err := CollectDaily(ctx, s.provider, DailyRequest{
		ProviderSymbol: target.ProviderSymbol, From: target.From, To: target.To,
	}, CollectOptions{MaxRetries: request.MaxRetries})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			_ = s.repository.cancelItem(context.WithoutCancel(ctx), runID, target.InstrumentID, s.now())
			return ctxErr
		}
		safe := safeImportError(err)
		if failErr := s.repository.failItem(context.WithoutCancel(ctx), runID, target.InstrumentID, safe, s.now()); failErr != nil {
			return errors.Join(err, failErr)
		}
		return err
	}
	expected, err := s.repository.expectedSessions(ctx, target)
	if err != nil {
		safe := SafeError{Code: "storage_error", Summary: "Market-data storage request failed."}
		_ = s.repository.failItem(context.WithoutCancel(ctx), runID, target.InstrumentID, safe, s.now())
		return err
	}
	validation, err := ValidateDailyPage(DailyPage{Bars: dataset.Bars, Actions: dataset.Actions}, ValidationOptions{ExpectedSessions: expected})
	if err != nil {
		safe := SafeError{Code: "validation_error", Summary: "Market-data validation failed."}
		_ = s.repository.failItem(context.WithoutCancel(ctx), runID, target.InstrumentID, safe, s.now())
		return err
	}
	scope, err := s.repository.BeginImportScope(ctx, request.Provider, target.InstrumentID, "daily")
	if err != nil {
		safe := SafeError{Code: "import_conflict", Summary: "Market-data import scope is already active."}
		_ = s.repository.failItem(context.WithoutCancel(ctx), runID, target.InstrumentID, safe, s.now())
		return err
	}
	defer func() { _ = scope.Rollback(context.WithoutCancel(ctx)) }()
	_, err = scope.persist(ctx, persistInput{
		RunID: runID, Target: target, Provider: request.Provider, Validation: validation,
		Processed: int64(len(dataset.Bars) + len(dataset.Actions)), ObservedAt: s.now(),
	})
	if err != nil {
		safe := SafeError{Code: "storage_error", Summary: "Market-data storage request failed."}
		_ = s.repository.failItem(context.WithoutCancel(ctx), runID, target.InstrumentID, safe, s.now())
		return err
	}
	if err := scope.Commit(ctx); err != nil {
		safe := SafeError{Code: "storage_error", Summary: "Market-data storage request failed."}
		_ = s.repository.failItem(context.WithoutCancel(ctx), runID, target.InstrumentID, safe, s.now())
		return fmt.Errorf("commit market-data import scope: %w", err)
	}
	return nil
}

func importRange(targets []ImportTarget) (SessionDate, SessionDate) {
	from, to := targets[0].From, targets[0].To
	for _, target := range targets[1:] {
		if target.From < from {
			from = target.From
		}
		if target.To > to {
			to = target.To
		}
	}
	return from, to
}

func safeImportError(err error) SafeError {
	var providerErr *ProviderError
	if errors.As(err, &providerErr) && providerErr.Code != "" && providerErr.Summary != "" {
		return SafeError{Code: providerErr.Code, Summary: providerErr.Summary}
	}
	return SanitizeError(err.Error())
}
