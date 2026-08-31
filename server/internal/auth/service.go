package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	netmail "net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"market-lens/server/internal/mail"
)

var (
	ErrAuthenticationFailed   = errors.New("authentication failed")
	ErrAuthenticationRequired = errors.New("authentication required")
	ErrMemberBlocked          = errors.New("member sign-in is temporarily blocked")
	ErrMemberLocked           = errors.New("member sign-in is administratively locked")
	ErrMemberNotFound         = errors.New("member was not found")
	ErrOwnerRequired          = errors.New("owner authorization is required")
	ErrRateLimited            = errors.New("too many attempts")
)

type ServiceDependencies struct {
	Repository             *Repository
	Passwords              *PasswordHasher
	Secrets                *Secrets
	Now                    func() time.Time
	OwnerIdleTimeout       time.Duration
	SessionAbsoluteTimeout time.Duration
	MemberCodes            *MemberCodeGenerator
	Mail                   mail.Sender
	Logger                 *slog.Logger
}

type Service struct {
	repository             *Repository
	passwords              *PasswordHasher
	secrets                *Secrets
	now                    func() time.Time
	ownerIdleTimeout       time.Duration
	sessionAbsoluteTimeout time.Duration
	memberCodes            *MemberCodeGenerator
	mail                   mail.Sender
	logger                 *slog.Logger
}

type OwnerLoginRequest struct {
	Email       string
	Password    string
	DeviceLabel string
	Origin      string
}

const GenericSignInMessage = "If you have an account, you should receive an email with a six-digit passcode."

type SignInStartRequest struct {
	Email  string
	Origin string
}

type SignInStartResult struct {
	// The wire name is fixed by the OpenAPI contract and read verbatim by the client.
	Message string `json:"message"`
}

type Account struct {
	ID              string
	Email           string
	DisplayName     string
	Role            string
	Status          string
	EmailVerifiedAt time.Time
}

type AuthenticationResult struct {
	Account      Account
	Session      SessionSummary
	SessionToken string
	CSRFToken    string
}

type Principal struct {
	UserID     string
	Role       string
	SessionID  string
	VerifyCSRF func(string) bool
}

func NewService(dependencies ServiceDependencies) (*Service, error) {
	if dependencies.Repository == nil || dependencies.Passwords == nil || dependencies.Secrets == nil || dependencies.Now == nil {
		return nil, errors.New("authentication service dependencies are incomplete")
	}
	if dependencies.OwnerIdleTimeout <= 0 || dependencies.SessionAbsoluteTimeout < dependencies.OwnerIdleTimeout {
		return nil, errors.New("authentication service lifetimes are invalid")
	}
	memberCodes := dependencies.MemberCodes
	if memberCodes == nil {
		memberCodes = NewMemberCodeGenerator(nil)
	}
	logger := dependencies.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		repository: dependencies.Repository, passwords: dependencies.Passwords, secrets: dependencies.Secrets,
		now: dependencies.Now, ownerIdleTimeout: dependencies.OwnerIdleTimeout,
		sessionAbsoluteTimeout: dependencies.SessionAbsoluteTimeout,
		memberCodes:            memberCodes, mail: dependencies.Mail, logger: logger,
	}, nil
}

// MemberCodeVerifyRequest carries one six-digit member sign-in attempt.
type MemberCodeVerifyRequest struct {
	Email       string
	Code        string
	DeviceLabel string
	Origin      string
}

// RateLimitedError reports a refused attempt with a deliberately coarse retry hint.
type RateLimitedError struct{ RetryAfter time.Duration }

func (e *RateLimitedError) Error() string { return "too many attempts" }

func (e *RateLimitedError) Unwrap() error { return ErrRateLimited }

// allowOrigin applies the per-origin sliding window. Origin exhaustion is safe to report
// because it identifies no account.
func (service *Service) allowOrigin(ctx context.Context, kind RateBucketKind, origin string, limits []RateLimit, now time.Time) error {
	decision, err := service.repository.AllowRate(ctx, kind, service.secrets.Digest(PurposeOrigin, origin), now, limits)
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return &RateLimitedError{RetryAfter: decision.RetryAfter}
	}
	return nil
}

