package marketdata

import (
	"context"
	"errors"
	"fmt"
	"time"

	"market-lens/server/internal/instruments"

	"github.com/jackc/pgx/v5"
)

// FallbackState is whether the product is running on fallback prices, derived from the bars
// themselves: an instrument is covered while any of its bars came from the fallback provider.
type FallbackState struct {
	Active bool
	// Since is the earliest session still carried by a fallback bar.
	Since       *SessionDate
	Instruments int
	// PendingReconciliation means the primary is delivering again but older fallback bars have not
	// been replaced yet.
	PendingReconciliation bool
	// Recorded is what the product last said, which is what a change is measured against.
	Recorded RecordedFallbackState
}

type RecordedFallbackState struct {
	Active                bool
	Since                 *SessionDate
	Instruments           int
	PendingReconciliation bool
	ChangedAt             time.Time
}

func (s FallbackState) differs(recorded RecordedFallbackState) bool {
	sameSince := (s.Since == nil && recorded.Since == nil) ||
		(s.Since != nil && recorded.Since != nil && *s.Since == *recorded.Since)
	return s.Active != recorded.Active || !sameSince || s.Instruments != recorded.Instruments ||
		s.PendingReconciliation != recorded.PendingReconciliation
}

// FallbackChange is what one observation found had changed. Entered and Ended are the two
// moments worth telling somebody about; every other change is published but not told.
type FallbackChange struct {
	State   FallbackState
	Entered bool
	Ended   bool
}

type CoverOptions struct {
	AppVersion string
	Workers    int
	MaxRetries int
}

// Fallback stands in for the primary provider while it refuses authentication (feature 030).
type Fallback struct {
	repository *Repository
	importer   *ImportService
	now        func() time.Time
	// Tell raises the notice for an entered or ended fallback, inside the transaction that records
	// the change — so a change is recorded if and only if it was told. Nil tells nobody.
	Tell func(context.Context, pgx.Tx, FallbackChange) error
}

func NewFallback(repository *Repository, importer *ImportService) *Fallback {
	return &Fallback{repository: repository, importer: importer, now: time.Now}
}

func (f *Fallback) Enabled(ctx context.Context) (bool, error) {
	var enabled bool
	if err := f.repository.pool.QueryRow(ctx, `SELECT enabled FROM market_data_fallback_settings`).Scan(&enabled); err != nil {
		return false, fmt.Errorf("read the fallback switch: %w", err)
	}
	return enabled, nil
}

func (f *Fallback) SetEnabled(ctx context.Context, enabled bool) error {
	if _, err := f.repository.pool.Exec(ctx, `UPDATE market_data_fallback_settings
		SET enabled = $1, updated_at = now()`, enabled); err != nil {
		return fmt.Errorf("set the fallback switch: %w", err)
	}
	return nil
}

