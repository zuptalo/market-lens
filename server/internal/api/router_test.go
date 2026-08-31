package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/httpx"
)

type databaseStub struct{ err error }

func (d databaseStub) Ping(context.Context) error { return d.err }

func TestHealthAndReadiness(t *testing.T) {
	tests := []struct {
		name, path string
		dbErr      error
		want       int
	}{
		{name: "health", path: "/api/v1/health", want: http.StatusOK},
		{name: "ready", path: "/api/v1/ready", want: http.StatusOK},
		{name: "database down", path: "/api/v1/ready", dbErr: errors.New("down"), want: http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			NewRouter(Dependencies{Database: databaseStub{tt.dbErr}, Version: "test"}).ServeHTTP(recorder, request)
			if recorder.Code != tt.want {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, tt.want, recorder.Body.String())
			}
		})
	}
}

func TestUnknownAPIRouteReturnsJSON404(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := authenticatedAPIRequest(http.MethodGet, "/api/v1/unknown")
	NewRouter(authenticatedDependencies(Dependencies{})).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound || recorder.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("unexpected response: status=%d content-type=%q", recorder.Code, recorder.Header().Get("Content-Type"))
	}
}

func authenticatedDependencies(dependencies Dependencies) Dependencies {
	dependencies.Authenticator = sessionAuthenticatorFunc(func(_ context.Context, token string) (auth.Principal, error) {
		if token != "test-session" {
			return auth.Principal{}, auth.ErrAuthenticationRequired
		}
		return auth.Principal{
			UserID: "10000000-0000-4000-8000-000000000001", Role: "owner",
			SessionID: "20000000-0000-4000-8000-000000000001", VerifyCSRF: func(string) bool { return true },
		}, nil
	})
	return dependencies
}

func authenticatedAPIRequest(method, path string) *http.Request {
	request := httptest.NewRequest(method, path, nil)
	request.AddCookie(&http.Cookie{Name: httpx.SessionCookieName, Value: "test-session"})
	return request
}