// StartSignIn advances every caller to code entry with one identical response. Only an
// eligible active member is actually emailed a code, and no branch of this method may leak
// which case occurred: throttling, ineligibility, lockout, and provider outages all return
// the same generic message.
func (service *Service) StartSignIn(ctx context.Context, request SignInStartRequest) (SignInStartResult, error) {
	now := service.now().UTC()
	if err := service.allowOrigin(ctx, RateOriginCodeRequest, request.Origin, OriginCodeRequestLimits, now); err != nil {
		return SignInStartResult{}, err
	}
	generic := SignInStartResult{Message: GenericSignInMessage}

	normalizedEmail, ok := normalizeLoginEmail(request.Email)
	if !ok {
		return generic, nil
	}
	account, err := service.repository.MemberForLogin(ctx, normalizedEmail)
	if err != nil {
		if errors.Is(err, ErrAuthenticationFailed) {
			return generic, nil
		}
		return SignInStartResult{}, err
	}

	// The per-account ceiling must stay silent, otherwise its response would confirm that
	// the address belongs to a real member.
	delivery, err := service.repository.AllowRate(ctx, RateMemberCodeDelivery,
		service.secrets.Digest(PurposeMemberCode, normalizedEmail), now, MemberCodeDeliveryLimits)
	if err != nil {
		return SignInStartResult{}, err
	}
	if !delivery.Allowed {
		return generic, nil
	}

	code, err := service.memberCodes.Generate()
	if err != nil {
		return SignInStartResult{}, err
	}
	challengeID, err := newAuthUUID()
	if err != nil {
		return SignInStartResult{}, err
	}
	deliveryID, err := newAuthUUID()
	if err != nil {
		return SignInStartResult{}, err
	}
	message, err := mail.MemberCodeMessage(account.Email, code)
	if err != nil {
		return SignInStartResult{}, err
	}
	if err := service.repository.IssueMemberChallenge(ctx, IssueMemberChallengeParams{
		ChallengeID: challengeID, DeliveryID: deliveryID, UserID: account.ID, Email: account.Email,
		CodeDigest: service.secrets.Digest(PurposeMemberCode, code),
		CreatedAt:  now, ExpiresAt: now.Add(MemberCodeTTL),
		OriginDigest: service.secrets.Digest(PurposeOrigin, request.Origin),
	}); err != nil {
		// A locked or ineligible member is never distinguishable from an unknown address.
		if errors.Is(err, ErrMemberLocked) || errors.Is(err, ErrMemberBlocked) || errors.Is(err, ErrAuthenticationFailed) {
			return generic, nil
		}
		return SignInStartResult{}, err
	}

	if service.mail == nil {
		return SignInStartResult{}, errors.New("transactional email sender is not configured")
	}
	sendErr := service.mail.Send(ctx, message)
	if markErr := service.repository.MarkMemberCodeDelivery(ctx, challengeID, deliveryID,
		service.now().UTC(), sendErr == nil); markErr != nil {
		return SignInStartResult{}, markErr
	}
	if sendErr != nil {
		// The provider outage is recorded and the undeliverable code retired, but the caller
		// still sees the same generic progression.
		service.logger.WarnContext(ctx, "member sign-in code delivery failed",
			"delivery_id", deliveryID, "error", sendErr)
	}
	return generic, nil
}

