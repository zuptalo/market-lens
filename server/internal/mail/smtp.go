package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

type smtpTransport interface {
	Send(context.Context, SMTPConfig, Message) error
}

type networkSMTPTransport struct {
	timeout time.Duration
}

func (t *networkSMTPTransport) Send(ctx context.Context, config SMTPConfig, message Message) error {
	dialer := net.Dialer{Timeout: t.timeout}
	connection, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(config.Host, fmt.Sprintf("%d", config.Port)))
	if err != nil {
		return err
	}
	defer connection.Close()

	deadline := time.Now().Add(t.timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := connection.SetDeadline(deadline); err != nil {
		return err
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
		return err
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: config.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	} else if config.Username != "" {
		return errors.New("SMTP authentication requires TLS")
	}
	if config.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", config.Username, config.Password, config.Host)); err != nil {
			return err
		}
	}
	from, err := mail.ParseAddress(config.From)
	if err != nil {
		return err
	}
	recipient, err := mail.ParseAddress(message.To)
	if err != nil {
		return err
	}
	if err := client.Mail(from.Address); err != nil {
		return err
	}
	if err := client.Rcpt(recipient.Address); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if err := writeSMTPMessage(writer, config.From, message); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func writeSMTPMessage(writer io.Writer, from string, message Message) error {
	body := strings.ReplaceAll(message.Text, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	body = strings.ReplaceAll(body, "\n", "\r\n")
	_, err := fmt.Fprintf(writer,
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n",
		from, message.To, message.Subject, body)
	return err
}

type SMTPSender struct {
	config    SMTPConfig
	logger    *slog.Logger
	transport smtpTransport
}

func newSMTPSender(config SMTPConfig, logger *slog.Logger, transport smtpTransport) *SMTPSender {
	return &SMTPSender{config: config, logger: logger, transport: transport}
}

func NewSMTPSender(config SMTPConfig, logger *slog.Logger) (*SMTPSender, error) {
	if strings.TrimSpace(config.Host) == "" || strings.ContainsAny(config.Host, "\r\n") {
		return nil, errors.New("SMTP host is invalid")
	}
	if config.Port < 1 || config.Port > 65535 {
		return nil, errors.New("SMTP port is invalid")
	}
	if config.From == "" || strings.ContainsAny(config.From, "\r\n") {
		return nil, errors.New("SMTP sender is invalid")
	}
	if _, err := mail.ParseAddress(config.From); err != nil {
		return nil, errors.New("SMTP sender is invalid")
	}
	if (config.Username == "") != (config.Password == "") {
		return nil, errors.New("SMTP credentials are incomplete")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return newSMTPSender(config, logger, &networkSMTPTransport{timeout: 10 * time.Second}), nil
}

func (s *SMTPSender) Send(ctx context.Context, message Message) error {
	if err := validateMessage(message); err != nil {
		return &DeliveryError{Code: "invalid_message", Retryable: false}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.transport == nil {
		return &DeliveryError{Code: "smtp_unavailable", Retryable: true}
	}
	if err := s.transport.Send(ctx, s.config, message); err != nil {
		if contextError := ctx.Err(); contextError != nil {
			return contextError
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if s.logger != nil {
			s.logger.Warn("transactional email delivery failed", "code", "smtp_unavailable")
		}
		return &DeliveryError{Code: "smtp_unavailable", Retryable: true}
	}
	return nil
}

func validateMessage(message Message) error {
	if message.To == "" || len(message.To) > 320 || strings.ContainsAny(message.To, "\r\n") {
		return errors.New("invalid recipient")
	}
	address, err := mail.ParseAddress(message.To)
	if err != nil || address.Address == "" {
		return errors.New("invalid recipient")
	}
	if message.Subject == "" || len(message.Subject) > 200 || strings.ContainsAny(message.Subject, "\r\n") {
		return errors.New("invalid subject")
	}
	if message.Text == "" || len(message.Text) > 16*1024 {
		return errors.New("invalid message body")
	}
	return nil
}
