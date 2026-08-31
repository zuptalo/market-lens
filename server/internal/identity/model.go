// Package identity owns users, bootstrap state, and invitation lifecycle.
package identity

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type Role string

const (
	RoleOwner  Role = "owner"
	RoleMember Role = "member"
)

type Status string

const (
	StatusActive      Status = "active"
	StatusDeactivated Status = "deactivated"
)

type User struct {
	ID              string
	Email           string
	NormalizedEmail string
	DisplayName     string
	Role            Role
	Status          Status
	EmailVerifiedAt *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeactivatedAt   *time.Time
}

func NormalizeEmail(value string) (string, string, error) {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) < 3 || utf8.RuneCountInString(value) > 320 || containsControl(value) {
		return "", "", errors.New("email is invalid")
	}
	at := strings.LastIndexByte(value, '@')
	if at <= 0 || at == len(value)-1 {
		return "", "", errors.New("email is invalid")
	}
	local, domain := value[:at], value[at+1:]
	if strings.TrimSpace(local) != local || strings.TrimSpace(domain) != domain {
		return "", "", errors.New("email is invalid")
	}
	delivery := local + "@" + strings.ToLower(domain)
	address, err := mail.ParseAddress(delivery)
	if err != nil || address.Name != "" || address.Address != delivery {
		return "", "", errors.New("email is invalid")
	}
	return delivery, strings.ToLower(delivery), nil
}

func (user User) Validate() error {
	if !validUUID(user.ID) {
		return errors.New("user ID is invalid")
	}
	delivery, normalized, err := NormalizeEmail(user.Email)
	if err != nil || delivery != user.Email || normalized != user.NormalizedEmail {
		return errors.New("user email is invalid")
	}
	if strings.TrimSpace(user.DisplayName) != user.DisplayName || containsControl(user.DisplayName) ||
		utf8.RuneCountInString(user.DisplayName) < 1 || utf8.RuneCountInString(user.DisplayName) > 120 {
		return errors.New("display name is invalid")
	}
	if user.Role != RoleOwner && user.Role != RoleMember {
		return errors.New("user role is invalid")
	}
	if user.Status != StatusActive && user.Status != StatusDeactivated {
		return errors.New("user status is invalid")
	}
	if user.Role == RoleOwner && user.Status != StatusActive {
		return errors.New("owner must remain active")
	}
	if user.Status == StatusActive && (user.EmailVerifiedAt == nil || user.DeactivatedAt != nil) {
		return errors.New("active user lifecycle is invalid")
	}
	if user.Status == StatusDeactivated && user.DeactivatedAt == nil {
		return errors.New("deactivated user lifecycle is invalid")
	}
	if user.CreatedAt.IsZero() || user.UpdatedAt.Before(user.CreatedAt) {
		return errors.New("user timestamps are invalid")
	}
	if user.EmailVerifiedAt != nil && user.EmailVerifiedAt.Before(user.CreatedAt) {
		return errors.New("email verification timestamp is invalid")
	}
	if user.DeactivatedAt != nil && user.DeactivatedAt.Before(user.CreatedAt) {
		return errors.New("deactivation timestamp is invalid")
	}
	return nil
}

type BootstrapState struct {
	ClosedAt    *time.Time
	OwnerUserID string
}

func (state BootstrapState) Open() bool { return state.ClosedAt == nil && state.OwnerUserID == "" }

func (state *BootstrapState) Close(ownerUserID string, now time.Time) error {
	if !state.Open() {
		return errors.New("owner setup is closed")
	}
	if !validUUID(ownerUserID) || now.IsZero() {
		return errors.New("owner setup closure is invalid")
	}
	closedAt := now.UTC()
	state.ClosedAt = &closedAt
	state.OwnerUserID = ownerUserID
	return nil
}

type CapabilityKind string

const (
	CapabilityOwnerSetup    CapabilityKind = "owner_setup"
	CapabilityOwnerRecovery CapabilityKind = "owner_recovery"
)

type Capability struct {
	ID          string
	Kind        CapabilityKind
	UserID      string
	TokenDigest []byte
	ExpiresAt   time.Time
	ConsumedAt  *time.Time
	RevokedAt   *time.Time
	CreatedAt   time.Time
}

