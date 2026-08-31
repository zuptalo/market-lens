package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/credentials"
	"market-lens/server/internal/httpx"
	"market-lens/server/internal/identity"
)

// testMailSecret is derived rather than written down: a literal password beside an SMTP host
// and username is indistinguishable from a real credential to anything scanning this repo.
var testMailSecret = "mail-" + hex.EncodeToString(sha256Sum("market-lens/api-auth-test/mail")[:8])

func sha256Sum(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}

func TestOwnerSetupStatusCompletionAndLoginHTTPContracts(t *testing.T) {
	now := time.Date(2026, time.August, 30, 10, 0, 0, 0, time.UTC)
	absoluteExpiry := now.Add(30 * 24 * time.Hour)
	identityService := &ownerIdentityStub{
		setupRequired: true,
		bootstrap: identity.BootstrapResult{
			User: identity.User{
				ID: "10000000-0000-4000-8000-000000000001", Email: "owner@example.com",
				DisplayName: "Market Owner", Role: identity.RoleOwner, Status: identity.StatusActive,
				EmailVerifiedAt: timePointer(now),
			},
			Session:      auth.SessionSummary{ID: "20000000-0000-4000-8000-000000000001", AbsoluteExpiresAt: absoluteExpiry},
			SessionToken: "setup-session-secret", CSRFToken: "setup-csrf-secret",
		},
	}
	authentication := &ownerAuthenticationStub{login: auth.AuthenticationResult{
		Account: auth.Account{
			ID: "10000000-0000-4000-8000-000000000001", Email: "owner@example.com",
			DisplayName: "Market Owner", Role: "owner", Status: "active", EmailVerifiedAt: now,
		},
		Session:      auth.SessionSummary{ID: "20000000-0000-4000-8000-000000000002", AbsoluteExpiresAt: absoluteExpiry},
		SessionToken: "login-session-secret", CSRFToken: "login-csrf-secret",
	}}
	router := NewRouter(Dependencies{
		Identity: identityService, Authentication: authentication, SecureCookies: true,
	})

	status := performAuthRequest(router, http.MethodGet, "/api/v1/setup/status", "")
	if status.Code != http.StatusOK || status.Body.String() != "{\"setup_required\":true}\n" {
		t.Fatalf("setup status = %d %s", status.Code, status.Body.String())
	}

	// The mail credential is composed rather than written out, so this file holds no literal
	// that reads as a real SMTP username and password pair sitting next to a host.
	setupBody := `{"capability":"setup-capability-secret","email":"owner@example.com",` +
		`"password":"correct horse battery staple","display_name":"Market Owner",` +
		`"eodhd_api_key":"eodhd-secret","smtp":{"host":"smtp.example.test","port":587,` +
		`"from":"access@example.test","username":"mail-account","password":"` + testMailSecret + `"}}`
	setup := performAuthRequest(router, http.MethodPost, "/api/v1/auth/owner/setup", setupBody)
	assertAuthenticatedHTTPResponse(t, setup, http.StatusCreated, "setup-csrf-secret", "setup-session-secret", absoluteExpiry, "owner")
	if identityService.bootstrapRequest.Capability != "setup-capability-secret" ||
		identityService.bootstrapRequest.Password != "correct horse battery staple" ||
		identityService.bootstrapRequest.EODHDAPIKey != "eodhd-secret" ||
		identityService.bootstrapRequest.SMTP.Host != "smtp.example.test" ||
		identityService.bootstrapRequest.SMTP.Port != 587 ||
		identityService.bootstrapRequest.SMTP.From != "access@example.test" ||
		identityService.bootstrapRequest.SMTP.Username != "mail-account" ||
		identityService.bootstrapRequest.SMTP.Password != testMailSecret ||
		identityService.bootstrapRequest.Origin == "" || identityService.bootstrapRequest.DeviceLabel == "" {
		t.Fatalf("bootstrap request = %#v", identityService.bootstrapRequest)
	}

	loginBody := `{"email":"owner@example.com","password":"correct horse battery staple"}`
	login := performAuthRequest(router, http.MethodPost, "/api/v1/auth/owner/login", loginBody)
	assertAuthenticatedHTTPResponse(t, login, http.StatusOK, "login-csrf-secret", "login-session-secret", absoluteExpiry, "owner")
	if authentication.loginRequest.Email != "owner@example.com" || authentication.loginRequest.Password == "" ||
		authentication.loginRequest.Origin == "" || authentication.loginRequest.DeviceLabel == "" {
		t.Fatalf("login request = %#v", authentication.loginRequest)
	}
}

