package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

// FR-009 on request. The owner is told which values this installation provisioned for itself
// and which they must retain to restore it, and the response contains no secret at all.
func TestOwnerIntegrationStatusReportsInstanceConfigurationWithoutSecrets(t *testing.T) {
	tests := []struct {
		name              string
		configuration     InstanceConfiguration
		statuses          []credentials.Status
		wantSource        string
		wantRequired      bool
		wantMustRetain    []string
		wantNotMustRetain []string
	}{
		{
			name: "self-provisioned installation retains nothing",
			configuration: InstanceConfiguration{
				SigningKeySource: "provisioned", SigningKeyGeneration: 1,
			},
			wantSource:        "provisioned",
			wantRequired:      false,
			wantNotMustRetain: []string{"AUTH_SECRET", "EXTERNAL_CREDENTIAL_KEY"},
		},
		{
			name: "stored credentials make the credential key required",
			configuration: InstanceConfiguration{
				SigningKeySource: "provisioned", SigningKeyGeneration: 3, ExternalKeyConfigured: true,
			},
			statuses: []credentials.Status{
				{Kind: credentials.KindEODHDAPI, Configured: true, Ready: true, KeyVersion: 1},
				{Kind: credentials.KindSMTP, Configured: true, Ready: true, KeyVersion: 1},
			},
			wantSource:        "provisioned",
			wantRequired:      true,
			wantMustRetain:    []string{"EXTERNAL_CREDENTIAL_KEY"},
			wantNotMustRetain: []string{"AUTH_SECRET"},
		},
		{
			name: "an existing deployment still retains both",
			configuration: InstanceConfiguration{
				SigningKeySource: "supplied", SigningKeyGeneration: 1, ExternalKeyConfigured: true,
			},
			statuses: []credentials.Status{
				{Kind: credentials.KindEODHDAPI, Configured: true, Ready: true, KeyVersion: 1},
				{Kind: credentials.KindSMTP, Configured: true, Ready: true, KeyVersion: 1},
			},
			wantSource:     "supplied",
			wantRequired:   true,
			wantMustRetain: []string{"AUTH_SECRET", "EXTERNAL_CREDENTIAL_KEY"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := NewRouter(Dependencies{
				Database: databaseStub{},
				Authenticator: sessionAuthenticatorFunc(func(context.Context, string) (auth.Principal, error) {
					return auth.Principal{
						UserID: "10000000-0000-4000-8000-000000000001",
						Role:   "owner", SessionID: "20000000-0000-4000-8000-000000000001",
					}, nil
				}),
				Authentication:        &ownerAuthenticationStub{},
				Integrations:          integrationStatusReaderStub{statuses: tt.statuses},
				InstanceConfiguration: tt.configuration,
			})
			request := httptest.NewRequest(http.MethodGet, "/api/v1/owner/integrations", nil)
			request.AddCookie(&http.Cookie{Name: httpx.SessionCookieName, Value: "opaque-session"})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}

			var body struct {
				Integrations  []map[string]any `json:"integrations"`
				Configuration struct {
					SigningKey struct {
						Source     string `json:"source"`
						Generation int    `json:"generation"`
					} `json:"signing_key"`
					ExternalCredentialKey struct {
						Source     string `json:"source"`
						Configured bool   `json:"configured"`
						Required   bool   `json:"required"`
					} `json:"external_credential_key"`
					OperatorMustRetain []string `json:"operator_must_retain"`
				} `json:"configuration"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("response is not the documented shape: %v: %s", err, response.Body.String())
			}
			if body.Integrations == nil {
				t.Fatal("the existing integrations array was dropped")
			}
			if body.Configuration.SigningKey.Source != tt.wantSource {
				t.Errorf("signing key source = %q, want %q", body.Configuration.SigningKey.Source, tt.wantSource)
			}
			if body.Configuration.SigningKey.Generation != tt.configuration.SigningKeyGeneration {
				t.Errorf("generation = %d, want %d",
					body.Configuration.SigningKey.Generation, tt.configuration.SigningKeyGeneration)
			}
			// The credential key is always external. The API must never suggest otherwise.
			if body.Configuration.ExternalCredentialKey.Source != "environment" {
				t.Errorf("external credential key source = %q, want environment",
					body.Configuration.ExternalCredentialKey.Source)
			}
			if body.Configuration.ExternalCredentialKey.Required != tt.wantRequired {
				t.Errorf("credential key required = %t, want %t",
					body.Configuration.ExternalCredentialKey.Required, tt.wantRequired)
			}
			retained := strings.Join(body.Configuration.OperatorMustRetain, ",")
			for _, name := range tt.wantMustRetain {
				if !strings.Contains(retained, name) {
					t.Errorf("operator_must_retain omits %q: %v", name, body.Configuration.OperatorMustRetain)
				}
			}
			for _, name := range tt.wantNotMustRetain {
				if strings.Contains(retained, name) {
					t.Errorf("operator_must_retain wrongly includes %q: %v", name, body.Configuration.OperatorMustRetain)
				}
			}
			// The variable names AUTH_SECRET and EXTERNAL_CREDENTIAL_KEY are meant to appear;
			// what must never appear is anything describing a key's value. Scan with those
			// names removed so the check tests disclosure rather than vocabulary.
			scanned := strings.ToLower(response.Body.String())
			for _, name := range []string{"auth_secret", "external_credential_key"} {
				scanned = strings.ReplaceAll(scanned, name, "")
			}
			for _, forbidden := range []string{
				"key_material", "fingerprint", "secret", "ciphertext", "length", "bytes",
			} {
				if strings.Contains(scanned, forbidden) {
					t.Errorf("configuration disclosed %q: %s", forbidden, response.Body.String())
				}
			}
		})
	}
}

// The configuration object describes the installation's secrets policy, so it is owner-only.
func TestInstanceConfigurationIsNeverServedBelowOwner(t *testing.T) {
	configuration := InstanceConfiguration{SigningKeySource: "provisioned", SigningKeyGeneration: 7}
	for _, principal := range []struct {
		name       string
		role       string
		wantStatus int
	}{
		{name: "member session", role: "member", wantStatus: http.StatusForbidden},
	} {
		t.Run(principal.name, func(t *testing.T) {
			router := NewRouter(Dependencies{
				Database: databaseStub{},
				Authenticator: sessionAuthenticatorFunc(func(context.Context, string) (auth.Principal, error) {
					return auth.Principal{
						UserID: "10000000-0000-4000-8000-000000000002",
						Role:   principal.role, SessionID: "20000000-0000-4000-8000-000000000002",
					}, nil
				}),
				Authentication:        &ownerAuthenticationStub{},
				Integrations:          integrationStatusReaderStub{},
				InstanceConfiguration: configuration,
			})
			request := httptest.NewRequest(http.MethodGet, "/api/v1/owner/integrations", nil)
			request.AddCookie(&http.Cookie{Name: httpx.SessionCookieName, Value: "opaque-session"})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != principal.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, principal.wantStatus)
			}
			if strings.Contains(response.Body.String(), "signing_key") {
				t.Fatalf("configuration leaked to a non-owner: %s", response.Body.String())
			}
		})
	}

	// An anonymous request must not reach it either.
	router := NewRouter(Dependencies{
		Database: databaseStub{},
		Authenticator: sessionAuthenticatorFunc(func(context.Context, string) (auth.Principal, error) {
			return auth.Principal{}, auth.ErrAuthenticationRequired
		}),
		Authentication:        &ownerAuthenticationStub{},
		Integrations:          integrationStatusReaderStub{},
		InstanceConfiguration: configuration,
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/owner/integrations", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, want 401", response.Code)
	}
	if strings.Contains(response.Body.String(), "signing_key") {
		t.Fatalf("configuration leaked to an anonymous request: %s", response.Body.String())
	}
}

// TestOwnerSetupNamesEveryFieldTheOperatorMustFix is feature 010's initial red. Setup used to
// answer "The request is invalid." for a short password, a malformed email, a rejected
// provider key and a wrong SMTP port alike, leaving the person setting up the installation to
// guess which of ten inputs to change.
func TestOwnerSetupNamesEveryFieldTheOperatorMustFix(t *testing.T) {
	submittedPassword := "short-pass"
	submittedKey := "submitted-eodhd-key-value"
	submittedSMTPPassword := "submitted-smtp-secret"

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantFields map[string]string
	}{
		{
			name: "a password below the minimum names the password and states the rule",
			err: &identity.SetupValidationError{Fields: []identity.SetupFieldError{
				{Field: "password", Code: "too_short",
					Message: "Password must be at least 12 characters."},
			}},
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_setup",
			wantFields: map[string]string{"password": "at least 12 characters"},
		},
		{
			name: "several bad values are reported together",
			err: &identity.SetupValidationError{Fields: []identity.SetupFieldError{
				{Field: "email", Code: "invalid_format", Message: "Enter a valid email address."},
				{Field: "password", Code: "too_short", Message: "Password must be at least 12 characters."},
				{Field: "smtp_port", Code: "out_of_range", Message: "SMTP port must be between 1 and 65535."},
			}},
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_setup",
			wantFields: map[string]string{
				"email": "valid email", "password": "at least 12", "smtp_port": "between 1 and 65535",
			},
		},
		{
			name: "a provider-rejected key names the provider field",
			err: &identity.SetupValidationError{Fields: []identity.SetupFieldError{
				{Field: "eodhd_api_key", Code: "rejected",
					Message: "EODHD rejected this API key."},
			}},
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_setup",
			wantFields: map[string]string{"eodhd_api_key": "rejected"},
		},
		{
			name: "an unreachable provider is distinguishable from a rejected key",
			err: &identity.SetupValidationError{Unreachable: true, Fields: []identity.SetupFieldError{
				{Field: "eodhd_api_key", Code: "unreachable",
					Message: "EODHD could not be reached, so the API key was not checked."},
			}},
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "dependency_unreachable",
			wantFields: map[string]string{"eodhd_api_key": "could not be reached"},
		},
		{
			name: "rejected SMTP credentials name the credential fields",
			err: &identity.SetupValidationError{Fields: []identity.SetupFieldError{
				{Field: "smtp_username", Code: "auth_rejected",
					Message: "The mail server rejected these credentials."},
				{Field: "smtp_password", Code: "auth_rejected",
					Message: "The mail server rejected these credentials."},
			}},
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_setup",
			wantFields: map[string]string{
				"smtp_username": "rejected these credentials", "smtp_password": "rejected these credentials",
			},
		},
		{
			name: "an unreachable mail server blocks setup and says so",
			err: &identity.SetupValidationError{Unreachable: true, Fields: []identity.SetupFieldError{
				{Field: "smtp_host", Code: "unreachable",
					Message: "Could not reach the mail server within 10s."},
			}},
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "dependency_unreachable",
			wantFields: map[string]string{"smtp_host": "Could not reach"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := NewRouter(Dependencies{
				Database: databaseStub{},
				Identity: &ownerIdentityStub{setupErr: tt.err},
			})
			body := `{"capability":"c","email":"owner@example.com","password":"` + submittedPassword +
				`","display_name":"Owner","eodhd_api_key":"` + submittedKey +
				`","smtp":{"host":"smtp.example.test","port":587,"from":"a@example.test",` +
				`"username":"mailer","password":"` + submittedSMTPPassword + `"}}`
			request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/owner/setup", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, tt.wantStatus, response.Body.String())
			}
			var decoded struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
					Fields  []struct {
						Field   string `json:"field"`
						Code    string `json:"code"`
						Message string `json:"message"`
					} `json:"fields"`
				} `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
				t.Fatalf("response is not the documented shape: %v: %s", err, response.Body.String())
			}
			if decoded.Error.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", decoded.Error.Code, tt.wantCode)
			}
			if len(decoded.Error.Fields) != len(tt.wantFields) {
				t.Fatalf("reported %d fields, want %d: %s",
					len(decoded.Error.Fields), len(tt.wantFields), response.Body.String())
			}
			seen := map[string]string{}
			for _, field := range decoded.Error.Fields {
				seen[field.Field] = field.Message
				if field.Code == "" {
					t.Errorf("field %q carries no machine-readable code", field.Field)
				}
			}
			for field, fragment := range tt.wantFields {
				message, ok := seen[field]
				if !ok {
					t.Errorf("response does not name field %q: %s", field, response.Body.String())
					continue
				}
				if !strings.Contains(message, fragment) {
					t.Errorf("message for %q = %q, want it to mention %q", field, message, fragment)
				}
			}
			// FR-009: nothing the caller submitted may be echoed back.
			for _, secret := range []string{submittedPassword, submittedKey, submittedSMTPPassword} {
				if strings.Contains(response.Body.String(), secret) {
					t.Errorf("response echoed a submitted secret: %s", response.Body.String())
				}
			}
		})
	}
}

// TestOwnerIntegrationsAreEditable is feature 011's initial red. Provider credentials are
// write-once today: nothing but bootstrap ever writes a value, so an expired EODHD key or a
// changed mail password has no supported recovery short of a new database.
func TestOwnerIntegrationsAreEditable(t *testing.T) {
	ownerRouter := func(stub *integrationAdminStub) http.Handler {
		return NewRouter(Dependencies{
			Database: databaseStub{},
			Authenticator: sessionAuthenticatorFunc(func(context.Context, string) (auth.Principal, error) {
				return auth.Principal{
					UserID: "10000000-0000-4000-8000-000000000001", Role: "owner",
					SessionID:  "20000000-0000-4000-8000-000000000001",
					VerifyCSRF: func(string) bool { return true },
				}, nil
			}),
			Authentication:   &ownerAuthenticationStub{},
			Integrations:     integrationStatusReaderStub{},
			IntegrationAdmin: stub,
		})
	}
	body := `{"smtp":{"host":"smtp.new.test","port":2525,"from":"ops@example.test","username":"mailer"}}`
	send := func(t *testing.T, handler http.Handler, method, path string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", "csrf")
		request.AddCookie(&http.Cookie{Name: httpx.SessionCookieName, Value: "opaque-session"})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	t.Run("a change can be checked without storing it", func(t *testing.T) {
		stub := &integrationAdminStub{}
		response := send(t, ownerRouter(stub), http.MethodPost, "/api/v1/owner/integrations/verify")
		if response.Code != http.StatusOK {
			t.Fatalf("verify status = %d body=%s, want 200", response.Code, response.Body.String())
		}
		if stub.verified != 1 || stub.updated != 0 {
			t.Fatalf("verify calls=%d update calls=%d, want 1 and 0", stub.verified, stub.updated)
		}
		if stub.request.SMTP == nil || stub.request.SMTP.Host != "smtp.new.test" || stub.request.SMTP.Port != 2525 {
			t.Fatalf("verified request = %#v", stub.request.SMTP)
		}
		// An omitted password means "keep the stored one", which the service must be able to
		// tell apart from an explicit empty one.
		if stub.request.SMTP.Password != nil {
			t.Error("an omitted SMTP password was decoded as a supplied value")
		}
	})

	t.Run("a verified change is saved", func(t *testing.T) {
		stub := &integrationAdminStub{}
		// A save reports each integration's result too, so the confirmation says which half
		// was checked rather than only that the request succeeded.
		response := send(t, ownerRouter(stub), http.MethodPut, "/api/v1/owner/integrations")
		if response.Code != http.StatusOK {
			t.Fatalf("update status = %d body=%s, want 200", response.Code, response.Body.String())
		}
		if !strings.Contains(response.Body.String(), `"results"`) {
			t.Fatalf("save did not report per-integration results: %s", response.Body.String())
		}
		if stub.updated != 1 {
			t.Fatalf("update calls = %d, want 1", stub.updated)
		}
	})

	t.Run("a change that does not verify is reported by field and stored nowhere", func(t *testing.T) {
		stub := &integrationAdminStub{err: &identity.SetupValidationError{Fields: []identity.SetupFieldError{
			{Field: "smtp_password", Code: "auth_rejected", Message: "The mail server rejected these credentials."},
		}}}
		response := send(t, ownerRouter(stub), http.MethodPut, "/api/v1/owner/integrations")
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", response.Code)
		}
		if !strings.Contains(response.Body.String(), "smtp_password") {
			t.Fatalf("rejection did not name the field: %s", response.Body.String())
		}
	})

	t.Run("a member may not read, check, or change integrations", func(t *testing.T) {
		stub := &integrationAdminStub{}
		memberRouter := NewRouter(Dependencies{
			Database: databaseStub{},
			Authenticator: sessionAuthenticatorFunc(func(context.Context, string) (auth.Principal, error) {
				return auth.Principal{
					UserID: "10000000-0000-4000-8000-000000000002", Role: "member",
					SessionID:  "20000000-0000-4000-8000-000000000002",
					VerifyCSRF: func(string) bool { return true },
				}, nil
			}),
			Authentication: &ownerAuthenticationStub{}, Integrations: integrationStatusReaderStub{},
			IntegrationAdmin: stub,
		})
		for _, call := range []struct{ method, path string }{
			{http.MethodGet, "/api/v1/owner/integrations"},
			{http.MethodPost, "/api/v1/owner/integrations/verify"},
			{http.MethodPut, "/api/v1/owner/integrations"},
		} {
			response := send(t, memberRouter, call.method, call.path)
			if response.Code != http.StatusForbidden {
				t.Errorf("%s %s status = %d, want 403", call.method, call.path, response.Code)
			}
			if strings.Contains(response.Body.String(), "smtp") {
				t.Errorf("%s %s disclosed configuration to a member: %s", call.method, call.path, response.Body.String())
			}
		}
		if stub.verified != 0 || stub.updated != 0 {
			t.Fatalf("a member reached the service: verify=%d update=%d", stub.verified, stub.updated)
		}
	})
}

type integrationAdminStub struct {
	verified int
	updated  int
	request  identity.IntegrationUpdate
	outcomes identity.IntegrationOutcomes
	err      error
}

func (stub *integrationAdminStub) VerifyIntegrations(_ context.Context, _ identity.Actor,
	request identity.IntegrationUpdate) (identity.IntegrationOutcomes, error) {
	stub.verified++
	stub.request = request
	return stub.outcomes, stub.err
}

func (stub *integrationAdminStub) UpdateIntegrations(_ context.Context, _ identity.Actor,
	request identity.IntegrationUpdate) (identity.IntegrationOutcomes, error) {
	stub.updated++
	stub.request = request
	return stub.outcomes, stub.err
}

func (stub *integrationAdminStub) IntegrationSettings(_ context.Context,
	_ identity.Actor) (identity.IntegrationSettings, error) {
	validated := "2026-08-31T09:00:00Z"
	return identity.IntegrationSettings{
		EODHDConfigured: true, EODHDValidatedAt: &validated, SMTPConfigured: true,
		SMTP: identity.SMTPIntegrationSettings{
			Host: "smtp.example.test", Port: 587, From: "access@example.test",
			Username: "mailer", PasswordConfigured: true,
		},
	}, nil
}

// The owner must be able to read the configuration in order to correct it, and must never be
// able to read a secret back.
func TestOwnerIntegrationSettingsAreReadableAndSecretFree(t *testing.T) {
	router := NewRouter(Dependencies{
		Database: databaseStub{},
		Authenticator: sessionAuthenticatorFunc(func(context.Context, string) (auth.Principal, error) {
			return auth.Principal{
				UserID: "10000000-0000-4000-8000-000000000001", Role: "owner",
				SessionID: "20000000-0000-4000-8000-000000000001",
			}, nil
		}),
		Authentication: &ownerAuthenticationStub{},
		Integrations: integrationStatusReaderStub{statuses: []credentials.Status{
			{Kind: credentials.KindEODHDAPI, Configured: true, Ready: true, KeyVersion: 1},
			{Kind: credentials.KindSMTP, Configured: true, Ready: true, KeyVersion: 1},
		}},
		IntegrationAdmin: &integrationAdminStub{},
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/owner/integrations", nil)
	request.AddCookie(&http.Cookie{Name: httpx.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}

	var body struct {
		Settings struct {
			EODHD struct {
				Configured  bool    `json:"configured"`
				ValidatedAt *string `json:"validated_at"`
			} `json:"eodhd"`
			SMTP struct {
				Configured         bool   `json:"configured"`
				Host               string `json:"host"`
				Port               int    `json:"port"`
				From               string `json:"from"`
				Username           string `json:"username"`
				PasswordConfigured bool   `json:"password_configured"`
			} `json:"smtp"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not the documented shape: %v: %s", err, response.Body.String())
	}
	if body.Settings.SMTP.Host != "smtp.example.test" || body.Settings.SMTP.Port != 587 ||
		body.Settings.SMTP.Username != "mailer" || !body.Settings.SMTP.PasswordConfigured {
		t.Fatalf("smtp settings = %#v", body.Settings.SMTP)
	}
	if !body.Settings.EODHD.Configured || body.Settings.EODHD.ValidatedAt == nil {
		t.Fatalf("eodhd settings = %#v", body.Settings.EODHD)
	}
	// The two write-only values must have no representation at all in the response.
	for _, forbidden := range []string{"api_key", "\"password\"", "secret"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Errorf("settings disclosed %q: %s", forbidden, response.Body.String())
		}
	}
}