func (capability Capability) Validate() error {
	if !validUUID(capability.ID) || len(capability.TokenDigest) != 32 {
		return errors.New("capability identity is invalid")
	}
	if capability.Kind != CapabilityOwnerSetup && capability.Kind != CapabilityOwnerRecovery {
		return errors.New("capability kind is invalid")
	}
	if (capability.Kind == CapabilityOwnerSetup && capability.UserID != "") ||
		(capability.Kind == CapabilityOwnerRecovery && !validUUID(capability.UserID)) {
		return errors.New("capability subject is invalid")
	}
	if capability.CreatedAt.IsZero() || !capability.ExpiresAt.After(capability.CreatedAt) {
		return errors.New("capability lifetime is invalid")
	}
	if capability.ConsumedAt != nil && capability.RevokedAt != nil {
		return errors.New("capability cannot be consumed and revoked")
	}
	if capability.ConsumedAt != nil && capability.ConsumedAt.Before(capability.CreatedAt) {
		return errors.New("capability consumption timestamp is invalid")
	}
	if capability.RevokedAt != nil && capability.RevokedAt.Before(capability.CreatedAt) {
		return errors.New("capability revocation timestamp is invalid")
	}
	return nil
}

func (capability Capability) UsableAt(now time.Time) bool {
	return capability.Validate() == nil && !now.Before(capability.CreatedAt) && now.Before(capability.ExpiresAt) &&
		capability.ConsumedAt == nil && capability.RevokedAt == nil
}

func (capability *Capability) Consume(now time.Time) error {
	if !capability.UsableAt(now) {
		return errors.New("capability is unavailable")
	}
	consumedAt := now.UTC()
	capability.ConsumedAt = &consumedAt
	return nil
}

func (capability *Capability) Revoke(now time.Time) error {
	if !capability.UsableAt(now) {
		return errors.New("capability is unavailable")
	}
	revokedAt := now.UTC()
	capability.RevokedAt = &revokedAt
	return nil
}

func containsControl(value string) bool {
	return strings.ContainsFunc(value, unicode.IsControl)
}

func validUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	decoded, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	return err == nil && len(decoded) == 16 && decoded[6]>>4 >= 1 && decoded[6]>>4 <= 5 && decoded[8]&0xc0 == 0x80
}

type OwnerCredential struct {
	UserID       string
	PasswordHash string
	ChangedAt    time.Time
	CreatedAt    time.Time
}

func (credential OwnerCredential) Validate() error {
	if !validUUID(credential.UserID) || len(credential.PasswordHash) < 32 || len(credential.PasswordHash) > 512 ||
		!strings.HasPrefix(credential.PasswordHash, "$argon2id$") {
		return errors.New("owner credential is invalid")
	}
	if credential.CreatedAt.IsZero() || credential.ChangedAt.Before(credential.CreatedAt) {
		return errors.New("owner credential timestamps are invalid")
	}
	return nil
}

type AuditOutcome string

const (
	AuditSucceeded AuditOutcome = "succeeded"
	AuditFailed    AuditOutcome = "failed"
	AuditBlocked   AuditOutcome = "blocked"
	AuditLocked    AuditOutcome = "locked"
)

type SecurityAuditEvent struct {
	OccurredAt    time.Time
	EventType     string
	ActorUserID   string
	SubjectUserID string
	SessionID     string
	Outcome       AuditOutcome
	OriginDigest  []byte
	Metadata      json.RawMessage
}

var auditEventTypePattern = regexp.MustCompile(`^[a-z_]+(?:\.[a-z_]+)+\.v[1-9][0-9]*$`)

func (event SecurityAuditEvent) Validate() error {
	if event.OccurredAt.IsZero() || !auditEventTypePattern.MatchString(event.EventType) {
		return errors.New("security audit event identity is invalid")
	}
	for _, id := range []string{event.ActorUserID, event.SubjectUserID, event.SessionID} {
		if id != "" && !validUUID(id) {
			return errors.New("security audit event subject is invalid")
		}
	}
	if event.Outcome != AuditSucceeded && event.Outcome != AuditFailed &&
		event.Outcome != AuditBlocked && event.Outcome != AuditLocked {
		return errors.New("security audit outcome is invalid")
	}
	if len(event.OriginDigest) != 0 && len(event.OriginDigest) != 32 {
		return errors.New("security audit origin is invalid")
	}
	if len(event.Metadata) == 0 || len(event.Metadata) > 4096 || !json.Valid(event.Metadata) {
		return errors.New("security audit metadata is invalid")
	}
	var metadata map[string]any
	if err := json.Unmarshal(event.Metadata, &metadata); err != nil || metadata == nil {
		return errors.New("security audit metadata must be an object")
	}
	return nil
}

