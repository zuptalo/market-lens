package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"market-lens/server/internal/httpx"
	"market-lens/server/internal/instruments"
	"market-lens/server/internal/strategies"
)

// SignalReader reads what strategies have said. There is deliberately no writer: computation is
// an owner action at the command line or a consequence of a feature run, never an HTTP call, so
// no request can make the product produce a new view of the market.
type SignalReader interface {
	SignalAsOf(ctx context.Context, instrumentID strategies.UUID, asOf strategies.SessionDate, strategy string, version int) (strategies.SignalView, error)
	Ranking(ctx context.Context, request strategies.RankingRequest) (strategies.RankingPage, error)
	Strategies(ctx context.Context, name string, includeSuperseded bool) ([]strategies.Strategy, error)
	ListRuns(ctx context.Context, limit int) ([]strategies.Run, error)
}

// strategyRefResponse travels with every signal. The caveat is a required field rather than an
// optional one precisely because it would otherwise be the first thing a client stopped sending.
type strategyRefResponse struct {
	Name       string `json:"name"`
	Version    int    `json:"version"`
	Title      string `json:"title"`
	Caveat     string `json:"caveat"`
	Superseded bool   `json:"superseded"`
}

type contributionResponse struct {
	Factor            string  `json:"factor"`
	Feature           string  `json:"feature"`
	FeatureValue      *string `json:"feature_value"`
	FeatureSession    *string `json:"feature_session"`
	FactorScore       *string `json:"factor_score"`
	Weight            string  `json:"weight"`
	Contribution      *string `json:"contribution"`
	UnavailableReason *string `json:"unavailable_reason"`
}

// signalResponse mirrors Signal in contracts/openapi.yaml. Every decimal is a string: the stored
// columns are numeric(24,12) and a JSON number would round them on the way through.
type signalResponse struct {
	InstrumentID  string                 `json:"instrument_id"`
	SessionDate   string                 `json:"session_date"`
	Strategy      strategyRefResponse    `json:"strategy"`
	Score         *string                `json:"score"`
	Action        *string                `json:"action"`
	Confidence    *string                `json:"confidence"`
	AbsenceReason *string                `json:"absence_reason"`
	Contributions []contributionResponse `json:"contributions"`
	Divisor       *string                `json:"divisor"`
	ComputedAt    time.Time              `json:"computed_at"`
}

type rankedSignalResponse struct {
	signalResponse
	Ticker string `json:"ticker"`
	Name   string `json:"name"`
	Rank   *int64 `json:"rank"`
}

type strategyFactorResponse struct {
	Name        string `json:"name"`
	Feature     string `json:"feature"`
	Mode        string `json:"mode"`
	Weight      string `json:"weight"`
	Description string `json:"description"`
}

type actionBandResponse struct {
	Lower  string `json:"lower"`
	Upper  string `json:"upper"`
	Action string `json:"action"`
}

type strategyResponse struct {
	strategyRefResponse
	Intent       string                   `json:"intent"`
	Factors      []strategyFactorResponse `json:"factors"`
	ActionBands  []actionBandResponse     `json:"action_bands"`
	PublishedAt  time.Time                `json:"published_at"`
	SupersededAt *time.Time               `json:"superseded_at"`
}

type strategyRunResponse struct {
	ID                  string               `json:"id"`
	Kind                string               `json:"kind"`
	Status              string               `json:"status"`
	Strategy            *strategyRefResponse `json:"strategy,omitempty"`
	StartedAt           time.Time            `json:"started_at"`
	FinishedAt          *time.Time           `json:"finished_at"`
	InstrumentCount     int64                `json:"instrument_count"`
	SignalCount         int64                `json:"signal_count"`
	FailedCount         int64                `json:"failed_count"`
	TriggerFeatureRunID *string              `json:"trigger_feature_run_id"`
	AppVersion          *string              `json:"app_version"`
}

