package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"market-lens/server/internal/backtest"
	"market-lens/server/internal/httpx"
	"market-lens/server/internal/instruments"
)

// BacktestReader reads what a strategy would have done.
//
// There is deliberately no writer. Running a backtest is an owner action at the command line, so
// no request can make the product produce a result — which also means there is no handler here
// that would have to be guarded against one.
type BacktestReader interface {
	ListRuns(ctx context.Context, limit int) ([]backtest.Summary, error)
	Backtest(ctx context.Context, id string) (backtest.Detail, error)
	Trades(ctx context.Context, id, cursor string, limit int) (backtest.TradePage, error)
	Equity(ctx context.Context, id string) ([]backtest.EquityPoint, error)
}

type backtestStrategyRef struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
}

type backtestSizing struct {
	Rule     string `json:"rule"`
	Holdings int    `json:"holdings"`
}

type backtestRebalance struct {
	Schedule string `json:"schedule"`
}

type backtestCosts struct {
	BrokerageBPS      string `json:"brokerage_bps"`
	BrokerageMinimum  string `json:"brokerage_minimum"`
	SlippageBPS       string `json:"slippage_bps"`
	CurrencySpreadBPS string `json:"currency_spread_bps"`
}

type backtestConfigurationResponse struct {
	Name               string              `json:"name"`
	Version            int                 `json:"version"`
	Title              string              `json:"title"`
	Intent             string              `json:"intent"`
	Caveat             string              `json:"caveat"`
	Strategy           backtestStrategyRef `json:"strategy"`
	Universe           string              `json:"universe"`
	FromSession        *string             `json:"from_session"`
	ToSession          *string             `json:"to_session"`
	StartingCapital    string              `json:"starting_capital"`
	AccountingCurrency string              `json:"accounting_currency"`
	Sizing             backtestSizing      `json:"sizing"`
	Rebalance          backtestRebalance   `json:"rebalance"`
	Costs              backtestCosts       `json:"costs"`
}

// backtestMeasures mirrors Measures in contracts/openapi.yaml. Every field is present whether or
// not it has a value: a client that had to remember to ask for maximum drawdown would eventually
// stop asking, and the result would look better for it.
type backtestMeasures struct {
	FromSession      *string `json:"from_session"`
	ToSession        *string `json:"to_session"`
	TotalReturn      *string `json:"total_return"`
	AnnualisedReturn *string `json:"annualised_return"`
	Volatility       *string `json:"volatility"`
	MaximumDrawdown  *string `json:"maximum_drawdown"`
	TradeCount       *int64  `json:"trade_count"`
	TotalCosts       *string `json:"total_costs"`
	AbsenceReason    *string `json:"absence_reason"`
}

type backtestBenchmark struct {
	MIC           string           `json:"mic"`
	Series        string           `json:"series"`
	Measures      backtestMeasures `json:"measures"`
	AbsenceReason *string          `json:"absence_reason"`
}

type backtestSkipTally struct {
	Reason string `json:"reason"`
	Count  int64  `json:"count"`
}

type backtestSummaryResponse struct {
	ID            string                        `json:"id"`
	Configuration backtestConfigurationResponse `json:"configuration"`
	Status        string                        `json:"status"`
	FromSession   string                        `json:"from_session"`
	ToSession     string                        `json:"to_session"`
	StartedAt     time.Time                     `json:"started_at"`
	FinishedAt    *time.Time                    `json:"finished_at"`
	TradeCount    int64                         `json:"trade_count"`
	SkippedCount  int64                         `json:"skipped_count"`
	Rebalances    int64                         `json:"rebalance_count"`
	// IsSimulation is always true, and always present. A result is a simulation over past data
	// and not a prediction; every surface showing one has to say so, and a field a client must
	// remember to add is one that will eventually be missing from the one screen that mattered.
	IsSimulation bool `json:"is_simulation"`
}

type backtestDetailResponse struct {
	backtestSummaryResponse
	Measures   backtestMeasures    `json:"measures"`
	Benchmarks []backtestBenchmark `json:"benchmarks"`
	Skipped    []backtestSkipTally `json:"skipped"`
}

type backtestTradeResponse struct {
	ID               string  `json:"id"`
	InstrumentID     string  `json:"instrument_id"`
	Ticker           string  `json:"ticker"`
	Name             string  `json:"name"`
	SignalSession    string  `json:"signal_session"`
	ExecutionSession string  `json:"execution_session"`
	Direction        string  `json:"direction"`
	Quantity         string  `json:"quantity"`
	Price            string  `json:"price"`
	Currency         string  `json:"currency"`
	ConversionRate   *string `json:"conversion_rate"`
	Brokerage        string  `json:"brokerage"`
	Slippage         string  `json:"slippage"`
	CurrencySpread   string  `json:"currency_spread"`
	CashEffect       string  `json:"cash_effect"`
	// SignalID is the signal this trade acted on, written the way the signal store identifies
	// one. It is what lets a reader reach the strategy's own contributions from any trade rather
	// than being asked to take the trade on trust.
	SignalID string `json:"signal_id"`
}

