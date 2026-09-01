// Package eodhd adapts EODHD HTTP responses to provider-neutral market-data records.
package eodhd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"market-lens/server/internal/marketdata"
)

const (
	defaultBaseURL  = "https://eodhd.com/api"
	maxResponseBody = 16 << 20
)

type Config struct {
	BaseURL  string
	APIToken string
	// TokenSource resolves the API token for each request. A self-hosted installation stores
	// its market-data key in the database and changes it from the settings screen, so the
	// token has to be read when it is used: capturing it once at construction would leave a
	// rotated key failing until the process restarted, and the symptom would look like a
	// broken importer rather than a stale token. APIToken remains for callers that genuinely
	// have a fixed one, such as tests and the credential validator.
	TokenSource func(context.Context) (string, error)
	HTTPClient  *http.Client
}

type Client struct {
	baseURL     *url.URL
	tokenSource func(context.Context) (string, error)
	httpClient  *http.Client
}

func New(config Config) (*Client, error) {
	source := config.TokenSource
	if source == nil {
		token := strings.TrimSpace(config.APIToken)
		if token == "" {
			return nil, errors.New("EODHD API token is required")
		}
		source = func(context.Context) (string, error) { return token, nil }
	}
	if config.BaseURL == "" {
		config.BaseURL = defaultBaseURL
	}
	baseURL, err := url.Parse(config.BaseURL)
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, errors.New("EODHD base URL is invalid")
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/")
	baseURL.RawQuery = ""
	baseURL.Fragment = ""
	if config.HTTPClient == nil {
		config.HTTPClient = http.DefaultClient
	}
	return &Client{baseURL: baseURL, tokenSource: source, httpClient: config.HTTPClient}, nil
}

func (*Client) Name() string { return "eodhd" }

func (c *Client) Resolve(ctx context.Context, request marketdata.ResolveRequest) (marketdata.ResolvedInstrument, error) {
	exchange, ok := nordicExchange(request.MIC)
	if !ok || strings.TrimSpace(request.ProviderSymbol) == "" {
		return marketdata.ResolvedInstrument{}, providerError("provider_request", "Market-data provider request is invalid.", false, 0)
	}

	var rows []symbolResponse
	if err := c.getJSON(ctx, "/exchange-symbol-list/"+exchange.code, nil, &rows); err != nil {
		return marketdata.ResolvedInstrument{}, err
	}
	ticker := request.ProviderSymbol
	if dot := strings.LastIndexByte(ticker, '.'); dot > 0 {
		ticker = ticker[:dot]
	}
	for _, row := range rows {
		if !strings.EqualFold(row.Code, ticker) {
			continue
		}
		return marketdata.ResolvedInstrument{
			ProviderSymbol: request.ProviderSymbol,
			ISIN:           strings.TrimSpace(row.ISIN),
			Ticker:         strings.TrimSpace(row.Code),
			Name:           strings.TrimSpace(row.Name),
			MIC:            request.MIC,
			Currency:       strings.ToUpper(strings.TrimSpace(row.Currency)),
			Timezone:       exchange.timezone,
		}, nil
	}
	return marketdata.ResolvedInstrument{}, providerError("provider_not_found", "Market-data provider instrument was not found.", false, 0)
}

func (c *Client) Daily(ctx context.Context, request marketdata.DailyRequest) (marketdata.DailyPage, error) {
	if request.Cursor != "" || request.ProviderSymbol == "" || request.From == "" || request.To == "" || request.From > request.To {
		return marketdata.DailyPage{}, providerError("provider_request", "Market-data provider request is invalid.", false, 0)
	}
	query := url.Values{"from": {request.From.String()}, "to": {request.To.String()}}

	var barRows []barResponse
	barQuery := cloneValues(query)
	barQuery.Set("period", "d")
	if err := c.getJSON(ctx, "/eod/"+url.PathEscape(request.ProviderSymbol), barQuery, &barRows); err != nil {
		return marketdata.DailyPage{}, err
	}
	bars := make([]marketdata.ProviderBar, 0, len(barRows))
	for _, row := range barRows {
		bar, err := mapBar(row)
		if err != nil {
			return marketdata.DailyPage{}, providerError("provider_payload", "Market-data provider returned an invalid daily record.", false, 0)
		}
		bars = append(bars, bar)
	}

	var splitRows []splitResponse
	if err := c.getJSON(ctx, "/splits/"+url.PathEscape(request.ProviderSymbol), query, &splitRows); err != nil {
		return marketdata.DailyPage{}, err
	}
	var dividendRows []dividendResponse
	if err := c.getJSON(ctx, "/div/"+url.PathEscape(request.ProviderSymbol), query, &dividendRows); err != nil {
		return marketdata.DailyPage{}, err
	}
	actions := make([]marketdata.ProviderAction, 0, len(splitRows)+len(dividendRows))
	for _, row := range splitRows {
		action, err := mapSplit(request.ProviderSymbol, row)
		if err != nil {
			return marketdata.DailyPage{}, providerError("provider_payload", "Market-data provider returned an invalid corporate action.", false, 0)
		}
		actions = append(actions, action)
	}
	for _, row := range dividendRows {
		action, err := mapDividend(request.ProviderSymbol, row)
		if err != nil {
			return marketdata.DailyPage{}, providerError("provider_payload", "Market-data provider returned an invalid corporate action.", false, 0)
		}
		actions = append(actions, action)
	}
	return marketdata.DailyPage{Bars: bars, Actions: actions}, nil
}

