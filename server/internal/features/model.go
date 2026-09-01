// Package features owns versioned quantitative features derived from the daily history the
// marketdata package stores. It reads market data and never writes it; the package boundary
// is how that stays true.
//
// Every stored value carries the definition version that produced it, is undefined rather
// than imputed when its window is not satisfied, and is reproducible at the stored precision
// (specs/013-feature-engine, research R-001).
package features

import (
	"errors"
	"time"

	"market-lens/server/internal/instruments"
)

type UUID = instruments.UUID

type SessionDate = instruments.SessionDate

// PriceBasis names which prices a definition reads. adjusted is engine-applied from recorded
// splits as of the session being computed, never the provider's back-adjusted column
// (FR-019).
type PriceBasis string

const (
	PriceBasisRaw      PriceBasis = "raw"
	PriceBasisAdjusted PriceBasis = "adjusted"
)

// AbsenceReason states why a computed feature has no value. It is a fact about the data,
// distinct from "not yet computed", which is the absence of any row (FR-014, FR-017).
type AbsenceReason string

const (
	AbsenceInsufficientHistory AbsenceReason = "insufficient_history"
	AbsenceWindowGap           AbsenceReason = "window_gap"
	AbsenceCompositeUndefined  AbsenceReason = "composite_undefined"
	AbsenceZeroDenominator     AbsenceReason = "zero_denominator"
)

// CompositeAbsenceInsufficientContributors is the composite's own absence reason: fewer
// instruments contributed than the definition's minimum (FR-008b).
const CompositeAbsenceInsufficientContributors = "insufficient_contributors"

// CompositeDefinitionName is the one definition computed per universe and session rather than
// per instrument. It is a composite of one curated list, never an index or a benchmark
// (FR-008c).
const CompositeDefinitionName = "composite_return_1"

// Definition is one published version of a feature. Definitions are additive and never edited
// in place (FR-001, FR-022).
type Definition struct {
	ID                     UUID
	Name                   string
	Version                int
	WindowSessions         *int
	PriceBasis             PriceBasis
	Parameters             map[string]any
	UndefinedConditions    string
	SessionLengthSensitive bool
	PublishedAt            time.Time
	SupersededAt           *time.Time
}

// CompositeReference names the composite a relative-strength value was measured against and
// how many instruments contributed to it (FR-008b, SC-003a).
type CompositeReference struct {
	Composite        string
	Version          int
	ContributorCount int
}

// Value is one feature for one instrument at one session. Exactly one of Value, Label and
// AbsenceReason is set: a number, a category name, or the reason there is neither.
type Value struct {
	Name              string
	DefinitionVersion int
	WindowSessions    *int
	SessionDate       SessionDate
	Value             *string
	Label             *string
	AbsenceReason     *AbsenceReason
	Currency          *string
	ComparedTo        *CompositeReference
	ComputedAt        time.Time
}

// FeatureSet is everything defined for an instrument as of a stored session. NotComputed
// names the active definitions with no stored row — the engine has not run for them, which
// is a different fact from an undefined value.
type FeatureSet struct {
	InstrumentID UUID
	SessionDate  SessionDate
	Features     []Value
	NotComputed  []string
}

type RunKind string

const (
	RunKindFull        RunKind = "full"
	RunKindIncremental RunKind = "incremental"
	RunKindDefinition  RunKind = "definition"
)

type RunStatus string

const (
	RunStatusRunning   RunStatus = "running"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusPartial   RunStatus = "partial"
	RunStatusFailed    RunStatus = "failed"
)

// Run is one execution of the engine, mirroring marketdata's import run so the operational
// vocabulary is one vocabulary.
type Run struct {
	ID              UUID
	Kind            RunKind
	Status          RunStatus
	UniverseID      UUID
	DefinitionName  *string
	TriggerRunID    *UUID
	StartedAt       time.Time
	FinishedAt      *time.Time
	InstrumentCount int64
	ValueCount      int64
	AppVersion      string
}

type RunItemStatus string

const (
	RunItemRunning   RunItemStatus = "running"
	RunItemSucceeded RunItemStatus = "succeeded"
	RunItemFailed    RunItemStatus = "failed"
	RunItemSkipped   RunItemStatus = "skipped"
)

// RunItem is one instrument's outcome within a run. It is what makes FR-023's partial-failure
// containment observable rather than asserted.
type RunItem struct {
	RunID        UUID
	InstrumentID UUID
	Status       RunItemStatus
	FromSession  *SessionDate
	ToSession    *SessionDate
	ValueCount   int64
	ErrorCode    *string
	ErrorSummary *string
	StartedAt    time.Time
	FinishedAt   *time.Time
}

var (
	// ErrNoHistory means no bar is stored on or before the requested session, which must
	// read as "no history" rather than as a set of empty values (US2-2).
	ErrNoHistory = errors.New("no stored history on or before the requested session")
	// ErrUnknownFeature means a requested feature name is not defined; the response names
	// the features that do exist (US2-3).
	ErrUnknownFeature = errors.New("unknown feature")
	// ErrClosedDate means the requested date is not a stored open session: no session, no
	// values (FR-016).
	ErrClosedDate = errors.New("the exchange was not open on the requested date")
)
