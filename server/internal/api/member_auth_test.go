package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/httpx"
	"market-lens/server/internal/identity"
)

type memberAuthenticationStub struct {
	result  auth.AuthenticationResult
	err     error
	request auth.MemberCodeVerifyRequest
	calls   int
}

func (stub *memberAuthenticationStub) VerifyMemberCode(_ context.Context, request auth.MemberCodeVerifyRequest) (auth.AuthenticationResult, error) {
	stub.request = request
	stub.calls++
	return stub.result, stub.err
}

type memberAdministrationStub struct {
	page         identity.MemberPage
	listErr      error
	unlockErr    error
	unlocked     string
	actor        identity.Actor
	listCursor   string
	listLimit    int
	statusMember string
	statusValue  identity.Status
	statusErr    error
	memberErr    error
}

type invitationAdministrationStub struct {
	page       identity.InvitationPage
	created    identity.Invitation
	createErr  error
	createdFor string
	resendErr  error
	resent     string
	revokeErr  error
	revoked    string
	accepted   identity.BootstrapResult
	acceptErr  error
	acceptedAs identity.AcceptInvitationRequest
	actor      identity.Actor
}

func (stub *invitationAdministrationStub) ListInvitations(_ context.Context, actor identity.Actor, _ string, _ int) (identity.InvitationPage, error) {
	stub.actor = actor
	return stub.page, stub.createErr
}

func (stub *invitationAdministrationStub) CreateInvitation(_ context.Context, actor identity.Actor, email string) (identity.Invitation, error) {
	stub.actor, stub.createdFor = actor, email
	return stub.created, stub.createErr
}

func (stub *invitationAdministrationStub) ResendInvitation(_ context.Context, actor identity.Actor, id string) (identity.Invitation, error) {
	stub.actor, stub.resent = actor, id
	return stub.created, stub.resendErr
}

func (stub *invitationAdministrationStub) RevokeInvitation(_ context.Context, actor identity.Actor, id string) error {
	stub.actor, stub.revoked = actor, id
	return stub.revokeErr
}

func (stub *invitationAdministrationStub) AcceptInvitation(_ context.Context, request identity.AcceptInvitationRequest) (identity.BootstrapResult, error) {
	stub.acceptedAs = request
	return stub.accepted, stub.acceptErr
}

func (stub *memberAdministrationStub) ListMembers(_ context.Context, actor identity.Actor, cursor string, limit int) (identity.MemberPage, error) {
	stub.actor, stub.listCursor, stub.listLimit = actor, cursor, limit
	return stub.page, stub.listErr
}

func (stub *memberAdministrationStub) UnlockMember(_ context.Context, actor identity.Actor, memberID string) error {
	stub.actor, stub.unlocked = actor, memberID
	return stub.unlockErr
}

func (stub *memberAdministrationStub) SetMemberStatus(_ context.Context, actor identity.Actor,
	memberID string, status identity.Status) error {
	stub.actor, stub.statusMember, stub.statusValue = actor, memberID, status
	return stub.statusErr
}

func (stub *memberAdministrationStub) Member(_ context.Context, actor identity.Actor, memberID string) (identity.Member, error) {
	stub.actor = actor
	if len(stub.page.Members) > 0 {
		return stub.page.Members[0], stub.memberErr
	}
	return identity.Member{ID: memberID, Status: stub.statusValue, LoginState: identity.MemberLoginAvailable}, stub.memberErr
}

func TestMemberCodeVerifyEstablishesASecureSessionWithoutPasswordFields(t *testing.T) {
	now := time.Date(2026, time.August, 30, 10, 0, 0, 0, time.UTC)
	absoluteExpiry := now.Add(30 * 24 * time.Hour)
	member := &memberAuthenticationStub{result: auth.AuthenticationResult{
		Account: auth.Account{
			ID: "10000000-0000-4000-8000-000000000501", Email: "member@example.com",
			DisplayName: "Member One", Role: "member", Status: "active", EmailVerifiedAt: now,
		},
		Session:      auth.SessionSummary{ID: "20000000-0000-4000-8000-000000000501", AbsoluteExpiresAt: absoluteExpiry},
		SessionToken: "member-session-secret", CSRFToken: "member-csrf-secret",
	}}
	router := NewRouter(Dependencies{MemberAuth: member, SecureCookies: true})

	response := performAuthRequest(router, http.MethodPost, "/api/v1/auth/member/code/verify",
		`{"email":"member@example.com","code":"012345"}`)
	assertAuthenticatedHTTPResponse(t, response, http.StatusOK, "member-csrf-secret", "member-session-secret", absoluteExpiry, "member")
	if member.request.Email != "member@example.com" || member.request.Code != "012345" ||
		member.request.Origin == "" || member.request.DeviceLabel == "" {
		t.Fatalf("verify request = %#v", member.request)
	}
	if strings.Contains(response.Body.String(), "012345") {
		t.Fatalf("the submitted code was echoed back: %s", response.Body.String())
	}

	// The endpoint accepts no password field at all.
	rejected := performAuthRequest(router, http.MethodPost, "/api/v1/auth/member/code/verify",
		`{"email":"member@example.com","code":"012345","password":"anything"}`)
	if rejected.Code != http.StatusBadRequest {
		t.Fatalf("password field response = %d, want 400", rejected.Code)
	}
}

