package portfolio

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"market-lens/server/internal/instruments"
)

// ErrNotFound is what a person gets for a trade that is not theirs, and for one that does not
// exist. Deliberately the same answer: a distinct response would confirm which identifiers are
// real, which is a slower way of leaking the same thing.
var ErrNotFound = errors.New("not found")

// Refusal is a recording the product declined, with what to do about it.
//
// The code matters because the interface acts on it, and the message matters because a person who
// mistyped a quantity needs to know what they actually hold. "Not found" is not an answer to
// "why can I not record this".
type Refusal struct {
	Code    string
	Message string
	// Held is set on sale_exceeds_position, so the refusal names the quantity actually held.
	Held string
}

func (r Refusal) Error() string { return r.Message }

const (
	RefusalInstrumentNotCarried = "instrument_not_carried"
	RefusalSaleExceedsPosition  = "sale_exceeds_position"
	RefusalTradeDateInFuture    = "trade_date_in_future"
	RefusalUnsupportedCurrency  = "unsupported_currency"
	RefusalInvalidQuantity      = "invalid_quantity"
	RefusalInvalidPrice         = "invalid_price"
)

// RecordRequest is one trade a person asserts.
type RecordRequest struct {
	InstrumentID UUID
	Direction    Direction
	Quantity     string
	Price        string
	Costs        string
	TradeDate    SessionDate
}

// TradeQuery pages the history.
type TradeQuery struct {
	Cursor           string
	Limit            int
	IncludeWithdrawn bool
}

// TradePage is one page of what a person recorded.
type TradePage struct {
	Items      []Trade
	NextCursor string
	Total      *int64
}

// defaultCurrency is what a portfolio gets before its owner states one.
//
// The euro, because it is the only currency every stored rate converts from directly, so a
// portfolio created implicitly by recording a first trade can always be valued. The owner changes
// it whenever they like and nothing is rewritten — every figure is derived.
const defaultCurrency = "EUR"

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

// Service owns recording, correcting and reading one person's portfolio.
//
// Every method takes the caller's user identifier as its first argument rather than reading it from
// a context, so there is no way to call any of this without saying whose data it is. A scoping
// mistake becomes a compile error rather than a leak.
type Service struct {
	repository *Repository
	logger     *slog.Logger
	// now is injectable so the future-dated refusal can be tested without waiting for tomorrow.
	now func() time.Time
}

