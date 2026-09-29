// Package yahoo reads daily prices from Yahoo Finance's public chart endpoint, as the temporary
// fallback of feature 030.
//
// The endpoint is unofficial and its terms restrict automated use; the owner accepted that for a
// personal, self-hosted installation. So this client is built to fail safe rather than to be
// relied on: one request per instrument, nothing it says is trusted beyond the prices it was
// asked for, and none of its own words ever reach an error.
package yahoo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"market-lens/server/internal/marketdata"
)

const (
	defaultBaseURL  = "https://query1.finance.yahoo.com"
	maxResponseBody = 4 << 20
	// From production the source answers 429 to a request without a browser's user agent, and 200
	// to one with it (research R2).
	userAgent = "Mozilla/5.0 (compatible; market-lens)"
)

type Config struct {
	BaseURL    string
	HTTPClient *http.Client
}

type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
}

func New(config Config) (*Client, error) {
	if config.BaseURL == "" {
		config.BaseURL = defaultBaseURL
	}
	baseURL, err := url.Parse(config.BaseURL)
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, errors.New("fallback base URL is invalid")
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/")
	baseURL.RawQuery, baseURL.Fragment = "", ""
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{baseURL: baseURL, httpClient: config.HTTPClient}, nil
}

func (*Client) Name() string { return marketdata.FallbackProvider }

// Resolve reports what the source says a symbol is: its currency, its exchange's zone and its
// name. It is evidence for the mapping audit, never a way to discover a symbol.
func (c *Client) Resolve(ctx context.Context, request marketdata.ResolveRequest) (marketdata.ResolvedInstrument, error) {
	if strings.TrimSpace(request.ProviderSymbol) == "" {
		return marketdata.ResolvedInstrument{}, providerError("provider_request", false, 0)
	}
	now := time.Now().UTC()
	chart, err := c.chart(ctx, request.ProviderSymbol, now.AddDate(0, 0, -7), now)
	if err != nil {
		return marketdata.ResolvedInstrument{}, err
	}
	name := chart.Meta.LongName
	if name == "" {
		name = chart.Meta.ShortName
	}
	return marketdata.ResolvedInstrument{
		ProviderSymbol: request.ProviderSymbol, Name: strings.TrimSpace(name), MIC: request.MIC,
		Currency: strings.ToUpper(strings.TrimSpace(chart.Meta.Currency)), Timezone: chart.Meta.ExchangeTimezoneName,
	}, nil
}