func TestMemberCodeVerifyFailuresAreUniformAndThrottlingIsCoarse(t *testing.T) {
	generic := &memberAuthenticationStub{err: auth.ErrAuthenticationFailed}
	router := NewRouter(Dependencies{MemberAuth: generic, SecureCookies: true})

	var bodies []string
	for _, body := range []string{
		`{"email":"member@example.com","code":"999999"}`,
		`{"email":"unknown@example.com","code":"012345"}`,
		`{"email":"owner@example.com","code":"012345"}`,
	} {
		response := performAuthRequest(router, http.MethodPost, "/api/v1/auth/member/code/verify", body)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("body %s status = %d, want 401", body, response.Code)
		}
		if cookie := response.Header().Get("Set-Cookie"); cookie != "" {
			t.Fatalf("a failed verification set a cookie: %s", cookie)
		}
		bodies = append(bodies, response.Body.String())
	}
	for _, body := range bodies[1:] {
		if body != bodies[0] {
			t.Fatalf("failure bodies differ:\n%s\n%s", bodies[0], body)
		}
	}

	// A malformed code is rejected before reaching the service, and never as a 401 variant.
	malformed := performAuthRequest(router, http.MethodPost, "/api/v1/auth/member/code/verify",
		`{"email":"member@example.com","code":"12ab56"}`)
	if malformed.Code != http.StatusUnauthorized || malformed.Body.String() != bodies[0] {
		t.Fatalf("malformed code = %d %s, want the uniform 401", malformed.Code, malformed.Body.String())
	}

	limited := &memberAuthenticationStub{err: &auth.RateLimitedError{RetryAfter: 2 * time.Minute}}
	limitedRouter := NewRouter(Dependencies{MemberAuth: limited, SecureCookies: true})
	throttled := performAuthRequest(limitedRouter, http.MethodPost, "/api/v1/auth/member/code/verify",
		`{"email":"member@example.com","code":"012345"}`)
	if throttled.Code != http.StatusTooManyRequests {
		t.Fatalf("throttled status = %d, want 429", throttled.Code)
	}
	if throttled.Header().Get("Retry-After") != "120" {
		t.Fatalf("Retry-After = %q, want a coarse 120", throttled.Header().Get("Retry-After"))
	}
}

func TestSignInStartReportsCoarseThrottlingWithoutIdentifyingAnAccount(t *testing.T) {
	authentication := &ownerAuthenticationStub{signInErr: &auth.RateLimitedError{RetryAfter: time.Minute}}
	router := NewRouter(Dependencies{Authentication: authentication, SecureCookies: true})
	response := performAuthRequest(router, http.MethodPost, "/api/v1/auth/sign-in/start", `{"email":"member@example.com"}`)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("throttled sign-in status = %d, want 429", response.Code)
	}
	if response.Header().Get("Retry-After") != "60" {
		t.Fatalf("Retry-After = %q, want 60", response.Header().Get("Retry-After"))
	}
	if strings.Contains(strings.ToLower(response.Body.String()), "member@example.com") {
		t.Fatalf("throttled body echoed the address: %s", response.Body.String())
	}
}

