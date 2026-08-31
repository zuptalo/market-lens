package identity_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/authtest"
	"market-lens/server/internal/credentials"
	"market-lens/server/internal/db"
	"market-lens/server/internal/identity"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

// seedMember inserts an active verified member directly so member administration can be
// exercised independently of the invitation journey.
func seedMember(t *testing.T, pool *pgxpool.Pool, id, email, displayName string, createdAt time.Time) string {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `INSERT INTO users
		(id,email,normalized_email,display_name,role,status,email_verified_at,created_at,updated_at)
		VALUES ($1,$2,lower($2),$3,'member','active',$4,$4,$4)`, id, email, displayName, createdAt); err != nil {
		t.Fatal(err)
	}
	return id
}

// lockMember drives the durable lock through the real failure path so the test never
// fabricates persistence state by hand.
func lockMember(t *testing.T, repository *auth.Repository, secrets *auth.Secrets, member string, start time.Time) time.Time {
	t.Helper()
	now := start
	origin := secrets.Digest(auth.PurposeOrigin, "203.0.113.0/24")
	for failure := 1; failure <= auth.MemberLockThreshold; failure++ {
		if (failure-1)%auth.MemberBlockThreshold == 0 {
			challengeID, deliveryID := newIdentityTestUUID(t), newIdentityTestUUID(t)
			if err := repository.IssueMemberChallenge(context.Background(), auth.IssueMemberChallengeParams{
				ChallengeID: challengeID, DeliveryID: deliveryID, UserID: member, Email: "member@example.com",
				CodeDigest: secrets.Digest(auth.PurposeMemberCode, digitsFor(failure)),
				CreatedAt:  now, ExpiresAt: now.Add(auth.MemberCodeTTL), OriginDigest: origin,
			}); err != nil {
				t.Fatal(err)
			}
		}
		result, err := repository.VerifyMemberChallenge(context.Background(), auth.VerifyMemberChallengeParams{
			UserID: member, CodeDigest: secrets.Digest(auth.PurposeMemberCode, "999999"), Now: now,
			Session: identityMemberSession(t, secrets, newIdentityTestUUID(t), member, now), OriginDigest: origin,
		})
		if err != nil {
			t.Fatal(err)
		}
		if result.Outcome == auth.MemberLoginBlocked {
			now = now.Add(auth.MemberBlockDuration + time.Minute)
			continue
		}
		now = now.Add(time.Second)
	}
	state, err := repository.MemberLoginStateFor(context.Background(), member)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Locked() {
		t.Fatalf("member was not locked by the real failure path: %#v", state)
	}
	return now
}

func digitsFor(failure int) string {
	return string(rune('0'+failure%10)) + "11111"
}

var identityTestUUIDCounter int

func newIdentityTestUUID(t *testing.T) string {
	t.Helper()
	identityTestUUIDCounter++
	return "60000000-0000-4000-8000-" + padTwelve(identityTestUUIDCounter)
}

func padTwelve(value int) string {
	digits := []byte("000000000000")
	for index := len(digits) - 1; index >= 0 && value > 0; index-- {
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits)
}

func identityMemberSession(t *testing.T, secrets *auth.Secrets, sessionID, userID string, now time.Time) auth.Session {
	t.Helper()
	session := auth.Session{
		ID: sessionID, UserID: userID,
		TokenDigest: secrets.Digest(auth.PurposeSession, "session-"+sessionID),
		CSRFDigest:  secrets.Digest(auth.PurposeCSRF, "csrf-"+sessionID),
		CreatedAt:   now, LastSeenAt: now,
		IdleExpiresAt: now.Add(2 * time.Hour), AbsoluteExpiresAt: now.Add(30 * 24 * time.Hour),
		DeviceLabel: "Member device", OriginDigest: secrets.Digest(auth.PurposeOrigin, "203.0.113.0/24"),
	}
	if err := session.Validate(); err != nil {
		t.Fatal(err)
	}
	return session
}

