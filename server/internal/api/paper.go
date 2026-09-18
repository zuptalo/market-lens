package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"market-lens/server/internal/httpx"
	"market-lens/server/internal/paper"
)

// PaperService is one person's simulated account.
//
// As with the portfolio, the limits and the intents, every method takes the caller's user
// identifier and no route carries one: there is no such thing as reading "the" paper account.
//
// There is deliberately no method that creates an order from a signal or a strategy. An order
// exists because a person promoted an intent they wrote down themselves, and nothing else.
type PaperService interface {
	View(ctx context.Context, userID string) (paper.View, error)
	Open(ctx context.Context, userID string, request paper.OpenRequest) (paper.Account, error)
	Promote(ctx context.Context, userID string, request paper.PromoteRequest) (paper.Order, error)
	Cancel(ctx context.Context, userID, orderID string) error
}

type paperFillResponse struct {
	Session        string `json:"fill_session"`
	OpenPrice      string `json:"open_price"`
	Quantity       string `json:"quantity"`
	Costs          string `json:"costs"`
	CashEffect     string `json:"cash_effect"`
	ConversionRate string `json:"conversion_rate"`
	// BarDiverged says the bar this fill read has since been corrected. The fill is never
	// re-priced; saying so keeps both facts, which is what makes the history reconcilable.
	BarDiverged bool      `json:"bar_diverged"`
	FilledAt    time.Time `json:"filled_at"`
}

// paperOrderResponse mirrors Order in contracts/openapi.yaml.
//
// No venue, no order type, no time in force, no destination, no limit or stop price. The absence
// is the safeguard, and the migration test asserts the columns do not exist either.
type paperOrderResponse struct {
	ID            string             `json:"id"`
	IntentID      string             `json:"intent_id"`
	InstrumentID  string             `json:"instrument_id"`
	Ticker        string             `json:"ticker"`
	Name          string             `json:"name"`
	Currency      string             `json:"currency"`
	Direction     string             `json:"direction"`
	Quantity      string             `json:"quantity"`
	ExpectedPrice string             `json:"expected_price"`
	PlacedSession string             `json:"placed_session"`
	State         string             `json:"state"`
	AbsenceReason *string            `json:"absence_reason"`
	PlacedAt      time.Time          `json:"placed_at"`
	SettledAt     *time.Time         `json:"settled_at"`
	Fill          *paperFillResponse `json:"fill"`
}

type paperComparisonResponse struct {
	Series          string  `json:"series"`
	HoldingReturn   *string `json:"holding_return"`
	BenchmarkReturn *string `json:"benchmark_return"`
	AbsenceReason   *string `json:"absence_reason"`
}

type paperHoldingResponse struct {
	InstrumentID  string                  `json:"instrument_id"`
	Ticker        string                  `json:"ticker"`
	Name          string                  `json:"name"`
	Currency      string                  `json:"currency"`
	Quantity      string                  `json:"quantity"`
	Cost          string                  `json:"cost"`
	Value         *string                 `json:"value"`
	Unrealised    *string                 `json:"unrealised"`
	Session       *string                 `json:"session"`
	AbsenceReason *string                 `json:"absence_reason"`
	Comparison    paperComparisonResponse `json:"comparison"`
}

type paperTotalsResponse struct {
	Value      *string `json:"value"`
	Cost       string  `json:"cost"`
	Unrealised *string `json:"unrealised"`
	Realised   string  `json:"realised"`
	// TotalReturn is the one return figure in this product. Feature 022 declines to state one
	// because it never saw the deposits; here every movement was recorded by this product.
	TotalReturn      *string `json:"total_return"`
	Complete         bool    `json:"complete"`
	IncompleteReason *string `json:"incomplete_reason"`
}

type paperCostsResponse struct {
	BrokerageBps      string `json:"brokerage_bps"`
	BrokerageMinimum  string `json:"brokerage_minimum"`
	SlippageBps       string `json:"slippage_bps"`
	CurrencySpreadBps string `json:"currency_spread_bps"`
}

type paperAccountResponse struct {
	StartingCash       string                 `json:"starting_cash"`
	AccountingCurrency string                 `json:"accounting_currency"`
	OpenedAt           time.Time              `json:"opened_at"`
	Costs              paperCostsResponse     `json:"costs"`
	Cash               string                 `json:"cash"`
	Holdings           []paperHoldingResponse `json:"holdings"`
	Orders             []paperOrderResponse   `json:"orders"`
	Totals             paperTotalsResponse    `json:"total"`
	// IsASimulation is always true and always present. Nothing here was traded, no order was
	// placed anywhere, and no figure here may be combined with a real holding.
	IsASimulation bool `json:"is_a_simulation"`
}

