package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"market-lens/server/internal/httpx"
	"market-lens/server/internal/instruments"
	"market-lens/server/internal/marketdata"
)

var ErrNotFound = errors.New("resource not found")

type FindingFilter = marketdata.FindingFilter

type MarketDataReader interface {
	ListImportRuns(context.Context, marketdata.ImportStatus, int) ([]marketdata.ImportRun, error)
	GetImportRun(context.Context, instruments.UUID) (marketdata.ImportRun, []marketdata.ImportItem, error)
	ListQualityFindings(context.Context, FindingFilter) ([]marketdata.QualityFinding, error)
}

// FindingDecider records an owner's judgement that a condition is a limitation of the data. It is
// the only mutation this surface has, and it is deliberately separate from the reader: reading
// findings is for everyone, deciding about them is not.
type FindingDecider interface {
	AcceptFinding(ctx context.Context, findingID, userID instruments.UUID, at time.Time) (marketdata.QualityFinding, error)
}

type importCountsResponse struct {
	Processed int64 `json:"processed"`
	Accepted  int64 `json:"accepted"`
	Rejected  int64 `json:"rejected"`
	Flagged   int64 `json:"flagged"`
	// Revised counts sessions this run replaced because the source had changed its answer.
	// Zero is the ordinary case and must not be presented as though a correction occurred.
	Revised int64 `json:"revised"`
}

type safeErrorResponse struct {
	Code    string `json:"code"`
	Summary string `json:"summary"`
}

type importRunResponse struct {
	ID            string                  `json:"id"`
	Kind          marketdata.ImportKind   `json:"kind"`
	Provider      string                  `json:"provider"`
	RequestedFrom *marketdata.SessionDate `json:"requested_from,omitempty"`
	RequestedTo   *marketdata.SessionDate `json:"requested_to,omitempty"`
	Status        marketdata.ImportStatus `json:"status"`
	ParentRunID   *instruments.UUID       `json:"parent_run_id,omitempty"`
	StartedAt     time.Time               `json:"started_at"`
	FinishedAt    *time.Time              `json:"finished_at,omitempty"`
	Counts        importCountsResponse    `json:"counts"`
	Error         *safeErrorResponse      `json:"error,omitempty"`
	AppVersion    string                  `json:"app_version"`
}

type importItemResponse struct {
	RunID         string                  `json:"run_id"`
	InstrumentID  string                  `json:"instrument_id"`
	RequestedFrom marketdata.SessionDate  `json:"requested_from"`
	RequestedTo   marketdata.SessionDate  `json:"requested_to"`
	Status        marketdata.ImportStatus `json:"status"`
	Counts        importCountsResponse    `json:"counts"`
	Attempts      int                     `json:"attempts"`
	StartedAt     *time.Time              `json:"started_at,omitempty"`
	FinishedAt    *time.Time              `json:"finished_at,omitempty"`
	Error         *safeErrorResponse      `json:"error,omitempty"`
}

type qualityFindingResponse struct {
	ID             string                        `json:"id"`
	InstrumentID   string                        `json:"instrument_id"`
	SessionDate    *marketdata.SessionDate       `json:"session_date,omitempty"`
	RunID          string                        `json:"run_id"`
	Rule           string                        `json:"rule"`
	Severity       marketdata.FindingSeverity    `json:"severity"`
	Disposition    marketdata.FindingDisposition `json:"disposition"`
	Detail         string                        `json:"detail"`
	Status         marketdata.FindingStatus      `json:"status"`
	CreatedAt      time.Time                     `json:"created_at"`
	ResolvedAt     *time.Time                    `json:"resolved_at,omitempty"`
	ResolvingRunID *instruments.UUID             `json:"resolving_run_id,omitempty"`
	// ReexaminedAt says an import covered this session again and raised the same rule, so a
	// further identical request cannot settle it. AwaitingDecision is what a reader acts on.
	ReexaminedAt     *time.Time        `json:"reexamined_at"`
	AwaitingDecision bool              `json:"awaiting_decision"`
	AcceptedAt       *time.Time        `json:"accepted_at,omitempty"`
	AcceptedBy       *instruments.UUID `json:"accepted_by,omitempty"`
}

