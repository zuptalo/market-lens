package api

import (
	"net/http"
	"time"

	"market-lens/server/internal/httpx"
	"market-lens/server/internal/marketdata"
)

// fallbackResponse is the banner's snapshot (feature 030). It is shared data — which source the
// prices come from is the same for everybody — so any signed-in person may read it; changing it is
// an owner command, not an endpoint.
type fallbackResponse struct {
	Active                bool       `json:"active"`
	Provider              string     `json:"provider"`
	Since                 *string    `json:"since"`
	Instruments           int        `json:"instruments"`
	PendingReconciliation bool       `json:"pending_reconciliation"`
	ChangedAt             *time.Time `json:"changed_at"`
}

func getFallbackHandler(reader FallbackReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state, err := reader.State(r.Context())
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "fallback state is unavailable")
			return
		}
		response := fallbackResponse{Active: state.Active, Provider: marketdata.FallbackProvider,
			Instruments: state.Instruments, PendingReconciliation: state.PendingReconciliation}
		if state.Since != nil {
			since := state.Since.String()
			response.Since = &since
		}
		if !state.Recorded.ChangedAt.IsZero() {
			changed := state.Recorded.ChangedAt.UTC()
			response.ChangedAt = &changed
		}
		httpx.JSON(w, http.StatusOK, response)
	}
}