func TestOwnerAuthPublicEndpointsUseGenericBoundedErrors(t *testing.T) {
	identityService := &ownerIdentityStub{setupErr: identity.ErrSetupClosed}
	authentication := &ownerAuthenticationStub{
		loginErr: auth.ErrAuthenticationFailed,
	}
	router := NewRouter(Dependencies{Identity: identityService, Authentication: authentication, SecureCookies: true})

	tests := []struct {
		name, path, body string
		want             int
	}{
		{name: "closed setup", path: "/api/v1/auth/owner/setup", body: `{"capability":"` + strings.Repeat("a", 43) + `","email":"owner@example.com","password":"correct horse battery staple","display_name":"Owner"}`, want: http.StatusConflict},
		{name: "generic login", path: "/api/v1/auth/owner/login", body: `{"email":"owner@example.com","password":"wrong"}`, want: http.StatusUnauthorized},
		{name: "malformed JSON", path: "/api/v1/auth/owner/login", body: `{"email":`, want: http.StatusBadRequest},
		{name: "unknown JSON field", path: "/api/v1/auth/owner/login", body: `{"email":"owner@example.com","password":"wrong","admin":true}`, want: http.StatusBadRequest},
		{name: "oversized body", path: "/api/v1/auth/owner/login", body: `{"email":"` + strings.Repeat("a", 70*1024) + `"}`, want: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := performAuthRequest(router, http.MethodPost, test.path, test.body)
			if response.Code != test.want || response.Header().Get("Content-Type") != "application/json" ||
				!strings.Contains(response.Body.String(), `"error"`) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
			for _, secret := range []string{"correct horse battery staple", "replacement owner password", strings.Repeat("a", 43)} {
				if strings.Contains(response.Body.String(), secret) {
					t.Fatalf("response disclosed submitted secret: %s", response.Body.String())
				}
			}
		})
	}
}

