package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"market-lens/server/internal/notify"
)

type notificationServiceStub struct {
	askedFor   string
	setKind    notify.Kind
	setChannel notify.Channel
	setEnabled bool
	quietHours *notify.QuietHours
	subscribed notify.SubscribeRequest
	revoked    string
	token      string
	err        error
	ownerOnly  bool
}

func (s *notificationServiceStub) Settings(_ context.Context, userID string) (notify.Settings, error) {
	s.askedFor = userID
	kinds := []notify.Kind{notify.KindDecisionWaiting, notify.KindPaperFill, notify.KindSignalChange}
	if s.ownerOnly {
		kinds = append(kinds, notify.KindPipelineFailure)
	}
	settings := notify.Settings{NothingIsOnByDefault: true, QuietHours: &notify.QuietHours{
		StartsAt: "22:00", EndsAt: "07:00", Timezone: "Europe/Stockholm"}}
	for _, kind := range kinds {
		for _, channel := range notify.Channels {
			settings.Preferences = append(settings.Preferences, notify.Preference{
				Kind: kind, Channel: channel,
				Enabled: kind == notify.KindDecisionWaiting && channel == notify.ChannelEmail,
			})
		}
	}
	return settings, nil
}

func (s *notificationServiceStub) SetPreference(_ context.Context, userID string, kind notify.Kind,
	channel notify.Channel, enabled bool) (notify.Settings, error) {
	s.askedFor, s.setKind, s.setChannel, s.setEnabled = userID, kind, channel, enabled
	if s.err != nil {
		return notify.Settings{}, s.err
	}
	return s.Settings(context.Background(), userID)
}

func (s *notificationServiceStub) SetQuietHours(_ context.Context, userID string,
	hours *notify.QuietHours) (notify.Settings, error) {
	s.askedFor, s.quietHours = userID, hours
	if s.err != nil {
		return notify.Settings{}, s.err
	}
	return s.Settings(context.Background(), userID)
}

func (s *notificationServiceStub) Subscribe(_ context.Context, userID string,
	request notify.SubscribeRequest) (notify.Subscription, error) {
	s.askedFor, s.subscribed = userID, request
	return notify.Subscription{}, s.err
}

func (s *notificationServiceStub) Subscriptions(_ context.Context, userID string) ([]notify.Subscription, error) {
	s.askedFor = userID
	used := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	return []notify.Subscription{{
		ID: "55555555-5555-4555-8555-555555555555", Label: "Kamran's phone",
		CreatedAt: used, LastUsedAt: &used,
	}}, nil
}

func (s *notificationServiceStub) Revoke(_ context.Context, userID, subscriptionID string) error {
	s.askedFor, s.revoked = userID, subscriptionID
	return s.err
}

func (s *notificationServiceStub) History(_ context.Context, userID string) ([]notify.Record, error) {
	s.askedFor = userID
	reason := "connection refused"
	created := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	return []notify.Record{{
		Kind: notify.KindDecisionWaiting, Channel: notify.ChannelEmail, State: notify.StateFailed,
		Count: 2, Attempts: 3, LastError: &reason, CreatedAt: created,
	}}, nil
}

func (s *notificationServiceStub) PublicPushKey(_ context.Context, userID string) (string, error) {
	s.askedFor = userID
	return "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4", nil
}

func (s *notificationServiceStub) Unsubscribe(_ context.Context, token string) (notify.Kind, notify.Channel, error) {
	s.token = token
	if s.err != nil {
		return "", "", s.err
	}
	return notify.KindDecisionWaiting, notify.ChannelEmail, nil
}

func (s *notificationServiceStub) SendTestEmail(_ context.Context, userID string) error {
	s.askedFor = userID
	return s.err
}

func notificationRouter(stub *notificationServiceStub) http.Handler {
	return NewRouter(authenticatedDependencies(Dependencies{Notifications: stub}))
}

func TestNotificationSettingsMatchTheContract(t *testing.T) {
	response := performRequest(notificationRouter(&notificationServiceStub{}),
		"/api/v1/notifications/preferences")
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	body := decodeBody(t, response)

	if body["nothing_is_on_by_default"] != true {
		t.Errorf("the settings do not state that nothing is on by default")
	}
	preferences, _ := body["preferences"].([]any)
	if len(preferences) != 6 {
		t.Fatalf("%d switches for a member, want three kinds on two channels", len(preferences))
	}
	first, _ := preferences[0].(map[string]any)
	if first["kind"] != "decision_waiting" || first["channel"] != "email" || first["enabled"] != true {
		t.Errorf("the first preference reads %#v", first)
	}
	hours, _ := body["quiet_hours"].(map[string]any)
	if hours["starts_at"] != "22:00" || hours["timezone"] != "Europe/Stockholm" {
		t.Errorf("the quiet hours read %#v", hours)
	}
}

// A device list is not a list of endpoints. An endpoint is where somebody reads their mail, and a
// page has no use for one.
func TestADeviceListCarriesNoEndpointAndNoKeys(t *testing.T) {
	response := performRequest(notificationRouter(&notificationServiceStub{}),
		"/api/v1/notifications/subscriptions")
	body := strings.ToLower(response.Body.String())
	for _, forbidden := range []string{"endpoint", "p256dh", "auth", "https://", "user_agent"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the device list carries %q: %s", forbidden, response.Body.String())
		}
	}
	if !strings.Contains(response.Body.String(), "Kamran's phone") {
		t.Errorf("the device list does not name the device")
	}
}