type backtestEquityResponse struct {
	SessionDate    string  `json:"session_date"`
	Cash           string  `json:"cash"`
	PositionsValue *string `json:"positions_value"`
	Total          *string `json:"total"`
	AbsenceReason  *string `json:"absence_reason"`
}

func backtestConfigurationDTO(configuration backtest.Configuration) backtestConfigurationResponse {
	response := backtestConfigurationResponse{
		Name: configuration.Name, Version: configuration.Version, Title: configuration.Title,
		Intent: configuration.Intent, Caveat: configuration.Caveat,
		Strategy:           backtestStrategyRef{Name: configuration.Strategy.Name, Version: configuration.Strategy.Version},
		Universe:           configuration.UniverseCode,
		StartingCapital:    configuration.StartingCapital,
		AccountingCurrency: configuration.AccountingCurrency,
		Sizing:             backtestSizing{Rule: string(configuration.SizingRule), Holdings: configuration.SizingN},
		Rebalance:          backtestRebalance{Schedule: string(configuration.Rebalance)},
		Costs: backtestCosts{
			BrokerageBPS:      configuration.Costs.BrokerageBasisPoints,
			BrokerageMinimum:  configuration.Costs.BrokerageMinimum,
			SlippageBPS:       configuration.Costs.SlippageBasisPoints,
			CurrencySpreadBPS: configuration.Costs.SpreadBasisPoints,
		},
	}
	if configuration.From != "" {
		value := configuration.From.String()
		response.FromSession = &value
	}
	if configuration.To != "" {
		value := configuration.To.String()
		response.ToSession = &value
	}
	return response
}

func backtestSummaryDTO(summary backtest.Summary) backtestSummaryResponse {
	return backtestSummaryResponse{
		ID: summary.Run.ID.String(), Configuration: backtestConfigurationDTO(summary.Configuration),
		Status: string(summary.Run.Status), FromSession: summary.Run.From.String(),
		ToSession: summary.Run.To.String(), StartedAt: summary.Run.StartedAt,
		FinishedAt: summary.Run.FinishedAt, TradeCount: summary.Run.TradeCount,
		SkippedCount: summary.Run.SkipCount, Rebalances: summary.Run.RebalanceCount,
		IsSimulation: true,
	}
}

func backtestMeasuresDTO(measures backtest.Measures) backtestMeasures {
	response := backtestMeasures{
		TotalReturn: measures.TotalReturn, AnnualisedReturn: measures.AnnualisedReturn,
		Volatility: measures.Volatility, MaximumDrawdown: measures.MaxDrawdown,
		TradeCount: measures.TradeCount, TotalCosts: measures.TotalCosts,
	}
	if measures.From != nil {
		value := measures.From.String()
		response.FromSession = &value
	}
	if measures.To != nil {
		value := measures.To.String()
		response.ToSession = &value
	}
	if measures.AbsenceReason != nil {
		value := string(*measures.AbsenceReason)
		response.AbsenceReason = &value
	}
	return response
}

func listBacktestsHandler(reader BacktestReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := 20
		if raw := r.URL.Query().Get("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > 50 {
				writeBacktestError(w, http.StatusBadRequest, "invalid_limit", "limit must be between 1 and 50.")
				return
			}
			limit = parsed
		}
		summaries, err := reader.ListRuns(r.Context(), limit)
		if err != nil {
			writeBacktestError(w, http.StatusInternalServerError, "backtests_unavailable", "The backtest request failed.")
			return
		}
		items := make([]backtestSummaryResponse, 0, len(summaries))
		for _, summary := range summaries {
			items = append(items, backtestSummaryDTO(summary))
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func getBacktestHandler(reader BacktestReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := instruments.ParseUUID(r.PathValue("id"))
		if err != nil {
			writeBacktestError(w, http.StatusBadRequest, "invalid_backtest_id", "The backtest identifier is invalid.")
			return
		}
		detail, err := reader.Backtest(r.Context(), id.String())
		if err != nil {
			if errors.Is(err, backtest.ErrNotFound) {
				writeBacktestError(w, http.StatusNotFound, "no_backtest", "No such backtest.")
				return
			}
			writeBacktestError(w, http.StatusInternalServerError, "backtests_unavailable", "The backtest request failed.")
			return
		}

		response := backtestDetailResponse{
			backtestSummaryResponse: backtestSummaryDTO(detail.Summary),
			Benchmarks:              []backtestBenchmark{},
			Skipped:                 []backtestSkipTally{},
		}
		for _, measure := range detail.Measures {
			if measure.Subject == backtest.MeasureSubjectStrategy {
				response.Measures = backtestMeasuresDTO(measure)
				continue
			}
			comparison := backtestBenchmark{
				MIC: detail.Markets[measure.Subject], Series: measure.Subject,
				Measures: backtestMeasuresDTO(measure),
			}
			if measure.AbsenceReason != nil {
				value := string(*measure.AbsenceReason)
				comparison.AbsenceReason = &value
			}
			response.Benchmarks = append(response.Benchmarks, comparison)
		}
		for _, tally := range detail.Skips {
			response.Skipped = append(response.Skipped, backtestSkipTally{
				Reason: string(tally.Reason), Count: tally.Count})
		}
		httpx.JSON(w, http.StatusOK, response)
	}
}