func (c *Client) getJSON(ctx context.Context, path string, query url.Values, destination any) error {
	// Resolved here rather than held on the client, so a key rotated through the settings
	// screen takes effect on the next request. A source that cannot answer fails the request
	// instead of sending an empty token the provider would reject as though it were wrong.
	token, err := c.tokenSource(ctx)
	if err != nil || strings.TrimSpace(token) == "" {
		return providerError("provider_credentials",
			"Market-data credentials are unavailable.", true, 0)
	}

	endpoint := *c.baseURL
	endpoint.Path += path
	values := cloneValues(query)
	values.Set("api_token", token)
	values.Set("fmt", "json")
	endpoint.RawQuery = values.Encode()

	request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if requestErr != nil {
		return providerError("provider_request", "Market-data provider request is invalid.", false, 0)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return context.Canceled
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return providerError("provider_timeout", "Market-data provider request timed out.", true, 0)
		}
		return providerError("provider_unavailable", "Market-data provider is unavailable.", true, 0)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBody))
		return statusError(response)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBody))
	decoder.UseNumber()
	if err := decoder.Decode(destination); err != nil {
		return providerError("provider_payload", "Market-data provider returned an invalid response.", false, 0)
	}
	return nil
}

type exchangeDetails struct {
	code     string
	timezone string
}

func nordicExchange(mic string) (exchangeDetails, bool) {
	exchanges := map[string]exchangeDetails{
		"XSTO": {code: "ST", timezone: "Europe/Stockholm"},
		"XCSE": {code: "CO", timezone: "Europe/Copenhagen"},
		"XHEL": {code: "HE", timezone: "Europe/Helsinki"},
		"XOSL": {code: "OL", timezone: "Europe/Oslo"},
	}
	result, ok := exchanges[strings.ToUpper(mic)]
	return result, ok
}

type symbolResponse struct {
	Code     string `json:"Code"`
	Name     string `json:"Name"`
	Currency string `json:"Currency"`
	ISIN     string `json:"Isin"`
}

type barResponse struct {
	Date          string      `json:"date"`
	Open          json.Number `json:"open"`
	High          json.Number `json:"high"`
	Low           json.Number `json:"low"`
	Close         json.Number `json:"close"`
	AdjustedClose json.Number `json:"adjusted_close"`
	Volume        int64       `json:"volume"`
}

type splitResponse struct {
	Date  string `json:"date"`
	Split string `json:"split"`
}

type dividendResponse struct {
	Date     string      `json:"date"`
	Value    json.Number `json:"value"`
	Currency string      `json:"currency"`
}

func mapBar(row barResponse) (marketdata.ProviderBar, error) {
	date, err := marketdata.ParseSessionDate(row.Date)
	if err != nil {
		return marketdata.ProviderBar{}, err
	}
	open, err := marketdata.ParseDecimal(row.Open.String())
	if err != nil {
		return marketdata.ProviderBar{}, err
	}
	high, err := marketdata.ParseDecimal(row.High.String())
	if err != nil {
		return marketdata.ProviderBar{}, err
	}
	low, err := marketdata.ParseDecimal(row.Low.String())
	if err != nil {
		return marketdata.ProviderBar{}, err
	}
	closeValue, err := marketdata.ParseDecimal(row.Close.String())
	if err != nil {
		return marketdata.ProviderBar{}, err
	}
	var adjusted *marketdata.Decimal
	if row.AdjustedClose.String() != "" {
		value, err := marketdata.ParseDecimal(row.AdjustedClose.String())
		if err != nil {
			return marketdata.ProviderBar{}, err
		}
		adjusted = &value
	}
	canonical := strings.Join([]string{row.Date, open.String(), high.String(), low.String(), closeValue.String(), decimalPointer(adjusted), strconv.FormatInt(row.Volume, 10)}, "|")
	return marketdata.ProviderBar{SessionDate: date, Open: open, High: high, Low: low, Close: closeValue, AdjustedClose: adjusted, Volume: row.Volume, SourceHash: sourceHash(canonical)}, nil
}

