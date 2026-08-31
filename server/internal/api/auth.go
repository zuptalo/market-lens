package api

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/credentials"
	"market-lens/server/internal/httpx"
	"market-lens/server/internal/identity"
)

const maxAuthenticationBody = 64 * 1024

func setupStatusHandler(service OwnerIdentity) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if service == nil {
			writeAuthenticationError(writer, http.StatusServiceUnavailable, "temporarily_unavailable", "Authentication is temporarily unavailable.")
			return
		}
		required, err := service.SetupRequired(request.Context())
		if err != nil {
			writeAuthenticationError(writer, http.StatusServiceUnavailable, "temporarily_unavailable", "Authentication is temporarily unavailable.")
			return
		}
		httpx.JSON(writer, http.StatusOK, map[string]bool{"setup_required": required})
	}
}

func completeOwnerSetupHandler(service OwnerIdentity, secureCookies bool) http.HandlerFunc {
	type setupRequest struct {
		Capability  string `json:"capability"`
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
		EODHDAPIKey string `json:"eodhd_api_key"`
		SMTP        struct {
			Host     string `json:"host"`
			Port     int    `json:"port"`
			From     string `json:"from"`
			Username string `json:"username"`
			Password string `json:"password"`
		} `json:"smtp"`
	}
	return func(writer http.ResponseWriter, request *http.Request) {
		var input setupRequest
		if service == nil {
			writeAuthenticationError(writer, http.StatusServiceUnavailable, "temporarily_unavailable", "Authentication is temporarily unavailable.")
			return
		}
		if decodeAuthenticationJSON(writer, request, &input) != nil {
			return
		}
		device, origin := authenticationClientMetadata(request)
		result, err := service.BootstrapOwner(request.Context(), identity.BootstrapRequest{
			Capability: input.Capability, Email: input.Email, Password: input.Password, DisplayName: input.DisplayName,
			DeviceLabel: device, Origin: origin,
			EODHDAPIKey: input.EODHDAPIKey,
			SMTP: identity.SMTPSetupConfiguration{
				Host: input.SMTP.Host, Port: input.SMTP.Port, From: input.SMTP.From,
				Username: input.SMTP.Username, Password: input.SMTP.Password,
			},
		})
		if err != nil {
			switch {
			case errors.Is(err, identity.ErrSetupClosed):
				writeAuthenticationError(writer, http.StatusConflict, "setup_closed", "Owner setup is unavailable.")
			case errors.Is(err, identity.ErrCapabilityUnavailable):
				writeAuthenticationError(writer, http.StatusBadRequest, "invalid_capability", "The setup request is invalid or unavailable.")
			case errors.Is(err, identity.ErrProviderUnavailable):
				writeAuthenticationError(writer, http.StatusServiceUnavailable, "provider_unavailable", "Provider validation is temporarily unavailable.")
			case errors.Is(err, identity.ErrProviderCredentialInvalid):
				writeAuthenticationError(writer, http.StatusBadRequest, "invalid_request", "The provider credential is invalid or unavailable.")
			default:
				writeAuthenticationError(writer, http.StatusBadRequest, "invalid_request", "The request is invalid.")
			}
			return
		}
		writeAuthenticated(writer, http.StatusCreated, result.SessionToken, result.CSRFToken,
			result.Session.AbsoluteExpiresAt, secureCookies, accountResponse{
				ID: result.User.ID, Email: result.User.Email, DisplayName: result.User.DisplayName,
				Role: string(result.User.Role), Status: string(result.User.Status), EmailVerifiedAt: result.User.EmailVerifiedAt,
			})
	}
}

func ownerLoginHandler(service OwnerAuthentication, secureCookies bool) http.HandlerFunc {
	type loginRequest struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	return func(writer http.ResponseWriter, request *http.Request) {
		var input loginRequest
		if service == nil {
			writeAuthenticationError(writer, http.StatusServiceUnavailable, "temporarily_unavailable", "Authentication is temporarily unavailable.")
			return
		}
		if decodeAuthenticationJSON(writer, request, &input) != nil {
			return
		}
		device, origin := authenticationClientMetadata(request)
		result, err := service.LoginOwner(request.Context(), auth.OwnerLoginRequest{
			Email: input.Email, Password: input.Password, DeviceLabel: device, Origin: origin,
		})
		if err != nil {
			if errors.Is(err, auth.ErrAuthenticationFailed) {
				writeAuthenticationError(writer, http.StatusUnauthorized, "authentication_failed", "Authentication failed.")
			} else {
				writeAuthenticationError(writer, http.StatusServiceUnavailable, "temporarily_unavailable", "Authentication is temporarily unavailable.")
			}
			return
		}
		verifiedAt := result.Account.EmailVerifiedAt
		writeAuthenticated(writer, http.StatusOK, result.SessionToken, result.CSRFToken,
			result.Session.AbsoluteExpiresAt, secureCookies, accountResponse{
				ID: result.Account.ID, Email: result.Account.Email, DisplayName: result.Account.DisplayName,
				Role: result.Account.Role, Status: result.Account.Status, EmailVerifiedAt: &verifiedAt,
			})
	}
}

