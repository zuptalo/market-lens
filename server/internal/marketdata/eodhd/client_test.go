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

func TestResolveMapsNordicExchangeQualifiedInstrument(t *testing.T) {
	const token = "test-provider-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/exchange-symbol-list/ST" {
			t.Errorf("path = %q", r.URL.Path)
		}
		assertCommonQuery(t, r, token)
		_, _ = w.Write([]byte(`[
			{"Code":"OTHER","Name":"Other AB","Country":"Sweden","Exchange":"Stockholm Exchange","Currency":"SEK","Type":"Common Stock","Isin":"SE0000000000"},
			{"Code":"ERIC-B","Name":"Telefon AB L.M. Ericsson","Country":"Sweden","Exchange":"Stockholm Exchange","Currency":"SEK","Type":"Common Stock","Isin":"SE0000108656"}
		]`))
	}))
	defer server.Close()

	client := newTestClient(t, server, token, time.Second)
	resolved, err := client.Resolve(context.Background(), marketdata.ResolveRequest{ProviderSymbol: "ERIC-B.ST", MIC: "XSTO"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ProviderSymbol != "ERIC-B.ST" || resolved.Ticker != "ERIC-B" || resolved.MIC != "XSTO" || resolved.ISIN != "SE0000108656" || resolved.Currency != "SEK" || resolved.Timezone != "Europe/Stockholm" {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestDailyMapsExactDecimalsDatesAdjustedCloseAndActions(t *testing.T) {
	const token = "test-provider-secret"
	requested := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested[r.URL.Path]++
		assertCommonQuery(t, r, token)
		if got := r.URL.Query().Get("from"); got != "2024-03-28" {
			t.Errorf("from = %q", got)
		}
		if got := r.URL.Query().Get("to"); got != "2024-04-15" {
			t.Errorf("to = %q", got)
		}
		switch r.URL.Path {
		case "/eod/NORD.ST":
			if got := r.URL.Query().Get("period"); got != "d" {
				t.Errorf("period = %q", got)
			}
			_, _ = w.Write([]byte(`[
				{"date":"2024-03-28","open":99.25000000,"high":101.00000000,"low":98.75000000,"close":100.50000000,"adjusted_close":50.25000000,"volume":125000},
				{"date":"2024-04-02","open":50.50000000,"high":52.00000000,"low":50.00000000,"close":51.75000000,"adjusted_close":51.75000000,"volume":180000}
			]`))
		case "/splits/NORD.ST":
			_, _ = w.Write([]byte(`[{"date":"2024-04-02","split":"2.00000000"}]`))
		case "/div/NORD.ST":
			_, _ = w.Write([]byte(`[{"date":"2024-04-15","value":1.25000000,"currency":"SEK"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server, token, time.Second)
	page, err := client.Daily(context.Background(), marketdata.DailyRequest{
		ProviderSymbol: "NORD.ST",
		From:           session(t, "2024-03-28"),
		To:             session(t, "2024-04-15"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Bars) != 2 || page.Bars[0].Open.String() != "99.25" || page.Bars[0].Close.String() != "100.5" || page.Bars[0].AdjustedClose == nil || page.Bars[0].AdjustedClose.String() != "50.25" || page.Bars[0].Volume != 125000 {
		t.Fatalf("bars = %#v", page.Bars)
	}
	if len(page.Actions) != 2 || page.Actions[0].Type != marketdata.ActionSplit || page.Actions[0].Ratio == nil || page.Actions[0].Ratio.String() != "2" || page.Actions[1].Type != marketdata.ActionDividend || page.Actions[1].Amount == nil || page.Actions[1].Amount.String() != "1.25" || page.Actions[1].Currency != "SEK" {
		t.Fatalf("actions = %#v", page.Actions)
	}
	for _, path := range []string{"/eod/NORD.ST", "/splits/NORD.ST", "/div/NORD.ST"} {
		if requested[path] != 1 {
			t.Errorf("requests[%q] = %d", path, requested[path])
		}
	}
}

func TestDailyHonorsTimeoutAndSanitizesProviderFailures(t *testing.T) {
	const token = "provider-token-never-expose"
	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}))
		defer server.Close()
		client := newTestClient(t, server, token, 20*time.Millisecond)
		_, err := client.Daily(context.Background(), marketdata.DailyRequest{ProviderSymbol: "NORD.ST", From: session(t, "2024-03-28"), To: session(t, "2024-04-02")})
		assertSafeProviderError(t, err, "provider_timeout", token, server.URL)
	})

	t.Run("authentication", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"bad token ` + token + `"}`))
		}))
		defer server.Close()
		client := newTestClient(t, server, token, time.Second)
		_, err := client.Daily(context.Background(), marketdata.DailyRequest{ProviderSymbol: "NORD.ST", From: session(t, "2024-03-28"), To: session(t, "2024-04-02")})
		assertSafeProviderError(t, err, "provider_authentication", token, server.URL)
	})
}

func newTestClient(t *testing.T, server *httptest.Server, token string, timeout time.Duration) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: server.URL, APIToken: token, HTTPClient: &http.Client{Timeout: timeout}})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func assertCommonQuery(t *testing.T, r *http.Request, token string) {
	t.Helper()
	if got := r.URL.Query().Get("api_token"); got != token {
		t.Errorf("api_token mismatch")
	}
	if got := r.URL.Query().Get("fmt"); got != "json" {
		t.Errorf("fmt = %q", got)
	}
}

func assertSafeProviderError(t *testing.T, err error, code string, forbidden ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected provider error")
	}
	var providerErr *marketdata.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != code {
		t.Fatalf("error = %#v", err)
	}
	for _, value := range forbidden {
		if value != "" && strings.Contains(err.Error(), value) {
			t.Fatalf("error leaked %q: %v", value, err)
		}
	}
}

func session(t *testing.T, value string) marketdata.SessionDate {
	t.Helper()
	result, err := marketdata.ParseSessionDate(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
