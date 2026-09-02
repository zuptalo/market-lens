// Package api wires the REST API, middleware, and production frontend serving.
package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/credentials"
	"market-lens/server/internal/httpx"
	"market-lens/server/internal/identity"
)

type Database interface {
	Ping(context.Context) error
}

type SessionAuthenticator interface {
	AuthenticateSession(context.Context, string) (auth.Principal, error)
}

type OwnerIdentity interface {
	SetupRequired(context.Context) (bool, error)
	BootstrapOwner(context.Context, identity.BootstrapRequest) (identity.BootstrapResult, error)
}

type OwnerAuthentication interface {
	StartSignIn(context.Context, auth.SignInStartRequest) (auth.SignInStartResult, error)
	LoginOwner(context.Context, auth.OwnerLoginRequest) (auth.AuthenticationResult, error)
	Account(context.Context, string) (auth.Account, error)
	ListSessions(context.Context, string, string) ([]auth.SessionSummary, error)
	RevokeSession(context.Context, string, string) error
	RevokeAllSessions(context.Context, string) error
}

type MemberAuthentication interface {
	VerifyMemberCode(context.Context, auth.MemberCodeVerifyRequest) (auth.AuthenticationResult, error)
}

type MemberAdministration interface {
	ListMembers(context.Context, identity.Actor, string, int) (identity.MemberPage, error)
	UnlockMember(context.Context, identity.Actor, string) error
	SetMemberStatus(context.Context, identity.Actor, string, identity.Status) error
	Member(context.Context, identity.Actor, string) (identity.Member, error)
}

type InvitationAdministration interface {
	ListInvitations(context.Context, identity.Actor, string, int) (identity.InvitationPage, error)
	CreateInvitation(context.Context, identity.Actor, string) (identity.Invitation, error)
	ResendInvitation(context.Context, identity.Actor, string) (identity.Invitation, error)
	RevokeInvitation(context.Context, identity.Actor, string) error
	AcceptInvitation(context.Context, identity.AcceptInvitationRequest) (identity.BootstrapResult, error)
}

// StreamRevalidator re-checks a session that already holds an open stream. A long-lived
// connection outlives the request-time authentication check, so it has to keep asking.
type StreamRevalidator interface {
	RevalidateSession(context.Context, string) error
}

type IntegrationStatusReader interface {
	Statuses(context.Context) ([]credentials.Status, error)
}

// IntegrationAdministration lets the owner check a configuration before committing to it, and
// change it only once it is proven to work.
type IntegrationAdministration interface {
	IntegrationSettings(context.Context, identity.Actor) (identity.IntegrationSettings, error)
	VerifyIntegrations(context.Context, identity.Actor, identity.IntegrationUpdate) (identity.IntegrationOutcomes, error)
	UpdateIntegrations(context.Context, identity.Actor, identity.IntegrationUpdate) (identity.IntegrationOutcomes, error)
}

// InstanceConfiguration describes which configuration values this installation provisioned
// for itself and which the operator must retain. It holds no secret and no derivative of one:
// the signing key generation is an ordinal, and the credential key is reported only as
// present or absent. The credential key is always external to the database, which is why its
// source is a constant rather than a field.
type InstanceConfiguration struct {
	SigningKeySource      string
	SigningKeyGeneration  int
	ExternalKeyConfigured bool
}

type Dependencies struct {
	Database                Database
	Authenticator           SessionAuthenticator
	Identity                OwnerIdentity
	Authentication          OwnerAuthentication
	Integrations            IntegrationStatusReader
	IntegrationAdmin        IntegrationAdministration
	InstanceConfiguration   InstanceConfiguration
	MemberAuth              MemberAuthentication
	Members                 MemberAdministration
	Invitations             InvitationAdministration
	SecureCookies           bool
	AllowedOrigins          []string
	StaticDir               string
	Version                 string
	MarketData              MarketDataReader
	Instruments             InstrumentReader
	Features                FeatureReader
	Events                  EventReader
	EventHeartbeat          time.Duration
	EventBatchLimit         int
	EventRevalidator        StreamRevalidator
	EventRevalidateInterval time.Duration
}

