// Package backtest replays stored signals over stored sessions under a stated, immutable
// configuration and records what would have happened.
//
// Three properties shape everything here. It reads stored data only — no provider call is on the
// path, which is what makes a recomputation reproducible at all. It executes at the open of the
// next session an instrument actually traded, because a signal computed from a close cannot
// honestly be acted on at that close. And it never recomputes a signal: strategy behaviour has
// one implementation, and a simulation that forked it would be measuring something else.
package backtest

import (
	"time"

	"market-lens/server/internal/instruments"
)

type (
	UUID        = instruments.UUID
	SessionDate = instruments.SessionDate
)

// EventCompleted is published in the same transaction that commits a run. It carries the run and
// its configuration, never the result: REST loads that.
const EventCompleted = "backtest.completed.v1"

// SizingRule is how capital is spread across what the strategy ranked highest.
//
// One value, deliberately. Anything cleverer is position sizing, which is a later milestone with
// its own specification, and adding a second rule here would be that milestone arriving without
// having been specified.
type SizingRule string

const SizingEqualWeightTopN SizingRule = "equal_weight_top_n"

// Rebalance is when the simulation acts on what it has been told.
type Rebalance string

const (
	RebalanceDaily   Rebalance = "daily"
	RebalanceWeekly  Rebalance = "weekly"
	RebalanceMonthly Rebalance = "monthly"
)

// Configuration is the stated, immutable set of rules a result was produced under. Held as one
// document in the store for the reason FR-002 gives: splitting it into columns would let one part
// change without publishing a version.
type Configuration struct {
	ID       UUID
	Name     string
	Version  int
	Title    string
	Intent   string
	Caveat   string
	Strategy StrategyRef
	// Universe is the curated set the strategy ranked. A backtest never picks its own.
	UniverseCode string
	// From and To bound the simulation. Empty means the stored history's own bounds, resolved
	// once at the start of a run and recorded on it, so "all of it" is still a stated range.
	From SessionDate
	To   SessionDate

	StartingCapital    string
	AccountingCurrency string
	SizingRule         SizingRule
	SizingN            int
	Rebalance          Rebalance
	Costs              Costs
	// GiveUpSessions is how long the simulation waits for an instrument to trade again before
	// recording that the trade did not happen. Without a bound, an instrument that stopped
	// trading in 2018 would fill at its next price in 2019 as though nothing had happened.
	GiveUpSessions int

	PublishedAt  time.Time
	SupersededAt *time.Time
}

// Superseded reports whether a later version has replaced this one. Its results remain readable.
func (c Configuration) Superseded() bool { return c.SupersededAt != nil }

// StrategyRef names the exact version whose signals are replayed.
type StrategyRef struct {
	Name    string
	Version int
}

// Costs are stated, never guessed. A backtest with no costs is a marketing document, which is
// why the published configuration's are non-zero and why a zero is reported rather than omitted.
type Costs struct {
	BrokerageBasisPoints string
	BrokerageMinimum     string
	SlippageBasisPoints  string
	SpreadBasisPoints    string
}

// RunStatus is the outcome of one execution of a configuration.
//
// There is no 'partial': a backtest either produced a result over its whole range or it did not.
// A half-simulated portfolio is not a smaller result, it is a wrong one.
type RunStatus string

const (
	RunStatusRunning   RunStatus = "running"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusFailed    RunStatus = "failed"
)

// Run is one execution of one configuration.
type Run struct {
	ID              UUID
	ConfigurationID UUID
	Status          RunStatus
	From            SessionDate
	To              SessionDate
	StrategyID      UUID
	// What the run read. A later correction to a bar does not rewrite a stored result, so these
	// are how a reader tells that the result predates the change (FR-017).
	SignalsComputedThrough *time.Time
	BarsObservedThrough    *time.Time
	StartedAt              time.Time
	FinishedAt             *time.Time
	TradeCount             int64
	SkipCount              int64
	RebalanceCount         int64
	ErrorCode              string
	ErrorSummary           string
	AppVersion             string
}

