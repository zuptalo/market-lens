package identity_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/db"
	"market-lens/server/internal/identity"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestInvitationDomainEnforcesSevenDayExpiryAndSingleUse(t *testing.T) {
	now := time.Date(2026, time.August, 30, 10, 0, 0, 0, time.UTC)
	digest := make([]byte, 32)
	for index := range digest {
		digest[index] = byte(index)
	}
	invitation, err := identity.NewInvitation(
		"70000000-0000-4000-8000-000000000001", " Member@Example.COM ",
		"10000000-0000-4000-8000-000000000001", digest, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	// The domain is case-insensitive so it is lowered for delivery, while the local part is
	// preserved because some mail servers treat it as significant.
	if invitation.NormalizedEmail != "member@example.com" || invitation.Email != "Member@example.com" {
		t.Fatalf("invitation email normalisation = %#v", invitation)
	}
	if invitation.State != identity.InvitationPending || !invitation.ExpiresAt.Equal(now.Add(identity.InvitationTTL)) {
		t.Fatalf("invitation lifetime = %#v", invitation)
	}
	if !invitation.UsableAt(now) || !invitation.UsableAt(invitation.ExpiresAt.Add(-time.Nanosecond)) ||
		invitation.UsableAt(invitation.ExpiresAt) {
		t.Fatalf("invitation usability window = %#v", invitation)
	}

	// Resending mints a new capability and extends the window; the old digest is replaced.
	replacement := make([]byte, 32)
	for index := range replacement {
		replacement[index] = byte(index + 100)
	}
	resentAt := now.Add(time.Hour)
	if err := invitation.Resend(replacement, resentAt); err != nil {
		t.Fatal(err)
	}
	if invitation.ResendCount != 1 || string(invitation.TokenDigest) != string(replacement) ||
		!invitation.ExpiresAt.Equal(resentAt.Add(identity.InvitationTTL)) {
		t.Fatalf("resent invitation = %#v", invitation)
	}

	// Acceptance is single use.
	if err := invitation.Accept("11111111-1111-4111-8111-111111111111", resentAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if invitation.State != identity.InvitationAccepted || invitation.AcceptedAt == nil ||
		invitation.UsableAt(resentAt.Add(2*time.Minute)) {
		t.Fatalf("accepted invitation = %#v", invitation)
	}
	if err := invitation.Accept("22222222-2222-4222-8222-222222222222", resentAt.Add(2*time.Minute)); err == nil {
		t.Fatal("an accepted invitation was accepted twice")
	}
	if err := invitation.Resend(replacement, resentAt.Add(2*time.Minute)); err == nil {
		t.Fatal("an accepted invitation was resent")
	}
	if err := invitation.Revoke(resentAt.Add(2 * time.Minute)); err == nil {
		t.Fatal("an accepted invitation was revoked")
	}
}

func TestRevokedAndExpiredInvitationsAreNeverUsable(t *testing.T) {
	now := time.Date(2026, time.August, 30, 10, 0, 0, 0, time.UTC)
	digest := make([]byte, 32)
	invitation, err := identity.NewInvitation("70000000-0000-4000-8000-000000000002", "member@example.com",
		"10000000-0000-4000-8000-000000000001", digest, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := invitation.Revoke(now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if invitation.State != identity.InvitationRevoked || invitation.RevokedAt == nil ||
		invitation.UsableAt(now.Add(2*time.Minute)) {
		t.Fatalf("revoked invitation = %#v", invitation)
	}
	if err := invitation.Accept("11111111-1111-4111-8111-111111111111", now.Add(2*time.Minute)); err == nil {
		t.Fatal("a revoked invitation was accepted")
	}

	expiring, err := identity.NewInvitation("70000000-0000-4000-8000-000000000003", "other@example.com",
		"10000000-0000-4000-8000-000000000001", digest, now)
	if err != nil {
		t.Fatal(err)
	}
	if expiring.UsableAt(now.Add(identity.InvitationTTL)) {
		t.Fatal("an expired invitation remained usable")
	}
	if err := expiring.Accept("11111111-1111-4111-8111-111111111111", now.Add(identity.InvitationTTL)); err == nil {
		t.Fatal("an expired invitation was accepted")
	}
}

func TestOnlyOnePendingInvitationExistsPerAddress(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	clock, secrets, bootstrap, _ := provisionMemberAdministration(t, pool)
	repository := identity.NewRepository(pool)
	now := clock.Now().Add(time.Hour)

	first := mustInvitation(t, "70000000-0000-4000-8000-000000000011", "invitee@example.com", bootstrap.User.ID,
		secrets.Digest(auth.PurposeInvitation, "capability-one"), now)
	if err := repository.CreateInvitation(context.Background(), identity.CreateInvitationParams{
		Invitation: first, DeliveryID: "71000000-0000-4000-8000-000000000011", Now: now,
	}); err != nil {
		t.Fatal(err)
	}

	// A second pending invitation for the same address is a conflict, not a silent duplicate.
	second := mustInvitation(t, "70000000-0000-4000-8000-000000000012", "INVITEE@example.com", bootstrap.User.ID,
		secrets.Digest(auth.PurposeInvitation, "capability-two"), now.Add(time.Minute))
	err := repository.CreateInvitation(context.Background(), identity.CreateInvitationParams{
		Invitation: second, DeliveryID: "71000000-0000-4000-8000-000000000012", Now: now.Add(time.Minute),
	})
	if !errors.Is(err, identity.ErrInvitationConflict) {
		t.Fatalf("duplicate pending invitation error = %v, want ErrInvitationConflict", err)
	}

	// Revoking the first frees the address for a new invitation.
	if err := repository.RevokeInvitation(context.Background(), first.ID, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateInvitation(context.Background(), identity.CreateInvitationParams{
		Invitation: second, DeliveryID: "71000000-0000-4000-8000-000000000012", Now: now.Add(3 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	// Revocation is idempotent and never resurrects a capability.
	if err := repository.RevokeInvitation(context.Background(), first.ID, now.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
}

func TestResendInvalidatesEveryEarlierInvitationCapability(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	clock, secrets, bootstrap, _ := provisionMemberAdministration(t, pool)
	repository := identity.NewRepository(pool)
	now := clock.Now().Add(time.Hour)

	invitation := mustInvitation(t, "70000000-0000-4000-8000-000000000021", "invitee@example.com", bootstrap.User.ID,
		secrets.Digest(auth.PurposeInvitation, "original-capability"), now)
	if err := repository.CreateInvitation(context.Background(), identity.CreateInvitationParams{
		Invitation: invitation, DeliveryID: "71000000-0000-4000-8000-000000000021", Now: now,
	}); err != nil {
		t.Fatal(err)
	}

	resent, err := repository.ResendInvitation(context.Background(), invitation.ID,
		secrets.Digest(auth.PurposeInvitation, "replacement-capability"),
		"71000000-0000-4000-8000-000000000022", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if resent.ResendCount != 1 {
		t.Fatalf("resend count = %d, want 1", resent.ResendCount)
	}

	// The original capability must no longer accept.
	if _, err := repository.AcceptInvitation(context.Background(), identity.AcceptInvitationParams{
		TokenDigest: secrets.Digest(auth.PurposeInvitation, "original-capability"),
		Email:       "invitee@example.com", DisplayName: "Invitee",
		UserID: "11111111-1111-4111-8111-111111111121",
		Session: identityMemberSession(t, secrets, "21000000-0000-4000-8000-000000000021",
			"11111111-1111-4111-8111-111111111121", now.Add(2*time.Hour)),
		Now: now.Add(2 * time.Hour), OriginDigest: secrets.Digest(auth.PurposeOrigin, "203.0.113.0/24"),
	}); !errors.Is(err, identity.ErrInvitationUnavailable) {
		t.Fatalf("superseded capability error = %v, want ErrInvitationUnavailable", err)
	}
}

func TestConcurrentAcceptanceCreatesExactlyOneMemberWithoutAPassword(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	clock, secrets, bootstrap, _ := provisionMemberAdministration(t, pool)
	repository := identity.NewRepository(pool)
	now := clock.Now().Add(time.Hour)

	invitation := mustInvitation(t, "70000000-0000-4000-8000-000000000031", "invitee@example.com", bootstrap.User.ID,
		secrets.Digest(auth.PurposeInvitation, "race-capability"), now)
	if err := repository.CreateInvitation(context.Background(), identity.CreateInvitationParams{
		Invitation: invitation, DeliveryID: "71000000-0000-4000-8000-000000000031", Now: now,
	}); err != nil {
		t.Fatal(err)
	}

	const attempts = 8
	type outcome struct {
		user identity.User
		err  error
	}
	results := make(chan outcome, attempts)
	var group sync.WaitGroup
	for index := range attempts {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			userID := "11111111-1111-4111-8111-" + padTwelve(900+index)
			sessionID := "21000000-0000-4000-8000-" + padTwelve(900+index)
			user, err := repository.AcceptInvitation(context.Background(), identity.AcceptInvitationParams{
				TokenDigest: secrets.Digest(auth.PurposeInvitation, "race-capability"),
				Email:       "invitee@example.com", DisplayName: "Invitee",
				UserID:  userID,
				Session: identityMemberSession(t, secrets, sessionID, userID, now.Add(time.Hour)),
				Now:     now.Add(time.Hour), OriginDigest: secrets.Digest(auth.PurposeOrigin, "203.0.113.0/24"),
			})
			results <- outcome{user: user, err: err}
		}(index)
	}
	group.Wait()
	close(results)

	accepted := 0
	for result := range results {
		if result.err == nil {
			accepted++
			continue
		}
		if !errors.Is(result.err, identity.ErrInvitationUnavailable) {
			t.Fatalf("unexpected concurrent acceptance error: %v", result.err)
		}
	}
	if accepted != 1 {
		t.Fatalf("concurrent acceptances = %d, want exactly 1", accepted)
	}

	var members, consumed, credentials, verified, sessions int
	if err := pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM users WHERE role='member'),
		(SELECT count(*) FROM invitations WHERE state='accepted'),
		(SELECT count(*) FROM owner_credentials c JOIN users u ON u.id=c.user_id WHERE u.role='member'),
		(SELECT count(*) FROM users WHERE role='member' AND email_verified_at IS NOT NULL),
		(SELECT count(*) FROM sessions s JOIN users u ON u.id=s.user_id WHERE u.role='member')`).
		Scan(&members, &consumed, &credentials, &verified, &sessions); err != nil {
		t.Fatal(err)
	}
	if members != 1 || consumed != 1 {
		t.Fatalf("members=%d acceptedInvitations=%d, want exactly one of each", members, consumed)
	}
	// Members are passwordless and their address is verified by holding the capability.
	if credentials != 0 || verified != 1 {
		t.Fatalf("member password credentials=%d verified=%d", credentials, verified)
	}
	if sessions != 1 {
		t.Fatalf("member sessions created = %d, want the single accepted attempt to sign in", sessions)
	}

	var audits, events int
	if err := pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM security_audit_events WHERE event_type='member.invitation_accepted.v1'),
		(SELECT count(*) FROM client_events WHERE event_type='member.changed.v1' AND scope='owner')`).
		Scan(&audits, &events); err != nil {
		t.Fatal(err)
	}
	if audits != 1 || events < 1 {
		t.Fatalf("acceptance audits=%d ownerEvents=%d", audits, events)
	}
}

func TestAcceptanceRejectsAMismatchedOrConflictingIdentity(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	clock, secrets, bootstrap, _ := provisionMemberAdministration(t, pool)
	repository := identity.NewRepository(pool)
	now := clock.Now().Add(time.Hour)
	origin := secrets.Digest(auth.PurposeOrigin, "203.0.113.0/24")

	invitation := mustInvitation(t, "70000000-0000-4000-8000-000000000041", "invitee@example.com", bootstrap.User.ID,
		secrets.Digest(auth.PurposeInvitation, "identity-capability"), now)
	if err := repository.CreateInvitation(context.Background(), identity.CreateInvitationParams{
		Invitation: invitation, DeliveryID: "71000000-0000-4000-8000-000000000041", Now: now,
	}); err != nil {
		t.Fatal(err)
	}

	// The capability is bound to the invited address, so another address cannot use it.
	if _, err := repository.AcceptInvitation(context.Background(), identity.AcceptInvitationParams{
		TokenDigest: secrets.Digest(auth.PurposeInvitation, "identity-capability"),
		Email:       "someone-else@example.com", DisplayName: "Someone Else",
		UserID: "11111111-1111-4111-8111-111111111141",
		Session: identityMemberSession(t, secrets, "21000000-0000-4000-8000-000000000041",
			"11111111-1111-4111-8111-111111111141", now.Add(time.Hour)),
		Now: now.Add(time.Hour), OriginDigest: origin,
	}); !errors.Is(err, identity.ErrInvitationUnavailable) {
		t.Fatalf("wrong-address acceptance error = %v, want ErrInvitationUnavailable", err)
	}

	// The owner's own address can never be claimed through an invitation.
	ownerInvitation := mustInvitation(t, "70000000-0000-4000-8000-000000000042", "owner@example.com", bootstrap.User.ID,
		secrets.Digest(auth.PurposeInvitation, "owner-capability"), now)
	if err := repository.CreateInvitation(context.Background(), identity.CreateInvitationParams{
		Invitation: ownerInvitation, DeliveryID: "71000000-0000-4000-8000-000000000042", Now: now,
	}); !errors.Is(err, identity.ErrInvitationConflict) {
		t.Fatalf("owner-address invitation error = %v, want ErrInvitationConflict", err)
	}

	var users int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE role='member'`).Scan(&users); err != nil {
		t.Fatal(err)
	}
	if users != 0 {
		t.Fatalf("rejected acceptances created %d members", users)
	}
}

func mustInvitation(t *testing.T, id, email, ownerID string, digest []byte, now time.Time) identity.Invitation {
	t.Helper()
	invitation, err := identity.NewInvitation(id, email, ownerID, digest, now)
	if err != nil {
		t.Fatal(err)
	}
	return invitation
}

var _ *pgxpool.Pool