// VerifyMemberCode consumes one emailed code. Every failure - wrong code, expired code,
// unknown address, owner address, temporary block, and administrative lock - returns the
// identical generic error so no attempt can enumerate accounts or probe throttling state.
func (service *Service) VerifyMemberCode(ctx context.Context, request MemberCodeVerifyRequest) (AuthenticationResult, error) {
	now := service.now().UTC()
	if err := service.allowOrigin(ctx, RateOriginCodeVerify, request.Origin, OriginCodeVerifyLimits, now); err != nil {
		return AuthenticationResult{}, err
	}
	normalizedEmail, ok := normalizeLoginEmail(request.Email)
	if !ok || !ValidMemberCode(request.Code) {
		return AuthenticationResult{}, ErrAuthenticationFailed
	}
	account, err := service.repository.MemberForLogin(ctx, normalizedEmail)
	if err != nil {
		if errors.Is(err, ErrAuthenticationFailed) {
			return AuthenticationResult{}, ErrAuthenticationFailed
		}
		return AuthenticationResult{}, err
	}

	sessionID, err := newAuthUUID()
	if err != nil {
		return AuthenticationResult{}, err
	}
	sessionToken, err := service.secrets.SessionToken()
	if err != nil {
		return AuthenticationResult{}, err
	}
	csrfToken, err := service.secrets.CSRFToken()
	if err != nil {
		return AuthenticationResult{}, err
	}
	absoluteExpiresAt := now.Add(service.sessionAbsoluteTimeout)
	idleExpiresAt := now.Add(service.ownerIdleTimeout)
	if idleExpiresAt.After(absoluteExpiresAt) {
		idleExpiresAt = absoluteExpiresAt
	}
	session := Session{
		ID: sessionID, UserID: account.ID,
		TokenDigest: service.secrets.Digest(PurposeSession, sessionToken),
		CSRFDigest:  service.secrets.Digest(PurposeCSRF, csrfToken),
		CreatedAt:   now, LastSeenAt: now, IdleExpiresAt: idleExpiresAt, AbsoluteExpiresAt: absoluteExpiresAt,
		DeviceLabel: request.DeviceLabel, OriginDigest: service.secrets.Digest(PurposeOrigin, request.Origin),
	}
	if err := session.Validate(); err != nil {
		return AuthenticationResult{}, ErrAuthenticationFailed
	}
	result, err := service.repository.VerifyMemberChallenge(ctx, VerifyMemberChallengeParams{
		UserID: account.ID, CodeDigest: service.secrets.Digest(PurposeMemberCode, request.Code),
		Session: session, Now: now, OriginDigest: service.secrets.Digest(PurposeOrigin, request.Origin),
	})
	if err != nil {
		return AuthenticationResult{}, err
	}
	if result.Outcome != MemberLoginSucceeded {
		return AuthenticationResult{}, ErrAuthenticationFailed
	}
	return AuthenticationResult{
		Account: result.Account, Session: session.Summary(session.ID),
		SessionToken: sessionToken, CSRFToken: csrfToken,
	}, nil
}

func (service *Service) ResetOwnerPassword(ctx context.Context, password string) error {
	if utf8.RuneCountInString(password) < 12 || utf8.RuneCountInString(password) > 1024 {
		return errors.New("owner password does not satisfy the password policy")
	}
	passwordHash, err := service.passwords.Encode(password)
	if err != nil {
		return err
	}
	return service.repository.ResetOwnerPassword(ctx, passwordHash, service.now().UTC())
}

func (service *Service) LoginOwner(ctx context.Context, request OwnerLoginRequest) (AuthenticationResult, error) {
	normalizedEmail, ok := normalizeLoginEmail(request.Email)
	if !ok || request.Password == "" || utf8.RuneCountInString(request.Password) > 1024 {
		return AuthenticationResult{}, ErrAuthenticationFailed
	}
	record, err := service.repository.OwnerCredential(ctx, normalizedEmail)
	if err != nil {
		if errors.Is(err, ErrAuthenticationFailed) {
			return AuthenticationResult{}, ErrAuthenticationFailed
		}
		return AuthenticationResult{}, err
	}
	valid, needsRehash, err := service.passwords.Verify(record.PasswordHash, request.Password)
	if err != nil || !valid {
		return AuthenticationResult{}, ErrAuthenticationFailed
	}

	now := service.now().UTC()
	sessionID, err := newAuthUUID()
	if err != nil {
		return AuthenticationResult{}, err
	}
	sessionToken, err := service.secrets.SessionToken()
	if err != nil {
		return AuthenticationResult{}, err
	}
	csrfToken, err := service.secrets.CSRFToken()
	if err != nil {
		return AuthenticationResult{}, err
	}
	absoluteExpiresAt := now.Add(service.sessionAbsoluteTimeout)
	idleExpiresAt := now.Add(service.ownerIdleTimeout)
	if idleExpiresAt.After(absoluteExpiresAt) {
		idleExpiresAt = absoluteExpiresAt
	}
	session := Session{
		ID: sessionID, UserID: record.Account.ID,
		TokenDigest: service.secrets.Digest(PurposeSession, sessionToken),
		CSRFDigest:  service.secrets.Digest(PurposeCSRF, csrfToken),
		CreatedAt:   now, LastSeenAt: now, IdleExpiresAt: idleExpiresAt, AbsoluteExpiresAt: absoluteExpiresAt,
		DeviceLabel: request.DeviceLabel, OriginDigest: service.secrets.Digest(PurposeOrigin, request.Origin),
	}
	if err := session.Validate(); err != nil {
		return AuthenticationResult{}, ErrAuthenticationFailed
	}
	replacementHash := ""
	if needsRehash {
		replacementHash, err = service.passwords.Encode(request.Password)
		if err != nil {
			return AuthenticationResult{}, err
		}
	}
	if err := service.repository.CreateOwnerSession(ctx, record.Account, session, replacementHash); err != nil {
		if errors.Is(err, ErrAuthenticationFailed) {
			return AuthenticationResult{}, ErrAuthenticationFailed
		}
		return AuthenticationResult{}, err
	}
	return AuthenticationResult{
		Account: record.Account, Session: session.Summary(session.ID), SessionToken: sessionToken, CSRFToken: csrfToken,
	}, nil
}