// Direction is what a trade did. There is no short: FR-022 forbids it, and the absence is
// enforced by there being no value to write.
type Direction string

const (
	DirectionBuy  Direction = "buy"
	DirectionSell Direction = "sell"
)

// Trade is one simulated execution. Never an order, an order intent, or anything a broker could
// act on.
type Trade struct {
	ID            UUID
	RunID         UUID
	InstrumentID  UUID
	StrategyID    UUID
	SignalSession SessionDate
	// Strictly later than SignalSession, and a session the instrument actually traded on. The
	// database enforces the first and the simulation can only produce the second.
	ExecutionSession SessionDate
	Direction        Direction
	Quantity         string
	Price            string
	PriceCurrency    string
	// Nil when the listing currency is the accounting currency: a single-currency backtest
	// requires no rate and performs no conversion.
	FXRate     *string
	Brokerage  string
	Slippage   string
	SpreadCost string
	CashEffect string
}

// PositionAbsence says why a holding could not be valued at a session.
type PositionAbsence string

const (
	PositionNoPrice PositionAbsence = "no_price"
	PositionNoRate  PositionAbsence = "no_rate"
)

// Position is what was held at a session, and what it was worth — or the stated reason it could
// not be valued. Carrying the previous session's price forward would be the product asserting a
// price nobody quoted.
type Position struct {
	RunID         UUID
	SessionDate   SessionDate
	InstrumentID  UUID
	Quantity      string
	Price         *string
	FXRate        *string
	Value         *string
	AbsenceReason *PositionAbsence
}

// EquityAbsence says why a session's portfolio value is not stated.
type EquityAbsence string

const EquityPositionUnvalued EquityAbsence = "position_unvalued"

// EquityPoint is the portfolio at one session, in the accounting currency.
type EquityPoint struct {
	RunID         UUID
	SessionDate   SessionDate
	Cash          string
	PositionValue *string
	Total         *string
	AbsenceReason *EquityAbsence
}

// MeasureAbsence says why a subject reports no figures.
type MeasureAbsence string

const (
	MeasureSeriesStartsAfter    MeasureAbsence = "series_starts_after_range"
	MeasureSeriesEndsBefore     MeasureAbsence = "series_ends_before_range"
	MeasureInsufficientSessions MeasureAbsence = "insufficient_sessions"
)

// MeasureSubjectStrategy is the row reporting the simulated portfolio itself. Every other row
// names a benchmark series by its code.
const MeasureSubjectStrategy = "strategy"

// Measures are the six figures for one subject over one range — all of them or none of them.
// A result free to report a subset would report the flattering one.
type Measures struct {
	RunID             UUID
	Subject           string
	BenchmarkSeriesID *UUID
	From              *SessionDate
	To                *SessionDate
	TotalReturn       *string
	AnnualisedReturn  *string
	Volatility        *string
	// Stated as a loss, so a positive one is a sign error rather than good news.
	MaxDrawdown   *string
	TradeCount    *int64
	TotalCosts    *string
	AbsenceReason *MeasureAbsence
}

// SkipReason is why the simulation considered a signal and did nothing.
//
// A closed vocabulary, so "nothing happened" can never be the answer to why the strategy said buy
// and no trade appeared.
type SkipReason string

const (
	SkipNotSelected   SkipReason = "not_selected"
	SkipAlreadyHeld   SkipReason = "already_held"
	SkipNoCash        SkipReason = "no_cash"
	SkipNotExecutable SkipReason = "not_executable"
	SkipNoPrice       SkipReason = "no_price"
	SkipNoRate        SkipReason = "no_rate"
	SkipNoNextSession SkipReason = "no_next_session"
)

// Skip is one considered signal that produced no trade.
type Skip struct {
	RunID         UUID
	InstrumentID  UUID
	SignalSession SessionDate
	Reason        SkipReason
}

// Result is a whole stored backtest as a reader receives it.
type Result struct {
	Run           Run
	Configuration Configuration
	Measures      []Measures
}
