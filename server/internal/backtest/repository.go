package backtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	clientevents "market-lens/server/internal/events"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrNotFound distinguishes "no such configuration" from a read failure.
	ErrNotFound = errors.New("not found")
	// ErrNoData refuses a run over a range with nothing in it, rather than recording an empty
	// result as though the strategy had been tested.
	ErrNoData = errors.New("no stored data in range")
)

// Repository owns every read a backtest performs and the single transactional write that commits
// one. Nothing here reaches a provider: a simulation that fetched would not be reproducible, and
// the absence of a provider on this path is what SC-001 rests on.
type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) ready() error {
	if r == nil || r.pool == nil {
		return errors.New("backtest repository is not configured")
	}
	return nil
}

// Configuration reads one published configuration — the current version when none is named.
func (r *Repository) Configuration(ctx context.Context, name string, version int) (Configuration, error) {
	if err := r.ready(); err != nil {
		return Configuration{}, err
	}
	query := `SELECT id::text, name, version, title, intent, caveat, parameters, published_at, superseded_at
		FROM backtest_configurations WHERE name = $1`
	args := []any{name}
	if version > 0 {
		query += ` AND version = $2`
		args = append(args, version)
	} else {
		query += ` AND superseded_at IS NULL`
	}

	var configuration Configuration
	var raw []byte
	if err := r.pool.QueryRow(ctx, query, args...).Scan(
		(*string)(&configuration.ID), &configuration.Name, &configuration.Version,
		&configuration.Title, &configuration.Intent, &configuration.Caveat, &raw,
		&configuration.PublishedAt, &configuration.SupersededAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Configuration{}, fmt.Errorf("%w: backtest configuration %q", ErrNotFound, name)
		}
		return Configuration{}, fmt.Errorf("read the configuration: %w", err)
	}
	if err := decodeParameters(raw, &configuration); err != nil {
		return Configuration{}, err
	}
	return configuration, nil
}

