package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"market-lens/server/internal/httpx"
	"market-lens/server/internal/identity"
)

type invitationResponse struct {
	ID            string     `json:"id"`
	Email         string     `json:"email"`
	State         string     `json:"state"`
	ExpiresAt     time.Time  `json:"expires_at"`
	AcceptedAt    *time.Time `json:"accepted_at"`
	DeliveryState string     `json:"delivery_state"`
	DeliveryError *string    `json:"delivery_error"`
	ResendCount   int        `json:"resend_count"`
	CreatedAt     time.Time  `json:"created_at"`
}

// presentInvitation exposes only safe lifecycle and delivery state. The capability itself is
// delivered by email and never appears in any API response.
func presentInvitation(invitation identity.Invitation) invitationResponse {
	var deliveryError *string
	if invitation.DeliveryError != "" {
		value := invitation.DeliveryError
		deliveryError = &value
	}
	return invitationResponse{
		ID: invitation.ID, Email: invitation.Email, State: string(invitation.State),
		ExpiresAt: invitation.ExpiresAt, AcceptedAt: invitation.AcceptedAt,
		DeliveryState: string(invitation.DeliveryState), DeliveryError: deliveryError,
		ResendCount: invitation.ResendCount, CreatedAt: invitation.CreatedAt,
	}
}

// writeInvitationError maps invitation failures onto the contract without disclosing whether a
// capability exists to a caller who is not entitled to know.
func writeInvitationError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identity.ErrOwnerRequired):
		writeAuthenticationError(writer, http.StatusForbidden, "authorization_denied", "Owner access is required.")
	case errors.Is(err, identity.ErrInvitationConflict):
		writeAuthenticationError(writer, http.StatusConflict, "conflict", "That address already has access or a pending invitation.")
	case errors.Is(err, identity.ErrInvitationResendLimit):
		writeAuthenticationError(writer, http.StatusConflict, "conflict", "This invitation cannot be resent again.")
	case errors.Is(err, identity.ErrInvitationUnavailable):
		writeAuthenticationError(writer, http.StatusNotFound, "not_found", "The invitation was not found.")
	default:
		writeAuthenticationError(writer, http.StatusServiceUnavailable, "temporarily_unavailable", "Invitation administration is temporarily unavailable.")
	}
}

func listInvitationsHandler(service InvitationAdministration) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		actor, ok := actorFromRequest(request)
		if !ok {
			writeAuthenticationError(writer, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
			return
		}
		if service == nil {
			writeAuthenticationError(writer, http.StatusServiceUnavailable, "temporarily_unavailable", "Invitation administration is temporarily unavailable.")
			return
		}
		limit := 50
		if raw := request.URL.Query().Get("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > 200 {
				writeAuthenticationError(writer, http.StatusBadRequest, "invalid_request", "The request is invalid.")
				return
			}
			limit = parsed
		}
		page, err := service.ListInvitations(request.Context(), actor, request.URL.Query().Get("cursor"), limit)
		if err != nil {
			writeInvitationError(writer, err)
			return
		}
		items := make([]invitationResponse, 0, len(page.Items))
		for _, invitation := range page.Items {
			items = append(items, presentInvitation(invitation))
		}
		httpx.JSON(writer, http.StatusOK, map[string]any{"items": items, "next_cursor": page.NextCursor})
	}
}

func createInvitationHandler(service InvitationAdministration) http.HandlerFunc {
	type createRequest struct {
		Email string `json:"email"`
	}
	return func(writer http.ResponseWriter, request *http.Request) {
		actor, ok := actorFromRequest(request)
		if !ok {
			writeAuthenticationError(writer, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
			return
		}
		if service == nil {
			writeAuthenticationError(writer, http.StatusServiceUnavailable, "temporarily_unavailable", "Invitation administration is temporarily unavailable.")
			return
		}
		var input createRequest
		if decodeAuthenticationJSON(writer, request, &input) != nil {
			return
		}
		invitation, err := service.CreateInvitation(request.Context(), actor, input.Email)
		if err != nil {
			writeInvitationError(writer, err)
			return
		}
		httpx.JSON(writer, http.StatusCreated, presentInvitation(invitation))
	}
}

func resendInvitationHandler(service InvitationAdministration) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		actor, ok := actorFromRequest(request)
		if !ok {
			writeAuthenticationError(writer, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
			return
		}
		if service == nil {
			writeAuthenticationError(writer, http.StatusServiceUnavailable, "temporarily_unavailable", "Invitation administration is temporarily unavailable.")
			return
		}
		invitation, err := service.ResendInvitation(request.Context(), actor, request.PathValue("invitationId"))
		if err != nil {
			writeInvitationError(writer, err)
			return
		}
		httpx.JSON(writer, http.StatusOK, presentInvitation(invitation))
	}
}

