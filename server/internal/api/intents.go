package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"market-lens/server/internal/httpx"
	"market-lens/server/internal/intents"
)

// IntentsService is what a person is considering, and what each would do.
//
// As with the portfolio and the limits, every method takes the caller's user identifier and no route
// carries one: there is no such thing as reading "the" intents, only somebody's.
//
// There is deliberately no method that creates an intent from a signal. The product records what a
// person decided to consider; it does not decide for them.
type IntentsService interface {
	Report(ctx context.Context, userID string, includeSettled bool) (intents.Report, error)
	Record(ctx context.Context, userID string, request intents.RecordRequest) (intents.Intent, error)
	Settle(ctx context.Context, userID, intentID string, status intents.Status) error
}

// intentConsequenceResponse mirrors Consequence in contracts/openapi.yaml.
//
// There is no field saying whether to act, and none a broker could read. The response says what the
// position would become and what the person's own limits would say about it; the decision stays
// with the person.
type intentConsequenceResponse struct {
	ResultingQuantity string                   `json:"resulting_quantity"`
	ResultingValue    *string                  `json:"resulting_value"`
	ResultingShare    *string                  `json:"resulting_share"`
	Denominator       *string                  `json:"denominator"`
	AbsenceReason     *string                  `json:"absence_reason"`
	Limits            []riskEvaluationResponse `json:"limits"`
}

type intentResponse struct {
	ID           string                     `json:"id"`
	InstrumentID string                     `json:"instrument_id"`
	Ticker       string                     `json:"ticker"`
	Name         string                     `json:"name"`
	Currency     string                     `json:"currency"`
	Direction    string                     `json:"direction"`
	Quantity     string                     `json:"quantity"`
	Price        string                     `json:"price"`
	Costs        string                     `json:"costs"`
	Status       string                     `json:"status"`
	RecordedAt   time.Time                  `json:"recorded_at"`
	SettledAt    *time.Time                 `json:"settled_at"`
	Consequence  *intentConsequenceResponse `json:"consequence"`
}

type intentReportResponse struct {
	Intents []intentResponse `json:"intents"`
	// EvaluatedIndependently and RecordsWhatYouAreConsidering are always true and always present.
	// Each intent is measured against the portfolio as it stands, and the product offers no advice.
	EvaluatedIndependently       bool `json:"evaluated_independently"`
	RecordsWhatYouAreConsidering bool `json:"records_what_you_are_considering"`
}

func intentReportDTO(report intents.Report) intentReportResponse {
	response := intentReportResponse{
		Intents:                      make([]intentResponse, 0, len(report.Intents)),
		EvaluatedIndependently:       true,
		RecordsWhatYouAreConsidering: true,
	}
	for _, intent := range report.Intents {
		item := intentResponse{
			ID: string(intent.ID), InstrumentID: string(intent.InstrumentID),
			Ticker: intent.Ticker, Name: intent.Name, Currency: intent.Currency,
			Direction: string(intent.Direction), Quantity: intent.Quantity,
			Price: intent.Price, Costs: intent.Costs, Status: string(intent.Status),
			RecordedAt: intent.RecordedAt, SettledAt: intent.SettledAt,
		}
		// A settled intent carries no consequence. Sending a zeroed one would read as "it would do
		// nothing", which is a different claim from "the question is no longer asked".
		if intent.Consequence != nil {
			consequence := &intentConsequenceResponse{
				ResultingQuantity: intent.Consequence.ResultingQuantity,
				ResultingValue:    intent.Consequence.ResultingValue,
				ResultingShare:    intent.Consequence.ResultingShare,
				Denominator:       intent.Consequence.Denominator,
				Limits:            make([]riskEvaluationResponse, 0, len(intent.Consequence.Limits)),
			}
			if intent.Consequence.AbsenceReason != nil {
				reason := string(*intent.Consequence.AbsenceReason)
				consequence.AbsenceReason = &reason
			}
			for _, evaluation := range intent.Consequence.Limits {
				consequence.Limits = append(consequence.Limits, riskEvaluationDTO(evaluation))
			}
			item.Consequence = consequence
		}
		response.Intents = append(response.Intents, item)
	}
	return response
}

func listIntentsHandler(service IntentsService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := callerOf(w, r)
		if !ok {
			return
		}
		respondWithIntentReport(w, r, service, userID, http.StatusOK)
	}
}

func recordIntentHandler(service IntentsService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := callerOf(w, r)
		if !ok {
			return
		}
		var body struct {
			InstrumentID string `json:"instrument_id"`
			Direction    string `json:"direction"`
			Quantity     string `json:"quantity"`
			Price        string `json:"price"`
			Costs        string `json:"costs"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			writeIntentError(w, http.StatusBadRequest, "invalid_request", "The request body is not readable.")
			return
		}
		_, err := service.Record(r.Context(), userID, intents.RecordRequest{
			InstrumentID: intents.UUID(body.InstrumentID), Direction: intents.Direction(body.Direction),
			Quantity: body.Quantity, Price: body.Price, Costs: body.Costs,
		})
		if err != nil {
			writeIntentRefusal(w, err)
			return
		}
		respondWithIntentReport(w, r, service, userID, http.StatusCreated)
	}
}

func settleIntentHandler(service IntentsService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := callerOf(w, r)
		if !ok {
			return
		}
		var body struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			writeIntentError(w, http.StatusBadRequest, "invalid_request", "The request body is not readable.")
			return
		}
		if err := service.Settle(r.Context(), userID, r.PathValue("id"), intents.Status(body.Status)); err != nil {
			writeIntentRefusal(w, err)
			return
		}
		respondWithIntentReport(w, r, service, userID, http.StatusOK)
	}
}

// respondWithIntentReport answers a change with the whole report. Every consequence is measured
// against the same portfolio, so one intent settling can move what another would do; a client that
// patched a single row would be showing stale arithmetic beside fresh.
func respondWithIntentReport(w http.ResponseWriter, r *http.Request, service IntentsService,
	userID string, status int) {
	report, err := service.Report(r.Context(), userID, r.URL.Query().Get("include_settled") == "true")
	if err != nil {
		writeIntentError(w, http.StatusInternalServerError, "intents_unavailable",
			"The order-intent request failed.")
		return
	}
	httpx.JSON(w, status, intentReportDTO(report))
}

func writeIntentRefusal(w http.ResponseWriter, err error) {
	var refusal intents.Refusal
	if errors.As(err, &refusal) {
		httpx.JSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]any{"code": refusal.Code, "message": refusal.Message}})
		return
	}
	if errors.Is(err, intents.ErrNotFound) {
		writeIntentError(w, http.StatusNotFound, "no_intent", "No such intent.")
		return
	}
	writeIntentError(w, http.StatusInternalServerError, "intents_unavailable",
		"The order-intent request failed.")
}

func writeIntentError(w http.ResponseWriter, status int, code, message string) {
	httpx.JSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message}})
}