// ListConfigurations reads the published configurations, current first.
func (r *Repository) ListConfigurations(ctx context.Context) ([]Configuration, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT id::text, name, version, title, intent, caveat,
		parameters, published_at, superseded_at FROM backtest_configurations
		ORDER BY superseded_at IS NOT NULL, name, version DESC`)
	if err != nil {
		return nil, fmt.Errorf("list configurations: %w", err)
	}
	defer rows.Close()
	var configurations []Configuration
	for rows.Next() {
		var configuration Configuration
		var raw []byte
		if err := rows.Scan((*string)(&configuration.ID), &configuration.Name, &configuration.Version,
			&configuration.Title, &configuration.Intent, &configuration.Caveat, &raw,
			&configuration.PublishedAt, &configuration.SupersededAt); err != nil {
			return nil, err
		}
		if err := decodeParameters(raw, &configuration); err != nil {
			return nil, err
		}
		configurations = append(configurations, configuration)
	}
	return configurations, rows.Err()
}

type parameterDocument struct {
	StrategyName       string `json:"strategy_name"`
	StrategyVersion    int    `json:"strategy_version"`
	UniverseCode       string `json:"universe_code"`
	From               string `json:"from_session"`
	To                 string `json:"to_session"`
	StartingCapital    string `json:"starting_capital"`
	AccountingCurrency string `json:"accounting_currency"`
	SizingRule         string `json:"sizing_rule"`
	SizingN            int    `json:"sizing_n"`
	Rebalance          string `json:"rebalance"`
	BrokerageBPS       string `json:"brokerage_bps"`
	BrokerageMinimum   string `json:"brokerage_minimum"`
	SlippageBPS        string `json:"slippage_bps"`
	SpreadBPS          string `json:"spread_bps"`
	GiveUpSessions     int    `json:"give_up_sessions"`
}

func decodeParameters(raw []byte, configuration *Configuration) error {
	var document parameterDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return fmt.Errorf("decode the configuration: %w", err)
	}
	configuration.Strategy = StrategyRef{Name: document.StrategyName, Version: document.StrategyVersion}
	configuration.UniverseCode = document.UniverseCode
	configuration.From = SessionDate(document.From)
	configuration.To = SessionDate(document.To)
	configuration.StartingCapital = document.StartingCapital
	configuration.AccountingCurrency = document.AccountingCurrency
	configuration.SizingRule = SizingRule(document.SizingRule)
	configuration.SizingN = document.SizingN
	configuration.Rebalance = Rebalance(document.Rebalance)
	configuration.Costs = Costs{
		BrokerageBasisPoints: document.BrokerageBPS,
		BrokerageMinimum:     document.BrokerageMinimum,
		SlippageBasisPoints:  document.SlippageBPS,
		SpreadBasisPoints:    document.SpreadBPS,
	}
	configuration.GiveUpSessions = document.GiveUpSessions
	return nil
}

// inputs is everything one run reads, held in memory for the duration of the simulation.
//
// Loading it up front rather than querying per session is what keeps the run inside its budget,
// and it has a second effect worth stating: the simulation cannot accidentally observe a change
// made while it is running, so what it read is exactly what the run records having read.
type inputs struct {
	strategyID UUID
	universeID UUID

	// calendar is every session any universe instrument traded on, in order. It is the union of
	// four market calendars, so a Swedish holiday does not stop Helsinki from being valued.
	calendar []SessionDate
	index    map[SessionDate]int

	instruments []instrument
	byID        map[UUID]*instrument

	// rates are keyed by the quote currency; the base is always the accounting currency.
	rates map[string]map[SessionDate]dec

	benchmarks []benchmark

	signals map[SessionDate][]signal

	signalsComputedThrough *time.Time
	barsObservedThrough    *time.Time
}

type instrument struct {
	id       UUID
	ticker   string
	currency string
	mic      string
	// traded are the sessions this instrument actually has a bar for, in order. The whole
	// no-lookahead rule reduces to reading this and never the calendar.
	traded []SessionDate
	opens  map[SessionDate]dec
	closes map[SessionDate]dec
}

type benchmark struct {
	id       UUID
	code     string
	mic      string
	currency string
	sessions []SessionDate
	closes   map[SessionDate]dec
}

type signal struct {
	instrumentID UUID
	score        dec
	scored       bool
}

// Inputs loads every stored fact one run reads. It performs no provider call, by construction:
// there is no provider in this package.
func (r *Repository) Inputs(ctx context.Context, configuration Configuration) (*inputs, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	loaded := &inputs{
		index:   map[SessionDate]int{},
		byID:    map[UUID]*instrument{},
		rates:   map[string]map[SessionDate]dec{},
		signals: map[SessionDate][]signal{},
	}

	if err := r.pool.QueryRow(ctx, `SELECT id::text FROM research_universes WHERE code = $1 AND active`,
		configuration.UniverseCode).Scan((*string)(&loaded.universeID)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: universe %q", ErrNotFound, configuration.UniverseCode)
		}
		return nil, fmt.Errorf("read the universe: %w", err)
	}
	if err := r.pool.QueryRow(ctx, `SELECT id::text FROM strategies WHERE name = $1 AND version = $2`,
		configuration.Strategy.Name, configuration.Strategy.Version).
		Scan((*string)(&loaded.strategyID)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: strategy %s v%d", ErrNotFound,
				configuration.Strategy.Name, configuration.Strategy.Version)
		}
		return nil, fmt.Errorf("read the strategy: %w", err)
	}

	from, to := configuration.From, configuration.To

	rows, err := r.pool.Query(ctx, `SELECT i.id::text, i.ticker, i.currency, e.mic
		FROM universe_memberships m
		JOIN instruments i ON i.id = m.instrument_id
		JOIN exchanges e ON e.id = i.exchange_id
		WHERE m.universe_id = $1 AND m.included_to IS NULL
		ORDER BY i.id::text`, loaded.universeID.String())
	if err != nil {
		return nil, fmt.Errorf("read the universe members: %w", err)
	}
	for rows.Next() {
		var member instrument
		if err := rows.Scan((*string)(&member.id), &member.ticker, &member.currency, &member.mic); err != nil {
			rows.Close()
			return nil, err
		}
		member.opens, member.closes = map[SessionDate]dec{}, map[SessionDate]dec{}
		loaded.instruments = append(loaded.instruments, member)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(loaded.instruments) == 0 {
		return nil, fmt.Errorf("%w: universe %q has no members", ErrNoData, configuration.UniverseCode)
	}
	identifiers := make([]string, 0, len(loaded.instruments))
	for index := range loaded.instruments {
		member := &loaded.instruments[index]
		loaded.byID[member.id] = member
		identifiers = append(identifiers, member.id.String())
	}

	if err := r.loadBars(ctx, loaded, identifiers, from, to); err != nil {
		return nil, err
	}
	if len(loaded.calendar) == 0 {
		return nil, fmt.Errorf("%w: no bars between %s and %s", ErrNoData, from, to)
	}
	if err := r.loadSignals(ctx, loaded, identifiers); err != nil {
		return nil, err
	}
	if err := r.loadRates(ctx, loaded, configuration.AccountingCurrency); err != nil {
		return nil, err
	}
	if err := r.loadBenchmarks(ctx, loaded); err != nil {
		return nil, err
	}
	return loaded, nil
}

func (r *Repository) loadBars(ctx context.Context, loaded *inputs, identifiers []string, from, to SessionDate) error {
	rows, err := r.pool.Query(ctx, `SELECT instrument_id::text, session_date::text, open::text, close::text
		FROM daily_price_bars
		WHERE instrument_id = ANY($1::uuid[])
		  AND ($2 = '' OR session_date >= $2::date)
		  AND ($3 = '' OR session_date <= $3::date)
		ORDER BY session_date, instrument_id::text`,
		identifiers, from.String(), to.String())
	if err != nil {
		return fmt.Errorf("read the bars: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, session, open, close string
		if err := rows.Scan(&id, &session, &open, &close); err != nil {
			return err
		}
		member := loaded.byID[UUID(id)]
		date := SessionDate(session)
		if _, seen := loaded.index[date]; !seen {
			loaded.index[date] = len(loaded.calendar)
			loaded.calendar = append(loaded.calendar, date)
		}
		openValue, err := parseDec(open)
		if err != nil {
			return fmt.Errorf("bar open for %s on %s: %w", id, session, err)
		}
		closeValue, err := parseDec(close)
		if err != nil {
			return fmt.Errorf("bar close for %s on %s: %w", id, session, err)
		}
		member.traded = append(member.traded, date)
		member.opens[date] = openValue
		member.closes[date] = closeValue
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return r.pool.QueryRow(ctx, `SELECT max(last_observed_at) FROM daily_price_bars
		WHERE instrument_id = ANY($1::uuid[])
		  AND ($2 = '' OR session_date >= $2::date) AND ($3 = '' OR session_date <= $3::date)`,
		identifiers, from.String(), to.String()).Scan(&loaded.barsObservedThrough)
}

// loadSignals reads what the strategy said. Signals are read, never recomputed: strategy
// behaviour has one implementation, and a simulation that forked it would be measuring a
// different strategy while reporting this one's name.
func (r *Repository) loadSignals(ctx context.Context, loaded *inputs, identifiers []string) error {
	first, last := loaded.calendar[0], loaded.calendar[len(loaded.calendar)-1]
	rows, err := r.pool.Query(ctx, `SELECT session_date::text, instrument_id::text, score::text
		FROM signals
		WHERE strategy_id = $1 AND instrument_id = ANY($2::uuid[])
		  AND session_date >= $3::date AND session_date <= $4::date
		-- A total order: score descending, then identity, so ties never depend on the plan the
		-- database happened to choose.
		ORDER BY session_date, score DESC NULLS LAST, instrument_id::text`,
		loaded.strategyID.String(), identifiers, first.String(), last.String())
	if err != nil {
		return fmt.Errorf("read the signals: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var session, id string
		var score *string
		if err := rows.Scan(&session, &id, &score); err != nil {
			return err
		}
		entry := signal{instrumentID: UUID(id)}
		if score != nil {
			value, err := parseDec(*score)
			if err != nil {
				return fmt.Errorf("signal score for %s on %s: %w", id, session, err)
			}
			entry.score, entry.scored = value, true
		}
		date := SessionDate(session)
		loaded.signals[date] = append(loaded.signals[date], entry)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return r.pool.QueryRow(ctx, `SELECT max(computed_at) FROM signals
		WHERE strategy_id = $1 AND instrument_id = ANY($2::uuid[])
		  AND session_date >= $3::date AND session_date <= $4::date`,
		loaded.strategyID.String(), identifiers, first.String(), last.String()).
		Scan(&loaded.signalsComputedThrough)
}

func (r *Repository) loadRates(ctx context.Context, loaded *inputs, accounting string) error {
	rows, err := r.pool.Query(ctx, `SELECT quote, session_date::text, rate::text
		FROM fx_rates WHERE base = $1 ORDER BY quote, session_date`, accounting)
	if err != nil {
		return fmt.Errorf("read the rates: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var quote, session, rate string
		if err := rows.Scan(&quote, &session, &rate); err != nil {
			return err
		}
		value, err := parseDec(rate)
		if err != nil {
			return fmt.Errorf("rate %s/%s on %s: %w", accounting, quote, session, err)
		}
		if loaded.rates[quote] == nil {
			loaded.rates[quote] = map[SessionDate]dec{}
		}
		loaded.rates[quote][SessionDate(session)] = value
	}
	return rows.Err()
}

// loadBenchmarks reads the series for the markets the universe actually spans. A Swedish holding
// compared against a Norwegian index would be a claim about a relationship that does not exist.
func (r *Repository) loadBenchmarks(ctx context.Context, loaded *inputs) error {
	markets := map[string]bool{}
	list := make([]string, 0, 4)
	for index := range loaded.instruments {
		if mic := loaded.instruments[index].mic; !markets[mic] {
			markets[mic] = true
			list = append(list, mic)
		}
	}
	rows, err := r.pool.Query(ctx, `SELECT id::text, code, mic, currency FROM benchmark_series
		WHERE mic = ANY($1::text[]) ORDER BY code`, list)
	if err != nil {
		return fmt.Errorf("read the benchmark series: %w", err)
	}
	for rows.Next() {
		var series benchmark
		if err := rows.Scan((*string)(&series.id), &series.code, &series.mic, &series.currency); err != nil {
			rows.Close()
			return err
		}
		series.closes = map[SessionDate]dec{}
		loaded.benchmarks = append(loaded.benchmarks, series)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for index := range loaded.benchmarks {
		series := &loaded.benchmarks[index]
		points, err := r.pool.Query(ctx, `SELECT session_date::text, close::text
			FROM benchmark_points WHERE series_id = $1 ORDER BY session_date`, series.id.String())
		if err != nil {
			return fmt.Errorf("read benchmark %s: %w", series.code, err)
		}
		for points.Next() {
			var session, close string
			if err := points.Scan(&session, &close); err != nil {
				points.Close()
				return err
			}
			value, err := parseDec(close)
			if err != nil {
				points.Close()
				return fmt.Errorf("benchmark %s on %s: %w", series.code, session, err)
			}
			date := SessionDate(session)
			series.sessions = append(series.sessions, date)
			series.closes[date] = value
		}
		points.Close()
		if err := points.Err(); err != nil {
			return err
		}
	}
	return nil
}

// result is everything one run produces, written in a single transaction.
type result struct {
	run       Run
	trades    []Trade
	positions []Position
	equity    []EquityPoint
	measures  []Measures
	skips     []Skip
}

// Write commits a whole result and publishes its event on the same transaction.
//
// One transaction because a half-written backtest is not a smaller result, it is a wrong one: a
// reader who found the trades without the equity, or the equity without the costs, would draw a
// conclusion the simulation never reached.
func (r *Repository) Write(ctx context.Context, outcome result) error {
	if err := r.ready(); err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	run := outcome.run
	if _, err := tx.Exec(ctx, `INSERT INTO backtest_runs
		(id, configuration_id, status, from_session, to_session, strategy_id,
		 signals_computed_through, bars_observed_through, started_at, finished_at,
		 trade_count, skip_count, rebalance_count, error_code, error_summary, app_version)
		VALUES ($1,$2,$3,$4::date,$5::date,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		run.ID.String(), run.ConfigurationID.String(), string(run.Status), run.From.String(),
		run.To.String(), run.StrategyID.String(), run.SignalsComputedThrough,
		run.BarsObservedThrough, run.StartedAt, run.FinishedAt, run.TradeCount, run.SkipCount,
		run.RebalanceCount, nullable(run.ErrorCode), nullable(run.ErrorSummary),
		run.AppVersion); err != nil {
		return fmt.Errorf("write the run: %w", err)
	}

	if err := writeTrades(ctx, tx, outcome.trades); err != nil {
		return err
	}
	if err := writePositions(ctx, tx, outcome.positions); err != nil {
		return err
	}
	if err := writeEquity(ctx, tx, outcome.equity); err != nil {
		return err
	}
	if err := writeMeasures(ctx, tx, outcome.measures); err != nil {
		return err
	}
	if err := writeSkips(ctx, tx, outcome.skips); err != nil {
		return err
	}

	payload, err := json.Marshal(map[string]any{
		"run_id":           run.ID.String(),
		"configuration_id": run.ConfigurationID.String(),
		"status":           string(run.Status),
		"from_session":     run.From.String(),
		"to_session":       run.To.String(),
		"trade_count":      run.TradeCount,
	})
	if err != nil {
		return fmt.Errorf("encode the completion: %w", err)
	}
	// The event carries the run, never the result: a reader loads that over REST, which keeps
	// the stream small and keeps one authorization decision in one place.
	if err := clientevents.Insert(ctx, tx, clientevents.Event{
		Type: EventCompleted, Version: 1, Scope: "shared", EntityType: "backtest_run",
		EntityID: run.ID.String(), Payload: payload, OccurredAt: time.Now().UTC(),
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func writeTrades(ctx context.Context, tx pgx.Tx, trades []Trade) error {
	if len(trades) == 0 {
		return nil
	}
	ids := make([]string, len(trades))
	runs := make([]string, len(trades))
	instrumentIDs := make([]string, len(trades))
	strategyIDs := make([]string, len(trades))
	signalSessions := make([]string, len(trades))
	executionSessions := make([]string, len(trades))
	directions := make([]string, len(trades))
	quantities := make([]string, len(trades))
	prices := make([]string, len(trades))
	currencies := make([]string, len(trades))
	rates := make([]*string, len(trades))
	brokerages := make([]string, len(trades))
	slippages := make([]string, len(trades))
	spreads := make([]string, len(trades))
	effects := make([]string, len(trades))
	for index, trade := range trades {
		ids[index] = trade.ID.String()
		runs[index] = trade.RunID.String()
		instrumentIDs[index] = trade.InstrumentID.String()
		strategyIDs[index] = trade.StrategyID.String()
		signalSessions[index] = trade.SignalSession.String()
		executionSessions[index] = trade.ExecutionSession.String()
		directions[index] = string(trade.Direction)
		quantities[index] = trade.Quantity
		prices[index] = trade.Price
		currencies[index] = trade.PriceCurrency
		rates[index] = trade.FXRate
		brokerages[index] = trade.Brokerage
		slippages[index] = trade.Slippage
		spreads[index] = trade.SpreadCost
		effects[index] = trade.CashEffect
	}
	_, err := tx.Exec(ctx, `INSERT INTO backtest_trades
		(id, run_id, instrument_id, strategy_id, signal_session, execution_session, direction,
		 quantity, price, price_currency, fx_rate, brokerage, slippage, spread_cost, cash_effect)
		SELECT id::uuid, run::uuid, instrument::uuid, strategy::uuid, signal::date, execution::date,
		       direction, quantity::numeric, price::numeric, currency, rate::numeric,
		       brokerage::numeric, slippage::numeric, spread::numeric, effect::numeric
		FROM unnest($1::text[],$2::text[],$3::text[],$4::text[],$5::text[],$6::text[],$7::text[],
		            $8::text[],$9::text[],$10::text[],$11::text[],$12::text[],$13::text[],
		            $14::text[],$15::text[])
		     AS t(id, run, instrument, strategy, signal, execution, direction, quantity, price,
		          currency, rate, brokerage, slippage, spread, effect)`,
		ids, runs, instrumentIDs, strategyIDs, signalSessions, executionSessions, directions,
		quantities, prices, currencies, rates, brokerages, slippages, spreads, effects)
	if err != nil {
		return fmt.Errorf("write the trades: %w", err)
	}
	return nil
}

func writePositions(ctx context.Context, tx pgx.Tx, positions []Position) error {
	if len(positions) == 0 {
		return nil
	}
	runs := make([]string, len(positions))
	sessions := make([]string, len(positions))
	instrumentIDs := make([]string, len(positions))
	quantities := make([]string, len(positions))
	prices := make([]*string, len(positions))
	priceSessions := make([]*string, len(positions))
	rates := make([]*string, len(positions))
	values := make([]*string, len(positions))
	reasons := make([]*string, len(positions))
	for index, position := range positions {
		runs[index] = position.RunID.String()
		sessions[index] = position.SessionDate.String()
		instrumentIDs[index] = position.InstrumentID.String()
		quantities[index] = position.Quantity
		prices[index] = position.Price
		if position.PriceSession != nil {
			value := position.PriceSession.String()
			priceSessions[index] = &value
		}
		rates[index] = position.FXRate
		values[index] = position.Value
		if position.AbsenceReason != nil {
			value := string(*position.AbsenceReason)
			reasons[index] = &value
		}
	}
	_, err := tx.Exec(ctx, `INSERT INTO backtest_positions
		(run_id, session_date, instrument_id, quantity, price, price_session, fx_rate, value, absence_reason)
		SELECT run::uuid, session::date, instrument::uuid, quantity::numeric, price::numeric,
		       price_session::date, rate::numeric, value::numeric, reason
		FROM unnest($1::text[],$2::text[],$3::text[],$4::text[],$5::text[],$6::text[],$7::text[],
		            $8::text[],$9::text[])
		     AS p(run, session, instrument, quantity, price, price_session, rate, value, reason)`,
		runs, sessions, instrumentIDs, quantities, prices, priceSessions, rates, values, reasons)
	if err != nil {
		return fmt.Errorf("write the positions: %w", err)
	}
	return nil
}

func writeEquity(ctx context.Context, tx pgx.Tx, points []EquityPoint) error {
	if len(points) == 0 {
		return nil
	}
	runs := make([]string, len(points))
	sessions := make([]string, len(points))
	cash := make([]string, len(points))
	positionValues := make([]*string, len(points))
	totals := make([]*string, len(points))
	reasons := make([]*string, len(points))
	for index, point := range points {
		runs[index] = point.RunID.String()
		sessions[index] = point.SessionDate.String()
		cash[index] = point.Cash
		positionValues[index] = point.PositionValue
		totals[index] = point.Total
		if point.AbsenceReason != nil {
			value := string(*point.AbsenceReason)
			reasons[index] = &value
		}
	}
	_, err := tx.Exec(ctx, `INSERT INTO backtest_equity
		(run_id, session_date, cash, position_value, total, absence_reason)
		SELECT run::uuid, session::date, cash::numeric, position_value::numeric, total::numeric, reason
		FROM unnest($1::text[],$2::text[],$3::text[],$4::text[],$5::text[],$6::text[])
		     AS e(run, session, cash, position_value, total, reason)`,
		runs, sessions, cash, positionValues, totals, reasons)
	if err != nil {
		return fmt.Errorf("write the equity curve: %w", err)
	}
	return nil
}

func writeMeasures(ctx context.Context, tx pgx.Tx, measures []Measures) error {
	for _, measure := range measures {
		var seriesID, from, to, reason *string
		if measure.BenchmarkSeriesID != nil {
			value := measure.BenchmarkSeriesID.String()
			seriesID = &value
		}
		if measure.From != nil {
			value := measure.From.String()
			from = &value
		}
		if measure.To != nil {
			value := measure.To.String()
			to = &value
		}
		if measure.AbsenceReason != nil {
			value := string(*measure.AbsenceReason)
			reason = &value
		}
		if _, err := tx.Exec(ctx, `INSERT INTO backtest_measures
			(run_id, subject, benchmark_series_id, from_session, to_session, total_return,
			 annualised_return, volatility, max_drawdown, trade_count, total_costs, absence_reason)
			VALUES ($1,$2,$3::uuid,$4::date,$5::date,$6::numeric,$7::numeric,$8::numeric,
			        $9::numeric,$10,$11::numeric,$12)`,
			measure.RunID.String(), measure.Subject, seriesID, from, to, measure.TotalReturn,
			measure.AnnualisedReturn, measure.Volatility, measure.MaxDrawdown, measure.TradeCount,
			measure.TotalCosts, reason); err != nil {
			return fmt.Errorf("write the measures for %s: %w", measure.Subject, err)
		}
	}
	return nil
}

func writeSkips(ctx context.Context, tx pgx.Tx, skips []Skip) error {
	if len(skips) == 0 {
		return nil
	}
	runs := make([]string, len(skips))
	instrumentIDs := make([]string, len(skips))
	sessions := make([]string, len(skips))
	reasons := make([]string, len(skips))
	for index, skip := range skips {
		runs[index] = skip.RunID.String()
		instrumentIDs[index] = skip.InstrumentID.String()
		sessions[index] = skip.SignalSession.String()
		reasons[index] = string(skip.Reason)
	}
	_, err := tx.Exec(ctx, `INSERT INTO backtest_skips (run_id, instrument_id, signal_session, reason)
		SELECT run::uuid, instrument::uuid, session::date, reason
		FROM unnest($1::text[],$2::text[],$3::text[],$4::text[]) AS s(run, instrument, session, reason)`,
		runs, instrumentIDs, sessions, reasons)
	if err != nil {
		return fmt.Errorf("write the skipped signals: %w", err)
	}
	return nil
}
