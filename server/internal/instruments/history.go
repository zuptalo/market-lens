package instruments

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// History returns one instrument's stored daily history for charting.
//
// The window is bounded in *stored exchange sessions*, never in calendar days: the stored
// unit is a session, and a "thirty day" window would silently mean a different number of
// observations on each exchange in a week containing a Norwegian holiday (research R7).
//
// Nothing here is interpolated, forward-filled, or padded. Where an observation is absent the
// window says so by naming the date in MissingSessions, and it is the chart's job to draw an
// interruption rather than to bridge it (FR-013, SC-005).
func (r *Repository) History(ctx context.Context, id UUID, filter HistoryFilter,
	asOf SessionDate) (HistoryWindow, error) {
	if filter.Sessions < 2 || filter.Sessions > 5000 {
		return HistoryWindow{}, fmt.Errorf("%w: sessions must be between 2 and 5000", ErrInvalidQuery)
	}
	if asOf == "" {
		return HistoryWindow{}, fmt.Errorf("%w: an as-of date is required", ErrInvalidQuery)
	}

	window := HistoryWindow{
		MissingSessions: []SessionDate{},
		Bars:            []DailyBar{},
		Actions:         []ChartAction{},
		Findings:        []ChartFinding{},
		SeriesBasis:     SeriesRaw,
	}

	// The identity row doubles as the existence check: an instrument that is not in the
	// listing is answered exactly as one that does not exist (FR-018).
	listing, err := r.listingRowFor(ctx, id, asOf)
	if err != nil {
		return HistoryWindow{}, err
	}
	window.Instrument = listing

	var exchangeID string
	var first, last *string
	var stored int64
	if err := r.pool.QueryRow(ctx, `SELECT i.exchange_id::text,
		min(b.session_date)::text, max(b.session_date)::text, count(b.*)
		FROM instruments i
		LEFT JOIN daily_price_bars b ON b.instrument_id = i.id
		WHERE i.id = $1 GROUP BY i.exchange_id`, id.String()).
		Scan(&exchangeID, &first, &last, &stored); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return HistoryWindow{}, ErrNotFound
		}
		return HistoryWindow{}, fmt.Errorf("read history coverage: %w", err)
	}
	window.Coverage = HistoryCoverage{BarCount: stored}
	if first != nil {
		window.Coverage.FirstSession = SessionDate(*first)
	}
	if last != nil {
		window.Coverage.LastSession = SessionDate(*last)
	}
	// No stored session means no range, so nothing can be missing from it. Reporting every
	// open session the exchange ever had would be true and useless.
	if stored == 0 {
		return window, nil
	}

	// The window ends at the requested session, or at the most recent stored one, and begins
	// at the Nth stored session before it. Resolving it against stored bars rather than the
	// calendar is what makes a window longer than the history clamp to where the data starts
	// instead of padding it.
	to := window.Coverage.LastSession
	if filter.To != "" && filter.To < to {
		to = filter.To
	}
	var from string
	if err := r.pool.QueryRow(ctx, `SELECT min(session_date)::text FROM (
			SELECT session_date FROM daily_price_bars
			WHERE instrument_id = $1 AND session_date <= $2
			ORDER BY session_date DESC LIMIT $3
		) recent`, id.String(), to.String(), filter.Sessions).Scan(&from); err != nil {
		return HistoryWindow{}, fmt.Errorf("resolve history window: %w", err)
	}
	window.RequestedFrom = SessionDate(from)
	window.RequestedTo = to

	if err := r.readBars(ctx, &window, id); err != nil {
		return HistoryWindow{}, err
	}
	if err := r.readMissingSessions(ctx, &window, id, exchangeID); err != nil {
		return HistoryWindow{}, err
	}
	if err := r.readAnnotations(ctx, &window, id); err != nil {
		return HistoryWindow{}, err
	}
	return window, nil
}

func (r *Repository) readBars(ctx context.Context, window *HistoryWindow, id UUID) error {
	rows, err := r.pool.Query(ctx, `SELECT session_date::text, open::text, high::text, low::text,
		close::text, adjusted_close::text, volume, currency, provider, last_observed_at
		FROM daily_price_bars
		WHERE instrument_id = $1 AND session_date BETWEEN $2 AND $3
		ORDER BY session_date`,
		id.String(), window.RequestedFrom.String(), window.RequestedTo.String())
	if err != nil {
		return fmt.Errorf("read history bars: %w", err)
	}
	defer rows.Close()

	adjustedSeen := false
	for rows.Next() {
		var bar DailyBar
		var adjusted *string
		if err := rows.Scan(&bar.SessionDate, &bar.Open, &bar.High, &bar.Low, &bar.Close,
			&adjusted, &bar.Volume, &bar.Currency, &bar.Provider, &bar.ObservedAt); err != nil {
			return fmt.Errorf("scan history bar: %w", err)
		}
		bar.AdjustedClose = adjusted
		if adjusted != nil {
			adjustedSeen = true
		}
		window.Bars = append(window.Bars, bar)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read history bars: %w", err)
	}

	// Whether the series is raw or provider-adjusted is a fact about what is displayed, and
	// a reader has to be told which one they are looking at (FR-014).
	if adjustedSeen {
		window.SeriesBasis = SeriesProviderAdjusted
	}
	if count := len(window.Bars); count > 0 {
		latest := window.Bars[count-1]
		provider := latest.Provider
		observed := latest.ObservedAt
		window.Provider = &provider
		window.ObservedAt = &observed
	}
	return nil
}

