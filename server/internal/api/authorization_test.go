package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/credentials"
	"market-lens/server/internal/httpx"
	"market-lens/server/internal/identity"
)

func TestAnonymousCanLoadOnlyAuthenticationSPAShellRoutes(t *testing.T) {
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("auth shell"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(staticDir, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "assets", "app.js"), []byte("public code"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "favicon.svg"), []byte("icon"), 0o600); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Dependencies{Database: databaseStub{}, StaticDir: staticDir})

	for _, path := range []string{"/login", "/setup", "/invite"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || response.Body.String() != "auth shell" {
			t.Fatalf("GET %s status=%d body=%q", path, response.Code, response.Body.String())
		}
	}
	recovery := httptest.NewRecorder()
	router.ServeHTTP(recovery, httptest.NewRequest(http.MethodGet, "/recover", nil))
	if recovery.Code != http.StatusUnauthorized {
		t.Fatalf("retired recovery shell status=%d body=%q", recovery.Code, recovery.Body.String())
	}
	asset := httptest.NewRecorder()
	router.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if asset.Code != http.StatusOK || asset.Body.String() != "public code" {
		t.Fatalf("GET public application asset status=%d body=%q", asset.Code, asset.Body.String())
	}
	// The sign-in shell references the favicon, so gating it behind authentication makes every
	// anonymous page request a failed icon fetch while disclosing nothing worth protecting.
	favicon := httptest.NewRecorder()
	router.ServeHTTP(favicon, httptest.NewRequest(http.MethodGet, "/favicon.svg", nil))
	if favicon.Code != http.StatusOK || favicon.Body.String() != "icon" {
		t.Fatalf("GET favicon status=%d body=%q", favicon.Code, favicon.Body.String())
	}
	// A request that is not browser navigation — no HTML in Accept — receives a refusal it
	// can act on rather than a redirect it would have to follow to discover the same thing.
	// Browser navigation to these same paths is sent to sign-in instead; see
	// TestAnonymousNavigationIsSentToSignInWhileDataStaysRefused.
	for _, path := range []string{"/", "/markets", "/account"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous GET %s status=%d body=%q", path, response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "auth shell") {
			t.Fatalf("anonymous GET %s served the application shell", path)
		}
	}
}

func TestApplicationAndSharedDataRoutesDenyAnonymousExpiredAndRevokedSessions(t *testing.T) {
	privatePaths := []string{
		"/markets",
		"/api/v1/instruments",
		"/api/v1/instruments/33000000-0000-4000-8000-000000000001",
		"/api/v1/instruments/33000000-0000-4000-8000-000000000001/prices",
		"/api/v1/market-data/imports",
		"/api/v1/market-data/imports/22000000-0000-4000-8000-000000000001",
		"/api/v1/market-data/quality-findings",
		"/api/v1/events",
	}
	tests := []struct {
		name          string
		cookie        string
		authenticator SessionAuthenticator
	}{
		{name: "anonymous"},
		{name: "expired", cookie: "expired-session", authenticator: sessionAuthenticatorFunc(func(context.Context, string) (auth.Principal, error) {
			return auth.Principal{}, auth.ErrAuthenticationRequired
		})},
		{name: "revoked", cookie: "revoked-session", authenticator: sessionAuthenticatorFunc(func(context.Context, string) (auth.Principal, error) {
			return auth.Principal{}, auth.ErrAuthenticationRequired
		})},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := NewRouter(Dependencies{
				Database: databaseStub{}, Authenticator: test.authenticator,
				Instruments: &instrumentReaderStub{}, MarketData: &marketDataReaderStub{}, Events: &eventReaderStub{},
			})
			for _, path := range privatePaths {
				request := httptest.NewRequest(http.MethodGet, path, nil)
				if test.cookie != "" {
					request.AddCookie(&http.Cookie{Name: httpx.SessionCookieName, Value: test.cookie})
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				if response.Code != http.StatusUnauthorized {
					t.Fatalf("GET %s status=%d body=%s", path, response.Code, response.Body.String())
				}
			}
		})
	}
}

