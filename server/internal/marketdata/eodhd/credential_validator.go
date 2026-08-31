package eodhd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxCredentialValidationBody = 256 << 10

type CredentialValidatorConfig struct {
	BaseURL     string
	HTTPClient  *http.Client
	Timeout     time.Duration
	Now         func() time.Time
	ProbeSymbol string
}

type CredentialValidator struct {
	baseURL     *url.URL
	httpClient  *http.Client
	timeout     time.Duration
	now         func() time.Time
	probeSymbol string
}

func NewCredentialValidator(config CredentialValidatorConfig) (*CredentialValidator, error) {
	if config.BaseURL == "" {
		config.BaseURL = defaultBaseURL
	}
	baseURL, err := url.Parse(config.BaseURL)
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, errors.New("EODHD credential validation URL is invalid")
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/")
	baseURL.RawQuery = ""
	baseURL.Fragment = ""
	if config.HTTPClient == nil {
		config.HTTPClient = http.DefaultClient
	}
	if config.Timeout <= 0 || config.Timeout > 30*time.Second {
		return nil, errors.New("EODHD credential validation timeout is invalid")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.ProbeSymbol == "" {
		config.ProbeSymbol = "ERIC-B.ST"
	}
	if strings.TrimSpace(config.ProbeSymbol) != config.ProbeSymbol || strings.ContainsAny(config.ProbeSymbol, "/?#") {
		return nil, errors.New("EODHD credential validation probe is invalid")
	}
	return &CredentialValidator{
		baseURL: baseURL, httpClient: config.HTTPClient, timeout: config.Timeout,
		now: config.Now, probeSymbol: config.ProbeSymbol,
	}, nil
}

func (validator *CredentialValidator) ValidateCredential(ctx context.Context, token string) error {
	if validator == nil || strings.TrimSpace(token) == "" || len(token) > 1024 {
		return providerError("provider_authentication", "Market-data provider authentication failed.", false, 0)
	}
	validationContext, cancel := context.WithTimeout(ctx, validator.timeout)
	defer cancel()
	var user struct {
		SubscriptionType string `json:"subscriptionType"`
		DailyRateLimit   int64  `json:"dailyRateLimit"`
	}
	if err := validator.getJSON(validationContext, "/user", token, nil, &user); err != nil {
		return err
	}
	if strings.TrimSpace(user.SubscriptionType) == "" || user.DailyRateLimit <= 0 {
		return providerError("provider_authentication", "Market-data provider authentication failed.", false, 0)
	}
	probeStart := validator.now().UTC().AddDate(-10, 0, 0)
	probeQuery := url.Values{
		"from": {probeStart.Format(time.DateOnly)},
		"to":   {probeStart.AddDate(0, 0, 14).Format(time.DateOnly)},
	}
	var historicalRows []json.RawMessage
	if err := validator.getJSON(validationContext, "/eod/"+url.PathEscape(validator.probeSymbol), token, probeQuery, &historicalRows); err != nil {
		return err
	}
	if len(historicalRows) == 0 {
		return providerError("provider_entitlement", "Market-data provider historical entitlement is unavailable.", false, 0)
	}
	return nil
}

func (validator *CredentialValidator) getJSON(ctx context.Context, path, token string, query url.Values, destination any) error {
	endpoint := *validator.baseURL
	endpoint.Path += path
	values := cloneValues(query)
	values.Set("api_token", token)
	values.Set("fmt", "json")
	endpoint.RawQuery = values.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return providerError("provider_request", "Market-data provider request is invalid.", false, 0)
	}
	response, err := validator.httpClient.Do(request)
	if err != nil {
		if errors.Is(err, context.Canceled) && ctx.Err() == context.DeadlineExceeded || errors.Is(err, context.DeadlineExceeded) {
			return providerError("provider_timeout", "Market-data provider request timed out.", true, 0)
		}
		if errors.Is(err, context.Canceled) {
			return context.Canceled
		}
		return providerError("provider_unavailable", "Market-data provider is unavailable.", true, 0)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusForbidden {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxCredentialValidationBody))
		return providerError("provider_entitlement", "Market-data provider historical entitlement is unavailable.", false, 0)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxCredentialValidationBody))
		return statusError(response)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxCredentialValidationBody))
	if err := decoder.Decode(destination); err != nil {
		return providerError("provider_payload", "Market-data provider returned an invalid response.", false, 0)
	}
	return nil
}
