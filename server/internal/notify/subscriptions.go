package notify

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"market-lens/server/internal/instruments"
	"market-lens/server/internal/notify/push"

	"github.com/jackc/pgx/v5"
)

// The devices a person subscribed, and the instance key their browsers subscribed against.

// Subscribe records one device. Subscribing the same browser twice replaces rather than duplicates:
// a browser that re-subscribes has produced a new key pair for the same endpoint, and keeping the
// old row would mean sending one message the browser can no longer read.
func (s *Service) Subscribe(ctx context.Context, userID string, request SubscribeRequest) (Subscription, error) {
	if err := s.ready(userID); err != nil {
		return Subscription{}, err
	}
	invalid := func(reason string) error {
		return Refusal{Code: RefusalInvalidSubscribe, Message: reason}
	}
	endpoint := strings.TrimSpace(request.Endpoint)
	if endpoint == "" || !strings.HasPrefix(endpoint, "https://") {
		return Subscription{}, invalid("A push subscription needs the secure endpoint the browser produced.")
	}
	label := strings.TrimSpace(request.Label)
	if label == "" || len(label) > 120 {
		return Subscription{}, invalid("Give this device a name you will recognise later.")
	}
	p256dh, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(request.P256DH, "="))
	if err != nil || len(p256dh) != 65 {
		return Subscription{}, invalid("The browser's key was not readable.")
	}
	auth, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(request.Auth, "="))
	if err != nil || len(auth) != 16 {
		return Subscription{}, invalid("The browser's secret was not readable.")
	}
	// Refuse here rather than at send time: a key that is not a point fails silently later, with a
	// subscription that looks fine and never receives anything.
	if _, err := push.Encrypt([]byte("{}"), push.Subscription{
		Endpoint: endpoint, P256DH: p256dh, Auth: auth,
	}); err != nil {
		return Subscription{}, invalid("The browser's key is not usable for push.")
	}

	id, err := instruments.NewUUID()
	if err != nil {
		return Subscription{}, err
	}
	if err := s.repository.subscribe(ctx, id.String(), userID, endpoint, p256dh, auth, label); err != nil {
		return Subscription{}, err
	}
	subscriptions, err := s.Subscriptions(ctx, userID)
	if err != nil {
		return Subscription{}, err
	}
	for _, subscription := range subscriptions {
		if subscription.Label == label {
			return subscription, nil
		}
	}
	return Subscription{}, ErrNotFound
}

// Subscriptions is the devices this person subscribed. Endpoints and keys are not returned: they
// are what is needed to send to a device, not something a page has any use for.
func (s *Service) Subscriptions(ctx context.Context, userID string) ([]Subscription, error) {
	if err := s.ready(userID); err != nil {
		return nil, err
	}
	rows, err := s.repository.pool.Query(ctx, `SELECT id::text, label, created_at, last_used_at
		FROM push_subscriptions WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("read the devices: %w", err)
	}
	defer rows.Close()

	subscriptions := make([]Subscription, 0, 4)
	for rows.Next() {
		var subscription Subscription
		if err := rows.Scan(&subscription.ID, &subscription.Label,
			&subscription.CreatedAt, &subscription.LastUsedAt); err != nil {
			return nil, fmt.Errorf("scan a device: %w", err)
		}
		subscriptions = append(subscriptions, subscription)
	}
	return subscriptions, rows.Err()
}

// Revoke stops sending to one device, from any signed-in device of the same person. Deleted rather
// than muted: a muted subscription is one somebody has to trust stays muted.
func (s *Service) Revoke(ctx context.Context, userID, subscriptionID string) error {
	if err := s.ready(userID); err != nil {
		return err
	}
	tag, err := s.repository.pool.Exec(ctx,
		`DELETE FROM push_subscriptions WHERE id = $1 AND user_id = $2`, subscriptionID, userID)
	if err != nil {
		return fmt.Errorf("revoke the device: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Somebody else's device and one that never existed answer the same way.
		return ErrNotFound
	}
	return nil
}

func (r *Repository) subscribe(ctx context.Context, id, userID, endpoint string,
	p256dh, auth []byte, label string) error {
	if err := r.ready(); err != nil {
		return err
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO push_subscriptions
		(id, user_id, endpoint, p256dh, auth, label)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id, endpoint) DO UPDATE
		SET p256dh = EXCLUDED.p256dh, auth = EXCLUDED.auth, label = EXCLUDED.label`,
		id, userID, endpoint, p256dh, auth, label)
	if err != nil {
		return fmt.Errorf("record the device: %w", err)
	}
	return nil
}

// EnsurePushKey provisions the instance's VAPID pair if it has none, and returns it either way.
//
// The same shape as the instance signing key (migration 0011): INSERT ... ON CONFLICT DO NOTHING
// followed by an unconditional SELECT, so two instances starting at once converge on one key
// without an advisory lock — the loser of the race adopts the winner's.
//
// It is generated here rather than configured because losing it is invisible: every subscription in
// existence was made against this public key, and a new one means pushes silently stop arriving.
func (r *Repository) EnsurePushKey(ctx context.Context, subject string) (push.KeyPair, error) {
	if err := r.ready(); err != nil {
		return push.KeyPair{}, err
	}
	generated, err := push.GenerateKeyPair(cryptoRandom, subject)
	if err != nil {
		return push.KeyPair{}, err
	}
	id, err := instruments.NewUUID()
	if err != nil {
		return push.KeyPair{}, err
	}
	if _, err := r.pool.Exec(ctx, `INSERT INTO instance_push_key
		(id, private_key, public_key, subject) VALUES ($1, $2, $3, $4)
		ON CONFLICT DO NOTHING`,
		id.String(), generated.Private, generated.Public, subject); err != nil {
		return push.KeyPair{}, fmt.Errorf("provision the push key: %w", err)
	}

	var stored push.KeyPair
	err = r.pool.QueryRow(ctx,
		`SELECT private_key, public_key, subject FROM instance_push_key`).
		Scan(&stored.Private, &stored.Public, &stored.Subject)
	if errors.Is(err, pgx.ErrNoRows) {
		return push.KeyPair{}, errors.New("no push key exists after provisioning one")
	}
	if err != nil {
		return push.KeyPair{}, fmt.Errorf("read the push key: %w", err)
	}
	return stored, nil
}

// PublicPushKey is what a browser needs to subscribe. Only the public half ever leaves the server.
func (s *Service) PublicPushKey(ctx context.Context, userID string) (string, error) {
	if err := s.ready(userID); err != nil {
		return "", err
	}
	var public []byte
	err := s.repository.pool.QueryRow(ctx, `SELECT public_key FROM instance_push_key`).Scan(&public)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read the push key: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(public), nil
}