func TestActiveSessionCanReadSharedRoutesWhileLivenessRemainsPublic(t *testing.T) {
	authenticator := sessionAuthenticatorFunc(func(_ context.Context, token string) (auth.Principal, error) {
		if token != "active-session" {
			return auth.Principal{}, auth.ErrAuthenticationRequired
		}
		return auth.Principal{
			UserID: "10000000-0000-4000-8000-000000000001", Role: "owner",
			SessionID: "20000000-0000-4000-8000-000000000001", VerifyCSRF: func(string) bool { return true },
		}, nil
	})
	router := NewRouter(Dependencies{
		Database: databaseStub{}, Authenticator: authenticator, Instruments: &instrumentReaderStub{},
	})

	for _, path := range []string{"/api/v1/health", "/api/v1/ready"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("public GET %s status=%d", path, response.Code)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/instruments", nil)
	request.AddCookie(&http.Cookie{Name: httpx.SessionCookieName, Value: "active-session"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("authenticated shared route status=%d body=%s", response.Code, response.Body.String())
	}
}

type sessionAuthenticatorFunc func(context.Context, string) (auth.Principal, error)

func (function sessionAuthenticatorFunc) AuthenticateSession(ctx context.Context, token string) (auth.Principal, error) {
	return function(ctx, token)
}

// ownerAdministrationRoutes is every route whose authority is instance ownership rather than
// data ownership. Each entry names the method, the path, and whether it mutates state.
var ownerAdministrationRoutes = []struct {
	method  string
	path    string
	mutates bool
	body    string
}{
	{method: http.MethodGet, path: "/api/v1/owner/members"},
	{method: http.MethodGet, path: "/api/v1/owner/invitations"},
	{method: http.MethodGet, path: "/api/v1/owner/integrations"},
	{method: http.MethodPost, path: "/api/v1/owner/members/10000000-0000-4000-8000-000000000601/unlock", mutates: true},
	{method: http.MethodPatch, path: "/api/v1/owner/members/10000000-0000-4000-8000-000000000601/status",
		mutates: true, body: `{"status":"deactivated"}`},
	{method: http.MethodPost, path: "/api/v1/owner/invitations", mutates: true, body: `{"email":"new@example.com"}`},
	{method: http.MethodPost, path: "/api/v1/owner/invitations/70000000-0000-4000-8000-000000000001/resend", mutates: true},
	{method: http.MethodDelete, path: "/api/v1/owner/invitations/70000000-0000-4000-8000-000000000001", mutates: true},
}

// permissiveAdministration answers every administration call successfully. It stands in for a
// service that has forgotten its own owner check, so the router's boundary is what is measured.
type permissiveAdministration struct {
	memberAdministrationStub
	invitationAdministrationStub
	reached bool
}

func (stub *permissiveAdministration) ListMembers(ctx context.Context, actor identity.Actor,
	cursor string, limit int) (identity.MemberPage, error) {
	stub.reached = true
	return identity.MemberPage{Members: []identity.Member{{
		ID: "10000000-0000-4000-8000-000000000601", Email: "member-b@example.com",
		Status: identity.StatusActive, LoginState: identity.MemberLoginAvailable,
	}}}, nil
}

func (stub *permissiveAdministration) UnlockMember(ctx context.Context, actor identity.Actor, memberID string) error {
	stub.reached = true
	return nil
}

func (stub *permissiveAdministration) SetMemberStatus(ctx context.Context, actor identity.Actor,
	memberID string, status identity.Status) error {
	stub.reached = true
	return nil
}

func (stub *permissiveAdministration) Member(ctx context.Context, actor identity.Actor,
	memberID string) (identity.Member, error) {
	stub.reached = true
	return identity.Member{ID: memberID, Status: identity.StatusActive}, nil
}

func (stub *permissiveAdministration) ListInvitations(ctx context.Context, actor identity.Actor,
	cursor string, limit int) (identity.InvitationPage, error) {
	stub.reached = true
	return identity.InvitationPage{Items: []identity.Invitation{{
		ID: "70000000-0000-4000-8000-000000000001", Email: "invitee@example.com",
		State: identity.InvitationPending,
	}}}, nil
}

func (stub *permissiveAdministration) CreateInvitation(ctx context.Context, actor identity.Actor,
	email string) (identity.Invitation, error) {
	stub.reached = true
	return identity.Invitation{ID: "70000000-0000-4000-8000-000000000002", Email: email}, nil
}

func (stub *permissiveAdministration) ResendInvitation(ctx context.Context, actor identity.Actor,
	invitationID string) (identity.Invitation, error) {
	stub.reached = true
	return identity.Invitation{ID: invitationID}, nil
}

func (stub *permissiveAdministration) RevokeInvitation(ctx context.Context, actor identity.Actor, invitationID string) error {
	stub.reached = true
	return nil
}

type permissiveIntegrations struct{ reached *bool }

func (stub permissiveIntegrations) Statuses(context.Context) ([]credentials.Status, error) {
	*stub.reached = true
	return []credentials.Status{{Kind: "example_provider", Configured: true, Ready: true}}, nil
}

func TestOwnerAdministrationRoutesRefuseMembersBeforeReachingTheService(t *testing.T) {
	member := principalStub{
		userID: "10000000-0000-4000-8000-000000000601", role: "member",
		sessionID: "20000000-0000-4000-8000-000000000601",
	}
	for _, route := range ownerAdministrationRoutes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			administration := &permissiveAdministration{}
			integrationsReached := false
			router := NewRouter(Dependencies{
				Authenticator: member, Members: administration, Invitations: administration,
				Integrations: permissiveIntegrations{reached: &integrationsReached}, SecureCookies: true,
			})
			response := performCSRFRequest(router, route.method, route.path, route.body, member)
			if response.Code != http.StatusForbidden {
				t.Fatalf("member %s %s = %d %s, want 403", route.method, route.path,
					response.Code, response.Body.String())
			}
			if administration.reached || integrationsReached {
				t.Fatalf("a member's %s %s reached the administration service", route.method, route.path)
			}
			// The refusal must not describe what exists behind it.
			for _, disclosure := range []string{"member-b@example.com", "invitee@example.com", "example_provider"} {
				if strings.Contains(response.Body.String(), disclosure) {
					t.Fatalf("refusal disclosed %q: %s", disclosure, response.Body.String())
				}
			}
		})
	}
}