func (service *Service) AuthenticateSession(ctx context.Context, token string) (Principal, error) {
	if token == "" || len(token) > 512 {
		return Principal{}, ErrAuthenticationRequired
	}
	digest := service.secrets.Digest(PurposeSession, token)
	session, account, err := service.repository.SessionByDigest(ctx, digest)
	if err != nil {
		if errors.Is(err, ErrAuthenticationRequired) {
			return Principal{}, ErrAuthenticationRequired
		}
		return Principal{}, err
	}
	now := service.now().UTC()
	if account.Status != "active" || !session.ActiveAt(now, true) {
		return Principal{}, ErrAuthenticationRequired
	}
	if err := session.Touch(now, service.ownerIdleTimeout); err != nil {
		return Principal{}, ErrAuthenticationRequired
	}
	if err := service.repository.UpdateSessionActivity(ctx, session); err != nil {
		if errors.Is(err, ErrAuthenticationRequired) {
			return Principal{}, ErrAuthenticationRequired
		}
		return Principal{}, err
	}
	csrfDigest := append([]byte(nil), session.CSRFDigest...)
	return Principal{
		UserID: account.ID, Role: account.Role, SessionID: session.ID,
		VerifyCSRF: func(value string) bool { return service.secrets.VerifyDigest(PurposeCSRF, value, csrfDigest) },
	}, nil
}

// RevalidateSession re-checks the session and account behind an already open event stream. The
// connection outlived the authentication that admitted it, so revocation, deactivation, and
// expiry all have to be observable here. It deliberately does not touch activity: watching a
// stream is not using the application, and an idle session must still time out.
func (service *Service) RevalidateSession(ctx context.Context, sessionID string) error {
	if !validSessionUUID(sessionID) {
		return ErrAuthenticationRequired
	}
	session, account, err := service.repository.SessionByID(ctx, sessionID)
	if err != nil {
		if errors.Is(err, ErrAuthenticationRequired) {
			return ErrAuthenticationRequired
		}
		return err
	}
	if account.Status != "active" || !session.ActiveAt(service.now().UTC(), true) {
		return ErrAuthenticationRequired
	}
	return nil
}

func (service *Service) ListSessions(ctx context.Context, userID, currentSessionID string) ([]SessionSummary, error) {
	if !validSessionUUID(userID) || !validSessionUUID(currentSessionID) {
		return nil, ErrAuthenticationRequired
	}
	sessions, err := service.repository.ListSessions(ctx, userID)
	if err != nil {
		return nil, err
	}
	summaries := make([]SessionSummary, 0, len(sessions))
	for _, session := range sessions {
		summaries = append(summaries, session.Summary(currentSessionID))
	}
	return summaries, nil
}

func (service *Service) Account(ctx context.Context, userID string) (Account, error) {
	if !validSessionUUID(userID) {
		return Account{}, ErrAuthenticationRequired
	}
	return service.repository.Account(ctx, userID)
}

func (service *Service) RevokeSession(ctx context.Context, userID, sessionID string) error {
	if !validSessionUUID(userID) || !validSessionUUID(sessionID) {
		return ErrAuthenticationRequired
	}
	return service.repository.RevokeSession(ctx, userID, sessionID, service.now().UTC())
}

func (service *Service) RevokeAllSessions(ctx context.Context, userID string) error {
	if !validSessionUUID(userID) {
		return ErrAuthenticationRequired
	}
	return service.repository.RevokeAllSessions(ctx, userID, service.now().UTC())
}

func normalizeLoginEmail(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if len(value) < 3 || len(value) > 320 || strings.ContainsFunc(value, unicode.IsControl) {
		return "", false
	}
	address, err := netmail.ParseAddress(value)
	if err != nil || address.Name != "" || address.Address != value {
		return "", false
	}
	return strings.ToLower(value), true
}

func newAuthUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate authentication ID: %w", err)
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32], nil
}
