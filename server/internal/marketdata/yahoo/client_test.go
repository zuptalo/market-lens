package yahoo

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

// Shaped from a real response for TELIA.ST: sessions are stamped at the exchange's opening
// instant, prices are binary floats, and a day the source has no price for arrives as nulls.
const chartFixture = `{"chart":{"result":[{
 "meta":{"currency":"SEK","symbol":"TELIA.ST","exchangeName":"STO","longName":"Telia Company AB (publ)",
  "gmtoffset":7200,"exchangeTimezoneName":"Europe/Stockholm","priceHint":2},
 "timestamp":[1790578800,1790665200,1790751600],
 "events":%s,
 "indicators":{"quote":[{
   "open":[45.0099983215332,45.20000076293945,null],
   "high":[45.5,45.599998474121094,null],
   "low":[44.900001525878906,45.02000045776367,null],
   "close":[45.380001068115234,45.130001068115234,null],
   "volume":[5123456,4250794,null]}],
  "adjclose":[{"adjclose":[45.380001068115234,45.130001068115234,null]}]}}],"error":null}}`

func fixture(events string) string { return strings.Replace(chartFixture, "%s", events, 1) }

func serve(t *testing.T, status int, body string, inspect func(*http.Request)) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if inspect != nil {
			inspect(r)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	client, err := New(Config{BaseURL: server.URL, HTTPClient: &http.Client{Timeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func request(t *testing.T, from, to string) marketdata.DailyRequest {
	t.Helper()
	fromDate, err := marketdata.ParseSessionDate(from)
	if err != nil {
		t.Fatal(err)
	}
	toDate, err := marketdata.ParseSessionDate(to)
	if err != nil {
		t.Fatal(err)
	}
	return marketdata.DailyRequest{ProviderSymbol: "TELIA.ST", From: fromDate, To: toDate}
}

func TestTheClientAnswersAsTheFallback(t *testing.T) {
	client := serve(t, 200, fixture("{}"), nil)
	if client.Name() != marketdata.FallbackProvider {
		t.Errorf("name = %q", client.Name())
	}
}

// One request per instrument, carrying the actions with the prices, and a browser's user agent:
// from production the source answers 429 to a request without one (research R2).
func TestDailyAsksOnceForPricesAndActions(t *testing.T) {
	calls := 0
	client := serve(t, 200, fixture("{}"), func(r *http.Request) {
		calls++
		if r.URL.Path != "/v8/finance/chart/TELIA.ST" {
			t.Errorf("path = %q", r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("interval") != "1d" || !strings.Contains(query.Get("events"), "div") ||
			!strings.Contains(query.Get("events"), "split") {
			t.Errorf("query = %q", r.URL.RawQuery)
		}
		if query.Get("period1") == "" || query.Get("period2") == "" {
			t.Errorf("no period in %q", r.URL.RawQuery)
		}
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "Mozilla/") {
			t.Errorf("user agent = %q", r.Header.Get("User-Agent"))
		}
	})
	if _, err := client.Daily(context.Background(), request(t, "2026-09-28", "2026-09-29")); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("%d requests for one instrument", calls)
	}
}

// Dates are the session in the exchange's own zone; prices are rounded to the source's own
// precision, which reproduces the primary's stored closes exactly; a null day is no bar; and the
// adjusted close is the close, which holds while no action follows the last primary price.
func TestDailyMapsSessionsAndExactPrices(t *testing.T) {
	client := serve(t, 200, fixture("{}"), nil)
	page, err := client.Daily(context.Background(), request(t, "2026-09-28", "2026-09-30"))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Bars) != 2 || len(page.Actions) != 0 || page.NextCursor != "" {
		t.Fatalf("page = %#v", page)
	}
	first := page.Bars[0]
	if first.SessionDate.String() != "2026-09-28" || first.Open.String() != "45.01" || first.High.String() != "45.5" ||
		first.Low.String() != "44.9" || first.Close.String() != "45.38" || first.Volume != 5123456 {
		t.Errorf("first bar = %s o=%s h=%s l=%s c=%s v=%d", first.SessionDate, first.Open, first.High,
			first.Low, first.Close, first.Volume)
	}
	if first.AdjustedClose == nil || first.AdjustedClose.String() != first.Close.String() {
		t.Errorf("adjusted close = %v", first.AdjustedClose)
	}
	if page.Bars[1].SessionDate.String() != "2026-09-29" || page.Bars[1].Close.String() != "45.13" {
		t.Errorf("second bar = %s %s", page.Bars[1].SessionDate, page.Bars[1].Close)
	}
	if first.SourceHash == "" || first.SourceHash == page.Bars[1].SourceHash {
		t.Errorf("source hashes %q and %q", first.SourceHash, page.Bars[1].SourceHash)
	}
}

// A session outside the window asked for is not the caller's to store.
func TestDailyKeepsToTheWindow(t *testing.T) {
	client := serve(t, 200, fixture("{}"), nil)
	page, err := client.Daily(context.Background(), request(t, "2026-09-29", "2026-09-29"))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Bars) != 1 || page.Bars[0].SessionDate.String() != "2026-09-29" {
		t.Fatalf("bars = %#v", page.Bars)
	}
}

// FR-008. A split or dividend inside the window means the primary's adjusted history is about to
// move, and a fallback bar cannot be made consistent with it — so the instrument is held back.
func TestDailyHoldsBackAWindowWithAnAction(t *testing.T) {
	for name, events := range map[string]string{
		"dividend": `{"dividends":{"1790665200":{"amount":0.51,"date":1790665200}}}`,
		"split":    `{"splits":{"1790665200":{"date":1790665200,"numerator":2,"denominator":1,"splitRatio":"2:1"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			client := serve(t, 200, fixture(events), nil)
			_, err := client.Daily(context.Background(), request(t, "2026-09-28", "2026-09-29"))
			var providerErr *marketdata.ProviderError
			if !errors.As(err, &providerErr) || providerErr.Code != "fallback_held_back" || providerErr.Transient {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

// An action before the window was already in the primary's history when it last answered.
func TestAnActionBeforeTheWindowDoesNotHoldBack(t *testing.T) {
	client := serve(t, 200, fixture(`{"dividends":{"1790578800":{"amount":0.51,"date":1790578800}}}`), nil)
	if _, err := client.Daily(context.Background(), request(t, "2026-09-29", "2026-09-29")); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestDailyClassifiesRefusals(t *testing.T) {
	for _, tc := range []struct {
		status    int
		code      string
		transient bool
	}{
		{http.StatusTooManyRequests, "provider_rate_limited", true},
		{http.StatusServiceUnavailable, "provider_unavailable", true},
		{http.StatusNotFound, "provider_not_found", false},
		{http.StatusUnauthorized, "provider_unavailable", false},
	} {
		client := serve(t, tc.status, `{"chart":{"result":null,"error":{"code":"x","description":"secret detail"}}}`, nil)
		_, err := client.Daily(context.Background(), request(t, "2026-09-28", "2026-09-29"))
		var providerErr *marketdata.ProviderError
		if !errors.As(err, &providerErr) || providerErr.Code != tc.code || providerErr.Transient != tc.transient {
			t.Errorf("%d: err = %#v", tc.status, err)
		}
		if err != nil && strings.Contains(err.Error(), "secret detail") {
			t.Errorf("%d: the source's own words reached the error", tc.status)
		}
	}
}

// A 200 carrying an error, or a body that is not a chart, is not data.
func TestDailyRefusesAnUnusableBody(t *testing.T) {
	for name, body := range map[string]string{
		"error":     `{"chart":{"result":null,"error":{"code":"Not Found","description":"No data found"}}}`,
		"malformed": `<html>`,
		"inverted":  strings.Replace(fixture("{}"), `"high":[45.5`, `"high":[40.5`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			client := serve(t, 200, body, nil)
			if _, err := client.Daily(context.Background(), request(t, "2026-09-28", "2026-09-29")); err == nil {
				t.Fatal("an unusable body was accepted")
			}
		})
	}
}

func TestResolveReportsCurrencyAndZone(t *testing.T) {
	client := serve(t, 200, fixture("{}"), nil)
	resolved, err := client.Resolve(context.Background(), marketdata.ResolveRequest{ProviderSymbol: "TELIA.ST", MIC: "XSTO"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ProviderSymbol != "TELIA.ST" || resolved.Currency != "SEK" || resolved.Timezone != "Europe/Stockholm" ||
		resolved.Name != "Telia Company AB (publ)" || resolved.MIC != "XSTO" {
		t.Errorf("resolved = %#v", resolved)
	}
}

// The audit's question: what does the source report for this symbol on this one session. Unlike
// Daily it is not held back by an action — a dividend on the day does not make the close unknown.
func TestCloseOnReportsTheCurrencyAndCloseOfOneSession(t *testing.T) {
	client := serve(t, 200, fixture(`{"dividends":{"1790578800":{"amount":0.51,"date":1790578800}}}`), nil)
	session, _ := marketdata.ParseSessionDate("2026-09-28")
	currency, closeValue, err := client.CloseOn(context.Background(), "TELIA.ST", session)
	if err != nil {
		t.Fatal(err)
	}
	if currency != "SEK" || closeValue == nil || closeValue.String() != "45.38" {
		t.Errorf("close on = %s %v", currency, closeValue)
	}
	missing, _ := marketdata.ParseSessionDate("2026-09-27")
	if _, closeValue, err := client.CloseOn(context.Background(), "TELIA.ST", missing); err != nil || closeValue != nil {
		t.Errorf("a session the source has no price for gave %v %v", closeValue, err)
	}
}
