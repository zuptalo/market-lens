// Package intents records what a person is considering and reports what it would do.
//
// It is the closest this product comes to telling somebody what to do, which is why the authorship
// runs the other way: the person writes the intent and the product evaluates it. The vision
// describes a risk engine filtering recommendations a strategy generated, and feature 021 then
// measured that strategy and found it lost to two of its three benchmarks over ten years.
// Generating trades from it here would contradict every other refusal in this codebase, in the one
// place the contradiction costs money.
//
// Nothing here is an order. There is no venue, no order type, no time in force and no destination —
// absences the migration test enforces, because an intent carrying one of them would be a field
// away from being transmissible.
package intents

import (
	"time"

	"market-lens/server/internal/instruments"
	"market-lens/server/internal/risk"
)

type UUID = instruments.UUID

// EventChanged is published in the same transaction that records, withdraws or settles an intent,
// scoped to its owner alone. It carries the intent's identity, never its consequence — that is
// computed when it is read.
const EventChanged = "order_intents.changed.v1"

// Direction is what the person is considering doing. No short: selling more than is held produces a
// negative resulting position, which is reported rather than refused.
type Direction string

const (
	DirectionBuy  Direction = "buy"
	DirectionSell Direction = "sell"
)

// Status is what became of an intent. None of the three implies transmission.
type Status string

const (
	StatusConsidering Status = "considering"
	StatusWithdrawn   Status = "withdrawn"
	// StatusActedOn records that the person acted. It does not record a trade: what they actually
	// paid is a fact only they can assert, and feature 022 is where they assert it.
	StatusActedOn Status = "acted_on"
)

// Intent is one thing a person is considering.
type Intent struct {
	ID           UUID
	UserID       UUID
	InstrumentID UUID
	Ticker       string
	Name         string
	Currency     string
	Direction    Direction
	Quantity     string
	// Price is what the person expects to pay, per share, in the instrument's own currency.
	Price      string
	Costs      string
	Status     Status
	RecordedAt time.Time
	SettledAt  *time.Time
	// Consequence is what acting on it today would do. Nil once the intent is settled: the question
	// has stopped being asked.
	Consequence *Consequence
}

// AbsenceReason says why a consequence could not be computed.
type AbsenceReason string

const (
	// AbsencePortfolioIncomplete: a holding could not be priced, so there is no total to measure a
	// share against — the same rule feature 023 applies to a limit.
	AbsencePortfolioIncomplete AbsenceReason = "portfolio_incomplete"
	// AbsenceNoPrice: the instrument itself has no stored price.
	AbsenceNoPrice AbsenceReason = "no_price"
)

// Consequence is what an intent would do, computed now.
type Consequence struct {
	// ResultingQuantity may be negative, when an intent proposes selling more than is held. That is
	// reported rather than refused: an intent is a thought, and a notebook that refused a thought
	// would be a strange notebook.
	ResultingQuantity string
	ResultingValue    *string
	ResultingShare    *string
	// Denominator is the portfolio total the share was measured against, so it can be checked.
	Denominator   *string
	AbsenceReason *AbsenceReason
	// Limits is what each of the person's stated limits would say afterwards.
	Limits []risk.Evaluation
}

// Report is everything a person is considering, and what each would do.
type Report struct {
	Intents []Intent
	// EvaluatedIndependently is always true. Each intent is measured against the portfolio as it
	// stands, never against another — combining them would need an order of application nobody
	// stated, and would report a consequence nobody proposed.
	EvaluatedIndependently bool
	// RecordsWhatYouAreConsidering is always true and always present. The product records what
	// somebody is considering and offers no advice.
	RecordsWhatYouAreConsidering bool
}
