package identity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/credentials"
	appmail "market-lens/server/internal/mail"
	"market-lens/server/internal/marketdata"
)

var (
	ErrSetupClosed               = errors.New("owner setup is closed")
	ErrCapabilityUnavailable     = errors.New("setup capability is unavailable")
	ErrProviderCredentialInvalid = errors.New("provider credential is invalid")
	ErrProviderUnavailable       = errors.New("provider is unavailable")
)

type EODHDCredentialValidator interface {
	ValidateCredential(context.Context, string) error
}

type SMTPSetupConfiguration struct {
	Host     string
	Port     int
	From     string
	Username string
	Password string
}

type ServiceDependencies struct {
	Repository             *Repository
	Passwords              *auth.PasswordHasher
	Secrets                *auth.Secrets
	Now                    func() time.Time
	SetupTTL               time.Duration
	OwnerIdleTimeout       time.Duration
	SessionAbsoluteTimeout time.Duration
	EODHDValidator         EODHDCredentialValidator
	SMTPVerifier           SMTPVerifier
	CredentialCipher       *credentials.Cipher
	// Credentials is the encrypted provider-credential store. Owner settings read and
	// replace through it; bootstrap writes through the same transaction it creates the owner in.
	Credentials  *credentials.Repository
	MemberAccess MemberAdministration
	Mail         appmail.Sender
	AppBaseURL   string
	Logger       *slog.Logger
}

type Service struct {
	repository             *Repository
	passwords              *auth.PasswordHasher
	secrets                *auth.Secrets
	now                    func() time.Time
	setupTTL               time.Duration
	ownerIdleTimeout       time.Duration
	sessionAbsoluteTimeout time.Duration
	eodhdValidator         EODHDCredentialValidator
	smtpVerifier           SMTPVerifier
	credentialCipher       *credentials.Cipher
	credentials            *credentials.Repository
	memberAccess           MemberAdministration
	mail                   appmail.Sender
	appBaseURL             string
	log                    *slog.Logger
}

type SetupCapability struct {
	ID        string
	Token     string
	ExpiresAt time.Time
}

type BootstrapRequest struct {
	Capability  string
	Email       string
	Password    string
	DisplayName string
	DeviceLabel string
	Origin      string
	EODHDAPIKey string
	SMTP        SMTPSetupConfiguration
}

type BootstrapResult struct {
	User         User
	Session      auth.SessionSummary
	SessionToken string
	CSRFToken    string
}

func NewService(dependencies ServiceDependencies) (*Service, error) {
	if dependencies.Repository == nil || dependencies.Passwords == nil || dependencies.Secrets == nil || dependencies.Now == nil {
		return nil, errors.New("identity service dependencies are incomplete")
	}
	if dependencies.EODHDValidator == nil {
		return nil, errors.New("identity setup credential dependencies are incomplete")
	}
	// The cipher may be absent: a deployment configured with only DATABASE_URL has no
	// EXTERNAL_CREDENTIAL_KEY yet and must still start and serve sign-in. Owner setup is the
	// one operation that cannot proceed without it, and refuses at the point of use below.
	if dependencies.SetupTTL <= 0 || dependencies.OwnerIdleTimeout <= 0 ||
		dependencies.SessionAbsoluteTimeout < dependencies.OwnerIdleTimeout {
		return nil, errors.New("identity service lifetimes are invalid")
	}
	return &Service{
		repository: dependencies.Repository, passwords: dependencies.Passwords, secrets: dependencies.Secrets,
		now: dependencies.Now, setupTTL: dependencies.SetupTTL, ownerIdleTimeout: dependencies.OwnerIdleTimeout,
		sessionAbsoluteTimeout: dependencies.SessionAbsoluteTimeout,
		eodhdValidator:         dependencies.EODHDValidator, smtpVerifier: dependencies.SMTPVerifier,
		credentialCipher: dependencies.CredentialCipher, credentials: dependencies.Credentials,
		memberAccess: dependencies.MemberAccess, mail: dependencies.Mail,
		appBaseURL: strings.TrimRight(dependencies.AppBaseURL, "/"), log: dependencies.Logger,
	}, nil
}