func NewRouter(deps Dependencies) http.Handler {
	public := http.NewServeMux()
	public.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{
			"status": "ok", "service": "market-lens", "version": deps.Version,
		})
	})
	public.HandleFunc("GET /api/v1/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if deps.Database == nil || deps.Database.Ping(ctx) != nil {
			httpx.JSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	public.HandleFunc("GET /api/v1/setup/status", setupStatusHandler(deps.Identity))
	public.HandleFunc("POST /api/v1/auth/owner/setup", completeOwnerSetupHandler(deps.Identity, deps.SecureCookies))
	public.HandleFunc("POST /api/v1/auth/owner/login", ownerLoginHandler(deps.Authentication, deps.SecureCookies))
	public.HandleFunc("POST /api/v1/auth/sign-in/start", signInStartHandler(deps.Authentication))
	public.HandleFunc("POST /api/v1/auth/member/code/verify", verifyMemberCodeHandler(deps.MemberAuth, deps.SecureCookies))
	public.HandleFunc("POST /api/v1/auth/invitations/accept", acceptInvitationHandler(deps.Invitations, deps.SecureCookies))
	if deps.StaticDir != "" {
		authenticationShell := spaHandler(deps.StaticDir)
		public.Handle("GET /login", authenticationShell)
		public.Handle("GET /setup", authenticationShell)
		public.Handle("GET /invite", authenticationShell)
		public.Handle("GET /assets/", authenticationShell)
		public.Handle("GET /favicon.svg", authenticationShell)
	}

	protected := http.NewServeMux()
	if deps.MarketData != nil {
		protected.HandleFunc("GET /api/v1/market-data/imports", listImportRunsHandler(deps.MarketData))
		protected.HandleFunc("GET /api/v1/market-data/imports/{id}", getImportRunHandler(deps.MarketData))
		protected.HandleFunc("GET /api/v1/market-data/quality-findings", listQualityFindingsHandler(deps.MarketData))
	}
	if deps.Instruments != nil {
		protected.HandleFunc("GET /api/v1/instruments", listInstrumentsHandler(deps.Instruments))
		// Registered before the identifier route so the literal segment wins; Go's ServeMux
		// prefers the more specific pattern, and a test holds that guarantee in place.
		protected.HandleFunc("GET /api/v1/instruments/sectors", listSectorsHandler(deps.Instruments))
		protected.HandleFunc("GET /api/v1/instruments/{id}", getInstrumentHandler(deps.Instruments))
		protected.HandleFunc("GET /api/v1/instruments/{id}/prices", listInstrumentPricesHandler(deps.Instruments))
		protected.HandleFunc("GET /api/v1/instruments/{id}/history", getInstrumentHistoryHandler(deps.Instruments))
	}
	if deps.Features != nil {
		protected.HandleFunc("GET /api/v1/instruments/{id}/features", getInstrumentFeaturesHandler(deps.Features))
		protected.HandleFunc("GET /api/v1/feature-definitions", listFeatureDefinitionsHandler(deps.Features))
		protected.HandleFunc("GET /api/v1/feature-runs", listFeatureRunsHandler(deps.Features))
	}
	if deps.Events != nil {
		protected.HandleFunc("GET /api/v1/events", eventsHandler(deps.Events, deps.EventHeartbeat,
			deps.EventBatchLimit, deps.EventRevalidator, deps.EventRevalidateInterval))
	}
	protected.HandleFunc("GET /api/v1/account", accountHandler(deps.Authentication))
	// Every owner route passes the same administration boundary, so a new one cannot be added
	// without it. Ownership is read from the persisted principal alone.
	protected.Handle("GET /api/v1/owner/integrations",
		httpx.RequireOwner(ownerIntegrationStatusHandler(deps.Integrations, deps.IntegrationAdmin, deps.InstanceConfiguration)))
	protected.Handle("POST /api/v1/owner/integrations/verify",
		httpx.RequireOwner(httpx.RequireCSRF(ownerIntegrationChangeHandler(deps.IntegrationAdmin, false))))
	protected.Handle("PUT /api/v1/owner/integrations",
		httpx.RequireOwner(httpx.RequireCSRF(ownerIntegrationChangeHandler(deps.IntegrationAdmin, true))))
	protected.Handle("GET /api/v1/owner/members", httpx.RequireOwner(listMembersHandler(deps.Members)))
	protected.Handle("POST /api/v1/owner/members/{memberId}/unlock", httpx.RequireOwner(httpx.RequireCSRF(unlockMemberHandler(deps.Members))))
	protected.Handle("PATCH /api/v1/owner/members/{memberId}/status", httpx.RequireOwner(httpx.RequireCSRF(memberStatusHandler(deps.Members))))
	protected.Handle("GET /api/v1/owner/invitations", httpx.RequireOwner(listInvitationsHandler(deps.Invitations)))
	protected.Handle("POST /api/v1/owner/invitations", httpx.RequireOwner(httpx.RequireCSRF(createInvitationHandler(deps.Invitations))))
	protected.Handle("POST /api/v1/owner/invitations/{invitationId}/resend", httpx.RequireOwner(httpx.RequireCSRF(resendInvitationHandler(deps.Invitations))))
	protected.Handle("DELETE /api/v1/owner/invitations/{invitationId}", httpx.RequireOwner(httpx.RequireCSRF(revokeInvitationHandler(deps.Invitations))))
	protected.HandleFunc("GET /api/v1/account/sessions", listSessionsHandler(deps.Authentication))
	protected.Handle("DELETE /api/v1/account/sessions", httpx.RequireCSRF(revokeAllSessionsHandler(deps.Authentication)))
	protected.Handle("DELETE /api/v1/account/sessions/{sessionId}", httpx.RequireCSRF(revokeSessionHandler(deps.Authentication)))
	protected.Handle("POST /api/v1/auth/logout", httpx.RequireCSRF(logoutHandler(deps.Authentication, deps.SecureCookies)))

	if deps.StaticDir != "" {
		protected.Handle("/", spaHandler(deps.StaticDir))
	} else {
		protected.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				httpx.Error(w, http.StatusNotFound, "not found")
				return
			}
			http.NotFound(w, r)
		})
	}

	root := http.NewServeMux()
	root.Handle("GET /api/v1/health", public)
	root.Handle("GET /api/v1/ready", public)
	root.Handle("GET /api/v1/setup/status", public)
	root.Handle("POST /api/v1/auth/owner/setup", public)
	root.Handle("POST /api/v1/auth/owner/login", public)
	root.Handle("POST /api/v1/auth/sign-in/start", public)
	root.Handle("POST /api/v1/auth/member/code/verify", public)
	root.Handle("POST /api/v1/auth/invitations/accept", public)
	root.HandleFunc("POST /api/v1/auth/owner/recovery/request", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	root.HandleFunc("POST /api/v1/auth/owner/recovery/complete", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	if deps.StaticDir != "" {
		root.Handle("GET /login", public)
		root.Handle("GET /setup", public)
		root.Handle("GET /invite", public)
		root.Handle("GET /assets/", public)
		root.Handle("GET /favicon.svg", public)
	}
	root.Handle("/", authenticateSession(deps.Authenticator, protected))
	return httpx.Chain(root, httpx.Recover, httpx.Log, httpx.CORS(deps.AllowedOrigins))
}

func authenticateSession(authenticator SessionAuthenticator, next http.Handler) http.Handler {
	protected := httpx.RequirePrincipal(next)
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if authenticator == nil {
			protected.ServeHTTP(writer, request)
			return
		}
		cookie, err := request.Cookie(httpx.SessionCookieName)
		if err != nil || cookie.Value == "" {
			protected.ServeHTTP(writer, request)
			return
		}
		principal, err := authenticator.AuthenticateSession(request.Context(), cookie.Value)
		if err != nil {
			protected.ServeHTTP(writer, request)
			return
		}
		request = httpx.WithPrincipal(request, httpx.Principal{
			UserID: principal.UserID, Role: principal.Role, SessionID: principal.SessionID, VerifyCSRF: principal.VerifyCSRF,
		})
		protected.ServeHTTP(writer, request)
	})
}
