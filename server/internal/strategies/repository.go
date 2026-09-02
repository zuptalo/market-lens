package strategies

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	clientevents "market-lens/server/internal/events"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a strategy version, an instrument or a session has no record.
var ErrNotFound = errors.New("not found")

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) ready() error {
	if r == nil || r.pool == nil {
		return errors.New("strategies repository is not configured")
	}
	return nil
}

// strategyDocument is the stored shape of a version's parameters. It is read, never written: a
// version is published by migration, which is what keeps "what produced this signal" answerable.
type strategyDocument struct {
	Factors []struct {
		Name        string `json:"name"`
		Feature     string `json:"feature"`
		Mode        string `json:"mode"`
		Weight      string `json:"weight"`
		Description string `json:"description"`
		Transform   *struct {
			Kind   string            `json:"kind"`
			Lower  string            `json:"lower"`
			Upper  string            `json:"upper"`
			Values map[string]string `json:"values"`
		} `json:"transform"`
	} `json:"factors"`
	ActionBands []struct {
		Lower  string `json:"lower"`
		Upper  string `json:"upper"`
		Action string `json:"action"`
	} `json:"action_bands"`
	Liquidity struct {
		MinimumStoredSessions int `json:"minimum_stored_sessions"`
	} `json:"liquidity"`
}

// Strategies returns published versions, newest first, optionally including superseded ones.
func (r *Repository) Strategies(ctx context.Context, name string, includeSuperseded bool) ([]Strategy, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT id::text, name, version, title, intent, caveat,
		parameters, published_at, superseded_at FROM strategies
		WHERE ($1 = '' OR name = $1) AND ($2 OR superseded_at IS NULL)
		ORDER BY name, version DESC`, name, includeSuperseded)
	if err != nil {
		return nil, fmt.Errorf("list strategies: %w", err)
	}
	defer rows.Close()
	var published []Strategy
	for rows.Next() {
		strategy, err := scanStrategy(rows)
		if err != nil {
			return nil, err
		}
		published = append(published, strategy)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list strategies: %w", err)
	}
	return published, nil
}

// Strategy resolves one version: the current one for a name, or a specific version.
func (r *Repository) Strategy(ctx context.Context, name string, version int) (Strategy, error) {
	if err := r.ready(); err != nil {
		return Strategy{}, err
	}
	query := `SELECT id::text, name, version, title, intent, caveat, parameters, published_at, superseded_at
		FROM strategies WHERE ($1 = '' OR name = $1) AND `
	args := []any{name}
	if version > 0 {
		query += `version = $2`
		args = append(args, version)
	} else {
		query += `superseded_at IS NULL`
	}
	query += ` ORDER BY name LIMIT 1`
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return Strategy{}, fmt.Errorf("read strategy: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return Strategy{}, fmt.Errorf("%w: strategy %q version %d", ErrNotFound, name, version)
	}
	return scanStrategy(rows)
}

func scanStrategy(rows pgx.Rows) (Strategy, error) {
	var strategy Strategy
	var raw []byte
	if err := rows.Scan((*string)(&strategy.ID), &strategy.Name, &strategy.Version, &strategy.Title,
		&strategy.Intent, &strategy.Caveat, &raw, &strategy.PublishedAt, &strategy.SupersededAt); err != nil {
		return Strategy{}, fmt.Errorf("scan strategy: %w", err)
	}
	var document strategyDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return Strategy{}, fmt.Errorf("read strategy %s v%d parameters: %w", strategy.Name, strategy.Version, err)
	}
	strategy.Transforms = map[string]Transform{}
	for _, factor := range document.Factors {
		weight, err := parseDecimal(factor.Weight)
		if err != nil {
			return Strategy{}, fmt.Errorf("factor %q weight: %w", factor.Name, err)
		}
		strategy.Factors = append(strategy.Factors, Factor{
			Name: factor.Name, Feature: factor.Feature, Mode: Mode(factor.Mode),
			Weight: weight, Description: factor.Description,
		})
		if factor.Transform != nil {
			transform := Transform{Kind: TransformKind(factor.Transform.Kind), Values: map[string]float64{}}
			if factor.Transform.Lower != "" {
				if transform.Lower, err = parseDecimal(factor.Transform.Lower); err != nil {
					return Strategy{}, fmt.Errorf("factor %q lower bound: %w", factor.Name, err)
				}
			}
			if factor.Transform.Upper != "" {
				if transform.Upper, err = parseDecimal(factor.Transform.Upper); err != nil {
					return Strategy{}, fmt.Errorf("factor %q upper bound: %w", factor.Name, err)
				}
			}
			for label, value := range factor.Transform.Values {
				mapped, err := parseDecimal(value)
				if err != nil {
					return Strategy{}, fmt.Errorf("factor %q label %q: %w", factor.Name, label, err)
				}
				transform.Values[label] = mapped
			}
			strategy.Transforms[factor.Name] = transform
		}
	}
	for _, band := range document.ActionBands {
		lower, err := parseDecimal(band.Lower)
		if err != nil {
			return Strategy{}, fmt.Errorf("action band lower bound: %w", err)
		}
		upper, err := parseDecimal(band.Upper)
		if err != nil {
			return Strategy{}, fmt.Errorf("action band upper bound: %w", err)
		}
		strategy.ActionBands = append(strategy.ActionBands, ActionBand{Lower: lower, Upper: upper, Action: band.Action})
	}
	strategy.MinSessions = document.Liquidity.MinimumStoredSessions
	return strategy, nil
}

// CreateRun records a computation as running.
func (r *Repository) CreateRun(ctx context.Context, run Run) error {
	if err := r.ready(); err != nil {
		return err
	}
	var trigger *string
	if run.TriggerFeatureRunID != nil {
		id := run.TriggerFeatureRunID.String()
		trigger = &id
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO strategy_runs
		(id, strategy_id, kind, status, universe_id, trigger_feature_run_id, started_at, app_version)
		VALUES ($1,$2,$3,'running',$4,$5,$6,$7)`,
		run.ID.String(), run.StrategyID.String(), string(run.Kind), run.UniverseID.String(),
		trigger, run.StartedAt, run.AppVersion)
	if err != nil {
		return fmt.Errorf("create strategy run: %w", err)
	}
	return nil
}

