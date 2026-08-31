package auth_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/authtest"
	"market-lens/server/internal/db"
	clientevents "market-lens/server/internal/events"
	"market-lens/server/internal/identity"
	appmail "market-lens/server/internal/mail"
	"market-lens/server/internal/mail/mailtest"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

// account is the whole feature assembled the way an installation actually uses it: one owner
// bootstraps, invites somebody, both sign in, the member gets locked out and unlocked, mail
// breaks and recovers, the process restarts, and every step is replayable from the outbox.
type account struct {
	pool     *pgxpool.Pool
	clock    *authtest.Clock
	secrets  *auth.Secrets
	hasher   *auth.PasswordHasher
	mailbox  *mailtest.Capture[appmail.Message]
	sender   appmail.Sender
	logs     *bytes.Buffer
	auth     *auth.Service
	identity *identity.Service
}

// rebuild constructs fresh services over the same database, which is what a restart is.
func (a *account) rebuild(t *testing.T, sender appmail.Sender) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(a.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	authService, err := auth.NewService(auth.ServiceDependencies{
		Repository: auth.NewRepository(a.pool), Passwords: a.hasher, Secrets: a.secrets, Now: a.clock.Now,
		OwnerIdleTimeout: 8 * time.Hour, SessionAbsoluteTimeout: 30 * 24 * time.Hour,
		MemberCodes: auth.NewMemberCodeGenerator(authtest.NewRandomReader(0x2f, 0x83, 0x1c, 0x57)),
		Mail:        sender, Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	identityService, err := identity.NewService(identity.ServiceDependencies{
		Repository: identity.NewRepository(a.pool), Passwords: a.hasher, Secrets: a.secrets, Now: a.clock.Now,
		SetupTTL: 15 * time.Minute, OwnerIdleTimeout: 8 * time.Hour, SessionAbsoluteTimeout: 30 * 24 * time.Hour,
		EODHDValidator:   eodhdValidatorFunc(func(context.Context, string) error { return nil }),
		CredentialCipher: ownerTestCredentialCipher(t),
		MemberAccess:     auth.NewRepository(a.pool), Mail: sender,
		AppBaseURL: "https://lens.example.test", Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	a.auth, a.identity, a.sender = authService, identityService, sender
}

func newAccountFixture(t *testing.T) *account {
	t.Helper()
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	pattern := make([]byte, 251)
	for index := range pattern {
		pattern[index] = byte(index*11 + 5)
	}
	secrets, err := auth.NewSecrets(bytes.Repeat([]byte{0x77}, 32), authtest.NewRandomReader(pattern...))
	if err != nil {
		t.Fatal(err)
	}
	hasher, err := auth.NewPasswordHasher(authtest.NewRandomReader(0x19), auth.Argon2Params{
		Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture := &account{
		pool: pool, clock: authtest.NewClock(time.Date(2026, time.August, 31, 9, 0, 0, 0, time.UTC)),
		secrets: secrets, hasher: hasher, mailbox: mailtest.NewCapture[appmail.Message](), logs: &bytes.Buffer{},
	}
	fixture.rebuild(t, fixture.mailbox)
	return fixture
}

func TestOneInstallationFromBootstrapToLockoutRecoveryAndRestart(t *testing.T) {
	fixture := newAccountFixture(t)
	ctx := context.Background()
	ownerPassword := fixtureSecret("acceptance-owner")

	// 1. The host issues one setup capability and the first owner claims it.
	setup, err := fixture.identity.IssueSetupCapability(ctx)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := fixture.identity.BootstrapOwner(ctx, identity.BootstrapRequest{
		Capability: setup.Token, Email: "owner@example.com", Password: ownerPassword,
		DisplayName: "Acceptance Owner", DeviceLabel: "Owner laptop", Origin: "192.0.2.0/24",
		EODHDAPIKey: fixtureSecret("acceptance-eodhd"),
		SMTP: identity.SMTPSetupConfiguration{
			Host: "smtp.example.test", Port: 587, From: "access@example.test",
			Username: fixtureSecret("acceptance-mail-account"),
			Password: fixtureSecret("acceptance-mail-secret"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Setup closes for good: the same capability cannot make a second owner.
	if _, err := fixture.identity.BootstrapOwner(ctx, identity.BootstrapRequest{
		Capability: setup.Token, Email: "impostor@example.com", Password: ownerPassword,
		DisplayName: "Impostor", DeviceLabel: "Other browser", Origin: "203.0.113.0/24",
		EODHDAPIKey: fixtureSecret("acceptance-eodhd"),
		SMTP:        identity.SMTPSetupConfiguration{Host: "smtp.example.test", Port: 587, From: "access@example.test"},
	}); err == nil {
		t.Fatal("a consumed setup capability created a second owner")
	}
	assertUserCount(t, fixture.pool, "owner", 1)

	// 2. The owner invites one person, who joins without ever choosing a password.
	ownerActor := identity.Actor{UserID: owner.User.ID, Role: identity.RoleOwner}
	fixture.clock.Advance(time.Minute)
	if _, err := fixture.identity.CreateInvitation(ctx, ownerActor, "member@example.com"); err != nil {
		t.Fatal(err)
	}
	capability := capabilityFromMail(t, fixture.mailbox.Messages(), "member@example.com")
	fixture.clock.Advance(time.Minute)
	member, err := fixture.identity.AcceptInvitation(ctx, identity.AcceptInvitationRequest{
		Capability: capability, Email: "member@example.com", DisplayName: "Acceptance Member",
		DeviceLabel: "Member phone", Origin: "198.51.100.0/24",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertCredentialCount(t, fixture.pool, member.User.ID, 0)

	// 3. Both sign in through their own path.
	fixture.clock.Advance(time.Minute)
	if _, err := fixture.auth.LoginOwner(ctx, auth.OwnerLoginRequest{
		Email: "owner@example.com", Password: ownerPassword, DeviceLabel: "Owner desktop", Origin: "192.0.2.0/24",
	}); err != nil {
		t.Fatalf("owner password login: %v", err)
	}
	fixture.clock.Advance(time.Minute)
	code := fixture.requestMemberCode(t, "198.51.100.0/24")
	fixture.clock.Advance(time.Minute)
	if _, err := fixture.auth.VerifyMemberCode(ctx, auth.MemberCodeVerifyRequest{
		Email: "member@example.com", Code: code, DeviceLabel: "Member laptop", Origin: "198.51.100.0/24",
	}); err != nil {
		t.Fatalf("member code login: %v", err)
	}

	// 4. Three wrong codes block temporarily; the block elapses on its own.
	fixture.clock.Advance(time.Minute)
	blockCode := fixture.requestMemberCode(t, "198.51.100.0/24")
	for attempt := 1; attempt <= auth.MemberBlockThreshold; attempt++ {
		fixture.clock.Advance(time.Second)
		if _, err := fixture.auth.VerifyMemberCode(ctx, auth.MemberCodeVerifyRequest{
			Email: "member@example.com", Code: wrongCode(blockCode), DeviceLabel: "Member laptop",
			Origin: "198.51.100.0/24",
		}); err == nil {
			t.Fatalf("wrong code attempt %d succeeded", attempt)
		}
	}
	blocked := fixture.memberState(t, member.User.ID)
	if !blocked.BlockedAt(fixture.clock.Now()) || blocked.Locked() {
		t.Fatalf("after %d wrong codes state = %#v, want a temporary block", auth.MemberBlockThreshold, blocked)
	}
	fixture.clock.Advance(auth.MemberBlockDuration + time.Minute)
	if fixture.memberState(t, member.User.ID).BlockedAt(fixture.clock.Now()) {
		t.Fatal("the temporary block did not elapse on its own")
	}

	// 5. Ten wrong codes in a rolling day escalate to an owner-only lock.
	// Each round is three more wrong codes after the previous block elapses. The lock arrives on
	// the tenth failure in the rolling day, so a handful of rounds is enough; the bound just
	// stops this from spinning if the escalation ever regresses.
	for round := 0; !fixture.memberState(t, member.User.ID).Locked(); round++ {
		if round > auth.MemberLockThreshold {
			t.Fatalf("the owner-only lock was never reached after %d rounds of wrong codes", round)
		}
		fixture.clock.Advance(auth.MemberBlockDuration + time.Minute)
		attemptCode := fixture.requestMemberCode(t, "198.51.100.0/24")
		for attempt := 0; attempt < auth.MemberBlockThreshold; attempt++ {
			fixture.clock.Advance(time.Second)
			if _, err := fixture.auth.VerifyMemberCode(ctx, auth.MemberCodeVerifyRequest{
				Email: "member@example.com", Code: wrongCode(attemptCode), DeviceLabel: "Member laptop",
				Origin: "198.51.100.0/24",
			}); err == nil {
				t.Fatal("a wrong code succeeded")
			}
			if fixture.memberState(t, member.User.ID).Locked() {
				break
			}
		}
	}
	locked := fixture.memberState(t, member.User.ID)
	if locked.LockedReason != auth.MemberLockedReason {
		t.Fatalf("lock reason = %q, want %q", locked.LockedReason, auth.MemberLockedReason)
	}
	// A locked member cannot get a new code, so waiting does not help.
	if _, err := fixture.auth.StartSignIn(ctx, auth.SignInStartRequest{
		Email: "member@example.com", Origin: "198.51.100.0/24",
	}); err != nil {
		t.Fatalf("sign-in start must stay uniform even for a locked member: %v", err)
	}
	if fixture.latestCodeIssuedAfter(t, member.User.ID, fixture.clock.Now()) {
		t.Fatal("a locked member was issued a new sign-in code")
	}

	// 6. Only the owner restores it, and the member signs in again immediately after.
	fixture.clock.Advance(time.Minute)
	memberActor := identity.Actor{UserID: member.User.ID, Role: identity.RoleMember}
	if err := fixture.identity.UnlockMember(ctx, memberActor, member.User.ID); !errors.Is(err, identity.ErrOwnerRequired) {
		t.Fatalf("a member unlocked themselves: %v", err)
	}
	if err := fixture.identity.UnlockMember(ctx, ownerActor, member.User.ID); err != nil {
		t.Fatalf("owner unlock: %v", err)
	}
	fixture.clock.Advance(time.Minute)
	recovered := fixture.requestMemberCode(t, "198.51.100.0/24")
	fixture.clock.Advance(time.Minute)
	restored, err := fixture.auth.VerifyMemberCode(ctx, auth.MemberCodeVerifyRequest{
		Email: "member@example.com", Code: recovered, DeviceLabel: "Member laptop", Origin: "198.51.100.0/24",
	})
	if err != nil {
		t.Fatalf("member sign-in after unlock: %v", err)
	}

	// 7. Sessions are the member's own to see and to end.
	sessions, err := fixture.auth.ListSessions(ctx, member.User.ID, restored.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) < 2 {
		t.Fatalf("member sessions = %d, want the invitation session and both sign-ins", len(sessions))
	}
	if err := fixture.auth.RevokeAllSessions(ctx, member.User.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.auth.AuthenticateSession(ctx, restored.SessionToken); !errors.Is(err, auth.ErrAuthenticationRequired) {
		t.Fatalf("a revoked session still authenticated: %v", err)
	}

	// 8. The email provider goes down. Existing work continues; nothing about the provider
	// reaches the caller, and the owner console keeps running.
	fixture.rebuild(t, mailtest.NewFailure[appmail.Message](errors.New("smtp.example.test: 421 service unavailable")))
	fixture.clock.Advance(time.Minute)
	outage, err := fixture.auth.StartSignIn(ctx, auth.SignInStartRequest{
		Email: "member@example.com", Origin: "198.51.100.0/24",
	})
	if err != nil {
		t.Fatalf("a provider outage must not fail the uniform sign-in response: %v", err)
	}
	if containsAny(outage.Message, "421", "smtp.example.test", "unavailable") {
		t.Fatalf("the sign-in response described the provider: %q", outage.Message)
	}
	if _, err := fixture.identity.ListMembers(ctx, ownerActor, "", 50); err != nil {
		t.Fatalf("owner administration stopped working during a mail outage: %v", err)
	}

	// 9. The process restarts. Everything durable survives and nothing has to be redone.
	fixture.rebuild(t, fixture.mailbox)
	fixture.clock.Advance(time.Minute)
	if _, err := fixture.auth.LoginOwner(ctx, auth.OwnerLoginRequest{
		Email: "owner@example.com", Password: ownerPassword, DeviceLabel: "Owner laptop", Origin: "192.0.2.0/24",
	}); err != nil {
		t.Fatalf("owner sign-in after restart: %v", err)
	}
	if required, err := fixture.identity.SetupRequired(ctx); err != nil || required {
		t.Fatalf("setup reopened after restart: required=%t err=%v", required, err)
	}

	// 10. Every step above is replayable, in order, by exactly the audience entitled to it.
	replay := clientevents.NewService(clientevents.NewRepository(fixture.pool))
	ownerFeed, err := replay.ListAuthorized(ctx, clientevents.Audience{UserID: owner.User.ID, Role: "owner"}, 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	memberFeed, err := replay.ListAuthorized(ctx, clientevents.Audience{UserID: member.User.ID, Role: "member"}, 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"member.changed.v1", "invitation.changed.v1"} {
		if !feedHas(ownerFeed, required) {
			t.Errorf("the owner feed is missing %s", required)
		}
		if feedHas(memberFeed, required) {
			t.Errorf("the member feed replayed owner-scoped %s", required)
		}
	}
	if !feedHas(memberFeed, "session.created.v1") {
		t.Error("the member feed is missing their own session events")
	}
	for _, feed := range [][]clientevents.Event{ownerFeed, memberFeed} {
		var previous int64
		for _, event := range feed {
			if event.ID <= previous {
				t.Fatalf("replay is not ordered: %d after %d", event.ID, previous)
			}
			previous = event.ID
		}
	}
	// Resuming from any delivered position replays the rest exactly once.
	if len(memberFeed) > 1 {
		rest, err := replay.ListAuthorized(ctx,
			clientevents.Audience{UserID: member.User.ID, Role: "member"}, memberFeed[0].ID, 500)
		if err != nil {
			t.Fatal(err)
		}
		if len(rest) != len(memberFeed)-1 {
			t.Fatalf("resumed replay returned %d events, want %d", len(rest), len(memberFeed)-1)
		}
	}
}

// requestMemberCode drives one sign-in start and returns the code that reached the mailbox.
func (a *account) requestMemberCode(t *testing.T, origin string) string {
	t.Helper()
	before := len(a.mailbox.Messages())
	if _, err := a.auth.StartSignIn(context.Background(), auth.SignInStartRequest{
		Email: "member@example.com", Origin: origin,
	}); err != nil {
		t.Fatal(err)
	}
	messages := a.mailbox.Messages()
	if len(messages) == before {
		t.Fatal("sign-in start delivered no code")
	}
	return codeFromMail(t, messages)
}

func (a *account) memberState(t *testing.T, userID string) auth.MemberLoginState {
	t.Helper()
	state, err := auth.NewRepository(a.pool).MemberLoginStateFor(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// latestCodeIssuedAfter reports whether any usable challenge was created after the given time.
func (a *account) latestCodeIssuedAfter(t *testing.T, userID string, at time.Time) bool {
	t.Helper()
	var issued bool
	if err := a.pool.QueryRow(context.Background(), `SELECT EXISTS(
		SELECT 1 FROM member_login_challenges WHERE user_id=$1 AND created_at>=$2)`, userID, at).Scan(&issued); err != nil {
		t.Fatal(err)
	}
	return issued
}

func wrongCode(actual string) string {
	if actual == "000000" {
		return "111111"
	}
	return "000000"
}

func containsAny(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if needle != "" && bytes.Contains([]byte(haystack), []byte(needle)) {
			return true
		}
	}
	return false
}

func feedHas(feed []clientevents.Event, eventType string) bool {
	for _, event := range feed {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

func assertUserCount(t *testing.T, pool *pgxpool.Pool, role string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE role=$1`, role).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", role, got, want)
	}
}

func assertCredentialCount(t *testing.T, pool *pgxpool.Pool, userID string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM owner_credentials WHERE user_id=$1`, userID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("credentials for %s = %d, want %d", userID, got, want)
	}
}
