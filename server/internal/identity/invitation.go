package identity

import (
	"errors"
	"time"
)

var (
	// ErrInvitationUnavailable covers every unusable capability without disclosing which
	// case occurred: unknown, expired, revoked, already accepted, or superseded by a resend.
	ErrInvitationUnavailable = errors.New("invitation is unavailable")
	// ErrInvitationConflict reports an invitation that cannot be created or changed because a
	// pending invitation or an account already exists for the address.
	ErrInvitationConflict = errors.New("invitation conflicts with existing access")
	// ErrInvitationResendLimit reports a bounded resend ceiling.
	ErrInvitationResendLimit = errors.New("invitation resend limit reached")
)

// InvitationTTL bounds how long an invitation capability stays usable.
const InvitationTTL = 7 * 24 * time.Hour

// MaxInvitationResends bounds resend attempts, matching the persisted constraint.
const MaxInvitationResends = 100

type InvitationState string

const (
	InvitationPending  InvitationState = "pending"
	InvitationAccepted InvitationState = "accepted"
	InvitationRevoked  InvitationState = "revoked"
	InvitationExpired  InvitationState = "expired"
)

// Invitation is an owner-issued, single-use, expiring capability for one email address.
// The capability itself is never stored; only its keyed digest is retained.
type Invitation struct {
	ID               string
	Email            string
	NormalizedEmail  string
	TokenDigest      []byte
	State            InvitationState
	ExpiresAt        time.Time
	AcceptedByUserID string
	AcceptedAt       *time.Time
	RevokedAt        *time.Time
	CreatedByUserID  string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ResendCount      int
	DeliveryState    DeliveryState
	DeliveryError    string
}

// InvitationPage is one cursor-paginated slice of owner-visible invitation state.
type InvitationPage struct {
	Items      []Invitation
	NextCursor string
}

// NewInvitation builds a pending invitation that stores only the capability digest.
func NewInvitation(id, email, ownerID string, tokenDigest []byte, now time.Time) (Invitation, error) {
	if !validUUID(id) || !validUUID(ownerID) {
		return Invitation{}, errors.New("invitation identity is invalid")
	}
	if len(tokenDigest) != 32 {
		return Invitation{}, errors.New("invitation capability digest is invalid")
	}
	if now.IsZero() {
		return Invitation{}, errors.New("invitation requires a clock")
	}
	address, normalized, err := NormalizeEmail(email)
	if err != nil {
		return Invitation{}, err
	}
	created := now.UTC()
	return Invitation{
		ID: id, Email: address, NormalizedEmail: normalized, TokenDigest: append([]byte(nil), tokenDigest...),
		State: InvitationPending, ExpiresAt: created.Add(InvitationTTL), CreatedByUserID: ownerID,
		CreatedAt: created, UpdatedAt: created, DeliveryState: DeliveryPending,
	}, nil
}

// UsableAt reports whether the capability may still be accepted at now.
func (invitation Invitation) UsableAt(now time.Time) bool {
	return invitation.State == InvitationPending && len(invitation.TokenDigest) == 32 &&
		!now.Before(invitation.CreatedAt) && now.Before(invitation.ExpiresAt)
}

// Resend replaces the capability and restarts the expiry window, invalidating every earlier
// capability for this invitation.
func (invitation *Invitation) Resend(tokenDigest []byte, now time.Time) error {
	if invitation.State != InvitationPending {
		return ErrInvitationUnavailable
	}
	if len(tokenDigest) != 32 {
		return errors.New("invitation capability digest is invalid")
	}
	if now.Before(invitation.CreatedAt) {
		return errors.New("invitation resend precedes creation")
	}
	if invitation.ResendCount >= MaxInvitationResends {
		return ErrInvitationResendLimit
	}
	invitation.TokenDigest = append([]byte(nil), tokenDigest...)
	invitation.ExpiresAt = now.UTC().Add(InvitationTTL)
	invitation.ResendCount++
	invitation.UpdatedAt = now.UTC()
	invitation.DeliveryState = DeliveryPending
	invitation.DeliveryError = ""
	return nil
}

// Revoke retires a pending invitation. Accepted invitations are historical records and
// cannot be revoked, because doing so would misrepresent how the member joined.
func (invitation *Invitation) Revoke(now time.Time) error {
	if invitation.State != InvitationPending {
		return ErrInvitationUnavailable
	}
	if now.Before(invitation.CreatedAt) {
		return errors.New("invitation revocation precedes creation")
	}
	revokedAt := now.UTC()
	invitation.State = InvitationRevoked
	invitation.RevokedAt = &revokedAt
	invitation.UpdatedAt = revokedAt
	return nil
}

// Accept consumes the invitation exactly once for the newly created member.
func (invitation *Invitation) Accept(userID string, now time.Time) error {
	if !validUUID(userID) {
		return errors.New("invitation acceptance identity is invalid")
	}
	if !invitation.UsableAt(now) {
		return ErrInvitationUnavailable
	}
	acceptedAt := now.UTC()
	invitation.State = InvitationAccepted
	invitation.AcceptedByUserID = userID
	invitation.AcceptedAt = &acceptedAt
	invitation.UpdatedAt = acceptedAt
	return nil
}
