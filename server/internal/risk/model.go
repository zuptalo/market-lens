// Package risk holds a person to the limits they wrote down.
//
// The distinction the whole package turns on: "you should hold no more than 25% in one company" is
// advice, and this product does not give advice. "You said 25%, you are at 41%, here is the sum" is
// somebody's own rule, applied. Everything here follows from staying on the second side of that
// line — no default limits, no suggested thresholds, no statement of what would close a gap, and no
// softening because a strategy likes the position.
//
// Nothing about an evaluation is stored. A limit's state is the person's holdings now against their
// limits now, computed on every read, so a changed limit changes every figure at once and no second
// copy can drift.
package risk

import (
	"time"

	"market-lens/server/internal/instruments"
)

type UUID = instruments.UUID

// EventChanged is published in the same transaction that states, changes or removes a limit, scoped
// to its owner alone. It carries the kind that changed, never an evaluation — those are computed
// when they are read.
const EventChanged = "risk_limits.changed.v1"

// Kind is what a limit is about. Exactly four, because these are the four the product can measure
// from what it stores.
//
// A fifth that it could not evaluate would be worse than absent: somebody would believe they were
// being held to a rule that could never report anything. Portfolio drawdown is the conspicuous
// omission, and it is structural — feature 022 values holdings only at their latest stored session
// and tracks no cash, so a portfolio value over time does not exist to measure against.
type Kind string

const (
	KindInstrumentShare Kind = "instrument_share"
	KindSectorShare     Kind = "sector_share"
	KindMarketShare     Kind = "market_share"
	KindHoldingCount    Kind = "holding_count"
)

// Kinds is every kind, in the order a screen shows them: narrowest concentration first.
var Kinds = []Kind{KindInstrumentShare, KindSectorShare, KindMarketShare, KindHoldingCount}

// IsShare reports whether a kind is measured as a proportion of the portfolio. The three that are
// divide by a total feature 022 states only when every holding could be valued; the one that is not
// counts things and needs no price at all.
func (k Kind) IsShare() bool { return k != KindHoldingCount }

// Limit is one rule a person wrote down.
type Limit struct {
	ID        UUID
	UserID    UUID
	Kind      Kind
	Threshold string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// State is what a limit says about the portfolio right now.
//
// Three values and no zero value worth having: an evaluation is constructed from an explicit branch
// rather than from a boolean that defaults to false, because the failure this feature must not have
// is reporting compliance when something could not be measured.
type State string

const (
	StateWithin      State = "within"
	StateExceeded    State = "exceeded"
	StateUnevaluable State = "unevaluable"
)

// AbsenceReason says why a limit could not be evaluated.
type AbsenceReason string

const (
	// AbsencePortfolioIncomplete: a holding could not be valued, so the total a share divides by is
	// a number feature 022 itself declines to state.
	AbsencePortfolioIncomplete AbsenceReason = "portfolio_incomplete"
	// AbsenceNothingHeld: there is nothing to measure. Not compliance.
	AbsenceNothingHeld AbsenceReason = "nothing_held"
)

// Contribution is one group that made up a measurement — an instrument, a sector or a market.
//
// The whole breakdown is reported in descending order rather than only the largest. A reader then
// sees where their money is, instead of being pointed at one holding — which would be the first
// step toward telling them what to sell.
type Contribution struct {
	Label string
	Value string
	Share string
}

// Evaluation is what one limit says about the portfolio, and the values behind it.
type Evaluation struct {
	Kind      Kind
	Threshold string
	State     State
	// Measured is the figure compared against the threshold. Nil exactly when unevaluable.
	Measured *string
	// Denominator is what a share was measured against, so the percentage can be checked rather
	// than believed. Nil for a count, which divides by nothing.
	Denominator   *string
	AbsenceReason *AbsenceReason
	Contributions []Contribution
}

// Report is every limit a person stated, and where they stand against each.
type Report struct {
	AccountingCurrency string
	Limits             []Evaluation
	// LimitsAreYourOwn is always true and always present. These are the person's rules; the product
	// neither sets them nor advises on them, and a claim a client can forget to fetch is one that
	// will eventually not be shown.
	LimitsAreYourOwn bool
}