func qualityFindingDTO(finding marketdata.QualityFinding) qualityFindingResponse {
	return qualityFindingResponse{
		ID: finding.ID.String(), InstrumentID: finding.InstrumentID.String(), SessionDate: finding.SessionDate,
		RunID: finding.RunID.String(), Rule: finding.Rule, Severity: finding.Severity,
		Disposition: finding.Disposition, Detail: finding.Detail, Status: finding.Status,
		CreatedAt: finding.CreatedAt, ResolvedAt: finding.ResolvedAt, ResolvingRunID: finding.ResolvingRunID,
		ReexaminedAt: finding.ReexaminedAt, AwaitingDecision: finding.AwaitingDecision,
		AcceptedAt: finding.AcceptedAt, AcceptedBy: finding.AcceptedBy,
	}
}

func listImportRunsHandler(reader MarketDataReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := marketdata.ImportStatus(r.URL.Query().Get("status"))
		if !validImportStatus(status) {
			httpx.Error(w, http.StatusBadRequest, "invalid import status")
			return
		}
		limit, ok := queryLimit(r, 20)
		if !ok {
			httpx.Error(w, http.StatusBadRequest, "invalid limit")
			return
		}
		runs, err := reader.ListImportRuns(r.Context(), status, limit)
		if err != nil {
			writeMarketDataError(w, err)
			return
		}
		items := make([]importRunResponse, 0, len(runs))
		for _, run := range runs {
			items = append(items, importRunDTO(run))
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func getImportRunHandler(reader MarketDataReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := instruments.ParseUUID(r.PathValue("id"))
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid import run ID")
			return
		}
		run, items, err := reader.GetImportRun(r.Context(), id)
		if err != nil {
			writeMarketDataError(w, err)
			return
		}
		itemDTOs := make([]importItemResponse, 0, len(items))
		for _, item := range items {
			itemDTOs = append(itemDTOs, importItemDTO(item))
		}
		httpx.JSON(w, http.StatusOK, map[string]any{
			"run": importRunDTO(run), "items": itemDTOs,
			"retry_command": "market-lens marketdata retry --run " + run.ID.String(),
		})
	}
}

func listQualityFindingsHandler(reader MarketDataReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filter := FindingFilter{
			Status:   marketdata.FindingStatus(r.URL.Query().Get("status")),
			Severity: marketdata.FindingSeverity(r.URL.Query().Get("severity")),
		}
		if raw := r.URL.Query().Get("awaiting_decision"); raw != "" {
			awaiting, err := strconv.ParseBool(raw)
			if err != nil {
				httpx.Error(w, http.StatusBadRequest, "invalid quality-finding filter")
				return
			}
			filter.AwaitingDecision = awaiting
		}
		if !validFindingStatus(filter.Status) || !validFindingSeverity(filter.Severity) {
			httpx.Error(w, http.StatusBadRequest, "invalid quality-finding filter")
			return
		}
		if rawID := r.URL.Query().Get("instrument_id"); rawID != "" {
			id, err := instruments.ParseUUID(rawID)
			if err != nil {
				httpx.Error(w, http.StatusBadRequest, "invalid instrument ID")
				return
			}
			filter.InstrumentID = &id
		}
		limit, ok := queryLimit(r, 50)
		if !ok {
			httpx.Error(w, http.StatusBadRequest, "invalid limit")
			return
		}
		filter.Limit = limit
		findings, err := reader.ListQualityFindings(r.Context(), filter)
		if err != nil {
			writeMarketDataError(w, err)
			return
		}
		items := make([]qualityFindingResponse, 0, len(findings))
		for _, finding := range findings {
			items = append(items, qualityFindingDTO(finding))
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func queryLimit(r *http.Request, defaultValue int) (int, bool) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return defaultValue, true
	}
	value, err := strconv.Atoi(raw)
	return value, err == nil && value >= 1 && value <= 100
}

