package strategies

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"market-lens/server/internal/features"
	"market-lens/server/internal/instruments"
)

// Service computes signals for one published strategy version over a universe.
//
// The computation is session-major, which the feature engine's is not, because a cross-sectional
// factor ranks an instrument against the rest of the universe for the same session: the
// universe's values for a session must all exist before any one instrument's rank does. Writes
// stay instrument-major, so one instrument's failure is contained the way the engine contains it.
type Service struct {
	repository *Repository
	features   *features.Repository
	logger     *slog.Logger
}

func NewService(repository *Repository, featureRepository *features.Repository, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{repository: repository, features: featureRepository, logger: logger}
}

// blockSessions bounds how many sessions are held in memory at once. A full pass over a decade
// of a hundred-instrument universe is a quarter of a million signals with their contributions;
// computing it a block at a time keeps that bounded without giving up the session-major order
// the cross-sectional factors require.
const blockSessions = 128

// Compute runs one strategy version over a universe and records what it made of it.
func (s *Service) Compute(ctx context.Context, request ComputeRequest) (Run, error) {
	if s == nil || s.repository == nil || s.features == nil {
		return Run{}, errors.New("strategies service is required")
	}
	switch request.Kind {
	case RunKindFull, RunKindIncremental, RunKindStrategy:
	default:
		return Run{}, fmt.Errorf("strategies: run kind %q is not supported", request.Kind)
	}
	if request.Kind == RunKindIncremental && request.SinceFeatureRun == "" {
		return Run{}, errors.New("strategies: an incremental run needs the feature run it follows")
	}
	if request.Universe == "" {
		return Run{}, errors.New("strategies: a universe is required")
	}
	if request.AppVersion == "" {
		return Run{}, errors.New("strategies: an application version is required")
	}
	workers := request.Workers
	if workers < 1 {
		workers = 4
	}

	strategy, err := s.resolveStrategy(ctx, request)
	if err != nil {
		return Run{}, err
	}
	universeID, err := s.features.UniverseID(ctx, request.Universe)
	if err != nil {
		return Run{}, err
	}
	from, to, ok, err := s.sessionRange(ctx, request, universeID)
	if err != nil {
		return Run{}, err
	}

	runID, err := instruments.NewUUID()
	if err != nil {
		return Run{}, fmt.Errorf("strategies: mint a run identifier: %w", err)
	}
	run := Run{
		ID: runID, StrategyID: strategy.ID, Kind: request.Kind,
		Status: RunStatusRunning, UniverseID: universeID, StartedAt: time.Now().UTC(),
		AppVersion: request.AppVersion,
	}
	if request.Kind == RunKindIncremental {
		trigger := request.SinceFeatureRun
		run.TriggerFeatureRunID = &trigger
	}
	if err := s.repository.CreateRun(ctx, run); err != nil {
		return Run{}, err
	}
	if !ok {
		// Nothing has been computed for this universe yet. A run that scored nothing is a
		// succeeded run over an empty range, not a failure: there is no error to report.
		finished := time.Now().UTC()
		run.Status, run.FinishedAt = RunStatusSucceeded, &finished
		return run, s.repository.FinishRun(ctx, run.ID, run.Status, 0, 0, finished)
	}

	sessions, err := s.features.Sessions(ctx, universeID, from, to)
	if err != nil {
		return s.fail(ctx, run, err)
	}
	counts, err := s.features.SessionCountsBefore(ctx, universeID, from)
	if err != nil {
		return s.fail(ctx, run, err)
	}

	// Two passes.
	//
	// The first computes and writes nothing; it exists only to learn which instruments this
	// version cannot score. The second writes, skipping them. The alternative — write as you
	// go and stop when an instrument fails — leaves that instrument holding a mixture: the
	// early sessions rewritten by the run that failed and the rest from an earlier one. Every
	// row would be well formed, the series would be from no single computation, and nothing
	// downstream could tell. Recomputing is the cheaper mistake.
	failures := map[UUID]error{}
	if err := s.pass(ctx, run, strategy, universeID, sessions, cloneCounts(counts), failures, workers, false); err != nil {
		return s.fail(ctx, run, err)
	}
	written := map[UUID]bool{}
	signalCount, err := s.writePass(ctx, run, strategy, universeID, sessions, cloneCounts(counts),
		failures, workers, written)
	if err != nil {
		return s.fail(ctx, run, err)
	}
	instrumentsSeen := written

	if err := s.recordFailures(ctx, run, failures); err != nil {
		return s.fail(ctx, run, err)
	}

	finished := time.Now().UTC()
	run.Status = RunStatusSucceeded
	switch {
	case len(failures) > 0 && len(instrumentsSeen) == 0:
		run.Status = RunStatusFailed
	case len(failures) > 0:
		run.Status = RunStatusPartial
	}
	run.FinishedAt = &finished
	run.InstrumentCount = int64(len(instrumentsSeen))
	run.SignalCount = signalCount
	run.FailedCount = int64(len(failures))
	if err := s.repository.FinishRun(ctx, run.ID, run.Status, run.InstrumentCount, run.SignalCount, finished); err != nil {
		return Run{}, err
	}
	s.logger.Info("strategy run finished", "run", run.ID.String(), "strategy", strategy.Name,
		"version", strategy.Version, "status", string(run.Status), "instruments", run.InstrumentCount,
		"signals", run.SignalCount, "failed", run.FailedCount)
	return run, nil
}

