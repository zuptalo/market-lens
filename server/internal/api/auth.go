package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
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
			var validation *identity.SetupValidationError
			if errors.As(err, &validation) {
				writeSetupFieldErrors(writer, validation, nil)
				return
			}
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

func ownerIntegrationStatusHandler(reader IntegrationStatusReader, admin IntegrationAdministration,
	configuration InstanceConfiguration) http.HandlerFunc {
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
		body := map[string]any{
			"integrations":  response,
			"configuration": describeInstanceConfiguration(configuration, len(statuses) > 0),
		}
		// The editable, non-secret view. Without it an owner cannot see what their
		// installation points at, and so cannot correct it.
		if admin != nil {
			settings, err := admin.IntegrationSettings(request.Context(),
				identity.Actor{UserID: principal.UserID, Role: identity.RoleOwner})
			switch {
			case err == nil:
				body["settings"] = describeIntegrationSettings(settings)
			default:
				// Swallowing this silently would leave the owner looking at an empty form with
				// no way to find out why, so it is at least recorded for whoever runs the host.
				slog.Warn("owner integration settings could not be read", "error", err.Error())
			}
		}
		httpx.JSON(writer, http.StatusOK, body)
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

// writeSetupFieldErrors reports every field the operator must fix in one response. An
// unreachable dependency is 503 with its own code, because "we could not check this" and
// "this is wrong" need opposite responses from the person setting the installation up.
func writeSetupFieldErrors(writer http.ResponseWriter, validation *identity.SetupValidationError,
	results map[string]string) {
	status, code := http.StatusBadRequest, "invalid_setup"
	summary := "Some of the details you entered need attention."
	if validation.Unreachable {
		status, code = http.StatusServiceUnavailable, "dependency_unreachable"
		summary = "Setup could not reach a service it needs to check."
	}
	fields := make([]map[string]string, 0, len(validation.Fields))
	for _, field := range validation.Fields {
		fields = append(fields, map[string]string{
			"field": field.Field, "code": field.Code, "message": field.Message,
		})
	}
	body := map[string]any{"code": code, "message": summary, "fields": fields}
	// The per-integration result matters most when something failed: that is exactly when the
	// owner needs to know which half of the configuration is at fault.
	if results != nil {
		body["results"] = results
	}
	httpx.JSON(writer, status, map[string]any{"error": body})
}

func writeAuthenticationError(writer http.ResponseWriter, status int, code, message string) {
	httpx.JSON(writer, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

// describeInstanceConfiguration answers the only question an owner can get wrong now: which
// values must travel with a database backup. EXTERNAL_CREDENTIAL_KEY is required as soon as
// anything is encrypted under it, because it is deliberately not stored in the database it
// protects; the signing key is, so it needs no retention at all.
func describeInstanceConfiguration(configuration InstanceConfiguration, credentialsStored bool) map[string]any {
	mustRetain := make([]string, 0, 2)
	if configuration.SigningKeySource == "supplied" {
		mustRetain = append(mustRetain, "AUTH_SECRET")
	}
	if configuration.ExternalKeyConfigured || credentialsStored {
		mustRetain = append(mustRetain, "EXTERNAL_CREDENTIAL_KEY")
	}
	return map[string]any{
		"signing_key": map[string]any{
			"source":     configuration.SigningKeySource,
			"generation": configuration.SigningKeyGeneration,
		},
		"external_credential_key": map[string]any{
			"source":     "environment",
			"configured": configuration.ExternalKeyConfigured,
			"required":   credentialsStored,
		},
		"operator_must_retain": mustRetain,
	}
}

// integrationUpdateBody is the wire shape for checking or changing integrations. Every
// integration is optional so each can be changed independently, and the SMTP password is a
// pointer so an omitted password ("keep the stored one") stays distinguishable from an
// explicitly empty one ("remove authentication").
type integrationUpdateBody struct {
	SMTP *struct {
		Host     string  `json:"host"`
		Port     int     `json:"port"`
		From     string  `json:"from"`
		Username string  `json:"username"`
		Password *string `json:"password"`
	} `json:"smtp"`
	EODHD *struct {
		APIKey string `json:"api_key"`
	} `json:"eodhd"`
}

func (body integrationUpdateBody) toUpdate() identity.IntegrationUpdate {
	update := identity.IntegrationUpdate{}
	if body.SMTP != nil {
		update.SMTP = &identity.SMTPUpdate{
			Host: body.SMTP.Host, Port: body.SMTP.Port, From: body.SMTP.From,
			Username: body.SMTP.Username, Password: body.SMTP.Password,
		}
	}
	if body.EODHD != nil {
		update.EODHD = &identity.EODHDUpdate{APIKey: body.EODHD.APIKey}
	}
	return update
}

// ownerIntegrationChangeHandler serves both checking and saving. They differ only in whether
// the verified configuration is written, so sharing the handler keeps the two from drifting
// into different validation.
func ownerIntegrationChangeHandler(admin IntegrationAdministration, save bool) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		principal, ok := httpx.PrincipalFromContext(request)
		if !ok || principal.Role != "owner" {
			writeAuthenticationError(writer, http.StatusForbidden, "authorization_denied", "Owner access is required.")
			return
		}
		if admin == nil {
			writeAuthenticationError(writer, http.StatusServiceUnavailable, "temporarily_unavailable",
				"Integration administration is temporarily unavailable.")
			return
		}
		var body integrationUpdateBody
		if decodeAuthenticationJSON(writer, request, &body) != nil {
			return
		}
		actor := identity.Actor{UserID: principal.UserID, Role: identity.RoleOwner}
		operation := admin.VerifyIntegrations
		if save {
			operation = admin.UpdateIntegrations
		}
		outcomes, err := operation(request.Context(), actor, body.toUpdate())
		if err != nil {
			var validation *identity.SetupValidationError
			if errors.As(err, &validation) {
				writeSetupFieldErrors(writer, validation, describeIntegrationOutcomes(outcomes))
				return
			}
			if errors.Is(err, identity.ErrOwnerRequired) {
				writeAuthenticationError(writer, http.StatusForbidden, "authorization_denied", "Owner access is required.")
				return
			}
			writeAuthenticationError(writer, http.StatusServiceUnavailable, "temporarily_unavailable",
				"Integration administration is temporarily unavailable.")
			return
		}
		httpx.JSON(writer, http.StatusOK, map[string]any{
			"verified": true, "results": describeIntegrationOutcomes(outcomes),
		})
	}
}

// describeIntegrationSettings renders what the owner may edit. The provider key and the mail
// password are write-only and are reported as present, never returned.
func describeIntegrationSettings(settings identity.IntegrationSettings) map[string]any {
	return map[string]any{
		"eodhd": map[string]any{
			"configured":   settings.EODHDConfigured,
			"validated_at": settings.EODHDValidatedAt,
		},
		"smtp": map[string]any{
			"configured":          settings.SMTPConfigured,
			"host":                settings.SMTP.Host,
			"port":                settings.SMTP.Port,
			"from":                settings.SMTP.From,
			"username":            settings.SMTP.Username,
			"password_configured": settings.SMTP.PasswordConfigured,
		},
	}
}

// describeIntegrationOutcomes reports each integration's own result, so the owner can be told
// which half of the configuration works rather than only whether the submission as a whole
// passed. "not_checked" is a real answer: a shape problem stops every network call.
func describeIntegrationOutcomes(outcomes identity.IntegrationOutcomes) map[string]string {
	outcome := func(value identity.IntegrationOutcome) string {
		if value == "" {
			return string(identity.IntegrationNotChecked)
		}
		return string(value)
	}
	return map[string]string{"eodhd": outcome(outcomes.EODHD), "smtp": outcome(outcomes.SMTP)}
}
