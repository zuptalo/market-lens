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
	// SinceRun is the import run an incremental run follows: the sessions it wrote or revised
	// are what gets recomputed.
	SinceRun UUID
	// Definition is the one definition a definition run recomputes across the full history.
	Definition string
}

// scope is one instrument's share of a run: the sessions to recompute and the definitions
// to recompute there — every active one, or the few a run is limited to.
type scope struct {
	h           *history
	from, to    SessionDate
	definitions []Definition
	// all says the definitions are every active one, so the write replaces every value in
	// the range rather than only the listed definitions'.
	all bool
}

// Service runs the engine: one composite stage over the universe, then every instrument in
// its own transaction (research R-005).
// SignalComputer is the strategy layer, seen from here as one question: the features for these
// sessions have changed, would you like to score them? The engine knows nothing else about it,
// which is what keeps the dependency pointing one way — signals read features, never the reverse.
type SignalComputer interface {
	ComputeSinceFeatureRun(context.Context, UUID) error
}

type Service struct {
	repository *Repository
	logger     *slog.Logger
	// Signals, when set, is asked to score every successful run. It is optional: the engine is
	// useful on its own, and the composition root decides whether a strategy layer exists.
	Signals SignalComputer
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

func (s *Service) load(ctx context.Context, instrument Instrument, calendar []Session) (*history, error) {
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

// loadAll reads every member's history, the exchange calendars once each and the per-instrument
// bars and splits concurrently: an incremental pass computes little but still reads whole
// histories, so the reads, not the arithmetic, set its floor.
func (s *Service) loadAll(ctx context.Context, members []Instrument, workers int) ([]*history, error) {
	calendars := map[UUID][]Session{}
	for _, member := range members {
		if _, ok := calendars[member.ExchangeID]; ok {
			continue
		}
		calendar, err := s.repository.Calendar(ctx, member.ExchangeID)
		if err != nil {
			return nil, err
		}
		calendars[member.ExchangeID] = calendar
	}
	histories := make([]*history, len(members))
	errs := make([]error, len(members))
	semaphore := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for index, member := range members {
		wg.Add(1)
		semaphore <- struct{}{}
		go func(index int, member Instrument) {
			defer wg.Done()
			defer func() { <-semaphore }()
			histories[index], errs[index] = s.load(ctx, member, calendars[member.ExchangeID])
		}(index, member)
	}
	wg.Wait()
	return histories, errors.Join(errs...)
}

// Compute runs the engine over a universe and reports the run.
func (s *Service) Compute(ctx context.Context, request ComputeRequest) (Run, error) {
	if s == nil || s.repository == nil {
		return Run{}, errors.New("features service is required")
	}
	switch request.Kind {
	case RunKindFull, RunKindIncremental, RunKindDefinition:
	default:
		return Run{}, fmt.Errorf("features: run kind %q is not supported", request.Kind)
	}
	if request.Kind == RunKindIncremental && request.SinceRun == "" {
		return Run{}, errors.New("features: an incremental run needs the import run it follows")
	}
	if request.Kind == RunKindDefinition && request.Definition == "" {
		return Run{}, errors.New("features: a definition run needs the definition to recompute")
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
	var recomputed []Definition
	if request.Kind == RunKindDefinition {
		if recomputed = registry.Named(request.Definition); len(recomputed) == 0 {
			return Run{}, fmt.Errorf("features: %q is not an active definition", request.Definition)
		}
	}
	universeID, err := s.repository.UniverseID(ctx, request.Universe)
	if err != nil {
		return Run{}, err
	}
	members, err := s.repository.UniverseInstruments(ctx, universeID)
	if err != nil {
		return Run{}, err
	}
	var touched map[UUID][]SessionDate
	if request.Kind == RunKindIncremental {
		if touched, err = s.repository.SessionsTouchedByRun(ctx, request.SinceRun); err != nil {
			return Run{}, err
		}
	}
	runID, err := instruments.NewUUID()
	if err != nil {
		return Run{}, err
	}
	run := Run{ID: UUID(runID), Kind: request.Kind, Status: RunStatusRunning, UniverseID: universeID,
		StartedAt: time.Now().UTC(), AppVersion: request.AppVersion}
	if request.Kind == RunKindIncremental {
		since := request.SinceRun
		run.TriggerRunID = &since
	}
	if request.Kind == RunKindDefinition {
		name := request.Definition
		run.DefinitionName = &name
	}
	if err := s.repository.CreateRun(ctx, run); err != nil {
		return Run{}, err
	}
	s.logger.Info("feature run started", "run_id", run.ID, "kind", run.Kind, "universe", request.Universe,
		"instruments", len(members), "workers", request.Workers, "trigger_run_id", request.SinceRun,
		"definition", request.Definition)

	if request.Kind == RunKindIncremental && len(touched) == 0 {
		return s.finish(ctx, run, 0, 0, 0, 0)
	}
	histories, err := s.loadAll(ctx, members, request.Workers)
	if err != nil {
		return s.fail(ctx, run, err)
	}

	// Stage one: the composite over the whole universe, before any instrument begins. A
	// definition run leaves the stored composite alone unless it is the composite itself.
	var scopes []scope
	var writeComposite *SessionRange
	switch request.Kind {
	case RunKindFull:
		writeComposite = &SessionRange{}
		for _, h := range histories {
			scopes = append(scopes, s.fullScope(h, registry.Active(), true))
		}
	case RunKindDefinition:
		if _, isComposite := registry.Composite(); isComposite && recomputed[0].Name == CompositeDefinitionName {
			writeComposite = &SessionRange{}
			recomputed = registry.CompositeUsers()
		}
		for _, h := range histories {
			scopes = append(scopes, s.fullScope(h, recomputed, false))
		}
	case RunKindIncremental:
		scopes, writeComposite = s.incrementalScopes(registry, histories, touched)
	}
	composites, err := s.computeComposite(ctx, registry, run, universeID, histories, writeComposite)
	if err != nil {
		return s.fail(ctx, run, err)
	}

	// Stage two: each instrument in its own scope; a failure is recorded and the run goes on.
	var mu sync.Mutex
	var succeeded, failed int
	var values int64
	semaphore := make(chan struct{}, request.Workers)
	var wg sync.WaitGroup
	for _, sc := range scopes {
		wg.Add(1)
		semaphore <- struct{}{}
		go func(sc scope) {
			defer wg.Done()
			defer func() { <-semaphore }()
			written, err := s.computeInstrument(ctx, registry, run, sc, composites)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed++
			} else {
				succeeded++
				values += written
			}
		}(sc)
	}
	wg.Wait()
	return s.finish(ctx, run, len(members), succeeded, failed, values)
}

// fullScope covers every stored session of an instrument.
func (s *Service) fullScope(h *history, definitions []Definition, all bool) scope {
	sc := scope{h: h, definitions: definitions, all: all}
	if len(h.bars) > 0 {
		sc.from, sc.to = h.bars[0].Session, h.bars[len(h.bars)-1].Session
	}
	return sc
}

// incrementalScopes limits each instrument to the sessions the touched bars take part in
// (research R-004). A touched instrument recomputes every definition over
// [S, S + W_max − 1]. The composite changes at each touched session and the one after it,
// so every other member recomputes only the definitions that read the composite, over the
// reach of the longest of their windows. The composite itself is rewritten over the union.
func (s *Service) incrementalScopes(registry *Registry, histories []*history, touched map[UUID][]SessionDate) ([]scope, *SessionRange) {
	users := registry.CompositeUsers()
	usersWindow := 0
	for _, definition := range users {
		usersWindow = max(usersWindow, *definition.WindowSessions)
	}
	var earliest, latest SessionDate
	for _, sessions := range touched {
		for _, session := range sessions {
			if earliest == "" || session < earliest {
				earliest = session
			}
			latest = max(latest, session)
		}
	}
	write := SessionRange{From: earliest, To: latest}
	var scopes []scope
	for _, h := range histories {
		if len(h.bars) == 0 {
			continue
		}
		lastBar := h.bars[len(h.bars)-1].Session
		if sessions, ok := touched[h.instrument.ID]; ok {
			first, last := sessions[0], sessions[0]
			for _, session := range sessions[1:] {
				first, last = min(first, session), max(last, session)
			}
			reach := AffectedRange(h.calendar, last, registry.WMax())
			write.To = max(write.To, reach.To)
			scopes = append(scopes, scope{h: h, from: first, to: min(reach.To, lastBar), definitions: registry.Active(), all: true})
			continue
		}
		if len(users) == 0 {
			continue
		}
		// A window of n sessions reads the composite at its last n−1 sessions, so the
		// composite at S+1 is read up to S + n − 1: the reach of a window of n from S.
		reach := AffectedRange(h.calendar, latest, usersWindow)
		from, to := max(earliest, h.bars[0].Session), min(reach.To, lastBar)
		if from > to {
			continue
		}
		scopes = append(scopes, scope{h: h, from: from, to: to, definitions: users})
	}
	return scopes, &write
}

func (s *Service) finish(ctx context.Context, run Run, instrumentCount, succeeded, failed int, values int64) (Run, error) {
	status := RunStatusSucceeded
	switch {
	case failed > 0 && succeeded == 0:
		status = RunStatusFailed
	case failed > 0:
		status = RunStatusPartial
	}
	finished := time.Now().UTC()
	if err := s.repository.FinishRun(ctx, run.ID, status, int64(instrumentCount), values, finished); err != nil {
		return Run{}, err
	}
	run.Status, run.FinishedAt, run.InstrumentCount, run.ValueCount = status, &finished, int64(instrumentCount), values
	s.logger.Info("feature run finished", "run_id", run.ID, "status", status, "instruments", instrumentCount,
		"failed", failed, "values", values, "elapsed", finished.Sub(run.StartedAt))

	// Signals are scored from what this run just committed, so the ask comes after the values
	// are durable. A failure there is logged and swallowed: the values are correct and stored,
	// and turning a good computation into a failed one would make an operator repeat the
	// expensive half of the work to fix the cheap half.
	if s.Signals != nil && status != RunStatusFailed {
		if err := s.Signals.ComputeSinceFeatureRun(ctx, run.ID); err != nil {
			s.logger.Error("signal computation after the feature run failed",
				"run_id", run.ID, "error", err)
		}
	}
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
//
// The series is computed in full so every window can read it; write says which sessions
// are stored — nil for none, an empty range for all, a range for the sessions inside it.
func (s *Service) computeComposite(ctx context.Context, registry *Registry, run Run, universeID UUID, histories []*history, write *SessionRange) (map[SessionDate]CompositeValue, error) {
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
	if write == nil {
		return series, nil
	}
	from, to := sessions[0], sessions[len(sessions)-1]
	if write.From != "" || write.To != "" {
		from, to = max(from, write.From), min(to, write.To)
		kept := rows[:0]
		for _, row := range rows {
			if row.Session >= from && row.Session <= to {
				kept = append(kept, row)
			}
		}
		rows = kept
	}
	if from > to {
		return series, nil
	}
	if err := s.repository.WriteComposite(ctx, universeID, definition.ID, run.ID, from, to, rows); err != nil {
		return nil, err
	}
	s.logger.Info("composite computed", "run_id", run.ID, "definition", definition.Name, "version", definition.Version,
		"from", from, "to", to, "sessions", len(rows))
	return series, nil
}

// computeInstrument computes the scope's definitions at every session in its range the
// instrument has a bar for, and commits them with the item and the change event as one
// transaction. A failure of any kind — an error or a panic inside one definition — is
// contained to this instrument: the scope rolls back, the item records it, and the run goes
// on with the previous values still in force (FR-023).
func (s *Service) computeInstrument(ctx context.Context, registry *Registry, run Run, sc scope, composites map[SessionDate]CompositeValue) (int64, error) {
	started := time.Now().UTC()
	item := RunItem{RunID: run.ID, InstrumentID: sc.h.instrument.ID, Status: RunItemRunning, StartedAt: started}
	if len(sc.h.bars) == 0 || sc.from == "" {
		item.Status = RunItemSkipped
		finished := time.Now().UTC()
		item.FinishedAt = &finished
		if err := s.repository.WriteItem(ctx, item); err != nil {
			return 0, err
		}
		s.logger.Info("instrument skipped", "run_id", run.ID, "instrument_id", sc.h.instrument.ID, "reason", "no stored history")
		return 0, nil
	}
	item.FromSession, item.ToSession = &sc.from, &sc.to
	var rows []ValueRow
	err, code := s.attempt(func() error {
		var err error
		if rows, err = s.rowsFor(registry, sc, composites); err != nil {
			return err
		}
		return s.commit(ctx, run, item, sc, rows)
	})
	if err != nil {
		summary := err.Error()
		item.Status, item.ErrorCode, item.ErrorSummary = RunItemFailed, &code, &summary
		finished := time.Now().UTC()
		item.FinishedAt = &finished
		if writeErr := s.repository.WriteItem(ctx, item); writeErr != nil {
			err = errors.Join(err, writeErr)
		}
		s.logger.Error("instrument failed", "run_id", run.ID, "instrument_id", sc.h.instrument.ID, "error_code", code, "error", err)
		return 0, err
	}
	s.logger.Info("instrument computed", "run_id", run.ID, "instrument_id", sc.h.instrument.ID,
		"from", sc.from, "to", sc.to, "definitions", len(sc.definitions), "values", len(rows), "elapsed", time.Since(started))
	return int64(len(rows)), nil
}

// attempt runs one instrument's computation and turns a panic into an error with its own
// code, so a defect in one definition at one session cannot take the run down.
func (s *Service) attempt(compute func() error) (err error, code string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err, code = fmt.Errorf("panic: %v", recovered), "compute_panicked"
		}
	}()
	if err := compute(); err != nil {
		return err, "compute_failed"
	}
	return nil, ""
}

func (s *Service) commit(ctx context.Context, run Run, item RunItem, sc scope, rows []ValueRow) error {
	tx, err := s.repository.BeginInstrumentScope(ctx, item.InstrumentID)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var only []UUID
	if !sc.all {
		for _, definition := range sc.definitions {
			only = append(only, definition.ID)
		}
	}
	if err := tx.WriteValues(ctx, run.ID, sc.from, sc.to, only, rows); err != nil {
		return err
	}
	finished := time.Now().UTC()
	item.Status, item.ValueCount, item.FinishedAt = RunItemSucceeded, int64(len(rows)), &finished
	if err := tx.WriteItem(ctx, item); err != nil {
		return err
	}
	return tx.Commit(ctx, Change{InstrumentID: item.InstrumentID, FromSession: sc.from, ToSession: sc.to, RunID: run.ID})
}

// rowsFor evaluates the scope's definitions at every stored session in its range.
func (s *Service) rowsFor(registry *Registry, sc scope, composites map[SessionDate]CompositeValue) ([]ValueRow, error) {
	h := sc.h
	var rows []ValueRow
	currency := h.instrument.Currency
	for _, session := range h.raw.Sessions() {
		if session < sc.from || session > sc.to {
			continue
		}
		if _, ok := h.raw.Bar(session); !ok {
			continue
		}
		adjusted := h.adjustedAt(session)
		for _, definition := range sc.definitions {
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
