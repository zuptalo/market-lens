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

// The API token is resolved for each request rather than captured once.
//
// A self-hosted installation stores its market-data key in the database and changes it from
// the settings screen. If the client held whatever the token was at construction, a rotated
// key would keep failing until somebody restarted the process — and the symptom would look
// like a broken importer rather than a stale token.
func TestTheTokenIsResolvedPerRequestSoARotatedKeyTakesEffect(t *testing.T) {
	seen := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query().Get("api_token"))
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	current := "first-key"
	client, err := New(Config{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
		TokenSource: func(context.Context) (string, error) {
			return current, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := client.Resolve(context.Background(),
		marketdata.ResolveRequest{ProviderSymbol: "ERIC-B.ST", MIC: "XSTO"}); err == nil || err != nil {
		// The response is deliberately empty; only the token sent matters here.
		_ = err
	}
	current = "rotated-key"
	if _, err := client.Resolve(context.Background(),
		marketdata.ResolveRequest{ProviderSymbol: "ERIC-B.ST", MIC: "XSTO"}); err == nil || err != nil {
		_ = err
	}

	if len(seen) != 2 || seen[0] != "first-key" || seen[1] != "rotated-key" {
		t.Fatalf("tokens sent = %#v; the second request must carry the rotated key", seen)
	}
}

// A token source that cannot answer must fail the request rather than send an empty token to
// the provider as though it were real.
func TestAnUnavailableTokenFailsTheRequestWithoutCallingTheProvider(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer server.Close()

	client, err := New(Config{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
		TokenSource: func(context.Context) (string, error) {
			return "", errors.New("market-data credentials are not available")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Resolve(context.Background(),
		marketdata.ResolveRequest{ProviderSymbol: "ERIC-B.ST", MIC: "XSTO"}); err == nil {
		t.Fatal("an unavailable token produced no error")
	}
	if called {
		t.Fatal("the provider was called without a token")
	}
}

func TestAClientNeedsEitherATokenOrASourceForOne(t *testing.T) {
	if _, err := New(Config{BaseURL: "https://example.test"}); err == nil {
		t.Fatal("a client was built with no way to obtain a token")
	}
}

// EODHD reports a split as a decimal fraction: "2.000000/1.000000".
//
// Go's big.Rat only accepts a fraction whose parts are integers, so every one of these was
// rejected — and because a bad action fails the whole page, every instrument that had ever
// split failed its entire import. In production that was 23 of 100 instruments, including
// Novo Nordisk, Ørsted and Sampo, each recording nothing at all.
func TestSplitRatiosArriveAsDecimalFractions(t *testing.T) {
	for _, testCase := range []struct {
		raw  string
		want string
	}{
		// Decimal is canonical: ParseDecimal normalizes trailing zeros, so 2.000000/1.000000
		// is the decimal 2 rather than the string "2.00000000".
		{"2.000000/1.000000", "2"},
		{"3.000000/2.000000", "1.5"},
		{"1.000000/10.000000", "0.1"}, // a reverse split
		{"2.5/1", "2.5"},
		{"2/1", "2"}, // the integer form must keep working
		{"4", "4"},   // and a bare multiplier has no fraction at all
	} {
		ratio, err := parseSplitRatio(testCase.raw)
		if err != nil {
			t.Errorf("%q was rejected: %v", testCase.raw, err)
			continue
		}
		if ratio.String() != testCase.want {
			t.Errorf("%q parsed to %s, expected %s", testCase.raw, ratio.String(), testCase.want)
		}
	}
}

func TestAnUnusableSplitRatioIsStillRejected(t *testing.T) {
	for _, raw := range []string{
		"", "not/a/ratio", "abc/1.0", "1.0/xyz",
		"1.000000/0.000000", // dividing by zero is not a split
		"-2.000000/1.000000",
		"0.000000/1.000000",
	} {
		if ratio, err := parseSplitRatio(raw); err == nil {
			t.Errorf("%q was accepted as ratio %s", raw, ratio.String())
		}
	}
}

// Listing an exchange's instruments is what makes a stale symbol diagnosable: the catalog
// carries the ISIN, which does not change when a ticker does, so a renamed company can be
// found again by the identifier we already hold.
func TestListInstrumentsReturnsTheExchangeCatalogWithISINs(t *testing.T) {
	const token = "test-provider-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/exchange-symbol-list/HE" {
			t.Errorf("path = %q", r.URL.Path)
		}
		assertCommonQuery(t, r, token)
		_, _ = w.Write([]byte(`[
			{"Code":"METSO","Name":"Metso Oyj","Country":"Finland","Exchange":"Helsinki","Currency":"EUR","Type":"Common Stock","Isin":"FI0009014575"},
			{"Code":"NOKIA","Name":"Nokia Oyj","Country":"Finland","Exchange":"Helsinki","Currency":"EUR","Type":"Common Stock","Isin":"FI0009000681"}
		]`))
	}))
	defer server.Close()

	catalog, err := newTestClient(t, server, token, time.Second).ListInstruments(context.Background(), "XHEL")
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) != 2 {
		t.Fatalf("catalog held %d entries", len(catalog))
	}
	metso := catalog[0]
	if metso.ProviderSymbol != "METSO.HE" || metso.ISIN != "FI0009014575" ||
		metso.Ticker != "METSO" || metso.Name != "Metso Oyj" || metso.Currency != "EUR" {
		t.Fatalf("catalog entry = %#v", metso)
	}
}

// TestImportsCannotReachAnExchangeTheProductDoesNotStore.
//
// This used to be asserted on the catalog read as well. That was defence in depth on a read-only
// diagnostic, and it cost more than it protected: it made the one tool for asking what a plan
// covers unable to ask about anything the product did not already have. The risk it guarded
// against — importing bars from a market with no stored calendar, where a session cannot be told
// from a closure — lives on the paths that import, and is asserted here.
func TestImportsCannotReachAnExchangeTheProductDoesNotStore(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("an unsupported exchange must not reach the provider on an import path")
	}))
	defer server.Close()
	client := newTestClient(t, server, "token", time.Second)

	if _, err := client.Resolve(context.Background(), marketdata.ResolveRequest{
		ProviderSymbol: "AAPL.US", MIC: "XNYS",
	}); err == nil {
		t.Fatal("resolving against an exchange the product does not store was accepted")
	}
}

