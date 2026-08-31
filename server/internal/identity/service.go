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
	CredentialCipher       *credentials.Cipher
	MemberAccess           MemberAdministration
	Mail                   appmail.Sender
	AppBaseURL             string
	Logger                 *slog.Logger
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
	credentialCipher       *credentials.Cipher
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
	if dependencies.EODHDValidator == nil || dependencies.CredentialCipher == nil {
		return nil, errors.New("identity setup credential dependencies are incomplete")
	}
	if dependencies.SetupTTL <= 0 || dependencies.OwnerIdleTimeout <= 0 ||
		dependencies.SessionAbsoluteTimeout < dependencies.OwnerIdleTimeout {
		return nil, errors.New("identity service lifetimes are invalid")
	}
	return &Service{
		repository: dependencies.Repository, passwords: dependencies.Passwords, secrets: dependencies.Secrets,
		now: dependencies.Now, setupTTL: dependencies.SetupTTL, ownerIdleTimeout: dependencies.OwnerIdleTimeout,
		sessionAbsoluteTimeout: dependencies.SessionAbsoluteTimeout,
		eodhdValidator:         dependencies.EODHDValidator, credentialCipher: dependencies.CredentialCipher,
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
	if utf8.RuneCountInString(request.Password) < 12 || utf8.RuneCountInString(request.Password) > 1024 {
		return BootstrapResult{}, errors.New("owner password does not meet length requirements")
	}
	if request.Origin == "" || len(request.Origin) > 256 || strings.ContainsFunc(request.Origin, unicode.IsControl) {
		return BootstrapResult{}, errors.New("request origin is invalid")
	}
	email, normalizedEmail, err := NormalizeEmail(request.Email)
	if err != nil {
		return BootstrapResult{}, err
	}
	if err := validateSetupCredentials(request); err != nil {
		return BootstrapResult{}, err
	}
	if err := service.eodhdValidator.ValidateCredential(ctx, request.EODHDAPIKey); err != nil {
		var providerError *marketdata.ProviderError
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrProviderUnavailable) ||
			errors.As(err, &providerError) && providerError.Transient {
			return BootstrapResult{}, ErrProviderUnavailable
		}
		return BootstrapResult{}, ErrProviderCredentialInvalid
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

func validateSetupCredentials(request BootstrapRequest) error {
	if strings.TrimSpace(request.EODHDAPIKey) == "" || len(request.EODHDAPIKey) > 1024 ||
		strings.ContainsFunc(request.EODHDAPIKey, unicode.IsControl) {
		return errors.New("EODHD API key is invalid")
	}
	smtp := request.SMTP
	if strings.TrimSpace(smtp.Host) != smtp.Host || smtp.Host == "" || len(smtp.Host) > 253 ||
		strings.ContainsFunc(smtp.Host, unicode.IsControl) || smtp.Port < 1 || smtp.Port > 65535 {
		return errors.New("SMTP configuration is invalid")
	}
	address, err := mail.ParseAddress(smtp.From)
	if err != nil || address.Address != smtp.From || address.Name != "" || strings.ContainsAny(smtp.From, "\r\n") {
		return errors.New("SMTP configuration is invalid")
	}
	if len(smtp.Username) > 320 || len(smtp.Password) > 1024 ||
		strings.ContainsFunc(smtp.Username, unicode.IsControl) || strings.ContainsFunc(smtp.Password, unicode.IsControl) ||
		(smtp.Username == "") != (smtp.Password == "") {
		return errors.New("SMTP configuration is invalid")
	}
	return nil
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