func signInStartHandler(service OwnerAuthentication) http.HandlerFunc {
	type signInRequest struct {
		Email string `json:"email"`
	}
	return func(writer http.ResponseWriter, request *http.Request) {
		var input signInRequest
		if service == nil {
			writeAuthenticationError(writer, http.StatusServiceUnavailable, "temporarily_unavailable", "Authentication is temporarily unavailable.")
			return
		}
		if decodeAuthenticationJSON(writer, request, &input) != nil {
			return
		}
		_, origin := authenticationClientMetadata(request)
		result, err := service.StartSignIn(request.Context(), auth.SignInStartRequest{Email: input.Email, Origin: origin})
		if err != nil {
			var limited *auth.RateLimitedError
			if errors.As(err, &limited) {
				writeRateLimited(writer, limited.RetryAfter)
				return
			}
			writeAuthenticationError(writer, http.StatusServiceUnavailable, "temporarily_unavailable", "Authentication is temporarily unavailable.")
			return
		}
		httpx.JSON(writer, http.StatusAccepted, result)
	}
}

func ownerIntegrationStatusHandler(reader IntegrationStatusReader) http.HandlerFunc {
	type statusResponse struct {
		Kind       credentials.Kind `json:"kind"`
		Configured bool             `json:"configured"`
		Ready      bool             `json:"ready"`
		KeyVersion uint32           `json:"key_version"`
	}
	return func(writer http.ResponseWriter, request *http.Request) {
		principal, ok := httpx.PrincipalFromContext(request)
		if !ok || principal.Role != "owner" {
			writeAuthenticationError(writer, http.StatusForbidden, "authorization_denied", "Owner access is required.")
			return
		}
		if reader == nil {
			writeAuthenticationError(writer, http.StatusServiceUnavailable, "temporarily_unavailable", "Integration status is temporarily unavailable.")
			return
		}
		statuses, err := reader.Statuses(request.Context())
		if err != nil {
			writeAuthenticationError(writer, http.StatusServiceUnavailable, "temporarily_unavailable", "Integration status is temporarily unavailable.")
			return
		}
		response := make([]statusResponse, 0, len(statuses))
		for _, status := range statuses {
			response = append(response, statusResponse{
				Kind: status.Kind, Configured: status.Configured, Ready: status.Ready, KeyVersion: status.KeyVersion,
			})
		}
		httpx.JSON(writer, http.StatusOK, map[string]any{"integrations": response})
	}
}

type accountResponse struct {
	ID              string     `json:"id"`
	Email           string     `json:"email"`
	DisplayName     string     `json:"display_name"`
	Role            string     `json:"role"`
	Status          string     `json:"status"`
	EmailVerifiedAt *time.Time `json:"email_verified_at"`
}

func writeAuthenticated(writer http.ResponseWriter, status int, sessionToken, csrfToken string,
	absoluteExpiry time.Time, secureCookies bool, account accountResponse) {
	httpx.SetSessionCookie(writer, sessionToken, secureCookies, absoluteExpiry)
	httpx.SetCSRFCookie(writer, csrfToken, secureCookies, absoluteExpiry)
	httpx.JSON(writer, status, map[string]any{"account": account, "csrf_token": csrfToken})
}

func decodeAuthenticationJSON(writer http.ResponseWriter, request *http.Request, target any) error {
	limited := http.MaxBytesReader(writer, request.Body, maxAuthenticationBody)
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeAuthenticationError(writer, http.StatusBadRequest, "invalid_request", "The request is invalid.")
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeAuthenticationError(writer, http.StatusBadRequest, "invalid_request", "The request is invalid.")
		return errors.New("authentication request contains trailing data")
	}
	return nil
}

func authenticationClientMetadata(request *http.Request) (device, origin string) {
	device = strings.TrimSpace(request.UserAgent())
	if device == "" {
		device = "Unknown device"
	}
	if len(device) > 160 {
		device = device[:160]
	}
	origin = request.RemoteAddr
	if host, _, err := net.SplitHostPort(request.RemoteAddr); err == nil {
		origin = host
	}
	if origin == "" {
		origin = "unknown"
	}
	if len(origin) > 256 {
		origin = origin[:256]
	}
	return device, origin
}

func writeAuthenticationError(writer http.ResponseWriter, status int, code, message string) {
	httpx.JSON(writer, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
