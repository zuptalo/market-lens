package mail

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestNewSMTPSenderRejectsUnsafeOrIncompleteConfiguration(t *testing.T) {
	valid := SMTPConfig{Host: "smtp.example.test", Port: 587, From: "Market Lens <access@example.test>"}
	tests := []SMTPConfig{
		{Port: 587, From: valid.From},
		{Host: valid.Host, Port: 0, From: valid.From},
		{Host: valid.Host, Port: 70000, From: valid.From},
		{Host: valid.Host, Port: 587, From: "bad\r\nBcc: attacker@example.test"},
		{Host: valid.Host, Port: 587, From: valid.From, Username: "mailer"},
	}
	for _, config := range tests {
		if _, err := NewSMTPSender(config, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))); err == nil {
			t.Fatalf("unsafe SMTP configuration succeeded: host=%q port=%d", config.Host, config.Port)
		}
	}
}

func TestNewSMTPSenderBuildsBoundedNetworkTransport(t *testing.T) {
	sender, err := NewSMTPSender(SMTPConfig{
		Host: "smtp.example.test", Port: 587, From: "Market Lens <access@example.test>",
	}, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := sender.transport.(*networkSMTPTransport)
	if !ok || transport.timeout <= 0 {
		t.Fatalf("SMTP network transport is not bounded: %#v", sender.transport)
	}
}

func TestAccountMailTemplatesAreMinimalBoundedAndValid(t *testing.T) {
	tests := []struct {
		name      string
		build     func(string, string) (Message, error)
		secret    string
		contained string
	}{
		{name: "owner recovery", build: OwnerRecoveryMessage, secret: "recovery-capability", contained: "https://market-lens.test/recover#recovery-capability"},
		{name: "member code", build: MemberCodeMessage, secret: "012345", contained: "012345"},
		{name: "invitation", build: InvitationMessage, secret: "invitation-capability", contained: "https://market-lens.test/invite#invitation-capability"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message, err := tt.build("person@example.test", tt.contained)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateMessage(message); err != nil {
				t.Fatalf("template produced invalid message: %v", err)
			}
			if !strings.Contains(message.Text, tt.secret) {
				t.Fatalf("template omitted its one-time value: %#v", message)
			}
			if strings.Contains(message.Text, "AUTH_SECRET") || len(message.Text) > 2048 {
				t.Fatalf("template is not minimal: %#v", message)
			}
		})
	}
}

func TestAccountMailTemplatesRejectUnsafeInputs(t *testing.T) {
	if _, err := MemberCodeMessage("person@example.test", "12345"); err == nil {
		t.Fatal("five-digit member code unexpectedly succeeded")
	}
	if _, err := MemberCodeMessage("person@example.test", "12a456"); err == nil {
		t.Fatal("non-numeric member code unexpectedly succeeded")
	}
	if _, err := OwnerRecoveryMessage("person@example.test\r\nBcc: attacker@example.test", "https://example.test/#token"); err == nil {
		t.Fatal("header-injection recipient unexpectedly succeeded")
	}
	if _, err := InvitationMessage("person@example.test", strings.Repeat("x", 2049)); err == nil {
		t.Fatal("unbounded invitation URL unexpectedly succeeded")
	}
}