// Daily returns the bars for the sessions asked for, and never any corporate actions: the primary
// is their only source.
//
// A split or dividend inside the window holds the instrument back instead. The primary's adjusted
// history would move with it, and a fallback bar cannot be made consistent with a history this
// product has not been told about yet (FR-008). Outside such a window the adjusted close is
// exactly the close, because nothing follows it that could adjust it.
func (c *Client) Daily(ctx context.Context, request marketdata.DailyRequest) (marketdata.DailyPage, error) {
	if request.Cursor != "" || strings.TrimSpace(request.ProviderSymbol) == "" || request.From == "" ||
		request.To == "" || request.From > request.To {
		return marketdata.DailyPage{}, providerError("provider_request", false, 0)
	}
	// Padded by a day either side, because a session's stamp is its local opening instant; what is
	// kept is decided by the session date in the exchange's zone, not by these bounds.
	start := request.From.Time(time.UTC).AddDate(0, 0, -1)
	end := request.To.Time(time.UTC).AddDate(0, 0, 2)
	chart, err := c.chart(ctx, request.ProviderSymbol, start, end)
	if err != nil {
		return marketdata.DailyPage{}, err
	}
	zone, err := time.LoadLocation(chart.Meta.ExchangeTimezoneName)
	if err != nil || chart.Meta.ExchangeTimezoneName == "" {
		return marketdata.DailyPage{}, providerError("provider_payload", false, 0)
	}
	inWindow := func(stamp int64) (marketdata.SessionDate, bool) {
		date := marketdata.SessionDate(time.Unix(stamp, 0).In(zone).Format("2006-01-02"))
		return date, date >= request.From && date <= request.To
	}
	for _, events := range []map[string]event{chart.Events.Dividends, chart.Events.Splits} {
		for _, action := range events {
			if _, inside := inWindow(action.Date); inside {
				return marketdata.DailyPage{}, providerError("fallback_held_back", false, 0)
			}
		}
	}

	if len(chart.Indicators.Quote) != 1 {
		return marketdata.DailyPage{}, providerError("provider_payload", false, 0)
	}
	quote := chart.Indicators.Quote[0]
	count := len(chart.Timestamp)
	if len(quote.Open) != count || len(quote.High) != count || len(quote.Low) != count ||
		len(quote.Close) != count || len(quote.Volume) != count {
		return marketdata.DailyPage{}, providerError("provider_payload", false, 0)
	}
	decimals := chart.Meta.PriceHint
	if decimals < 2 {
		decimals = 2
	}
	if decimals > 6 {
		decimals = 6
	}
	bars := make([]marketdata.ProviderBar, 0, count)
	for index, stamp := range chart.Timestamp {
		date, inside := inWindow(stamp)
		if !inside {
			continue
		}
		// A day the source has no price for is no bar, not a zero.
		if quote.Open[index] == nil || quote.High[index] == nil || quote.Low[index] == nil ||
			quote.Close[index] == nil || quote.Volume[index] == nil {
			continue
		}
		bar, err := mapBar(date, decimals, *quote.Open[index], *quote.High[index], *quote.Low[index],
			*quote.Close[index], *quote.Volume[index])
		if err != nil {
			return marketdata.DailyPage{}, providerError("provider_payload", false, 0)
		}
		bars = append(bars, bar)
	}
	return marketdata.DailyPage{Bars: bars}, nil
}

// mapBar rounds each price to the source's own precision. The source sends binary floats
// (45.380001068115234); at its stated precision they are exactly the primary's stored closes,
// which is what the mapping audit found for all 100 instruments.
func mapBar(date marketdata.SessionDate, decimals int, open, high, low, closeValue, volume float64) (marketdata.ProviderBar, error) {
	values := make([]marketdata.Decimal, 0, 4)
	for _, raw := range []float64{open, high, low, closeValue} {
		if math.IsNaN(raw) || math.IsInf(raw, 0) || raw <= 0 {
			return marketdata.ProviderBar{}, errors.New("price must be positive")
		}
		value, err := marketdata.ParseDecimal(strconv.FormatFloat(raw, 'f', decimals, 64))
		if err != nil {
			return marketdata.ProviderBar{}, err
		}
		values = append(values, value)
	}
	if volume < 0 || volume != math.Trunc(volume) || volume > math.MaxInt64/2 {
		return marketdata.ProviderBar{}, errors.New("volume must be a whole non-negative number")
	}
	openValue, highValue, lowValue, closeDecimal := values[0], values[1], values[2], values[3]
	if !ordered(lowValue, openValue, highValue) || !ordered(lowValue, closeDecimal, highValue) {
		return marketdata.ProviderBar{}, errors.New("prices are not ordered")
	}
	adjusted := closeDecimal
	shares := int64(volume)
	canonical := strings.Join([]string{marketdata.FallbackProvider, date.String(), openValue.String(),
		highValue.String(), lowValue.String(), closeDecimal.String(), strconv.FormatInt(shares, 10)}, "|")
	digest := sha256.Sum256([]byte(canonical))
	return marketdata.ProviderBar{
		SessionDate: date, Open: openValue, High: highValue, Low: lowValue, Close: closeDecimal,
		AdjustedClose: &adjusted, Volume: shares, SourceHash: hex.EncodeToString(digest[:]),
	}, nil
}