// FinishRun records the outcome.
func (r *Repository) FinishRun(ctx context.Context, id UUID, status RunStatus, instruments, signals int64, at time.Time) error {
	if err := r.ready(); err != nil {
		return err
	}
	_, err := r.pool.Exec(ctx, `UPDATE strategy_runs
		SET status = $2, instrument_count = $3, signal_count = $4, finished_at = $5 WHERE id = $1`,
		id.String(), string(status), instruments, signals, at)
	if err != nil {
		return fmt.Errorf("finish strategy run: %w", err)
	}
	return nil
}

// WriteItem records one instrument's outcome outside a signal write — a skip or an early
// failure, where there are no signals to commit alongside it.
func (r *Repository) WriteItem(ctx context.Context, item RunItem) error {
	if err := r.ready(); err != nil {
		return err
	}
	return writeRunItem(ctx, r.pool, item)
}

// execer is what the pool and a transaction have in common.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// ListRuns returns recent computations, newest first, with the count of failed instruments.
func (r *Repository) ListRuns(ctx context.Context, limit int) ([]Run, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 50 {
		return nil, fmt.Errorf("strategy run limit must be between 1 and 50")
	}
	rows, err := r.pool.Query(ctx, `SELECT r.id::text, r.strategy_id::text, r.kind, r.status,
		r.universe_id::text, r.trigger_feature_run_id::text, r.started_at, r.finished_at,
		r.instrument_count, r.signal_count, r.app_version,
		(SELECT count(*) FROM strategy_run_items i WHERE i.run_id = r.id AND i.status = 'failed')
		FROM strategy_runs r ORDER BY r.started_at DESC, r.id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list strategy runs: %w", err)
	}
	defer rows.Close()
	runs := make([]Run, 0, limit)
	for rows.Next() {
		var run Run
		var trigger *string
		if err := rows.Scan((*string)(&run.ID), (*string)(&run.StrategyID), (*string)(&run.Kind),
			(*string)(&run.Status), (*string)(&run.UniverseID), &trigger, &run.StartedAt,
			&run.FinishedAt, &run.InstrumentCount, &run.SignalCount, &run.AppVersion,
			&run.FailedCount); err != nil {
			return nil, fmt.Errorf("scan strategy run: %w", err)
		}
		if trigger != nil {
			id := UUID(*trigger)
			run.TriggerFeatureRunID = &id
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list strategy runs: %w", err)
	}
	return runs, nil
}

// WriteSignals replaces one instrument's signals over a session range and publishes the change,
// in one transaction. Coupling them is what stops a reconnecting client missing a committed
// change: the event cannot exist without the signals, or the signals without the event.
func (r *Repository) WriteSignals(ctx context.Context, run Run, item RunItem, signals []Signal, change Change) error {
	if err := r.ready(); err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin signal write: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('signals:' || $1::text, 0))`,
		item.InstrumentID.String()); err != nil {
		return fmt.Errorf("acquire signal scope: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM signals WHERE instrument_id = $1 AND strategy_id = $2
		AND session_date BETWEEN $3::date AND $4::date`,
		item.InstrumentID.String(), run.StrategyID.String(), change.FromSession.String(), change.ToSession.String()); err != nil {
		return fmt.Errorf("clear signals: %w", err)
	}

	for _, signal := range signals {
		contributions, err := json.Marshal(contributionDocuments(signal.Contributions))
		if err != nil {
			return fmt.Errorf("encode contributions: %w", err)
		}
		var action *string
		if signal.Action != nil {
			value := string(*signal.Action)
			action = &value
		}
		var absence *string
		if signal.AbsenceReason != nil {
			value := string(*signal.AbsenceReason)
			absence = &value
		}
		if _, err := tx.Exec(ctx, `INSERT INTO signals
			(instrument_id, session_date, strategy_id, score, action, confidence, absence_reason,
			 contributions, divisor, computed_at, run_id)
			VALUES ($1,$2::date,$3,$4::numeric,$5,$6::numeric,$7,$8::jsonb,$9::numeric,$10,$11)`,
			signal.InstrumentID.String(), signal.SessionDate.String(), signal.StrategyID.String(),
			signal.Score, action, signal.Confidence, absence, contributions, signal.Divisor,
			signal.ComputedAt, signal.RunID.String()); err != nil {
			return fmt.Errorf("write signal: %w", err)
		}
	}

	if err := writeRunItem(ctx, tx, item); err != nil {
		return err
	}

	payload, err := json.Marshal(map[string]string{
		"instrument_id": change.InstrumentID.String(),
		"from_session":  change.FromSession.String(),
		"to_session":    change.ToSession.String(),
		"run_id":        change.RunID.String(),
		"strategy_id":   change.StrategyID.String(),
	})
	if err != nil {
		return fmt.Errorf("encode signal change: %w", err)
	}
	if err := clientevents.Insert(ctx, tx, clientevents.Event{
		Type: EventSignalsChanged, Version: 1, Scope: "shared", EntityType: "instrument",
		EntityID: change.InstrumentID.String(), Payload: payload, OccurredAt: time.Now().UTC(),
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func writeRunItem(ctx context.Context, db execer, item RunItem) error {
	var from, to *string
	if item.FromSession != nil {
		value := item.FromSession.String()
		from = &value
	}
	if item.ToSession != nil {
		value := item.ToSession.String()
		to = &value
	}
	if _, err := db.Exec(ctx, `INSERT INTO strategy_run_items
		(run_id, instrument_id, status, from_session, to_session, signal_count, error_code,
		 error_summary, started_at, finished_at)
		VALUES ($1,$2,$3,$4::date,$5::date,$6,$7,$8,$9,$10)
		ON CONFLICT (run_id, instrument_id) DO UPDATE SET status = excluded.status,
		  from_session = excluded.from_session, to_session = excluded.to_session,
		  signal_count = excluded.signal_count, error_code = excluded.error_code,
		  error_summary = excluded.error_summary, finished_at = excluded.finished_at`,
		item.RunID.String(), item.InstrumentID.String(), string(item.Status), from, to,
		item.SignalCount, item.ErrorCode, item.ErrorSummary, item.StartedAt, item.FinishedAt); err != nil {
		return fmt.Errorf("write strategy run item: %w", err)
	}
	return nil
}

func contributionDocuments(contributions []Contribution) []map[string]any {
	documents := make([]map[string]any, 0, len(contributions))
	for _, contribution := range contributions {
		document := map[string]any{
			"factor":  contribution.Factor,
			"feature": contribution.Feature,
			"weight":  Round(contribution.Weight),
		}
		if contribution.FeatureValue != nil {
			document["feature_value"] = *contribution.FeatureValue
		}
		if contribution.FeatureSession != "" {
			document["feature_session"] = contribution.FeatureSession
		}
		if contribution.FactorScore != nil {
			document["factor_score"] = Round(*contribution.FactorScore)
		}
		if contribution.Contribution != nil {
			document["contribution"] = Round(*contribution.Contribution)
		}
		if contribution.UnavailableReason != "" {
			document["unavailable_reason"] = contribution.UnavailableReason
		}
		documents = append(documents, document)
	}
	return documents
}

// parseDecimal reads a stored decimal string. Parameters are stored as strings rather than JSON
// numbers so a weight is exactly what was published, not what a float round-trip made of it.
func parseDecimal(text string) (float64, error) {
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a decimal", text)
	}
	return value, nil
}

// SignalView is one instrument's signal together with the version that produced it. The version
// travels with the signal rather than being looked up beside it, because a caveat that a caller
// can forget to fetch is a caveat that will eventually not be shown.
type SignalView struct {
	Signal   Signal
	Strategy Strategy
}

// ErrNoSignal distinguishes "this instrument and session have no signal" from a read failure.
var ErrNoSignal = errors.New("no signal")

// SignalAsOf reads one instrument's signal as of a session under one strategy version — the
// current version when none is named, the latest session on or before asOf when none is given.
func (r *Repository) SignalAsOf(ctx context.Context, instrumentID UUID, asOf SessionDate, name string, version int) (SignalView, error) {
	if err := r.ready(); err != nil {
		return SignalView{}, err
	}
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM instruments WHERE id = $1)`,
		instrumentID.String()).Scan(&exists); err != nil {
		return SignalView{}, fmt.Errorf("read instrument: %w", err)
	}
	if !exists {
		return SignalView{}, fmt.Errorf("%w: instrument %s", ErrNotFound, instrumentID)
	}
	strategy, err := r.Strategy(ctx, name, version)
	if err != nil {
		return SignalView{}, err
	}

	// The latest session on or before the one asked for: a weekend, a holiday or a session the
	// instrument had no bar for is answered with the most recent view rather than with nothing.
	query := `SELECT instrument_id::text, session_date::text, score::text, action, confidence::text,
		absence_reason, contributions, divisor::text, computed_at, run_id::text
		FROM signals WHERE instrument_id = $1 AND strategy_id = $2`
	args := []any{instrumentID.String(), strategy.ID.String()}
	if asOf != "" {
		query += ` AND session_date <= $3::date`
		args = append(args, asOf.String())
	}
	query += ` ORDER BY session_date DESC LIMIT 1`

	view := SignalView{Strategy: strategy}
	var raw []byte
	var action, absence *string
	if err := r.pool.QueryRow(ctx, query, args...).Scan(
		(*string)(&view.Signal.InstrumentID), (*string)(&view.Signal.SessionDate), &view.Signal.Score,
		&action, &view.Signal.Confidence, &absence, &raw, &view.Signal.Divisor,
		&view.Signal.ComputedAt, (*string)(&view.Signal.RunID)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SignalView{}, fmt.Errorf("%w for instrument %s as of %s", ErrNoSignal, instrumentID, asOf)
		}
		return SignalView{}, fmt.Errorf("read signal: %w", err)
	}
	if action != nil {
		chosen := Action(*action)
		view.Signal.Action = &chosen
	}
	if absence != nil {
		reason := AbsenceReason(*absence)
		view.Signal.AbsenceReason = &reason
	}
	view.Signal.StrategyID = strategy.ID
	if view.Signal.Contributions, err = decodeContributions(raw); err != nil {
		return SignalView{}, err
	}
	return view, nil
}

