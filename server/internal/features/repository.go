package features

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	clientevents "market-lens/server/internal/events"
)

// EventFeatureValuesChanged is published in the transaction that commits one instrument's
// recomputed values (Constitution X; contracts/openapi.yaml x-market-lens-events).
const EventFeatureValuesChanged = "feature_values.changed.v1"

// Repository persists runs, values and composites, and reads them back as of a session.
type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) ready() error {
	if r == nil || r.pool == nil {
		return errors.New("features repository is required")
	}
	return nil
}

// Definitions returns every published definition, superseded ones included.
func (r *Repository) Definitions(ctx context.Context) ([]Definition, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT id::text, name, version, window_sessions, price_basis, parameters,
		undefined_conditions, session_length_sensitive, published_at, superseded_at
		FROM feature_definitions ORDER BY name, version`)
	if err != nil {
		return nil, fmt.Errorf("read feature definitions: %w", err)
	}
	defer rows.Close()
	var definitions []Definition
	for rows.Next() {
		var d Definition
		var parameters []byte
		if err := rows.Scan((*string)(&d.ID), &d.Name, &d.Version, &d.WindowSessions, (*string)(&d.PriceBasis),
			&parameters, &d.UndefinedConditions, &d.SessionLengthSensitive, &d.PublishedAt, &d.SupersededAt); err != nil {
			return nil, fmt.Errorf("scan feature definition: %w", err)
		}
		d.Parameters = map[string]any{}
		if len(parameters) > 0 {
			if err := json.Unmarshal(parameters, &d.Parameters); err != nil {
				return nil, fmt.Errorf("definition %s v%d parameters: %w", d.Name, d.Version, err)
			}
		}
		definitions = append(definitions, d)
	}
	return definitions, rows.Err()
}

// Instrument is one universe member as the engine needs it.
type Instrument struct {
	ID         UUID
	ExchangeID UUID
	Currency   string
}

// UniverseID resolves a universe code.
func (r *Repository) UniverseID(ctx context.Context, code string) (UUID, error) {
	if err := r.ready(); err != nil {
		return "", err
	}
	var id string
	err := r.pool.QueryRow(ctx, `SELECT id::text FROM research_universes WHERE code = $1`, code).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("universe %q does not exist", code)
	}
	if err != nil {
		return "", fmt.Errorf("resolve universe %q: %w", code, err)
	}
	return UUID(id), nil
}

// UniverseInstruments returns the current members of a universe in instrument-id order.
func (r *Repository) UniverseInstruments(ctx context.Context, universeID UUID) ([]Instrument, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT i.id::text, i.exchange_id::text, i.currency
		FROM universe_memberships m JOIN instruments i ON i.id = m.instrument_id
		WHERE m.universe_id = $1 AND m.included_to IS NULL
		ORDER BY i.id`, universeID.String())
	if err != nil {
		return nil, fmt.Errorf("read universe instruments: %w", err)
	}
	defer rows.Close()
	var members []Instrument
	for rows.Next() {
		var member Instrument
		if err := rows.Scan((*string)(&member.ID), (*string)(&member.ExchangeID), &member.Currency); err != nil {
			return nil, fmt.Errorf("scan universe instrument: %w", err)
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

// Calendar returns an exchange's sessions, ascending, closed days included.
func (r *Repository) Calendar(ctx context.Context, exchangeID UUID) ([]Session, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT session_date::text, status FROM exchange_sessions
		WHERE exchange_id = $1 ORDER BY session_date`, exchangeID.String())
	if err != nil {
		return nil, fmt.Errorf("read exchange calendar: %w", err)
	}
	defer rows.Close()
	var sessions []Session
	for rows.Next() {
		var session Session
		if err := rows.Scan((*string)(&session.Date), (*string)(&session.Status)); err != nil {
			return nil, fmt.Errorf("scan exchange session: %w", err)
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

// Bars returns an instrument's stored raw bars, ascending. Prices are parsed from their
// decimal text so the float64 is the correctly rounded one, whatever the driver would do.
func (r *Repository) Bars(ctx context.Context, instrumentID UUID) ([]Bar, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT session_date::text, open::text, high::text, low::text, close::text, volume
		FROM daily_price_bars WHERE instrument_id = $1 ORDER BY session_date`, instrumentID.String())
	if err != nil {
		return nil, fmt.Errorf("read bars: %w", err)
	}
	defer rows.Close()
	var bars []Bar
	for rows.Next() {
		var bar Bar
		var open, high, low, close string
		if err := rows.Scan((*string)(&bar.Session), &open, &high, &low, &close, &bar.Volume); err != nil {
			return nil, fmt.Errorf("scan bar: %w", err)
		}
		for _, field := range []struct {
			text string
			into *float64
		}{{open, &bar.Open}, {high, &bar.High}, {low, &bar.Low}, {close, &bar.Close}} {
			if *field.into, err = strconv.ParseFloat(field.text, 64); err != nil {
				return nil, fmt.Errorf("bar %s: %w", bar.Session, err)
			}
		}
		bars = append(bars, bar)
	}
	return bars, rows.Err()
}

// Splits returns an instrument's split and reverse-split actions with a ratio, by ex-date.
func (r *Repository) Splits(ctx context.Context, instrumentID UUID) ([]Split, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT ex_date::text, ratio::text FROM corporate_actions
		WHERE instrument_id = $1 AND action_type IN ('split', 'reverse_split') AND ratio IS NOT NULL
		ORDER BY ex_date`, instrumentID.String())
	if err != nil {
		return nil, fmt.Errorf("read splits: %w", err)
	}
	defer rows.Close()
	var splits []Split
	for rows.Next() {
		var split Split
		var ratio string
		if err := rows.Scan((*string)(&split.ExDate), &ratio); err != nil {
			return nil, fmt.Errorf("scan split: %w", err)
		}
		if split.Ratio, err = strconv.ParseFloat(ratio, 64); err != nil {
			return nil, fmt.Errorf("split at %s: %w", split.ExDate, err)
		}
		splits = append(splits, split)
	}
	return splits, rows.Err()
}

// CreateRun records a run in its running state.
func (r *Repository) CreateRun(ctx context.Context, run Run) error {
	if err := r.ready(); err != nil {
		return err
	}
	var trigger *string
	if run.TriggerRunID != nil {
		text := run.TriggerRunID.String()
		trigger = &text
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO feature_runs
		(id, kind, status, universe_id, definition_name, trigger_run_id, started_at, app_version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, run.ID.String(), string(run.Kind), string(run.Status),
		run.UniverseID.String(), run.DefinitionName, trigger, run.StartedAt, run.AppVersion)
	if err != nil {
		return fmt.Errorf("create feature run: %w", err)
	}
	return nil
}

// FinishRun closes a run with its outcome and counts.
func (r *Repository) FinishRun(ctx context.Context, id UUID, status RunStatus, instrumentCount, valueCount int64, finishedAt time.Time) error {
	if err := r.ready(); err != nil {
		return err
	}
	_, err := r.pool.Exec(ctx, `UPDATE feature_runs SET status = $2, instrument_count = $3, value_count = $4,
		finished_at = $5 WHERE id = $1`, id.String(), string(status), instrumentCount, valueCount, finishedAt)
	if err != nil {
		return fmt.Errorf("finish feature run: %w", err)
	}
	return nil
}

// Run reads one run back.
func (r *Repository) Run(ctx context.Context, id UUID) (Run, error) {
	if err := r.ready(); err != nil {
		return Run{}, err
	}
	var run Run
	var trigger *string
	err := r.pool.QueryRow(ctx, `SELECT id::text, kind, status, universe_id::text, definition_name, trigger_run_id::text,
		started_at, finished_at, instrument_count, value_count, app_version FROM feature_runs WHERE id = $1`,
		id.String()).Scan((*string)(&run.ID), (*string)(&run.Kind), (*string)(&run.Status), (*string)(&run.UniverseID),
		&run.DefinitionName, &trigger, &run.StartedAt, &run.FinishedAt, &run.InstrumentCount, &run.ValueCount, &run.AppVersion)
	if err != nil {
		return Run{}, fmt.Errorf("read feature run: %w", err)
	}
	if trigger != nil {
		triggerID := UUID(*trigger)
		run.TriggerRunID = &triggerID
	}
	return run, nil
}

// WriteItem records an instrument's outcome outside any scope — a failure that never
// reached a transaction, or an instrument that was skipped.
func (r *Repository) WriteItem(ctx context.Context, item RunItem) error {
	if err := r.ready(); err != nil {
		return err
	}
	return writeItem(ctx, r.pool, item)
}

// execer is what the pool and a transaction have in common.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func writeItem(ctx context.Context, db execer, item RunItem) error {
	_, err := db.Exec(ctx, `INSERT INTO feature_run_items
		(run_id, instrument_id, status, from_session, to_session, value_count, error_code, error_summary, started_at, finished_at)
		VALUES ($1, $2, $3, $4::date, $5::date, $6, $7, $8, $9, $10)
		ON CONFLICT (run_id, instrument_id) DO UPDATE SET status = excluded.status,
		    from_session = excluded.from_session, to_session = excluded.to_session,
		    value_count = excluded.value_count, error_code = excluded.error_code,
		    error_summary = excluded.error_summary, finished_at = excluded.finished_at`,
		item.RunID.String(), item.InstrumentID.String(), string(item.Status), sessionText(item.FromSession),
		sessionText(item.ToSession), item.ValueCount, item.ErrorCode, item.ErrorSummary, item.StartedAt, item.FinishedAt)
	if err != nil {
		return fmt.Errorf("write feature run item: %w", err)
	}
	return nil
}

func sessionText(session *SessionDate) *string {
	if session == nil {
		return nil
	}
	text := session.String()
	return &text
}

// CompositeRow is the composite at one session, as stored.
type CompositeRow struct {
	Session          SessionDate
	MeanReturn       *string
	ContributorCount int
	AbsenceReason    *string
}

// WriteComposite replaces the composite series over [from, to] for one universe and
// definition, in one transaction: stage one of a run (research R-005).
func (r *Repository) WriteComposite(ctx context.Context, universeID, definitionID, runID UUID, from, to SessionDate, rows []CompositeRow) error {
	if err := r.ready(); err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin composite write: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DELETE FROM universe_composites WHERE universe_id = $1 AND definition_id = $2
		AND session_date BETWEEN $3::date AND $4::date`, universeID.String(), definitionID.String(), from.String(), to.String()); err != nil {
		return fmt.Errorf("clear composite range: %w", err)
	}
	computedAt := time.Now().UTC()
	sessions := make([]string, len(rows))
	means := make([]*string, len(rows))
	counts := make([]int, len(rows))
	reasons := make([]*string, len(rows))
	for i, row := range rows {
		sessions[i], means[i], counts[i], reasons[i] = row.Session.String(), row.MeanReturn, row.ContributorCount, row.AbsenceReason
	}
	if _, err := tx.Exec(ctx, `INSERT INTO universe_composites
		(universe_id, session_date, definition_id, mean_return, contributor_count, absence_reason, computed_at, run_id)
		SELECT $1::uuid, s::date, $2::uuid, m::numeric, c, a, $3, $4::uuid
		FROM unnest($5::text[], $6::text[], $7::int[], $8::text[]) AS t(s, m, c, a)`,
		universeID.String(), definitionID.String(), computedAt, runID.String(), sessions, means, counts, reasons); err != nil {
		return fmt.Errorf("write composite: %w", err)
	}
	return tx.Commit(ctx)
}