// Cover asks the fallback for what the primary run could not get because it refused
// authentication, and reports whether there was anything to ask about.
//
// Nothing else is covered: a rate limit, a timeout or an outage is the primary's to retry
// (FR-002). Each instrument is asked only for the sessions after its newest primary price, so the
// fallback never touches the history before the lapse, and a split or dividend anywhere since
// that price holds the instrument back (FR-008). An instrument with no primary price at all is
// not covered: there is nothing to check the fallback against.
func (f *Fallback) Cover(ctx context.Context, primary ImportRun, options CoverOptions) (ImportRun, bool, error) {
	enabled, err := f.Enabled(ctx)
	if err != nil || !enabled {
		return ImportRun{}, false, err
	}
	rows, err := f.repository.pool.Query(ctx, `SELECT it.instrument_id::text, p.provider_symbol, i.currency,
			((SELECT max(b.session_date) FROM daily_price_bars b
			  WHERE b.instrument_id = it.instrument_id AND b.provider <> $2) + 1)::text,
			it.requested_to::text
		FROM import_items it
		JOIN instruments i ON i.id = it.instrument_id AND i.active
		JOIN provider_instruments p ON p.instrument_id = it.instrument_id AND p.provider = $2 AND p.active
		WHERE it.run_id = $1 AND it.status = 'failed' AND it.error_code = 'provider_authentication'
		  AND EXISTS (SELECT 1 FROM daily_price_bars b
		              WHERE b.instrument_id = it.instrument_id AND b.provider <> $2)
		ORDER BY i.ticker`, primary.ID.String(), FallbackProvider)
	if err != nil {
		return ImportRun{}, false, fmt.Errorf("read what the primary could not get: %w", err)
	}
	var targets []ImportTarget
	for rows.Next() {
		var id, symbol, currency, from, to string
		if err := rows.Scan(&id, &symbol, &currency, &from, &to); err != nil {
			rows.Close()
			return ImportRun{}, false, err
		}
		target := ImportTarget{ProviderSymbol: symbol, Currency: currency,
			From: SessionDate(from), To: SessionDate(to)}
		if target.InstrumentID, err = instruments.ParseUUID(id); err != nil {
			rows.Close()
			return ImportRun{}, false, err
		}
		if target.From <= target.To {
			targets = append(targets, target)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return ImportRun{}, false, err
	}
	if len(targets) == 0 {
		return ImportRun{}, false, nil
	}
	parent := primary.ID
	run, err := f.importer.Import(ctx, ImportRequest{
		Kind: ImportFallback, Provider: FallbackProvider, AppVersion: options.AppVersion,
		ParentRunID: &parent, Targets: targets, MaxRetries: options.MaxRetries, Workers: options.Workers,
	})
	return run, true, err
}

// derive reads the state from the bars.
func deriveFallbackState(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (FallbackState, error) {
	var state FallbackState
	var since *string
	// Read from the fallback bars alone (a partial index), and the primary's newest session by a
	// backward scan that stops at the first primary bar — never a pass over every stored bar.
	if err := q.QueryRow(ctx, `SELECT count(DISTINCT f.instrument_id), min(f.session_date)::text,
			coalesce((SELECT b.session_date FROM daily_price_bars b WHERE b.provider <> $1
			          ORDER BY b.session_date DESC LIMIT 1) > max(f.session_date), false)
		FROM daily_price_bars f WHERE f.provider = $1`, FallbackProvider).Scan(&state.Instruments, &since, &state.PendingReconciliation); err != nil {
		return FallbackState{}, fmt.Errorf("derive the fallback state: %w", err)
	}
	state.Active = state.Instruments > 0
	if since != nil {
		date := SessionDate(*since)
		state.Since = &date
	}
	return state, nil
}

func readRecorded(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, lock bool) (RecordedFallbackState, error) {
	query := `SELECT active, since::text, instruments, pending_reconciliation, changed_at FROM market_data_fallback_state`
	if lock {
		query += ` FOR UPDATE`
	}
	var recorded RecordedFallbackState
	var since *string
	if err := q.QueryRow(ctx, query).Scan(&recorded.Active, &since, &recorded.Instruments,
		&recorded.PendingReconciliation, &recorded.ChangedAt); err != nil {
		return RecordedFallbackState{}, fmt.Errorf("read the recorded fallback state: %w", err)
	}
	if since != nil {
		date := SessionDate(*since)
		recorded.Since = &date
	}
	return recorded, nil
}

// State is what a screen loads: the state as last recorded and published. A screen shows what
// the events it follows describe, and reading one row keeps every page load from deriving it.
func (f *Fallback) State(ctx context.Context) (FallbackState, error) {
	recorded, err := readRecorded(ctx, f.repository.pool, false)
	if err != nil {
		return FallbackState{}, err
	}
	return FallbackState{Active: recorded.Active, Since: recorded.Since, Instruments: recorded.Instruments,
		PendingReconciliation: recorded.PendingReconciliation, Recorded: recorded}, nil
}

// Observe compares the state the bars show with the one last recorded and, if they differ,
// records the new one, publishes it and tells whoever asked — all in one transaction. The row lock
// is what makes a change told once however many passes observe it; a repeated observation finds
// nothing different and says nothing.
func (f *Fallback) Observe(ctx context.Context) (FallbackState, bool, error) {
	tx, err := f.repository.pool.Begin(ctx)
	if err != nil {
		return FallbackState{}, false, fmt.Errorf("begin observing the fallback: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	recorded, err := readRecorded(ctx, tx, true)
	if err != nil {
		return FallbackState{}, false, err
	}
	state, err := deriveFallbackState(ctx, tx)
	if err != nil {
		return FallbackState{}, false, err
	}
	state.Recorded = recorded
	if !state.differs(recorded) {
		return state, false, nil
	}
	now := f.now().UTC()
	var since any
	if state.Since != nil {
		since = state.Since.String()
	}
	if _, err := tx.Exec(ctx, `UPDATE market_data_fallback_state
		SET active = $1, since = $2::date, instruments = $3, pending_reconciliation = $4, changed_at = $5`,
		state.Active, since, state.Instruments, state.PendingReconciliation, now); err != nil {
		return FallbackState{}, false, fmt.Errorf("record the fallback state: %w", err)
	}
	if err := emitEvent(ctx, tx, "market_data_fallback", "instance", map[string]any{
		"active": state.Active, "since": since, "instruments": state.Instruments,
		"pending_reconciliation": state.PendingReconciliation,
	}, now); err != nil {
		return FallbackState{}, false, err
	}
	change := FallbackChange{State: state, Entered: state.Active && !recorded.Active, Ended: !state.Active && recorded.Active}
	if (change.Entered || change.Ended) && f.Tell != nil {
		if err := f.Tell(ctx, tx, change); err != nil {
			return FallbackState{}, false, fmt.Errorf("tell about the fallback: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return FallbackState{}, false, fmt.Errorf("commit the fallback state: %w", err)
	}
	state.Recorded = RecordedFallbackState{Active: state.Active, Since: state.Since, Instruments: state.Instruments,
		PendingReconciliation: state.PendingReconciliation, ChangedAt: now}
	return state, true, nil
}

// ReconcileTargets is what the primary must be asked for to replace every fallback bar: each
// covered instrument, under the primary's own symbol, over the span its fallback bars cover.
func (f *Fallback) ReconcileTargets(ctx context.Context, primaryProvider string) ([]ImportTarget, error) {
	if primaryProvider == "" || primaryProvider == FallbackProvider {
		return nil, errors.New("reconciliation needs the primary provider")
	}
	rows, err := f.repository.pool.Query(ctx, `SELECT b.instrument_id::text, p.provider_symbol, i.currency,
			min(b.session_date)::text, max(b.session_date)::text
		FROM daily_price_bars b
		JOIN instruments i ON i.id = b.instrument_id
		JOIN provider_instruments p ON p.instrument_id = b.instrument_id AND p.provider = $2 AND p.active
		WHERE b.provider = $1
		GROUP BY b.instrument_id, p.provider_symbol, i.currency, i.ticker
		ORDER BY i.ticker`, FallbackProvider, primaryProvider)
	if err != nil {
		return nil, fmt.Errorf("read the fallback span: %w", err)
	}
	defer rows.Close()
	var targets []ImportTarget
	for rows.Next() {
		var id, symbol, currency, from, to string
		if err := rows.Scan(&id, &symbol, &currency, &from, &to); err != nil {
			return nil, err
		}
		target := ImportTarget{ProviderSymbol: symbol, Currency: currency, From: SessionDate(from), To: SessionDate(to)}
		if target.InstrumentID, err = instruments.ParseUUID(id); err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, rows.Err()
}
