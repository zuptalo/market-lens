package auth

import (
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type RevokeReason string

const (
	RevokeLogout             RevokeReason = "logout"
	RevokeOwnerPasswordReset RevokeReason = "owner_password_reset"
	RevokeOwnerRecovery      RevokeReason = "owner_recovery"
	RevokeUserDeactivated    RevokeReason = "user_deactivated"
	RevokeUserRequested      RevokeReason = "user_requested"
	RevokeAllDevices         RevokeReason = "all_devices"
	RevokeCredentialChanged  RevokeReason = "credential_changed"
	RevokeAdministrative     RevokeReason = "administrative"
	RevokeSigningKeyRotated  RevokeReason = "signing_key_rotated"
)

type Session struct {
	ID                string
	UserID            string
	TokenDigest       []byte
	CSRFDigest        []byte
	CreatedAt         time.Time
	LastSeenAt        time.Time
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
	RevokedAt         *time.Time
	RevokedReason     RevokeReason
	DeviceLabel       string
	OriginDigest      []byte
}

type SessionSummary struct {
	ID                string
	Current           bool
	DeviceLabel       string
	CreatedAt         time.Time
	LastSeenAt        time.Time
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
	Revoked           bool
}

func (session Session) Validate() error {
	if !validSessionUUID(session.ID) || !validSessionUUID(session.UserID) {
		return errors.New("session identity is invalid")
	}
	if len(session.TokenDigest) != 32 || len(session.CSRFDigest) != 32 || len(session.OriginDigest) != 32 {
		return errors.New("session digest is invalid")
	}
	if session.CreatedAt.IsZero() || session.LastSeenAt.Before(session.CreatedAt) ||
		!session.IdleExpiresAt.After(session.LastSeenAt) || !session.AbsoluteExpiresAt.After(session.CreatedAt) ||
		session.IdleExpiresAt.After(session.AbsoluteExpiresAt) {
		return errors.New("session lifetime is invalid")
	}
	if strings.TrimSpace(session.DeviceLabel) != session.DeviceLabel ||
		utf8.RuneCountInString(session.DeviceLabel) < 1 || utf8.RuneCountInString(session.DeviceLabel) > 160 ||
		strings.ContainsFunc(session.DeviceLabel, unicode.IsControl) {
		return errors.New("session device label is invalid")
	}
	if session.RevokedAt == nil && session.RevokedReason != "" {
		return errors.New("session revocation state is invalid")
	}
	if session.RevokedAt != nil {
		if session.RevokedAt.Before(session.CreatedAt) || !validRevokeReason(session.RevokedReason) {
			return errors.New("session revocation state is invalid")
		}
	}
	return nil
}

func (session Session) ActiveAt(now time.Time, userActive bool) bool {
	return userActive && session.Validate() == nil && session.RevokedAt == nil &&
		!now.Before(session.CreatedAt) && now.Before(session.IdleExpiresAt) && now.Before(session.AbsoluteExpiresAt)
}

func (session *Session) Touch(now time.Time, idleTimeout time.Duration) error {
	if idleTimeout <= 0 || now.Before(session.LastSeenAt) || !session.ActiveAt(now, true) {
		return errors.New("session cannot be refreshed")
	}
	idleExpiresAt := now.Add(idleTimeout)
	if idleExpiresAt.After(session.AbsoluteExpiresAt) {
		idleExpiresAt = session.AbsoluteExpiresAt
	}
	if !idleExpiresAt.After(now) {
		return errors.New("session cannot be refreshed")
	}
	session.LastSeenAt = now.UTC()
	session.IdleExpiresAt = idleExpiresAt.UTC()
	return nil
}

func (session *Session) Revoke(reason RevokeReason, now time.Time) error {
	if session.RevokedAt != nil {
		return nil
	}
	if !validRevokeReason(reason) || now.Before(session.CreatedAt) {
		return errors.New("session revocation is invalid")
	}
	revokedAt := now.UTC()
	session.RevokedAt = &revokedAt
	session.RevokedReason = reason
	return nil
}

func (session Session) Summary(currentSessionID string) SessionSummary {
	return SessionSummary{
		ID: session.ID, Current: session.ID == currentSessionID, DeviceLabel: session.DeviceLabel,
		CreatedAt: session.CreatedAt, LastSeenAt: session.LastSeenAt, IdleExpiresAt: session.IdleExpiresAt,
		AbsoluteExpiresAt: session.AbsoluteExpiresAt, Revoked: session.RevokedAt != nil,
	}
}

func validRevokeReason(reason RevokeReason) bool {
	switch reason {
	case RevokeLogout, RevokeOwnerPasswordReset, RevokeOwnerRecovery, RevokeUserDeactivated, RevokeUserRequested,
		RevokeAllDevices, RevokeCredentialChanged, RevokeAdministrative, RevokeSigningKeyRotated:
		return true
	default:
		return false
	}
}

func validSessionUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	decoded, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	return err == nil && len(decoded) == 16 && decoded[6]>>4 >= 1 && decoded[6]>>4 <= 5 && decoded[8]&0xc0 == 0x80
}