// Composites reads a universe's stored composite series for one definition, by session.
func (r *Repository) Composites(ctx context.Context, universeID, definitionID UUID) (map[SessionDate]CompositeValue, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT session_date::text, mean_return::text, contributor_count
		FROM universe_composites WHERE universe_id = $1 AND definition_id = $2`, universeID.String(), definitionID.String())
	if err != nil {
		return nil, fmt.Errorf("read composites: %w", err)
	}
	defer rows.Close()
	series := map[SessionDate]CompositeValue{}
	for rows.Next() {
		var session string
		var mean *string
		var count int
		if err := rows.Scan(&session, &mean, &count); err != nil {
			return nil, fmt.Errorf("scan composite: %w", err)
		}
		value := CompositeValue{ContributorCount: count}
		if mean != nil {
			if value.MeanReturn, err = strconv.ParseFloat(*mean, 64); err != nil {
				return nil, fmt.Errorf("composite at %s: %w", session, err)
			}
			value.Defined = true
		}
		series[SessionDate(session)] = value
	}
	return series, rows.Err()
}

// ValueRow is one stored feature value or absence, ready to write.
type ValueRow struct {
	DefinitionID  UUID
	Session       SessionDate
	Value         *string
	Label         *string
	AbsenceReason *AbsenceReason
	Currency      *string
}

// Change is what one committed scope announces: the instrument and the inclusive session
// range whose values changed, and the run that produced them.
type Change struct {
	InstrumentID UUID
	FromSession  SessionDate
	ToSession    SessionDate
	RunID        UUID
}

// Scope is one instrument's recomputation: a transaction holding the instrument's advisory
// lock, so two recomputations of one instrument serialise and a reader never sees a mixture
// (data-model.md, Transactional boundaries).
type Scope struct {
	tx           pgx.Tx
	instrumentID UUID
}

// BeginInstrumentScope opens the transaction and waits for the instrument's lock.
func (r *Repository) BeginInstrumentScope(ctx context.Context, instrumentID UUID) (*Scope, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin feature scope: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('features:' || $1::text, 0))`,
		instrumentID.String()); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("acquire feature scope: %w", err)
	}
	return &Scope{tx: tx, instrumentID: instrumentID}, nil
}

