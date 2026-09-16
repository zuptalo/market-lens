package backtest

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// The reading side.
//
// Nothing here writes. A backtest is run by an owner at the command line, so no request can make
// the product produce a result — which is also why there is no handler that would need guarding
// against one.

// Summary is one completed run as a list shows it.
type Summary struct {
	Run           Run
	Configuration Configuration
}

// ListRuns reads the completed runs, newest first.
func (r *Repository) ListRuns(ctx context.Context, limit int) ([]Summary, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 50 {
		limit = 20
	}
	rows, err := r.pool.Query(ctx, `SELECT `+runColumns+`, c.id::text, c.name, c.version, c.title,
		c.intent, c.caveat, c.parameters, c.published_at, c.superseded_at
		FROM backtest_runs r JOIN backtest_configurations c ON c.id = r.configuration_id
		ORDER BY r.started_at DESC, r.id LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list the backtests: %w", err)
	}
	defer rows.Close()
	var summaries []Summary
	for rows.Next() {
		summary, err := scanSummary(rows)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	return summaries, rows.Err()
}

const runColumns = `r.id::text, r.configuration_id::text, r.status, r.from_session::text,
	r.to_session::text, r.strategy_id::text, r.signals_computed_through, r.bars_observed_through,
	r.started_at, r.finished_at, r.trade_count, r.skip_count, r.rebalance_count,
	coalesce(r.error_code, ''), coalesce(r.error_summary, ''), r.app_version`

type scanner interface{ Scan(...any) error }

func scanSummary(row scanner) (Summary, error) {
	var summary Summary
	var raw []byte
	if err := row.Scan(
		(*string)(&summary.Run.ID), (*string)(&summary.Run.ConfigurationID), (*string)(&summary.Run.Status),
		(*string)(&summary.Run.From), (*string)(&summary.Run.To), (*string)(&summary.Run.StrategyID),
		&summary.Run.SignalsComputedThrough, &summary.Run.BarsObservedThrough,
		&summary.Run.StartedAt, &summary.Run.FinishedAt, &summary.Run.TradeCount,
		&summary.Run.SkipCount, &summary.Run.RebalanceCount, &summary.Run.ErrorCode,
		&summary.Run.ErrorSummary, &summary.Run.AppVersion,
		(*string)(&summary.Configuration.ID), &summary.Configuration.Name, &summary.Configuration.Version,
		&summary.Configuration.Title, &summary.Configuration.Intent, &summary.Configuration.Caveat,
		&raw, &summary.Configuration.PublishedAt, &summary.Configuration.SupersededAt); err != nil {
		return Summary{}, err
	}
	if err := decodeParameters(raw, &summary.Configuration); err != nil {
		return Summary{}, err
	}
	return summary, nil
}

// SkipTally is how many signals each reason accounts for. Counted rather than listed because the
// question a reader asks first is "why did nothing happen", not "which twelve thousand".
type SkipTally struct {
	Reason SkipReason
	Count  int64
}

// Detail is one whole result as a reader receives it.
type Detail struct {
	Summary
	Measures []Measures
	Skips    []SkipTally
	// Benchmarks names the market each reported series belongs to, so a comparison is never shown
	// without saying what it is a comparison against.
	Markets map[string]string
}

func (r *Repository) Backtest(ctx context.Context, id string) (Detail, error) {
	if err := r.ready(); err != nil {
		return Detail{}, err
	}
	row := r.pool.QueryRow(ctx, `SELECT `+runColumns+`, c.id::text, c.name, c.version, c.title,
		c.intent, c.caveat, c.parameters, c.published_at, c.superseded_at
		FROM backtest_runs r JOIN backtest_configurations c ON c.id = r.configuration_id
		WHERE r.id = $1`, id)
	summary, err := scanSummary(row)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return Detail{}, fmt.Errorf("%w: backtest %s", ErrNotFound, id)
		}
		return Detail{}, fmt.Errorf("read the backtest: %w", err)
	}
	detail := Detail{Summary: summary, Markets: map[string]string{}}

	measures, err := r.pool.Query(ctx, `SELECT m.subject, m.benchmark_series_id::text,
		m.from_session::text, m.to_session::text, m.total_return::text, m.annualised_return::text,
		m.volatility::text, m.max_drawdown::text, m.trade_count, m.total_costs::text,
		m.absence_reason, b.mic
		FROM backtest_measures m LEFT JOIN benchmark_series b ON b.id = m.benchmark_series_id
		WHERE m.run_id = $1 ORDER BY m.subject = 'strategy' DESC, m.subject`, id)
	if err != nil {
		return Detail{}, fmt.Errorf("read the measures: %w", err)
	}
	for measures.Next() {
		var measure Measures
		var seriesID, from, to, reason, mic *string
		if err := measures.Scan(&measure.Subject, &seriesID, &from, &to, &measure.TotalReturn,
			&measure.AnnualisedReturn, &measure.Volatility, &measure.MaxDrawdown,
			&measure.TradeCount, &measure.TotalCosts, &reason, &mic); err != nil {
			measures.Close()
			return Detail{}, err
		}
		measure.RunID = summary.Run.ID
		if seriesID != nil {
			value := UUID(*seriesID)
			measure.BenchmarkSeriesID = &value
		}
		if from != nil {
			value := SessionDate(*from)
			measure.From = &value
		}
		if to != nil {
			value := SessionDate(*to)
			measure.To = &value
		}
		if reason != nil {
			value := MeasureAbsence(*reason)
			measure.AbsenceReason = &value
		}
		if mic != nil {
			detail.Markets[measure.Subject] = *mic
		}
		detail.Measures = append(detail.Measures, measure)
	}
	measures.Close()
	if err := measures.Err(); err != nil {
		return Detail{}, err
	}

	skips, err := r.pool.Query(ctx, `SELECT reason, count(*) FROM backtest_skips
		WHERE run_id = $1 GROUP BY reason ORDER BY count(*) DESC, reason`, id)
	if err != nil {
		return Detail{}, fmt.Errorf("read the skipped signals: %w", err)
	}
	defer skips.Close()
	for skips.Next() {
		var tally SkipTally
		if err := skips.Scan((*string)(&tally.Reason), &tally.Count); err != nil {
			return Detail{}, err
		}
		detail.Skips = append(detail.Skips, tally)
	}
	return detail, skips.Err()
}

// TradeView is a trade with what a reader needs to recognise it, and the identity of the signal
// behind it.
type TradeView struct {
	Trade
	Ticker string
	Name   string
}

type TradePage struct {
	Items      []TradeView
	NextCursor string
	// Total is counted only on a cursor-less request. Counting on every page would defeat the
	// early termination keyset paging exists for — the rule feature 014 established.
	Total *int64
}

func (r *Repository) Trades(ctx context.Context, id, cursor string, limit int) (TradePage, error) {
	if err := r.ready(); err != nil {
		return TradePage{}, err
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	var page TradePage
	if cursor == "" {
		var total int64
		if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM backtest_trades WHERE run_id = $1`, id).
			Scan(&total); err != nil {
			return TradePage{}, fmt.Errorf("count the trades: %w", err)
		}
		page.Total = &total
	}
	after, err := decodeTradeCursor(cursor)
	if err != nil {
		return TradePage{}, err
	}

	query := `SELECT t.id::text, t.instrument_id::text, t.strategy_id::text, t.signal_session::text,
		t.execution_session::text, t.direction, t.quantity::text, t.price::text, t.price_currency,
		t.fx_rate::text, t.brokerage::text, t.slippage::text, t.spread_cost::text,
		t.cash_effect::text, i.ticker, i.name
		FROM backtest_trades t JOIN instruments i ON i.id = t.instrument_id
		WHERE t.run_id = $1`
	args := []any{id}
	if after != nil {
		// Keyset over (execution session, identity), which is a total order and therefore stable
		// when a rebalance fills a dozen trades on the same session.
		query += ` AND (t.execution_session, t.id::text) > ($2::date, $3)`
		args = append(args, after.session, after.id)
	}
	query += ` ORDER BY t.execution_session, t.id::text LIMIT $` + strconv.Itoa(len(args)+1)
	args = append(args, limit+1)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return TradePage{}, fmt.Errorf("read the trades: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var view TradeView
		if err := rows.Scan((*string)(&view.ID), (*string)(&view.InstrumentID),
			(*string)(&view.StrategyID), (*string)(&view.SignalSession),
			(*string)(&view.ExecutionSession), (*string)(&view.Direction), &view.Quantity,
			&view.Price, &view.PriceCurrency, &view.FXRate, &view.Brokerage, &view.Slippage,
			&view.SpreadCost, &view.CashEffect, &view.Ticker, &view.Name); err != nil {
			return TradePage{}, err
		}
		view.RunID = UUID(id)
		page.Items = append(page.Items, view)
	}
	if err := rows.Err(); err != nil {
		return TradePage{}, err
	}
	if len(page.Items) > limit {
		last := page.Items[limit-1]
		page.Items = page.Items[:limit]
		page.NextCursor = encodeTradeCursor(last.ExecutionSession.String(), last.ID.String())
	}
	return page, nil
}

