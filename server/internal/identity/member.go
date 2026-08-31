package identity

import (
	"context"
	"errors"
	"time"

	"market-lens/server/internal/auth"
)

var (
	// ErrOwnerRequired is returned when a non-owner attempts owner administration.
	ErrOwnerRequired = errors.New("owner authorization is required")
	// ErrMemberNotFound is returned for an unknown or non-member subject.
	ErrMemberNotFound = errors.New("member was not found")
	// ErrMemberSelfAction is returned when the owner targets their own account.
	ErrMemberSelfAction = errors.New("the owner cannot administer their own account")
)

// MemberLoginPresentation is the owner-visible sign-in availability of a member.
type MemberLoginPresentation string

const (
	MemberLoginAvailable         MemberLoginPresentation = "available"
	MemberTemporarilyBlocked     MemberLoginPresentation = "temporarily_blocked"
	MemberAdministrativelyLocked MemberLoginPresentation = "administratively_locked"
)

// Actor is the authenticated principal performing an identity operation.
type Actor struct {
	UserID string
	Role   Role
}

// Owner reports whether the actor may perform owner administration.
func (actor Actor) Owner() bool { return actor.Role == RoleOwner && validUUID(actor.UserID) }

// Member is account and security metadata only. It deliberately carries no private
// financial activity, because owner administration must not expose another user's research.
type Member struct {
	ID                 string
	Email              string
	DisplayName        string
	Status             Status
	LoginState         MemberLoginPresentation
	BlockedUntil       *time.Time
	LockedAt           *time.Time
	ActiveSessionCount int
	CreatedAt          time.Time
}

// MemberPage is one cursor-paginated slice of member administration metadata.
type MemberPage struct {
	Members    []Member
	NextCursor string
}

// MemberLoginStateFor derives the owner-visible presentation from durable throttling fields.
func MemberLoginStateFor(blockedUntil, lockedAt *time.Time, now time.Time) MemberLoginPresentation {
	if lockedAt != nil {
		return MemberAdministrativelyLocked
	}
	if blockedUntil != nil && now.Before(*blockedUntil) {
		return MemberTemporarilyBlocked
	}
	return MemberLoginAvailable
}

// MemberAdministration abstracts the durable member throttling store owned by the auth package,
// so identity can authorize owner administration without importing its persistence details.
type MemberAdministration interface {
	UnlockMember(ctx context.Context, ownerID, memberID string, now time.Time) error
}

// requireOwner confirms the caller is the persisted owner. The client-supplied role is never
// trusted on its own, so the durable record is re-read for every administration call.
func (service *Service) requireOwner(ctx context.Context, actor Actor) error {
	if !actor.Owner() {
		return ErrOwnerRequired
	}
	owner, err := service.repository.IsOwner(ctx, actor.UserID)
	if err != nil {
		return err
	}
	if !owner {
		return ErrOwnerRequired
	}
	return nil
}

// ListMembers returns owner-visible account and security metadata for every member.
func (service *Service) ListMembers(ctx context.Context, actor Actor, cursor string, limit int) (MemberPage, error) {
	if err := service.requireOwner(ctx, actor); err != nil {
		return MemberPage{}, err
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	return service.repository.ListMembers(ctx, actor, cursor, limit, service.now().UTC())
}

// UnlockMember clears an administrative lock for one member on owner authority alone. It never
// reactivates a deactivated account and never grants a session.
func (service *Service) UnlockMember(ctx context.Context, actor Actor, memberID string) error {
	if err := service.requireOwner(ctx, actor); err != nil {
		return err
	}
	if !validUUID(memberID) {
		return ErrMemberNotFound
	}
	if memberID == actor.UserID {
		return ErrMemberSelfAction
	}
	if service.memberAccess == nil {
		return errors.New("member administration store is not configured")
	}
	err := service.memberAccess.UnlockMember(ctx, actor.UserID, memberID, service.now().UTC())
	if errors.Is(err, auth.ErrMemberNotFound) {
		return ErrMemberNotFound
	}
	if errors.Is(err, auth.ErrOwnerRequired) {
		return ErrOwnerRequired
	}
	return err
}

// SetMemberStatus activates or deactivates one member on owner authority alone.
func (service *Service) SetMemberStatus(ctx context.Context, actor Actor, memberID string, status Status) error {
	if err := service.requireOwner(ctx, actor); err != nil {
		return err
	}
	if status != StatusActive && status != StatusDeactivated {
		return errors.New("member status is invalid")
	}
	if !validUUID(memberID) {
		return ErrMemberNotFound
	}
	if memberID == actor.UserID {
		return ErrMemberSelfAction
	}
	_, err := service.repository.SetMemberStatus(ctx, actor.UserID, memberID, status, service.now().UTC())
	return err
}

// Member returns one member's administration metadata for the owner.
func (service *Service) Member(ctx context.Context, actor Actor, memberID string) (Member, error) {
	if err := service.requireOwner(ctx, actor); err != nil {
		return Member{}, err
	}
	return service.repository.Member(ctx, actor, memberID, service.now().UTC())
}
