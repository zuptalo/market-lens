package auth_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/authtest"
	"market-lens/server/internal/db"
	appmail "market-lens/server/internal/mail"
	"market-lens/server/internal/mail/mailtest"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newMemberAuthService builds an auth service with a captured mail transport and a log sink,
// so both the emailed code and every log line can be inspected for disclosure.
func newMemberAuthService(t *testing.T, pool *pgxpool.Pool, clock *authtest.Clock, secrets *auth.Secrets,
	sender appmail.Sender) (*auth.Service, *bytes.Buffer) {
	t.Helper()
	hasher, err := auth.NewPasswordHasher(authtest.NewRandomReader(0x91), auth.Argon2Params{
		Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	service, err := auth.NewService(auth.ServiceDependencies{
		Repository: auth.NewRepository(pool), Passwords: hasher, Secrets: secrets, Now: clock.Now,
		OwnerIdleTimeout: 8 * time.Hour, SessionAbsoluteTimeout: 30 * 24 * time.Hour,
		MemberCodes: auth.NewMemberCodeGenerator(authtest.NewRandomReader(0x00)),
		Mail:        sender,
		Logger:      slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, logs
}

// deterministicCode is the value the zero-pattern random reader always yields.
const deterministicCode = "000000"

func TestSignInStartIsIndistinguishableAcrossMemberOwnerAndUnknownEmails(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	clock, secrets, _ := provisionOwner(t, pool)
	start := clock.Now().Add(time.Hour)
	clock.Set(start)
	provisionMember(t, pool, "10000000-0000-4000-8000-000000000401", "member@example.com", "Member One", start)
	captured := mailtest.NewCapture[appmail.Message]()
	service, logs := newMemberAuthService(t, pool, clock, secrets, captured)

	// Every shape of address produces one identical response body.
	for index, email := range []string{
		"member@example.com", "owner@example.com", "unknown@example.com", "not-an-email", "",
	} {
		clock.Set(start.Add(time.Duration(index) * 2 * time.Minute))
		result, err := service.StartSignIn(context.Background(), auth.SignInStartRequest{
			Email: email, Origin: "203.0.113." + string(rune('1'+index)),
		})
		if err != nil {
			t.Fatalf("email %q returned a distinguishable error: %v", email, err)
		}
		if result.Message != auth.GenericSignInMessage {
			t.Fatalf("email %q message = %q, want the generic message", email, result.Message)
		}
	}

	// Only the eligible member is actually emailed a code.
	messages := captured.Messages()
	if len(messages) != 1 || messages[0].To != "member@example.com" {
		t.Fatalf("captured messages = %#v, want exactly one to the member", messages)
	}
	if !strings.Contains(messages[0].Text, deterministicCode) {
		t.Fatalf("member message omitted the code: %q", messages[0].Text)
	}
	var challenges, ownerChallenges int
	if err := pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM member_login_challenges WHERE state='active'),
		(SELECT count(*) FROM member_login_challenges c JOIN users u ON u.id=c.user_id WHERE u.role='owner')`).
		Scan(&challenges, &ownerChallenges); err != nil {
		t.Fatal(err)
	}
	if challenges != 1 || ownerChallenges != 0 {
		t.Fatalf("active challenges = %d, owner challenges = %d", challenges, ownerChallenges)
	}
	// The plaintext code must never reach the logs or persistence.
	mailtest.AssertSafeText(t, deterministicCode, logs.String())
	var digestMatches int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM member_login_challenges WHERE code_digest=$1`,
		[]byte(deterministicCode)).Scan(&digestMatches); err != nil {
		t.Fatal(err)
	}
	if digestMatches != 0 {
		t.Fatal("a plaintext member code was stored")
	}
}

func TestRepeatedSignInStartIsThrottledWithoutRevealingTheAccount(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	clock, secrets, _ := provisionOwner(t, pool)
	start := clock.Now().Add(time.Hour)
	clock.Set(start)
	provisionMember(t, pool, "10000000-0000-4000-8000-000000000402", "member@example.com", "Member One", start)
	captured := mailtest.NewCapture[appmail.Message]()
	service, _ := newMemberAuthService(t, pool, clock, secrets, captured)

	// A second request within the same minute is silently not delivered, and the response is
	// identical, so an attacker cannot use throttling to confirm that an address exists.
	first, err := service.StartSignIn(context.Background(), auth.SignInStartRequest{Email: "member@example.com", Origin: "203.0.113.20"})
	if err != nil {
		t.Fatal(err)
	}
	clock.Set(start.Add(20 * time.Second))
	second, err := service.StartSignIn(context.Background(), auth.SignInStartRequest{Email: "member@example.com", Origin: "203.0.113.20"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Message != second.Message {
		t.Fatalf("throttled response %q differs from first %q", second.Message, first.Message)
	}
	if len(captured.Messages()) != 1 {
		t.Fatalf("deliveries = %d, want the per-account minute ceiling to hold", len(captured.Messages()))
	}

	// Exhausting the per-origin bucket is safe to report, because it identifies no account.
	var limited *auth.RateLimitedError
	for attempt := range auth.OriginCodeRequestLimits[0].Limit + 2 {
		clock.Set(start.Add(time.Duration(attempt+2) * time.Second))
		_, err = service.StartSignIn(context.Background(), auth.SignInStartRequest{
			Email: "unknown@example.com", Origin: "203.0.113.99",
		})
		if err != nil {
			break
		}
	}
	if !errors.As(err, &limited) || !errors.Is(err, auth.ErrRateLimited) {
		t.Fatalf("origin exhaustion error = %v, want a rate limited error", err)
	}
	if limited.RetryAfter%auth.RateRetryGranularity != 0 || limited.RetryAfter <= 0 {
		t.Fatalf("retry hint = %v, want a positive coarse multiple", limited.RetryAfter)
	}
}

func TestMemberCodeDeliveryFailureRetiresTheCodeAndStaysGeneric(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	clock, secrets, _ := provisionOwner(t, pool)
	start := clock.Now().Add(time.Hour)
	clock.Set(start)
	member := provisionMember(t, pool, "10000000-0000-4000-8000-000000000403", "member@example.com", "Member One", start)
	failing := mailtest.NewFailure[appmail.Message](&appmail.DeliveryError{Code: "temporary_failure", Retryable: true})
	service, logs := newMemberAuthService(t, pool, clock, secrets, failing)

	result, err := service.StartSignIn(context.Background(), auth.SignInStartRequest{Email: "member@example.com", Origin: "203.0.113.30"})
	if err != nil {
		t.Fatalf("a provider outage surfaced to the client: %v", err)
	}
	if result.Message != auth.GenericSignInMessage {
		t.Fatalf("outage message = %q, want the generic message", result.Message)
	}
	var activeChallenges, failedDeliveries int
	if err := pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM member_login_challenges WHERE user_id=$1 AND state='active'),
		(SELECT count(*) FROM account_email_deliveries WHERE subject_user_id=$1 AND state='failed')`,
		member).Scan(&activeChallenges, &failedDeliveries); err != nil {
		t.Fatal(err)
	}
	if activeChallenges != 0 || failedDeliveries != 1 {
		t.Fatalf("after outage active=%d failed=%d, want the undelivered code retired", activeChallenges, failedDeliveries)
	}
	// An undelivered code must not authenticate.
	if _, err := service.VerifyMemberCode(context.Background(), auth.MemberCodeVerifyRequest{
		Email: "member@example.com", Code: deterministicCode, DeviceLabel: "Member device", Origin: "203.0.113.30",
	}); !errors.Is(err, auth.ErrAuthenticationFailed) {
		t.Fatalf("undelivered code verification error = %v, want generic failure", err)
	}
	mailtest.AssertSafeText(t, deterministicCode, logs.String())
}

func TestMemberCodeVerificationEstablishesASessionAndFailsUniformly(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	clock, secrets, _ := provisionOwner(t, pool)
	start := clock.Now().Add(time.Hour)
	clock.Set(start)
	member := provisionMember(t, pool, "10000000-0000-4000-8000-000000000404", "member@example.com", "Member One", start)
	captured := mailtest.NewCapture[appmail.Message]()
	service, logs := newMemberAuthService(t, pool, clock, secrets, captured)

	if _, err := service.StartSignIn(context.Background(), auth.SignInStartRequest{Email: "member@example.com", Origin: "203.0.113.40"}); err != nil {
		t.Fatal(err)
	}

	// Wrong code, unknown account, and malformed code are externally identical failures.
	clock.Set(start.Add(time.Minute))
	for _, attempt := range []auth.MemberCodeVerifyRequest{
		{Email: "member@example.com", Code: "999999", DeviceLabel: "Device", Origin: "203.0.113.40"},
		{Email: "unknown@example.com", Code: deterministicCode, DeviceLabel: "Device", Origin: "203.0.113.40"},
		{Email: "member@example.com", Code: "12345", DeviceLabel: "Device", Origin: "203.0.113.40"},
		{Email: "owner@example.com", Code: deterministicCode, DeviceLabel: "Device", Origin: "203.0.113.40"},
	} {
		if _, err := service.VerifyMemberCode(context.Background(), attempt); !errors.Is(err, auth.ErrAuthenticationFailed) {
			t.Fatalf("attempt %#v error = %v, want a uniform generic failure", attempt, err)
		}
	}

	// The correct code still works and returns a usable session.
	clock.Set(start.Add(2 * time.Minute))
	result, err := service.VerifyMemberCode(context.Background(), auth.MemberCodeVerifyRequest{
		Email: "member@example.com", Code: deterministicCode, DeviceLabel: "Member phone", Origin: "203.0.113.40",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Account.ID != member || result.Account.Role != "member" {
		t.Fatalf("verified account = %#v", result.Account)
	}
	if result.SessionToken == "" || result.CSRFToken == "" || result.SessionToken == result.CSRFToken {
		t.Fatal("member verification did not return distinct session and CSRF tokens")
	}
	principal, err := service.AuthenticateSession(context.Background(), result.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if principal.UserID != member || principal.Role != "member" {
		t.Fatalf("member principal = %#v", principal)
	}

	// Replaying the same code cannot mint a second session.
	if _, err := service.VerifyMemberCode(context.Background(), auth.MemberCodeVerifyRequest{
		Email: "member@example.com", Code: deterministicCode, DeviceLabel: "Member phone", Origin: "203.0.113.40",
	}); !errors.Is(err, auth.ErrAuthenticationFailed) {
		t.Fatal("a consumed member code was replayed successfully")
	}
	mailtest.AssertSafeText(t, deterministicCode, logs.String())
	mailtest.AssertSafeText(t, result.SessionToken, logs.String())
}

func TestBlockedAndLockedMembersFailVerificationWithoutDisclosure(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	clock, secrets, _ := provisionOwner(t, pool)
	start := clock.Now().Add(time.Hour)
	clock.Set(start)
	provisionMember(t, pool, "10000000-0000-4000-8000-000000000405", "member@example.com", "Member One", start)
	captured := mailtest.NewCapture[appmail.Message]()
	service, _ := newMemberAuthService(t, pool, clock, secrets, captured)
	if _, err := service.StartSignIn(context.Background(), auth.SignInStartRequest{Email: "member@example.com", Origin: "203.0.113.50"}); err != nil {
		t.Fatal(err)
	}

	// Drive the member into a temporary block through the public API only.
	for attempt := range auth.MemberBlockThreshold {
		clock.Set(start.Add(time.Duration(attempt+1) * time.Second))
		if _, err := service.VerifyMemberCode(context.Background(), auth.MemberCodeVerifyRequest{
			Email: "member@example.com", Code: "999999", DeviceLabel: "Device", Origin: "203.0.113.50",
		}); !errors.Is(err, auth.ErrAuthenticationFailed) {
			t.Fatalf("wrong-code attempt %d error = %v, want generic failure", attempt+1, err)
		}
	}

	// While blocked, even the correct code fails with the identical generic error.
	clock.Set(start.Add(time.Minute))
	if _, err := service.VerifyMemberCode(context.Background(), auth.MemberCodeVerifyRequest{
		Email: "member@example.com", Code: deterministicCode, DeviceLabel: "Device", Origin: "203.0.113.50",
	}); !errors.Is(err, auth.ErrAuthenticationFailed) {
		t.Fatalf("blocked verification error = %v, want the same generic failure", err)
	}

	// Requesting a new code while blocked also stays generic and delivers nothing new.
	before := len(captured.Messages())
	clock.Set(start.Add(2 * time.Minute))
	result, err := service.StartSignIn(context.Background(), auth.SignInStartRequest{Email: "member@example.com", Origin: "203.0.113.51"})
	if err != nil {
		t.Fatalf("blocked code request error = %v, want the generic message", err)
	}
	if result.Message != auth.GenericSignInMessage {
		t.Fatalf("blocked code request message = %q", result.Message)
	}
	if len(captured.Messages()) != before {
		t.Fatal("a blocked member was emailed a new code")
	}
}
