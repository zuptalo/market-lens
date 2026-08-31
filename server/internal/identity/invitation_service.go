package identity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode"
	"unicode/utf8"

	"market-lens/server/internal/auth"
	appmail "market-lens/server/internal/mail"
)

// AcceptInvitationRequest carries one passwordless invitation acceptance.
type AcceptInvitationRequest struct {
	Capability  string
	Email       string
	DisplayName string
	DeviceLabel string
	Origin      string
}

// invitationURL builds the absolute link carried by the invitation email. The capability lives
// in the fragment so it is never sent to the server in a request line, logged by a proxy, or
// captured in a referrer header.
func (service *Service) invitationURL(capability string) (string, error) {
	if service.appBaseURL == "" {
		return "", errors.New("application base URL is not configured")
	}
	return fmt.Sprintf("%s/invite#%s", service.appBaseURL, capability), nil
}

// ListInvitations returns owner-visible invitation and delivery state.
func (service *Service) ListInvitations(ctx context.Context, actor Actor, cursor string, limit int) (InvitationPage, error) {
	if err := service.requireOwner(ctx, actor); err != nil {
		return InvitationPage{}, err
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	return service.repository.ListInvitations(ctx, actor, cursor, limit, service.now().UTC())
}

// CreateInvitation issues one expiring single-use capability and hands it to the mail
// transport. Delivery failure is recorded and reported as safe state rather than losing the
// invitation, so the owner can resend without creating a duplicate.
func (service *Service) CreateInvitation(ctx context.Context, actor Actor, email string) (Invitation, error) {
	if err := service.requireOwner(ctx, actor); err != nil {
		return Invitation{}, err
	}
	now := service.now().UTC()
	invitationID, err := newUUID()
	if err != nil {
		return Invitation{}, err
	}
	deliveryID, err := newUUID()
	if err != nil {
		return Invitation{}, err
	}
	capability, err := service.secrets.Capability()
	if err != nil {
		return Invitation{}, err
	}
	invitation, err := NewInvitation(invitationID, email, actor.UserID,
		service.secrets.Digest(auth.PurposeInvitation, capability), now)
	if err != nil {
		return Invitation{}, err
	}
	if err := service.repository.CreateInvitation(ctx, CreateInvitationParams{
		Invitation: invitation, DeliveryID: deliveryID, Now: now,
	}); err != nil {
		return Invitation{}, err
	}
	invitation.DeliveryState = service.deliverInvitation(ctx, invitation, deliveryID, capability)
	return invitation, nil
}

// ResendInvitation mints a replacement capability, invalidating every earlier one.
func (service *Service) ResendInvitation(ctx context.Context, actor Actor, invitationID string) (Invitation, error) {
	if err := service.requireOwner(ctx, actor); err != nil {
		return Invitation{}, err
	}
	now := service.now().UTC()
	deliveryID, err := newUUID()
	if err != nil {
		return Invitation{}, err
	}
	capability, err := service.secrets.Capability()
	if err != nil {
		return Invitation{}, err
	}
	invitation, err := service.repository.ResendInvitation(ctx, invitationID,
		service.secrets.Digest(auth.PurposeInvitation, capability), deliveryID, now)
	if err != nil {
		return Invitation{}, err
	}
	invitation.DeliveryState = service.deliverInvitation(ctx, invitation, deliveryID, capability)
	return invitation, nil
}

// deliverInvitation hands one capability to the mail transport and records the outcome. The
// capability is never persisted, logged, or returned to the caller.
func (service *Service) deliverInvitation(ctx context.Context, invitation Invitation, deliveryID, capability string) DeliveryState {
	delivered := false
	defer func() {
		if err := service.repository.MarkInvitationDelivery(ctx, invitation.ID, deliveryID,
			service.now().UTC(), delivered); err != nil {
			service.logger().WarnContext(ctx, "invitation delivery state could not be recorded",
				"invitation_id", invitation.ID, "error", err)
		}
	}()
	if service.mail == nil {
		service.logger().ErrorContext(ctx, "invitation cannot be delivered without a mail transport",
			"invitation_id", invitation.ID)
		return DeliveryFailed
	}
	link, err := service.invitationURL(capability)
	if err != nil {
		service.logger().ErrorContext(ctx, "invitation link could not be built",
			"invitation_id", invitation.ID, "error", err)
		return DeliveryFailed
	}
	message, err := appmail.InvitationMessage(invitation.Email, link)
	if err != nil {
		service.logger().ErrorContext(ctx, "invitation message could not be built",
			"invitation_id", invitation.ID, "error", err)
		return DeliveryFailed
	}
	if err := service.mail.Send(ctx, message); err != nil {
		service.logger().WarnContext(ctx, "invitation delivery failed",
			"invitation_id", invitation.ID, "delivery_id", deliveryID, "error", err)
		return DeliveryFailed
	}
	delivered = true
	return DeliverySent
}

// RevokeInvitation retires a pending invitation idempotently.
func (service *Service) RevokeInvitation(ctx context.Context, actor Actor, invitationID string) error {
	if err := service.requireOwner(ctx, actor); err != nil {
		return err
	}
	return service.repository.RevokeInvitation(ctx, invitationID, service.now().UTC())
}

// AcceptInvitation creates the member and their first session from a capability alone. No
// password is ever requested, and every unusable capability fails identically.
func (service *Service) AcceptInvitation(ctx context.Context, request AcceptInvitationRequest) (BootstrapResult, error) {
	if request.Capability == "" || len(request.Capability) > 512 {
		return BootstrapResult{}, ErrInvitationUnavailable
	}
	if strings.TrimSpace(request.DisplayName) != request.DisplayName ||
		utf8.RuneCountInString(request.DisplayName) < 1 || utf8.RuneCountInString(request.DisplayName) > 120 ||
		strings.ContainsFunc(request.DisplayName, unicode.IsControl) {
		return BootstrapResult{}, errors.New("display name is invalid")
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
	sessionToken, err := service.secrets.SessionToken()
	if err != nil {
		return BootstrapResult{}, err
	}
	csrfToken, err := service.secrets.CSRFToken()
	if err != nil {
		return BootstrapResult{}, err
	}
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
	if err := session.Validate(); err != nil {
		return BootstrapResult{}, ErrInvitationUnavailable
	}
	user, err := service.repository.AcceptInvitation(ctx, AcceptInvitationParams{
		TokenDigest: service.secrets.Digest(auth.PurposeInvitation, request.Capability),
		Email:       request.Email, DisplayName: request.DisplayName, UserID: userID,
		Session: session, Now: now, OriginDigest: service.secrets.Digest(auth.PurposeOrigin, request.Origin),
	})
	if err != nil {
		return BootstrapResult{}, err
	}
	return BootstrapResult{
		User: user, Session: session.Summary(session.ID), SessionToken: sessionToken, CSRFToken: csrfToken,
	}, nil
}

func (service *Service) logger() *slog.Logger {
	if service.log != nil {
		return service.log
	}
	return slog.Default()
}