// ListInstruments returns everything the provider lists for one exchange.
//
// Resolve answers "does this ticker exist"; this answers "what does this exchange contain",
// which is the question worth asking when a stored ticker has gone stale. Each entry carries
// its ISIN, so an instrument that has been renamed can be found again by the identifier that
// did not change.
func (c *Client) ListInstruments(ctx context.Context, mic string) ([]marketdata.CatalogEntry, error) {
	exchange, ok := nordicExchange(mic)
	if !ok {
		return nil, providerError("provider_request", "Market-data provider request is invalid.", false, 0)
	}

	var rows []symbolResponse
	if err := c.getJSON(ctx, "/exchange-symbol-list/"+exchange.code, nil, &rows); err != nil {
		return nil, err
	}

	catalog := make([]marketdata.CatalogEntry, 0, len(rows))
	for _, row := range rows {
		ticker := strings.TrimSpace(row.Code)
		if ticker == "" {
			continue
		}
		catalog = append(catalog, marketdata.CatalogEntry{
			ProviderSymbol: ticker + "." + exchange.code,
			ISIN:           strings.TrimSpace(row.ISIN),
			Ticker:         ticker,
			Name:           strings.TrimSpace(row.Name),
			Currency:       strings.ToUpper(strings.TrimSpace(row.Currency)),
		})
	}
	return catalog, nil
}

func mapSplit(symbol string, row splitResponse) (marketdata.ProviderAction, error) {
	date, err := marketdata.ParseSessionDate(row.Date)
	if err != nil {
		return marketdata.ProviderAction{}, err
	}
	ratio, err := parseSplitRatio(row.Split)
	if err != nil {
		return marketdata.ProviderAction{}, err
	}
	canonical := strings.Join([]string{symbol, "split", row.Date, ratio.String()}, "|")
	return marketdata.ProviderAction{ProviderActionID: sourceHash(canonical), Type: marketdata.ActionSplit, ExDate: date, Ratio: &ratio, SourceHash: sourceHash(canonical)}, nil
}

func mapDividend(symbol string, row dividendResponse) (marketdata.ProviderAction, error) {
	date, err := marketdata.ParseSessionDate(row.Date)
	if err != nil {
		return marketdata.ProviderAction{}, err
	}
	amount, err := marketdata.ParseDecimal(row.Value.String())
	if err != nil {
		return marketdata.ProviderAction{}, err
	}
	currency := strings.ToUpper(strings.TrimSpace(row.Currency))
	canonical := strings.Join([]string{symbol, "dividend", row.Date, amount.String(), currency}, "|")
	return marketdata.ProviderAction{ProviderActionID: sourceHash(canonical), Type: marketdata.ActionDividend, ExDate: date, Amount: &amount, Currency: currency, SourceHash: sourceHash(canonical)}, nil
}

// parseSplitRatio reads the split factor EODHD reports.
//
// It arrives as a fraction of two decimals — "2.000000/1.000000" for a two-for-one — and
// big.Rat only accepts a fraction whose parts are integers. Handing it the whole string
// rejected every real split, and because one unusable action fails the entire page, every
// instrument that had ever split imported nothing at all.
//
// So each side is parsed on its own, where a decimal is perfectly acceptable, and the
// division is done here.
func parseSplitRatio(value string) (marketdata.Decimal, error) {
	value = strings.TrimSpace(value)
	if !strings.Contains(value, "/") {
		return marketdata.ParseDecimal(value)
	}

	numerator, denominator, _ := strings.Cut(value, "/")
	top, ok := new(big.Rat).SetString(strings.TrimSpace(numerator))
	if !ok {
		return "", errors.New("invalid split ratio")
	}
	bottom, ok := new(big.Rat).SetString(strings.TrimSpace(denominator))
	// A zero denominator is not a split, and neither is a zero or negative factor.
	if !ok || bottom.Sign() <= 0 || top.Sign() <= 0 {
		return "", errors.New("invalid split ratio")
	}
	return marketdata.ParseDecimal(new(big.Rat).Quo(top, bottom).FloatString(8))
}

func statusError(response *http.Response) error {
	switch response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return providerError("provider_authentication", "Market-data provider authentication failed.", false, 0)
	case http.StatusTooManyRequests:
		retryAfter := time.Duration(0)
		if seconds, err := strconv.Atoi(response.Header.Get("Retry-After")); err == nil && seconds > 0 {
			retryAfter = time.Duration(seconds) * time.Second
		}
		return providerError("provider_rate_limited", "Market-data provider rate limit was reached.", true, retryAfter)
	case http.StatusNotFound:
		return providerError("provider_not_found", "Market-data provider data was not found.", false, 0)
	default:
		return providerError("provider_unavailable", "Market-data provider is unavailable.", response.StatusCode >= 500, 0)
	}
}

func providerError(code, summary string, transient bool, retryAfter time.Duration) error {
	return &marketdata.ProviderError{Code: code, Summary: summary, Transient: transient, RetryAfter: retryAfter}
}

func cloneValues(source url.Values) url.Values {
	result := make(url.Values, len(source)+2)
	for key, values := range source {
		result[key] = append([]string(nil), values...)
	}
	return result
}

func sourceHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func decimalPointer(value *marketdata.Decimal) string {
	if value == nil {
		return ""
	}
	return value.String()
}

var _ marketdata.Provider = (*Client)(nil)
