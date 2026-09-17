package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"market-lens/server/internal/httpx"
	"market-lens/server/internal/portfolio"
)

// PortfolioService is one person's record of what they hold.
//
// Every method takes the caller's user identifier, because there is no such thing as reading "the"
// portfolio — only somebody's. The handlers below take it from the persisted principal and never
// from anything the client sends, so a caller cannot ask for another person's by supplying an
// identifier.
type PortfolioService interface {
	View(ctx context.Context, userID string) (portfolio.View, error)
	SetCurrency(ctx context.Context, userID, currency string) (portfolio.Portfolio, error)
	Record(ctx context.Context, userID string, request portfolio.RecordRequest) (portfolio.Trade, error)
	Correct(ctx context.Context, userID, tradeID string, request portfolio.RecordRequest) (portfolio.Trade, error)
	Withdraw(ctx context.Context, userID, tradeID string) error
	Trades(ctx context.Context, userID string, query portfolio.TradeQuery) (portfolio.TradePage, error)
}

type portfolioValuationResponse struct {
	Value          *string `json:"value"`
	Session        *string `json:"session"`
	ConversionRate *string `json:"conversion_rate"`
	AbsenceReason  *string `json:"absence_reason"`
}

type portfolioComparisonResponse struct {
	Series          string  `json:"series"`
	FromSession     *string `json:"from_session"`
	ToSession       *string `json:"to_session"`
	HoldingReturn   *string `json:"holding_return"`
	BenchmarkReturn *string `json:"benchmark_return"`
	AbsenceReason   *string `json:"absence_reason"`
}

type portfolioHoldingResponse struct {
	InstrumentID string                      `json:"instrument_id"`
	Ticker       string                      `json:"ticker"`
	Name         string                      `json:"name"`
	Currency     string                      `json:"currency"`
	Quantity     string                      `json:"quantity"`
	Cost         string                      `json:"cost"`
	Valuation    portfolioValuationResponse  `json:"valuation"`
	Unrealised   *string                     `json:"unrealised"`
	Comparison   portfolioComparisonResponse `json:"comparison"`
}

type portfolioRealisedResponse struct {
	InstrumentID string `json:"instrument_id"`
	Ticker       string `json:"ticker"`
	Name         string `json:"name"`
	Quantity     string `json:"quantity"`
	Proceeds     string `json:"proceeds"`
	Cost         string `json:"cost"`
	Realised     string `json:"realised"`
	CostBasis    string `json:"cost_basis"`
}

type portfolioTotalsResponse struct {
	Value            *string `json:"value"`
	Cost             string  `json:"cost"`
	Unrealised       *string `json:"unrealised"`
	Realised         string  `json:"realised"`
	Complete         bool    `json:"complete"`
	IncompleteReason *string `json:"incomplete_reason"`
	// ReturnAbsence is always present and always says the same thing. The product does not know
	// what was paid in, so it reports no return — and says so rather than leaving a field out,
	// because a missing figure reads as an oversight and a stated one reads as a decision.
	ReturnAbsence string `json:"return_absence"`
}

type portfolioResponse struct {
	AccountingCurrency string                      `json:"accounting_currency"`
	Holdings           []portfolioHoldingResponse  `json:"holdings"`
	Realised           []portfolioRealisedResponse `json:"realised"`
	Totals             portfolioTotalsResponse     `json:"total"`
	// RecordsWhatYouEntered is always true and always present. A claim a client can forget to
	// fetch is one that will eventually not be shown.
	RecordsWhatYouEntered bool `json:"records_what_you_entered"`
}

type portfolioTradeResponse struct {
	ID           string     `json:"id"`
	InstrumentID string     `json:"instrument_id"`
	Ticker       string     `json:"ticker"`
	Name         string     `json:"name"`
	Direction    string     `json:"direction"`
	Quantity     string     `json:"quantity"`
	Price        string     `json:"price"`
	Currency     string     `json:"currency"`
	Costs        string     `json:"costs"`
	TradeDate    string     `json:"trade_date"`
	Sequence     int64      `json:"sequence"`
	Status       string     `json:"status"`
	Supersedes   *string    `json:"supersedes"`
	RecordedAt   time.Time  `json:"recorded_at"`
	ChangedAt    *time.Time `json:"changed_at"`
}

type portfolioTradeInput struct {
	InstrumentID string `json:"instrument_id"`
	Direction    string `json:"direction"`
	Quantity     string `json:"quantity"`
	Price        string `json:"price"`
	Costs        string `json:"costs"`
	TradeDate    string `json:"trade_date"`
}

