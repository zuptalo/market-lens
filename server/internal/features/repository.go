package features

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository persists runs, values and composites, and reads them back as of a session.
type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

// ReadAsOf returns every active definition for one instrument at one session: a stored value
// or absence for each definition that has a row, and the names of those that have none.
func (r *Repository) ReadAsOf(ctx context.Context, instrumentID UUID, asOf SessionDate) (FeatureSet, error) {
	if r == nil || r.pool == nil {
		return FeatureSet{}, errors.New("features repository is required")
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
