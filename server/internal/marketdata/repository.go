package marketdata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	clientevents "market-lens/server/internal/events"
	"market-lens/server/internal/instruments"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrImportConflict = errors.New("market-data import scope is already locked")
var ErrNotFound = errors.New("market-data resource not found")

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) TargetsForUniverse(ctx context.Context, provider, universe string) ([]ImportTarget, error) {
	if strings.TrimSpace(provider) == "" || strings.TrimSpace(universe) == "" {
		return nil, errors.New("provider and universe are required")
	}
	rows, err := r.pool.Query(ctx, `SELECT i.id::text,p.provider_symbol,i.currency
		FROM research_universes u
		JOIN universe_memberships m ON m.universe_id=u.id AND m.included_to IS NULL
		JOIN instruments i ON i.id=m.instrument_id AND i.active
		JOIN provider_instruments p ON p.instrument_id=i.id AND p.provider=$2 AND p.active
		JOIN exchanges e ON e.id=i.exchange_id
		WHERE u.code=$1 AND u.active
		ORDER BY e.mic,i.ticker,i.id`, universe, provider)
	if err != nil {
		return nil, fmt.Errorf("load universe import targets: %w", err)
	}
	defer rows.Close()
	targets := make([]ImportTarget, 0)
	for rows.Next() {
		var rawID string
		var target ImportTarget
		if err := rows.Scan(&rawID, &target.ProviderSymbol, &target.Currency); err != nil {
			return nil, err
		}
		target.InstrumentID, err = instruments.ParseUUID(rawID)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, errors.New("research universe has no active provider mappings")
	}
	return targets, nil
}

type ImportScope struct{ tx pgx.Tx }