type tradeCursor struct{ session, id string }

func encodeTradeCursor(session, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(session + "|" + id))
}

func decodeTradeCursor(value string) (*tradeCursor, error) {
	if value == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, errors.New("the cursor is not readable")
	}
	session, id, found := strings.Cut(string(raw), "|")
	if !found || session == "" || id == "" {
		return nil, errors.New("the cursor is not readable")
	}
	return &tradeCursor{session: session, id: id}, nil
}

// Equity reads the whole curve. It is not paged: the figures are the chart, and a reader who
// cannot see a canvas must not receive a poorer version of the result than one who can.
func (r *Repository) Equity(ctx context.Context, id string) ([]EquityPoint, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT session_date::text, cash::text, position_value::text,
		total::text, absence_reason FROM backtest_equity WHERE run_id = $1 ORDER BY session_date`, id)
	if err != nil {
		return nil, fmt.Errorf("read the equity curve: %w", err)
	}
	defer rows.Close()
	var curve []EquityPoint
	for rows.Next() {
		var item EquityPoint
		var reason *string
		if err := rows.Scan((*string)(&item.SessionDate), &item.Cash, &item.PositionValue,
			&item.Total, &reason); err != nil {
			return nil, err
		}
		item.RunID = UUID(id)
		if reason != nil {
			value := EquityAbsence(*reason)
			item.AbsenceReason = &value
		}
		curve = append(curve, item)
	}
	return curve, rows.Err()
}