func (service *Service) IssueSetupCapability(ctx context.Context) (SetupCapability, error) {
	now := service.now().UTC()
	id, err := newUUID()
	if err != nil {
		return SetupCapability{}, err
	}
	token, err := service.secrets.Capability()
	if err != nil {
		return SetupCapability{}, err
	}
	capability := Capability{
		ID: id, Kind: CapabilityOwnerSetup, TokenDigest: service.secrets.Digest(auth.PurposeSetup, token),
		CreatedAt: now, ExpiresAt: now.Add(service.setupTTL),
	}
	if err := service.repository.IssueSetupCapability(ctx, capability, now); err != nil {
		return SetupCapability{}, err
	}
	return SetupCapability{ID: id, Token: token, ExpiresAt: capability.ExpiresAt}, nil
}

func (service *Service) SetupRequired(ctx context.Context) (bool, error) {
	return service.repository.SetupRequired(ctx)
}

func (service *Service) BootstrapOwner(ctx context.Context, request BootstrapRequest) (BootstrapResult, error) {
	if request.Capability == "" || len(request.Capability) > 512 {
		return BootstrapResult{}, ErrCapabilityUnavailable
	}
	// The origin is derived from the request, not typed by anyone, so it is not a field the
	// operator could correct and stays a plain rejection.
	if request.Origin == "" || len(request.Origin) > 256 || strings.ContainsFunc(request.Origin, unicode.IsControl) {
		return BootstrapResult{}, errors.New("request origin is invalid")
	}

	// Everything below is collected rather than returned on first failure, so somebody filling
	// in ten inputs is told about all their mistakes at once instead of one per round trip.
	validation := &SetupValidationError{}
	email, normalizedEmail, emailErr := NormalizeEmail(request.Email)
	if emailErr != nil {
		validation.add("email", "invalid_format", "Enter a valid email address, such as you@example.com.")
	}
	if name := strings.TrimSpace(request.DisplayName); name == "" || name != request.DisplayName ||
		utf8.RuneCountInString(request.DisplayName) > 120 || strings.ContainsFunc(request.DisplayName, unicode.IsControl) {
		validation.add("display_name", "invalid_format",
			"Enter a display name of 1 to 120 characters, with no leading or trailing spaces.")
	}
	switch length := utf8.RuneCountInString(request.Password); {
	case length < 12:
		validation.add("password", "too_short", "Password must be at least 12 characters.")
	case length > 1024:
		validation.add("password", "too_long", "Password must be at most 1024 characters.")
	}
	validateSetupCredentials(request, validation)

	// A request that cannot succeed must not spend a provider call or open a connection to
	// somebody's mail server, so the network checks run only once the shape is right.
	if len(validation.Fields) > 0 {
		return BootstrapResult{}, validation
	}

	if err := service.eodhdValidator.ValidateCredential(ctx, request.EODHDAPIKey); err != nil {
		var providerError *marketdata.ProviderError
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrProviderUnavailable) ||
			errors.As(err, &providerError) && providerError.Transient {
			validation.Unreachable = true
			validation.add("eodhd_api_key", "unreachable",
				"EODHD could not be reached, so the API key could not be checked. Try again shortly.")
		} else {
			validation.add("eodhd_api_key", "rejected",
				"EODHD rejected this API key. Check it in your EODHD account and paste it again.")
		}
	}
	service.verifySetupMail(ctx, request.SMTP, validation)
	if len(validation.Fields) > 0 {
		return BootstrapResult{}, validation
	}

	now := service.now().UTC()
	userID, err := newUUID()
	if err != nil {
		return BootstrapResult{}, err
	}
	sessionID, err := newUUID()
	if err != nil {
		return BootstrapResult{}, err
	}
	passwordHash, err := service.passwords.Encode(request.Password)
	if err != nil {
		return BootstrapResult{}, err
	}
	sessionToken, err := service.secrets.SessionToken()
	if err != nil {
		return BootstrapResult{}, err
	}
	csrfToken, err := service.secrets.CSRFToken()
	if err != nil {
		return BootstrapResult{}, err
	}

	verifiedAt := now
	user := User{
		ID: userID, Email: email, NormalizedEmail: normalizedEmail, DisplayName: request.DisplayName,
		Role: RoleOwner, Status: StatusActive, EmailVerifiedAt: &verifiedAt, CreatedAt: now, UpdatedAt: now,
	}
	credential := OwnerCredential{UserID: userID, PasswordHash: passwordHash, ChangedAt: now, CreatedAt: now}
	absoluteExpiresAt := now.Add(service.sessionAbsoluteTimeout)
	idleExpiresAt := now.Add(service.ownerIdleTimeout)
	if idleExpiresAt.After(absoluteExpiresAt) {
		idleExpiresAt = absoluteExpiresAt
	}
	session := auth.Session{
		ID: sessionID, UserID: userID,
		TokenDigest: service.secrets.Digest(auth.PurposeSession, sessionToken),
		CSRFDigest:  service.secrets.Digest(auth.PurposeCSRF, csrfToken),
		CreatedAt:   now, LastSeenAt: now, IdleExpiresAt: idleExpiresAt, AbsoluteExpiresAt: absoluteExpiresAt,
		DeviceLabel: request.DeviceLabel, OriginDigest: service.secrets.Digest(auth.PurposeOrigin, request.Origin),
	}
	audit := SecurityAuditEvent{
		OccurredAt: now, EventType: "owner.setup.v1", SubjectUserID: userID, SessionID: sessionID,
		Outcome: AuditSucceeded, OriginDigest: service.secrets.Digest(auth.PurposeOrigin, request.Origin),
		Metadata: json.RawMessage(`{"source":"host_setup"}`),
	}
	externalCredentials, err := service.sealSetupCredentials(request, now)
	if err != nil {
		return BootstrapResult{}, err
	}
	for _, validation := range []func() error{user.Validate, credential.Validate, session.Validate, audit.Validate} {
		if err := validation(); err != nil {
			return BootstrapResult{}, err
		}
	}
	if err := service.repository.CompleteBootstrap(ctx, CompleteBootstrapParams{
		CapabilityDigest: service.secrets.Digest(auth.PurposeSetup, request.Capability),
		User:             user, Credential: credential, Session: session, Audit: audit,
		ExternalCredentials: externalCredentials, Now: now,
	}); err != nil {
		return BootstrapResult{}, err
	}
	return BootstrapResult{
		User: user, Session: session.Summary(session.ID), SessionToken: sessionToken, CSRFToken: csrfToken,
	}, nil
}

