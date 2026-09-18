package notify

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"log/slog"

	"market-lens/server/internal/mail"
	"market-lens/server/internal/notify/push"
)

// ErrNotFound is returned for something of this person's that does not exist — and, deliberately,
// for somebody else's. The two answer identically, so a caller cannot learn that another person's
// device exists by being refused differently.
var ErrNotFound = errors.New("not found")

// Refusal is something the product declined, with a reason a person can act on.
type Refusal struct {
	Code    RefusalCode
	Message string
}

func (r Refusal) Error() string { return string(r.Code) + ": " + r.Message }

type RefusalCode string

const (
	RefusalUnknownKind      RefusalCode = "unknown_kind"
	RefusalUnknownChannel   RefusalCode = "unknown_channel"
	RefusalNotAvailable     RefusalCode = "not_available_to_you"
	RefusalInvalidQuiet     RefusalCode = "invalid_quiet_hours"
	RefusalUnknownTimezone  RefusalCode = "unknown_timezone"
	RefusalInvalidSubscribe RefusalCode = "invalid_subscription"
	RefusalInvalidToken     RefusalCode = "invalid_token"
)

// Mailer is the SMTP path the owner already configured. This feature adds templates, not transport.
type Mailer interface {
	Send(ctx context.Context, message mail.Message) error
}

// Pusher posts an encrypted message to a browser's own push service.
type Pusher interface {
	Send(ctx context.Context, subscription push.Subscription, payload []byte) error
}

// SubscribeRequest is exactly what a browser produced, plus a label the person will recognise.
type SubscribeRequest struct {
	Endpoint string
	P256DH   string
	Auth     string
	Label    string
}

// Service is consent, subscription and delivery.
//
// Every method takes the caller's user identifier as its first argument rather than reading a
// context, so a scoping mistake is a compile error. DeliverDue is the one exception, because it
// acts for everybody at once — and it is therefore the method whose isolation is asserted hardest.
type Service struct {
	repository *Repository
	mailer     Mailer
	pusher     Pusher
	// baseURL is where a link in a message points. Links leave the building, so this is stated
	// rather than guessed from a request header somebody else controls.
	baseURL string
	logger  *slog.Logger
}

func NewService(repository *Repository, mailer Mailer, pusher Pusher, baseURL string,
	logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{repository: repository, mailer: mailer, pusher: pusher,
		baseURL: baseURL, logger: logger}
}

// cryptoRandom is where generated keys come from. Named so the one place it is used reads as a
// deliberate choice rather than an import.
var cryptoRandom = cryptorand.Reader

func (s *Service) ready(userID string) error {
	if s == nil || s.repository == nil {
		return errors.New("notification service is not configured")
	}
	if userID == "" {
		return ErrNotFound
	}
	return nil
}

// SendTestEmail proves a message actually arrives, which the settings check cannot.
//
// It goes to the caller's own address and nowhere else: a test send that took a recipient would be
// a way to make this installation mail a stranger.
func (s *Service) SendTestEmail(ctx context.Context, userID string) error {
	if err := s.ready(userID); err != nil {
		return err
	}
	if s.mailer == nil {
		return Refusal{Code: RefusalInvalidSubscribe,
			Message: "No mail server is configured yet."}
	}
	var recipient string
	err := s.repository.pool.QueryRow(ctx,
		`SELECT email FROM users WHERE id = $1 AND status = 'active'`, userID).Scan(&recipient)
	if err != nil {
		return ErrNotFound
	}
	if err := s.mailer.Send(ctx, TestMessage(recipient)); err != nil {
		return Refusal{Code: RefusalInvalidSubscribe,
			Message: "The mail server did not accept the message. Check the settings above and try again."}
	}
	return nil
}