func getInstrumentSignalHandler(reader SignalReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := instruments.ParseUUID(r.PathValue("id"))
		if err != nil {
			writeSignalError(w, http.StatusBadRequest, "invalid_instrument_id", "The instrument identifier is invalid.")
			return
		}
		var asOf strategies.SessionDate
		if raw := r.URL.Query().Get("as_of"); raw != "" {
			session, err := instruments.ParseSessionDate(raw)
			if err != nil {
				writeSignalError(w, http.StatusBadRequest, "invalid_as_of", "as_of must be a valid YYYY-MM-DD date.")
				return
			}
			asOf = session
		}
		version, ok := signalVersion(w, r)
		if !ok {
			return
		}
		view, err := reader.SignalAsOf(r.Context(), id, asOf, r.URL.Query().Get("strategy"), version)
		if err != nil {
			switch {
			// An instrument that does not exist and one with no signal answer the same way, so
			// the response never confirms which identifiers are real.
			case errors.Is(err, strategies.ErrNotFound), errors.Is(err, strategies.ErrNoSignal):
				writeSignalError(w, http.StatusNotFound, "no_signal",
					"No strategy has recorded a view of this instrument on or before that session.")
			default:
				writeSignalError(w, http.StatusInternalServerError, "signals_unavailable", "The signal request failed.")
			}
			return
		}
		httpx.JSON(w, http.StatusOK, signalDTO(view.Signal, view.Strategy))
	}
}

func listSignalsHandler(reader SignalReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request := strategies.RankingRequest{
			Strategy: r.URL.Query().Get("strategy"),
			Cursor:   r.URL.Query().Get("cursor"),
			Limit:    50,
		}
		if raw := r.URL.Query().Get("as_of"); raw != "" {
			session, err := instruments.ParseSessionDate(raw)
			if err != nil {
				writeSignalError(w, http.StatusBadRequest, "invalid_as_of", "as_of must be a valid YYYY-MM-DD date.")
				return
			}
			request.AsOf = session
		}
		version, ok := signalVersion(w, r)
		if !ok {
			return
		}
		request.Version = version
		if raw := r.URL.Query().Get("limit"); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 1 || value > 200 {
				writeSignalError(w, http.StatusBadRequest, "invalid_limit", "limit must be between 1 and 200.")
				return
			}
			request.Limit = value
		}

		page, err := reader.Ranking(r.Context(), request)
		if err != nil {
			if errors.Is(err, strategies.ErrNotFound) {
				writeSignalError(w, http.StatusNotFound, "no_strategy", "There is no such strategy version.")
				return
			}
			writeSignalError(w, http.StatusInternalServerError, "signals_unavailable", "The ranking request failed.")
			return
		}
		items := make([]rankedSignalResponse, 0, len(page.Items))
		for _, item := range page.Items {
			items = append(items, rankedSignalResponse{
				signalResponse: signalDTO(item.Signal, page.Strategy),
				Ticker:         item.Ticker, Name: item.Name, Rank: item.Rank,
			})
		}
		var cursor *string
		if page.NextCursor != "" {
			next := page.NextCursor
			cursor = &next
		}
		httpx.JSON(w, http.StatusOK, map[string]any{
			"items": items, "next_cursor": cursor, "total": page.Total,
			"strategy": strategyRefDTO(page.Strategy), "session_date": page.SessionDate.String(),
			"scored": page.Scored, "unscored": page.Unscored,
		})
	}
}

