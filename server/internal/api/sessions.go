package api

import (
	"errors"
	"net/http"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/httpx"
)

func accountHandler(service OwnerAuthentication) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		principal, ok := httpx.PrincipalFromContext(request)
		if !ok || service == nil {
			writeAuthenticationError(writer, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
			return
		}
		account, err := service.Account(request.Context(), principal.UserID)
		if err != nil {
			writeAuthenticationError(writer, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
			return
		}
		verifiedAt := account.EmailVerifiedAt
		httpx.JSON(writer, http.StatusOK, accountResponse{
			ID: account.ID, Email: account.Email, DisplayName: account.DisplayName,
			Role: account.Role, Status: account.Status, EmailVerifiedAt: &verifiedAt,
		})
	}
}

func listSessionsHandler(service OwnerAuthentication) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		principal, ok := httpx.PrincipalFromContext(request)
		if !ok || service == nil {
			writeAuthenticationError(writer, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
			return
		}
		sessions, err := service.ListSessions(request.Context(), principal.UserID, principal.SessionID)
		if err != nil {
			writeAuthenticationError(writer, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
			return
		}
		items := make([]sessionResponse, 0, len(sessions))
		for _, session := range sessions {
			items = append(items, newSessionResponse(session))
		}
		httpx.JSON(writer, http.StatusOK, map[string]any{"items": items})
	}
}

func revokeSessionHandler(service OwnerAuthentication) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		principal, ok := httpx.PrincipalFromContext(request)
		if !ok || service == nil {
			writeAuthenticationError(writer, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
			return
		}
		if err := service.RevokeSession(request.Context(), principal.UserID, request.PathValue("sessionId")); err != nil {
			if errors.Is(err, auth.ErrAuthenticationRequired) {
				writeAuthenticationError(writer, http.StatusNotFound, "not_found", "The session was not found.")
			} else {
				writeAuthenticationError(writer, http.StatusServiceUnavailable, "temporarily_unavailable", "Authentication is temporarily unavailable.")
			}
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}
}

func revokeAllSessionsHandler(service OwnerAuthentication) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		principal, ok := httpx.PrincipalFromContext(request)
		if !ok || service == nil {
			writeAuthenticationError(writer, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
			return
		}
		if err := service.RevokeAllSessions(request.Context(), principal.UserID); err != nil {
			writeAuthenticationError(writer, http.StatusServiceUnavailable, "temporarily_unavailable", "Authentication is temporarily unavailable.")
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}
}

func logoutHandler(service OwnerAuthentication, secureCookies bool) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		principal, ok := httpx.PrincipalFromContext(request)
		if !ok || service == nil {
			writeAuthenticationError(writer, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
			return
		}
		if err := service.RevokeSession(request.Context(), principal.UserID, principal.SessionID); err != nil &&
			!errors.Is(err, auth.ErrAuthenticationRequired) {
			writeAuthenticationError(writer, http.StatusServiceUnavailable, "temporarily_unavailable", "Authentication is temporarily unavailable.")
			return
		}
		httpx.ClearSessionCookie(writer, secureCookies)
		httpx.ClearCSRFCookie(writer, secureCookies)
		writer.WriteHeader(http.StatusNoContent)
	}
}

type sessionResponse struct {
	ID                string    `json:"id"`
	Current           bool      `json:"current"`
	DeviceLabel       string    `json:"device_label"`
	CreatedAt         time.Time `json:"created_at"`
	LastSeenAt        time.Time `json:"last_seen_at"`
	IdleExpiresAt     time.Time `json:"idle_expires_at"`
	AbsoluteExpiresAt time.Time `json:"absolute_expires_at"`
	Revoked           bool      `json:"revoked"`
}

func newSessionResponse(session auth.SessionSummary) sessionResponse {
	return sessionResponse{
		ID: session.ID, Current: session.Current, DeviceLabel: session.DeviceLabel,
		CreatedAt: session.CreatedAt, LastSeenAt: session.LastSeenAt, IdleExpiresAt: session.IdleExpiresAt,
		AbsoluteExpiresAt: session.AbsoluteExpiresAt, Revoked: session.Revoked,
	}
}
