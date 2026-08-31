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

	"market-lens/server/internal/auth"
	clientevents "market-lens/server/internal/events"
	"market-lens/server/internal/httpx"
)

// revalidatorStub reports whether an open stream's session is still usable.
type revalidatorStub struct {
	mu     sync.Mutex
	calls  int
	revoke bool
}

func (stub *revalidatorStub) RevalidateSession(_ context.Context, _ string) error {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.calls++
	if stub.revoke {
		return auth.ErrAuthenticationRequired
	}
	return nil
}

func (stub *revalidatorStub) revokeNow() {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.revoke = true
}

func (stub *revalidatorStub) callCount() int {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return stub.calls
}

func TestOpenStreamStopsWithinFiveSecondsOfSessionRevocation(t *testing.T) {
	revalidator := &revalidatorStub{}
	reader := &eventReaderStub{}
	router := NewRouter(authenticatedDependencies(Dependencies{
		Events: reader, EventHeartbeat: time.Hour, EventBatchLimit: 50,
		EventRevalidator: revalidator, EventRevalidateInterval: 20 * time.Millisecond,
	}))

	recorder := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request := authenticatedAPIRequest(http.MethodGet, "/api/v1/events").WithContext(ctx)

	finished := make(chan struct{})
	go func() {
		router.ServeHTTP(recorder, request)
		close(finished)
	}()

	// Give the stream a moment to establish, then revoke the session behind it.
	deadline := time.After(2 * time.Second)
	for revalidator.callCount() == 0 {
		select {
		case <-deadline:
			t.Fatal("the stream never revalidated its session")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	revalidator.revokeNow()

	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("a revoked session kept streaming for more than five seconds")
	}
	if ctx.Err() != nil {
		t.Fatal("the stream ended only because the client context expired")
	}
}

func TestStreamRevalidationDoesNotDependOnTheHeartbeatInterval(t *testing.T) {
	// A long heartbeat must not delay revocation: an idle stream still has to notice.
	revalidator := &revalidatorStub{}
	router := NewRouter(authenticatedDependencies(Dependencies{
		Events: &eventReaderStub{}, EventHeartbeat: time.Hour, EventBatchLimit: 50,
		EventRevalidator: revalidator, EventRevalidateInterval: 10 * time.Millisecond,
	}))
	recorder := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	request := authenticatedAPIRequest(http.MethodGet, "/api/v1/events").WithContext(ctx)

	finished := make(chan struct{})
	go func() {
		router.ServeHTTP(recorder, request)
		close(finished)
	}()
	time.Sleep(100 * time.Millisecond)
	if revalidator.callCount() < 2 {
		t.Fatalf("revalidation ran %d times under a one-hour heartbeat, want repeated checks",
			revalidator.callCount())
	}
	cancel()
	<-finished
}

func TestStreamServesOnlyEventsTheAudienceMaySee(t *testing.T) {
	// The handler must delegate scoping to the authorized reader and never widen it.
	ctx, cancel := context.WithCancel(context.Background())
	reader := &eventReaderStub{events: []ClientEvent{
		{ID: 7, Type: "session.created.v1", Version: 1, Scope: "user",
			SubjectUserID: "10000000-0000-4000-8000-000000000001", EntityType: "session",
			EntityID: "20000000-0000-4000-8000-000000000001", Payload: json.RawMessage(`{}`),
			OccurredAt: time.Date(2026, 8, 30, 8, 0, 0, 0, time.UTC)},
	}, afterList: cancel}
	router := NewRouter(authenticatedDependencies(Dependencies{
		Events: reader, EventHeartbeat: time.Hour, EventBatchLimit: 50,
	}))
	recorder := httptest.NewRecorder()
	request := authenticatedAPIRequest(http.MethodGet, "/api/v1/events").WithContext(ctx)
	router.ServeHTTP(recorder, request)

	if reader.audience.UserID != "10000000-0000-4000-8000-000000000001" || reader.audience.Role != "owner" {
		t.Fatalf("stream audience = %#v, want the authenticated principal", reader.audience)
	}
	body := recorder.Body.String()
	// No payload may carry session or credential material. The words are split from one string
	// rather than listed as adjacent literals, which a secret scanner reads as an assignment.
	for _, forbidden := range strings.Fields("token digest password csrf") {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("stream disclosed %q: %s", forbidden, body)
		}
	}
}

