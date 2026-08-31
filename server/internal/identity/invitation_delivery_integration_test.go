package identity_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/authtest"
	"market-lens/server/internal/credentials"
	"market-lens/server/internal/db"
	"market-lens/server/internal/identity"
	appmail "market-lens/server/internal/mail"
	"market-lens/server/internal/mail/mailtest"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

// provisionInvitationService bootstraps the owner and returns a service wired to the supplied
// mail transport plus a log sink, so delivery outcomes and disclosure can both be inspected.
func provisionInvitationService(t *testing.T, pool *pgxpool.Pool, sender appmail.Sender) (
	*authtest.Clock, *auth.Secrets, identity.BootstrapResult, *identity.Service, *bytes.Buffer) {
	t.Helper()
	clock := authtest.NewClock(time.Date(2026, time.August, 30, 9, 0, 0, 0, time.UTC))
	hasher, err := auth.NewPasswordHasher(authtest.NewRandomReader(0xb1), auth.Argon2Params{
		Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	pattern := make([]byte, 67)
	for index := range pattern {
		pattern[index] = byte(index + 1)
	}
	secrets, err := auth.NewSecrets(bytes.Repeat([]byte{0xb2}, 32), authtest.NewRandomReader(pattern...))
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := credentials.NewCipher(bytes.Repeat([]byte{0xb3}, 32), 1)
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	service, err := identity.NewService(identity.ServiceDependencies{
		Repository: identity.NewRepository(pool), Passwords: hasher, Secrets: secrets, Now: clock.Now,
		SetupTTL: 15 * time.Minute, OwnerIdleTimeout: 8 * time.Hour, SessionAbsoluteTimeout: 30 * 24 * time.Hour,
		EODHDValidator: memberAdminValidator{}, CredentialCipher: cipher,
		MemberAccess: auth.NewRepository(pool), Mail: sender,
		AppBaseURL: "https://lens.example.test",
		Logger:     slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err != nil {
		t.Fatal(err)
	}
	capability, err := service.IssueSetupCapability(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.BootstrapOwner(context.Background(), identity.BootstrapRequest{
		Capability: capability.Token, Email: "owner@example.com", Password: "correct horse battery staple",
		DisplayName: "Market Owner", DeviceLabel: "Bootstrap browser", Origin: "192.0.2.0/24",
		EODHDAPIKey: "invitation-test-eodhd-key",
		SMTP:        identity.SMTPSetupConfiguration{Host: "smtp.example.test", Port: 587, From: "access@example.test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return clock, secrets, result, service, logs
}

func TestInvitationDeliveryEmailsAnAbsoluteLinkAndStoresNoCapability(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	captured := mailtest.NewCapture[appmail.Message]()
	clock, _, bootstrap, service, logs := provisionInvitationService(t, pool, captured)
	clock.Set(clock.Now().Add(time.Hour))
	owner := identity.Actor{UserID: bootstrap.User.ID, Role: identity.RoleOwner}

	invitation, err := service.CreateInvitation(context.Background(), owner, "invitee@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if invitation.State != identity.InvitationPending || invitation.DeliveryState != identity.DeliverySent {
		t.Fatalf("invitation = %#v", invitation)
	}

	messages := captured.Messages()
	if len(messages) != 1 || messages[0].To != "invitee@example.com" {
		t.Fatalf("captured invitation messages = %#v", messages)
	}
	// The capability travels in the URL fragment so it never reaches a server log or referrer.
	prefix := "https://lens.example.test/invite#"
	index := strings.Index(messages[0].Text, prefix)
	if index < 0 {
		t.Fatalf("invitation message lacks an absolute link: %q", messages[0].Text)
	}
	capability := strings.Fields(messages[0].Text[index+len(prefix):])[0]
	if len(capability) < 32 {
		t.Fatalf("invitation capability looks too weak: %q", capability)
	}
	if strings.Contains(strings.ToLower(messages[0].Text), "password") {
		t.Fatalf("invitation message mentions a password: %q", messages[0].Text)
	}

	// Only the digest may be persisted, and the capability must never be logged.
	var stored int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM invitations WHERE encode(token_digest,'escape') LIKE '%' || $1 || '%'`,
		capability).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != 0 {
		t.Fatal("an invitation capability was stored in plaintext")
	}
	mailtest.AssertSafeText(t, capability, logs.String())

	// The delivery is recorded as sent and surfaced to the owner listing.
	page, err := service.ListInvitations(context.Background(), owner, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].DeliveryState != identity.DeliverySent ||
		page.Items[0].DeliveryError != "" {
		t.Fatalf("listed invitation = %#v", page.Items)
	}

	// The capability actually works.
	accepted, err := service.AcceptInvitation(context.Background(), identity.AcceptInvitationRequest{
		Capability: capability, Email: "invitee@example.com", DisplayName: "Invitee",
		DeviceLabel: "Invitee browser", Origin: "203.0.113.0/24",
	})
	if err != nil {
		t.Fatal(err)
	}
	if accepted.User.Role != identity.RoleMember || accepted.SessionToken == "" || accepted.CSRFToken == "" {
		t.Fatalf("accepted result = %#v", accepted.User)
	}
}

func TestInvitationSurvivesAProviderOutageAsSafeResendableState(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	failing := mailtest.NewFailure[appmail.Message](&appmail.DeliveryError{Code: "temporary_failure", Retryable: true})
	clock, _, bootstrap, service, logs := provisionInvitationService(t, pool, failing)
	clock.Set(clock.Now().Add(time.Hour))
	owner := identity.Actor{UserID: bootstrap.User.ID, Role: identity.RoleOwner}

	// An SMTP outage must not lose the invitation or surface provider detail.
	invitation, err := service.CreateInvitation(context.Background(), owner, "invitee@example.com")
	if err != nil {
		t.Fatalf("a provider outage failed the whole invitation: %v", err)
	}
	if invitation.DeliveryState != identity.DeliveryFailed {
		t.Fatalf("invitation delivery state = %q, want failed", invitation.DeliveryState)
	}
	page, err := service.ListInvitations(context.Background(), owner, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].State != identity.InvitationPending ||
		page.Items[0].DeliveryState != identity.DeliveryFailed ||
		page.Items[0].DeliveryError != "temporary_failure" {
		t.Fatalf("failed invitation state = %#v", page.Items)
	}
	// The safe state must not expose the provider's own error text.
	if strings.Contains(logs.String(), "smtp.example.test") {
		t.Fatal("the provider host leaked into logs")
	}

	// Existing authenticated work is unaffected by the outage.
	if _, err := service.ListMembers(context.Background(), owner, "", 50); err != nil {
		t.Fatalf("owner administration broke during a mail outage: %v", err)
	}

	// The owner can resend once the provider recovers, without creating a duplicate.
	clock.Set(clock.Now().Add(time.Minute))
	resent, err := service.ResendInvitation(context.Background(), owner, invitation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resent.ResendCount != 1 {
		t.Fatalf("resend count = %d, want 1", resent.ResendCount)
	}
	page, err = service.ListInvitations(context.Background(), owner, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("resend created %d invitations, want 1", len(page.Items))
	}
}

func TestInvitationLifecycleIsOwnerOnlyAndConflictAware(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	captured := mailtest.NewCapture[appmail.Message]()
	clock, _, bootstrap, service, _ := provisionInvitationService(t, pool, captured)
	clock.Set(clock.Now().Add(time.Hour))
	owner := identity.Actor{UserID: bootstrap.User.ID, Role: identity.RoleOwner}
	member := identity.Actor{UserID: "10000000-0000-4000-8000-000000000601", Role: identity.RoleMember}

	for name, call := range map[string]func() error{
		"list": func() error { _, err := service.ListInvitations(context.Background(), member, "", 50); return err },
		"create": func() error {
			_, err := service.CreateInvitation(context.Background(), member, "x@example.com")
			return err
		},
		"resend": func() error {
			_, err := service.ResendInvitation(context.Background(), member, "70000000-0000-4000-8000-000000000001")
			return err
		},
		"revoke": func() error {
			return service.RevokeInvitation(context.Background(), member, "70000000-0000-4000-8000-000000000001")
		},
	} {
		if err := call(); err == nil {
			t.Fatalf("a member performed invitation %s", name)
		}
	}

	invitation, err := service.CreateInvitation(context.Background(), owner, "invitee@example.com")
	if err != nil {
		t.Fatal(err)
	}
	// A second pending invitation for the same address is refused rather than duplicated.
	if _, err := service.CreateInvitation(context.Background(), owner, "Invitee@Example.com"); err == nil {
		t.Fatal("a duplicate pending invitation was created")
	}
	// The owner's own address can never be invited.
	if _, err := service.CreateInvitation(context.Background(), owner, "owner@example.com"); err == nil {
		t.Fatal("the owner address was invited")
	}

	if err := service.RevokeInvitation(context.Background(), owner, invitation.ID); err != nil {
		t.Fatal(err)
	}
	// Revocation is idempotent, and a revoked capability can never be resent.
	if err := service.RevokeInvitation(context.Background(), owner, invitation.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResendInvitation(context.Background(), owner, invitation.ID); err == nil {
		t.Fatal("a revoked invitation was resent")
	}
}
