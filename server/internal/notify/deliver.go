package notify

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"market-lens/server/internal/notify/push"

	"github.com/jackc/pgx/v5"
)

// due is one notification ready to be sent, with everything sending it needs.
type due struct {
	id         string
	userID     string
	email      string
	kind       Kind
	channel    Channel
	count      int
	detail     map[string]string
	attempts   int
	subjectKey string
}

// DeliverDue sends everything that is due, for everybody.
//
// Two properties matter more than throughput. **At most once per channel** comes from the row's
// state machine, not from this pass being careful: claiming is a conditional update, and losing the
// race means somebody else has it. And **nothing reaches somebody who did not ask** — which is
// already true before this runs, because a notification is only written for a person who consented.
func (s *Service) DeliverDue(ctx context.Context) (int, error) {
	if s == nil || s.repository == nil {
		return 0, errors.New("notification service is not configured")
	}
	pending, err := s.repository.due(ctx, time.Now().UTC())
	if err != nil {
		return 0, err
	}

	delivered := 0
	for _, notification := range pending {
		// Claim it. Zero rows means another pass took it between the read and now, which is the
		// whole point of doing it this way rather than trusting a single worker.
		claimed, err := s.repository.claim(ctx, notification.id)
		if err != nil {
			return delivered, err
		}
		if !claimed {
			continue
		}
		if err := s.sendOne(ctx, notification); err != nil {
			s.repository.recordFailure(ctx, notification, err, s.logger)
			continue
		}
		if err := s.repository.markSent(ctx, notification.id); err != nil {
			return delivered, err
		}
		delivered++
	}
	return delivered, nil
}

func (s *Service) sendOne(ctx context.Context, notification due) error {
	switch notification.channel {
	case ChannelEmail:
		if s.mailer == nil {
			return errors.New("no mail server is configured")
		}
		token, err := s.repository.unsubscribeToken(ctx, notification.userID,
			notification.kind, notification.channel)
		if err != nil {
			return err
		}
		message, err := buildEmail(notification.email, notification.kind, notification.count,
			notification.detail, s.baseURL, token)
		if err != nil {
			return err
		}
		return s.mailer.Send(ctx, message)

	case ChannelWebPush:
		if s.pusher == nil {
			return errors.New("push is not configured")
		}
		payload, err := buildPushPayload(notification.kind, notification.count)
		if err != nil {
			return err
		}
		subscriptions, err := s.repository.subscriptionsFor(ctx, notification.userID)
		if err != nil {
			return err
		}
		if len(subscriptions) == 0 {
			// Asked for push and has no device. Not a failure to retry: there is nothing to retry
			// against, and it stays true until they subscribe one.
			return nil
		}
		var lastErr error
		reached := 0
		for _, subscription := range subscriptions {
			err := s.pusher.Send(ctx, subscription.endpoint, payload)
			switch {
			case err == nil:
				reached++
				s.repository.touchSubscription(ctx, subscription.id)
			case errors.Is(err, push.ErrSubscriptionGone):
				// A browser that forgot is telling the truth. Delete it rather than keep something
				// that fails every pass forever.
				s.repository.deleteSubscription(ctx, subscription.id)
			default:
				lastErr = err
			}
		}
		if reached == 0 && lastErr != nil {
			return lastErr
		}
		return nil
	}
	return fmt.Errorf("unknown channel %q", notification.channel)
}

// backoff is how long to wait before trying again: a widening gap, so a mail server restarting is
// ridden out without hammering it.
func backoff(attempts int) time.Duration {
	minutes := math.Pow(3, float64(attempts))
	if minutes > 240 {
		minutes = 240
	}
	return time.Duration(minutes) * time.Minute
}