func (r *Repository) BeginImportScope(ctx context.Context, provider string, instrumentID instruments.UUID, interval string) (*ImportScope, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("market-data repository is required")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin market-data import scope: %w", err)
	}
	key := strings.Join([]string{provider, instrumentID.String(), interval}, "|")
	var acquired bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1, 0))`, key).Scan(&acquired); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("acquire market-data import scope: %w", err)
	}
	if !acquired {
		_ = tx.Rollback(ctx)
		return nil, ErrImportConflict
	}
	return &ImportScope{tx: tx}, nil
}

func (s *ImportScope) Commit(ctx context.Context) error {
	if s == nil || s.tx == nil {
		return errors.New("market-data import scope is not active")
	}
	return s.tx.Commit(ctx)
}

func (s *ImportScope) Rollback(ctx context.Context) error {
	if s == nil || s.tx == nil {
		return nil
	}
	return s.tx.Rollback(ctx)
}

func (r *Repository) createRun(ctx context.Context, run ImportRun, targets []ImportTarget) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin import run: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var from, to any
	if run.RequestedFrom != nil {
		from = run.RequestedFrom.String()
	}
	if run.RequestedTo != nil {
		to = run.RequestedTo.String()
	}
	if _, err := tx.Exec(ctx, `INSERT INTO import_runs
		(id,kind,provider,requested_from,requested_to,status,parent_run_id,started_at,app_version)
		VALUES ($1,$2,$3,$4,$5,'running',$6,$7,$8)`, run.ID.String(), run.Kind, run.Provider,
		from, to, uuidValue(run.ParentRunID), run.StartedAt, run.AppVersion); err != nil {
		return fmt.Errorf("insert import run: %w", err)
	}
	if err := emitEvent(ctx, tx, "import_run", run.ID.String(), map[string]any{"status": ImportRunning}, run.StartedAt); err != nil {
		return err
	}
	for _, target := range targets {
		if _, err := tx.Exec(ctx, `INSERT INTO import_items
			(run_id,instrument_id,requested_from,requested_to,status)
			VALUES ($1,$2,$3,$4,'queued')`, run.ID.String(), target.InstrumentID.String(),
			target.From.String(), target.To.String()); err != nil {
			return fmt.Errorf("insert import item: %w", err)
		}
		if err := emitEvent(ctx, tx, "import_item", importItemEntityID(run.ID, target.InstrumentID),
			map[string]any{"status": ImportQueued, "run_id": run.ID.String(), "instrument_id": target.InstrumentID.String()}, run.StartedAt); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit import run: %w", err)
	}
	return nil
}

func (r *Repository) markItemRunning(ctx context.Context, runID, instrumentID instruments.UUID, startedAt time.Time) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := tx.Exec(ctx, `UPDATE import_items SET status='running', started_at=$3,
		attempts=attempts+1 WHERE run_id=$1 AND instrument_id=$2 AND status='queued'`,
		runID.String(), instrumentID.String(), startedAt)
	if err != nil {
		return fmt.Errorf("start import item: %w", err)
	}
	if result.RowsAffected() != 1 {
		return errors.New("import item is not queued")
	}
	if err := emitEvent(ctx, tx, "import_item", importItemEntityID(runID, instrumentID),
		map[string]any{"status": ImportRunning, "run_id": runID.String(), "instrument_id": instrumentID.String()}, startedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) failItem(ctx context.Context, runID, instrumentID instruments.UUID, safe SafeError, finishedAt time.Time) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `UPDATE import_items SET status='failed', finished_at=$3,
		error_code=$4,error_summary=$5 WHERE run_id=$1 AND instrument_id=$2 AND status='running'`,
		runID.String(), instrumentID.String(), finishedAt, safe.Code, safe.Summary)
	if err != nil {
		return fmt.Errorf("fail import item: %w", err)
	}
	if err := emitEvent(ctx, tx, "import_item", importItemEntityID(runID, instrumentID),
		map[string]any{"status": ImportFailed, "run_id": runID.String(), "instrument_id": instrumentID.String()}, finishedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) cancelItem(ctx context.Context, runID, instrumentID instruments.UUID, finishedAt time.Time) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `UPDATE import_items SET status='cancelled',
		started_at=coalesce(started_at,$3),finished_at=$3,error_code='cancelled',
		error_summary='Market-data request was cancelled.'
		WHERE run_id=$1 AND instrument_id=$2 AND status IN ('queued','running')`,
		runID.String(), instrumentID.String(), finishedAt)
	if err != nil {
		return fmt.Errorf("cancel import item: %w", err)
	}
	if err := emitEvent(ctx, tx, "import_item", importItemEntityID(runID, instrumentID),
		map[string]any{"status": ImportCancelled, "run_id": runID.String(), "instrument_id": instrumentID.String()}, finishedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) expectedSessions(ctx context.Context, target ImportTarget) (map[SessionDate]struct{}, error) {
	rows, err := r.pool.Query(ctx, `SELECT s.session_date::text FROM exchange_sessions s
		JOIN instruments i ON i.exchange_id=s.exchange_id
		WHERE i.id=$1 AND s.session_date BETWEEN $2 AND $3 AND s.status IN ('open','half_day')
		ORDER BY s.session_date`, target.InstrumentID.String(), target.From.String(), target.To.String())
	if err != nil {
		return nil, fmt.Errorf("load expected exchange sessions: %w", err)
	}
	defer rows.Close()
	result := make(map[SessionDate]struct{})
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		date, err := ParseSessionDate(raw)
		if err != nil {
			return nil, err
		}
		result[date] = struct{}{}
	}
	return result, rows.Err()
}

type persistInput struct {
	RunID      instruments.UUID
	Target     ImportTarget
	Provider   string
	Validation ValidationResult
	Processed  int64
	ObservedAt time.Time
}

func (s *ImportScope) persist(ctx context.Context, input persistInput) (ImportCounts, error) {
	counts := ImportCounts{
		Processed: input.Processed,
		Accepted:  int64(len(input.Validation.Bars) + len(input.Validation.Actions)),
		Rejected:  int64(input.Validation.Rejected),
		Flagged:   flaggedObservationCount(input.Validation, input.Processed),
	}
	for _, candidate := range input.Validation.Bars {
		changed, err := s.upsertBar(ctx, input, candidate)
		if err != nil {
			return ImportCounts{}, err
		}
		if changed {
			entityID := input.Target.InstrumentID.String() + ":" + candidate.SessionDate.String()
			if err := emitEvent(ctx, s.tx, "daily_bar", entityID,
				map[string]any{"instrument_id": input.Target.InstrumentID.String(), "session_date": candidate.SessionDate.String()}, input.ObservedAt); err != nil {
				return ImportCounts{}, err
			}
		}
	}
	for _, action := range input.Validation.Actions {
		if _, err := s.upsertAction(ctx, input, action); err != nil {
			return ImportCounts{}, err
		}
	}
	for _, issue := range input.Validation.Issues {
		findingID, err := s.insertFinding(ctx, input, issue)
		if err != nil {
			return ImportCounts{}, err
		}
		if err := emitEvent(ctx, s.tx, "quality_finding", findingID.String(),
			map[string]any{"instrument_id": input.Target.InstrumentID.String(), "rule": issue.Rule}, input.ObservedAt); err != nil {
			return ImportCounts{}, err
		}
	}
	status := ImportSucceeded
	if counts.Rejected > 0 {
		status = ImportPartial
	}
	_, err := s.tx.Exec(ctx, `UPDATE import_items SET status=$3,processed_count=$4,accepted_count=$5,
		rejected_count=$6,flagged_count=$7,finished_at=$8 WHERE run_id=$1 AND instrument_id=$2 AND status='running'`,
		input.RunID.String(), input.Target.InstrumentID.String(), status, counts.Processed, counts.Accepted,
		counts.Rejected, counts.Flagged, input.ObservedAt)
	if err != nil {
		return ImportCounts{}, fmt.Errorf("finish import item: %w", err)
	}
	if err := emitEvent(ctx, s.tx, "import_item", importItemEntityID(input.RunID, input.Target.InstrumentID),
		map[string]any{"status": status, "run_id": input.RunID.String(), "instrument_id": input.Target.InstrumentID.String()}, input.ObservedAt); err != nil {
		return ImportCounts{}, err
	}
	return counts, nil
}

func (s *ImportScope) upsertBar(ctx context.Context, input persistInput, candidate ProviderBar) (bool, error) {
	var existing DailyBar
	var open, high, low, closeValue string
	var adjusted *string
	var importRunID string
	err := s.tx.QueryRow(ctx, `SELECT open::text,high::text,low::text,close::text,adjusted_close::text,
		volume,currency,provider,source_hash,import_run_id::text,first_observed_at,last_observed_at
		FROM daily_price_bars WHERE instrument_id=$1 AND session_date=$2 FOR UPDATE`,
		input.Target.InstrumentID.String(), candidate.SessionDate.String()).Scan(
		&open, &high, &low, &closeValue, &adjusted, &existing.Volume,
		&existing.Currency, &existing.Provider, &existing.SourceHash, &importRunID,
		&existing.FirstObservedAt, &existing.LastObservedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = s.tx.Exec(ctx, `INSERT INTO daily_price_bars
			(instrument_id,session_date,open,high,low,close,adjusted_close,volume,currency,provider,
			source_hash,import_run_id,first_observed_at,last_observed_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$13)`,
			input.Target.InstrumentID.String(), candidate.SessionDate.String(), candidate.Open.String(),
			candidate.High.String(), candidate.Low.String(), candidate.Close.String(), decimalValue(candidate.AdjustedClose),
			candidate.Volume, input.Target.Currency, input.Provider, candidate.SourceHash, input.RunID.String(), input.ObservedAt)
		return err == nil, err
	}
	if err != nil {
		return false, fmt.Errorf("read current daily bar: %w", err)
	}
	for _, field := range []struct {
		raw         string
		destination *Decimal
	}{
		{open, &existing.Open}, {high, &existing.High}, {low, &existing.Low}, {closeValue, &existing.Close},
	} {
		value, parseErr := ParseDecimal(field.raw)
		if parseErr != nil {
			return false, parseErr
		}
		*field.destination = value
	}
	parsedRunID, err := instruments.ParseUUID(importRunID)
	if err != nil {
		return false, err
	}
	existing.ImportRunID = parsedRunID
	if adjusted != nil {
		value, parseErr := ParseDecimal(*adjusted)
		if parseErr != nil {
			return false, parseErr
		}
		existing.AdjustedClose = &value
	}
	if existing.SourceHash == candidate.SourceHash {
		return false, nil
	}
	var revision int
	if err := s.tx.QueryRow(ctx, `SELECT coalesce(max(revision),0)+1 FROM price_bar_revisions
		WHERE instrument_id=$1 AND session_date=$2`, input.Target.InstrumentID.String(), candidate.SessionDate.String()).Scan(&revision); err != nil {
		return false, err
	}
	revisionID, err := instruments.NewUUID()
	if err != nil {
		return false, err
	}
	if _, err := s.tx.Exec(ctx, `INSERT INTO price_bar_revisions
		(id,instrument_id,session_date,revision,open,high,low,close,adjusted_close,volume,currency,
		provider,source_hash,import_run_id,first_observed_at,last_observed_at,superseding_run_id,superseded_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
		revisionID.String(), input.Target.InstrumentID.String(), candidate.SessionDate.String(), revision,
		existing.Open.String(), existing.High.String(), existing.Low.String(), existing.Close.String(),
		decimalValue(existing.AdjustedClose), existing.Volume, existing.Currency, existing.Provider,
		existing.SourceHash, existing.ImportRunID.String(), existing.FirstObservedAt, existing.LastObservedAt,
		input.RunID.String(), input.ObservedAt); err != nil {
		return false, fmt.Errorf("archive corrected daily bar: %w", err)
	}
	_, err = s.tx.Exec(ctx, `UPDATE daily_price_bars SET open=$3,high=$4,low=$5,close=$6,
		adjusted_close=$7,volume=$8,currency=$9,provider=$10,source_hash=$11,import_run_id=$12,
		last_observed_at=$13 WHERE instrument_id=$1 AND session_date=$2`,
		input.Target.InstrumentID.String(), candidate.SessionDate.String(), candidate.Open.String(), candidate.High.String(),
		candidate.Low.String(), candidate.Close.String(), decimalValue(candidate.AdjustedClose), candidate.Volume,
		input.Target.Currency, input.Provider, candidate.SourceHash, input.RunID.String(), input.ObservedAt)
	return err == nil, err
}