// Each integration is reported separately, so the owner is told which half of their
// configuration works. "No error" is not the same as "verified": a shape problem stops every
// network call, and claiming success for something never checked would be a lie.
func TestIntegrationChecksReportEachIntegrationSeparately(t *testing.T) {
	tests := []struct {
		name       string
		outcomes   identity.IntegrationOutcomes
		err        error
		wantStatus int
		wantEODHD  string
		wantSMTP   string
	}{
		{
			name:       "both verified",
			outcomes:   identity.IntegrationOutcomes{EODHD: identity.IntegrationVerified, SMTP: identity.IntegrationVerified},
			wantStatus: http.StatusOK, wantEODHD: "verified", wantSMTP: "verified",
		},
		{
			name: "mail fails while the provider key passes",
			outcomes: identity.IntegrationOutcomes{
				EODHD: identity.IntegrationVerified, SMTP: identity.IntegrationFailed,
			},
			err: &identity.SetupValidationError{Fields: []identity.SetupFieldError{
				{Field: "smtp_password", Code: "auth_rejected", Message: "rejected"},
			}},
			wantStatus: http.StatusBadRequest, wantEODHD: "verified", wantSMTP: "failed",
		},
		{
			name: "a shape problem means neither was checked",
			outcomes: identity.IntegrationOutcomes{
				EODHD: identity.IntegrationNotChecked, SMTP: identity.IntegrationNotChecked,
			},
			err: &identity.SetupValidationError{Fields: []identity.SetupFieldError{
				{Field: "smtp_port", Code: "out_of_range", Message: "range"},
			}},
			wantStatus: http.StatusBadRequest, wantEODHD: "not_checked", wantSMTP: "not_checked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &integrationAdminStub{outcomes: tt.outcomes, err: tt.err}
			router := NewRouter(Dependencies{
				Database: databaseStub{},
				Authenticator: sessionAuthenticatorFunc(func(context.Context, string) (auth.Principal, error) {
					return auth.Principal{
						UserID: "10000000-0000-4000-8000-000000000001", Role: "owner",
						SessionID:  "20000000-0000-4000-8000-000000000001",
						VerifyCSRF: func(string) bool { return true },
					}, nil
				}),
				Authentication: &ownerAuthenticationStub{}, Integrations: integrationStatusReaderStub{},
				IntegrationAdmin: stub,
			})
			request := httptest.NewRequest(http.MethodPost, "/api/v1/owner/integrations/verify",
				strings.NewReader(`{"smtp":{"host":"smtp.example.test","port":587,"from":"a@example.test"}}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-CSRF-Token", "csrf")
			request.AddCookie(&http.Cookie{Name: httpx.SessionCookieName, Value: "opaque-session"})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, tt.wantStatus, response.Body.String())
			}
			var body struct {
				Results map[string]string `json:"results"`
				Error   struct {
					Results map[string]string `json:"results"`
				} `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("response is not the documented shape: %v: %s", err, response.Body.String())
			}
			results := body.Results
			if len(results) == 0 {
				results = body.Error.Results
			}
			if results["eodhd"] != tt.wantEODHD || results["smtp"] != tt.wantSMTP {
				t.Fatalf("results = %v, want eodhd %q smtp %q", results, tt.wantEODHD, tt.wantSMTP)
			}
		})
	}
}
