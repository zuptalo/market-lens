package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Notifications delivers what has become due, on its own short interval.
//
// It exists because delivery used to be driven only by the market-data import, which runs once a
// day — and two of the notification feature's stated behaviours quietly did not hold as a result.
// A message held for somebody's quiet hours was released at the right moment and then sat until the
// following evening, and a retry backoff measured in minutes waited a day between attempts. Both
// looked correct in their own tests, which move the clock by hand and call the pass directly: what
// was never asserted was how often anything calls it.
//
// The other passes hang off the import for a good reason — they have nothing to do until new prices
// exist. This one is different: quiet hours end and backoffs expire on their own schedule, and
// neither has anything to do with market data.
type Notifications struct {
	deliverer NotificationDeliverer
	interval  time.Duration
	logger    *slog.Logger
}

// DefaultDeliveryInterval is how often delivery is attempted.
//
// A minute: quiet hours end on a minute boundary and the shortest retry backoff is three minutes,
// so anything finer buys nothing, and anything much coarser starts to show. The query reads only
// what is due, so an idle minute costs one indexed lookup.
const DefaultDeliveryInterval = time.Minute

// MaximumDeliveryInterval guards against a configured value that would look like a working setting
// and behave like silence.
const MaximumDeliveryInterval = time.Hour

func NewNotifications(deliverer NotificationDeliverer, interval time.Duration,
	logger *slog.Logger) (*Notifications, error) {
	if deliverer == nil {
		return nil, errors.New("a notification ticker needs something to deliver")
	}
	if interval <= 0 || interval > MaximumDeliveryInterval {
		return nil, fmt.Errorf("a delivery interval of %s is not usable; it must be between 1ns and %s",
			interval, MaximumDeliveryInterval)
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Notifications{deliverer: deliverer, interval: interval, logger: logger}, nil
}

// Run delivers on the interval until its context is cancelled.
//
// A failing pass is logged and the loop continues. Stopping on the first failure would mean one
// unreachable mail server silenced every future notification until somebody restarted the process,
// which is a far worse outcome than a repeated line in the log.
func (s *Notifications) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			s.deliverOnce(ctx)
		}
	}
}

func (s *Notifications) deliverOnce(ctx context.Context) {
	sent, err := s.deliverer.DeliverDue(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		s.logger.Error("a notification delivery pass failed", "error", err)
		return
	}
	if sent > 0 {
		s.logger.Info("notifications delivered", "sent", sent)
	}
}
