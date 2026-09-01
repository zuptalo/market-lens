package features

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"sync"
	"time"

	"market-lens/server/internal/instruments"
)

// ComputeRequest describes one run of the engine.
type ComputeRequest struct {
	Kind       RunKind
	Universe   string
	Workers    int
	AppVersion string
}

// Service runs the engine: one composite stage over the universe, then every instrument in
// its own transaction (research R-005).
type Service struct {
	repository *Repository
	logger     *slog.Logger
}

func NewService(repository *Repository, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{repository: repository, logger: logger}
}

// history is one instrument's stored bars and calendar, with the adjusted view cached per
// split segment: every session between two ex-dates sees the same adjusted series.
type history struct {
	instrument Instrument
	calendar   []Session
	bars       []Bar
	splits     []Split
	raw        *History
	adjusted   map[int]*History // by the number of splits applied
}

func (h *history) adjustedAt(session SessionDate) *History {
	applied := 0
	for _, split := range h.splits {
		if split.ExDate <= session {
			applied++
		}
	}
	if applied == 0 {
		return h.raw
	}
	if view, ok := h.adjusted[applied]; ok {
		return view
	}
	view := NewHistory(Adjusted(h.bars, h.splits[:applied], session), h.calendar)
	h.adjusted[applied] = view
	return view
}

func (s *Service) load(ctx context.Context, instrument Instrument, calendars map[UUID][]Session) (*history, error) {
	calendar, ok := calendars[instrument.ExchangeID]
	if !ok {
		var err error
		if calendar, err = s.repository.Calendar(ctx, instrument.ExchangeID); err != nil {
			return nil, err
		}
		calendars[instrument.ExchangeID] = calendar
	}
	bars, err := s.repository.Bars(ctx, instrument.ID)
	if err != nil {
		return nil, err
	}
	splits, err := s.repository.Splits(ctx, instrument.ID)
	if err != nil {
		return nil, err
	}
	sort.Slice(splits, func(i, j int) bool { return splits[i].ExDate < splits[j].ExDate })
	return &history{instrument: instrument, calendar: calendar, bars: bars, splits: splits,
		raw: NewHistory(bars, calendar), adjusted: map[int]*History{}}, nil
}

// Compute runs the engine over a universe and reports the run.
func (s *Service) Compute(ctx context.Context, request ComputeRequest) (Run, error) {
	if s == nil || s.repository == nil {
		return Run{}, errors.New("features service is required")
	}
	if request.Kind != RunKindFull {
		return Run{}, fmt.Errorf("features: run kind %q is not supported", request.Kind)
	}
	if request.Workers < 1 {
		request.Workers = 1
	}
	if request.AppVersion == "" {
		request.AppVersion = "unknown"
	}
	definitions, err := s.repository.Definitions(ctx)
	if err != nil {
		return Run{}, err
	}
	registry, err := NewRegistry(definitions)
	if err != nil {
		return Run{}, err
	}
	universeID, err := s.repository.UniverseID(ctx, request.Universe)
	if err != nil {
		return Run{}, err
	}
	members, err := s.repository.UniverseInstruments(ctx, universeID)
	if err != nil {
		return Run{}, err
	}
	runID, err := instruments.NewUUID()
	if err != nil {
		return Run{}, err
	}
	run := Run{ID: UUID(runID), Kind: request.Kind, Status: RunStatusRunning, UniverseID: universeID,
		StartedAt: time.Now().UTC(), AppVersion: request.AppVersion}
	if err := s.repository.CreateRun(ctx, run); err != nil {
		return Run{}, err
	}
	s.logger.Info("feature run started", "run_id", run.ID, "kind", run.Kind, "universe", request.Universe,
		"instruments", len(members), "workers", request.Workers)

	histories := make([]*history, 0, len(members))
	calendars := map[UUID][]Session{}
	for _, member := range members {
		h, err := s.load(ctx, member, calendars)
		if err != nil {
			return s.fail(ctx, run, err)
		}
		histories = append(histories, h)
	}

	// Stage one: the composite over the whole universe, before any instrument begins.
	composites, err := s.computeComposite(ctx, registry, run, universeID, histories)
	if err != nil {
		return s.fail(ctx, run, err)
	}

	// Stage two: each instrument in its own scope; a failure is recorded and the run goes on.
	var mu sync.Mutex
	var succeeded, failed int
	var values int64
	semaphore := make(chan struct{}, request.Workers)
	var wg sync.WaitGroup
	for _, h := range histories {
		wg.Add(1)
		semaphore <- struct{}{}
		go func(h *history) {
			defer wg.Done()
			defer func() { <-semaphore }()
			written, err := s.computeInstrument(ctx, registry, run, h, composites)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed++
			} else {
				succeeded++
				values += written
			}
		}(h)
	}
	wg.Wait()

	status := RunStatusSucceeded
	switch {
	case failed > 0 && succeeded == 0:
		status = RunStatusFailed
	case failed > 0:
		status = RunStatusPartial
	}
	finished := time.Now().UTC()
	if err := s.repository.FinishRun(ctx, run.ID, status, int64(len(members)), values, finished); err != nil {
		return Run{}, err
	}
	run.Status, run.FinishedAt, run.InstrumentCount, run.ValueCount = status, &finished, int64(len(members)), values
	s.logger.Info("feature run finished", "run_id", run.ID, "status", status, "instruments", len(members),
		"failed", failed, "values", values, "elapsed", finished.Sub(run.StartedAt))
	return run, nil
}

