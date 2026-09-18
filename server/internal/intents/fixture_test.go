package intents_test

import (
	"log/slog"
	"strconv"
	"testing"

	"market-lens/server/internal/intents"
	"market-lens/server/internal/portfolio"
	"market-lens/server/internal/risk"
)

// The intents fixture builds on the portfolio one, because an intent's whole meaning is what it
// would do to a portfolio. Two people, always: these are private records.

type intentsFixture struct {
	*portfolioFixture
	riskService *risk.Service
}

func newIntentsFixture(t *testing.T) *intentsFixture {
	t.Helper()
	base := newPortfolioFixture(t)
	return &intentsFixture{
		portfolioFixture: base,
		riskService: risk.NewService(risk.NewRepository(base.pool),
			portfolio.NewService(portfolio.NewRepository(base.pool), slog.Default()), slog.Default()),
	}
}

func (f *intentsFixture) service() *intents.Service {
	return intents.NewService(intents.NewRepository(f.pool),
		portfolio.NewService(portfolio.NewRepository(f.pool), slog.Default()),
		f.riskService, slog.Default())
}

func (f *intentsFixture) limit(user portfolio.UUID, kind risk.Kind, threshold string) {
	f.t.Helper()
	if _, err := f.riskService.State(f.ctx, user.String(), kind, threshold); err != nil {
		f.t.Fatalf("state %s: %v", kind, err)
	}
}

func (f *intentsFixture) consider(user portfolio.UUID, ticker string, direction intents.Direction,
	quantity, price string) intents.Intent {
	f.t.Helper()
	intent, err := f.service().Record(f.ctx, user.String(), intents.RecordRequest{
		InstrumentID: f.instruments[ticker], Direction: direction,
		Quantity: quantity, Price: price, Costs: "39",
	})
	if err != nil {
		f.t.Fatalf("record an intent for %s: %v", ticker, err)
	}
	return intent
}

func (f *intentsFixture) report(user portfolio.UUID, includeSettled bool) intents.Report {
	f.t.Helper()
	report, err := f.service().Report(f.ctx, user.String(), includeSettled)
	if err != nil {
		f.t.Fatalf("read the intents: %v", err)
	}
	return report
}

func (f *intentsFixture) only(report intents.Report) intents.Intent {
	f.t.Helper()
	if len(report.Intents) != 1 {
		f.t.Fatalf("%d intents, want 1: %+v", len(report.Intents), report.Intents)
	}
	return report.Intents[0]
}

func asFloat(t *testing.T, value string) float64 {
	t.Helper()
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		t.Fatalf("parse %s: %v", value, err)
	}
	return parsed
}