func (s *Service) fail(ctx context.Context, run Run, cause error) (Run, error) {
	finished := time.Now().UTC()
	if err := s.repository.FinishRun(ctx, run.ID, RunStatusFailed, 0, 0, finished); err != nil {
		s.logger.Error("could not record the failed strategy run", "run", run.ID.String(), "error", err)
	}
	return Run{}, cause
}

func (s *Service) resolveStrategy(ctx context.Context, request ComputeRequest) (Strategy, error) {
	if request.Strategy != "" || request.Version > 0 {
		return s.repository.Strategy(ctx, request.Strategy, request.Version)
	}
	published, err := s.repository.Strategies(ctx, "", false)
	if err != nil {
		return Strategy{}, err
	}
	switch len(published) {
	case 0:
		return Strategy{}, fmt.Errorf("%w: no strategy is published", ErrNotFound)
	case 1:
		return published[0], nil
	default:
		return Strategy{}, errors.New("strategies: name the strategy to compute; several are published")
	}
}

func (s *Service) sessionRange(ctx context.Context, request ComputeRequest, universeID UUID) (SessionDate, SessionDate, bool, error) {
	if request.Kind == RunKindIncremental {
		return s.features.SessionsTouchedByFeatureRun(ctx, request.SinceFeatureRun)
	}
	return s.features.SessionBounds(ctx, universeID)
}

// blockResult is one block's signals, grouped by the instrument they belong to.
type blockResult map[UUID][]Signal

func cloneCounts(counts map[UUID]int) map[UUID]int {
	copied := make(map[UUID]int, len(counts))
	for instrument, count := range counts {
		copied[instrument] = count
	}
	return copied
}

// pass walks the sessions in blocks. When write is false nothing is stored: it is the
// validation pass, and its only output is the set of instruments that could not be scored.
func (s *Service) pass(
	ctx context.Context, run Run, strategy Strategy, universeID UUID, sessions []SessionDate,
	counts map[UUID]int, failures map[UUID]error, workers int, write bool,
) error {
	_, err := s.walk(ctx, run, strategy, universeID, sessions, counts, failures, workers, write, nil)
	return err
}

func (s *Service) writePass(
	ctx context.Context, run Run, strategy Strategy, universeID UUID, sessions []SessionDate,
	counts map[UUID]int, failures map[UUID]error, workers int, written map[UUID]bool,
) (int64, error) {
	return s.walk(ctx, run, strategy, universeID, sessions, counts, failures, workers, true, written)
}

func (s *Service) walk(
	ctx context.Context, run Run, strategy Strategy, universeID UUID, sessions []SessionDate,
	counts map[UUID]int, failures map[UUID]error, workers int, write bool, written map[UUID]bool,
) (int64, error) {
	names := make([]string, 0, len(strategy.Factors))
	for _, factor := range strategy.Factors {
		names = append(names, factor.Feature)
	}
	var total int64
	for start := 0; start < len(sessions); start += blockSessions {
		end := start + blockSessions
		if end > len(sessions) {
			end = len(sessions)
		}
		block := sessions[start:end]
		result, err := s.computeBlock(ctx, run, strategy, universeID, names, block, counts, failures)
		if err != nil {
			return 0, err
		}
		if !write {
			continue
		}
		if err := s.writeBlock(ctx, run, result, block[0], block[len(block)-1], workers); err != nil {
			return 0, err
		}
		for instrument, signals := range result {
			total += int64(len(signals))
			if written != nil {
				written[instrument] = true
			}
		}
	}
	return total, nil
}

