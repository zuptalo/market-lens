package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/identity"
)

func ownerPrincipal() principalStub {
	return principalStub{
		userID: "10000000-0000-4000-8000-000000000001", role: "owner",
		sessionID: "20000000-0000-4000-8000-000000000001",
	}
}

func sampleInvitation(now time.Time) identity.Invitation {
	return identity.Invitation{
		ID: "70000000-0000-4000-8000-000000000001", Email: "invitee@example.com",
		State: identity.InvitationPending, ExpiresAt: now.Add(identity.InvitationTTL),
		DeliveryState: identity.DeliverySent, ResendCount: 0, CreatedAt: now,
	}
}

func TestOwnerInvitationLifecycleHTTPContract(t *testing.T) {
	now := time.Date(2026, time.August, 30, 10, 0, 0, 0, time.UTC)
	invitations := &invitationAdministrationStub{
		created: sampleInvitation(now),
		page:    identity.InvitationPage{Items: []identity.Invitation{sampleInvitation(now)}},
	}
	owner := ownerPrincipal()
	router := NewRouter(Dependencies{Invitations: invitations, Authenticator: owner, SecureCookies: true})

	listing := performAuthenticatedRequest(router, http.MethodGet, "/api/v1/owner/invitations", "", owner)
	if listing.Code != http.StatusOK {
		t.Fatalf("invitation listing = %d %s", listing.Code, listing.Body.String())
	}
	var page struct {
		Items []struct {
			ID            string  `json:"id"`
			Email         string  `json:"email"`
			State         string  `json:"state"`
			DeliveryState string  `json:"delivery_state"`
			DeliveryError *string `json:"delivery_error"`
			ResendCount   int     `json:"resend_count"`
		} `json:"items"`
	}
	if err := json.Unmarshal(listing.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].State != "pending" || page.Items[0].DeliveryState != "sent" {
		t.Fatalf("invitation page = %s", listing.Body.String())
	}
	// A capability must never appear in any response.
	if strings.Contains(strings.ToLower(listing.Body.String()), "capability") ||
		strings.Contains(listing.Body.String(), "token") {
		t.Fatalf("invitation listing disclosed capability material: %s", listing.Body.String())
	}

	// Creation is a state change and requires CSRF.
	withoutCSRF := performAuthenticatedRequest(router, http.MethodPost, "/api/v1/owner/invitations",
		`{"email":"invitee@example.com"}`, owner)
	if withoutCSRF.Code != http.StatusForbidden {
		t.Fatalf("create without CSRF = %d, want 403", withoutCSRF.Code)
	}
	if invitations.createdFor != "" {
		t.Fatal("a CSRF-less invitation reached the service")
	}

	created := performCSRFRequest(router, http.MethodPost, "/api/v1/owner/invitations",
		`{"email":"invitee@example.com"}`, owner)
	if created.Code != http.StatusCreated || invitations.createdFor != "invitee@example.com" {
		t.Fatalf("create = %d %s (service saw %q)", created.Code, created.Body.String(), invitations.createdFor)
	}
	if invitations.actor.Role != identity.RoleOwner || invitations.actor.UserID != owner.userID {
		t.Fatalf("create actor = %#v", invitations.actor)
	}

	resent := performCSRFRequest(router, http.MethodPost,
		"/api/v1/owner/invitations/70000000-0000-4000-8000-000000000001/resend", "", owner)
	if resent.Code != http.StatusOK || invitations.resent != "70000000-0000-4000-8000-000000000001" {
		t.Fatalf("resend = %d %s", resent.Code, resent.Body.String())
	}

	revoked := performCSRFRequest(router, http.MethodDelete,
		"/api/v1/owner/invitations/70000000-0000-4000-8000-000000000001", "", owner)
	if revoked.Code != http.StatusNoContent || invitations.revoked != "70000000-0000-4000-8000-000000000001" {
		t.Fatalf("revoke = %d %s", revoked.Code, revoked.Body.String())
	}
}

func TestInvitationAdministrationDeniesNonOwnersAndReportsConflicts(t *testing.T) {
	member := principalStub{
		userID: "10000000-0000-4000-8000-000000000601", role: "member",
		sessionID: "20000000-0000-4000-8000-000000000601",
	}
	denied := &invitationAdministrationStub{createErr: identity.ErrOwnerRequired}
	deniedRouter := NewRouter(Dependencies{Invitations: denied, Authenticator: member, SecureCookies: true})

	if response := performAuthenticatedRequest(deniedRouter, http.MethodGet, "/api/v1/owner/invitations", "", member); response.Code != http.StatusForbidden {
		t.Fatalf("member invitation listing = %d, want 403", response.Code)
	}
	if response := performCSRFRequest(deniedRouter, http.MethodPost, "/api/v1/owner/invitations",
		`{"email":"x@example.com"}`, member); response.Code != http.StatusForbidden {
		t.Fatalf("member invitation creation = %d, want 403", response.Code)
	}

	owner := ownerPrincipal()
	conflict := &invitationAdministrationStub{createErr: identity.ErrInvitationConflict}
	conflictRouter := NewRouter(Dependencies{Invitations: conflict, Authenticator: owner, SecureCookies: true})
	if response := performCSRFRequest(conflictRouter, http.MethodPost, "/api/v1/owner/invitations",
		`{"email":"existing@example.com"}`, owner); response.Code != http.StatusConflict {
		t.Fatalf("duplicate invitation = %d, want 409", response.Code)
	}

	missing := &invitationAdministrationStub{resendErr: identity.ErrInvitationUnavailable}
	missingRouter := NewRouter(Dependencies{Invitations: missing, Authenticator: owner, SecureCookies: true})
	if response := performCSRFRequest(missingRouter, http.MethodPost,
		"/api/v1/owner/invitations/70000000-0000-4000-8000-0000000000ff/resend", "", owner); response.Code != http.StatusNotFound {
		t.Fatalf("unknown invitation resend = %d, want 404", response.Code)
	}
}