func TestNotificationWritesRequireTheCSRFHeader(t *testing.T) {
	router := notificationRouter(&notificationServiceStub{})
	for _, write := range []struct{ method, path, body string }{
		{http.MethodPut, "/api/v1/notifications/preferences", `{"kind":"paper_fill","channel":"email","enabled":true}`},
		{http.MethodPut, "/api/v1/notifications/quiet-hours", `{"starts_at":"22:00","ends_at":"07:00","timezone":"UTC"}`},
		{http.MethodPost, "/api/v1/notifications/subscriptions", `{"endpoint":"https://x","p256dh":"a","auth":"b","label":"c"}`},
		{http.MethodDelete, "/api/v1/notifications/subscriptions/55555555-5555-4555-8555-555555555555", ``},
	} {
		request := authenticatedAPIRequest(write.method, write.path)
		request.Body = io.NopCloser(strings.NewReader(write.body))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden {
			t.Errorf("%s %s without the CSRF header returned %d", write.method, write.path, recorder.Code)
		}
	}
}

func TestTurningSomethingOnReachesTheServiceWithTheCaller(t *testing.T) {
	stub := &notificationServiceStub{}
	response := performNotificationWrite(t, notificationRouter(stub), http.MethodPut,
		"/api/v1/notifications/preferences",
		`{"kind":"paper_fill","channel":"web_push","enabled":true}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	if stub.setKind != notify.KindPaperFill || stub.setChannel != notify.ChannelWebPush || !stub.setEnabled {
		t.Errorf("the service was asked for %s/%s=%v", stub.setKind, stub.setChannel, stub.setEnabled)
	}
	if stub.askedFor == "" {
		t.Errorf("the service was not told whose preference it is")
	}
}

// Half a window is not a thing: any part missing clears it entirely.
func TestClearingQuietHoursSendsNothingRatherThanHalfAWindow(t *testing.T) {
	stub := &notificationServiceStub{}
	performNotificationWrite(t, notificationRouter(stub), http.MethodPut,
		"/api/v1/notifications/quiet-hours", `{"starts_at":null,"ends_at":null,"timezone":null}`)
	if stub.quietHours != nil {
		t.Errorf("clearing the window sent %+v", stub.quietHours)
	}

	performNotificationWrite(t, notificationRouter(stub), http.MethodPut,
		"/api/v1/notifications/quiet-hours", `{"starts_at":"22:00","ends_at":"","timezone":"UTC"}`)
	if stub.quietHours != nil {
		t.Errorf("half a window was sent as a window: %+v", stub.quietHours)
	}
}

// A kind somebody is not offered is refused with a status that says so, rather than 400.
func TestAKindNotOfferedToThisPersonIsForbidden(t *testing.T) {
	stub := &notificationServiceStub{err: notify.Refusal{
		Code: notify.RefusalNotAvailable, Message: "That kind is only offered to the owner."}}
	response := performNotificationWrite(t, notificationRouter(stub), http.MethodPut,
		"/api/v1/notifications/preferences",
		`{"kind":"pipeline_failure","channel":"email","enabled":true}`)
	if response.Code != http.StatusForbidden {
		t.Errorf("asking for a kind you are not offered returned %d", response.Code)
	}
}

// The one route that works without a session, because an unsubscribe that needs a sign-in is one
// people do not use.
func TestUnsubscribingNeedsNoSession(t *testing.T) {
	stub := &notificationServiceStub{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/unsubscribe",
		strings.NewReader(`{"token":"abc.def"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	notificationRouter(stub).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	if stub.token != "abc.def" {
		t.Errorf("the service was given the token %q", stub.token)
	}
	body := decodeBody(t, recorder)
	if body["stopped"] != true || body["kind"] != "decision_waiting" {
		t.Errorf("the answer reads %#v", body)
	}
}

func TestAnUnreadableUnsubscribeLinkSaysSo(t *testing.T) {
	stub := &notificationServiceStub{err: notify.Refusal{
		Code: notify.RefusalInvalidToken, Message: "This link is not readable."}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/unsubscribe",
		strings.NewReader(`{"token":"nonsense"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	notificationRouter(stub).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("a broken link returned %d", recorder.Code)
	}
}

func TestNotificationReadsAreRefusedWithoutASession(t *testing.T) {
	for _, path := range []string{
		"/api/v1/notifications/preferences",
		"/api/v1/notifications/subscriptions",
		"/api/v1/notifications/history",
		"/api/v1/notifications/push-key",
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		notificationRouter(&notificationServiceStub{}).ServeHTTP(recorder, request)
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without a session returned %d", path, recorder.Code)
		}
	}
}

// Only the public half of the instance key is ever served.
func TestOnlyThePublicPushKeyIsServed(t *testing.T) {
	response := performRequest(notificationRouter(&notificationServiceStub{}),
		"/api/v1/notifications/push-key")
	body := decodeBody(t, response)
	if body["public_key"] == nil || body["public_key"] == "" {
		t.Errorf("no public key was served")
	}
	if _, present := body["private_key"]; present {
		t.Errorf("the private key was served")
	}
}

// A test send goes to the caller's own address. Taking a recipient would be a way to make this
// installation mail a stranger.
func TestATestSendTakesNoRecipient(t *testing.T) {
	stub := &notificationServiceStub{}
	response := performNotificationWrite(t, notificationRouter(stub), http.MethodPost,
		"/api/v1/notifications/test-email", `{"to":"somebody-else@example.com"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	if stub.askedFor == "" {
		t.Errorf("the service was not told whose address to use")
	}
	if strings.Contains(response.Body.String(), "somebody-else") {
		t.Errorf("a recipient from the request reached the response")
	}
}

func performNotificationWrite(t *testing.T, router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := authenticatedAPIRequest(method, path)
	request.Body = io.NopCloser(bytes.NewReader([]byte(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "test-csrf")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}
