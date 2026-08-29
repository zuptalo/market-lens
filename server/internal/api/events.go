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

type EventReader interface {
	ListClientEvents(context.Context, string, int64, int) ([]ClientEvent, error)
}

type eventEnvelope struct {
	Version    int             `json:"version"`
	Scope      string          `json:"scope"`
	EntityType string          `json:"entity_type"`
	EntityID   string          `json:"entity_id"`
	Payload    json.RawMessage `json:"payload"`
	OccurredAt time.Time       `json:"occurred_at"`
}

func eventsHandler(reader EventReader, scope string, heartbeat time.Duration, batchLimit int) http.HandlerFunc {
	if scope == "" {
		scope = "shared"
	}
	if heartbeat <= 0 {
		heartbeat = 15 * time.Second
	}
	if batchLimit < 1 || batchLimit > 1000 {
		batchLimit = 100
	}
	return func(w http.ResponseWriter, r *http.Request) {
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
		for {
			if err := r.Context().Err(); err != nil {
				return
			}
			events, err := reader.ListClientEvents(r.Context(), scope, after, batchLimit)
			if err != nil {
				return
			}
			if len(events) > 0 {
				for _, event := range events {
					data, err := json.Marshal(eventEnvelope{
						Version: event.Version, Scope: event.Scope, EntityType: event.EntityType,
						EntityID: event.EntityID, Payload: event.Payload, OccurredAt: event.OccurredAt,
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
			case <-ticker.C:
				if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
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