func TestSignInStartBodyMatchesTheGenericContractFieldName(t *testing.T) {
	authentication := &ownerAuthenticationStub{}
	router := NewRouter(Dependencies{Authentication: authentication, SecureCookies: true})
	response := performAuthRequest(router, http.MethodPost, "/api/v1/auth/sign-in/start", `{"email":"member@example.com"}`)
	if response.Code != http.StatusAccepted {
		t.Fatalf("sign-in start status = %d, want 202", response.Code)
	}
	// The contract names this field "message"; the client reads it verbatim, so a Go-cased
	// field name would silently break the entire passwordless sign-in journey.
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["message"] != auth.GenericSignInMessage {
		t.Fatalf("sign-in start body = %s, want a lowercase message field", response.Body.String())
	}
	if len(body) != 1 {
		t.Fatalf("sign-in start body carried extra fields: %s", response.Body.String())
	}
}

func TestAuthenticationPublishesAReadableCSRFCookieThatSurvivesReload(t *testing.T) {
	now := time.Date(2026, time.August, 30, 10, 0, 0, 0, time.UTC)
	absoluteExpiry := now.Add(30 * 24 * time.Hour)
	member := &memberAuthenticationStub{result: auth.AuthenticationResult{
		Account: auth.Account{
			ID: "10000000-0000-4000-8000-000000000501", Email: "member@example.com",
			DisplayName: "Member One", Role: "member", Status: "active", EmailVerifiedAt: now,
		},
		Session:      auth.SessionSummary{ID: "20000000-0000-4000-8000-000000000501", AbsoluteExpiresAt: absoluteExpiry},
		SessionToken: "member-session-secret", CSRFToken: "member-csrf-secret",
	}}
	router := NewRouter(Dependencies{MemberAuth: member, SecureCookies: true})
	response := performAuthRequest(router, http.MethodPost, "/api/v1/auth/member/code/verify",
		`{"email":"member@example.com","code":"012345"}`)

	var session, csrf *http.Cookie
	for _, cookie := range response.Result().Cookies() {
		switch cookie.Name {
		case httpx.SessionCookieName:
			session = cookie
		case httpx.CSRFCookieName:
			csrf = cookie
		}
	}
	if session == nil || !session.HttpOnly {
		t.Fatalf("session cookie = %#v, want an HttpOnly cookie", session)
	}
	// The CSRF token is not a bearer credential: same-origin script must be able to read it so
	// a reloaded page can keep making authorized mutations without signing in again.
	if csrf == nil {
		t.Fatal("no CSRF cookie was published, so a reloaded page could never mutate again")
	}
	if csrf.HttpOnly {
		t.Fatal("the CSRF cookie is HttpOnly, so the page cannot read it after a reload")
	}
	if csrf.Value != "member-csrf-secret" || !csrf.Secure || csrf.Path != "/" ||
		csrf.SameSite != http.SameSiteLaxMode || !csrf.Expires.Equal(absoluteExpiry) {
		t.Fatalf("unsafe CSRF cookie: %#v", csrf)
	}
}

func TestOwnerMemberAdministrationRequiresOwnerSessionAndCSRF(t *testing.T) {
	now := time.Date(2026, time.August, 30, 10, 0, 0, 0, time.UTC)
	blockedUntil := now.Add(15 * time.Minute)
	members := &memberAdministrationStub{page: identity.MemberPage{Members: []identity.Member{{
		ID: "10000000-0000-4000-8000-000000000601", Email: "member@example.com", DisplayName: "Member One",
		Status: identity.StatusActive, LoginState: identity.MemberTemporarilyBlocked,
		BlockedUntil: &blockedUntil, ActiveSessionCount: 2, CreatedAt: now,
	}}}}
	owner := principalStub{userID: "10000000-0000-4000-8000-000000000001", role: "owner", sessionID: "20000000-0000-4000-8000-000000000001"}
	router := NewRouter(Dependencies{
		Members: members, Authenticator: owner, SecureCookies: true,
	})

	// Anonymous callers receive 401 and never reach the service.
	anonymous := performAuthRequest(router, http.MethodGet, "/api/v1/owner/members", "")
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous member listing = %d, want 401", anonymous.Code)
	}

	listing := performAuthenticatedRequest(router, http.MethodGet, "/api/v1/owner/members", "", owner)
	if listing.Code != http.StatusOK {
		t.Fatalf("owner member listing = %d %s", listing.Code, listing.Body.String())
	}
	var page struct {
		Members []struct {
			ID                 string `json:"id"`
			Email              string `json:"email"`
			LoginState         string `json:"login_state"`
			ActiveSessionCount int    `json:"active_session_count"`
		} `json:"members"`
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal(listing.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Members) != 1 || page.Members[0].LoginState != "temporarily_blocked" ||
		page.Members[0].ActiveSessionCount != 2 {
		t.Fatalf("member page = %s", listing.Body.String())
	}
	if members.actor.UserID != owner.userID || members.actor.Role != identity.RoleOwner {
		t.Fatalf("listing actor = %#v", members.actor)
	}

	// Unlock is a state change, so it requires CSRF in addition to the owner session.
	withoutCSRF := performAuthenticatedRequest(router, http.MethodPost,
		"/api/v1/owner/members/10000000-0000-4000-8000-000000000601/unlock", "", owner)
	if withoutCSRF.Code != http.StatusForbidden {
		t.Fatalf("unlock without CSRF = %d, want 403", withoutCSRF.Code)
	}
	if members.unlocked != "" {
		t.Fatal("a CSRF-less unlock reached the service")
	}

	unlocked := performCSRFRequest(router, http.MethodPost,
		"/api/v1/owner/members/10000000-0000-4000-8000-000000000601/unlock", "", owner)
	if unlocked.Code != http.StatusNoContent {
		t.Fatalf("unlock = %d %s, want 204", unlocked.Code, unlocked.Body.String())
	}
	if members.unlocked != "10000000-0000-4000-8000-000000000601" {
		t.Fatalf("unlocked member = %q", members.unlocked)
	}
}

