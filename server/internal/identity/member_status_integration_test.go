package identity_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/db"
	"market-lens/server/internal/identity"
	"market-lens/server/internal/testdb"
)

func TestDeactivationEndsEveryMemberSessionAndOutstandingCode(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	clock, secrets, bootstrap, _ := provisionMemberAdministration(t, pool)
	now := clock.Now().Add(time.Hour)
	member := seedMember(t, pool, "10000000-0000-4000-8000-000000000501", "member@example.com", "Ada Member", now)
	repository := identity.NewRepository(pool)
	authRepository := auth.NewRepository(pool)
	origin := secrets.Digest(auth.PurposeOrigin, "203.0.113.0/24")

	// Give the member a live session and an outstanding login code.
	if err := authRepository.IssueMemberChallenge(context.Background(), auth.IssueMemberChallengeParams{
		ChallengeID: newIdentityTestUUID(t), DeliveryID: newIdentityTestUUID(t), UserID: member,
		Email: "member@example.com", CodeDigest: secrets.Digest(auth.PurposeMemberCode, "515151"),
		CreatedAt: now, ExpiresAt: now.Add(auth.MemberCodeTTL), OriginDigest: origin,
	}); err != nil {
		t.Fatal(err)
	}
	session := identityMemberSession(t, secrets, "21000000-0000-4000-8000-000000000501", member, now)
	if result, err := authRepository.VerifyMemberChallenge(context.Background(), auth.VerifyMemberChallengeParams{
		UserID: member, CodeDigest: secrets.Digest(auth.PurposeMemberCode, "515151"),
		Session: session, Now: now.Add(time.Minute), OriginDigest: origin,
	}); err != nil || result.Outcome != auth.MemberLoginSucceeded {
		t.Fatalf("member sign-in = %v %v", result.Outcome, err)
	}
	if err := authRepository.IssueMemberChallenge(context.Background(), auth.IssueMemberChallengeParams{
		ChallengeID: newIdentityTestUUID(t), DeliveryID: newIdentityTestUUID(t), UserID: member,
		Email: "member@example.com", CodeDigest: secrets.Digest(auth.PurposeMemberCode, "626262"),
		CreatedAt: now.Add(2 * time.Minute), ExpiresAt: now.Add(2*time.Minute + auth.MemberCodeTTL),
		OriginDigest: origin,
	}); err != nil {
		t.Fatal(err)
	}

	deactivatedAt := now.Add(5 * time.Minute)
	result, err := repository.SetMemberStatus(context.Background(), bootstrap.User.ID, member,
		identity.StatusDeactivated, deactivatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != identity.StatusDeactivated {
		t.Fatalf("deactivated member = %#v", result)
	}

	var activeSessions, activeChallenges, revokedReasons, ownerEvents, memberEvents int
	if err := pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM sessions WHERE user_id=$1 AND revoked_at IS NULL),
		(SELECT count(*) FROM member_login_challenges WHERE user_id=$1 AND state='active'),
		(SELECT count(*) FROM sessions WHERE user_id=$1 AND revoked_reason='user_deactivated'),
		(SELECT count(*) FROM client_events WHERE event_type='member.changed.v1' AND scope='owner'),
		(SELECT count(*) FROM client_events WHERE event_type='sessions.revoked.v1' AND subject_user_id=$1)`,
		member).Scan(&activeSessions, &activeChallenges, &revokedReasons, &ownerEvents, &memberEvents); err != nil {
		t.Fatal(err)
	}
	if activeSessions != 0 || activeChallenges != 0 || revokedReasons != 1 {
		t.Fatalf("after deactivation sessions=%d challenges=%d revoked=%d",
			activeSessions, activeChallenges, revokedReasons)
	}
	// Both the owner console and the member's own devices must learn about this immediately.
	if ownerEvents < 1 || memberEvents < 1 {
		t.Fatalf("deactivation events owner=%d member=%d", ownerEvents, memberEvents)
	}

	// A deactivated member can no longer be issued a code at all.
	if err := authRepository.IssueMemberChallenge(context.Background(), auth.IssueMemberChallengeParams{
		ChallengeID: newIdentityTestUUID(t), DeliveryID: newIdentityTestUUID(t), UserID: member,
		Email: "member@example.com", CodeDigest: secrets.Digest(auth.PurposeMemberCode, "737373"),
		CreatedAt: deactivatedAt.Add(time.Minute), ExpiresAt: deactivatedAt.Add(time.Minute + auth.MemberCodeTTL),
		OriginDigest: origin,
	}); err == nil {
		t.Fatal("a deactivated member was issued a new login code")
	}
}

func TestReactivationRestoresAccessWithoutANewInvitationOrOldSessions(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	clock, secrets, bootstrap, _ := provisionMemberAdministration(t, pool)
	now := clock.Now().Add(time.Hour)
	member := seedMember(t, pool, "10000000-0000-4000-8000-000000000511", "member@example.com", "Ada Member", now)
	repository := identity.NewRepository(pool)
	authRepository := auth.NewRepository(pool)
	origin := secrets.Digest(auth.PurposeOrigin, "203.0.113.0/24")

	if _, err := repository.SetMemberStatus(context.Background(), bootstrap.User.ID, member,
		identity.StatusDeactivated, now); err != nil {
		t.Fatal(err)
	}
	reactivated, err := repository.SetMemberStatus(context.Background(), bootstrap.User.ID, member,
		identity.StatusActive, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if reactivated.Status != identity.StatusActive {
		t.Fatalf("reactivated member = %#v", reactivated)
	}

	// Reactivation alone restores sign-in: no second invitation is required.
	if err := authRepository.IssueMemberChallenge(context.Background(), auth.IssueMemberChallengeParams{
		ChallengeID: newIdentityTestUUID(t), DeliveryID: newIdentityTestUUID(t), UserID: member,
		Email: "member@example.com", CodeDigest: secrets.Digest(auth.PurposeMemberCode, "848484"),
		CreatedAt: now.Add(2 * time.Hour), ExpiresAt: now.Add(2*time.Hour + auth.MemberCodeTTL),
		OriginDigest: origin,
	}); err != nil {
		t.Fatal(err)
	}
	// Sessions revoked by deactivation stay revoked.
	var revived int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM sessions WHERE user_id=$1 AND revoked_at IS NULL`, member).Scan(&revived); err != nil {
		t.Fatal(err)
	}
	if revived != 0 {
		t.Fatalf("reactivation revived %d revoked sessions", revived)
	}

	// Setting the same status again is idempotent and records no second audit entry.
	if _, err := repository.SetMemberStatus(context.Background(), bootstrap.User.ID, member,
		identity.StatusActive, now.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var audits int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM security_audit_events WHERE event_type='member.reactivated.v1'`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("reactivation audits = %d, want exactly 1", audits)
	}
}

func TestMemberStatusChangesRejectNonMembersAndUnknownSubjects(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	clock, _, bootstrap, service := provisionMemberAdministration(t, pool)
	now := clock.Now().Add(time.Hour)
	repository := identity.NewRepository(pool)

	// The owner account is never administrable through the member status route.
	if _, err := repository.SetMemberStatus(context.Background(), bootstrap.User.ID, bootstrap.User.ID,
		identity.StatusDeactivated, now); !errors.Is(err, identity.ErrMemberNotFound) {
		t.Fatalf("owner status change error = %v, want ErrMemberNotFound", err)
	}
	if _, err := repository.SetMemberStatus(context.Background(), bootstrap.User.ID,
		"10000000-0000-4000-8000-0000000005ff", identity.StatusDeactivated, now); !errors.Is(err, identity.ErrMemberNotFound) {
		t.Fatalf("unknown status change error = %v, want ErrMemberNotFound", err)
	}

	// A member may not administer anyone, including themselves.
	member := seedMember(t, pool, "10000000-0000-4000-8000-000000000521", "member@example.com", "Ada Member", now)
	clock.Set(now)
	if err := service.SetMemberStatus(context.Background(),
		identity.Actor{UserID: member, Role: identity.RoleMember}, member, identity.StatusActive); !errors.Is(err, identity.ErrOwnerRequired) {
		t.Fatalf("member self-administration error = %v, want ErrOwnerRequired", err)
	}
}
