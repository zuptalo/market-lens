package backtest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"market-lens/server/internal/instruments"
)

// RunRequest asks for one execution of one published configuration.
//
// There is nothing here to tune. The range, the universe, the sizing and the costs all come from
// the configuration, because a caller free to vary them would be free to search for the flattering
// combination — which is the practice FR-023 exists to forbid, and the reason this struct has no
// field for any of them.
type RunRequest struct {
	Configuration string
	// Version selects a superseded configuration. Zero means the current one.
	Version    int
	AppVersion string
}

// Service replays stored signals under a stated configuration and records what would have
// happened.
type Service struct {
	repository *Repository
	logger     *slog.Logger
}

func NewService(repository *Repository, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{repository: repository, logger: logger}
}

// engine is one run's state: the rules, the data it read, and the identity it writes under.
type engine struct {
	configuration Configuration
	in            *inputs
	runID         UUID

	slippage         dec
	spread           dec
	brokerageRate    dec
	brokerageMinimum dec
}

// Run executes one configuration over stored data.
//
// It reads stored data only. No provider call is on this path — there is no provider in this
// package — which is what makes a recomputation reproducible and what lets a backtest run with no
// credential and no network.
func (s *Service) Run(ctx context.Context, request RunRequest) (Run, error) {
	if s == nil || s.repository == nil {
		return Run{}, errors.New("backtest service is not configured")
	}
	if request.AppVersion == "" {
		return Run{}, errors.New("an application version is required")
	}
	configuration, err := s.repository.Configuration(ctx, request.Configuration, request.Version)
	if err != nil {
		return Run{}, err
	}
	if configuration.SizingRule != SizingEqualWeightTopN || configuration.SizingN < 1 {
		return Run{}, fmt.Errorf("configuration %s states an unsupported sizing rule %q",
			configuration.Name, configuration.SizingRule)
	}
	loaded, err := s.repository.Inputs(ctx, configuration)
	if err != nil {
		return Run{}, err
	}

	runID, err := instruments.NewUUID()
	if err != nil {
		return Run{}, err
	}
	e := &engine{configuration: configuration, in: loaded, runID: runID}
	if err := e.readCosts(); err != nil {
		return Run{}, err
	}

	started := time.Now().UTC()
	outcome, err := e.simulate(ctx)
	if err != nil {
		return Run{}, err
	}
	finished := time.Now().UTC()
	outcome.run = Run{
		ID:                     runID,
		ConfigurationID:        configuration.ID,
		Status:                 RunStatusSucceeded,
		From:                   loaded.calendar[0],
		To:                     loaded.calendar[len(loaded.calendar)-1],
		StrategyID:             loaded.strategyID,
		SignalsComputedThrough: loaded.signalsComputedThrough,
		BarsObservedThrough:    loaded.barsObservedThrough,
		StartedAt:              started,
		FinishedAt:             &finished,
		TradeCount:             int64(len(outcome.trades)),
		SkipCount:              int64(len(outcome.skips)),
		RebalanceCount:         outcome.rebalances,
		AppVersion:             request.AppVersion,
	}
	for index := range outcome.measures {
		outcome.measures[index].RunID = runID
	}
	if err := s.repository.Write(ctx, outcome.result); err != nil {
		return Run{}, err
	}
	s.logger.Info("backtest finished", "run", runID.String(), "configuration", configuration.Name,
		"version", configuration.Version, "from", outcome.run.From.String(), "to", outcome.run.To.String(),
		"trades", outcome.run.TradeCount, "skips", outcome.run.SkipCount,
		"rebalances", outcome.run.RebalanceCount, "elapsed", finished.Sub(started))
	return outcome.run, nil
}

func (e *engine) readCosts() error {
	costs := e.configuration.Costs
	for _, field := range []struct {
		name   string
		text   string
		target *dec
		factor bool
	}{
		{"slippage_bps", costs.SlippageBasisPoints, &e.slippage, true},
		{"spread_bps", costs.SpreadBasisPoints, &e.spread, true},
		{"brokerage_bps", costs.BrokerageBasisPoints, &e.brokerageRate, true},
		{"brokerage_minimum", costs.BrokerageMinimum, &e.brokerageMinimum, false},
	} {
		// A missing cost is zero, and a zero cost is reported rather than omitted. It is never
		// inferred: a configuration that forgot to state one would otherwise become the most
		// flattering configuration in the product by accident.
		text := field.text
		if text == "" {
			text = "0"
		}
		value, err := parseDec(text)
		if err != nil {
			return fmt.Errorf("configuration %s states an invalid %s: %w",
				e.configuration.Name, field.name, err)
		}
		if value.Sign() < 0 {
			return fmt.Errorf("configuration %s states a negative %s", e.configuration.Name, field.name)
		}
		if field.factor {
			value = basisPoints(value)
		}
		*field.target = value
	}
	return nil
}

