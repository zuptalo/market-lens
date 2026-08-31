package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	clientevents "market-lens/server/internal/events"
	"market-lens/server/internal/httpx"
)

type ClientEvent = clientevents.Event

// EventReader both resolves who a stream is for and serves that audience its authorized replay.
// Resolution belongs here rather than in the handler because role and status are durable facts,
// not something a request or a long-lived connection may keep asserting for itself.
type EventReader interface {
	Audience(context.Context, string) (clientevents.Audience, error)
	ListAuthorized(context.Context, clientevents.Audience, int64, int) ([]ClientEvent, error)
}

// maxRevalidateInterval bounds how long a revoked or deactivated session may keep an already
// open stream. The product commitment is five seconds, so nothing slower is accepted.
const maxRevalidateInterval = 5 * time.Second

type eventEnvelope struct {
	Version       int             `json:"version"`
	Scope         string          `json:"scope"`
	SubjectUserID string          `json:"subject_user_id,omitempty"`
	EntityType    string          `json:"entity_type"`
	EntityID      string          `json:"entity_id"`
	Payload       json.RawMessage `json:"payload"`
	OccurredAt    time.Time       `json:"occurred_at"`
}

func eventsHandler(reader EventReader, heartbeat time.Duration, batchLimit int,
	revalidator StreamRevalidator, revalidateInterval time.Duration) http.HandlerFunc {
	if heartbeat <= 0 {
		heartbeat = 15 * time.Second
	}
	if batchLimit < 1 || batchLimit > 1000 {
		batchLimit = 100
	}
	if revalidateInterval <= 0 || revalidateInterval > maxRevalidateInterval {
		revalidateInterval = 2 * time.Second
	}
	return func(w http.ResponseWriter, r *http.Request) {
		principal, authenticated := httpx.PrincipalFromContext(r)
		if !authenticated {
			writeAuthenticationError(w, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
			return
		}
		audience, err := reader.Audience(r.Context(), principal.UserID)
		if err != nil || audience.Deactivated {
			writeAuthenticationError(w, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
			return
		}
		after, ok := eventResumeID(r)
		if !ok {
			httpx.Error(w, http.StatusBadRequest, "invalid event resume ID")
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			httpx.Error(w, http.StatusInternalServerError, "event streaming is unavailable")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		ticker := time.NewTicker(heartbeat)
		defer ticker.Stop()
		revalidate := time.NewTicker(revalidateInterval)
		defer revalidate.Stop()
		for {
			if err := r.Context().Err(); err != nil {
				return
			}
			events, err := reader.ListAuthorized(r.Context(), audience, after, batchLimit)
			if err != nil {
				return
			}
			if len(events) > 0 {
				for _, event := range events {
					data, err := json.Marshal(eventEnvelope{
						Version: event.Version, Scope: event.Scope, SubjectUserID: event.SubjectUserID,
						EntityType: event.EntityType, EntityID: event.EntityID, Payload: event.Payload,
						OccurredAt: event.OccurredAt,
					})
					if err != nil {
						return
					}
					if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.ID, event.Type, data); err != nil {
						return
					}
					after = event.ID
				}
				flusher.Flush()
				continue
			}
			select {
			case <-r.Context().Done():
				return
			case <-revalidate.C:
				// An open stream outlives the check that admitted it, so the session and the
				// account behind it are re-read on a bound the product can promise.
				if !streamStillAuthorized(r.Context(), reader, revalidator, principal.SessionID, principal.UserID, &audience) {
					return
				}
			case <-ticker.C:
				if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}

// streamStillAuthorized re-reads the durable session and audience behind an open stream and
// narrows or ends it. A demotion narrows the audience; a revocation or deactivation ends it.
func streamStillAuthorized(ctx context.Context, reader EventReader, revalidator StreamRevalidator,
	sessionID, userID string, audience *clientevents.Audience) bool {
	if revalidator != nil {
		if err := revalidator.RevalidateSession(ctx, sessionID); err != nil {
			return false
		}
	}
	current, err := reader.Audience(ctx, userID)
	if err != nil || current.Deactivated {
		return false
	}
	*audience = current
	return true
}

func eventResumeID(r *http.Request) (int64, bool) {
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		raw = r.URL.Query().Get("last_event_id")
	}
	if raw == "" {
		return 0, true
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	return value, err == nil && value >= 0
}
