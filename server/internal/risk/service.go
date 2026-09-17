package risk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"market-lens/server/internal/decimal"
	"market-lens/server/internal/portfolio"
)

// ErrNotFound is what somebody gets for a limit that is not theirs, and for one that does not
// exist. The same answer deliberately: a distinct response would confirm which limits other people
// have, which is a slower way of leaking the same thing.
var ErrNotFound = errors.New("not found")

// Refusal is a threshold the product declined, with what a threshold of that kind can be.
//
// The message matters. Somebody who typed 25 meaning a quarter needs to be told a share is written
// as 0.25, not that their input was invalid.
type Refusal struct {
	Code    string
	Message string
}

func (r Refusal) Error() string { return r.Message }

const (
	RefusalThresholdOutOfRange = "threshold_out_of_range"
	RefusalThresholdNotANumber = "threshold_not_a_number"
	RefusalUnknownKind         = "unknown_limit_kind"
)

// Portfolio is the half of feature 022 this package reads.
//
// An interface rather than the concrete service, so the dependency is visibly one way and visibly
// narrow: risk measures what the portfolio reports and has no way to change it.
type Portfolio interface {
	View(ctx context.Context, userID string) (portfolio.View, error)
}

// Service states, changes and removes a person's limits, and reports where they stand.
//
// Every method takes the caller's user identifier first, as feature 022's does, so there is no way
// to call any of this without saying whose rules they are.
type Service struct {
	repository *Repository
	portfolio  Portfolio
	logger     *slog.Logger
}

func NewService(repository *Repository, holdings Portfolio, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{repository: repository, portfolio: holdings, logger: logger}
}

func (s *Service) ready(userID string) error {
	if s == nil || s.repository == nil || s.portfolio == nil {
		return errors.New("risk service is not configured")
	}
	if strings.TrimSpace(userID) == "" {
		return errors.New("a limit is always somebody's")
	}
	return nil
}

// State writes one limit, replacing the one of that kind if it exists.
func (s *Service) State(ctx context.Context, userID string, kind Kind, threshold string) (Limit, error) {
	if err := s.ready(userID); err != nil {
		return Limit{}, err
	}
	if !known(kind) {
		return Limit{}, Refusal{Code: RefusalUnknownKind,
			Message: "A limit is about one instrument, one sector, one market, or how many holdings you have."}
	}
	value, err := decimal.ParseDec(strings.TrimSpace(threshold))
	if err != nil {
		return Limit{}, Refusal{Code: RefusalThresholdNotANumber,
			Message: "A threshold is a number."}
	}

	if kind.IsShare() {
		// A share above all of it is a typo, and a share of none of it is not a rule anybody could
		// satisfy. The message names the form rather than the fault, because somebody who typed 25
		// meaning a quarter needs to know it is written 0.25.
		if value.Sign() <= 0 || value.Cmp(decimal.One) > 0 {
			return Limit{}, Refusal{Code: RefusalThresholdOutOfRange,
				Message: "A share is more than 0 and at most 1 — a quarter is 0.25."}
		}
	} else {
		if value.Sign() <= 0 || value.Cmp(decimal.FromInt(value.Floor())) != 0 {
			return Limit{}, Refusal{Code: RefusalThresholdOutOfRange,
				Message: "A holding count is a whole number of holdings, at least 1."}
		}
	}

	stored, err := s.repository.Upsert(ctx, userID, kind, value.String())
	if err != nil {
		return Limit{}, err
	}
	s.logger.Info("risk limit stated", "user", userID, "kind", string(kind))
	return stored, nil
}

// Remove deletes one limit.
func (s *Service) Remove(ctx context.Context, userID string, kind Kind) error {
	if err := s.ready(userID); err != nil {
		return err
	}
	if !known(kind) {
		return ErrNotFound
	}
	return s.repository.Delete(ctx, userID, kind)
}

// Report is every limit the person stated, and where they stand against each.
//
// It reports nothing for a kind they did not state. The product publishes no defaults: a person with
// no limits has none, and a default threshold would be the product telling them what is prudent.
func (s *Service) Report(ctx context.Context, userID string) (Report, error) {
	if err := s.ready(userID); err != nil {
		return Report{}, err
	}
	limits, err := s.repository.Limits(ctx, userID)
	if err != nil {
		return Report{}, err
	}

	view, err := s.portfolio.View(ctx, userID)
	if err != nil {
		return Report{}, err
	}

	evaluations, err := EvaluateAgainst(limits, view)
	if err != nil {
		return Report{}, fmt.Errorf("evaluate limits: %w", err)
	}
	return Report{AccountingCurrency: view.Portfolio.AccountingCurrency,
		Limits: evaluations, LimitsAreYourOwn: true}, nil
}

func known(kind Kind) bool {
	for _, candidate := range Kinds {
		if candidate == kind {
			return true
		}
	}
	return false
}

// Limits reads one person's stated limits, for a caller that will evaluate them against a portfolio
// of its own construction. Exported for feature 025, which measures what a proposed trade would do.
func (s *Service) Limits(ctx context.Context, userID string) ([]Limit, error) {
	if err := s.ready(userID); err != nil {
		return nil, err
	}
	return s.repository.Limits(ctx, userID)
}