func listStrategiesHandler(reader SignalReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		includeSuperseded := true
		if raw := r.URL.Query().Get("include_superseded"); raw != "" {
			value, err := strconv.ParseBool(raw)
			if err != nil {
				writeSignalError(w, http.StatusBadRequest, "invalid_include_superseded",
					"include_superseded must be true or false.")
				return
			}
			includeSuperseded = value
		}
		published, err := reader.Strategies(r.Context(), r.URL.Query().Get("name"), includeSuperseded)
		if err != nil {
			writeSignalError(w, http.StatusInternalServerError, "signals_unavailable", "The strategy request failed.")
			return
		}
		items := make([]strategyResponse, 0, len(published))
		for _, strategy := range published {
			item := strategyResponse{
				strategyRefResponse: strategyRefDTO(strategy), Intent: strategy.Intent,
				Factors:     make([]strategyFactorResponse, 0, len(strategy.Factors)),
				ActionBands: make([]actionBandResponse, 0, len(strategy.ActionBands)),
				PublishedAt: strategy.PublishedAt, SupersededAt: strategy.SupersededAt,
			}
			for _, factor := range strategy.Factors {
				item.Factors = append(item.Factors, strategyFactorResponse{
					Name: factor.Name, Feature: factor.Feature, Mode: string(factor.Mode),
					Weight: strategies.Round(factor.Weight), Description: factor.Description,
				})
			}
			for _, band := range strategy.ActionBands {
				item.ActionBands = append(item.ActionBands, actionBandResponse{
					Lower: strategies.Round(band.Lower), Upper: strategies.Round(band.Upper), Action: band.Action,
				})
			}
			items = append(items, item)
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func listStrategyRunsHandler(reader SignalReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := 10
		if raw := r.URL.Query().Get("limit"); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 1 || value > 50 {
				writeSignalError(w, http.StatusBadRequest, "invalid_limit", "limit must be between 1 and 50.")
				return
			}
			limit = value
		}
		runs, err := reader.ListRuns(r.Context(), limit)
		if err != nil {
			writeSignalError(w, http.StatusInternalServerError, "signals_unavailable", "The run request failed.")
			return
		}
		items := make([]strategyRunResponse, 0, len(runs))
		for _, run := range runs {
			item := strategyRunResponse{
				ID: run.ID.String(), Kind: string(run.Kind), Status: string(run.Status),
				StartedAt: run.StartedAt, FinishedAt: run.FinishedAt,
				InstrumentCount: run.InstrumentCount, SignalCount: run.SignalCount,
				FailedCount: run.FailedCount,
			}
			if run.TriggerFeatureRunID != nil {
				trigger := run.TriggerFeatureRunID.String()
				item.TriggerFeatureRunID = &trigger
			}
			if run.AppVersion != "" {
				version := run.AppVersion
				item.AppVersion = &version
			}
			items = append(items, item)
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func signalVersion(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := r.URL.Query().Get("version")
	if raw == "" {
		return 0, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		writeSignalError(w, http.StatusBadRequest, "invalid_version", "version must be a positive integer.")
		return 0, false
	}
	return value, true
}

func strategyRefDTO(strategy strategies.Strategy) strategyRefResponse {
	return strategyRefResponse{
		Name: strategy.Name, Version: strategy.Version, Title: strategy.Title,
		Caveat: strategy.Caveat, Superseded: strategy.Superseded(),
	}
}

func signalDTO(signal strategies.Signal, strategy strategies.Strategy) signalResponse {
	response := signalResponse{
		InstrumentID: signal.InstrumentID.String(), SessionDate: signal.SessionDate.String(),
		Strategy: strategyRefDTO(strategy), Score: signal.Score, Confidence: signal.Confidence,
		Divisor: signal.Divisor, ComputedAt: signal.ComputedAt,
		Contributions: make([]contributionResponse, 0, len(signal.Contributions)),
	}
	if signal.Action != nil {
		action := string(*signal.Action)
		response.Action = &action
	}
	if signal.AbsenceReason != nil {
		reason := string(*signal.AbsenceReason)
		response.AbsenceReason = &reason
	}
	for _, contribution := range signal.Contributions {
		item := contributionResponse{
			Factor: contribution.Factor, Feature: contribution.Feature,
			FeatureValue: contribution.FeatureValue, Weight: strategies.Round(contribution.Weight),
		}
		if contribution.FeatureSession != "" {
			session := contribution.FeatureSession
			item.FeatureSession = &session
		}
		if contribution.FactorScore != nil {
			score := strategies.Round(*contribution.FactorScore)
			item.FactorScore = &score
		}
		if contribution.Contribution != nil {
			value := strategies.Round(*contribution.Contribution)
			item.Contribution = &value
		}
		if contribution.UnavailableReason != "" {
			reason := contribution.UnavailableReason
			item.UnavailableReason = &reason
		}
		response.Contributions = append(response.Contributions, item)
	}
	return response
}

func writeSignalError(w http.ResponseWriter, status int, code, message string) {
	httpx.JSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message}})
}