// TestTheCatalogCanBeAskedAboutAnExchangeTheProductDoesNotStore.
//
// The catalog read is a diagnostic: it is how an owner checks whether a symbol still exists, and
// it is the only way to find out what a market-data plan actually entitles this deployment to
// before committing a specification to it. Scoped to the four exchanges the product stores, it
// could not answer that question at all — which is a strange limit for a tool whose purpose is
// looking at what the product does not yet have.
//
// Imports are untouched: they resolve through the exchanges the product knows, and an unknown one
// is still refused there.
func TestTheCatalogCanBeAskedAboutAnExchangeTheProductDoesNotStore(t *testing.T) {
	const token = "test-provider-secret"
	var requested string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = r.URL.Path
		assertCommonQuery(t, r, token)
		_, _ = w.Write([]byte(`[
			{"Code":"GSPC","Name":"S&P 500 Index","Currency":"USD","Type":"INDEX","Isin":null}
		]`))
	}))
	defer server.Close()
	client := newTestClient(t, server, token, time.Second)

	listed, err := client.ListInstruments(context.Background(), "INDX")
	if err != nil {
		t.Fatalf("listing an exchange the product does not store: %v", err)
	}
	if requested != "/exchange-symbol-list/INDX" {
		t.Fatalf("asked for %q", requested)
	}
	if len(listed) != 1 || listed[0].ProviderSymbol != "GSPC.INDX" || listed[0].Name != "S&P 500 Index" {
		t.Fatalf("listed = %#v", listed)
	}

	// A known MIC still maps through the exchange the product knows, not through its own name.
	requested = ""
	if _, err := client.ListInstruments(context.Background(), "XSTO"); err != nil {
		t.Fatalf("listing a stored exchange: %v", err)
	}
	if requested != "/exchange-symbol-list/ST" {
		t.Fatalf("a stored exchange asked for %q", requested)
	}

	// Resolving, which imports depend on, still refuses an exchange the product does not store.
	if _, err := client.Resolve(context.Background(), marketdata.ResolveRequest{
		ProviderSymbol: "GSPC.INDX", MIC: "INDX",
	}); err == nil {
		t.Fatalf("resolve accepted an exchange the product does not store")
	}
}
