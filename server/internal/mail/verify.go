package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"time"
)

// Classified verification outcomes. The server's own response text is deliberately never
// wrapped into these: a failure banner routinely echoes the credential just sent, and that
// text ends up in an operator-facing message.
var (
	ErrVerifyUnreachable = errors.New("mail server could not be reached")
	ErrVerifyTLS         = errors.New("mail server connection could not be encrypted")
	ErrVerifyAuth        = errors.New("mail server rejected the credentials")
	ErrVerifySender      = errors.New("mail server rejected the sender address")
)

// DefaultVerifyTimeout bounds a verification so a setup submission cannot hang on a mail
// server that accepts a connection and then says nothing.
const DefaultVerifyTimeout = 10 * time.Second

// VerifySMTP proves a mail configuration works before it is stored, without delivering
// anything: it dials, negotiates TLS, authenticates when credentials are supplied, confirms
// the sender is accepted, and resets.
func VerifySMTP(ctx context.Context, config SMTPConfig) error {
	return verifySMTP(ctx, config, DefaultVerifyTimeout, false)
}

func verifySMTP(ctx context.Context, config SMTPConfig, timeout time.Duration, skipTLSVerify bool) error {
	dialer := net.Dialer{Timeout: timeout}
	connection, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(config.Host, strconv.Itoa(config.Port)))
	if err != nil {
		return ErrVerifyUnreachable
	}
	defer connection.Close()

	deadline := time.Now().Add(timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := connection.SetDeadline(deadline); err != nil {
		return ErrVerifyUnreachable
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = connection.SetDeadline(time.Now())
		case <-done:
		}
	}()

	client, err := smtp.NewClient(connection, config.Host)
	if err != nil {
		return ErrVerifyUnreachable
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{
			ServerName: config.Host, MinVersion: tls.VersionTLS12,
			// Only ever set by this package's own tests, which use a self-signed certificate.
			InsecureSkipVerify: skipTLSVerify, //nolint:gosec
		}); err != nil {
			return ErrVerifyTLS
		}
	} else if config.Username != "" {
		// Refusing here is the point: sending the password over a plaintext connection to
		// find out whether it is correct would be worse than not checking at all.
		return ErrVerifyTLS
	}
	if config.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", config.Username, config.Password, config.Host)); err != nil {
			return ErrVerifyAuth
		}
	}
	from, err := mail.ParseAddress(config.From)
	if err != nil {
		return ErrVerifySender
	}
	// MAIL FROM proves the server will accept this sender. Verification stops here and resets,
	// so setup never delivers a message to anybody.
	if err := client.Mail(from.Address); err != nil {
		return ErrVerifySender
	}
	if err := client.Reset(); err != nil {
		return ErrVerifyUnreachable
	}
	return client.Quit()
}