// validateSetupCredentials records what is wrong with each provider input. It used to answer
// "SMTP configuration is invalid" for eight different problems across five inputs, which told
// the operator nothing about which one to change.
func validateSetupCredentials(request BootstrapRequest, validation *SetupValidationError) {
	validateEODHDKeyShape(request.EODHDAPIKey, validation)
	validateSMTPShape(request.SMTP, validation)
}

func validateEODHDKeyShape(key string, validation *SetupValidationError) {
	if strings.TrimSpace(key) == "" || len(key) > 1024 || strings.ContainsFunc(key, unicode.IsControl) {
		validation.add("eodhd_api_key", "invalid_format",
			"Enter your EODHD API key. It cannot be blank or contain control characters.")
	}
}

// validateSMTPShape is shared by bootstrap and by the owner settings screen, so the two can
// never drift into different ideas of a valid mail configuration.
func validateSMTPShape(smtp SMTPSetupConfiguration, validation *SetupValidationError) {
	if strings.TrimSpace(smtp.Host) != smtp.Host || smtp.Host == "" || len(smtp.Host) > 253 ||
		strings.ContainsFunc(smtp.Host, unicode.IsControl) {
		validation.add("smtp_host", "invalid_format",
			"Enter the mail server host name, such as smtp.example.com, with no surrounding spaces.")
	}
	if smtp.Port < 1 || smtp.Port > 65535 {
		validation.add("smtp_port", "out_of_range", "SMTP port must be between 1 and 65535. It is usually 587.")
	}
	address, err := mail.ParseAddress(smtp.From)
	if err != nil || address.Address != smtp.From || address.Name != "" || strings.ContainsAny(smtp.From, "\r\n") {
		validation.add("smtp_from", "invalid_format",
			"Enter the plain address that mail is sent from, such as market-lens@example.com.")
	}
	if len(smtp.Username) > 320 || strings.ContainsFunc(smtp.Username, unicode.IsControl) {
		validation.add("smtp_username", "invalid_format",
			"The SMTP username is too long or contains control characters.")
	}
	if len(smtp.Password) > 1024 || strings.ContainsFunc(smtp.Password, unicode.IsControl) {
		validation.add("smtp_password", "invalid_format",
			"The SMTP password is too long or contains control characters.")
	}
	// Authentication needs both halves. Supplying one is always a mistake, and saying which
	// half is missing is more useful than calling the whole section invalid.
	if smtp.Username != "" && smtp.Password == "" {
		validation.add("smtp_password", "required",
			"Enter the SMTP password that goes with this username, or clear the username to connect without authentication.")
	}
	if smtp.Username == "" && smtp.Password != "" {
		validation.add("smtp_username", "required",
			"Enter the SMTP username that goes with this password, or clear the password to connect without authentication.")
	}
}

