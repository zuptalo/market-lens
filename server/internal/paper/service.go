package paper

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"market-lens/server/internal/decimal"
	"market-lens/server/internal/intents"
	"market-lens/server/internal/portfolio"
)

// ErrNotFound is returned when a person asks for something of theirs that does not exist — and,
// deliberately, when they ask for somebody else's. The two answer identically, so a caller cannot
// learn that another person's account exists by being refused differently.
var ErrNotFound = errors.New("not found")

// Refusal is something the product declined, with a reason a person can act on.
type Refusal struct {
	Code    RefusalCode
	Message string
}

func (r Refusal) Error() string { return string(r.Code) + ": " + r.Message }

type RefusalCode string

const (
	RefusalAccountAlreadyOpen    RefusalCode = "account_already_open"
	RefusalNoAccount             RefusalCode = "no_account"
	RefusalInvalidStartingCash   RefusalCode = "invalid_starting_cash"
	RefusalUnknownCurrency       RefusalCode = "unknown_currency"
	RefusalIntentAlreadyPromoted RefusalCode = "intent_already_promoted"
	RefusalIntentNotConsidering  RefusalCode = "intent_not_considering"
	RefusalOrderNotPending       RefusalCode = "order_not_pending"
)

// Intents is what a person wrote down. An order can come from nothing else.
type Intents interface {
	Report(ctx context.Context, userID string, includeSettled bool) (intents.Report, error)
	Settle(ctx context.Context, userID, intentID string, status intents.Status) error
}

// Holdings is feature 022's service, read for the benchmark comparison a holding is measured
// against. The paper account's own positions come from its own fills.
type Holdings interface {
	View(ctx context.Context, userID string) (portfolio.View, error)
}

// OpenRequest is the terms of an account, stated once and fixed afterwards.
type OpenRequest struct {
	StartingCash       string
	AccountingCurrency string
}

// PromoteRequest turns an intent into a pending order.
type PromoteRequest struct {
	IntentID UUID
	// PlacedSession is the session the order was placed in. It fills at the open of a later one.
	PlacedSession SessionDate
}

// Service is the paper account: opening it, promoting into it, and filling what is pending.
//
// Every method takes the caller's user identifier as its first argument rather than reading a
// context, so a scoping mistake is a compile error rather than a leak. FillPending is the one
// exception, because it acts for everybody at once — and it is therefore the method whose
// isolation is asserted hardest.
type Service struct {
	repository *Repository
	intents    Intents
	holdings   Holdings
	logger     *slog.Logger
}

func NewService(repository *Repository, considered Intents, holdings Holdings, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{repository: repository, intents: considered, holdings: holdings, logger: logger}
}

func (s *Service) ready(userID string) error {
	if s == nil || s.repository == nil {
		return errors.New("paper service is not configured")
	}
	if userID == "" {
		return ErrNotFound
	}
	return nil
}

// Open creates the account. Once: its terms cannot be changed afterwards, and the database refuses
// a second one for the same person.
func (s *Service) Open(ctx context.Context, userID string, request OpenRequest) (Account, error) {
	if err := s.ready(userID); err != nil {
		return Account{}, err
	}
	if _, err := s.repository.Account(ctx, userID); err == nil {
		return Account{}, Refusal{Code: RefusalAccountAlreadyOpen,
			Message: "You already have a paper account. Its terms are fixed so the record cannot be tuned afterwards."}
	} else if !errors.Is(err, ErrNotFound) {
		return Account{}, err
	}

	cash, err := decimal.ParseDec(request.StartingCash)
	if err != nil || cash.Sign() <= 0 {
		return Account{}, Refusal{Code: RefusalInvalidStartingCash,
			Message: "A paper account starts with an amount greater than nothing."}
	}
	currency := strings.ToUpper(strings.TrimSpace(request.AccountingCurrency))
	if !currencyPattern.MatchString(currency) {
		return Account{}, Refusal{Code: RefusalUnknownCurrency,
			Message: "State the currency as three letters, such as SEK or EUR."}
	}

	// Feature 021's defaults, so a paper run and a backtest of the same strategy are comparable
	// without anybody configuring anything.
	return s.repository.Open(ctx, userID, cash.String(), currency, DefaultCostRates)
}

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

// Promote turns an intent the person wrote down into a pending order, and settles the intent as
// acted on so its own history stays truthful.
func (s *Service) Promote(ctx context.Context, userID string, request PromoteRequest) (Order, error) {
	if err := s.ready(userID); err != nil {
		return Order{}, err
	}
	account, err := s.repository.Account(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Order{}, Refusal{Code: RefusalNoAccount,
				Message: "Open a paper account before promoting anything into it."}
		}
		return Order{}, err
	}

	// The intent is read through its own service, scoped to this person. Reading it any other way
	// would be a second place that decides whose intent it is.
	report, err := s.intents.Report(ctx, userID, true)
	if err != nil {
		return Order{}, err
	}
	var found *intents.Intent
	for index := range report.Intents {
		if string(report.Intents[index].ID) == string(request.IntentID) {
			found = &report.Intents[index]
			break
		}
	}
	if found == nil {
		return Order{}, ErrNotFound
	}
	if found.Status != intents.StatusConsidering {
		return Order{}, Refusal{Code: RefusalIntentNotConsidering,
			Message: "That intent has already been settled."}
	}

	session := request.PlacedSession
	if session == "" {
		session = SessionDate(time.Now().UTC().Format("2006-01-02"))
	}
	orderID, err := s.repository.Promote(ctx, userID, string(account.ID), promotedIntent{
		id: string(found.ID), instrumentID: string(found.InstrumentID),
		direction: Direction(found.Direction), quantity: found.Quantity, price: found.Price,
	}, session)
	if err != nil {
		if isUniqueViolation(err) {
			return Order{}, Refusal{Code: RefusalIntentAlreadyPromoted,
				Message: "That intent is already in your paper account."}
		}
		return Order{}, err
	}

	orders, err := s.repository.Orders(ctx, userID)
	if err != nil {
		return Order{}, err
	}
	for _, order := range orders {
		if string(order.ID) == orderID {
			return order, nil
		}
	}
	return Order{}, ErrNotFound
}

// isUniqueViolation says whether the database refused a row because one already claims that key.
// An intent becomes at most one order, and the index rather than a read-then-write is what makes
// that true under two requests at once.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// Cancel withdraws a pending order. A filled one is permanent.
func (s *Service) Cancel(ctx context.Context, userID, orderID string) error {
	if err := s.ready(userID); err != nil {
		return err
	}
	return s.repository.Cancel(ctx, userID, orderID)
}