func listBacktestTradesHandler(reader BacktestReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := instruments.ParseUUID(r.PathValue("id"))
		if err != nil {
			writeBacktestError(w, http.StatusBadRequest, "invalid_backtest_id", "The backtest identifier is invalid.")
			return
		}
		limit := 50
		if raw := r.URL.Query().Get("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > 200 {
				writeBacktestError(w, http.StatusBadRequest, "invalid_limit", "limit must be between 1 and 200.")
				return
			}
			limit = parsed
		}
		page, err := reader.Trades(r.Context(), id.String(), r.URL.Query().Get("cursor"), limit)
		if err != nil {
			writeBacktestError(w, http.StatusBadRequest, "invalid_cursor", "The cursor is not readable.")
			return
		}
		items := make([]backtestTradeResponse, 0, len(page.Items))
		for _, trade := range page.Items {
			items = append(items, backtestTradeResponse{
				ID: trade.ID.String(), InstrumentID: trade.InstrumentID.String(),
				Ticker: trade.Ticker, Name: trade.Name,
				SignalSession:    trade.SignalSession.String(),
				ExecutionSession: trade.ExecutionSession.String(),
				Direction:        string(trade.Direction), Quantity: trade.Quantity, Price: trade.Price,
				Currency: trade.PriceCurrency, ConversionRate: trade.FXRate,
				Brokerage: trade.Brokerage, Slippage: trade.Slippage,
				CurrencySpread: trade.SpreadCost, CashEffect: trade.CashEffect,
				// The signal store identifies a signal by instrument, session and strategy
				// version. Writing all three is what makes the link followable without a reader
				// having to know how signals are keyed.
				SignalID: trade.InstrumentID.String() + "/" + trade.SignalSession.String() +
					"/" + trade.StrategyID.String(),
			})
		}
		var next *string
		if page.NextCursor != "" {
			next = &page.NextCursor
		}
		httpx.JSON(w, http.StatusOK, map[string]any{
			"items": items, "next_cursor": next, "total": page.Total})
	}
}

func getBacktestEquityHandler(reader BacktestReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := instruments.ParseUUID(r.PathValue("id"))
		if err != nil {
			writeBacktestError(w, http.StatusBadRequest, "invalid_backtest_id", "The backtest identifier is invalid.")
			return
		}
		detail, err := reader.Backtest(r.Context(), id.String())
		if err != nil {
			if errors.Is(err, backtest.ErrNotFound) {
				writeBacktestError(w, http.StatusNotFound, "no_backtest", "No such backtest.")
				return
			}
			writeBacktestError(w, http.StatusInternalServerError, "backtests_unavailable", "The backtest request failed.")
			return
		}
		curve, err := reader.Equity(r.Context(), id.String())
		if err != nil {
			writeBacktestError(w, http.StatusInternalServerError, "backtests_unavailable", "The backtest request failed.")
			return
		}
		items := make([]backtestEquityResponse, 0, len(curve))
		for _, point := range curve {
			item := backtestEquityResponse{SessionDate: point.SessionDate.String(), Cash: point.Cash,
				PositionsValue: point.PositionValue, Total: point.Total}
			if point.AbsenceReason != nil {
				value := string(*point.AbsenceReason)
				item.AbsenceReason = &value
			}
			items = append(items, item)
		}
		httpx.JSON(w, http.StatusOK, map[string]any{
			"currency": detail.Configuration.AccountingCurrency, "items": items})
	}
}

func writeBacktestError(w http.ResponseWriter, status int, code, message string) {
	httpx.JSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message}})
}