func TestOwnerAuthorityCannotBeAssertedByTheClient(t *testing.T) {
	member := principalStub{
		userID: "10000000-0000-4000-8000-000000000601", role: "member",
		sessionID: "20000000-0000-4000-8000-000000000601",
	}
	// Every place a client could try to name its own role or somebody else's identity.
	overrides := []struct {
		name    string
		headers map[string]string
		query   string
	}{
		{name: "role header", headers: map[string]string{"X-Role": "owner", "X-User-Role": "owner"}},
		{name: "impersonation header", headers: map[string]string{
			"X-User-Id": "10000000-0000-4000-8000-000000000001", "X-Owner": "true"}},
		{name: "query parameter", query: "?role=owner&user_id=10000000-0000-4000-8000-000000000001&actor=owner"},
	}
	for _, override := range overrides {
		t.Run(override.name, func(t *testing.T) {
			administration := &permissiveAdministration{}
			router := NewRouter(Dependencies{
				Authenticator: member, Members: administration, Invitations: administration, SecureCookies: true,
			})
			request := httptest.NewRequest(http.MethodGet, "/api/v1/owner/members"+override.query, nil)
			request.AddCookie(&http.Cookie{Name: httpx.SessionCookieName, Value: "active-session-secret"})
			for name, value := range override.headers {
				request.Header.Set(name, value)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden || administration.reached {
				t.Fatalf("%s produced %d reached=%t, want a 403 that never reached the service",
					override.name, response.Code, administration.reached)
			}
		})
	}
}

func TestTheOwnerActorAlwaysCarriesTheAuthenticatedPrincipal(t *testing.T) {
	owner := principalStub{
		userID: "10000000-0000-4000-8000-000000000001", role: "owner",
		sessionID: "20000000-0000-4000-8000-000000000001",
	}
	members := &memberAdministrationStub{page: identity.MemberPage{Members: []identity.Member{{
		ID: "10000000-0000-4000-8000-000000000601", Email: "member-b@example.com",
		Status: identity.StatusActive, LoginState: identity.MemberLoginAvailable,
	}}}}
	router := NewRouter(Dependencies{Authenticator: owner, Members: members, SecureCookies: true})

	// Even when the client names a different subject, the actor is the session's principal.
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/owner/members?user_id=10000000-0000-4000-8000-000000000601", nil)
	request.AddCookie(&http.Cookie{Name: httpx.SessionCookieName, Value: "active-session-secret"})
	request.Header.Set("X-User-Id", "10000000-0000-4000-8000-000000000601")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("owner listing = %d %s", response.Code, response.Body.String())
	}
	if members.actor.UserID != owner.userID || members.actor.Role != identity.RoleOwner {
		t.Fatalf("propagated actor = %#v, want the authenticated owner", members.actor)
	}
}