func (s *Service) computeBlock(
	ctx context.Context, run Run, strategy Strategy, universeID UUID, names []string,
	block []SessionDate, counts map[UUID]int, failures map[UUID]error,
) (blockResult, error) {
	from, to := block[0], block[len(block)-1]
	values, err := s.features.ValuesForSessions(ctx, universeID, names, from, to)
	if err != nil {
		return nil, err
	}

	// session -> instrument -> feature name -> value
	bySession := map[SessionDate]map[UUID]map[string]features.UniverseValue{}
	for _, value := range values {
		instruments, known := bySession[value.SessionDate]
		if !known {
			instruments = map[UUID]map[string]features.UniverseValue{}
			bySession[value.SessionDate] = instruments
		}
		byName, known := instruments[value.InstrumentID]
		if !known {
			byName = map[string]features.UniverseValue{}
			instruments[value.InstrumentID] = byName
		}
		byName[value.Name] = value
	}

	// The history gate counts sessions as the walk advances, so it stays exact without
	// re-reading the history it is counting.
	result := blockResult{}
	computedAt := time.Now().UTC()
	for _, session := range block {
		instruments := bySession[session]
		ranks := s.rankUniverse(strategy, instruments)
		for instrument, byName := range instruments {
			if failures[instrument] != nil {
				continue
			}
			signal, err := s.scoreInstrument(strategy, instrument, session, byName, ranks, counts[instrument])
			if err != nil {
				failures[instrument] = err
				delete(result, instrument)
				continue
			}
			signal.StrategyID, signal.RunID, signal.ComputedAt = strategy.ID, run.ID, computedAt
			result[instrument] = append(result[instrument], signal)
		}
		for instrument := range instruments {
			counts[instrument]++
		}
	}
	// An instrument that failed anywhere in this block contributes nothing from it, even for
	// the sessions before the failure.
	for instrument := range failures {
		delete(result, instrument)
	}
	return result, nil
}

func (s *Service) rankUniverse(strategy Strategy, instruments map[UUID]map[string]features.UniverseValue) map[string]map[string]float64 {
	ranks := map[string]map[string]float64{}
	for _, factor := range strategy.Factors {
		if factor.Mode != CrossSectional {
			continue
		}
		observations := make([]Observation, 0, len(instruments))
		for instrument, byName := range instruments {
			value, known := byName[factor.Feature]
			if !known || value.Value == nil {
				continue
			}
			number, err := parseDecimal(*value.Value)
			if err != nil {
				continue
			}
			observations = append(observations, Observation{Instrument: instrument.String(), Value: number})
		}
		ranks[factor.Name] = PercentileRanks(observations)
	}
	return ranks
}

