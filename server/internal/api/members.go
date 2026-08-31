package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/httpx"
	"market-lens/server/internal/identity"
)

// writeRateLimited reports a refused attempt with a coarse whole-second hint. The hint is
// deliberately imprecise so it cannot be used to probe an account's position in a bucket.
func writeRateLimited(writer http.ResponseWriter, retryAfter time.Duration) {
	if retryAfter <= 0 {
		retryAfter = auth.RateRetryGranularity
	}
	writer.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
	writeAuthenticationError(writer, http.StatusTooManyRequests, "rate_limited", "Too many attempts. Try again later.")
}

// verifyMemberCodeHandler exchanges one emailed six-digit code for a session. Every failure
// mode shares a single 401 body so no response can distinguish a wrong code from an unknown
// address, a blocked member, or a locked member.
func verifyMemberCodeHandler(service MemberAuthentication, secureCookies bool) http.HandlerFunc {
	type verifyRequest struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	return func(writer http.ResponseWriter, request *http.Request) {
		var input verifyRequest
		if service == nil {
			writeAuthenticationError(writer, http.StatusServiceUnavailable, "temporarily_unavailable", "Authentication is temporarily unavailable.")
			return
		}
		if decodeAuthenticationJSON(writer, request, &input) != nil {
			return
		}
		device, origin := authenticationClientMetadata(request)
		result, err := service.VerifyMemberCode(request.Context(), auth.MemberCodeVerifyRequest{
			Email: input.Email, Code: input.Code, DeviceLabel: device, Origin: origin,
		})
		if err != nil {
			var limited *auth.RateLimitedError
			switch {
			case errors.As(err, &limited):
				writeRateLimited(writer, limited.RetryAfter)
			case errors.Is(err, auth.ErrAuthenticationFailed):
				writeAuthenticationError(writer, http.StatusUnauthorized, "authentication_failed", "Authentication failed.")
			default:
				writeAuthenticationError(writer, http.StatusServiceUnavailable, "temporarily_unavailable", "Authentication is temporarily unavailable.")
			}
			return
		}
		verifiedAt := result.Account.EmailVerifiedAt
		writeAuthenticated(writer, http.StatusOK, result.SessionToken, result.CSRFToken,
			result.Session.AbsoluteExpiresAt, secureCookies, accountResponse{
				ID: result.Account.ID, Email: result.Account.Email, DisplayName: result.Account.DisplayName,
				Role: result.Account.Role, Status: result.Account.Status, EmailVerifiedAt: &verifiedAt,
			})
	}
}

type memberResponse struct {
	ID                 string     `json:"id"`
	Email              string     `json:"email"`
	DisplayName        string     `json:"display_name"`
	Status             string     `json:"status"`
	LoginState         string     `json:"login_state"`
	BlockedUntil       *time.Time `json:"blocked_until"`
	LockedAt           *time.Time `json:"locked_at"`
	ActiveSessionCount int        `json:"active_session_count"`
	CreatedAt          time.Time  `json:"created_at"`
}

func presentMember(member identity.Member) memberResponse {
	return memberResponse{
		ID: member.ID, Email: member.Email, DisplayName: member.DisplayName,
		Status: string(member.Status), LoginState: string(member.LoginState),
		BlockedUntil: member.BlockedUntil, LockedAt: member.LockedAt,
		ActiveSessionCount: member.ActiveSessionCount, CreatedAt: member.CreatedAt,
	}
}

// actorFromRequest builds the identity actor from the authenticated principal only. A client
// can never supply its own role, because the principal comes from the session middleware.
func actorFromRequest(request *http.Request) (identity.Actor, bool) {
	principal, ok := httpx.PrincipalFromContext(request)
	if !ok {
		return identity.Actor{}, false
	}
	return identity.Actor{UserID: principal.UserID, Role: identity.Role(principal.Role)}, true
}

// writeMemberAdministrationError maps owner-administration failures onto the contract without
// revealing whether a subject exists to an unauthorized caller.
func writeMemberAdministrationError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identity.ErrOwnerRequired):
		writeAuthenticationError(writer, http.StatusForbidden, "authorization_denied", "Owner access is required.")
	case errors.Is(err, identity.ErrMemberNotFound):
		writeAuthenticationError(writer, http.StatusNotFound, "not_found", "The member was not found.")
	case errors.Is(err, identity.ErrMemberSelfAction):
		writeAuthenticationError(writer, http.StatusConflict, "conflict", "The owner account cannot be administered here.")
	default:
		writeAuthenticationError(writer, http.StatusServiceUnavailable, "temporarily_unavailable", "Member administration is temporarily unavailable.")
	}
}

func listMembersHandler(service MemberAdministration) http.HandlerFunc {
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
		limit := 50
		if raw := request.URL.Query().Get("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > 200 {
				writeAuthenticationError(writer, http.StatusBadRequest, "invalid_request", "The request is invalid.")
				return
			}
			limit = parsed
		}
		page, err := service.ListMembers(request.Context(), actor, request.URL.Query().Get("cursor"), limit)
		if err != nil {
			writeMemberAdministrationError(writer, err)
			return
		}
		members := make([]memberResponse, 0, len(page.Members))
		for _, member := range page.Members {
			members = append(members, presentMember(member))
		}
		httpx.JSON(writer, http.StatusOK, map[string]any{"members": members, "next_cursor": page.NextCursor})
	}
}

func unlockMemberHandler(service MemberAdministration) http.HandlerFunc {
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
		if err := service.UnlockMember(request.Context(), actor, request.PathValue("memberId")); err != nil {
			writeMemberAdministrationError(writer, err)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}
}