func (s *ImportScope) upsertAction(ctx context.Context, input persistInput, action ProviderAction) (bool, error) {
	var sourceHash string
	err := s.tx.QueryRow(ctx, `SELECT source_hash FROM corporate_actions
		WHERE provider=$1 AND provider_action_id=$2 FOR UPDATE`, input.Provider, action.ProviderActionID).Scan(&sourceHash)
	if err == nil {
		if sourceHash != action.SourceHash {
			return false, errors.New("provider returned a conflicting corporate action correction")
		}
		return false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	id, err := instruments.NewUUID()
	if err != nil {
		return false, err
	}
	_, err = s.tx.Exec(ctx, `INSERT INTO corporate_actions
		(id,instrument_id,provider,provider_action_id,action_type,ex_date,effective_date,ratio,amount,currency,
		old_symbol,new_symbol,source_hash,import_run_id,first_observed_at,last_observed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NULLIF($10,''),NULLIF($11,''),NULLIF($12,''),$13,$14,$15,$15)`,
		id.String(), input.Target.InstrumentID.String(), input.Provider, action.ProviderActionID, action.Type,
		action.ExDate.String(), sessionValue(action.EffectiveDate), decimalValue(action.Ratio), decimalValue(action.Amount),
		action.Currency, action.OldSymbol, action.NewSymbol, action.SourceHash, input.RunID.String(), input.ObservedAt)
	return err == nil, err
}

func (s *ImportScope) insertFinding(ctx context.Context, input persistInput, issue ValidationIssue) (instruments.UUID, error) {
	id, err := instruments.NewUUID()
	if err != nil {
		return "", err
	}
	_, err = s.tx.Exec(ctx, `INSERT INTO data_quality_findings
		(id,instrument_id,session_date,run_id,rule,severity,disposition,detail,status,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'open',$9)`, id.String(), input.Target.InstrumentID.String(),
		issue.SessionDate.String(), input.RunID.String(), issue.Rule, issue.Severity, issue.Disposition,
		issue.Detail, input.ObservedAt)
	return id, err
}

func (r *Repository) finishRun(ctx context.Context, runID instruments.UUID, finishedAt time.Time) (ImportRun, error) {
	var total, succeeded, partial, failed, cancelled int
	var counts ImportCounts
	if err := r.pool.QueryRow(ctx, `SELECT count(*),
		count(*) FILTER (WHERE status='succeeded'), count(*) FILTER (WHERE status='partial'),
		count(*) FILTER (WHERE status='failed'), count(*) FILTER (WHERE status='cancelled'),
		coalesce(sum(processed_count),0),coalesce(sum(accepted_count),0),
		coalesce(sum(rejected_count),0),coalesce(sum(flagged_count),0)
		FROM import_items WHERE run_id=$1`, runID.String()).Scan(&total, &succeeded, &partial, &failed, &cancelled,
		&counts.Processed, &counts.Accepted, &counts.Rejected, &counts.Flagged); err != nil {
		return ImportRun{}, fmt.Errorf("summarize import run: %w", err)
	}
	status := ImportSucceeded
	switch {
	case cancelled == total && total > 0:
		status = ImportCancelled
	case failed == total && total > 0:
		status = ImportFailed
	case partial > 0 || failed > 0 || cancelled > 0 || succeeded < total:
		status = ImportPartial
	}
	var errorCode, errorSummary *string
	if status == ImportFailed || status == ImportPartial {
		_ = r.pool.QueryRow(ctx, `SELECT error_code,error_summary FROM import_items
			WHERE run_id=$1 AND error_code IS NOT NULL ORDER BY instrument_id LIMIT 1`, runID.String()).Scan(&errorCode, &errorSummary)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ImportRun{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE import_runs SET status=$2,finished_at=$3,
		processed_count=$4,accepted_count=$5,rejected_count=$6,flagged_count=$7,
		error_code=$8,error_summary=$9 WHERE id=$1`,
		runID.String(), status, finishedAt, counts.Processed, counts.Accepted, counts.Rejected, counts.Flagged,
		errorCode, errorSummary); err != nil {
		return ImportRun{}, fmt.Errorf("finish import run: %w", err)
	}
	if err := emitEvent(ctx, tx, "import_run", runID.String(), map[string]any{
		"status": status, "processed": counts.Processed, "accepted": counts.Accepted,
		"rejected": counts.Rejected, "flagged": counts.Flagged,
	}, finishedAt); err != nil {
		return ImportRun{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ImportRun{}, err
	}
	return r.getRun(ctx, runID)
}

func (r *Repository) retryTargets(ctx context.Context, parentRunID instruments.UUID) (string, []ImportTarget, error) {
	var provider string
	if err := r.pool.QueryRow(ctx, `SELECT provider FROM import_runs
		WHERE id=$1 AND status IN ('partial','failed','cancelled')`, parentRunID.String()).Scan(&provider); err != nil {
		return "", nil, fmt.Errorf("load retry parent: %w", err)
	}
	rows, err := r.pool.Query(ctx, `SELECT item.instrument_id::text,p.provider_symbol,i.currency,
		item.requested_from::text,item.requested_to::text
		FROM import_items item
		JOIN instruments i ON i.id=item.instrument_id
		JOIN provider_instruments p ON p.instrument_id=i.id AND p.provider=$2 AND p.active
		WHERE item.run_id=$1 AND item.status IN ('failed','cancelled')
		ORDER BY item.instrument_id`, parentRunID.String(), provider)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	targets := make([]ImportTarget, 0)
	for rows.Next() {
		var rawID, from, to string
		var target ImportTarget
		if err := rows.Scan(&rawID, &target.ProviderSymbol, &target.Currency, &from, &to); err != nil {
			return "", nil, err
		}
		var parseErr error
		if target.InstrumentID, parseErr = instruments.ParseUUID(rawID); parseErr != nil {
			return "", nil, parseErr
		}
		if target.From, parseErr = ParseSessionDate(from); parseErr != nil {
			return "", nil, parseErr
		}
		if target.To, parseErr = ParseSessionDate(to); parseErr != nil {
			return "", nil, parseErr
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return "", nil, err
	}
	if len(targets) == 0 {
		return "", nil, errors.New("retry parent has no failed scopes")
	}
	return provider, targets, nil
}

func (r *Repository) getRun(ctx context.Context, runID instruments.UUID) (ImportRun, error) {
	var run ImportRun
	var id string
	var from, to, parent, errorCode, errorSummary *string
	if err := r.pool.QueryRow(ctx, `SELECT id::text,kind,provider,requested_from::text,requested_to::text,
		status,parent_run_id::text,started_at,finished_at,processed_count,accepted_count,rejected_count,
		flagged_count,error_code,error_summary,app_version FROM import_runs WHERE id=$1`, runID.String()).Scan(
		&id, &run.Kind, &run.Provider, &from, &to, &run.Status, &parent, &run.StartedAt, &run.FinishedAt,
		&run.Counts.Processed, &run.Counts.Accepted, &run.Counts.Rejected, &run.Counts.Flagged,
		&errorCode, &errorSummary, &run.AppVersion); err != nil {
		return ImportRun{}, err
	}
	var err error
	if run.ID, err = instruments.ParseUUID(id); err != nil {
		return ImportRun{}, err
	}
	if from != nil {
		value, parseErr := ParseSessionDate(*from)
		if parseErr != nil {
			return ImportRun{}, parseErr
		}
		run.RequestedFrom = &value
	}
	if to != nil {
		value, parseErr := ParseSessionDate(*to)
		if parseErr != nil {
			return ImportRun{}, parseErr
		}
		run.RequestedTo = &value
	}
	if parent != nil {
		value, parseErr := instruments.ParseUUID(*parent)
		if parseErr != nil {
			return ImportRun{}, parseErr
		}
		run.ParentRunID = &value
	}
	if errorCode != nil {
		run.Error = &SafeError{Code: *errorCode, Summary: valueOrEmpty(errorSummary)}
	}
	return run, nil
}

func (r *Repository) ListImportRuns(ctx context.Context, status ImportStatus, limit int) ([]ImportRun, error) {
	rows, err := r.pool.Query(ctx, `SELECT id::text FROM import_runs
		WHERE ($1='' OR status=$1) ORDER BY started_at DESC,id DESC LIMIT $2`, status, limit)
	if err != nil {
		return nil, fmt.Errorf("list import runs: %w", err)
	}
	defer rows.Close()
	ids := make([]instruments.UUID, 0)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		id, err := instruments.ParseUUID(raw)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	runs := make([]ImportRun, 0, len(ids))
	for _, id := range ids {
		run, err := r.getRun(ctx, id)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, nil
}

func (r *Repository) GetImportRun(ctx context.Context, runID instruments.UUID) (ImportRun, []ImportItem, error) {
	run, err := r.getRun(ctx, runID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ImportRun{}, nil, ErrNotFound
	}
	if err != nil {
		return ImportRun{}, nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT instrument_id::text,requested_from::text,requested_to::text,
		status,processed_count,accepted_count,rejected_count,flagged_count,attempts,started_at,finished_at,
		error_code,error_summary FROM import_items WHERE run_id=$1 ORDER BY instrument_id`, runID.String())
	if err != nil {
		return ImportRun{}, nil, err
	}
	defer rows.Close()
	items := make([]ImportItem, 0)
	for rows.Next() {
		var item ImportItem
		var rawID, from, to string
		var errorCode, errorSummary *string
		if err := rows.Scan(&rawID, &from, &to, &item.Status, &item.Counts.Processed, &item.Counts.Accepted,
			&item.Counts.Rejected, &item.Counts.Flagged, &item.Attempts, &item.StartedAt, &item.FinishedAt,
			&errorCode, &errorSummary); err != nil {
			return ImportRun{}, nil, err
		}
		item.RunID = runID
		if item.InstrumentID, err = instruments.ParseUUID(rawID); err != nil {
			return ImportRun{}, nil, err
		}
		if item.RequestedFrom, err = ParseSessionDate(from); err != nil {
			return ImportRun{}, nil, err
		}
		if item.RequestedTo, err = ParseSessionDate(to); err != nil {
			return ImportRun{}, nil, err
		}
		if errorCode != nil {
			item.Error = &SafeError{Code: *errorCode, Summary: valueOrEmpty(errorSummary)}
		}
		items = append(items, item)
	}
	return run, items, rows.Err()
}

func (r *Repository) ListQualityFindings(ctx context.Context, filter FindingFilter) ([]QualityFinding, error) {
	instrumentID := ""
	if filter.InstrumentID != nil {
		instrumentID = filter.InstrumentID.String()
	}
	rows, err := r.pool.Query(ctx, `SELECT id::text,instrument_id::text,session_date::text,run_id::text,
		rule,severity,disposition,detail,status,created_at,resolved_at,resolving_run_id::text
		FROM data_quality_findings
		WHERE ($1='' OR instrument_id=NULLIF($1,'')::uuid) AND ($2='' OR status=$2) AND ($3='' OR severity=$3)
		ORDER BY created_at DESC,id DESC LIMIT $4`, instrumentID, filter.Status, filter.Severity, filter.Limit)
	if err != nil {
		return nil, fmt.Errorf("list quality findings: %w", err)
	}
	defer rows.Close()
	findings := make([]QualityFinding, 0)
	for rows.Next() {
		var finding QualityFinding
		var rawID, rawInstrumentID, rawRunID string
		var rawSession, rawResolvingRunID *string
		if err := rows.Scan(&rawID, &rawInstrumentID, &rawSession, &rawRunID, &finding.Rule,
			&finding.Severity, &finding.Disposition, &finding.Detail, &finding.Status,
			&finding.CreatedAt, &finding.ResolvedAt, &rawResolvingRunID); err != nil {
			return nil, err
		}
		var parseErr error
		if finding.ID, parseErr = instruments.ParseUUID(rawID); parseErr != nil {
			return nil, parseErr
		}
		if finding.InstrumentID, parseErr = instruments.ParseUUID(rawInstrumentID); parseErr != nil {
			return nil, parseErr
		}
		if finding.RunID, parseErr = instruments.ParseUUID(rawRunID); parseErr != nil {
			return nil, parseErr
		}
		if rawSession != nil {
			value, parseErr := ParseSessionDate(*rawSession)
			if parseErr != nil {
				return nil, parseErr
			}
			finding.SessionDate = &value
		}
		if rawResolvingRunID != nil {
			value, parseErr := instruments.ParseUUID(*rawResolvingRunID)
			if parseErr != nil {
				return nil, parseErr
			}
			finding.ResolvingRunID = &value
		}
		findings = append(findings, finding)
	}
	return findings, rows.Err()
}

func decimalValue(value *Decimal) any {
	if value == nil {
		return nil
	}
	return value.String()
}

func sessionValue(value *SessionDate) any {
	if value == nil {
		return nil
	}
	return value.String()
}

func uuidValue(value *instruments.UUID) any {
	if value == nil {
		return nil
	}
	return value.String()
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func emitEvent(ctx context.Context, tx pgx.Tx, entityType, entityID string, payload any, occurredAt time.Time) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return clientevents.Insert(ctx, tx, clientevents.Event{
		Type: entityType + ".changed.v1", Version: 1, Scope: "shared",
		EntityType: entityType, EntityID: entityID, Payload: encoded, OccurredAt: occurredAt,
	})
}

func importItemEntityID(runID, instrumentID instruments.UUID) string {
	return runID.String() + ":" + instrumentID.String()
}

func flaggedObservationCount(validation ValidationResult, processed int64) int64 {
	flagged := make(map[SessionDate]struct{})
	for _, issue := range validation.Issues {
		if issue.Rule != "missing_session" {
			flagged[issue.SessionDate] = struct{}{}
		}
	}
	count := int64(len(flagged))
	if count > processed {
		return processed
	}
	return count
}
