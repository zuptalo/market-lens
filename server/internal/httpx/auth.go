package httpx

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"market-lens/server/internal/authorization"
)

const SessionCookieName = "__Host-market_lens_session"

// CSRFCookieName holds the double-submit token. Unlike the session cookie it is deliberately
// readable by same-origin script: the token proves a request came from this application's own
// code rather than a cross-site form post, so a reloaded page must be able to recover it.
const CSRFCookieName = "__Host-market_lens_csrf"

type Principal struct {
	UserID     string
	Role       string
	SessionID  string
	VerifyCSRF func(string) bool
}

type principalContextKey struct{}

func WithPrincipal(request *http.Request, principal Principal) *http.Request {
	return request.WithContext(context.WithValue(request.Context(), principalContextKey{}, principal))
}

func PrincipalFromContext(request *http.Request) (Principal, bool) {
	principal, ok := request.Context().Value(principalContextKey{}).(Principal)
	return principal, ok && principal.UserID != "" && principal.SessionID != ""
}

func RequirePrincipal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if _, ok := PrincipalFromContext(request); !ok {
			// A person typing an address is not a script calling an API. Answering a page
			// request with a JSON error is a dead end, so navigation is sent to sign-in
			// while every data request still receives a refusal it can act on.
			if destination, ok := signInDestination(request); ok {
				http.Redirect(writer, request, destination, http.StatusFound)
				return
			}
			writeAuthError(writer, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
			return
		}
		next.ServeHTTP(writer, request)
	})
}

// signInDestination reports where an unauthenticated page request should be sent. It answers
// only for browser navigation: an API path is never redirected, whatever it claims to accept,
// so an Accept header cannot turn a refusal into a redirect.
func signInDestination(request *http.Request) (string, bool) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		return "", false
	}
	if strings.HasPrefix(request.URL.Path, "/api/") {
		return "", false
	}
	if !strings.Contains(request.Header.Get("Accept"), "text/html") {
		return "", false
	}
	target := "/login"
	// Only a bare local path is ever reflected back, so the redirect cannot be pointed at
	// another site by asking for one.
	if path := request.URL.EscapedPath(); localPath(path) {
		target += "?redirect=" + url.QueryEscape(path)
	}
	return target, true
}

// localPath accepts only a single-slash absolute path, which excludes "//host" and any
// scheme-bearing value a browser would treat as another origin.
func localPath(path string) bool {
	return strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "//") && !strings.Contains(path, ":")
}

func RequireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if isSafeMethod(request.Method) {
			next.ServeHTTP(writer, request)
			return
		}
		principal, ok := PrincipalFromContext(request)
		provided := request.Header.Get("X-CSRF-Token")
		if !ok || principal.VerifyCSRF == nil || provided == "" || !principal.VerifyCSRF(provided) {
			writeAuthError(writer, http.StatusForbidden, "csrf_rejected", "Request verification failed.")
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func SetSessionCookie(writer http.ResponseWriter, token string, secure bool, expires time.Time) {
	http.SetCookie(writer, &http.Cookie{
		Name: SessionCookieName, Value: token, Path: "/", Expires: expires,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
}

// SetCSRFCookie publishes the double-submit token for same-origin script.
func SetCSRFCookie(writer http.ResponseWriter, token string, secure bool, expires time.Time) {
	http.SetCookie(writer, &http.Cookie{
		Name: CSRFCookieName, Value: token, Path: "/", Expires: expires,
		HttpOnly: false, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
}

// ClearCSRFCookie removes the double-submit token alongside the session.
func ClearCSRFCookie(writer http.ResponseWriter, secure bool) {
	http.SetCookie(writer, &http.Cookie{
		Name: CSRFCookieName, Path: "/", Expires: time.Unix(1, 0).UTC(), MaxAge: -1,
		HttpOnly: false, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
}

func ClearSessionCookie(writer http.ResponseWriter, secure bool) {
	http.SetCookie(writer, &http.Cookie{
		Name: SessionCookieName, Path: "/", Expires: time.Unix(1, 0).UTC(), MaxAge: -1,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

func writeAuthError(writer http.ResponseWriter, status int, code, message string) {
	JSON(writer, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

// RequireOwner gates instance administration. It is a boundary in its own right rather than a
// check each handler remembers to make, because a route that forgets it silently exposes every
// other member's account and security metadata.
func RequireOwner(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		principal, ok := PrincipalFromContext(request)
		if !ok {
			writeAuthError(writer, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
			return
		}
		decision := authorization.Authorize(authorization.Principal{
			UserID: principal.UserID, Role: authorization.Role(principal.Role), Authenticated: true,
		}, authorization.Resource{Scope: authorization.ScopeOwner})
		if !decision.Allowed {
			// The refusal says nothing about what lies behind it.
			writeAuthError(writer, http.StatusForbidden, "forbidden", "Owner authorization is required.")
			return
		}
		next.ServeHTTP(writer, request)
	})
}