// verifySetupMail proves the mail configuration works before setup stores it. The operator
// chose to block setup on any failure: an installation whose mail does not work cannot invite
// anybody or issue a login code, so completing setup against broken mail produces something
// that looks healthy and is a dead end.
func (service *Service) verifySetupMail(ctx context.Context, config SMTPSetupConfiguration,
	validation *SetupValidationError) {
	if service.smtpVerifier == nil {
		return
	}
	err := service.smtpVerifier.VerifySMTP(ctx, config)
	switch {
	case err == nil:
		return
	case errors.Is(err, ErrSMTPAuthRejected):
		// Both halves are named, because either one could be the wrong part.
		validation.add("smtp_username", "auth_rejected",
			"The mail server rejected these credentials. Check the username and password.")
		validation.add("smtp_password", "auth_rejected",
			"The mail server rejected these credentials. Check the username and password.")
	case errors.Is(err, ErrSMTPSenderRejected):
		validation.add("smtp_from", "sender_rejected",
			"The mail server refused this sender address. It usually has to be an address that server is allowed to send as.")
	case errors.Is(err, ErrSMTPTLSFailed):
		validation.add("smtp_host", "tls_failed",
			"The connection to the mail server could not be encrypted. Check the host and port.")
	default:
		validation.Unreachable = true
		validation.add("smtp_host", "unreachable",
			"Could not reach the mail server. Check the host and port, and that it accepts connections from here.")
	}
}

func (service *Service) sealSetupCredentials(request BootstrapRequest, now time.Time) ([]credentials.StoredCredential, error) {
	type eodhdPayload struct {
		APIKey string `json:"api_key"`
	}
	type smtpPayload struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		From     string `json:"from"`
		Username string `json:"username,omitempty"`
		Password string `json:"password,omitempty"`
	}
	items := []struct {
		kind      credentials.Kind
		payload   any
		validated *time.Time
	}{
		{kind: credentials.KindEODHDAPI, payload: eodhdPayload{APIKey: request.EODHDAPIKey}, validated: &now},
		{kind: credentials.KindSMTP, payload: smtpPayload{
			Host: request.SMTP.Host, Port: request.SMTP.Port, From: request.SMTP.From,
			Username: request.SMTP.Username, Password: request.SMTP.Password,
		}},
	}
	if service.credentialCipher == nil {
		// Naming the value and what it is for, without echoing anything the caller submitted.
		return nil, errors.New("EXTERNAL_CREDENTIAL_KEY is not configured, so provider " +
			"credentials cannot be encrypted and stored; supply one and keep it with your " +
			"database backups, because it is never stored in the database")
	}
	result := make([]credentials.StoredCredential, 0, len(items))
	for _, item := range items {
		id, err := newUUID()
		if err != nil {
			return nil, err
		}
		plaintext, err := json.Marshal(item.payload)
		if err != nil {
			return nil, errors.New("encode external credential")
		}
		metadata := credentials.Metadata{
			ID: id, Kind: item.kind, PayloadVersion: 1, KeyVersion: service.credentialCipher.KeyVersion(),
		}
		ciphertext, err := service.credentialCipher.Seal(metadata, plaintext)
		clear(plaintext)
		if err != nil {
			return nil, err
		}
		result = append(result, credentials.StoredCredential{
			Record:      credentials.Record{Metadata: metadata, Ciphertext: ciphertext},
			ValidatedAt: item.validated, CreatedAt: now, UpdatedAt: now,
		})
	}
	return result, nil
}

func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate identity ID: %w", err)
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32], nil
}