// pending is one execution the simulation has decided on but cannot yet perform, because the
// session it executes on has not arrived.
type pending struct {
	sequence      int
	member        *instrument
	signalSession SessionDate
	direction     Direction
	// budget is the target position value in the accounting currency. A buy sizes against it and
	// against the cash actually available when the session arrives — which may be less, because a
	// sale that was supposed to fund it may itself have failed to execute.
	budget dec
}

type simulation struct {
	result
	rebalances int64
}

func (e *engine) simulate(ctx context.Context) (simulation, error) {
	capital, err := parseDec(e.configuration.StartingCapital)
	if err != nil {
		return simulation{}, fmt.Errorf("configuration %s states an invalid starting capital: %w",
			e.configuration.Name, err)
	}
	if capital.Sign() <= 0 {
		return simulation{}, fmt.Errorf("configuration %s starts with no capital", e.configuration.Name)
	}
	book := newPortfolio(capital)

	var outcome simulation
	queue := map[SessionDate][]pending{}
	sequence := 0

	for _, session := range e.in.calendar {
		if err := ctx.Err(); err != nil {
			return simulation{}, err
		}

		// 1. What was decided earlier and falls due today.
		due := queue[session]
		sort.Slice(due, func(i, j int) bool { return due[i].sequence < due[j].sequence })
		for _, item := range due {
			e.execute(book, item, session, &outcome)
		}
		delete(queue, session)

		// 2. What the portfolio is worth at today's close.
		valued := e.value(book, session)
		outcome.positions = append(outcome.positions, valued.positions...)
		outcome.equity = append(outcome.equity, e.equityPoint(session, book.cash, valued))

		// 3. What today's close tells the strategy, and what that means for tomorrow. Planning
		//    reads only what is knowable at this session: today's signals and today's valuation.
		if !e.isRebalance(session) {
			continue
		}
		outcome.rebalances++
		sequence = e.plan(book, session, valued, queue, sequence, &outcome)
	}

	// Anything still queued when the range ends never executed: there was no session left to
	// execute it on, and saying so is the result.
	remaining := make([]SessionDate, 0, len(queue))
	for session := range queue {
		remaining = append(remaining, session)
	}
	sort.Slice(remaining, func(i, j int) bool { return remaining[i] < remaining[j] })
	for _, session := range remaining {
		items := queue[session]
		sort.Slice(items, func(i, j int) bool { return items[i].sequence < items[j].sequence })
		for _, item := range items {
			outcome.skips = append(outcome.skips, Skip{RunID: e.runID, InstrumentID: item.member.id,
				SignalSession: item.signalSession, Reason: SkipNoNextSession})
		}
	}

	outcome.measures = e.measure(outcome.equity, outcome.trades)
	return outcome, nil
}

// isRebalance says whether the configuration's schedule trades on this session.
//
// A signal on any other session is never considered, and no row is written for it: under a
// monthly schedule that would be a quarter of a million rows saying "it was a Tuesday". The
// schedule is recorded on the run instead, which answers the same question in one fact.
func (e *engine) isRebalance(session SessionDate) bool {
	position := e.in.index[session]
	if position == 0 {
		return true
	}
	previous := e.in.calendar[position-1]
	switch e.configuration.Rebalance {
	case RebalanceDaily:
		return true
	case RebalanceWeekly:
		return isoWeek(session) != isoWeek(previous)
	case RebalanceMonthly:
		return session[:7] != previous[:7]
	default:
		return false
	}
}

func isoWeek(session SessionDate) string {
	parsed, err := time.Parse("2006-01-02", session.String())
	if err != nil {
		return session.String()
	}
	year, week := parsed.ISOWeek()
	return fmt.Sprintf("%04d-W%02d", year, week)
}