func NewService(repository *Repository, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{repository: repository, logger: logger, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) ready(userID string) error {
	if s == nil || s.repository == nil {
		return errors.New("portfolio service is not configured")
	}
	if strings.TrimSpace(userID) == "" {
		return errors.New("a portfolio is always somebody's")
	}
	return nil
}

// SetCurrency creates the portfolio on first use and states its accounting currency.
func (s *Service) SetCurrency(ctx context.Context, userID, currency string) (Portfolio, error) {
	if err := s.ready(userID); err != nil {
		return Portfolio{}, err
	}
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if !currencyPattern.MatchString(currency) {
		return Portfolio{}, Refusal{Code: RefusalUnsupportedCurrency,
			Message: "An accounting currency is a three-letter code."}
	}
	known, err := s.repository.CurrencyIsConvertible(ctx, currency)
	if err != nil {
		return Portfolio{}, err
	}
	if !known {
		// Refused rather than accepted-and-broken: a currency with no stored rate would make every
		// holding permanently unvalued, and the person would have no idea why.
		return Portfolio{}, Refusal{Code: RefusalUnsupportedCurrency,
			Message: fmt.Sprintf("This product has no stored exchange rates for %s, so it could not value anything in it.", currency)}
	}
	return s.repository.EnsurePortfolio(ctx, userID, currency)
}

// Record stores a purchase or a sale.
func (s *Service) Record(ctx context.Context, userID string, request RecordRequest) (Trade, error) {
	if err := s.ready(userID); err != nil {
		return Trade{}, err
	}
	return s.write(ctx, userID, request, "", "")
}

// Correct supersedes a recorded trade with a new version. The earlier one stays readable, because
// somebody reconciling last month's figure against a broker statement needs the version that
// produced it.
func (s *Service) Correct(ctx context.Context, userID, tradeID string, request RecordRequest) (Trade, error) {
	if err := s.ready(userID); err != nil {
		return Trade{}, err
	}
	existing, err := s.repository.Trade(ctx, userID, tradeID)
	if err != nil {
		return Trade{}, err
	}
	if existing.Status != TradeCurrent {
		return Trade{}, ErrNotFound
	}
	return s.write(ctx, userID, request, tradeID, "")
}

// Withdraw stops a trade counting and leaves it readable.
func (s *Service) Withdraw(ctx context.Context, userID, tradeID string) error {
	if err := s.ready(userID); err != nil {
		return err
	}
	existing, err := s.repository.Trade(ctx, userID, tradeID)
	if err != nil {
		return err
	}
	if existing.Status != TradeCurrent {
		return ErrNotFound
	}
	if err := s.refusesForPosition(ctx, userID, existing.InstrumentID, RecordRequest{}, tradeID); err != nil {
		return err
	}
	_, err = s.repository.WriteTrade(ctx, writeRequest{
		PortfolioID: existing.PortfolioID, UserID: userID, Withdraw: tradeID})
	return err
}

// write is the one path a recording and a correction share, so a refusal can never apply to one and
// not the other.
func (s *Service) write(ctx context.Context, userID string, request RecordRequest,
	supersede, _ string) (Trade, error) {
	quantity, err := parseDec(request.Quantity)
	if err != nil || quantity.Sign() <= 0 {
		return Trade{}, Refusal{Code: RefusalInvalidQuantity,
			Message: "A quantity is a positive number of shares."}
	}
	price, err := parseDec(request.Price)
	if err != nil || price.Sign() <= 0 {
		return Trade{}, Refusal{Code: RefusalInvalidPrice, Message: "A price is a positive amount."}
	}
	costs := decZero
	if strings.TrimSpace(request.Costs) != "" {
		if costs, err = parseDec(request.Costs); err != nil || costs.Sign() < 0 {
			return Trade{}, Refusal{Code: RefusalInvalidPrice,
				Message: "Trade costs cannot be negative."}
		}
	}
	if request.Direction != DirectionBuy && request.Direction != DirectionSell {
		return Trade{}, Refusal{Code: RefusalInvalidQuantity,
			Message: "A trade is a purchase or a sale."}
	}
	if request.TradeDate == "" {
		return Trade{}, Refusal{Code: RefusalTradeDateInFuture, Message: "A trade needs a date."}
	}
	if request.TradeDate.String() > s.now().Format("2006-01-02") {
		return Trade{}, Refusal{Code: RefusalTradeDateInFuture,
			Message: "A holding you do not have yet is not a holding."}
	}

	carried, err := s.repository.InstrumentIsCarried(ctx, request.InstrumentID.String())
	if err != nil {
		return Trade{}, err
	}
	if !carried {
		// A holding with no stored prices could never be valued or compared, so it is refused
		// rather than accepted as a permanently broken row that makes every total incomplete.
		return Trade{}, Refusal{Code: RefusalInstrumentNotCarried,
			Message: "This product tracks a curated universe of Nordic listings and does not carry that instrument."}
	}

	if err := s.refusesForPosition(ctx, userID, request.InstrumentID, request, supersede); err != nil {
		return Trade{}, err
	}

	held, err := s.repository.Portfolio(ctx, userID)
	if errors.Is(err, ErrNotFound) {
		if held, err = s.repository.EnsurePortfolio(ctx, userID, defaultCurrency); err != nil {
			return Trade{}, err
		}
	} else if err != nil {
		return Trade{}, err
	}

	id, err := instruments.NewUUID()
	if err != nil {
		return Trade{}, err
	}
	trade := Trade{ID: id, PortfolioID: held.ID, UserID: UUID(userID),
		InstrumentID: request.InstrumentID, Direction: request.Direction,
		Quantity: quantity.String(), Price: price.String(), Costs: costs.String(),
		TradeDate: request.TradeDate}

	if _, err := s.repository.WriteTrade(ctx, writeRequest{
		PortfolioID: held.ID, UserID: userID, Insert: &trade, Supersede: supersede}); err != nil {
		return Trade{}, err
	}
	s.logger.Info("portfolio trade recorded", "user", userID, "instrument",
		request.InstrumentID.String(), "direction", string(request.Direction),
		"change", describeChange(writeRequest{Supersede: supersede}))
	// Read it back rather than returning what was sent. The stored row carries the sequence the
	// database allocated and the instrument's own ticker and currency, and a caller that received a
	// struct assembled from the request would be looking at a slightly different thing from the one
	// every later read returns.
	return s.repository.Trade(ctx, userID, trade.ID.String())
}

// refusesForPosition rejects anything that would make a position negative.
//
// It is applied to a recording, a correction and a withdrawal alike, because all three can do it: a
// sale bigger than the holding, a purchase corrected down below what a later sale consumed, or a
// purchase withdrawn out from under one. There is no shorting here, so a negative position is a
// data-entry error rather than a trade.
func (s *Service) refusesForPosition(ctx context.Context, userID string, instrument UUID,
	request RecordRequest, excluding string) error {
	trades, err := s.repository.CurrentTrades(ctx, userID)
	if err != nil {
		return err
	}
	var relevant []Trade
	for _, trade := range trades {
		if trade.InstrumentID != instrument || trade.ID.String() == excluding {
			continue
		}
		relevant = append(relevant, trade)
	}
	if request.Quantity != "" {
		relevant = append(relevant, Trade{InstrumentID: instrument, Direction: request.Direction,
			Quantity: request.Quantity, TradeDate: request.TradeDate})
	}

	held, err := heldAfter(relevant)
	if err != nil {
		return err
	}
	if held.Sign() < 0 {
		available, err := heldAfter(relevant[:max(0, len(relevant)-1)])
		if err != nil {
			return err
		}
		return Refusal{Code: RefusalSaleExceedsPosition, Held: available.String(),
			Message: fmt.Sprintf("That is more than you hold. You hold %s.", trimTrailingZeros(available.String()))}
	}
	return nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// trimTrailingZeros renders a stored twelve-place decimal the way a person wrote it, so a refusal
// says "you hold 100" rather than "you hold 100.000000000000".
func trimTrailingZeros(value string) string {
	if !strings.Contains(value, ".") {
		return value
	}
	value = strings.TrimRight(value, "0")
	return strings.TrimSuffix(value, ".")
}

// Trades is the history, including superseded versions so a figure can be reconciled.
func (s *Service) Trades(ctx context.Context, userID string, query TradeQuery) (TradePage, error) {
	if err := s.ready(userID); err != nil {
		return TradePage{}, err
	}
	return s.repository.ListTrades(ctx, userID, query)
}