// WriteValues replaces every value of the instrument over [from, to] with the rows given.
// Delete-then-insert over the whole affected range is what makes a definition that no
// longer yields a row (a session whose bar was removed) disappear rather than linger.
func (s *Scope) WriteValues(ctx context.Context, runID UUID, from, to SessionDate, rows []ValueRow) error {
	if s == nil || s.tx == nil {
		return errors.New("feature scope is not active")
	}
	if _, err := s.tx.Exec(ctx, `DELETE FROM feature_values WHERE instrument_id = $1
		AND session_date BETWEEN $2::date AND $3::date`, s.instrumentID.String(), from.String(), to.String()); err != nil {
		return fmt.Errorf("clear feature values: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}
	computedAt := time.Now().UTC()
	definitions := make([]string, len(rows))
	sessions := make([]string, len(rows))
	values := make([]*string, len(rows))
	labels := make([]*string, len(rows))
	reasons := make([]*string, len(rows))
	currencies := make([]*string, len(rows))
	for i, row := range rows {
		definitions[i], sessions[i], values[i], labels[i], currencies[i] =
			row.DefinitionID.String(), row.Session.String(), row.Value, row.Label, row.Currency
		if row.AbsenceReason != nil {
			reason := string(*row.AbsenceReason)
			reasons[i] = &reason
		}
	}
	if _, err := s.tx.Exec(ctx, `INSERT INTO feature_values
		(instrument_id, session_date, definition_id, value, label, absence_reason, currency, computed_at, run_id)
		SELECT $1::uuid, s::date, d::uuid, v::numeric, l, a, c, $2, $3::uuid
		FROM unnest($4::text[], $5::text[], $6::text[], $7::text[], $8::text[], $9::text[]) AS t(d, s, v, l, a, c)`,
		s.instrumentID.String(), computedAt, runID.String(), definitions, sessions, values, labels, reasons, currencies); err != nil {
		return fmt.Errorf("write feature values: %w", err)
	}
	return nil
}

// WriteItem records the instrument's outcome inside the scope, so it commits with the values.
func (s *Scope) WriteItem(ctx context.Context, item RunItem) error {
	if s == nil || s.tx == nil {
		return errors.New("feature scope is not active")
	}
	return writeItem(ctx, s.tx, item)
}

// Commit publishes the change event in the same transaction and commits.
func (s *Scope) Commit(ctx context.Context, change Change) error {
	if s == nil || s.tx == nil {
		return errors.New("feature scope is not active")
	}
	payload, err := json.Marshal(map[string]string{
		"instrument_id": change.InstrumentID.String(), "from_session": change.FromSession.String(),
		"to_session": change.ToSession.String(), "run_id": change.RunID.String(),
	})
	if err != nil {
		return fmt.Errorf("encode feature change: %w", err)
	}
	if err := clientevents.Insert(ctx, s.tx, clientevents.Event{
		Type: EventFeatureValuesChanged, Version: 1, Scope: "shared", EntityType: "instrument",
		EntityID: change.InstrumentID.String(), Payload: payload, OccurredAt: time.Now().UTC(),
	}); err != nil {
		return err
	}
	return s.tx.Commit(ctx)
}

// Rollback abandons the scope; every staged value, item and event goes with it.
func (s *Scope) Rollback(ctx context.Context) error {
	if s == nil || s.tx == nil {
		return nil
	}
	return s.tx.Rollback(ctx)
}

// ReadAsOf returns every active definition for one instrument at one session: a stored value
// or absence for each definition that has a row, and the names of those that have none.
func (r *Repository) ReadAsOf(ctx context.Context, instrumentID UUID, asOf SessionDate) (FeatureSet, error) {
	if err := r.ready(); err != nil {
		return FeatureSet{}, err
	}
	set := FeatureSet{InstrumentID: instrumentID, SessionDate: asOf}
	rows, err := r.pool.Query(ctx, `
		SELECT d.name, d.version, d.window_sessions,
		       v.value::text, v.label, v.absence_reason, v.currency, v.computed_at
		FROM feature_definitions d
		LEFT JOIN feature_values v
		       ON v.definition_id = d.id AND v.instrument_id = $1 AND v.session_date = $2
		WHERE d.superseded_at IS NULL AND d.name <> $3
		ORDER BY d.name`, instrumentID.String(), asOf.String(), CompositeDefinitionName)
	if err != nil {
		return FeatureSet{}, fmt.Errorf("read features as of %s: %w", asOf, err)
	}
	defer rows.Close()
	set.NotComputed = []string{}
	for rows.Next() {
		var value Value
		var reason *string
		var computedAt *time.Time
		if err := rows.Scan(&value.Name, &value.DefinitionVersion, &value.WindowSessions,
			&value.Value, &value.Label, &reason, &value.Currency, &computedAt); err != nil {
			return FeatureSet{}, fmt.Errorf("scan feature value: %w", err)
		}
		if value.Value == nil && value.Label == nil && reason == nil {
			set.NotComputed = append(set.NotComputed, value.Name)
			continue
		}
		if reason != nil {
			absence := AbsenceReason(*reason)
			value.AbsenceReason = &absence
		}
		value.SessionDate = asOf
		if computedAt != nil {
			value.ComputedAt = *computedAt
		}
		set.Features = append(set.Features, value)
	}
	if err := rows.Err(); err != nil {
		return FeatureSet{}, fmt.Errorf("read features as of %s: %w", asOf, err)
	}
	return set, nil
}