func TestInvitationAcceptanceHTTPContractIsPasswordlessAndUniform(t *testing.T) {
	now := time.Date(2026, time.August, 30, 10, 0, 0, 0, time.UTC)
	absoluteExpiry := now.Add(30 * 24 * time.Hour)
	verifiedAt := now
	invitations := &invitationAdministrationStub{accepted: identity.BootstrapResult{
		User: identity.User{
			ID: "11111111-1111-4111-8111-111111111111", Email: "invitee@example.com",
			DisplayName: "Invitee", Role: identity.RoleMember, Status: identity.StatusActive,
			EmailVerifiedAt: &verifiedAt,
		},
		Session:      auth.SessionSummary{ID: "21000000-0000-4000-8000-000000000001", AbsoluteExpiresAt: absoluteExpiry},
		SessionToken: "invite-session-secret", CSRFToken: "invite-csrf-secret",
	}}
	router := NewRouter(Dependencies{Invitations: invitations, SecureCookies: true})

	response := performAuthRequest(router, http.MethodPost, "/api/v1/auth/invitations/accept",
		`{"capability":"invitation-capability-secret","email":"invitee@example.com","display_name":"Invitee"}`)
	assertAuthenticatedHTTPResponse(t, response, http.StatusCreated, "invite-csrf-secret", "invite-session-secret",
		absoluteExpiry, "member")
	if invitations.acceptedAs.Capability != "invitation-capability-secret" ||
		invitations.acceptedAs.Origin == "" || invitations.acceptedAs.DeviceLabel == "" {
		t.Fatalf("acceptance request = %#v", invitations.acceptedAs)
	}

	// Acceptance never takes a password.
	withPassword := performAuthRequest(router, http.MethodPost, "/api/v1/auth/invitations/accept",
		`{"capability":"c","email":"invitee@example.com","display_name":"Invitee","password":"anything"}`)
	if withPassword.Code != http.StatusBadRequest {
		t.Fatalf("password field response = %d, want 400", withPassword.Code)
	}

	// Every unusable capability is reported identically so invitations cannot be probed.
	unusable := &invitationAdministrationStub{acceptErr: identity.ErrInvitationUnavailable}
	unusableRouter := NewRouter(Dependencies{Invitations: unusable, SecureCookies: true})
	var bodies []string
	for _, body := range []string{
		`{"capability":"expired-capability","email":"invitee@example.com","display_name":"Invitee"}`,
		`{"capability":"revoked-capability","email":"invitee@example.com","display_name":"Invitee"}`,
		`{"capability":"unknown-capability","email":"someone@example.com","display_name":"Someone"}`,
	} {
		response := performAuthRequest(unusableRouter, http.MethodPost, "/api/v1/auth/invitations/accept", body)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("unusable capability status = %d, want 400", response.Code)
		}
		if cookie := response.Header().Get("Set-Cookie"); cookie != "" {
			t.Fatalf("a rejected acceptance set a cookie: %s", cookie)
		}
		bodies = append(bodies, response.Body.String())
	}
	for _, body := range bodies[1:] {
		if body != bodies[0] {
			t.Fatalf("unusable capability bodies differ:\n%s\n%s", bodies[0], body)
		}
	}
}

func TestMemberStatusHTTPContractRequiresOwnerAndCSRF(t *testing.T) {
	now := time.Date(2026, time.August, 30, 10, 0, 0, 0, time.UTC)
	members := &memberAdministrationStub{page: identity.MemberPage{Members: []identity.Member{{
		ID: "10000000-0000-4000-8000-000000000601", Email: "member@example.com", DisplayName: "Ada Member",
		Status: identity.StatusDeactivated, LoginState: identity.MemberLoginAvailable, CreatedAt: now,
	}}}}
	owner := ownerPrincipal()
	router := NewRouter(Dependencies{Members: members, Authenticator: owner, SecureCookies: true})

	withoutCSRF := performAuthenticatedRequest(router, http.MethodPatch,
		"/api/v1/owner/members/10000000-0000-4000-8000-000000000601/status", `{"status":"deactivated"}`, owner)
	if withoutCSRF.Code != http.StatusForbidden {
		t.Fatalf("status change without CSRF = %d, want 403", withoutCSRF.Code)
	}
	if members.statusMember != "" {
		t.Fatal("a CSRF-less status change reached the service")
	}

	changed := performCSRFRequest(router, http.MethodPatch,
		"/api/v1/owner/members/10000000-0000-4000-8000-000000000601/status", `{"status":"deactivated"}`, owner)
	if changed.Code != http.StatusOK {
		t.Fatalf("status change = %d %s", changed.Code, changed.Body.String())
	}
	if members.statusValue != identity.StatusDeactivated ||
		members.statusMember != "10000000-0000-4000-8000-000000000601" {
		t.Fatalf("status change reached service as %q %q", members.statusMember, members.statusValue)
	}

	// An unsupported status is rejected before reaching the service.
	members.statusMember = ""
	invalid := performCSRFRequest(router, http.MethodPatch,
		"/api/v1/owner/members/10000000-0000-4000-8000-000000000601/status", `{"status":"owner"}`, owner)
	if invalid.Code != http.StatusBadRequest || members.statusMember != "" {
		t.Fatalf("invalid status = %d, service saw %q", invalid.Code, members.statusMember)
	}
}