func revokeInvitationHandler(service InvitationAdministration) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		actor, ok := actorFromRequest(request)
		if !ok {
			writeAuthenticationError(writer, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
			return
		}
		if service == nil {
			writeAuthenticationError(writer, http.StatusServiceUnavailable, "temporarily_unavailable", "Invitation administration is temporarily unavailable.")
			return
		}
		if err := service.RevokeInvitation(request.Context(), actor, request.PathValue("invitationId")); err != nil {
			writeInvitationError(writer, err)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}
}

// acceptInvitationHandler completes passwordless onboarding. It accepts no password field, and
// every unusable capability produces the same response so invitations cannot be probed.
func acceptInvitationHandler(service InvitationAdministration, secureCookies bool) http.HandlerFunc {
	type acceptRequest struct {
		Capability  string `json:"capability"`
		Email       string `json:"email"`
		DisplayName string `json:"display_name"`
	}
	return func(writer http.ResponseWriter, request *http.Request) {
		if service == nil {
			writeAuthenticationError(writer, http.StatusServiceUnavailable, "temporarily_unavailable", "Authentication is temporarily unavailable.")
			return
		}
		var input acceptRequest
		if decodeAuthenticationJSON(writer, request, &input) != nil {
			return
		}
		device, origin := authenticationClientMetadata(request)
		result, err := service.AcceptInvitation(request.Context(), identity.AcceptInvitationRequest{
			Capability: input.Capability, Email: input.Email, DisplayName: input.DisplayName,
			DeviceLabel: device, Origin: origin,
		})
		if err != nil {
			switch {
			case errors.Is(err, identity.ErrInvitationConflict):
				writeAuthenticationError(writer, http.StatusConflict, "conflict", "That address already has access.")
			case errors.Is(err, identity.ErrInvitationUnavailable):
				writeAuthenticationError(writer, http.StatusBadRequest, "invalid_capability", "The invitation is invalid or unavailable.")
			default:
				writeAuthenticationError(writer, http.StatusBadRequest, "invalid_request", "The request is invalid.")
			}
			return
		}
		writeAuthenticated(writer, http.StatusCreated, result.SessionToken, result.CSRFToken,
			result.Session.AbsoluteExpiresAt, secureCookies, accountResponse{
				ID: result.User.ID, Email: result.User.Email, DisplayName: result.User.DisplayName,
				Role: string(result.User.Role), Status: string(result.User.Status),
				EmailVerifiedAt: result.User.EmailVerifiedAt,
			})
	}
}

func memberStatusHandler(service MemberAdministration) http.HandlerFunc {
	type statusRequest struct {
		Status string `json:"status"`
	}
	return func(writer http.ResponseWriter, request *http.Request) {
		actor, ok := actorFromRequest(request)
		if !ok {
			writeAuthenticationError(writer, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
			return
		}
		if service == nil {
			writeAuthenticationError(writer, http.StatusServiceUnavailable, "temporarily_unavailable", "Member administration is temporarily unavailable.")
			return
		}
		var input statusRequest
		if decodeAuthenticationJSON(writer, request, &input) != nil {
			return
		}
		status := identity.Status(input.Status)
		if status != identity.StatusActive && status != identity.StatusDeactivated {
			writeAuthenticationError(writer, http.StatusBadRequest, "invalid_request", "The request is invalid.")
			return
		}
		memberID := request.PathValue("memberId")
		if err := service.SetMemberStatus(request.Context(), actor, memberID, status); err != nil {
			writeMemberAdministrationError(writer, err)
			return
		}
		member, err := service.Member(request.Context(), actor, memberID)
		if err != nil {
			writeMemberAdministrationError(writer, err)
			return
		}
		httpx.JSON(writer, http.StatusOK, presentMember(member))
	}
}
