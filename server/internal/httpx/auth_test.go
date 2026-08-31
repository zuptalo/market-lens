package httpx

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPrincipalDoesNotCarryPlaintextCSRFMaterial(t *testing.T) {
	principalType := reflect.TypeOf(Principal{})
	if field, ok := principalType.FieldByName("CSRFToken"); ok {
		t.Fatalf("principal exposes plaintext CSRF field %s", field.Name)
	}
	if _, ok := principalType.FieldByName("VerifyCSRF"); !ok {
		t.Fatal("principal has no CSRF verifier")
	}
}

func TestPrincipalContextAndAuthenticationMiddleware(t *testing.T) {
	principal := Principal{UserID: "user-1", Role: "owner", SessionID: "session-1", VerifyCSRF: func(value string) bool { return value == "csrf-secret" }}
	request := WithPrincipal(httptest.NewRequest(http.MethodGet, "/private", nil), principal)
	if got, ok := PrincipalFromContext(request); !ok || got.UserID != principal.UserID || got.Role != principal.Role ||
		got.SessionID != principal.SessionID || got.VerifyCSRF == nil {
		t.Fatalf("principal = %#v, %v; want authenticated owner principal", got, ok)
	}

	protected := RequirePrincipal(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		got, ok := PrincipalFromContext(request)
		if !ok || got.UserID != "user-1" {
			t.Fatal("authenticated principal was not forwarded")
		}
		writer.WriteHeader(http.StatusNoContent)
	}))

	authorized := httptest.NewRecorder()
	protected.ServeHTTP(authorized, request)
	if authorized.Code != http.StatusNoContent {
		t.Fatalf("authorized status = %d", authorized.Code)
	}

	anonymous := httptest.NewRecorder()
	protected.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, "/private", nil))
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, want 401", anonymous.Code)
	}
	if body := anonymous.Body.String(); !strings.Contains(body, "authentication_required") || strings.Contains(strings.ToLower(body), "session") {
		t.Fatalf("authentication error is not generic: %s", body)
	}
}

func TestCSRFMiddlewareRequiresMatchingHeaderOnlyForUnsafeMethods(t *testing.T) {
	called := 0
	handler := RequireCSRF(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		called++
		writer.WriteHeader(http.StatusNoContent)
	}))
	principal := Principal{UserID: "user-1", SessionID: "session-1", VerifyCSRF: func(value string) bool { return value == "expected-csrf" }}

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, WithPrincipal(httptest.NewRequest(method, "/resource", nil), principal))
		if response.Code != http.StatusNoContent {
			t.Fatalf("safe %s status = %d", method, response.Code)
		}
	}

	for _, token := range []string{"", "wrong"} {
		request := WithPrincipal(httptest.NewRequest(http.MethodPost, "/resource", nil), principal)
		request.Header.Set("X-CSRF-Token", token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "csrf_rejected") {
			t.Fatalf("CSRF rejection = %d %s", response.Code, response.Body.String())
		}
	}

	request := WithPrincipal(httptest.NewRequest(http.MethodPost, "/resource", nil), principal)
	request.Header.Set("X-CSRF-Token", "expected-csrf")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || called != 4 {
		t.Fatalf("valid CSRF status=%d called=%d", response.Code, called)
	}
}

func TestSessionCookiesAreSecureHostOnlyAndClearable(t *testing.T) {
	expires := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	response := httptest.NewRecorder()
	SetSessionCookie(response, "opaque-session-token", true, expires)
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %#v", cookies)
	}
	cookie := cookies[0]
	if cookie.Name != SessionCookieName || cookie.Value != "opaque-session-token" || cookie.Path != "/" || cookie.Domain != "" || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || !cookie.Expires.Equal(expires) {
		t.Fatalf("unsafe session cookie: %#v", cookie)
	}

	cleared := httptest.NewRecorder()
	ClearSessionCookie(cleared, true)
	clearCookie := cleared.Result().Cookies()[0]
	if clearCookie.Name != SessionCookieName || clearCookie.Value != "" || clearCookie.MaxAge >= 0 || !clearCookie.HttpOnly || !clearCookie.Secure {
		t.Fatalf("unsafe cleared cookie: %#v", clearCookie)
	}
}