func validImportStatus(status marketdata.ImportStatus) bool {
	switch status {
	case "", marketdata.ImportQueued, marketdata.ImportRunning, marketdata.ImportSucceeded,
		marketdata.ImportPartial, marketdata.ImportFailed, marketdata.ImportCancelled:
		return true
	default:
		return false
	}
}

func validFindingStatus(status marketdata.FindingStatus) bool {
	switch status {
	case "", marketdata.FindingOpen, marketdata.FindingResolved, marketdata.FindingAcceptedLimitation:
		return true
	default:
		return false
	}
}

func validFindingSeverity(severity marketdata.FindingSeverity) bool {
	return severity == "" || severity == marketdata.SeverityWarning || severity == marketdata.SeverityError
}

func importRunDTO(run marketdata.ImportRun) importRunResponse {
	return importRunResponse{
		ID: run.ID.String(), Kind: run.Kind, Provider: run.Provider, RequestedFrom: run.RequestedFrom,
		RequestedTo: run.RequestedTo, Status: run.Status, ParentRunID: run.ParentRunID,
		StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, Counts: countsDTO(run.Counts),
		Error: errorDTO(run.Error), AppVersion: run.AppVersion,
	}
}

func importItemDTO(item marketdata.ImportItem) importItemResponse {
	return importItemResponse{
		RunID: item.RunID.String(), InstrumentID: item.InstrumentID.String(), RequestedFrom: item.RequestedFrom,
		RequestedTo: item.RequestedTo, Status: item.Status, Counts: countsDTO(item.Counts), Attempts: item.Attempts,
		StartedAt: item.StartedAt, FinishedAt: item.FinishedAt, Error: errorDTO(item.Error),
	}
}

func countsDTO(counts marketdata.ImportCounts) importCountsResponse {
	return importCountsResponse{counts.Processed, counts.Accepted, counts.Rejected, counts.Flagged, counts.Revised}
}

func errorDTO(safe *marketdata.SafeError) *safeErrorResponse {
	if safe == nil {
		return nil
	}
	normalized := marketdata.NormalizeSafeError(*safe)
	return &safeErrorResponse{Code: normalized.Code, Summary: normalized.Summary}
}

func writeMarketDataError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) || errors.Is(err, marketdata.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "not found")
		return
	}
	httpx.Error(w, http.StatusInternalServerError, "market-data request failed")
}

// acceptQualityFindingHandler records that the owner has judged a condition a limitation of the
// data. The product may report that asking the source again did not change the answer; it may not
// decide from that which conditions are acceptable.
func acceptQualityFindingHandler(decider FindingDecider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := httpx.PrincipalFromContext(r)
		if !ok || principal.Role != "owner" {
			httpx.Error(w, http.StatusForbidden, "owner authorization is required")
			return
		}
		findingID, err := instruments.ParseUUID(r.PathValue("id"))
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid finding ID")
			return
		}
		userID, err := instruments.ParseUUID(principal.UserID)
		if err != nil {
			httpx.Error(w, http.StatusForbidden, "owner authorization is required")
			return
		}
		finding, err := decider.AcceptFinding(r.Context(), findingID, userID, time.Now().UTC())
		switch {
		case errors.Is(err, marketdata.ErrFindingNotFound):
			httpx.Error(w, http.StatusNotFound, "no such finding")
			return
		case errors.Is(err, marketdata.ErrFindingNotAwaitingDecision):
			httpx.Error(w, http.StatusBadRequest,
				"this finding has not been re-examined, so there is nothing to decide yet")
			return
		case errors.Is(err, marketdata.ErrFindingNotOpen):
			httpx.Error(w, http.StatusConflict, "this finding is no longer open")
			return
		case err != nil:
			writeMarketDataError(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, qualityFindingDTO(finding))
	}
}
