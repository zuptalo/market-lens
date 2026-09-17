package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"market-lens/server/internal/httpx"
	"market-lens/server/internal/risk"
)

// RiskService is a person's own limits, and where they stand against them.
//
// As with the portfolio, every method takes the caller's user identifier and no route carries one:
// there is no such thing as reading "the" limits, only somebody's.
type RiskService interface {
	Report(ctx context.Context, userID string) (risk.Report, error)
	State(ctx context.Context, userID string, kind risk.Kind, threshold string) (risk.Limit, error)
	Remove(ctx context.Context, userID string, kind risk.Kind) error
}

type riskContributionResponse struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Share string `json:"share"`
}

// riskEvaluationResponse mirrors Evaluation in contracts/openapi.yaml.
//
// There is deliberately no field naming an amount to move or a holding to reduce. A field that
// exists will eventually be rendered, and rendering one would make the product's first
// recommendation — which belongs to the order-intent feature and the review that implies.
type riskEvaluationResponse struct {
	Kind          string                     `json:"kind"`
	Threshold     string                     `json:"threshold"`
	State         string                     `json:"state"`
	Measured      *string                    `json:"measured"`
	Denominator   *string                    `json:"denominator"`
	AbsenceReason *string                    `json:"absence_reason"`
	Contributions []riskContributionResponse `json:"contributions"`
}

type riskReportResponse struct {
	AccountingCurrency string                   `json:"accounting_currency"`
	Limits             []riskEvaluationResponse `json:"limits"`
	// LimitsAreYourOwn is always true and always present. These are the person's rules; the product
	// neither sets them nor advises on them.
	LimitsAreYourOwn bool `json:"limits_are_your_own"`
}

func riskReportDTO(report risk.Report) riskReportResponse {
	response := riskReportResponse{
		AccountingCurrency: report.AccountingCurrency,
		Limits:             make([]riskEvaluationResponse, 0, len(report.Limits)),
		LimitsAreYourOwn:   true,
	}
	for _, evaluation := range report.Limits {
		item := riskEvaluationResponse{
			Kind: string(evaluation.Kind), Threshold: evaluation.Threshold,
			State: string(evaluation.State), Measured: evaluation.Measured,
			Denominator:   evaluation.Denominator,
			Contributions: make([]riskContributionResponse, 0, len(evaluation.Contributions)),
		}
		if evaluation.AbsenceReason != nil {
			value := string(*evaluation.AbsenceReason)
			item.AbsenceReason = &value
		}
		for _, contribution := range evaluation.Contributions {
			item.Contributions = append(item.Contributions, riskContributionResponse{
				Label: contribution.Label, Value: contribution.Value, Share: contribution.Share})
		}
		response.Limits = append(response.Limits, item)
	}
	return response
}

func getRiskLimitsHandler(service RiskService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := callerOf(w, r)
		if !ok {
			return
		}
		report, err := service.Report(r.Context(), userID)
		if err != nil {
			writeRiskError(w, http.StatusInternalServerError, "risk_unavailable",
				"The risk request failed.")
			return
		}
		httpx.JSON(w, http.StatusOK, riskReportDTO(report))
	}
}

func setRiskLimitHandler(service RiskService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := callerOf(w, r)
		if !ok {
			return
		}
		var body struct {
			Threshold string `json:"threshold"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			writeRiskError(w, http.StatusBadRequest, "invalid_request", "The request body is not readable.")
			return
		}
		if _, err := service.State(r.Context(), userID, risk.Kind(r.PathValue("kind")), body.Threshold); err != nil {
			writeRiskRefusal(w, err)
			return
		}
		respondWithRiskReport(w, r, service, userID)
	}
}

func removeRiskLimitHandler(service RiskService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := callerOf(w, r)
		if !ok {
			return
		}
		if err := service.Remove(r.Context(), userID, risk.Kind(r.PathValue("kind"))); err != nil {
			writeRiskRefusal(w, err)
			return
		}
		respondWithRiskReport(w, r, service, userID)
	}
}

// respondWithRiskReport answers a change with the whole report, because every figure moves when one
// limit does and a client that patched a single row would be showing stale arithmetic beside fresh.
func respondWithRiskReport(w http.ResponseWriter, r *http.Request, service RiskService, userID string) {
	report, err := service.Report(r.Context(), userID)
	if err != nil {
		writeRiskError(w, http.StatusInternalServerError, "risk_unavailable", "The risk request failed.")
		return
	}
	httpx.JSON(w, http.StatusOK, riskReportDTO(report))
}

func writeRiskRefusal(w http.ResponseWriter, err error) {
	var refusal risk.Refusal
	if errors.As(err, &refusal) {
		httpx.JSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]any{"code": refusal.Code, "message": refusal.Message}})
		return
	}
	if errors.Is(err, risk.ErrNotFound) {
		writeRiskError(w, http.StatusNotFound, "no_limit", "No such limit.")
		return
	}
	writeRiskError(w, http.StatusInternalServerError, "risk_unavailable", "The risk request failed.")
}

func writeRiskError(w http.ResponseWriter, status int, code, message string) {
	httpx.JSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message}})
}
