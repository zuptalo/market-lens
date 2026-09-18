package push

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testSubscription(t *testing.T, endpoint string) Subscription {
	t.Helper()
	return Subscription{
		Endpoint: endpoint,
		P256DH:   decode(t, rfcSubscriptionPub),
		Auth:     decode(t, rfcSubscriptionAuth),
	}
}

func TestAPushCarriesTheAssertionAndTheEncryptedBody(t *testing.T) {
	var got *http.Request
	var body []byte
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		body = make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer service.Close()

	pair, err := GenerateKeyPair(rand.Reader, "mailto:owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	sender := NewSender(pair, service.Client())

	if err := sender.Send(context.Background(), testSubscription(t, service.URL+"/p/abc"),
		[]byte(`{"kind":"paper_fill"}`)); err != nil {
		t.Fatalf("send: %v", err)
	}

	if got.Method != http.MethodPost {
		t.Errorf("a push was sent as %s", got.Method)
	}
	if got.Header.Get("Authorization") == "" {
		t.Errorf("the push carried no assertion")
	}
	if encoding := got.Header.Get("Content-Encoding"); encoding != "aes128gcm" {
		t.Errorf("the content encoding is %q", encoding)
	}
	// Urgency and TTL are what stop a service holding a message forever, or waking a device for
	// something that has stopped mattering.
	if got.Header.Get("TTL") == "" {
		t.Errorf("the push carried no TTL, so a service may hold it indefinitely")
	}
	if len(body) == 0 {
		t.Errorf("the push carried no body")
	}
	// The payload is encrypted: the plaintext must not be on the wire.
	if string(body) == `{"kind":"paper_fill"}` {
		t.Errorf("the payload was sent in the clear")
	}
}

// A browser that forgot its subscription is not an error to report — it is a subscription to
// delete. Reporting it would leave a dead row that fails every pass forever.
func TestAGoneSubscriptionSaysSoRatherThanFailing(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusGone} {
		service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))
		pair, _ := GenerateKeyPair(rand.Reader, "mailto:owner@example.com")
		sender := NewSender(pair, service.Client())

		err := sender.Send(context.Background(), testSubscription(t, service.URL+"/p/abc"), []byte("{}"))
		if !errors.Is(err, ErrSubscriptionGone) {
			t.Errorf("status %d returned %v, want a gone subscription", status, err)
		}
		service.Close()
	}
}

// Anything else is worth retrying, and worth keeping the reason for.
func TestAServiceFailureIsReportedAndRetryable(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer service.Close()

	pair, _ := GenerateKeyPair(rand.Reader, "mailto:owner@example.com")
	sender := NewSender(pair, service.Client())

	err := sender.Send(context.Background(), testSubscription(t, service.URL+"/p/abc"), []byte("{}"))
	if err == nil {
		t.Fatal("a failing push service reported success")
	}
	if errors.Is(err, ErrSubscriptionGone) {
		t.Errorf("a temporary failure deleted a subscription")
	}
	// The reason is readable, because a person is shown why their alert did not arrive.
	if err.Error() == "" {
		t.Errorf("the failure carries no reason")
	}
}
