package strategies

import (
	"time"

	"market-lens/server/internal/instruments"
)

// UUID and SessionDate are the shared identity and calendar types, so a strategy speaks the same
// vocabulary as the engine it reads.
type (
	UUID        = instruments.UUID
	SessionDate = instruments.SessionDate
)

// Action is a stated view, never an instruction.
//
// HOLD means the strategy has a view and that view is hold. It never stands in for missing data:
// an instrument that cannot be scored records an absence with a reason instead, which the
// database enforces rather than trusting anybody to remember.
type Action string

const (
	ActionBuy    Action = "BUY"
	ActionHold   Action = "HOLD"
	ActionReduce Action = "REDUCE"
	ActionSell   Action = "SELL"
	ActionWatch  Action = "WATCH"
)

// AbsenceReason says why no view could be formed.
type AbsenceReason string

const (
	AbsenceInsufficientHistory AbsenceReason = "insufficient_history"
	AbsenceFeatureMissing      AbsenceReason = "feature_unavailable"
	AbsenceCompositeUndefined  AbsenceReason = "composite_undefined"
	AbsenceLiquidityExcluded   AbsenceReason = "liquidity_excluded"
)

// Strategy is one published, immutable version.
type Strategy struct {
	ID           UUID
	Name         string
	Version      int
	Title        string
	Intent       string
	Caveat       string
	Factors      []Factor
	Transforms   map[string]Transform
	ActionBands  []ActionBand
	MinSessions  int
	PublishedAt  time.Time
	SupersededAt *time.Time
}

// Superseded reports whether a later version has replaced this one. Its signals remain readable.
func (s Strategy) Superseded() bool { return s.SupersededAt != nil }

// TotalWeight is the sum of every factor's weight, whether or not it was available — the
// denominator of the coverage term in confidence.
func (s Strategy) TotalWeight() float64 {
	var total float64
	for _, factor := range s.Factors {
		total += factor.Weight
	}
	return total
}

// Signal is one strategy version's view of one instrument as of one session, or its stated
// refusal to form one.
type Signal struct {
	InstrumentID  UUID
	SessionDate   SessionDate
	StrategyID    UUID
	Score         *string
	Action        *Action
	Confidence    *string
	AbsenceReason *AbsenceReason
	Contributions []Contribution
	Divisor       *string
	ComputedAt    time.Time
	RunID         UUID
}

// RunKind distinguishes the three ways a computation is asked for.
type RunKind string

const (
	RunKindFull        RunKind = "full"
	RunKindIncremental RunKind = "incremental"
	RunKindStrategy    RunKind = "strategy"
)

type RunStatus string

const (
	RunStatusRunning   RunStatus = "running"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusPartial   RunStatus = "partial"
	RunStatusFailed    RunStatus = "failed"
)

// Run is one computation over a universe and a span of sessions.
type Run struct {
	ID                  UUID
	StrategyID          UUID
	Kind                RunKind
	Status              RunStatus
	UniverseID          UUID
	TriggerFeatureRunID *UUID
	StartedAt           time.Time
	FinishedAt          *time.Time
	InstrumentCount     int64
	SignalCount         int64
	FailedCount         int64
	AppVersion          string
}

type RunItemStatus string

const (
	RunItemRunning   RunItemStatus = "running"
	RunItemSucceeded RunItemStatus = "succeeded"
	RunItemFailed    RunItemStatus = "failed"
	RunItemSkipped   RunItemStatus = "skipped"
)

// RunItem is one instrument's outcome within a run.
type RunItem struct {
	RunID        UUID
	InstrumentID UUID
	Status       RunItemStatus
	FromSession  *SessionDate
	ToSession    *SessionDate
	SignalCount  int64
	ErrorCode    *string
	ErrorSummary *string
	StartedAt    time.Time
	FinishedAt   *time.Time
}

// ComputeRequest asks for one computation.
type ComputeRequest struct {
	Kind       RunKind
	Universe   string
	Workers    int
	AppVersion string
	// SinceFeatureRun scopes an incremental pass to the sessions that feature run wrote.
	SinceFeatureRun UUID
	// Strategy and Version select one published version for a strategy run.
	Strategy string
	Version  int
}

// EventSignalsChanged is published in the same transaction as the signals it describes.
const EventSignalsChanged = "signals.changed.v1"

// Change is what that event carries: which instrument, over which sessions, by which run. Never
// the signals themselves — a client re-reads them through the authorized path.
type Change struct {
	InstrumentID UUID
	FromSession  SessionDate
	ToSession    SessionDate
	RunID        UUID
	StrategyID   UUID
}
