package eodhd

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"market-lens/server/internal/marketdata"
)

func TestCredentialValidatorProvesTokenAndTenYearNonUSEODEntitlement(t *testing.T) {
	const token = "setup-eodhd-secret"
	requested := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requested[request.URL.Path]++
		if request.URL.Query().Get("api_token") != token || request.URL.Query().Get("fmt") != "json" {
			t.Error("credential validation omitted common query")
		}
		switch request.URL.Path {
		case "/user":
			_, _ = writer.Write([]byte(`{"subscriptionType":"yearly","dailyRateLimit":100000}`))
		case "/eod/ERIC-B.ST":
			if request.URL.Query().Get("from") != "2016-08-30" || request.URL.Query().Get("to") != "2016-09-13" {
				t.Errorf("entitlement range = %s..%s", request.URL.Query().Get("from"), request.URL.Query().Get("to"))
			}
			_, _ = writer.Write([]byte(`[{"date":"2016-08-30","close":50}]`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	validator, err := NewCredentialValidator(CredentialValidatorConfig{
		BaseURL: server.URL, HTTPClient: server.Client(), Timeout: time.Second,
		Now: func() time.Time { return time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := validator.ValidateCredential(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	if requested["/user"] != 1 || requested["/eod/ERIC-B.ST"] != 1 {
		t.Fatalf("validation requests = %#v", requested)
	}
}

func TestCredentialValidatorClassifiesInvalidEntitlementAndTimeoutWithoutSecrets(t *testing.T) {
	const token = "invalid-eodhd-secret-never-log"
	for _, tt := range []struct {
		name    string
		handler http.HandlerFunc
		code    string
	}{
		{name: "invalid token", code: "provider_authentication", handler: func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusUnauthorized)
		}},
		{name: "missing historical entitlement", code: "provider_entitlement", handler: func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path == "/user" {
				_, _ = writer.Write([]byte(`{"subscriptionType":"monthly","dailyRateLimit":100000}`))
				return
			}
			writer.WriteHeader(http.StatusForbidden)
		}},
		{name: "timeout", code: "provider_timeout", handler: func(_ http.ResponseWriter, request *http.Request) {
			<-request.Context().Done()
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()
			validator, err := NewCredentialValidator(CredentialValidatorConfig{
				BaseURL: server.URL, HTTPClient: server.Client(), Timeout: 20 * time.Millisecond,
				Now: func() time.Time { return time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC) },
			})
			if err != nil {
				t.Fatal(err)
			}
			err = validator.ValidateCredential(context.Background(), token)
			var providerError *marketdata.ProviderError
			if !errors.As(err, &providerError) || providerError.Code != tt.code {
				t.Fatalf("validation error = %#v, want %s", err, tt.code)
			}
			if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), server.URL) {
				t.Fatalf("validation error disclosed secret/provider URL: %v", err)
			}
		})
	}
}
