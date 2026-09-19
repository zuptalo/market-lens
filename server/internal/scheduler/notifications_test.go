package scheduler

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type tickingDeliverer struct {
	calls atomic.Int64
	err   error
}

func (d *tickingDeliverer) DeliverDue(context.Context) (int, error) {
	d.calls.Add(1)
	return 0, d.err
}

// waitFor polls until the condition holds, so a slow machine fails late rather than falsely.
func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestDeliveryDoesNotWaitForTheNightlyImport is the property this exists for.
//
// Delivery used to be driven only by the market-data import, which runs once a day. Two of this
// feature's stated behaviours quietly did not hold as a result: a notification held for quiet hours
// was released at the right moment and then delivered the *following evening*, and a retry backoff
// measured in minutes waited a day between attempts.
func TestDeliveryDoesNotWaitForTheNightlyImport(t *testing.T) {
	deliverer := &tickingDeliverer{}
	ticker, err := NewNotifications(deliverer, 5*time.Millisecond, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() { _ = ticker.Run(ctx) }()

	// Several passes, with no import anywhere near it.
	waitFor(t, "delivery to run repeatedly", func() bool { return deliverer.calls.Load() >= 3 })
}

// A mail server that is down must not take the loop down with it, or the first outage would stop
// every future notification until somebody restarted the process.
func TestAFailedPassDoesNotStopTheTicker(t *testing.T) {
	deliverer := &tickingDeliverer{err: errors.New("the mail server is unreachable")}
	ticker, err := NewNotifications(deliverer, 5*time.Millisecond, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() { _ = ticker.Run(ctx) }()

	waitFor(t, "delivery to keep trying after a failure", func() bool {
		return deliverer.calls.Load() >= 3
	})
}

func TestTheTickerStopsWhenAsked(t *testing.T) {
	deliverer := &tickingDeliverer{}
	ticker, err := NewNotifications(deliverer, time.Millisecond, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ticker.Run(ctx) }()

	waitFor(t, "the first pass", func() bool { return deliverer.calls.Load() >= 1 })
	stop()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("stopping returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the ticker did not stop when its context was cancelled")
	}
}

// An interval nobody meant is refused rather than turned into a busy loop or an hour's silence.
func TestAnUnusableIntervalIsRefused(t *testing.T) {
	for _, interval := range []time.Duration{0, -time.Second, 25 * time.Hour} {
		if _, err := NewNotifications(&tickingDeliverer{}, interval, nil); err == nil {
			t.Errorf("an interval of %s was accepted", interval)
		}
	}
	if _, err := NewNotifications(nil, time.Minute, nil); err == nil {
		t.Errorf("a ticker with nothing to deliver was accepted")
	}
}
