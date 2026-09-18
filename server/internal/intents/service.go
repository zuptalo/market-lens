package intents

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"market-lens/server/internal/portfolio"
	"market-lens/server/internal/risk"
)

// ErrNotFound is what somebody gets for an intent that is not theirs, and for one that does not
// exist — the same answer, so a response never confirms which identifiers are real.
var ErrNotFound = errors.New("not found")

// Refusal is a recording the product declined, with what to do about it.
type Refusal struct {
	Code    string
	Message string
}

func (r Refusal) Error() string { return r.Message }

const (
	RefusalInstrumentNotCarried = "instrument_not_carried"
	RefusalInvalidQuantity      = "invalid_quantity"
	RefusalInvalidPrice         = "invalid_price"
	RefusalAlreadySettled       = "already_settled"
	RefusalUnknownStatus        = "unknown_status"
)

// RecordRequest is one thing a person is considering.
//
// There is no venue, no order type and no time in force, and there is nowhere to put one. The
// absence is the safeguard.
type RecordRequest struct {
	InstrumentID UUID
	Direction    Direction
	Quantity     string
	Price        string
	Costs        string
}

// Portfolio and Risk are the halves of features 022 and 023 this package reads. Interfaces rather
// than concrete services, so the dependency is visibly one way: intents measure what those report
// and can change neither.
type Portfolio interface {
	View(ctx context.Context, userID string) (portfolio.View, error)
	ValueOf(ctx context.Context, userID string, instrumentID UUID, quantity string) (portfolio.Valuation, error)
}

type Risk interface {
	Limits(ctx context.Context, userID string) ([]risk.Limit, error)
}

// Service records what a person is considering and reports what it would do.
type Service struct {
	repository *Repository
	portfolio  Portfolio
	risk       Risk
	logger     *slog.Logger
}

func NewService(repository *Repository, holdings Portfolio, limits Risk, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{repository: repository, portfolio: holdings, risk: limits, logger: logger}
}

func (s *Service) ready(userID string) error {
	if s == nil || s.repository == nil || s.portfolio == nil || s.risk == nil {
		return errors.New("intents service is not configured")
	}
	if strings.TrimSpace(userID) == "" {
		return errors.New("an intent is always somebody's")
	}
	return nil
}

// Record writes down one thing a person is considering.
//
// It refuses an instrument the product does not carry, and a quantity or price that cannot mean
// anything. It does **not** refuse a sale larger than the position: that is a consequence to report,
// and refusing to let somebody write down a thought they are having would be a strange thing for a
// notebook to do.
func (s *Service) Record(ctx context.Context, userID string, request RecordRequest) (Intent, error) {
	if err := s.ready(userID); err != nil {
		return Intent{}, err
	}
	quantity, err := parseDec(request.Quantity)
	if err != nil || quantity.Sign() <= 0 {
		return Intent{}, Refusal{Code: RefusalInvalidQuantity,
			Message: "A quantity is a positive number of shares."}
	}
	price, err := parseDec(request.Price)
	if err != nil || price.Sign() <= 0 {
		return Intent{}, Refusal{Code: RefusalInvalidPrice, Message: "A price is a positive amount."}
	}
	costs := decZero
	if strings.TrimSpace(request.Costs) != "" {
		if costs, err = parseDec(request.Costs); err != nil || costs.Sign() < 0 {
			return Intent{}, Refusal{Code: RefusalInvalidPrice,
				Message: "Expected costs cannot be negative."}
		}
	}
	if request.Direction != DirectionBuy && request.Direction != DirectionSell {
		return Intent{}, Refusal{Code: RefusalInvalidQuantity,
			Message: "An intent is to buy or to sell."}
	}

	carried, err := s.repository.InstrumentIsCarried(ctx, request.InstrumentID.String())
	if err != nil {
		return Intent{}, err
	}
	if !carried {
		return Intent{}, Refusal{Code: RefusalInstrumentNotCarried,
			Message: "This product tracks a curated universe of Nordic listings and does not carry that instrument."}
	}

	return s.repository.Insert(ctx, userID, request, quantity.String(), price.String(), costs.String())
}

// Settle withdraws an intent, or records that the person acted on it.
func (s *Service) Settle(ctx context.Context, userID, intentID string, status Status) error {
	if err := s.ready(userID); err != nil {
		return err
	}
	if status != StatusWithdrawn && status != StatusActedOn {
		return Refusal{Code: RefusalUnknownStatus,
			Message: "An intent is withdrawn or acted on."}
	}
	existing, err := s.repository.Intent(ctx, userID, intentID)
	if err != nil {
		return err
	}
	if existing.Status != StatusConsidering {
		return Refusal{Code: RefusalAlreadySettled,
			Message: "This intent is no longer under consideration."}
	}
	// Acting on an intent records that the person acted. It creates no trade: what they actually
	// paid is a fact only they can assert, and the portfolio is where they assert it.
	return s.repository.Settle(ctx, userID, intentID, status)
}

// Report is everything the person is considering, with what each would do.
func (s *Service) Report(ctx context.Context, userID string, includeSettled bool) (Report, error) {
	if err := s.ready(userID); err != nil {
		return Report{}, err
	}
	stored, err := s.repository.List(ctx, userID, includeSettled)
	if err != nil {
		return Report{}, err
	}

	report := Report{Intents: make([]Intent, 0, len(stored)),
		EvaluatedIndependently: true, RecordsWhatYouAreConsidering: true}
	if len(stored) == 0 {
		return report, nil
	}

	view, err := s.portfolio.View(ctx, userID)
	if err != nil {
		return Report{}, err
	}
	limits, err := s.risk.Limits(ctx, userID)
	if err != nil {
		return Report{}, err
	}

	for _, intent := range stored {
		if intent.Status == StatusConsidering {
			// Each against the portfolio as it stands, never against another intent.
			consequence, err := consequenceOf(ctx, userID, intent, view, limits, s.portfolio)
			if err != nil {
				return Report{}, err
			}
			intent.Consequence = &consequence
		}
		report.Intents = append(report.Intents, intent)
	}
	return report, nil
}
