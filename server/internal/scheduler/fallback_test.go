package scheduler

import (
	"context"
	"errors"
	"testing"

	"market-lens/server/internal/instruments"
	"market-lens/server/internal/marketdata"
)

type fallbackStub struct {
	covered   bool
	coverErr  error
	coverRun  marketdata.ImportRun
	asked     []marketdata.ImportRun
	options   []marketdata.CoverOptions
	observed  int
	observeOK error
	order     *[]string
}

func (s *fallbackStub) Cover(_ context.Context, primary marketdata.ImportRun, options marketdata.CoverOptions) (marketdata.ImportRun, bool, error) {
	s.asked = append(s.asked, primary)
	s.options = append(s.options, options)
	if s.order != nil {
		*s.order = append(*s.order, "cover")
	}
	return s.coverRun, s.covered, s.coverErr
}

func (s *fallbackStub) Observe(context.Context) (marketdata.FallbackState, bool, error) {
	s.observed++
	if s.order != nil {
		*s.order = append(*s.order, "observe")
	}
	return marketdata.FallbackState{}, false, s.observeOK
}

type orderedFills struct{ order *[]string }

func (o orderedFills) FillPending(context.Context) (int, error) {
	*o.order = append(*o.order, "fills")
	return 0, nil
}

// Every night's run is offered to the fallback, which decides for itself whether there is anything
// to cover; the fallback's bars go through the same incremental feature pass as the primary's.
func TestTheNightsRunIsOfferedToTheFallback(t *testing.T) {
	scheduler := schedulerForTest(t)
	fallbackRun, _ := instruments.NewUUID()
	stub := &fallbackStub{covered: true, coverRun: marketdata.ImportRun{ID: fallbackRun}}
	computer := &recordingComputer{}
	scheduler.Fallback, scheduler.Features = stub, computer

	if err := scheduler.RunDue(context.Background(), dueTime()); err != nil {
		t.Fatal(err)
	}
	if len(stub.asked) != 1 {
		t.Fatalf("the fallback was offered %d runs", len(stub.asked))
	}
	if stub.options[0].AppVersion != "test" || stub.options[0].Workers < 1 || stub.options[0].Workers > 2 {
		t.Errorf("cover options = %#v", stub.options[0])
	}
	if len(computer.runs) != 2 || computer.runs[1] != fallbackRun {
		t.Errorf("features were computed for %v, want the primary run then the fallback run", computer.runs)
	}
}

// Nothing covered, nothing extra computed.
func TestAnUncoveredNightComputesNothingExtra(t *testing.T) {
	scheduler := schedulerForTest(t)
	stub := &fallbackStub{}
	computer := &recordingComputer{}
	scheduler.Fallback, scheduler.Features = stub, computer

	if err := scheduler.RunDue(context.Background(), dueTime()); err != nil {
		t.Fatal(err)
	}
	if len(computer.runs) != 1 {
		t.Errorf("features were computed %d times", len(computer.runs))
	}
}

// The state is observed every night, after the fallback and before the paper fills, so a
// recovering primary ends the fallback on the night it replaces the last fallback bar.
func TestTheFallbackIsObservedBeforeFills(t *testing.T) {
	scheduler := schedulerForTest(t)
	var order []string
	stub := &fallbackStub{order: &order}
	scheduler.Fallback = stub
	scheduler.PaperFills = orderedFills{order: &order}

	if err := scheduler.RunDue(context.Background(), dueTime()); err != nil {
		t.Fatal(err)
	}
	if len(order) != 3 || order[0] != "cover" || order[1] != "observe" || order[2] != "fills" {
		t.Errorf("order = %v", order)
	}
}

// The fallback is a courtesy: its failure never becomes the night's failure.
func TestAFailingFallbackDoesNotFailTheNight(t *testing.T) {
	scheduler := schedulerForTest(t)
	stub := &fallbackStub{coverErr: errors.New("unavailable"), observeOK: errors.New("no")}
	scheduler.Fallback = stub
	fills := &paperFillsStub{}
	scheduler.PaperFills = fills

	if err := scheduler.RunDue(context.Background(), dueTime()); err != nil {
		t.Fatalf("a failing fallback failed the night: %v", err)
	}
	if fills.calls != 1 {
		t.Error("a failing fallback stopped the rest of the night's work")
	}
}