func TestPrivateResourcesGuessedByAnotherUserAnswerLikeUnknownOnes(t *testing.T) {
	member := principalStub{
		userID: "10000000-0000-4000-8000-000000000601", role: "member",
		sessionID: "20000000-0000-4000-8000-000000000601",
	}
	// The session service refuses every subject that is not the caller's own, so a correctly
	// guessed identifier and an invented one must be indistinguishable from outside.
	authentication := &scopedSessionStub{ownerUserID: member.userID}
	router := NewRouter(Dependencies{Authenticator: member, Authentication: authentication, SecureCookies: true})

	guessed := performCSRFRequest(router, http.MethodDelete,
		"/api/v1/account/sessions/20000000-0000-4000-8000-0000000000ff", "", member)
	unknown := performCSRFRequest(router, http.MethodDelete,
		"/api/v1/account/sessions/20000000-0000-4000-8000-00000000dead", "", member)
	if guessed.Code != unknown.Code || guessed.Body.String() != unknown.Body.String() {
		t.Fatalf("guessed=%d %s unknown=%d %s, want identical responses",
			guessed.Code, guessed.Body.String(), unknown.Code, unknown.Body.String())
	}
	if guessed.Code == http.StatusForbidden {
		t.Fatalf("a scoped miss answered 403, which confirms the resource exists: %s", guessed.Body.String())
	}
	if authentication.revokedFor != "" && authentication.revokedFor != member.userID {
		t.Fatalf("revocation was attempted for %q, want the authenticated member", authentication.revokedFor)
	}
}

// scopedSessionStub only ever acknowledges sessions belonging to ownerUserID.
type scopedSessionStub struct {
	ownerUserID string
	revokedFor  string
}

func (stub *scopedSessionStub) StartSignIn(context.Context, auth.SignInStartRequest) (auth.SignInStartResult, error) {
	return auth.SignInStartResult{}, auth.ErrAuthenticationRequired
}

func (stub *scopedSessionStub) LoginOwner(context.Context, auth.OwnerLoginRequest) (auth.AuthenticationResult, error) {
	return auth.AuthenticationResult{}, auth.ErrAuthenticationRequired
}

func (stub *scopedSessionStub) Account(context.Context, string) (auth.Account, error) {
	return auth.Account{}, auth.ErrAuthenticationRequired
}

func (stub *scopedSessionStub) ListSessions(context.Context, string, string) ([]auth.SessionSummary, error) {
	return nil, nil
}

func (stub *scopedSessionStub) RevokeSession(_ context.Context, userID, _ string) error {
	stub.revokedFor = userID
	return auth.ErrAuthenticationRequired
}

func (stub *scopedSessionStub) RevokeAllSessions(context.Context, string) error { return nil }

func TestAnonymousNavigationIsSentToSignInWhileDataStaysRefused(t *testing.T) {
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("app shell"), 0o600); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Dependencies{
		Database: databaseStub{}, StaticDir: staticDir,
		Instruments: &instrumentReaderStub{}, MarketData: &marketDataReaderStub{}, Events: &eventReaderStub{},
	})

	// Somebody typing the bare domain is a person, not a script. Answering them with a JSON
	// error is a dead end; they are sent to sign in, and told where they were going.
	for _, path := range []string{"/", "/markets", "/account", "/markets/33000000-0000-4000-8000-000000000001"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusFound {
			t.Fatalf("anonymous navigation to %s = %d %s, want a redirect to sign-in",
				path, response.Code, response.Body.String())
		}
		location := response.Header().Get("Location")
		if !strings.HasPrefix(location, "/login?redirect=") {
			t.Fatalf("anonymous navigation to %s redirected to %q", path, location)
		}
		// The redirect must stay on this site: a Location carrying a scheme or an authority
		// would send somebody to another origin because they asked for one.
		if target, err := url.Parse(location); err != nil || target.Scheme != "" || target.Host != "" {
			t.Fatalf("redirect target for %s leaves this origin: %q (%v)", path, location, err)
		}
		// No protected content may be served alongside the redirect.
		if strings.Contains(response.Body.String(), "app shell") {
			t.Fatalf("anonymous navigation to %s was served the application shell", path)
		}
	}

	// An off-site destination is never reachable through the redirect, however it is asked
	// for. Whatever this answers, the Location must name no other origin.
	for _, hostileTarget := range []string{"//evil.example.com/", "/\\evil.example.com", "https://evil.example.com/"} {
		hostile := httptest.NewRequest(http.MethodGet, hostileTarget, nil)
		hostile.Header.Set("Accept", "text/html")
		hostileResponse := httptest.NewRecorder()
		router.ServeHTTP(hostileResponse, hostile)
		location := hostileResponse.Header().Get("Location")
		if location == "" {
			continue
		}
		target, err := url.Parse(location)
		if err != nil || target.Scheme != "" || target.Host != "" {
			t.Fatalf("request for %q produced an off-origin redirect: %q (%v)", hostileTarget, location, err)
		}
	}

	// Data is unaffected: an API caller still receives a refusal it can act on, and the
	// Accept header must not be a way to turn one into a redirect.
	for _, path := range []string{
		"/api/v1/instruments", "/api/v1/market-data/imports", "/api/v1/events", "/api/v1/account",
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Accept", "text/html")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous %s = %d, want 401 regardless of Accept", path, response.Code)
		}
	}
}