func TestOnlyTheOwnerMayUnlockALockedMember(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	clock, secrets, bootstrap, service := provisionMemberAdministration(t, pool)
	start := clock.Now().Add(time.Hour)
	member := seedMember(t, pool, "10000000-0000-4000-8000-000000000301", "member@example.com", "Member One", start)
	other := seedMember(t, pool, "10000000-0000-4000-8000-000000000302", "other@example.com", "Member Two", start)
	authRepository := auth.NewRepository(pool)
	lockedAt := lockMember(t, authRepository, secrets, member, start)
	clock.Set(lockedAt.Add(time.Minute))

	// A member, a deactivated principal, and an anonymous caller are all refused.
	for name, actor := range map[string]identity.Actor{
		"member":       {UserID: member, Role: identity.RoleMember},
		"other member": {UserID: other, Role: identity.RoleMember},
		"anonymous":    {},
		"forged owner": {UserID: member, Role: identity.RoleOwner},
	} {
		err := service.UnlockMember(context.Background(), actor, member)
		if name == "forged owner" {
			// A client-supplied role must never be trusted over the persisted role.
			if err == nil {
				t.Fatal("a forged owner role unlocked a member")
			}
			continue
		}
		if !errors.Is(err, identity.ErrOwnerRequired) {
			t.Fatalf("%s unlock error = %v, want ErrOwnerRequired", name, err)
		}
	}
	state, err := authRepository.MemberLoginStateFor(context.Background(), member)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Locked() {
		t.Fatal("a refused unlock attempt cleared the administrative lock")
	}

	// The owner unlock clears failure state without reactivating or granting access.
	owner := identity.Actor{UserID: bootstrap.User.ID, Role: identity.RoleOwner}
	if err := service.UnlockMember(context.Background(), owner, member); err != nil {
		t.Fatal(err)
	}
	state, err = authRepository.MemberLoginStateFor(context.Background(), member)
	if err != nil {
		t.Fatal(err)
	}
	if state.Locked() || state.BlockedAt(clock.Now()) || state.ConsecutiveFailures != 0 {
		t.Fatalf("unlocked state = %#v", state)
	}

	// Unlocking an unknown or non-member subject is a not-found, never a silent success.
	if err := service.UnlockMember(context.Background(), owner, "10000000-0000-4000-8000-0000000009ff"); !errors.Is(err, identity.ErrMemberNotFound) {
		t.Fatalf("unknown member unlock error = %v, want ErrMemberNotFound", err)
	}
	if err := service.UnlockMember(context.Background(), owner, bootstrap.User.ID); err == nil {
		t.Fatal("the owner unlocked their own account")
	}
}

func TestOwnerMemberListingExposesSecurityMetadataWithoutPrivateActivity(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	clock, secrets, bootstrap, service := provisionMemberAdministration(t, pool)
	start := clock.Now().Add(time.Hour)
	locked := seedMember(t, pool, "10000000-0000-4000-8000-000000000311", "locked@example.com", "Locked Member", start)
	available := seedMember(t, pool, "10000000-0000-4000-8000-000000000312", "available@example.com", "Available Member", start)
	authRepository := auth.NewRepository(pool)
	lockedAt := lockMember(t, authRepository, secrets, locked, start)
	clock.Set(lockedAt.Add(time.Minute))

	owner := identity.Actor{UserID: bootstrap.User.ID, Role: identity.RoleMember}
	if _, err := service.ListMembers(context.Background(), owner, "", 50); !errors.Is(err, identity.ErrOwnerRequired) {
		t.Fatalf("member listing as non-owner error = %v, want ErrOwnerRequired", err)
	}

	page, err := service.ListMembers(context.Background(), identity.Actor{UserID: bootstrap.User.ID, Role: identity.RoleOwner}, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Members) != 2 {
		t.Fatalf("listed members = %d, want 2 (the owner is not a member)", len(page.Members))
	}
	byID := map[string]identity.Member{}
	for _, member := range page.Members {
		byID[member.ID] = member
	}
	if byID[locked].LoginState != identity.MemberAdministrativelyLocked || byID[locked].LockedAt == nil {
		t.Fatalf("locked member presentation = %#v", byID[locked])
	}
	if byID[available].LoginState != identity.MemberLoginAvailable ||
		byID[available].BlockedUntil != nil || byID[available].LockedAt != nil {
		t.Fatalf("available member presentation = %#v", byID[available])
	}
	if byID[available].Email != "available@example.com" || byID[available].Status != identity.StatusActive {
		t.Fatalf("member metadata = %#v", byID[available])
	}
	for _, member := range page.Members {
		if member.ID == bootstrap.User.ID {
			t.Fatal("owner administration listed the owner among members")
		}
	}
}