// ordered reports low <= value <= high, compared as numbers rather than as text.
func ordered(low, value, high marketdata.Decimal) bool {
	number := func(d marketdata.Decimal) float64 {
		parsed, _ := strconv.ParseFloat(d.String(), 64)
		return parsed
	}
	return number(low) <= number(value) && number(value) <= number(high)
}

type chartResponse struct {
	Chart struct {
		Result []chartResult    `json:"result"`
		Error  *json.RawMessage `json:"error"`
	} `json:"chart"`
}

type chartResult struct {
	Meta struct {
		Currency             string `json:"currency"`
		LongName             string `json:"longName"`
		ShortName            string `json:"shortName"`
		ExchangeTimezoneName string `json:"exchangeTimezoneName"`
		PriceHint            int    `json:"priceHint"`
	} `json:"meta"`
	Timestamp []int64 `json:"timestamp"`
	Events    struct {
		Dividends map[string]event `json:"dividends"`
		Splits    map[string]event `json:"splits"`
	} `json:"events"`
	Indicators struct {
		Quote []struct {
			Open   []*float64 `json:"open"`
			High   []*float64 `json:"high"`
			Low    []*float64 `json:"low"`
			Close  []*float64 `json:"close"`
			Volume []*float64 `json:"volume"`
		} `json:"quote"`
	} `json:"indicators"`
}

type event struct {
	Date int64 `json:"date"`
}

func (c *Client) chart(ctx context.Context, symbol string, from, to time.Time) (chartResult, error) {
	endpoint := *c.baseURL
	endpoint.Path += "/v8/finance/chart/" + url.PathEscape(symbol)
	endpoint.RawQuery = url.Values{
		"period1":  {strconv.FormatInt(from.Unix(), 10)},
		"period2":  {strconv.FormatInt(to.Unix(), 10)},
		"interval": {"1d"},
		"events":   {"div|split"},
	}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return chartResult{}, providerError("provider_request", false, 0)
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return chartResult{}, context.Canceled
		}
		if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "Timeout") {
			return chartResult{}, providerError("provider_timeout", true, 0)
		}
		return chartResult{}, providerError("provider_unavailable", true, 0)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBody))
		return chartResult{}, statusError(response)
	}
	var decoded chartResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, maxResponseBody)).Decode(&decoded); err != nil {
		return chartResult{}, providerError("provider_payload", false, 0)
	}
	if decoded.Chart.Error != nil && string(*decoded.Chart.Error) != "null" {
		return chartResult{}, providerError("provider_not_found", false, 0)
	}
	if len(decoded.Chart.Result) != 1 {
		return chartResult{}, providerError("provider_payload", false, 0)
	}
	return decoded.Chart.Result[0], nil
}

// statusError classifies a refusal. There is no credential here to be wrong, so a 401 or 403 is
// the source declining to serve us — unavailable, not an authentication failure the owner could
// fix — and it is not retried within the run.
func statusError(response *http.Response) error {
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		retryAfter := time.Duration(0)
		if seconds, err := strconv.Atoi(response.Header.Get("Retry-After")); err == nil && seconds > 0 {
			retryAfter = time.Duration(seconds) * time.Second
		}
		return providerError("provider_rate_limited", true, retryAfter)
	case response.StatusCode == http.StatusNotFound:
		return providerError("provider_not_found", false, 0)
	case response.StatusCode >= 500:
		return providerError("provider_unavailable", true, 0)
	default:
		return providerError("provider_unavailable", false, 0)
	}
}

// providerError carries a code and its canonical summary, never anything the source said.
func providerError(code string, transient bool, retryAfter time.Duration) error {
	safe := marketdata.NormalizeSafeError(marketdata.SafeError{Code: code})
	return &marketdata.ProviderError{Code: safe.Code, Summary: safe.Summary, Transient: transient, RetryAfter: retryAfter}
}

var _ marketdata.Provider = (*Client)(nil)