// plan decides what the portfolio should hold from this session's ranking, and queues the
// executions that get it there. It never touches cash or holdings: everything it decides happens
// on a later session, which is the whole no-lookahead rule expressed as control flow.
func (e *engine) plan(book *portfolio, session SessionDate, valued valuation,
	queue map[SessionDate][]pending, sequence int, outcome *simulation) int {
	ranked := e.in.signals[session]
	// Considered is what the strategy expressed a view on today. An instrument whose own market
	// was closed is not in it, and nothing happens to it: the alternative — selling a holding
	// because its exchange had a holiday — would be the simulation acting on the absence of
	// information as though it were information.
	considered := make(map[UUID]bool, len(ranked))
	desired := map[UUID]bool{}
	for _, entry := range ranked {
		considered[entry.instrumentID] = true
	}
	for _, entry := range ranked {
		if !entry.scored {
			continue
		}
		if len(desired) >= e.configuration.SizingN {
			break
		}
		desired[entry.instrumentID] = true
	}

	// Equal weight across N, not across however many happened to be rankable today. Dividing by
	// the number available would concentrate the whole portfolio into two names on a session when
	// only one market was open — the spec's own edge case, and a rule that would quietly make a
	// thin day the most leveraged day in the result. The shortfall stays in cash.
	target := valued.investable.Div(decFromInt(bigFromInt(e.configuration.SizingN)))

	schedule := func(member *instrument, direction Direction, attribution SessionDate, budget dec) {
		execution, reason, ok := e.nextTradedSession(member, session)
		if !ok {
			outcome.skips = append(outcome.skips, Skip{RunID: e.runID, InstrumentID: member.id,
				SignalSession: attribution, Reason: reason})
			return
		}
		sequence++
		queue[execution] = append(queue[execution], pending{sequence: sequence, member: member,
			signalSession: attribution, direction: direction, budget: budget})
	}

	// Sells are queued before buys and therefore execute first when they fall on the same
	// session, so a purchase can be funded by a sale decided at the same moment.
	selling := map[UUID]bool{}
	for _, id := range book.heldOrder() {
		if desired[id] || !considered[id] {
			continue
		}
		selling[id] = true
		schedule(e.in.byID[id], DirectionSell, session, decZero)
	}
	for _, entry := range ranked {
		if selling[entry.instrumentID] {
			// The sale *is* the outcome of not being selected. Recording a reason beside it
			// would account for the same signal twice and make "why" ambiguous.
			continue
		}
		if !desired[entry.instrumentID] {
			// Everything the ranking did not select, and everything it could not score.
			outcome.skips = append(outcome.skips, Skip{RunID: e.runID,
				InstrumentID: entry.instrumentID, SignalSession: session, Reason: SkipNotSelected})
			continue
		}
		if _, held := book.holdings[entry.instrumentID]; held {
			// Already at the weight the rule asks for. This configuration does not add to an
			// existing position, and saying that is more use than a silent absence of a trade.
			outcome.skips = append(outcome.skips, Skip{RunID: e.runID,
				InstrumentID: entry.instrumentID, SignalSession: session, Reason: SkipAlreadyHeld})
			continue
		}
		schedule(e.in.byID[entry.instrumentID], DirectionBuy, session, target)
	}
	return sequence
}

// execute performs one queued decision at the session it falls due on, or records why it did not
// happen. Exactly one of the two, so every considered signal has an outcome.
func (e *engine) execute(book *portfolio, item pending, session SessionDate, outcome *simulation) {
	skip := func(reason SkipReason) {
		outcome.skips = append(outcome.skips, Skip{RunID: e.runID, InstrumentID: item.member.id,
			SignalSession: item.signalSession, Reason: reason})
	}
	price, traded := item.member.opens[session]
	if !traded {
		skip(SkipNoPrice)
		return
	}
	rate, converted, rated := e.rate(item.member.currency, session)
	if !rated {
		skip(SkipNoRate)
		return
	}

	quantity := decZero
	switch item.direction {
	case DirectionSell:
		held, exists := book.holdings[item.member.id]
		if !exists {
			skip(SkipNotSelected)
			return
		}
		quantity = held.quantity
	case DirectionBuy:
		budget := item.budget
		if book.cash.Cmp(budget) < 0 {
			budget = book.cash
		}
		quantity = e.affordableQuantity(budget, price, rate, converted)
		if quantity.Sign() <= 0 {
			skip(SkipNoCash)
			return
		}
	}

	costs := e.costsOf(item.direction, quantity, price, rate, converted)
	if item.direction == DirectionBuy {
		// The estimate that sized the order is conservative, so this should never fire. It is
		// here because "should never" is not a guarantee, and a portfolio that spent money it did
		// not have would produce a curve nobody could have achieved.
		for quantity.Sign() > 0 && book.cash.Add(costs.cashEffect).Sign() < 0 {
			quantity = quantity.Sub(decOne)
			costs = e.costsOf(item.direction, quantity, price, rate, converted)
		}
		if quantity.Sign() <= 0 {
			skip(SkipNoCash)
			return
		}
	}

	trade := Trade{
		RunID: e.runID, InstrumentID: item.member.id, StrategyID: e.in.strategyID,
		SignalSession: item.signalSession, ExecutionSession: session, Direction: item.direction,
		Quantity: quantity.String(), Price: price.String(), PriceCurrency: item.member.currency,
		Brokerage: costs.brokerage.String(), Slippage: costs.slippage.String(),
		SpreadCost: costs.spread.String(), CashEffect: costs.cashEffect.String(),
	}
	if converted {
		rateText := rate.String()
		trade.FXRate = &rateText
	}
	id, err := instruments.NewUUID()
	if err != nil {
		skip(SkipNotExecutable)
		return
	}
	trade.ID = id

	book.cash = book.cash.Add(costs.cashEffect)
	switch item.direction {
	case DirectionBuy:
		book.holdings[item.member.id] = &holding{quantity: quantity, signalSession: item.signalSession}
	case DirectionSell:
		delete(book.holdings, item.member.id)
	}
	outcome.trades = append(outcome.trades, trade)
}