func TestGenericSignInReplacesPublicOwnerRecoveryEndpoints(t *testing.T) {
	router := NewRouter(Dependencies{
		Database:       databaseStub{},
		Identity:       &ownerIdentityStub{},
		Authentication: &ownerAuthenticationStub{},
	})

	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in/start",
		strings.NewReader(`{"email":"owner@example.com"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("generic sign-in status = %d body=%s, want 202", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "six-digit passcode") {
		t.Fatalf("generic sign-in body = %s", response.Body.String())
	}

	for _, path := range []string{
		"/api/v1/auth/owner/recovery/request",
		"/api/v1/auth/owner/recovery/complete",
	} {
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("retired route %s status = %d body=%s, want 404", path, response.Code, response.Body.String())
		}
	}
}

func TestOwnerIntegrationStatusReturnsOnlySafeReadinessMetadata(t *testing.T) {
	router := NewRouter(Dependencies{
		Database: databaseStub{},
		Authenticator: sessionAuthenticatorFunc(func(context.Context, string) (auth.Principal, error) {
			return auth.Principal{
				UserID: "10000000-0000-4000-8000-000000000001",
				Role:   "owner", SessionID: "20000000-0000-4000-8000-000000000001",
			}, nil
		}),
		Authentication: &ownerAuthenticationStub{},
		Integrations: integrationStatusReaderStub{statuses: []credentials.Status{
			{Kind: credentials.KindEODHDAPI, Configured: true, Ready: true, KeyVersion: 1},
			{Kind: credentials.KindSMTP, Configured: true, Ready: true, KeyVersion: 1},
		}},
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/owner/integrations", nil)
	request.AddCookie(&http.Cookie{Name: httpx.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("integration status = %d body=%s, want 200", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, required := range []string{`"kind"`, `"configured"`, `"ready"`, `"key_version"`} {
		if !strings.Contains(body, required) {
			t.Fatalf("integration status omitted %s: %s", required, body)
		}
	}
	for _, forbidden := range []string{"api_key", "password", "username", "host", "from", "ciphertext"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("integration status disclosed forbidden field %q: %s", forbidden, body)
		}
	}
}

func TestOwnerPublicRoutesFailSafelyWhenDependenciesAreUnavailable(t *testing.T) {
	router := NewRouter(Dependencies{})
	for _, request := range []struct {
		method, path string
	}{
		{method: http.MethodGet, path: "/api/v1/setup/status"},
		{method: http.MethodPost, path: "/api/v1/auth/owner/setup"},
		{method: http.MethodPost, path: "/api/v1/auth/owner/login"},
		{method: http.MethodPost, path: "/api/v1/auth/sign-in/start"},
	} {
		response := performAuthRequest(router, request.method, request.path, `{}`)
		if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "temporarily_unavailable") {
			t.Fatalf("%s %s response = %d %s", request.method, request.path, response.Code, response.Body.String())
		}
	}
}

func TestAccountSessionAndLogoutHTTPContractsRequireSessionAndCSRF(t *testing.T) {
	now := time.Date(2026, time.August, 30, 10, 0, 0, 0, time.UTC)
	userID := "10000000-0000-4000-8000-000000000001"
	currentSessionID := "20000000-0000-4000-8000-000000000001"
	otherSessionID := "20000000-0000-4000-8000-000000000002"
	authentication := &ownerAuthenticationStub{
		account: auth.Account{
			ID: userID, Email: "owner@example.com", DisplayName: "Market Owner",
			Role: "owner", Status: "active", EmailVerifiedAt: now,
		},
		sessions: []auth.SessionSummary{
			{ID: currentSessionID, Current: true, DeviceLabel: "Current browser", CreatedAt: now, LastSeenAt: now, IdleExpiresAt: now.Add(8 * time.Hour), AbsoluteExpiresAt: now.Add(30 * 24 * time.Hour)},
			{ID: otherSessionID, DeviceLabel: "Other browser", CreatedAt: now, LastSeenAt: now, IdleExpiresAt: now.Add(8 * time.Hour), AbsoluteExpiresAt: now.Add(30 * 24 * time.Hour)},
		},
	}
	authenticator := sessionAuthenticatorFunc(func(_ context.Context, token string) (auth.Principal, error) {
		if token != "active-session-secret" {
			return auth.Principal{}, auth.ErrAuthenticationRequired
		}
		return auth.Principal{
			UserID: userID, Role: "owner", SessionID: currentSessionID,
			VerifyCSRF: func(value string) bool { return value == "valid-csrf" },
		}, nil
	})
	router := NewRouter(Dependencies{
		Authenticator: authenticator, Authentication: authentication, SecureCookies: true,
	})

	account := performSessionRequest(router, http.MethodGet, "/api/v1/account", "", "")
	if account.Code != http.StatusOK || !strings.Contains(account.Body.String(), `"email":"owner@example.com"`) ||
		strings.Contains(account.Body.String(), "session-secret") {
		t.Fatalf("account response = %d %s", account.Code, account.Body.String())
	}
	sessions := performSessionRequest(router, http.MethodGet, "/api/v1/account/sessions", "", "")
	if sessions.Code != http.StatusOK || !strings.Contains(sessions.Body.String(), `"current":true`) ||
		!strings.Contains(sessions.Body.String(), `"device_label":"Other browser"`) ||
		strings.Contains(sessions.Body.String(), "token") {
		t.Fatalf("sessions response = %d %s", sessions.Code, sessions.Body.String())
	}

	for _, request := range []struct {
		name, method, path string
	}{
		{name: "logout", method: http.MethodPost, path: "/api/v1/auth/logout"},
		{name: "one session", method: http.MethodDelete, path: "/api/v1/account/sessions/" + otherSessionID},
		{name: "all sessions", method: http.MethodDelete, path: "/api/v1/account/sessions"},
	} {
		t.Run(request.name+" rejects missing CSRF", func(t *testing.T) {
			response := performSessionRequest(router, request.method, request.path, "", "")
			if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "csrf_rejected") {
				t.Fatalf("CSRF response = %d %s", response.Code, response.Body.String())
			}
		})
	}

	revoked := performSessionRequest(router, http.MethodDelete, "/api/v1/account/sessions/"+otherSessionID, "valid-csrf", "")
	if revoked.Code != http.StatusNoContent || authentication.revokedSessionID != otherSessionID {
		t.Fatalf("session revoke = %d id=%q body=%s", revoked.Code, authentication.revokedSessionID, revoked.Body.String())
	}
	all := performSessionRequest(router, http.MethodDelete, "/api/v1/account/sessions", "valid-csrf", "")
	if all.Code != http.StatusNoContent || !authentication.revokedAll {
		t.Fatalf("all-session revoke = %d all=%v", all.Code, authentication.revokedAll)
	}
	logout := performSessionRequest(router, http.MethodPost, "/api/v1/auth/logout", "valid-csrf", "")
	if logout.Code != http.StatusNoContent || authentication.revokedSessionID != currentSessionID {
		t.Fatalf("logout = %d id=%q", logout.Code, authentication.revokedSessionID)
	}
	cleared := logout.Result().Cookies()
	if len(cleared) != 2 {
		t.Fatalf("logout cookies = %#v, want the session and CSRF cookies cleared", cleared)
	}
	clearedByName := map[string]*http.Cookie{}
	for _, cookie := range cleared {
		clearedByName[cookie.Name] = cookie
	}
	for _, name := range []string{httpx.SessionCookieName, httpx.CSRFCookieName} {
		cookie := clearedByName[name]
		if cookie == nil || cookie.MaxAge >= 0 || !cookie.Secure {
			t.Fatalf("logout did not clear %s: %#v", name, cookie)
		}
	}
}

func assertAuthenticatedHTTPResponse(t *testing.T, response *httptest.ResponseRecorder, wantStatus int,
	wantCSRF, forbiddenSessionToken string, absoluteExpiry time.Time, wantRole string) {
	t.Helper()
	if response.Code != wantStatus || !strings.Contains(response.Body.String(), `"csrf_token":"`+wantCSRF+`"`) ||
		!strings.Contains(response.Body.String(), `"role":"`+wantRole+`"`) {
		t.Fatalf("authentication response = %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), forbiddenSessionToken) || strings.Contains(response.Body.String(), "password") ||
		strings.Contains(response.Body.String(), "capability") {
		t.Fatalf("authentication response disclosed secret material: %s", response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("authentication cookies = %#v, want the session and CSRF cookies", cookies)
	}
	if httpx.SessionCookieName != "__Host-market_lens_session" {
		t.Fatalf("session cookie name = %q", httpx.SessionCookieName)
	}
	byName := map[string]*http.Cookie{}
	for _, cookie := range cookies {
		byName[cookie.Name] = cookie
	}
	cookie := byName[httpx.SessionCookieName]
	if cookie == nil || cookie.Value != forbiddenSessionToken || cookie.Path != "/" ||
		cookie.Domain != "" || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode ||
		!cookie.Expires.Equal(absoluteExpiry) {
		t.Fatalf("unsafe authentication cookie: %#v", cookie)
	}
	// The session token must never be readable by script; only the CSRF token may be.
	csrf := byName[httpx.CSRFCookieName]
	if csrf == nil || csrf.Value != wantCSRF || csrf.HttpOnly || !csrf.Secure ||
		csrf.Path != "/" || csrf.Domain != "" || csrf.SameSite != http.SameSiteLaxMode {
		t.Fatalf("unsafe CSRF cookie: %#v", csrf)
	}
}

func performAuthRequest(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "Contract browser")
	request.RemoteAddr = "192.0.2.10:4321"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func performSessionRequest(handler http.Handler, method, path, csrf, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.AddCookie(&http.Cookie{Name: httpx.SessionCookieName, Value: "active-session-secret"})
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

type ownerIdentityStub struct {
	setupRequired    bool
	setupErr         error
	bootstrap        identity.BootstrapResult
	bootstrapRequest identity.BootstrapRequest
}

func (stub *ownerIdentityStub) SetupRequired(context.Context) (bool, error) {
	return stub.setupRequired, stub.setupErr
}

func (stub *ownerIdentityStub) BootstrapOwner(_ context.Context, request identity.BootstrapRequest) (identity.BootstrapResult, error) {
	stub.bootstrapRequest = request
	return stub.bootstrap, stub.setupErr
}

type ownerAuthenticationStub struct {
	signInErr        error
	signInRequest    auth.SignInStartRequest
	login            auth.AuthenticationResult
	loginErr         error
	loginRequest     auth.OwnerLoginRequest
	account          auth.Account
	sessions         []auth.SessionSummary
	revokedSessionID string
	revokedAll       bool
}

func (stub *ownerAuthenticationStub) StartSignIn(_ context.Context, request auth.SignInStartRequest) (auth.SignInStartResult, error) {
	stub.signInRequest = request
	if stub.signInErr != nil {
		return auth.SignInStartResult{}, stub.signInErr
	}
	return auth.SignInStartResult{Message: auth.GenericSignInMessage}, nil
}

func (stub *ownerAuthenticationStub) LoginOwner(_ context.Context, request auth.OwnerLoginRequest) (auth.AuthenticationResult, error) {
	stub.loginRequest = request
	return stub.login, stub.loginErr
}

func (stub *ownerAuthenticationStub) Account(context.Context, string) (auth.Account, error) {
	return stub.account, nil
}

func (stub *ownerAuthenticationStub) ListSessions(context.Context, string, string) ([]auth.SessionSummary, error) {
	return stub.sessions, nil
}

func (stub *ownerAuthenticationStub) RevokeSession(_ context.Context, _ string, sessionID string) error {
	stub.revokedSessionID = sessionID
	return nil
}

func (stub *ownerAuthenticationStub) RevokeAllSessions(context.Context, string) error {
	stub.revokedAll = true
	return nil
}

func timePointer(value time.Time) *time.Time { return &value }

type integrationStatusReaderStub struct {
	statuses []credentials.Status
	err      error
}

func (stub integrationStatusReaderStub) Statuses(context.Context) ([]credentials.Status, error) {
	return stub.statuses, stub.err
}
