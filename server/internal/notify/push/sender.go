package push

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// ErrSubscriptionGone means the browser no longer holds this subscription.
//
// Not a failure to report and retry: a browser that forgot is telling the truth, and the right
// answer is to delete the row. Keeping it would leave something that fails every pass forever.
var ErrSubscriptionGone = errors.New("the browser no longer holds this subscription")

// TimeToLive is how long a push service may hold a message for a device that is offline.
//
// Four hours: long enough to survive a phone being asleep overnight in most timezones, short enough
// that nothing arrives about something that has stopped mattering.
const TimeToLive = 4 * time.Hour

// Sender posts encrypted messages to browsers' own push services.
type Sender struct {
	keys   KeyPair
	client *http.Client
}

func NewSender(keys KeyPair, client *http.Client) *Sender {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Sender{keys: keys, client: client}
}

// Send encrypts the payload for this subscription and posts it.
//
// The payload is encrypted to the subscription before it leaves, because a push service holds the
// message until the browser collects it. The threat there is not interception, it is accumulation:
// a service should learn nothing from what it is asked to store.
func (s *Sender) Send(ctx context.Context, subscription Subscription, payload []byte) error {
	if s == nil || s.client == nil {
		return errors.New("the push sender is not configured")
	}
	body, err := Encrypt(payload, subscription)
	if err != nil {
		return fmt.Errorf("encrypt for the subscription: %w", err)
	}
	authorization, err := s.keys.AuthorizationHeader(subscription.Endpoint, time.Now())
	if err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, subscription.Endpoint,
		bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("prepare the push: %w", err)
	}
	request.Header.Set("Authorization", authorization)
	request.Header.Set("Content-Encoding", "aes128gcm")
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("TTL", strconv.Itoa(int(TimeToLive.Seconds())))
	// Normal urgency: this is worth waking a device for, and is not an emergency.
	request.Header.Set("Urgency", "normal")

	response, err := s.client.Do(request)
	if err != nil {
		return fmt.Errorf("reach the push service: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	switch {
	case response.StatusCode == http.StatusNotFound, response.StatusCode == http.StatusGone:
		return ErrSubscriptionGone
	case response.StatusCode >= 200 && response.StatusCode < 300:
		return nil
	default:
		// The status and nothing else. A push service's body can name internal state, and this
		// reason is shown to the person whose alert did not arrive.
		return fmt.Errorf("the push service answered %d", response.StatusCode)
	}
}
