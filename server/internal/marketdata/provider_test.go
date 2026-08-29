package marketdata

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestProviderResolvesExchangeQualifiedInstrument(t *testing.T) {
	provider := &scriptedProvider{resolved: ResolvedInstrument{
		ProviderSymbol: "SAME.ST", ISIN: "SE0000000001", Ticker: "SAME", Name: "Swedish Listing",
		MIC: "XSTO", Currency: "SEK", Timezone: "Europe/Stockholm",
	}}
	resolved, err := provider.Resolve(context.Background(), ResolveRequest{ProviderSymbol: "SAME.ST", MIC: "XSTO"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.MIC != "XSTO" || resolved.ProviderSymbol != "SAME.ST" || resolved.ISIN == "" || resolved.Timezone == "" {
		t.Fatalf("incomplete resolution: %#v", resolved)
	}
}

func TestCollectDailyOrdersAndDeduplicatesPaginationOverlap(t *testing.T) {
	dayOne := mustSession(t, "2026-08-24")
	dayTwo := mustSession(t, "2026-08-25")
	dayThree := mustSession(t, "2026-08-26")
	provider := &scriptedProvider{pages: []DailyPage{
		{Bars: []ProviderBar{bar(t, dayTwo, "102"), bar(t, dayOne, "101")}, NextCursor: "next"},
		{Bars: []ProviderBar{bar(t, dayTwo, "102"), bar(t, dayThree, "103")}, Actions: []ProviderAction{
			{ProviderActionID: "split-2", Type: ActionSplit, ExDate: dayThree, SourceHash: "action-2"},
			{ProviderActionID: "split-1", Type: ActionSplit, ExDate: dayOne, SourceHash: "action-1"},
		}},
	}}
	dataset, err := CollectDaily(context.Background(), provider, DailyRequest{
		ProviderSymbol: "SAME.ST", From: dayOne, To: dayThree,
	}, CollectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.requests != 2 || len(dataset.Bars) != 3 || dataset.Bars[0].SessionDate != dayOne || dataset.Bars[2].SessionDate != dayThree {
		t.Fatalf("unordered/overlapping bars: requests=%d bars=%#v", provider.requests, dataset.Bars)
	}
	if len(dataset.Actions) != 2 || dataset.Actions[0].ProviderActionID != "split-1" {
		t.Fatalf("unordered actions: %#v", dataset.Actions)
	}
}

func TestCollectDailyRetriesTransientRateLimitWithoutLeakingSecrets(t *testing.T) {
	const secret = "provider-token-never-expose"
	day := mustSession(t, "2026-08-24")
	provider := &scriptedProvider{
		errors: []error{&ProviderError{Code: "rate_limited", Summary: "Provider rate limit reached.", Transient: true, RetryAfter: time.Second}},
		pages:  []DailyPage{{Bars: []ProviderBar{bar(t, day, "101")}}},
	}
	backoffs := 0
	dataset, err := CollectDaily(context.Background(), provider, DailyRequest{ProviderSymbol: "SAME.ST", From: day, To: day}, CollectOptions{
		MaxRetries: 2,
		Backoff: func(_ context.Context, attempt int, retryAfter time.Duration) error {
			backoffs++
			if attempt != 1 || retryAfter != time.Second {
				t.Fatalf("backoff attempt=%d retryAfter=%s", attempt, retryAfter)
			}
			return nil
		},
	})
	if err != nil || len(dataset.Bars) != 1 || provider.requests != 2 || backoffs != 1 {
		t.Fatalf("retry result=%#v requests=%d backoffs=%d err=%v", dataset, provider.requests, backoffs, err)
	}

	provider = &scriptedProvider{errors: []error{&ProviderError{
		Code: "provider_authentication", Summary: "Provider authentication failed.",
	}}}
	_, err = CollectDaily(context.Background(), provider, DailyRequest{ProviderSymbol: secret, From: day, To: day}, CollectOptions{})
	if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "token") || strings.Contains(err.Error(), "http") {
		t.Fatalf("unsafe provider failure: %v", err)
	}
}

func TestCollectDailyStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	provider := &scriptedProvider{block: true}
	done := make(chan error, 1)
	go func() {
		_, err := CollectDaily(ctx, provider, DailyRequest{ProviderSymbol: "SAME.ST", From: mustSession(t, "2026-08-24"), To: mustSession(t, "2026-08-25")}, CollectOptions{MaxRetries: 2})
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("provider collection ignored cancellation")
	}
}

type scriptedProvider struct {
	resolved ResolvedInstrument
	pages    []DailyPage
	errors   []error
	requests int
	block    bool
}

func (p *scriptedProvider) Name() string { return "fixture" }

func (p *scriptedProvider) Resolve(context.Context, ResolveRequest) (ResolvedInstrument, error) {
	return p.resolved, nil
}

func (p *scriptedProvider) Daily(ctx context.Context, _ DailyRequest) (DailyPage, error) {
	p.requests++
	if p.block {
		<-ctx.Done()
		return DailyPage{}, ctx.Err()
	}
	if len(p.errors) > 0 {
		err := p.errors[0]
		p.errors = p.errors[1:]
		return DailyPage{}, err
	}
	if len(p.pages) == 0 {
		return DailyPage{}, nil
	}
	page := p.pages[0]
	p.pages = p.pages[1:]
	return page, nil
}

func mustSession(t *testing.T, value string) SessionDate {
	t.Helper()
	session, err := ParseSessionDate(value)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func bar(t *testing.T, session SessionDate, closeValue string) ProviderBar {
	t.Helper()
	open, _ := ParseDecimal("100")
	high, _ := ParseDecimal("110")
	low, _ := ParseDecimal("90")
	closeDecimal, err := ParseDecimal(closeValue)
	if err != nil {
		t.Fatal(err)
	}
	return ProviderBar{SessionDate: session, Open: open, High: high, Low: low, Close: closeDecimal, Volume: 100, SourceHash: session.String() + ":" + closeValue}
}