func (s *Service) fail(ctx context.Context, run Run, cause error) (Run, error) {
	finished := time.Now().UTC()
	if err := s.repository.FinishRun(ctx, run.ID, RunStatusFailed, 0, 0, finished); err != nil {
		return Run{}, errors.Join(cause, err)
	}
	s.logger.Error("feature run failed", "run_id", run.ID, "error", cause)
	return Run{}, cause
}

// computeComposite writes the composite at every session any member traded: the
// equal-weighted mean of the session-over-session adjusted return of every member with a
// bar at the session and at its exchange's previous session, as adjusted as of that session.
func (s *Service) computeComposite(ctx context.Context, registry *Registry, run Run, universeID UUID, histories []*history) (map[SessionDate]CompositeValue, error) {
	definition, ok := registry.Composite()
	if !ok {
		return map[SessionDate]CompositeValue{}, nil
	}
	contributors := map[SessionDate][]Contributor{}
	for _, h := range histories {
		sessions := h.raw.Sessions()
		for i := 1; i < len(sessions); i++ {
			if _, ok := h.raw.Bar(sessions[i]); !ok {
				continue
			}
			view := h.adjustedAt(sessions[i])
			window, reason := view.Window(sessions[i], 2)
			if reason != "" {
				if _, ok := contributors[sessions[i]]; !ok {
					contributors[sessions[i]] = nil
				}
				continue
			}
			value, reason := Return(closes(window))
			if reason != "" {
				continue
			}
			contributors[sessions[i]] = append(contributors[sessions[i]], Contributor{InstrumentID: h.instrument.ID, Return: value})
		}
	}
	if len(contributors) == 0 {
		return map[SessionDate]CompositeValue{}, nil
	}
	sessions := make([]SessionDate, 0, len(contributors))
	for session := range contributors {
		sessions = append(sessions, session)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i] < sessions[j] })
	rows := make([]CompositeRow, 0, len(sessions))
	series := make(map[SessionDate]CompositeValue, len(sessions))
	for _, session := range sessions {
		mean, count, reason := Composite(contributors[session], registry.MinContributors())
		row := CompositeRow{Session: session, ContributorCount: count}
		if reason != "" {
			absence := reason
			row.AbsenceReason = &absence
			series[session] = CompositeValue{ContributorCount: count}
		} else {
			text := Round(mean)
			row.MeanReturn = &text
			series[session] = CompositeValue{MeanReturn: mean, Defined: true, ContributorCount: count}
		}
		rows = append(rows, row)
	}
	if err := s.repository.WriteComposite(ctx, universeID, definition.ID, run.ID, sessions[0], sessions[len(sessions)-1], rows); err != nil {
		return nil, err
	}
	s.logger.Info("composite computed", "run_id", run.ID, "definition", definition.Name, "version", definition.Version,
		"sessions", len(rows))
	return series, nil
}

