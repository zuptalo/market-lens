package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"market-lens/server/internal/httpx"
	"market-lens/server/internal/notify"
)

// NotificationService is what a person asked to be told about, and where.
//
// Every method but Unsubscribe takes the caller's user identifier, and no route carries one. The
// exception is the point: an unsubscribe link has to work for somebody who is not signed in, which
// is why it is a signed single-purpose token instead of a session.
type NotificationService interface {
	Settings(ctx context.Context, userID string) (notify.Settings, error)
	SetPreference(ctx context.Context, userID string, kind notify.Kind, channel notify.Channel,
		enabled bool) (notify.Settings, error)
	SetQuietHours(ctx context.Context, userID string, hours *notify.QuietHours) (notify.Settings, error)
	Subscribe(ctx context.Context, userID string, request notify.SubscribeRequest) (notify.Subscription, error)
	Subscriptions(ctx context.Context, userID string) ([]notify.Subscription, error)
	Revoke(ctx context.Context, userID, subscriptionID string) error
	History(ctx context.Context, userID string) ([]notify.Record, error)
	PublicPushKey(ctx context.Context, userID string) (string, error)
	Unsubscribe(ctx context.Context, token string) (notify.Kind, notify.Channel, error)
	SendTestEmail(ctx context.Context, userID string) error
}

type preferenceResponse struct {
	Kind    string `json:"kind"`
	Channel string `json:"channel"`
	Enabled bool   `json:"enabled"`
}

type quietHoursResponse struct {
	StartsAt string `json:"starts_at"`
	EndsAt   string `json:"ends_at"`
	Timezone string `json:"timezone"`
}

type notificationSettingsResponse struct {
	Preferences []preferenceResponse `json:"preferences"`
	QuietHours  *quietHoursResponse  `json:"quiet_hours"`
	// NothingIsOnByDefault is always true and always present. A product that mails somebody unasked
	// has decided on their behalf.
	NothingIsOnByDefault bool `json:"nothing_is_on_by_default"`
}

func notificationSettingsDTO(settings notify.Settings) notificationSettingsResponse {
	response := notificationSettingsResponse{
		Preferences:          make([]preferenceResponse, 0, len(settings.Preferences)),
		NothingIsOnByDefault: true,
	}
	for _, preference := range settings.Preferences {
		response.Preferences = append(response.Preferences, preferenceResponse{
			Kind: string(preference.Kind), Channel: string(preference.Channel),
			Enabled: preference.Enabled,
		})
	}
	if settings.QuietHours != nil {
		response.QuietHours = &quietHoursResponse{
			StartsAt: settings.QuietHours.StartsAt, EndsAt: settings.QuietHours.EndsAt,
			Timezone: settings.QuietHours.Timezone,
		}
	}
	return response
}

type subscriptionResponse struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// No endpoint and no keys. They are what is needed to send to a device, not something a page
	// has any use for — and a list of endpoints is a list of where somebody reads their mail.
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

type notificationRecordResponse struct {
	Kind      string     `json:"kind"`
	Channel   string     `json:"channel"`
	State     string     `json:"state"`
	Count     int        `json:"count"`
	Attempts  int        `json:"attempts"`
	LastError *string    `json:"last_error"`
	CreatedAt time.Time  `json:"created_at"`
	SentAt    *time.Time `json:"sent_at"`
}

func getNotificationSettingsHandler(service NotificationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := callerOf(w, r)
		if !ok {
			return
		}
		settings, err := service.Settings(r.Context(), userID)
		if err != nil {
			writeNotificationRefusal(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, notificationSettingsDTO(settings))
	}
}

func setNotificationPreferenceHandler(service NotificationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := callerOf(w, r)
		if !ok {
			return
		}
		var body struct {
			Kind    string `json:"kind"`
			Channel string `json:"channel"`
			Enabled bool   `json:"enabled"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			writeNotificationError(w, http.StatusBadRequest, "invalid_request",
				"The request body is not readable.")
			return
		}
		settings, err := service.SetPreference(r.Context(), userID,
			notify.Kind(body.Kind), notify.Channel(body.Channel), body.Enabled)
		if err != nil {
			writeNotificationRefusal(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, notificationSettingsDTO(settings))
	}
}

func setQuietHoursHandler(service NotificationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := callerOf(w, r)
		if !ok {
			return
		}
		var body struct {
			StartsAt *string `json:"starts_at"`
			EndsAt   *string `json:"ends_at"`
			Timezone *string `json:"timezone"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			writeNotificationError(w, http.StatusBadRequest, "invalid_request",
				"The request body is not readable.")
			return
		}
		// Any part missing clears the window entirely. Half a window is not a thing.
		var hours *notify.QuietHours
		if body.StartsAt != nil && body.EndsAt != nil && body.Timezone != nil &&
			*body.StartsAt != "" && *body.EndsAt != "" && *body.Timezone != "" {
			hours = &notify.QuietHours{
				StartsAt: *body.StartsAt, EndsAt: *body.EndsAt, Timezone: *body.Timezone,
			}
		}
		settings, err := service.SetQuietHours(r.Context(), userID, hours)
		if err != nil {
			writeNotificationRefusal(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, notificationSettingsDTO(settings))
	}
}

