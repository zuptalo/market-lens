package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/httpx"
)

// The two facts the screen shows, on the wire. A session that has no recorded address sends null
// rather than an empty string or a placeholder: the client says "Not recorded" in words, and a
// value that could be mistaken for an address must never be invented (FR-003).
func TestSessionListReportsTheAddressItWasLastSeenFrom(t *testing.T) {
	now := time.Date(2026, time.September, 19, 10, 0, 0, 0, time.UTC)
	userID := "10000000-0000-4000-8000-000000000001"
	currentSessionID := "20000000-0000-4000-8000-000000000001"
	olderSessionID := "20000000-0000-4000-8000-000000000002"
	authentication := &ownerAuthenticationStub{
		account: auth.Account{
			ID: userID, Email: "owner@example.com", DisplayName: "Market Owner",
			Role: "owner", Status: "active", EmailVerifiedAt: now,
		},
		sessions: []auth.SessionSummary{
			{
				ID: currentSessionID, Current: true, DeviceLabel: "Current browser",
				CreatedAt: now, LastSeenAt: now, IdleExpiresAt: now.Add(8 * time.Hour),
				AbsoluteExpiresAt: now.Add(30 * 24 * time.Hour),
				CreatedFrom:       netip.MustParseAddr("203.0.113.12"),
				LastSeenFrom:      netip.MustParseAddr("198.51.100.4"),
			},
			// The session that predates the feature: no address, and none invented for it.
			{
				ID: olderSessionID, DeviceLabel: "Older browser", CreatedAt: now, LastSeenAt: now,
				IdleExpiresAt: now.Add(8 * time.Hour), AbsoluteExpiresAt: now.Add(30 * 24 * time.Hour),
			},
		},
	}
	authenticator := sessionAuthenticatorFunc(func(_ context.Context, token string, _ netip.Addr) (auth.Principal, error) {
		if token != "active-session-secret" {
			return auth.Principal{}, auth.ErrAuthenticationRequired
		}
		return auth.Principal{UserID: userID, Role: "owner", SessionID: currentSessionID,
			VerifyCSRF: func(value string) bool { return value == "valid-csrf" }}, nil
	})
	router := NewRouter(Dependencies{Authenticator: authenticator, Authentication: authentication, SecureCookies: true})

	response := performSessionRequest(router, http.MethodGet, "/api/v1/account/sessions", "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("sessions response = %d %s", response.Code, response.Body.String())
	}
	var body struct {
		Items []struct {
			ID           string  `json:"id"`
			CreatedFrom  *string `json:"created_from"`
			LastSeenFrom *string `json:"last_seen_from"`
		} `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (%s)", err, response.Body.String())
	}
	if len(body.Items) != 2 {
		t.Fatalf("expected two sessions, got %d", len(body.Items))
	}
	current := body.Items[0]
	if current.ID != currentSessionID {
		t.Fatalf("unexpected first session %s", current.ID)
	}
	if current.LastSeenFrom == nil || *current.LastSeenFrom != "198.51.100.4" {
		t.Fatalf("last_seen_from = %v, want the address of the most recent request", current.LastSeenFrom)
	}
	if current.CreatedFrom == nil || *current.CreatedFrom != "203.0.113.12" {
		t.Fatalf("created_from = %v", current.CreatedFrom)
	}
	if body.Items[1].LastSeenFrom != nil || body.Items[1].CreatedFrom != nil {
		t.Fatalf("an unrecorded address must be null, got %v", body.Items[1])
	}
}

// Behind the ingress the peer is Traefik. Whether the product records the client or the proxy is
// decided by one configured list, and a request that forges the header from an untrusted peer must
// change nothing (D1, FR-005).
func TestAuthenticatedRequestRecordsTheResolvedClientAddress(t *testing.T) {
	userID := "10000000-0000-4000-8000-000000000001"
	sessionID := "20000000-0000-4000-8000-000000000001"
	seen := make(chan netip.Addr, 4)
	authenticator := sessionAuthenticatorFunc(func(_ context.Context, token string, address netip.Addr) (auth.Principal, error) {
		seen <- address
		return auth.Principal{UserID: userID, Role: "owner", SessionID: sessionID,
			VerifyCSRF: func(string) bool { return true }}, nil
	})
	trusted, err := httpx.ParseTrustedProxies([]string{"10.42.0.0/16"})
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Dependencies{Authenticator: authenticator, SecureCookies: true, TrustedProxies: trusted})

	call := func(remote, forwarded string) netip.Addr {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/account", nil)
		request.AddCookie(&http.Cookie{Name: httpx.SessionCookieName, Value: "active-session-secret"})
		request.RemoteAddr = remote
		if forwarded != "" {
			request.Header.Set("X-Forwarded-For", forwarded)
		}
		router.ServeHTTP(httptest.NewRecorder(), request)
		select {
		case address := <-seen:
			return address
		case <-time.After(time.Second):
			t.Fatal("the authenticator was never called")
			return netip.Addr{}
		}
	}

	if address := call("10.42.0.31:41234", "203.0.113.12"); address.String() != "203.0.113.12" {
		t.Fatalf("behind the trusted ingress the client is the client, got %s", address)
	}
	if address := call("198.51.100.9:443", "203.0.113.12"); address.String() != "198.51.100.9" {
		t.Fatalf("a forged header from an untrusted peer must change nothing, got %s", address)
	}
}

// Nothing about a session's address may travel anywhere but the owner's own snapshot.
func TestSessionResponseCarriesNoSecretsAlongsideTheAddress(t *testing.T) {
	now := time.Date(2026, time.September, 19, 10, 0, 0, 0, time.UTC)
	userID := "10000000-0000-4000-8000-000000000001"
	sessionID := "20000000-0000-4000-8000-000000000001"
	authentication := &ownerAuthenticationStub{
		account: auth.Account{ID: userID, Email: "owner@example.com", DisplayName: "Market Owner",
			Role: "owner", Status: "active", EmailVerifiedAt: now},
		sessions: []auth.SessionSummary{{
			ID: sessionID, Current: true, DeviceLabel: "Current browser", CreatedAt: now, LastSeenAt: now,
			IdleExpiresAt: now.Add(8 * time.Hour), AbsoluteExpiresAt: now.Add(30 * 24 * time.Hour),
			LastSeenFrom: netip.MustParseAddr("203.0.113.12"),
		}},
	}
	authenticator := sessionAuthenticatorFunc(func(_ context.Context, token string, _ netip.Addr) (auth.Principal, error) {
		return auth.Principal{UserID: userID, Role: "owner", SessionID: sessionID,
			VerifyCSRF: func(string) bool { return true }}, nil
	})
	router := NewRouter(Dependencies{Authenticator: authenticator, Authentication: authentication, SecureCookies: true})
	response := performSessionRequest(router, http.MethodGet, "/api/v1/account/sessions", "", "")
	for _, forbidden := range []string{"digest", "token", "origin_digest"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("session response disclosed %q: %s", forbidden, response.Body.String())
		}
	}
}
