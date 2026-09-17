// Package portfolio records what a person actually holds and derives what it is worth.
//
// It is the first user-owned domain record in this product. Everything else stored here —
// instruments, bars, features, signals, backtests — is the same for every user, so the ownership
// boundary this package establishes is the one risk limits and order intents will inherit.
//
// Two properties shape everything below. Nothing derived is stored: a position, its cost and its
// realised result are a fold over the person's own trades, which is what makes a correction a
// re-read rather than a repair. And the arithmetic is feature 021's, reused rather than forked —
// convert by dividing through the euro, round every intermediate to the stored precision before
// the next step reads it, and state an absence with a reason rather than carrying a stale value
// forward.
package portfolio

import (
	"time"

	"market-lens/server/internal/instruments"
)

type (
	UUID        = instruments.UUID
	SessionDate = instruments.SessionDate
)

// EventChanged is published in the same transaction that records, corrects or withdraws a trade,
// scoped to its owner alone. The mechanism is feature 004's; this is its first domain use.
const EventChanged = "portfolio.changed.v1"

// CostBasisFIFO is the only basis this product offers, and it is stated on every realised figure.
//
// First-in-first-out because it is what the Nordic tax authorities expect by default, so a
// person's figure has a chance of matching their own filing — though this product computes no tax
// and claims no figure is suitable for a return.
const CostBasisFIFO = "fifo"

// Direction is what a trade did. There is no short and no leverage, which is why there is no third
// value to write.
type Direction string

const (
	DirectionBuy  Direction = "buy"
	DirectionSell Direction = "sell"
)

// TradeStatus distinguishes the version that counts from the ones kept for reconciliation.
type TradeStatus string

const (
	TradeCurrent    TradeStatus = "current"
	TradeSuperseded TradeStatus = "superseded"
	TradeWithdrawn  TradeStatus = "withdrawn"
)

// Portfolio is one person's, and its stated accounting currency.
type Portfolio struct {
	ID                 UUID
	UserID             UUID
	AccountingCurrency string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// Trade is one purchase or sale a person asserted — not a fact the product observed, which is why
// it is correctable and why the correction is visible.
type Trade struct {
	ID           UUID
	PortfolioID  UUID
	UserID       UUID
	InstrumentID UUID
	Ticker       string
	Name         string
	Currency     string
	Sector       string
	SectorName   string
	MIC          string
	Direction    Direction
	Quantity     string
	Price        string
	Costs        string
	TradeDate    SessionDate
	// Sequence is the order this trade was recorded in. Two trades on one date are consumed in
	// this order, so it is shown rather than hidden.
	Sequence   int64
	Status     TradeStatus
	Supersedes *UUID
	RecordedAt time.Time
	ChangedAt  *time.Time
}

// ValuationAbsence says why a holding could not be valued.
type ValuationAbsence string

const (
	// ValuationNoPrice: the instrument has no stored close at all.
	ValuationNoPrice ValuationAbsence = "no_price"
	// ValuationNoRate: one leg of the euro cross is missing for the session.
	ValuationNoRate ValuationAbsence = "no_rate"
)

// Valuation is what a holding is worth, or the stated reason it is not known.
type Valuation struct {
	Value *string
	// Session is where the price came from. Stated per holding because a multi-market portfolio is
	// valued at slightly different sessions, and hiding that inside one total would be the
	// dishonest option.
	Session        *SessionDate
	ConversionRate *string
	AbsenceReason  *ValuationAbsence
}

// ComparisonAbsence says why a holding cannot be compared with its market.
type ComparisonAbsence string

const (
	ComparisonSeriesUncovered ComparisonAbsence = "series_does_not_cover_window"
	ComparisonNoBenchmark     ComparisonAbsence = "no_benchmark_for_market"
	ComparisonNotValued       ComparisonAbsence = "holding_not_valued"
)

// Comparison is the holding against its own market over its own window. Not a money-weighted
// return, and it does not claim to be: a position added to over time has no single honest return
// figure without cash flows, which this product does not track.
type Comparison struct {
	Series          string
	FromSession     *SessionDate
	ToSession       *SessionDate
	HoldingReturn   *string
	BenchmarkReturn *string
	AbsenceReason   *ComparisonAbsence
}

// Holding is what is currently held in one instrument. Every field is derived.
type Holding struct {
	InstrumentID UUID
	Ticker       string
	Name         string
	Currency     string
	// Sector is feature 014's curated classification, including its explicit `unclassified` value.
	// MIC is the exchange the instrument is listed on, which is also how feature 021 chooses a
	// benchmark. Both are on the holding rather than looked up separately, because the join that
	// fetches the ticker and name has them already and two sources would be two places to get the
	// unclassified case wrong.
	Sector     string
	SectorName string
	MIC        string
	Quantity   string
	// Cost is what the shares still held actually cost under first-in-first-out, including the
	// proportion of each purchase's trade costs.
	Cost       string
	Valuation  Valuation
	Unrealised *string
	Comparison Comparison
}

// RealisedResult is profit or loss on shares that have been sold.
type RealisedResult struct {
	InstrumentID UUID
	Ticker       string
	Name         string
	Quantity     string
	Proceeds     string
	Cost         string
	Realised     string
	// CostBasis is stated on every realised figure. A realised number without its basis cannot be
	// checked against anything.
	CostBasis string
}

// ReturnAbsence is why no portfolio return is reported, and it is always this.
//
// The product knows what was bought and what it is worth; it does not know what was paid in. A
// return computed without that divides by a number it would have to invent. Saying so is the
// requirement — a silently missing figure is not.
const ReturnAbsence = "cash_is_not_tracked"

// Totals are the portfolio's figures, and the one it refuses to report.
type Totals struct {
	Value      *string
	Cost       string
	Unrealised *string
	Realised   string
	// Complete is false when any holding could not be valued. The unvaluable holding is still
	// listed: omitting it would make the total look whole.
	Complete         bool
	IncompleteReason *string
	ReturnAbsence    string
}

// View is a whole portfolio as its owner receives it.
type View struct {
	Portfolio Portfolio
	Holdings  []Holding
	Realised  []RealisedResult
	Totals    Totals
	// RecordsWhatYouEntered is always true and always present. The product records what a person
	// entered and offers no advice; a field a client must remember to add is one that will
	// eventually be missing from the one screen that mattered.
	RecordsWhatYouEntered bool
}