func getPushKeyHandler(service NotificationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := callerOf(w, r)
		if !ok {
			return
		}
		key, err := service.PublicPushKey(r.Context(), userID)
		if err != nil {
			writeNotificationRefusal(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]string{"public_key": key})
	}
}

func listSubscriptionsHandler(service NotificationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := callerOf(w, r)
		if !ok {
			return
		}
		respondWithSubscriptions(w, r, service, userID, http.StatusOK)
	}
}

func subscribeHandler(service NotificationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := callerOf(w, r)
		if !ok {
			return
		}
		var body struct {
			Endpoint string `json:"endpoint"`
			P256DH   string `json:"p256dh"`
			Auth     string `json:"auth"`
			Label    string `json:"label"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body); err != nil {
			writeNotificationError(w, http.StatusBadRequest, "invalid_request",
				"The request body is not readable.")
			return
		}
		if _, err := service.Subscribe(r.Context(), userID, notify.SubscribeRequest{
			Endpoint: body.Endpoint, P256DH: body.P256DH, Auth: body.Auth, Label: body.Label,
		}); err != nil {
			writeNotificationRefusal(w, err)
			return
		}
		respondWithSubscriptions(w, r, service, userID, http.StatusCreated)
	}
}

func revokeSubscriptionHandler(service NotificationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := callerOf(w, r)
		if !ok {
			return
		}
		if err := service.Revoke(r.Context(), userID, r.PathValue("id")); err != nil {
			writeNotificationRefusal(w, err)
			return
		}
		respondWithSubscriptions(w, r, service, userID, http.StatusOK)
	}
}

func notificationHistoryHandler(service NotificationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := callerOf(w, r)
		if !ok {
			return
		}
		records, err := service.History(r.Context(), userID)
		if err != nil {
			writeNotificationRefusal(w, err)
			return
		}
		response := make([]notificationRecordResponse, 0, len(records))
		for _, record := range records {
			response = append(response, notificationRecordResponse{
				Kind: string(record.Kind), Channel: string(record.Channel),
				State: string(record.State), Count: record.Count, Attempts: record.Attempts,
				LastError: record.LastError, CreatedAt: record.CreatedAt, SentAt: record.SentAt,
			})
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"notifications": response})
	}
}

// unsubscribeHandler is the one route here that does not need a session.
//
// An unsubscribe that requires signing in is one people do not use: they mark the mail as spam
// instead, and the sending domain pays for it. The token names one person, one kind and one
// channel, and can only ever turn something off.
func unsubscribeHandler(service NotificationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			writeNotificationError(w, http.StatusBadRequest, "invalid_token",
				"This link is not readable.")
			return
		}
		kind, channel, err := service.Unsubscribe(r.Context(), body.Token)
		if err != nil {
			writeNotificationRefusal(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{
			"kind": string(kind), "channel": string(channel), "stopped": true,
		})
	}
}

// sendTestEmailHandler proves mail works, to the caller's own address and nowhere else. A test send
// that took a recipient would be a way to make this installation mail a stranger.
func sendTestEmailHandler(service NotificationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := callerOf(w, r)
		if !ok {
			return
		}
		if err := service.SendTestEmail(r.Context(), userID); err != nil {
			writeNotificationRefusal(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"sent": true})
	}
}

func respondWithSubscriptions(w http.ResponseWriter, r *http.Request, service NotificationService,
	userID string, status int) {
	subscriptions, err := service.Subscriptions(r.Context(), userID)
	if err != nil {
		writeNotificationRefusal(w, err)
		return
	}
	response := make([]subscriptionResponse, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		response = append(response, subscriptionResponse{
			ID: string(subscription.ID), Label: subscription.Label,
			CreatedAt: subscription.CreatedAt, LastUsedAt: subscription.LastUsedAt,
		})
	}
	httpx.JSON(w, status, map[string]any{"subscriptions": response})
}

func writeNotificationRefusal(w http.ResponseWriter, err error) {
	var refusal notify.Refusal
	if errors.As(err, &refusal) {
		status := http.StatusBadRequest
		if refusal.Code == notify.RefusalNotAvailable {
			status = http.StatusForbidden
		}
		httpx.JSON(w, status, map[string]any{
			"error": map[string]any{"code": refusal.Code, "message": refusal.Message}})
		return
	}
	if errors.Is(err, notify.ErrNotFound) {
		writeNotificationError(w, http.StatusNotFound, "not_found", "No such record.")
		return
	}
	writeNotificationError(w, http.StatusInternalServerError, "notifications_unavailable",
		"The notification request failed.")
}

func writeNotificationError(w http.ResponseWriter, status int, code, message string) {
	httpx.JSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message}})
}