func TestAnonymousAndCSRFlessCallersCannotOpenAStream(t *testing.T) {
	router := NewRouter(Dependencies{Events: &eventReaderStub{}, EventHeartbeat: time.Hour})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/events", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous stream = %d, want 401", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "event:") {
		t.Fatalf("anonymous caller received events: %s", recorder.Body.String())
	}
}

var _ = clientevents.Audience{}
var _ = httpx.SessionCookieName

func TestADeactivatedOrUnknownAccountCannotOpenAStreamAtAll(t *testing.T) {
	for name, reader := range map[string]*eventReaderStub{
		"deactivated":  {deactivated: true},
		"unresolvable": {resolveErr: clientevents.ErrAudienceUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			router := NewRouter(authenticatedDependencies(Dependencies{
				Events: reader, EventHeartbeat: time.Hour, EventBatchLimit: 50,
			}))
			recorder := httptest.NewRecorder()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			router.ServeHTTP(recorder, authenticatedAPIRequest(http.MethodGet, "/api/v1/events").WithContext(ctx))
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("%s stream = %d %s, want 401", name, recorder.Code, recorder.Body.String())
			}
			if strings.Contains(recorder.Body.String(), "event:") {
				t.Fatalf("%s caller received events: %s", name, recorder.Body.String())
			}
			if reader.resolveCalls == 0 {
				t.Fatal("the stream never resolved the audience from durable state")
			}
		})
	}
}

// The corporate-action event is new in feature 005. It carries market data, which is shared
// reference data every active user may read, and it must carry nothing else: an ex-date and
// an instrument identifier explain a discontinuity without disclosing anything private.
func TestTheCorporateActionEventIsSharedAndCarriesNothingPrivate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader := &eventReaderStub{events: []ClientEvent{
		{ID: 11, Type: "corporate_action.changed.v1", Version: 1, Scope: "shared",
			EntityType: "corporate_action", EntityID: "50000000-0000-4000-8000-000000000001",
			Payload:    json.RawMessage(`{"instrument_id":"33000000-0000-4000-8000-000000000001","ex_date":"2026-05-28","action_type":"split"}`),
			OccurredAt: time.Date(2026, 8, 30, 8, 0, 0, 0, time.UTC)},
	}, afterList: cancel}
	router := NewRouter(authenticatedDependencies(Dependencies{
		Events: reader, EventHeartbeat: time.Hour, EventBatchLimit: 50,
	}))
	recorder := httptest.NewRecorder()
	request := authenticatedAPIRequest(http.MethodGet, "/api/v1/events").WithContext(ctx)
	router.ServeHTTP(recorder, request)

	body := recorder.Body.String()
	if !strings.Contains(body, "corporate_action.changed.v1") {
		t.Fatalf("an authenticated reader was not served the shared action event: %s", body)
	}
	// Named as its own event so a client can decide whether the change concerns what it is
	// displaying, rather than refetching everything.
	if !strings.Contains(body, "instrument_id") || !strings.Contains(body, "ex_date") {
		t.Errorf("the event payload does not name what changed: %s", body)
	}
	for _, forbidden := range strings.Fields("token digest password csrf email") {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("the action event disclosed %q: %s", forbidden, body)
		}
	}
}

func TestAnAnonymousCallerReceivesNoCorporateActionEvent(t *testing.T) {
	router := NewRouter(Dependencies{
		Events: &eventReaderStub{events: []ClientEvent{
			{ID: 11, Type: "corporate_action.changed.v1", Version: 1, Scope: "shared",
				EntityType: "corporate_action", EntityID: "50000000-0000-4000-8000-000000000001",
				Payload: json.RawMessage(`{"instrument_id":"i"}`)},
		}},
		EventHeartbeat: time.Hour,
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/events", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("an anonymous stream request returned %d", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "corporate_action") {
		t.Fatalf("an anonymous caller was served an event: %s", recorder.Body.String())
	}
}