func TestTemporaryBlockPresentsAsBlockedOnlyWhileItLasts(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	clock, secrets, bootstrap, service := provisionMemberAdministration(t, pool)
	start := clock.Now().Add(time.Hour)
	member := seedMember(t, pool, "10000000-0000-4000-8000-000000000321", "blocked@example.com", "Blocked Member", start)
	authRepository := auth.NewRepository(pool)
	origin := secrets.Digest(auth.PurposeOrigin, "203.0.113.0/24")
	if err := authRepository.IssueMemberChallenge(context.Background(), auth.IssueMemberChallengeParams{
		ChallengeID: newIdentityTestUUID(t), DeliveryID: newIdentityTestUUID(t), UserID: member,
		Email: "blocked@example.com", CodeDigest: secrets.Digest(auth.PurposeMemberCode, "111111"),
		CreatedAt: start, ExpiresAt: start.Add(auth.MemberCodeTTL), OriginDigest: origin,
	}); err != nil {
		t.Fatal(err)
	}
	var blockedAt time.Time
	for attempt := range auth.MemberBlockThreshold {
		blockedAt = start.Add(time.Duration(attempt+1) * time.Second)
		if _, err := authRepository.VerifyMemberChallenge(context.Background(), auth.VerifyMemberChallengeParams{
			UserID: member, CodeDigest: secrets.Digest(auth.PurposeMemberCode, "999999"), Now: blockedAt,
			Session: identityMemberSession(t, secrets, newIdentityTestUUID(t), member, blockedAt), OriginDigest: origin,
		}); err != nil {
			t.Fatal(err)
		}
	}

	owner := identity.Actor{UserID: bootstrap.User.ID, Role: identity.RoleOwner}
	clock.Set(blockedAt.Add(time.Minute))
	page, err := service.ListMembers(context.Background(), owner, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if page.Members[0].LoginState != identity.MemberTemporarilyBlocked || page.Members[0].BlockedUntil == nil {
		t.Fatalf("during the block, presentation = %#v", page.Members[0])
	}

	// Once the block elapses the member becomes available again with no owner action.
	clock.Set(blockedAt.Add(auth.MemberBlockDuration + time.Minute))
	page, err = service.ListMembers(context.Background(), owner, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if page.Members[0].LoginState != identity.MemberLoginAvailable {
		t.Fatalf("after the block elapsed, presentation = %#v", page.Members[0])
	}
}

// provisionMemberAdministration bootstraps the owner and returns an identity service wired to
// the real durable member-throttling store.
func provisionMemberAdministration(t *testing.T, pool *pgxpool.Pool) (*authtest.Clock, *auth.Secrets, identity.BootstrapResult, *identity.Service) {
	t.Helper()
	clock := authtest.NewClock(time.Date(2026, time.August, 30, 9, 0, 0, 0, time.UTC))
	hasher, err := auth.NewPasswordHasher(authtest.NewRandomReader(0x81), auth.Argon2Params{
		Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	pattern := make([]byte, 67)
	for index := range pattern {
		pattern[index] = byte(index + 1)
	}
	secrets, err := auth.NewSecrets(bytes.Repeat([]byte{0x82}, 32), authtest.NewRandomReader(pattern...))
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := credentials.NewCipher(bytes.Repeat([]byte{0x83}, 32), 1)
	if err != nil {
		t.Fatal(err)
	}
	service, err := identity.NewService(identity.ServiceDependencies{
		Repository: identity.NewRepository(pool), Passwords: hasher, Secrets: secrets, Now: clock.Now,
		SetupTTL: 15 * time.Minute, OwnerIdleTimeout: 8 * time.Hour, SessionAbsoluteTimeout: 30 * 24 * time.Hour,
		EODHDValidator:   memberAdminValidator{},
		CredentialCipher: cipher,
		MemberAccess:     auth.NewRepository(pool),
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
		EODHDAPIKey: "member-admin-eodhd-key",
		SMTP:        identity.SMTPSetupConfiguration{Host: "smtp.example.test", Port: 587, From: "access@example.test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return clock, secrets, result, service
}

type memberAdminValidator struct{}

func (memberAdminValidator) ValidateCredential(context.Context, string) error { return nil }