func paperViewDTO(view paper.View) paperAccountResponse {
	response := paperAccountResponse{
		StartingCash: view.Account.StartingCash, AccountingCurrency: view.Account.AccountingCurrency,
		OpenedAt: view.Account.OpenedAt, Cash: view.Cash,
		Costs: paperCostsResponse{
			BrokerageBps:      view.Account.Costs.BrokerageBps,
			BrokerageMinimum:  view.Account.Costs.BrokerageMinimum,
			SlippageBps:       view.Account.Costs.SlippageBps,
			CurrencySpreadBps: view.Account.Costs.CurrencySpreadBps,
		},
		Holdings:      make([]paperHoldingResponse, 0, len(view.Holdings)),
		Orders:        make([]paperOrderResponse, 0, len(view.Orders)),
		IsASimulation: true,
	}
	for _, holding := range view.Holdings {
		item := paperHoldingResponse{
			InstrumentID: string(holding.InstrumentID), Ticker: holding.Ticker, Name: holding.Name,
			Currency: holding.Currency, Quantity: holding.Quantity, Cost: holding.Cost,
			Value: holding.Value, Unrealised: holding.Unrealised,
			AbsenceReason: holding.AbsenceReason,
			Comparison: paperComparisonResponse{
				Series:          holding.Comparison.Series,
				HoldingReturn:   holding.Comparison.HoldingReturn,
				BenchmarkReturn: holding.Comparison.BenchmarkReturn,
				AbsenceReason:   holding.Comparison.AbsenceReason,
			},
		}
		if holding.Session != nil {
			session := string(*holding.Session)
			item.Session = &session
		}
		response.Holdings = append(response.Holdings, item)
	}
	for _, order := range view.Orders {
		response.Orders = append(response.Orders, paperOrderDTO(order))
	}
	response.Totals = paperTotalsResponse{
		Value: view.Totals.Value, Cost: view.Totals.Cost, Unrealised: view.Totals.Unrealised,
		Realised: view.Totals.Realised, TotalReturn: view.Totals.TotalReturn,
		Complete: view.Totals.Complete, IncompleteReason: view.Totals.IncompleteReason,
	}
	return response
}

func paperOrderDTO(order paper.Order) paperOrderResponse {
	item := paperOrderResponse{
		ID: string(order.ID), IntentID: string(order.IntentID),
		InstrumentID: string(order.InstrumentID), Ticker: order.Ticker, Name: order.Name,
		Currency: order.Currency, Direction: string(order.Direction), Quantity: order.Quantity,
		ExpectedPrice: order.ExpectedPrice, PlacedSession: string(order.PlacedSession),
		State: string(order.State), PlacedAt: order.PlacedAt, SettledAt: order.SettledAt,
	}
	if order.AbsenceReason != nil {
		reason := string(*order.AbsenceReason)
		item.AbsenceReason = &reason
	}
	if order.Fill != nil {
		item.Fill = &paperFillResponse{
			Session: string(order.Fill.Session), OpenPrice: order.Fill.OpenPrice,
			Quantity: order.Fill.Quantity, Costs: order.Fill.Costs,
			CashEffect: order.Fill.CashEffect, ConversionRate: order.Fill.ConversionRate,
			BarDiverged: order.Fill.BarDiverged, FilledAt: order.Fill.FilledAt,
		}
	}
	return item
}

func getPaperAccountHandler(service PaperService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := callerOf(w, r)
		if !ok {
			return
		}
		respondWithPaperAccount(w, r, service, userID, http.StatusOK)
	}
}

func openPaperAccountHandler(service PaperService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := callerOf(w, r)
		if !ok {
			return
		}
		var body struct {
			StartingCash       string `json:"starting_cash"`
			AccountingCurrency string `json:"accounting_currency"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			writePaperError(w, http.StatusBadRequest, "invalid_request", "The request body is not readable.")
			return
		}
		if _, err := service.Open(r.Context(), userID, paper.OpenRequest{
			StartingCash: body.StartingCash, AccountingCurrency: body.AccountingCurrency,
		}); err != nil {
			writePaperRefusal(w, err)
			return
		}
		respondWithPaperAccount(w, r, service, userID, http.StatusCreated)
	}
}

func promotePaperOrderHandler(service PaperService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := callerOf(w, r)
		if !ok {
			return
		}
		var body struct {
			IntentID string `json:"intent_id"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			writePaperError(w, http.StatusBadRequest, "invalid_request", "The request body is not readable.")
			return
		}
		if _, err := service.Promote(r.Context(), userID, paper.PromoteRequest{
			IntentID: paper.UUID(body.IntentID),
		}); err != nil {
			writePaperRefusal(w, err)
			return
		}
		respondWithPaperAccount(w, r, service, userID, http.StatusCreated)
	}
}

func cancelPaperOrderHandler(service PaperService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := callerOf(w, r)
		if !ok {
			return
		}
		if err := service.Cancel(r.Context(), userID, r.PathValue("id")); err != nil {
			writePaperRefusal(w, err)
			return
		}
		respondWithPaperAccount(w, r, service, userID, http.StatusOK)
	}
}

// respondWithPaperAccount answers a change with the whole account, because a fill moves cash, a
// holding, a total and a return at once, and a client patching one row would be showing stale
// arithmetic beside fresh.
func respondWithPaperAccount(w http.ResponseWriter, r *http.Request, service PaperService,
	userID string, status int) {
	view, err := service.View(r.Context(), userID)
	if errors.Is(err, paper.ErrNotFound) {
		writePaperError(w, http.StatusNotFound, "no_account", "You have not opened a paper account.")
		return
	}
	if err != nil {
		writePaperError(w, http.StatusInternalServerError, "paper_unavailable",
			"The paper account request failed.")
		return
	}
	httpx.JSON(w, status, paperViewDTO(view))
}

func writePaperRefusal(w http.ResponseWriter, err error) {
	var refusal paper.Refusal
	if errors.As(err, &refusal) {
		status := http.StatusBadRequest
		if refusal.Code == paper.RefusalAccountAlreadyOpen {
			status = http.StatusConflict
		}
		httpx.JSON(w, status, map[string]any{
			"error": map[string]any{"code": refusal.Code, "message": refusal.Message}})
		return
	}
	if errors.Is(err, paper.ErrNotFound) {
		writePaperError(w, http.StatusNotFound, "not_found", "No such record.")
		return
	}
	writePaperError(w, http.StatusInternalServerError, "paper_unavailable",
		"The paper account request failed.")
}

func writePaperError(w http.ResponseWriter, status int, code, message string) {
	httpx.JSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message}})
}