func portfolioTradeDTO(trade portfolio.Trade) portfolioTradeResponse {
	response := portfolioTradeResponse{
		ID: trade.ID.String(), InstrumentID: trade.InstrumentID.String(), Ticker: trade.Ticker,
		Name: trade.Name, Direction: string(trade.Direction), Quantity: trade.Quantity,
		Price: trade.Price, Currency: trade.Currency, Costs: trade.Costs,
		TradeDate: trade.TradeDate.String(), Sequence: trade.Sequence,
		Status: string(trade.Status), RecordedAt: trade.RecordedAt, ChangedAt: trade.ChangedAt,
	}
	if trade.Supersedes != nil {
		value := trade.Supersedes.String()
		response.Supersedes = &value
	}
	return response
}

func portfolioDTO(view portfolio.View) portfolioResponse {
	response := portfolioResponse{
		AccountingCurrency: view.Portfolio.AccountingCurrency,
		Holdings:           make([]portfolioHoldingResponse, 0, len(view.Holdings)),
		Realised:           make([]portfolioRealisedResponse, 0, len(view.Realised)),
		Totals: portfolioTotalsResponse{
			Value: view.Totals.Value, Cost: view.Totals.Cost, Unrealised: view.Totals.Unrealised,
			Realised: view.Totals.Realised, Complete: view.Totals.Complete,
			IncompleteReason: view.Totals.IncompleteReason,
			ReturnAbsence:    view.Totals.ReturnAbsence,
		},
		RecordsWhatYouEntered: true,
	}
	for _, holding := range view.Holdings {
		item := portfolioHoldingResponse{
			InstrumentID: holding.InstrumentID.String(), Ticker: holding.Ticker,
			Name: holding.Name, Currency: holding.Currency, Quantity: holding.Quantity,
			Cost: holding.Cost, Unrealised: holding.Unrealised,
			Valuation: portfolioValuationResponse{
				Value: holding.Valuation.Value, ConversionRate: holding.Valuation.ConversionRate},
			Comparison: portfolioComparisonResponse{Series: holding.Comparison.Series,
				HoldingReturn:   holding.Comparison.HoldingReturn,
				BenchmarkReturn: holding.Comparison.BenchmarkReturn},
		}
		if holding.Valuation.Session != nil {
			value := holding.Valuation.Session.String()
			item.Valuation.Session = &value
		}
		if holding.Valuation.AbsenceReason != nil {
			value := string(*holding.Valuation.AbsenceReason)
			item.Valuation.AbsenceReason = &value
		}
		if holding.Comparison.FromSession != nil {
			value := holding.Comparison.FromSession.String()
			item.Comparison.FromSession = &value
		}
		if holding.Comparison.ToSession != nil {
			value := holding.Comparison.ToSession.String()
			item.Comparison.ToSession = &value
		}
		if holding.Comparison.AbsenceReason != nil {
			value := string(*holding.Comparison.AbsenceReason)
			item.Comparison.AbsenceReason = &value
		}
		response.Holdings = append(response.Holdings, item)
	}
	for _, realised := range view.Realised {
		response.Realised = append(response.Realised, portfolioRealisedResponse{
			InstrumentID: realised.InstrumentID.String(), Ticker: realised.Ticker,
			Name: realised.Name, Quantity: realised.Quantity, Proceeds: realised.Proceeds,
			Cost: realised.Cost, Realised: realised.Realised, CostBasis: realised.CostBasis,
		})
	}
	return response
}

// callerOf reads whose portfolio this is from the persisted principal. It is never taken from the
// request, which is the difference between a private record and a guessable one.
func callerOf(w http.ResponseWriter, r *http.Request) (string, bool) {
	principal, ok := httpx.PrincipalFromContext(r)
	if !ok || principal.UserID == "" {
		writePortfolioError(w, http.StatusUnauthorized, "authentication_required",
			"This request needs an active session.")
		return "", false
	}
	return principal.UserID, true
}

func getPortfolioHandler(service PortfolioService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := callerOf(w, r)
		if !ok {
			return
		}
		view, err := service.View(r.Context(), userID)
		if err != nil {
			writePortfolioError(w, http.StatusInternalServerError, "portfolio_unavailable",
				"The portfolio request failed.")
			return
		}
		httpx.JSON(w, http.StatusOK, portfolioDTO(view))
	}
}