// decodeContributions reads a stored explanation back. Decimals stay strings on the way out for
// display, and become numbers only where arithmetic is actually done.
func decodeContributions(raw []byte) ([]Contribution, error) {
	var documents []struct {
		Factor            string  `json:"factor"`
		Feature           string  `json:"feature"`
		FeatureValue      *string `json:"feature_value"`
		FeatureSession    string  `json:"feature_session"`
		FactorScore       *string `json:"factor_score"`
		Weight            string  `json:"weight"`
		Contribution      *string `json:"contribution"`
		UnavailableReason string  `json:"unavailable_reason"`
	}
	if err := json.Unmarshal(raw, &documents); err != nil {
		return nil, fmt.Errorf("read contributions: %w", err)
	}
	contributions := make([]Contribution, 0, len(documents))
	for _, document := range documents {
		contribution := Contribution{
			Factor: document.Factor, Feature: document.Feature, FeatureValue: document.FeatureValue,
			FeatureSession: document.FeatureSession, UnavailableReason: document.UnavailableReason,
		}
		weight, err := parseDecimal(document.Weight)
		if err != nil {
			return nil, fmt.Errorf("contribution %q weight: %w", document.Factor, err)
		}
		contribution.Weight = weight
		if document.FactorScore != nil {
			score, err := parseDecimal(*document.FactorScore)
			if err != nil {
				return nil, fmt.Errorf("contribution %q factor score: %w", document.Factor, err)
			}
			contribution.FactorScore = &score
		}
		if document.Contribution != nil {
			value, err := parseDecimal(*document.Contribution)
			if err != nil {
				return nil, fmt.Errorf("contribution %q: %w", document.Factor, err)
			}
			contribution.Contribution = &value
		}
		contributions = append(contributions, contribution)
	}
	return contributions, nil
}