type DeliveryKind string

const (
	DeliveryInvitation     DeliveryKind = "invitation"
	DeliveryMemberCode     DeliveryKind = "member_login_code"
	DeliveryOwnerRecovery  DeliveryKind = "owner_recovery"
	DeliverySecurityNotice DeliveryKind = "security_notice"
)

type DeliveryState string

const (
	DeliveryPending   DeliveryState = "pending"
	DeliverySending   DeliveryState = "sending"
	DeliverySent      DeliveryState = "sent"
	DeliveryFailed    DeliveryState = "failed"
	DeliveryAbandoned DeliveryState = "abandoned"
)

type EmailDelivery struct {
	ID             string
	Kind           DeliveryKind
	RecipientEmail string
	SubjectUserID  string
	InvitationID   string
	ChallengeID    string
	State          DeliveryState
	AttemptCount   int16
	LastAttemptAt  *time.Time
	SentAt         *time.Time
	ErrorCode      string
	CreatedAt      time.Time
}

func (delivery EmailDelivery) Validate() error {
	if !validUUID(delivery.ID) {
		return errors.New("email delivery ID is invalid")
	}
	if delivery.Kind != DeliveryInvitation && delivery.Kind != DeliveryMemberCode &&
		delivery.Kind != DeliveryOwnerRecovery && delivery.Kind != DeliverySecurityNotice {
		return errors.New("email delivery kind is invalid")
	}
	address, _, err := NormalizeEmail(delivery.RecipientEmail)
	if err != nil || address != delivery.RecipientEmail {
		return errors.New("email delivery recipient is invalid")
	}
	for _, id := range []string{delivery.SubjectUserID, delivery.InvitationID, delivery.ChallengeID} {
		if id != "" && !validUUID(id) {
			return errors.New("email delivery relationship is invalid")
		}
	}
	if delivery.AttemptCount < 0 || delivery.AttemptCount > 20 || delivery.CreatedAt.IsZero() {
		return errors.New("email delivery attempt state is invalid")
	}
	if delivery.LastAttemptAt != nil && delivery.LastAttemptAt.Before(delivery.CreatedAt) {
		return errors.New("email delivery attempt timestamp is invalid")
	}
	if delivery.SentAt != nil && delivery.SentAt.Before(delivery.CreatedAt) {
		return errors.New("email delivery sent timestamp is invalid")
	}
	switch delivery.State {
	case DeliveryPending:
		if delivery.SentAt != nil || delivery.ErrorCode != "" {
			return errors.New("pending email delivery state is invalid")
		}
	case DeliverySending:
		if delivery.LastAttemptAt == nil || delivery.SentAt != nil || delivery.ErrorCode != "" {
			return errors.New("sending email delivery state is invalid")
		}
	case DeliverySent:
		if delivery.SentAt == nil || delivery.ErrorCode != "" {
			return errors.New("sent email delivery state is invalid")
		}
	case DeliveryFailed:
		if delivery.SentAt != nil || (delivery.ErrorCode != "temporary_failure" && delivery.ErrorCode != "permanent_failure") {
			return errors.New("failed email delivery state is invalid")
		}
	case DeliveryAbandoned:
		if delivery.SentAt != nil || delivery.ErrorCode != "abandoned" {
			return errors.New("abandoned email delivery state is invalid")
		}
	default:
		return errors.New("email delivery state is invalid")
	}
	return nil
}

func (delivery *EmailDelivery) MarkSent(now time.Time) error {
	if (delivery.State != DeliveryPending && delivery.State != DeliverySending) || now.Before(delivery.CreatedAt) {
		return errors.New("email delivery cannot be marked sent")
	}
	completedAt := now.UTC()
	if delivery.State == DeliveryPending {
		delivery.AttemptCount++
		delivery.LastAttemptAt = &completedAt
	}
	delivery.State = DeliverySent
	delivery.SentAt = &completedAt
	delivery.ErrorCode = ""
	return delivery.Validate()
}