func setPortfolioCurrencyHandler(service PortfolioService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := callerOf(w, r)
		if !ok {
			return
		}
		var body struct {
			AccountingCurrency string `json:"accounting_currency"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			writePortfolioError(w, http.StatusBadRequest, "invalid_request", "The request body is not readable.")
			return
		}
		if _, err := service.SetCurrency(r.Context(), userID, body.AccountingCurrency); err != nil {
			writeRefusal(w, err)
			return
		}
		view, err := service.View(r.Context(), userID)
		if err != nil {
			writePortfolioError(w, http.StatusInternalServerError, "portfolio_unavailable",
				"The portfolio request failed.")
			return
		}
		httpx.JSON(w, http.StatusOK, portfolioDTO(view))
	}
}

func readTradeInput(w http.ResponseWriter, r *http.Request) (portfolio.RecordRequest, bool) {
	var body portfolioTradeInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body); err != nil {
		writePortfolioError(w, http.StatusBadRequest, "invalid_request", "The request body is not readable.")
		return portfolio.RecordRequest{}, false
	}
	return portfolio.RecordRequest{
		InstrumentID: portfolio.UUID(body.InstrumentID),
		Direction:    portfolio.Direction(body.Direction),
		Quantity:     body.Quantity, Price: body.Price, Costs: body.Costs,
		TradeDate: portfolio.SessionDate(body.TradeDate),
	}, true
}

func recordTradeHandler(service PortfolioService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := callerOf(w, r)
		if !ok {
			return
		}
		request, ok := readTradeInput(w, r)
		if !ok {
			return
		}
		trade, err := service.Record(r.Context(), userID, request)
		if err != nil {
			writeRefusal(w, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, portfolioTradeDTO(trade))
	}
}

func correctTradeHandler(service PortfolioService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := callerOf(w, r)
		if !ok {
			return
		}
		request, ok := readTradeInput(w, r)
		if !ok {
			return
		}
		trade, err := service.Correct(r.Context(), userID, r.PathValue("id"), request)
		if err != nil {
			writeRefusal(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, portfolioTradeDTO(trade))
	}
}

func withdrawTradeHandler(service PortfolioService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := callerOf(w, r)
		if !ok {
			return
		}
		if err := service.Withdraw(r.Context(), userID, r.PathValue("id")); err != nil {
			writeRefusal(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func listTradesHandler(service PortfolioService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := callerOf(w, r)
		if !ok {
			return
		}
		query := portfolio.TradeQuery{Cursor: r.URL.Query().Get("cursor"), Limit: 50,
			IncludeWithdrawn: r.URL.Query().Get("include_withdrawn") == "true"}
		if raw := r.URL.Query().Get("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > 200 {
				writePortfolioError(w, http.StatusBadRequest, "invalid_limit", "limit must be between 1 and 200.")
				return
			}
			query.Limit = parsed
		}
		page, err := service.Trades(r.Context(), userID, query)
		if err != nil {
			writePortfolioError(w, http.StatusBadRequest, "invalid_cursor", "The cursor is not readable.")
			return
		}
		items := make([]portfolioTradeResponse, 0, len(page.Items))
		for _, trade := range page.Items {
			items = append(items, portfolioTradeDTO(trade))
		}
		var next *string
		if page.NextCursor != "" {
			next = &page.NextCursor
		}
		httpx.JSON(w, http.StatusOK, map[string]any{
			"items": items, "next_cursor": next, "total": page.Total})
	}
}

// writeRefusal turns a refusal into an answer a person can act on, and everything else into the
// least informative response that is still true.
//
// A trade belonging to somebody else answers exactly as one that does not exist, so a response
// never confirms which identifiers are real.
func writeRefusal(w http.ResponseWriter, err error) {
	var refusal portfolio.Refusal
	if errors.As(err, &refusal) {
		body := map[string]any{"code": refusal.Code, "message": refusal.Message}
		if refusal.Held != "" {
			body["held_quantity"] = refusal.Held
		}
		httpx.JSON(w, http.StatusBadRequest, map[string]any{"error": body})
		return
	}
	if errors.Is(err, portfolio.ErrNotFound) {
		writePortfolioError(w, http.StatusNotFound, "no_trade", "No such trade.")
		return
	}
	writePortfolioError(w, http.StatusInternalServerError, "portfolio_unavailable",
		"The portfolio request failed.")
}

func writePortfolioError(w http.ResponseWriter, status int, code, message string) {
	httpx.JSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message}})
}