func (s *Service) scoreInstrument(
	strategy Strategy, instrument UUID, session SessionDate,
	byName map[string]features.UniverseValue, ranks map[string]map[string]float64, storedSessions int,
) (Signal, error) {
	signal := Signal{InstrumentID: instrument, SessionDate: session}

	if storedSessions < strategy.MinSessions {
		reason := AbsenceInsufficientHistory
		signal.AbsenceReason = &reason
		signal.Contributions = []Contribution{}
		return signal, nil
	}

	inputs := make([]FactorInput, 0, len(strategy.Factors))
	for _, factor := range strategy.Factors {
		input := FactorInput{Factor: factor, FeatureSession: session.String()}
		value, known := byName[factor.Feature]
		switch {
		case !known:
			input.UnavailableReason = string(AbsenceFeatureMissing)
		case value.AbsenceReason != nil:
			input.UnavailableReason = string(*value.AbsenceReason)
		case factor.Mode == CrossSectional:
			if value.Value == nil {
				input.UnavailableReason = string(AbsenceFeatureMissing)
				break
			}
			rank, ranked := ranks[factor.Name][instrument.String()]
			if !ranked {
				input.UnavailableReason = string(AbsenceFeatureMissing)
				break
			}
			input.FeatureValue = value.Value
			score := rank
			input.Score = &score
		default:
			transform, defined := strategy.Transforms[factor.Name]
			if !defined {
				return Signal{}, fmt.Errorf("factor %q has no transform", factor.Name)
			}
			var number float64
			var label string
			if value.Value != nil {
				parsed, err := parseDecimal(*value.Value)
				if err != nil {
					return Signal{}, fmt.Errorf("factor %q value: %w", factor.Name, err)
				}
				number = parsed
				input.FeatureValue = value.Value
			}
			if value.Label != nil {
				label = *value.Label
				input.FeatureValue = value.Label
			}
			if value.Value == nil && value.Label == nil {
				input.UnavailableReason = string(AbsenceFeatureMissing)
				break
			}
			score, err := Normalise(transform, number, label)
			if err != nil {
				return Signal{}, fmt.Errorf("factor %q: %w", factor.Name, err)
			}
			input.Score = &score
		}
		inputs = append(inputs, input)
	}

	scored := Score(inputs)
	signal.Contributions = scored.Contributions
	if scored.Absent {
		reason := AbsenceReason(scored.AbsenceReason)
		signal.AbsenceReason = &reason
		return signal, nil
	}
	action, err := ActionFor(strategy.ActionBands, scored.Score)
	if err != nil {
		return Signal{}, err
	}
	score, confidence, divisor := Round(scored.Score), Round(scored.Confidence), Round(scored.Divisor)
	chosen := Action(action)
	signal.Score, signal.Confidence, signal.Divisor, signal.Action = &score, &confidence, &divisor, &chosen
	return signal, nil
}

func (s *Service) writeBlock(
	ctx context.Context, run Run, result blockResult, from, to SessionDate, workers int,
) error {
	type job struct {
		instrument UUID
		signals    []Signal
	}
	jobs := make(chan job)
	var group sync.WaitGroup
	var mutex sync.Mutex
	var firstError error

	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for next := range jobs {
				if err := s.writeInstrument(ctx, run, next.instrument, next.signals, from, to); err != nil {
					mutex.Lock()
					if firstError == nil {
						firstError = err
					}
					mutex.Unlock()
				}
			}
		}()
	}
	for instrument, signals := range result {
		jobs <- job{instrument: instrument, signals: signals}
	}
	close(jobs)
	group.Wait()
	return firstError
}

// recordFailures gives every contained failure its own run item. A run that quietly covered
// fewer instruments than the universe holds would look like a smaller universe, so the
// operations screen has to be able to say which instrument was left out and why.
func (s *Service) recordFailures(ctx context.Context, run Run, failures map[UUID]error) error {
	for instrument, cause := range failures {
		code, summary := "compute_failed", cause.Error()
		finished := time.Now().UTC()
		if err := s.repository.WriteItem(ctx, RunItem{
			RunID: run.ID, InstrumentID: instrument, Status: RunItemFailed,
			ErrorCode: &code, ErrorSummary: &summary, StartedAt: run.StartedAt, FinishedAt: &finished,
		}); err != nil {
			return err
		}
		s.logger.Warn("an instrument was left out of the strategy run",
			"run", run.ID.String(), "instrument", instrument.String(), "error", cause)
	}
	return nil
}

func (s *Service) writeInstrument(ctx context.Context, run Run, instrument UUID, signals []Signal, from, to SessionDate) (err error) {
	// One instrument's failure must not end the run, including a failure nobody predicted.
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic while writing signals: %v", recovered)
		}
	}()
	finished := time.Now().UTC()
	item := RunItem{
		RunID: run.ID, InstrumentID: instrument, Status: RunItemSucceeded,
		FromSession: &from, ToSession: &to, SignalCount: int64(len(signals)),
		StartedAt: run.StartedAt, FinishedAt: &finished,
	}
	change := Change{
		InstrumentID: instrument, FromSession: from, ToSession: to,
		RunID: run.ID, StrategyID: run.StrategyID,
	}
	return s.repository.WriteSignals(ctx, run, item, signals, change)
}
