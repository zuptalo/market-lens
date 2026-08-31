package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	clientevents "market-lens/server/internal/events"
)

func TestEventsStreamReplaysVersionedSharedEventsAfterLastID(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader := &eventReaderStub{events: []ClientEvent{
		{ID: 41, Type: "daily_bar.changed.v1", Version: 1, Scope: "shared", EntityType: "daily_bar", EntityID: "bar-1", Payload: json.RawMessage(`{"instrument_id":"one"}`), OccurredAt: time.Date(2026, 8, 29, 8, 0, 0, 0, time.UTC)},
		{ID: 42, Type: "import_run.changed.v1", Version: 1, Scope: "shared", EntityType: "import_run", EntityID: "run-1", Payload: json.RawMessage(`{"status":"succeeded"}`), OccurredAt: time.Date(2026, 8, 29, 8, 0, 1, 0, time.UTC)},
	}, afterList: cancel}
	router := NewRouter(authenticatedDependencies(Dependencies{Events: reader, EventHeartbeat: time.Hour, EventBatchLimit: 50}))
	recorder := httptest.NewRecorder()
	request := authenticatedAPIRequest(http.MethodGet, "/api/v1/events").WithContext(ctx)
	request.Header.Set("Last-Event-ID", "40")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "text/event-stream" ||
		recorder.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("response = %d %#v %s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
	body := recorder.Body.String()
	if strings.Index(body, "id: 41") > strings.Index(body, "id: 42") ||
		!strings.Contains(body, "event: daily_bar.changed.v1") || !strings.Contains(body, `"version":1`) ||
		!strings.Contains(body, `"scope":"shared"`) || strings.Contains(body, `"scope":"private"`) {
		t.Fatalf("stream = %q", body)
	}
	if reader.after != 40 || reader.audience.UserID != "10000000-0000-4000-8000-000000000001" ||
		reader.audience.Role != "owner" || reader.limit != 50 {
		t.Fatalf("replay query = after %d audience %#v limit %d", reader.after, reader.audience, reader.limit)
	}
}

func TestEventsStreamValidatesResumeIDAndEmitsHeartbeat(t *testing.T) {
	router := NewRouter(authenticatedDependencies(Dependencies{Events: &eventReaderStub{}}))
	recorder := httptest.NewRecorder()
	request := authenticatedAPIRequest(http.MethodGet, "/api/v1/events")
	request.Header.Set("Last-Event-ID", "invalid")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"error":`) {
		t.Fatalf("invalid resume response = %d %s", recorder.Code, recorder.Body.String())
	}

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	heartbeat := httptest.NewRecorder()
	heartbeatRequest := authenticatedAPIRequest(http.MethodGet, "/api/v1/events").WithContext(ctx)
	NewRouter(authenticatedDependencies(Dependencies{Events: &eventReaderStub{}, EventHeartbeat: 5 * time.Millisecond})).ServeHTTP(heartbeat, heartbeatRequest)
	if !strings.Contains(heartbeat.Body.String(), ": heartbeat") {
		t.Fatalf("heartbeat stream = %q", heartbeat.Body.String())
	}
}

func TestEventsStreamStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		recorder := httptest.NewRecorder()
		request := authenticatedAPIRequest(http.MethodGet, "/api/v1/events").WithContext(ctx)
		NewRouter(authenticatedDependencies(Dependencies{Events: &eventReaderStub{}})).ServeHTTP(recorder, request)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("event stream did not stop after cancellation")
	}
}

type eventReaderStub struct {
	mu           sync.Mutex
	events       []ClientEvent
	afterList    func()
	after        int64
	audience     clientevents.Audience
	limit        int
	resolveRole  string
	resolveErr   error
	deactivated  bool
	resolveCalls int
	head         int64
	headCalls    int
}

// Head stands in for the current end of the event log, which a new subscriber starts from.
func (s *eventReaderStub) Head(context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.headCalls++
	return s.head, nil
}

// Audience stands in for the durable user record the real reader consults.
func (s *eventReaderStub) Audience(_ context.Context, userID string) (clientevents.Audience, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolveCalls++
	if s.resolveErr != nil {
		return clientevents.Audience{}, s.resolveErr
	}
	role := s.resolveRole
	if role == "" {
		role = "owner"
	}
	return clientevents.Audience{UserID: userID, Role: role, Deactivated: s.deactivated}, nil
}

func (s *eventReaderStub) ListAuthorized(_ context.Context, audience clientevents.Audience, after int64, limit int) ([]ClientEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.after, s.audience, s.limit = after, audience, limit
	events := append([]ClientEvent(nil), s.events...)
	s.events = nil
	if s.afterList != nil {
		s.afterList()
		s.afterList = nil
	}
	return events, nil
}