func (r *Repository) due(ctx context.Context, now time.Time) ([]due, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT n.id::text, n.user_id::text, u.email, n.kind, n.channel,
			n.count, n.detail::text, n.attempts, n.subject_key
		FROM notifications n
		JOIN users u ON u.id = n.user_id
		WHERE n.state IN ('pending', 'failed') AND n.available_at <= $1
		  -- A deactivated account is not sent to. Its pending notifications simply stop being due.
		  AND u.status = 'active'
		ORDER BY n.available_at, n.created_at
		LIMIT 500`, now)
	if err != nil {
		return nil, fmt.Errorf("read what is due: %w", err)
	}
	defer rows.Close()

	var pending []due
	for rows.Next() {
		var notification due
		var detail string
		if err := rows.Scan(&notification.id, &notification.userID, &notification.email,
			&notification.kind, &notification.channel, &notification.count, &detail,
			&notification.attempts, &notification.subjectKey); err != nil {
			return nil, fmt.Errorf("scan a notification: %w", err)
		}
		if err := json.Unmarshal([]byte(detail), &notification.detail); err != nil {
			notification.detail = map[string]string{}
		}
		pending = append(pending, notification)
	}
	return pending, rows.Err()
}

// claim moves one notification to sending, and says whether this caller got it.
//
// The conditional update is what makes at-most-once true under two passes at once. Without it the
// property would depend on there being exactly one pass, which is an assumption nobody can enforce
// from inside the process.
func (r *Repository) claim(ctx context.Context, id string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE notifications SET state = 'sending', attempts = attempts + 1
		WHERE id = $1 AND state IN ('pending', 'failed')`, id)
	if err != nil {
		return false, fmt.Errorf("claim the notification: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *Repository) markSent(ctx context.Context, id string) error {
	if _, err := r.pool.Exec(ctx,
		`UPDATE notifications SET state = 'sent', sent_at = now(), last_error = NULL
		 WHERE id = $1`, id); err != nil {
		return fmt.Errorf("record the delivery: %w", err)
	}
	return nil
}

// recordFailure keeps the reason, because a person is shown why their alert did not arrive.
func (r *Repository) recordFailure(ctx context.Context, notification due, cause error, logger logger) {
	attempts := notification.attempts + 1
	state := string(StateFailed)
	availableAt := time.Now().UTC().Add(backoff(attempts))
	if attempts >= MaximumAttempts {
		state = string(StateAbandoned)
	}
	if _, err := r.pool.Exec(ctx, `UPDATE notifications
		SET state = $2, last_error = $3, available_at = $4 WHERE id = $1`,
		notification.id, state, cause.Error(), availableAt); err != nil {
		logger.Error("a failed notification could not be recorded", "error", err)
	}
}

type logger interface {
	Error(message string, args ...any)
}

type storedSubscription struct {
	id       string
	endpoint push.Subscription
}

func (r *Repository) subscriptionsFor(ctx context.Context, userID string) ([]storedSubscription, error) {
	rows, err := r.pool.Query(ctx, `SELECT id::text, endpoint, p256dh, auth
		FROM push_subscriptions WHERE user_id = $1`, userID)
	if err != nil {
		return nil, fmt.Errorf("read the devices: %w", err)
	}
	defer rows.Close()

	var subscriptions []storedSubscription
	for rows.Next() {
		var found storedSubscription
		if err := rows.Scan(&found.id, &found.endpoint.Endpoint,
			&found.endpoint.P256DH, &found.endpoint.Auth); err != nil {
			return nil, fmt.Errorf("scan a device: %w", err)
		}
		subscriptions = append(subscriptions, found)
	}
	return subscriptions, rows.Err()
}

func (r *Repository) touchSubscription(ctx context.Context, id string) {
	_, _ = r.pool.Exec(ctx, `UPDATE push_subscriptions SET last_used_at = now() WHERE id = $1`, id)
}

func (r *Repository) deleteSubscription(ctx context.Context, id string) {
	_, _ = r.pool.Exec(ctx, `DELETE FROM push_subscriptions WHERE id = $1`, id)
}

// History is what was sent, when, and what failed — without the payload, which is not kept beyond
// what was needed to send it.
func (s *Service) History(ctx context.Context, userID string) ([]Record, error) {
	if err := s.ready(userID); err != nil {
		return nil, err
	}
	rows, err := s.repository.pool.Query(ctx, `SELECT kind, channel, state, count, attempts,
			last_error, created_at, sent_at
		FROM notifications WHERE user_id = $1 ORDER BY created_at DESC LIMIT 100`, userID)
	if err != nil {
		return nil, fmt.Errorf("read the history: %w", err)
	}
	defer rows.Close()

	records := make([]Record, 0, 16)
	for rows.Next() {
		var record Record
		if err := rows.Scan(&record.Kind, &record.Channel, &record.State, &record.Count,
			&record.Attempts, &record.LastError, &record.CreatedAt, &record.SentAt); err != nil {
			return nil, fmt.Errorf("scan a record: %w", err)
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

var _ = base64.RawURLEncoding
var _ = pgx.ErrNoRows
