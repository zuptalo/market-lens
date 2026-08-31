package mailtest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type testMessage struct {
	Subject string
	Body    string
}

func TestCaptureStoresMessagesAndReturnsDefensiveSnapshots(t *testing.T) {
	sender := NewCapture[testMessage]()
	want := testMessage{Subject: "Sign in", Body: "Your code is 012345"}
	if err := sender.Send(context.Background(), want); err != nil {
		t.Fatal(err)
	}

	first := sender.Messages()
	if len(first) != 1 || first[0] != want {
		t.Fatalf("captured messages = %#v, want %#v", first, []testMessage{want})
	}
	first[0].Body = "changed by test"
	if got := sender.Messages()[0]; got != want {
		t.Fatalf("capture exposed mutable storage: %#v", got)
	}
}

func TestCaptureHonorsCanceledContext(t *testing.T) {
	sender := NewCapture[testMessage]()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := sender.Send(ctx, testMessage{Subject: "must not send"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Send() error = %v, want context.Canceled", err)
	}
	if got := sender.Messages(); len(got) != 0 {
		t.Fatalf("canceled message was captured: %#v", got)
	}
}

func TestFailureSenderReturnsConfiguredSafeFailureAndCountsAttempts(t *testing.T) {
	want := errors.New("transactional email unavailable")
	sender := NewFailure[testMessage](want)

	if err := sender.Send(context.Background(), testMessage{Body: "sensitive content"}); !errors.Is(err, want) {
		t.Fatalf("Send() error = %v, want %v", err, want)
	}
	if got := sender.Attempts(); got != 1 {
		t.Fatalf("Attempts() = %d, want 1", got)
	}
}

func TestAssertSafeTextDetectsSecretWithoutRepeatingIt(t *testing.T) {
	clean := &recordingReporter{}
	AssertSafeText(clean, "one-time-code", "Sign in", "Delivery queued")
	if clean.failed {
		t.Fatalf("safe text failed assertion: %s", clean.message)
	}

	leaked := &recordingReporter{}
	AssertSafeText(leaked, "one-time-code", "Code: ONE-TIME-CODE")
	if !leaked.failed || !strings.Contains(leaked.message, "text 1") {
		t.Fatalf("unsafe text was not identified: failed=%v message=%q", leaked.failed, leaked.message)
	}
	if strings.Contains(strings.ToLower(leaked.message), "one-time-code") {
		t.Fatalf("assertion disclosed secret: %q", leaked.message)
	}
}

type recordingReporter struct {
	failed  bool
	message string
}

func (*recordingReporter) Helper() {}

func (r *recordingReporter) Errorf(format string, args ...any) {
	r.failed = true
	r.message = fmt.Sprintf(format, args...)
}
