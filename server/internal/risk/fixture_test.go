package risk_test

import (
	"context"
	"log/slog"
	"strconv"
	"testing"

	"market-lens/server/internal/portfolio"
	"market-lens/server/internal/risk"
	"market-lens/server/internal/testdb"
)

// The risk fixture builds on the portfolio one: limits are measured against holdings, so a fixture
// that invented its own holdings would be measuring something the product never produces.
//
// Two people, always. These are the first rules anybody writes down in this product and they are
// private; a fixture with one user cannot fail an isolation test.

type riskFixture struct {
	*portfolioFixture
}

func newRiskFixture(t *testing.T) *riskFixture {
	t.Helper()
	return &riskFixture{portfolioFixture: newPortfolioFixture(t)}
}

func (f *riskFixture) service() *risk.Service {
	return risk.NewService(risk.NewRepository(f.pool),
		portfolio.NewService(portfolio.NewRepository(f.pool), slog.Default()), slog.Default())
}

// state writes one limit the ordinary way, so a test says what rule somebody wrote rather than how
// it was stored.
func (f *riskFixture) state(user portfolio.UUID, kind risk.Kind, threshold string) {
	f.t.Helper()
	if _, err := f.service().State(f.ctx, user.String(), kind, threshold); err != nil {
		f.t.Fatalf("state %s = %s: %v", kind, threshold, err)
	}
}

func (f *riskFixture) report(user portfolio.UUID) risk.Report {
	f.t.Helper()
	report, err := f.service().Report(f.ctx, user.String())
	if err != nil {
		f.t.Fatalf("read the report: %v", err)
	}
	return report
}

// evaluation picks one kind out of a report, so an assertion names the rule it is about.
func (f *riskFixture) evaluation(report risk.Report, kind risk.Kind) risk.Evaluation {
	f.t.Helper()
	for _, evaluation := range report.Limits {
		if evaluation.Kind == kind {
			return evaluation
		}
	}
	f.t.Fatalf("no %s evaluation in %+v", kind, report.Limits)
	return risk.Evaluation{}
}

var _ = context.Background
var _ = testdb.Open

func parseFloat(value string) (float64, error) {
	return strconv.ParseFloat(value, 64)
}