// readMissingSessions asks the exchange calendar which sessions inside the window the market
// was open for and no bar is stored.
//
// This is the difference between an honest chart and a misleading one, and it is why the
// answer comes from the calendar rather than from weekday arithmetic: a Swedish midsummer or
// a Danish Constitution Day is recorded as closed, so it is not a gap. Guessing from
// weekends would report every public holiday as missing data and train a reader to ignore
// the warning entirely (research R3).
func (r *Repository) readMissingSessions(ctx context.Context, window *HistoryWindow,
	id UUID, exchangeID string) error {
	rows, err := r.pool.Query(ctx, `SELECT s.session_date::text
		FROM exchange_sessions s
		LEFT JOIN daily_price_bars b
		  ON b.instrument_id = $1 AND b.session_date = s.session_date
		WHERE s.exchange_id = $2
		  AND s.status IN ('open', 'half_day')
		  AND s.session_date BETWEEN $3 AND $4
		  AND b.instrument_id IS NULL
		ORDER BY s.session_date`,
		id.String(), exchangeID, window.RequestedFrom.String(), window.RequestedTo.String())
	if err != nil {
		return fmt.Errorf("read missing sessions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var date string
		if err := rows.Scan(&date); err != nil {
			return fmt.Errorf("scan missing session: %w", err)
		}
		window.MissingSessions = append(window.MissingSessions, SessionDate(date))
	}
	return rows.Err()
}

// readAnnotations collects the corporate actions and quality findings that explain a
// discontinuity, so a reader can tell a real fifty-percent move from an unadjusted split.
func (r *Repository) readAnnotations(ctx context.Context, window *HistoryWindow, id UUID) error {
	actions, err := r.pool.Query(ctx, `SELECT id::text, action_type, ex_date::text,
		ratio::text, amount::text, currency, old_symbol, new_symbol
		FROM corporate_actions
		WHERE instrument_id = $1 AND ex_date BETWEEN $2 AND $3
		ORDER BY ex_date, id`,
		id.String(), window.RequestedFrom.String(), window.RequestedTo.String())
	if err != nil {
		return fmt.Errorf("read corporate actions: %w", err)
	}
	defer actions.Close()
	for actions.Next() {
		var action ChartAction
		var actionID, exDate string
		var ratio, amount *string
		if err := actions.Scan(&actionID, &action.Type, &exDate, &ratio, &amount,
			&action.Currency, &action.OldSymbol, &action.NewSymbol); err != nil {
			return fmt.Errorf("scan corporate action: %w", err)
		}
		action.ID = UUID(actionID)
		action.ExDate = SessionDate(exDate)
		action.Ratio = decimalPointer(ratio)
		action.Amount = decimalPointer(amount)
		window.Actions = append(window.Actions, action)
	}
	if err := actions.Err(); err != nil {
		return fmt.Errorf("read corporate actions: %w", err)
	}

	// A finding with no session concerns the instrument as a whole and is always relevant;
	// one anchored to a session is included when that session is in view.
	findings, err := r.pool.Query(ctx, `SELECT id::text, rule, status, severity,
		session_date::text, detail
		FROM data_quality_findings
		WHERE instrument_id = $1
		  AND (session_date IS NULL OR session_date BETWEEN $2 AND $3)
		ORDER BY session_date NULLS FIRST, id`,
		id.String(), window.RequestedFrom.String(), window.RequestedTo.String())
	if err != nil {
		return fmt.Errorf("read quality findings: %w", err)
	}
	defer findings.Close()
	for findings.Next() {
		var finding ChartFinding
		var findingID string
		var session *string
		if err := findings.Scan(&findingID, &finding.Rule, &finding.Status, &finding.Severity,
			&session, &finding.Detail); err != nil {
			return fmt.Errorf("scan quality finding: %w", err)
		}
		finding.ID = UUID(findingID)
		if session != nil {
			date := SessionDate(*session)
			finding.SessionDate = &date
		}
		window.Findings = append(window.Findings, finding)
	}
	return findings.Err()
}

// listingRowFor reuses the listing projection for one instrument so the identity and
// statistics shown beside a chart are derived exactly as they are in the list. Two
// derivations of the same number would eventually disagree.
func (r *Repository) listingRowFor(ctx context.Context, id UUID, asOf SessionDate) (ListingRow, error) {
	page, err := r.Listing(ctx, ListingFilter{Limit: 1, Sort: SortName, AsOf: asOf, ID: id})
	if err != nil {
		return ListingRow{}, err
	}
	if len(page.Items) == 0 {
		return ListingRow{}, ErrNotFound
	}
	return page.Items[0], nil
}
