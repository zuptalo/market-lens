// Package paper is a simulated account over stored prices.
//
// It answers the one question the rest of the product cannot: would the decisions a person actually
// made have worked? And it answers it forward, on prices nobody had seen when the order was placed.
//
// It is not a second portfolio. Feature 022 records what somebody did; this records what they would
// have done, and no figure from the two is ever added together. Three things keep that honest, and
// each is enforced rather than intended:
//
//   - An order exists only because a person promoted an intent they wrote down themselves. There is
//     no path here from a strategy, a signal or a scheduler. Feature 021 measured the strategy and
//     found it lost to two of its three benchmarks over ten years.
//   - A fill uses the open of a session *after* the one the order was placed in, so the price could
//     not have been known when the order was placed. The database refuses anything else.
//   - A fill is recorded rather than derived, because it is a statement about a moment. Re-deriving
//     it would let a corrected bar silently rewrite the account's history.
package paper

import (
	"time"

	"market-lens/server/internal/instruments"
	"market-lens/server/internal/portfolio"
)

type (
	UUID        = instruments.UUID
	SessionDate = portfolio.SessionDate
)

// EventChanged is published in the transaction that promotes, cancels or fills, scoped to the
// account's owner alone. It carries the account's identity, never its figures.
const EventChanged = "paper_account.changed.v1"

// Direction is which way an order goes. There is no short: a sale larger than the position is
// refused rather than opened the other way.
type Direction string

const (
	DirectionBuy  Direction = "buy"
	DirectionSell Direction = "sell"
)

// State is what became of an order. None of the four implies transmission anywhere.
type State string

const (
	StatePending    State = "pending"
	StateFilled     State = "filled"
	StateCancelled  State = "cancelled"
	StateUnfillable State = "unfillable"
)

// AbsenceReason says why an order could not be filled. Present exactly when the state is
// unfillable, which the database checks from both sides.
type AbsenceReason string

const (
	// AbsenceInsufficientCash: the account's cash could not cover the consideration and its costs.
	// Cash is tracked here, unlike feature 022, because an account that cannot run out of money
	// measures nothing — every order fills and position sizing stops mattering.
	AbsenceInsufficientCash AbsenceReason = "insufficient_cash"
	// AbsenceExceedsPosition: a sale larger than the paper position. A paper account cannot go short.
	AbsenceExceedsPosition AbsenceReason = "exceeds_position"
	// AbsenceNoPrice: no stored session after the placement session, for long enough to give up.
	AbsenceNoPrice AbsenceReason = "no_price"
)

// GiveUpSessions is how long a pending order waits for a price before it is abandoned.
//
// Calendar days rather than sessions, because the thing being waited for is a session that has not
// happened. Three weeks is long enough to cross a market holiday and a provider outage, and short
// enough that an order in a delisted instrument does not sit pending forever.
const GiveUpSessions = 21

// Account is one person's simulated account. Its terms are fixed when it is opened.
type Account struct {
	ID                 UUID
	UserID             UUID
	StartingCash       string
	AccountingCurrency string
	Costs              CostRates
	OpenedAt           time.Time
}

// CostRates are feature 021's cost model, stated in basis points except the minimum.
type CostRates struct {
	BrokerageBps      string
	BrokerageMinimum  string
	SlippageBps       string
	CurrencySpreadBps string
}

// DefaultCostRates match feature 021's defaults, so a paper run and a backtest of the same strategy
// are comparable without anybody configuring anything.
var DefaultCostRates = CostRates{
	BrokerageBps:      "10",
	BrokerageMinimum:  "5",
	SlippageBps:       "5",
	CurrencySpreadBps: "10",
}

// Order is something a person promoted into the account.
type Order struct {
	ID            UUID
	IntentID      UUID
	InstrumentID  UUID
	Ticker        string
	Name          string
	Currency      string
	Direction     Direction
	Quantity      string
	ExpectedPrice string
	PlacedSession SessionDate
	State         State
	AbsenceReason *AbsenceReason
	PlacedAt      time.Time
	SettledAt     *time.Time
	Fill          *Fill
}

// Fill is what an order became. Recorded, never derived.
type Fill struct {
	Session SessionDate
	// OpenPrice is the stored open of that session, unmodified. Costs are kept separately so the
	// price can be checked against the bar.
	OpenPrice      string
	Quantity       string
	Costs          string
	CashEffect     string
	ConversionRate string
	// BarDiverged is true when the bar this fill read has since been corrected. The fill is never
	// re-priced: the divergence is reported so the history stays reconcilable week to week.
	BarDiverged bool
	FilledAt    time.Time
}

// Holding is a position the account holds, derived from its fills.
type Holding struct {
	InstrumentID  UUID
	Ticker        string
	Name          string
	Currency      string
	Quantity      string
	Cost          string
	Value         *string
	Unrealised    *string
	Session       *SessionDate
	AbsenceReason *string
	Comparison    Comparison
}

// Comparison is a holding against the benchmark for its market, over its own holding period.
type Comparison struct {
	Series          string
	HoldingReturn   *string
	BenchmarkReturn *string
	AbsenceReason   *string
}

// Totals is what the account is worth and what it has done.
type Totals struct {
	Value      *string
	Cost       string
	Unrealised *string
	Realised   string
	// TotalReturn is cash plus holdings against starting cash. This is the only place in the
	// product that reports a return: feature 022 declines to, because it never saw the deposits,
	// and here every movement was recorded by this product.
	TotalReturn      *string
	Complete         bool
	IncompleteReason *string
}

// View is the whole account as a person reads it.
type View struct {
	Account  Account
	Cash     string
	Holdings []Holding
	Orders   []Order
	Totals   Totals
	// IsASimulation is always true and always present. Nothing here was traded, no order was placed
	// anywhere, and no figure here may be combined with a real holding.
	IsASimulation bool
}