// RankedSignal is one row of the universe in a strategy's order. An unscored instrument appears
// in the same list with no rank rather than in a footnote: it is part of the universe, and the
// reason it has no view is information, not an omission to be tidied away.
type RankedSignal struct {
	Signal Signal
	Ticker string
	Name   string
	Rank   *int64
}

// RankingRequest asks for one page of one session's ranking.
type RankingRequest struct {
	Strategy string
	Version  int
	AsOf     SessionDate
	Cursor   string
	Limit    int
}

// RankingPage is that page, with the version and session it is of.
type RankingPage struct {
	Strategy    Strategy
	SessionDate SessionDate
	Items       []RankedSignal
	NextCursor  string
	// Total is the size of the whole ranking, counted only on a cursor-less request: counting
	// on every page would defeat the early termination keyset paging exists for.
	Total    *int64
	Scored   int64
	Unscored int64
}

// Ranking reads one page of the universe in a strategy's order for one session.
//
// Scored instruments come first in descending score; instruments the strategy could not score
// follow, alphabetically, each carrying its reason. The two groups share one ordering so the
// reader sees the whole universe, and are separated within it so an absence is never mistaken
// for a weak score. Paging is keyset over (group, score descending, instrument), which is a
// total order and therefore stable when scores tie.
func (r *Repository) Ranking(ctx context.Context, request RankingRequest) (RankingPage, error) {
	if err := r.ready(); err != nil {
		return RankingPage{}, err
	}
	limit := request.Limit
	if limit < 1 || limit > 200 {
		limit = 50
	}
	strategy, err := r.Strategy(ctx, request.Strategy, request.Version)
	if err != nil {
		return RankingPage{}, err
	}

	session := request.AsOf
	if session == "" {
		var latest *string
		if err := r.pool.QueryRow(ctx, `SELECT max(session_date)::text FROM signals WHERE strategy_id = $1`,
			strategy.ID.String()).Scan(&latest); err != nil {
			return RankingPage{}, fmt.Errorf("read the latest scored session: %w", err)
		}
		if latest == nil {
			return RankingPage{Strategy: strategy}, nil
		}
		session = SessionDate(*latest)
	} else {
		// The latest session on or before the one asked for, so a weekend answers with the
		// most recent ranking rather than with an empty one.
		var resolved *string
		if err := r.pool.QueryRow(ctx, `SELECT max(session_date)::text FROM signals
			WHERE strategy_id = $1 AND session_date <= $2::date`,
			strategy.ID.String(), session.String()).Scan(&resolved); err != nil {
			return RankingPage{}, fmt.Errorf("resolve the ranking session: %w", err)
		}
		if resolved == nil {
			return RankingPage{Strategy: strategy, SessionDate: session}, nil
		}
		session = SessionDate(*resolved)
	}

	page := RankingPage{Strategy: strategy, SessionDate: session}
	if err := r.pool.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE score IS NOT NULL), count(*) FILTER (WHERE score IS NULL)
		FROM signals WHERE strategy_id = $1 AND session_date = $2::date`,
		strategy.ID.String(), session.String()).Scan(&page.Scored, &page.Unscored); err != nil {
		return RankingPage{}, fmt.Errorf("count the ranking: %w", err)
	}

	after, err := decodeRankingCursor(request.Cursor)
	if err != nil {
		return RankingPage{}, err
	}
	if after == nil {
		total := page.Scored + page.Unscored
		page.Total = &total
	}

	// group 0 is scored, 1 is unscored; ordering by (group, -score, instrument) is total.
	query := `SELECT s.instrument_id::text, i.ticker, i.name, s.score::text, s.action,
		s.confidence::text, s.absence_reason, s.contributions, s.divisor::text, s.computed_at,
		s.run_id::text, CASE WHEN s.score IS NULL THEN 1 ELSE 0 END AS grp
		FROM signals s JOIN instruments i ON i.id = s.instrument_id
		WHERE s.strategy_id = $1 AND s.session_date = $2::date`
	args := []any{strategy.ID.String(), session.String()}
	if after != nil {
		query += ` AND (CASE WHEN s.score IS NULL THEN 1 ELSE 0 END, -coalesce(s.score, 0), s.instrument_id::text)
			> ($3::int, $4::numeric, $5)`
		args = append(args, after.Group, after.NegatedScore, after.Instrument)
	}
	query += ` ORDER BY grp, -coalesce(s.score, 0), s.instrument_id::text LIMIT $` +
		strconv.Itoa(len(args)+1)
	args = append(args, limit+1)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return RankingPage{}, fmt.Errorf("read the ranking: %w", err)
	}
	defer rows.Close()
	rank := int64(0)
	if after != nil {
		rank = after.Rank
	}
	var last *rankingCursor
	for rows.Next() {
		var item RankedSignal
		var raw []byte
		var action, absence *string
		var group int
		if err := rows.Scan((*string)(&item.Signal.InstrumentID), &item.Ticker, &item.Name,
			&item.Signal.Score, &action, &item.Signal.Confidence, &absence, &raw,
			&item.Signal.Divisor, &item.Signal.ComputedAt, (*string)(&item.Signal.RunID), &group); err != nil {
			return RankingPage{}, fmt.Errorf("scan a ranked signal: %w", err)
		}
		item.Signal.SessionDate, item.Signal.StrategyID = session, strategy.ID
		if action != nil {
			chosen := Action(*action)
			item.Signal.Action = &chosen
		}
		if absence != nil {
			reason := AbsenceReason(*absence)
			item.Signal.AbsenceReason = &reason
		}
		if item.Signal.Contributions, err = decodeContributions(raw); err != nil {
			return RankingPage{}, err
		}
		negated := 0.0
		if item.Signal.Score != nil {
			rank++
			position := rank
			item.Rank = &position
			score, err := parseDecimal(*item.Signal.Score)
			if err != nil {
				return RankingPage{}, fmt.Errorf("stored score %q: %w", *item.Signal.Score, err)
			}
			negated = -score
		}
		last = &rankingCursor{Group: group, NegatedScore: negated,
			Instrument: item.Signal.InstrumentID.String(), Rank: rank}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return RankingPage{}, fmt.Errorf("read the ranking: %w", err)
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		trimmed := page.Items[limit-1]
		negated := 0.0
		group := 1
		rank := int64(0)
		if trimmed.Signal.Score != nil {
			score, _ := parseDecimal(*trimmed.Signal.Score)
			negated, group = -score, 0
		}
		if trimmed.Rank != nil {
			rank = *trimmed.Rank
		} else if after != nil {
			rank = after.Rank
		}
		last = &rankingCursor{Group: group, NegatedScore: negated,
			Instrument: trimmed.Signal.InstrumentID.String(), Rank: rank}
		page.NextCursor = encodeRankingCursor(*last)
	}
	return page, nil
}

// rankingCursor is the position keyset paging resumes from. The rank travels with it so a later
// page can continue numbering without counting the rows before it.
type rankingCursor struct {
	Group        int     `json:"g"`
	NegatedScore float64 `json:"s"`
	Instrument   string  `json:"i"`
	Rank         int64   `json:"r"`
}

func encodeRankingCursor(cursor rankingCursor) string {
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func decodeRankingCursor(text string) (*rankingCursor, error) {
	if text == "" {
		return nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil {
		return nil, fmt.Errorf("the ranking cursor is not readable")
	}
	var cursor rankingCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil {
		return nil, fmt.Errorf("the ranking cursor is not readable")
	}
	return &cursor, nil
}