func TestMemberAdministrationDeniesNonOwnersAndReportsMissingMembers(t *testing.T) {
	member := principalStub{userID: "10000000-0000-4000-8000-000000000601", role: "member", sessionID: "20000000-0000-4000-8000-000000000601"}
	denied := &memberAdministrationStub{listErr: identity.ErrOwnerRequired, unlockErr: identity.ErrOwnerRequired}
	router := NewRouter(Dependencies{Members: denied, Authenticator: member, SecureCookies: true})

	listing := performAuthenticatedRequest(router, http.MethodGet, "/api/v1/owner/members", "", member)
	if listing.Code != http.StatusForbidden {
		t.Fatalf("member listing as member = %d, want 403", listing.Code)
	}
	unlock := performCSRFRequest(router, http.MethodPost,
		"/api/v1/owner/members/10000000-0000-4000-8000-000000000701/unlock", "", member)
	if unlock.Code != http.StatusForbidden {
		t.Fatalf("unlock as member = %d, want 403", unlock.Code)
	}

	owner := principalStub{userID: "10000000-0000-4000-8000-000000000001", role: "owner", sessionID: "20000000-0000-4000-8000-000000000001"}
	missing := &memberAdministrationStub{unlockErr: identity.ErrMemberNotFound}
	missingRouter := NewRouter(Dependencies{Members: missing, Authenticator: owner, SecureCookies: true})
	notFound := performCSRFRequest(missingRouter, http.MethodPost,
		"/api/v1/owner/members/10000000-0000-4000-8000-0000000007ff/unlock", "", owner)
	if notFound.Code != http.StatusNotFound {
		t.Fatalf("unknown member unlock = %d, want 404", notFound.Code)
	}

	conflict := &memberAdministrationStub{unlockErr: identity.ErrMemberSelfAction}
	conflictRouter := NewRouter(Dependencies{Members: conflict, Authenticator: owner, SecureCookies: true})
	selfAction := performCSRFRequest(conflictRouter, http.MethodPost,
		"/api/v1/owner/members/10000000-0000-4000-8000-000000000001/unlock", "", owner)
	if selfAction.Code != http.StatusConflict {
		t.Fatalf("owner self-unlock = %d, want 409", selfAction.Code)
	}
}

// principalStub authenticates one fixed session cookie as a specific role, so owner and
// member boundaries can be exercised through the real router middleware.
type principalStub struct {
	userID    string
	role      string
	sessionID string
}

func (stub principalStub) AuthenticateSession(_ context.Context, token string) (auth.Principal, error) {
	if token != "active-session-secret" {
		return auth.Principal{}, auth.ErrAuthenticationRequired
	}
	return auth.Principal{
		UserID: stub.userID, Role: stub.role, SessionID: stub.sessionID,
		VerifyCSRF: func(value string) bool { return value == "valid-csrf" },
	}, nil
}

func performAuthenticatedRequest(handler http.Handler, method, path, body string, _ principalStub) *httptest.ResponseRecorder {
	return performSessionRequest(handler, method, path, "", body)
}

func performCSRFRequest(handler http.Handler, method, path, body string, _ principalStub) *httptest.ResponseRecorder {
	return performSessionRequest(handler, method, path, "valid-csrf", body)
}