// computeInstrument computes every active definition at every session the instrument has a
// bar for, and commits them with the item and the change event as one transaction.
func (s *Service) computeInstrument(ctx context.Context, registry *Registry, run Run, h *history, composites map[SessionDate]CompositeValue) (int64, error) {
	started := time.Now().UTC()
	item := RunItem{RunID: run.ID, InstrumentID: h.instrument.ID, Status: RunItemRunning, StartedAt: started}
	if len(h.bars) == 0 {
		item.Status = RunItemSkipped
		finished := time.Now().UTC()
		item.FinishedAt = &finished
		if err := s.repository.WriteItem(ctx, item); err != nil {
			return 0, err
		}
		s.logger.Info("instrument skipped", "run_id", run.ID, "instrument_id", h.instrument.ID, "reason", "no stored history")
		return 0, nil
	}
	from, to := h.bars[0].Session, h.bars[len(h.bars)-1].Session
	item.FromSession, item.ToSession = &from, &to
	rows, err := s.rowsFor(registry, h, from, to, composites)
	if err == nil {
		err = s.commit(ctx, run, item, from, to, rows)
	}
	if err != nil {
		code, summary := "compute_failed", err.Error()
		item.Status, item.ErrorCode, item.ErrorSummary = RunItemFailed, &code, &summary
		finished := time.Now().UTC()
		item.FinishedAt = &finished
		if writeErr := s.repository.WriteItem(ctx, item); writeErr != nil {
			err = errors.Join(err, writeErr)
		}
		s.logger.Error("instrument failed", "run_id", run.ID, "instrument_id", h.instrument.ID, "error", err)
		return 0, err
	}
	s.logger.Info("instrument computed", "run_id", run.ID, "instrument_id", h.instrument.ID,
		"from", from, "to", to, "values", len(rows), "elapsed", time.Since(started))
	return int64(len(rows)), nil
}

func (s *Service) commit(ctx context.Context, run Run, item RunItem, from, to SessionDate, rows []ValueRow) error {
	scope, err := s.repository.BeginInstrumentScope(ctx, item.InstrumentID)
	if err != nil {
		return err
	}
	defer func() { _ = scope.Rollback(ctx) }()
	if err := scope.WriteValues(ctx, run.ID, from, to, rows); err != nil {
		return err
	}
	finished := time.Now().UTC()
	item.Status, item.ValueCount, item.FinishedAt = RunItemSucceeded, int64(len(rows)), &finished
	if err := scope.WriteItem(ctx, item); err != nil {
		return err
	}
	return scope.Commit(ctx, Change{InstrumentID: item.InstrumentID, FromSession: from, ToSession: to, RunID: run.ID})
}

// rowsFor evaluates every active definition at every stored session in [from, to].
func (s *Service) rowsFor(registry *Registry, h *history, from, to SessionDate, composites map[SessionDate]CompositeValue) ([]ValueRow, error) {
	active := registry.Active()
	var rows []ValueRow
	currency := h.instrument.Currency
	for _, session := range h.raw.Sessions() {
		if session < from || session > to {
			continue
		}
		if _, ok := h.raw.Bar(session); !ok {
			continue
		}
		adjusted := h.adjustedAt(session)
		for _, definition := range active {
			view := h.raw
			if definition.PriceBasis == PriceBasisAdjusted {
				view = adjusted
			}
			row := ValueRow{DefinitionID: definition.ID, Session: session}
			window, reason := view.Window(session, *definition.WindowSessions)
			if reason != "" {
				row.AbsenceReason = &reason
				rows = append(rows, row)
				continue
			}
			input := Input{Bars: window}
			if registry.UsesComposite(definition.Name) {
				input.Composites = make([]CompositeValue, 0, len(window)-1)
				for _, bar := range window[1:] {
					input.Composites = append(input.Composites, composites[bar.Session])
				}
			}
			result := registry.Compute(definition, input)
			switch {
			case result.Reason != "":
				reason := result.Reason
				row.AbsenceReason = &reason
			case result.Label != "":
				label := result.Label
				row.Label = &label
			case result.Value != nil:
				if math.IsNaN(*result.Value) || math.IsInf(*result.Value, 0) {
					return nil, fmt.Errorf("%s at %s is not finite", definition.Name, session)
				}
				text := Round(*result.Value)
				row.Value = &text
				if registry.Currency(definition.Name) {
					row.Currency = &currency
				}
			default:
				return nil, fmt.Errorf("%s at %s produced neither a value nor a reason", definition.Name, session)
			}
			rows = append(rows, row)
		}
	}
	return rows, nil
}
