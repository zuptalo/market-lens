package risk_test

import (
	"errors"
	"testing"

	"market-lens/server/internal/portfolio"
	"market-lens/server/internal/risk"
)

// A limit is somebody's own rule about their own money. These are private in the way feature 022's
// holdings are private, and the owner role grants nothing: ownership here is not administrative.

func TestAPersonSeesOnlyTheirOwnLimits(t *testing.T) {
	f := newRiskFixture(t)
	f.state(aliceID, risk.KindInstrumentShare, "0.25")
	f.state(bobID, risk.KindSectorShare, "0.60")

	alice := f.report(aliceID)
	bob := f.report(bobID)

	if len(alice.Limits) != 1 || alice.Limits[0].Kind != risk.KindInstrumentShare {
		t.Fatalf("alice's limits are %+v", alice.Limits)
	}
	if len(bob.Limits) != 1 || bob.Limits[0].Kind != risk.KindSectorShare {
		t.Fatalf("bob's limits are %+v", bob.Limits)
	}

	// Alice is the owner. That grants her nothing over bob's rules.
	for _, evaluation := range alice.Limits {
		if evaluation.Kind == risk.KindSectorShare {
			t.Errorf("the owner role reached another person's limit")
		}
	}

	// Removing somebody else's limit answers exactly as removing one that does not exist.
	if err := f.service().Remove(f.ctx, aliceID.String(), risk.KindSectorShare); !errors.Is(err, risk.ErrNotFound) {
		t.Errorf("removing bob's limit as alice returned %v, want not found", err)
	}
	// And bob still has it.
	if len(f.report(bobID).Limits) != 1 {
		t.Errorf("bob's limit was removed by somebody else")
	}

	// The stored events are scoped to one person each.
	if mine := f.count(`SELECT count(*) FROM client_events
		WHERE event_type = $1 AND scope = 'user' AND subject_user_id = $2`,
		risk.EventChanged, aliceID.String()); mine == 0 {
		t.Errorf("alice's limit published no event scoped to her")
	}
	if leaked := f.count(`SELECT count(*) FROM client_events
		WHERE event_type = $1 AND (scope <> 'user' OR subject_user_id IS NULL)`,
		risk.EventChanged); leaked != 0 {
		t.Errorf("%d risk events are not scoped to one person", leaked)
	}

	// A caller with no identity reaches nothing.
	if _, err := f.service().Report(f.ctx, ""); err == nil {
		t.Errorf("an unauthenticated caller read a risk report")
	}
}

func TestStatingALimitReplacesTheOneOfThatKind(t *testing.T) {
	f := newRiskFixture(t)
	f.state(aliceID, risk.KindInstrumentShare, "0.25")
	f.state(aliceID, risk.KindInstrumentShare, "0.40")

	report := f.report(aliceID)
	if len(report.Limits) != 1 {
		t.Fatalf("two thresholds for one kind produced %d limits", len(report.Limits))
	}
	if report.Limits[0].Threshold != "0.400000000000" {
		t.Errorf("the threshold is %s, want the one stated second", report.Limits[0].Threshold)
	}
}

func TestAThresholdThatCannotMeanAnythingIsRefused(t *testing.T) {
	f := newRiskFixture(t)
	service := f.service()

	for _, attempt := range []struct {
		kind      risk.Kind
		threshold string
		why       string
	}{
		{risk.KindInstrumentShare, "1.5", "a share above all of it"},
		{risk.KindSectorShare, "0", "a share of none of it"},
		{risk.KindMarketShare, "-0.2", "a negative share"},
		{risk.KindHoldingCount, "7.5", "a fraction of a holding"},
		{risk.KindHoldingCount, "0", "no holdings at all"},
		{risk.KindInstrumentShare, "a quarter", "words"},
		{risk.Kind("portfolio_drawdown"), "0.2", "a kind the product cannot measure"},
	} {
		_, err := service.State(f.ctx, aliceID.String(), attempt.kind, attempt.threshold)
		var refusal risk.Refusal
		if !errors.As(err, &refusal) {
			t.Errorf("%s (%s = %s) returned %v, want a refusal",
				attempt.why, attempt.kind, attempt.threshold, err)
			continue
		}
		if refusal.Message == "" {
			t.Errorf("%s was refused with nothing a person could act on", attempt.why)
		}
	}

	// And nothing was stored by any of them.
	if len(f.report(aliceID).Limits) != 0 {
		t.Errorf("a refused threshold was stored anyway")
	}
}

// TestTheProductSuppliesNoLimits. A default threshold is the product telling somebody what is
// prudent, which is the line this whole feature is built on staying the right side of.
func TestTheProductSuppliesNoLimits(t *testing.T) {
	f := newRiskFixture(t)
	f.record(aliceID, betaTicker, portfolio.DirectionBuy, "100", "100.00", "0", f.session("XHEL", 5))

	report := f.report(aliceID)
	if len(report.Limits) != 0 {
		t.Errorf("a person who stated nothing has %d limits: %+v", len(report.Limits), report.Limits)
	}
	if !report.LimitsAreYourOwn {
		t.Errorf("the report does not state that the limits are the person's own")
	}
	// Not even after holding something, recording a trade, or reading the report twice.
	if stored := f.count(`SELECT count(*) FROM risk_limits`); stored != 0 {
		t.Errorf("%d limits exist that nobody stated", stored)
	}
}
