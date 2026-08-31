package auth_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/identity"

	"github.com/jackc/pgx/v5/pgxpool"
)

// concurrentWorkers is deliberately larger than any pool size, so the invariants below are
// enforced by the database rather than by there being too few callers to collide.
const concurrentWorkers = 100

// runConcurrently starts count goroutines that all begin at once and collects their errors.
func runConcurrently(count int, work func(index int) error) []error {
	start := make(chan struct{})
	results := make([]error, count)
	var group sync.WaitGroup
	group.Add(count)
	for index := 0; index < count; index++ {
		go func(index int) {
			defer group.Done()
			<-start
			results[index] = work(index)
		}(index)
	}
	close(start)
	group.Wait()
	return results
}

func countSucceeded(results []error) int {
	succeeded := 0
	for _, err := range results {
		if err == nil {
			succeeded++
		}
	}
	return succeeded
}

func TestOneHundredSimultaneousSetupClaimsProduceExactlyOneOwner(t *testing.T) {
	fixture := newAccountFixture(t)
	ctx := context.Background()
	setup, err := fixture.identity.IssueSetupCapability(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Every claim uses the same capability, which is exactly the race a public setup link
	// invites. One must win; the rest must leave nothing behind at all.
	results := runConcurrently(concurrentWorkers, func(index int) error {
		_, err := fixture.identity.BootstrapOwner(ctx, identity.BootstrapRequest{
			Capability: setup.Token, Email: "owner@example.com", Password: fixtureSecret("race-owner"),
			DisplayName: "Race Owner", DeviceLabel: "Race browser", Origin: "192.0.2.0/24",
			EODHDAPIKey: fixtureSecret("race-eodhd"),
			SMTP: identity.SMTPSetupConfiguration{
				Host: "smtp.example.test", Port: 587, From: "access@example.test",
			},
		})
		return err
	})
	if succeeded := countSucceeded(results); succeeded != 1 {
		t.Fatalf("%d of %d simultaneous setup claims succeeded, want exactly one",
			succeeded, concurrentWorkers)
	}
	assertUserCount(t, fixture.pool, "owner", 1)
	assertRowCount(t, fixture.pool, "sessions", 1)
	// A losing claim must not leave a half-written credential or an extra encrypted secret.
	assertRowCount(t, fixture.pool, "owner_credentials", 1)
	assertRowCount(t, fixture.pool, "external_service_credentials", 2)
	if required, err := fixture.identity.SetupRequired(ctx); err != nil || required {
		t.Fatalf("setup stayed open after the race: required=%t err=%v", required, err)
	}
}

func TestOneHundredSimultaneousInvitationAcceptancesProduceExactlyOneMember(t *testing.T) {
	fixture := newAccountFixture(t)
	ctx := context.Background()
	owner := fixture.bootstrapOwner(t)
	fixture.clock.Advance(time.Minute)
	if _, err := fixture.identity.CreateInvitation(ctx,
		identity.Actor{UserID: owner.User.ID, Role: identity.RoleOwner}, "member@example.com"); err != nil {
		t.Fatal(err)
	}
	capability := capabilityFromMail(t, fixture.mailbox.Messages(), "member@example.com")

	results := runConcurrently(concurrentWorkers, func(index int) error {
		_, err := fixture.identity.AcceptInvitation(ctx, identity.AcceptInvitationRequest{
			Capability: capability, Email: "member@example.com", DisplayName: "Race Member",
			DeviceLabel: "Race device", Origin: "198.51.100.0/24",
		})
		return err
	})
	if succeeded := countSucceeded(results); succeeded != 1 {
		t.Fatalf("%d of %d simultaneous acceptances succeeded, want exactly one",
			succeeded, concurrentWorkers)
	}
	assertUserCount(t, fixture.pool, "member", 1)
	// One member, one session for them, and no password anywhere in the passwordless path.
	assertRowCount(t, fixture.pool, "owner_credentials", 1)
	var pending int
	if err := fixture.pool.QueryRow(ctx,
		`SELECT count(*) FROM invitations WHERE state='pending'`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatalf("%d invitations stayed pending after acceptance", pending)
	}
}

func TestSimultaneousCodeIssuanceAndVerificationCannotBeReplayed(t *testing.T) {
	fixture := newAccountFixture(t)
	ctx := context.Background()
	member := fixture.provisionMemberAccount(t)

	// Many devices ask for a code at the same instant. Each issuance supersedes the last, so
	// only the final code may ever verify.
	fixture.clock.Advance(time.Minute)
	runConcurrently(20, func(index int) error {
		_, err := fixture.auth.StartSignIn(ctx, auth.SignInStartRequest{
			Email: "member@example.com", Origin: "198.51.100.0/24",
		})
		return err
	})
	var active int
	if err := fixture.pool.QueryRow(ctx,
		`SELECT count(*) FROM member_login_challenges WHERE user_id=$1 AND state='active'`,
		member).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active > 1 {
		t.Fatalf("%d member codes are usable at once, want at most one", active)
	}

	// The same code submitted from many devices at once may create exactly one session.
	code := codeFromMail(t, fixture.mailbox.Messages())
	fixture.clock.Advance(time.Second)
	before := sessionCount(t, fixture, member)
	results := runConcurrently(concurrentWorkers, func(index int) error {
		_, err := fixture.auth.VerifyMemberCode(ctx, auth.MemberCodeVerifyRequest{
			Email: "member@example.com", Code: code, DeviceLabel: "Race device", Origin: "198.51.100.0/24",
		})
		return err
	})
	if succeeded := countSucceeded(results); succeeded != 1 {
		t.Fatalf("%d of %d simultaneous verifications succeeded, want exactly one",
			succeeded, concurrentWorkers)
	}
	if created := sessionCount(t, fixture, member) - before; created != 1 {
		t.Fatalf("%d sessions were created by one code, want exactly one", created)
	}
}

func TestRateBucketBoundariesAreExactAndRefusalsConsumeNoBudget(t *testing.T) {
	fixture := newAccountFixture(t)
	ctx := context.Background()
	repository := auth.NewRepository(fixture.pool)
	digest := fixture.secrets.Digest(auth.PurposeOrigin, "203.0.113.0/24")
	limits := []auth.RateLimit{{Limit: 5, Window: time.Minute}}
	now := fixture.clock.Now()

	// The limit is a hard boundary, not an approximation, even under simultaneous callers.
	results := runConcurrently(concurrentWorkers, func(index int) error {
		decision, err := repository.AllowRate(ctx, auth.RateOriginCodeRequest, digest, now, limits)
		if err != nil {
			return err
		}
		if !decision.Allowed {
			return errors.New("refused")
		}
		return nil
	})
	if allowed := countSucceeded(results); allowed != 5 {
		t.Fatalf("%d of %d simultaneous attempts were allowed, want exactly the limit of 5",
			allowed, concurrentWorkers)
	}

	// A refused attempt must not consume budget or push the window forward, or a client that
	// keeps retrying would lock itself out for longer than the window it was told about.
	atBoundary := now.Add(time.Minute)
	if decision, err := repository.AllowRate(ctx, auth.RateOriginCodeRequest, digest, atBoundary, limits); err != nil || !decision.Allowed {
		t.Fatalf("the window did not reopen exactly one window later: %#v err=%v", decision, err)
	}
	// Independent buckets do not share budget.
	if decision, err := repository.AllowRate(ctx, auth.RateOriginCodeVerify, digest, now, limits); err != nil || !decision.Allowed {
		t.Fatalf("a separate bucket was throttled by another one's traffic: %#v err=%v", decision, err)
	}
}

func TestSimultaneousRevocationAndAuthenticationNeverLeavesAUsableSession(t *testing.T) {
	fixture := newAccountFixture(t)
	ctx := context.Background()
	member := fixture.provisionMemberAccount(t)
	fixture.clock.Advance(time.Minute)
	if _, err := fixture.auth.StartSignIn(ctx, auth.SignInStartRequest{
		Email: "member@example.com", Origin: "198.51.100.0/24",
	}); err != nil {
		t.Fatal(err)
	}
	fixture.clock.Advance(time.Second)
	signedIn, err := fixture.auth.VerifyMemberCode(ctx, auth.MemberCodeVerifyRequest{
		Email: "member@example.com", Code: codeFromMail(t, fixture.mailbox.Messages()),
		DeviceLabel: "Member laptop", Origin: "198.51.100.0/24",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Authentication and revocation race. Whatever order they land in, the session must be
	// unusable once revocation has returned.
	runConcurrently(concurrentWorkers, func(index int) error {
		if index%2 == 0 {
			_, err := fixture.auth.AuthenticateSession(ctx, signedIn.SessionToken)
			return err
		}
		return fixture.auth.RevokeAllSessions(ctx, member)
	})
	if _, err := fixture.auth.AuthenticateSession(ctx, signedIn.SessionToken); !errors.Is(err, auth.ErrAuthenticationRequired) {
		t.Fatalf("a revoked session still authenticated: %v", err)
	}
	if err := fixture.auth.RevalidateSession(ctx, signedIn.Session.ID); !errors.Is(err, auth.ErrAuthenticationRequired) {
		t.Fatalf("an open stream kept a revoked session: %v", err)
	}
}

func TestSimultaneousWrongCodesEscalateOnceRatherThanPerAttempt(t *testing.T) {
	fixture := newAccountFixture(t)
	ctx := context.Background()
	member := fixture.provisionMemberAccount(t)
	fixture.clock.Advance(time.Minute)
	if _, err := fixture.auth.StartSignIn(ctx, auth.SignInStartRequest{
		Email: "member@example.com", Origin: "198.51.100.0/24",
	}); err != nil {
		t.Fatal(err)
	}
	code := codeFromMail(t, fixture.mailbox.Messages())
	fixture.clock.Advance(time.Second)

	// Simultaneous wrong codes must leave one coherent throttling state, not a torn one where
	// a member is both blocked and holding a stale failure count.
	results := runConcurrently(concurrentWorkers, func(index int) error {
		_, err := fixture.auth.VerifyMemberCode(ctx, auth.MemberCodeVerifyRequest{
			Email: "member@example.com", Code: wrongCode(code), DeviceLabel: "Race device",
			Origin: "198.51.100.0/24",
		})
		return err
	})
	if succeeded := countSucceeded(results); succeeded != 0 {
		t.Fatalf("%d wrong codes were accepted", succeeded)
	}
	state := fixture.memberState(t, member)
	if !state.BlockedAt(fixture.clock.Now()) && !state.Locked() {
		t.Fatalf("state after %d simultaneous wrong codes = %#v, want a block or a lock",
			concurrentWorkers, state)
	}
	if state.ConsecutiveFailures >= auth.MemberBlockThreshold {
		t.Fatalf("consecutive failures = %d, want the counter reset by the escalation it caused",
			state.ConsecutiveFailures)
	}
	// The correct code cannot be used to slip past an active block.
	if _, err := fixture.auth.VerifyMemberCode(ctx, auth.MemberCodeVerifyRequest{
		Email: "member@example.com", Code: code, DeviceLabel: "Member laptop", Origin: "198.51.100.0/24",
	}); err == nil {
		t.Fatal("a blocked member signed in with the correct code")
	}
}

// bootstrapOwner claims setup for the tests that need an owner but are not testing bootstrap.
func (a *account) bootstrapOwner(t *testing.T) identity.BootstrapResult {
	t.Helper()
	setup, err := a.identity.IssueSetupCapability(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	owner, err := a.identity.BootstrapOwner(context.Background(), identity.BootstrapRequest{
		Capability: setup.Token, Email: "owner@example.com", Password: fixtureSecret("concurrency-owner"),
		DisplayName: "Concurrency Owner", DeviceLabel: "Owner laptop", Origin: "192.0.2.0/24",
		EODHDAPIKey: fixtureSecret("concurrency-eodhd"),
		SMTP:        identity.SMTPSetupConfiguration{Host: "smtp.example.test", Port: 587, From: "access@example.test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

// provisionMemberAccount returns an active member reached the only way one can exist.
func (a *account) provisionMemberAccount(t *testing.T) string {
	t.Helper()
	owner := a.bootstrapOwner(t)
	a.clock.Advance(time.Minute)
	if _, err := a.identity.CreateInvitation(context.Background(),
		identity.Actor{UserID: owner.User.ID, Role: identity.RoleOwner}, "member@example.com"); err != nil {
		t.Fatal(err)
	}
	a.clock.Advance(time.Minute)
	member, err := a.identity.AcceptInvitation(context.Background(), identity.AcceptInvitationRequest{
		Capability: capabilityFromMail(t, a.mailbox.Messages(), "member@example.com"),
		Email:      "member@example.com", DisplayName: "Concurrency Member",
		DeviceLabel: "Member phone", Origin: "198.51.100.0/24",
	})
	if err != nil {
		t.Fatal(err)
	}
	return member.User.ID
}

func sessionCount(t *testing.T, fixture *account, userID string) int {
	t.Helper()
	var count int
	if err := fixture.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM sessions WHERE user_id=$1`, userID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func assertRowCount(t *testing.T, pool *pgxpool.Pool, table string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM `+quoteIdentifier(table)).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s rows = %d, want %d", table, got, want)
	}
}
