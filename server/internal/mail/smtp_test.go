package mail

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

const smtpTestSecret = "smtp-secret-never-disclose"

func TestSMTPSenderRejectsUnboundedOrHeaderUnsafeMessages(t *testing.T) {
	transport := &recordingTransport{}
	sender := testSMTPSender(transport, &bytes.Buffer{})
	tests := []Message{
		{To: "", Subject: "Hello", Text: "Body"},
		{To: "user@example.test\r\nBcc: attacker@example.test", Subject: "Hello", Text: "Body"},
		{To: "user@example.test", Subject: strings.Repeat("s", 201), Text: "Body"},
		{To: "user@example.test", Subject: "Hello", Text: strings.Repeat("b", 16*1024+1)},
	}
	for _, message := range tests {
		err := sender.Send(context.Background(), message)
		var deliveryError *DeliveryError
		if !errors.As(err, &deliveryError) || deliveryError.Code != "invalid_message" || deliveryError.Retryable {
			t.Fatalf("invalid message error = %#v", err)
		}
	}
	if transport.calls != 0 {
		t.Fatalf("transport received %d invalid messages", transport.calls)
	}
}

func TestSMTPSenderHonorsContextCancellationBeforeDelivery(t *testing.T) {
	transport := &recordingTransport{}
	sender := testSMTPSender(transport, &bytes.Buffer{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := sender.Send(ctx, Message{To: "user@example.test", Subject: "Sign in", Text: "Safe body"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Send() error = %v, want context.Canceled", err)
	}
	if transport.calls != 0 {
		t.Fatal("transport was called after cancellation")
	}
}

func TestSMTPSenderClassifiesProviderFailureWithoutLeakingSecrets(t *testing.T) {
	var logs bytes.Buffer
	transport := &recordingTransport{err: errors.New("dial failed password=" + smtpTestSecret)}
	sender := testSMTPSender(transport, &logs)
	message := Message{To: "user@example.test", Subject: "Recovery", Text: "capability=" + smtpTestSecret}

	err := sender.Send(context.Background(), message)
	var deliveryError *DeliveryError
	if !errors.As(err, &deliveryError) || deliveryError.Code != "smtp_unavailable" || !deliveryError.Retryable {
		t.Fatalf("provider error = %#v", err)
	}
	for name, output := range map[string]string{"error": err.Error(), "logs": logs.String()} {
		if strings.Contains(output, smtpTestSecret) || strings.Contains(output, message.Text) || strings.Contains(output, "password=") {
			t.Fatalf("%s disclosed SMTP or message secret: %q", name, output)
		}
	}
}

func TestSMTPSenderDeliversValidBoundedMessage(t *testing.T) {
	transport := &recordingTransport{}
	sender := testSMTPSender(transport, &bytes.Buffer{})
	want := Message{To: "user@example.test", Subject: "Sign in", Text: "Your requested sign-in code is ready."}
	if err := sender.Send(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	if transport.calls != 1 || transport.message != want {
		t.Fatalf("transport calls=%d message=%#v", transport.calls, transport.message)
	}
}

func testSMTPSender(transport smtpTransport, logs *bytes.Buffer) *SMTPSender {
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	return newSMTPSender(SMTPConfig{
		Host: "smtp.example.test", Port: 587, Username: "mail-account", Password: smtpTestSecret,
		From: "Market Lens <access@example.test>",
	}, logger, transport)
}

type recordingTransport struct {
	calls   int
	message Message
	err     error
}

func (t *recordingTransport) Send(ctx context.Context, _ SMTPConfig, message Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t.calls++
	t.message = message
	return t.err
}
