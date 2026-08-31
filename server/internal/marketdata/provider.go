package marketdata

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

var ErrProviderContractNotImplemented = errors.New("provider collection is not implemented")

type ResolveRequest struct {
	ProviderSymbol string
	MIC            string
}

type ResolvedInstrument struct {
	ProviderSymbol string
	ISIN           string
	Ticker         string
	Name           string
	MIC            string
	Currency       string
	Timezone       string
}

type ProviderBar struct {
	SessionDate   SessionDate
	Open          Decimal
	High          Decimal
	Low           Decimal
	Close         Decimal
	AdjustedClose *Decimal
	Volume        int64
	SourceHash    string
}

type ProviderAction struct {
	ProviderActionID string
	Type             CorporateActionType
	ExDate           SessionDate
	EffectiveDate    *SessionDate
	Ratio            *Decimal
	Amount           *Decimal
	Currency         string
	OldSymbol        string
	NewSymbol        string
	SourceHash       string
}

type DailyRequest struct {
	ProviderSymbol string
	From           SessionDate
	To             SessionDate
	Cursor         string
}

type DailyPage struct {
	Bars       []ProviderBar
	Actions    []ProviderAction
	NextCursor string
}

type Provider interface {
	Name() string
	Resolve(context.Context, ResolveRequest) (ResolvedInstrument, error)
	Daily(context.Context, DailyRequest) (DailyPage, error)
}

type ProviderError struct {
	Code       string
	Summary    string
	Transient  bool
	RetryAfter time.Duration
}

func (e *ProviderError) Error() string { return e.Summary }

type CollectOptions struct {
	MaxRetries int
	Backoff    func(context.Context, int, time.Duration) error
}

type DailyDataset struct {
	Bars    []ProviderBar
	Actions []ProviderAction
}

func CollectDaily(ctx context.Context, provider Provider, request DailyRequest, options CollectOptions) (DailyDataset, error) {
	if provider == nil {
		return DailyDataset{}, errors.New("market-data provider is required")
	}
	if err := ctx.Err(); err != nil {
		return DailyDataset{}, err
	}
	if options.MaxRetries < 0 {
		return DailyDataset{}, errors.New("maximum retries must not be negative")
	}

	bars := make(map[SessionDate]ProviderBar)
	actions := make(map[string]ProviderAction)
	seenCursors := make(map[string]struct{})

	for {
		page, err := collectDailyPage(ctx, provider, request, options)
		if err != nil {
			return DailyDataset{}, err
		}

		for _, candidate := range page.Bars {
			if existing, exists := bars[candidate.SessionDate]; exists && existing.SourceHash != candidate.SourceHash {
				return DailyDataset{}, errors.New("market-data provider returned conflicting overlapping daily bars")
			}
			bars[candidate.SessionDate] = candidate
		}
		for _, candidate := range page.Actions {
			key := candidate.ProviderActionID
			if key == "" {
				key = fmt.Sprintf("%s:%s:%s", candidate.Type, candidate.ExDate, candidate.SourceHash)
			}
			if existing, exists := actions[key]; exists && existing.SourceHash != candidate.SourceHash {
				return DailyDataset{}, errors.New("market-data provider returned conflicting overlapping corporate actions")
			}
			actions[key] = candidate
		}

		if page.NextCursor == "" {
			break
		}
		if _, exists := seenCursors[page.NextCursor]; exists {
			return DailyDataset{}, errors.New("market-data provider returned a repeated pagination cursor")
		}
		seenCursors[page.NextCursor] = struct{}{}
		request.Cursor = page.NextCursor
	}

	dataset := DailyDataset{
		Bars:    make([]ProviderBar, 0, len(bars)),
		Actions: make([]ProviderAction, 0, len(actions)),
	}
	for _, candidate := range bars {
		dataset.Bars = append(dataset.Bars, candidate)
	}
	for _, candidate := range actions {
		dataset.Actions = append(dataset.Actions, candidate)
	}
	sort.Slice(dataset.Bars, func(i, j int) bool {
		return dataset.Bars[i].SessionDate < dataset.Bars[j].SessionDate
	})
	sort.Slice(dataset.Actions, func(i, j int) bool {
		if dataset.Actions[i].ExDate == dataset.Actions[j].ExDate {
			return dataset.Actions[i].ProviderActionID < dataset.Actions[j].ProviderActionID
		}
		return dataset.Actions[i].ExDate < dataset.Actions[j].ExDate
	})
	return dataset, nil
}

func collectDailyPage(ctx context.Context, provider Provider, request DailyRequest, options CollectOptions) (DailyPage, error) {
	for attempt := 0; ; attempt++ {
		page, err := provider.Daily(ctx, request)
		if err == nil {
			return page, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return DailyPage{}, ctxErr
		}

		var providerErr *ProviderError
		if !errors.As(err, &providerErr) || !providerErr.Transient || attempt >= options.MaxRetries {
			return DailyPage{}, safeProviderError(err)
		}

		nextAttempt := attempt + 1
		if options.Backoff != nil {
			if err := options.Backoff(ctx, nextAttempt, providerErr.RetryAfter); err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return DailyPage{}, ctxErr
				}
				return DailyPage{}, safeProviderError(err)
			}
			continue
		}
		if err := waitForRetry(ctx, providerErr.RetryAfter); err != nil {
			return DailyPage{}, err
		}
	}
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		delay = 100 * time.Millisecond
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func safeProviderError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	// An error that already carries a code has been classified by whoever knew what happened —
	// the client saw the status, or the decoder saw the payload. Re-deriving a code from its
	// summary threw that away: the summaries are deliberately free of anything the substring
	// matching keys on, so every classified failure fell through to the generic
	// "provider_error" and an owner could not tell an expired key from an unknown symbol from
	// an exhausted quota.
	//
	// The summary is still normalized rather than trusted, so a code that arrives with an
	// invented summary gets the canonical one for that code.
	var classified *ProviderError
	if errors.As(err, &classified) && classified.Code != "" {
		safe := NormalizeSafeError(SafeError{Code: classified.Code, Summary: classified.Summary})
		return &ProviderError{
			Code: safe.Code, Summary: safe.Summary,
			Transient: classified.Transient, RetryAfter: classified.RetryAfter,
		}
	}
	safe := SanitizeError(err.Error())
	return &ProviderError{Code: safe.Code, Summary: safe.Summary}
}
