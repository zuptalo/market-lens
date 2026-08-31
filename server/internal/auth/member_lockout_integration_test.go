package auth_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/db"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

// memberVerifyRecord collects one concurrent verification result.
type memberVerifyRecord struct {
	Outcome auth.MemberLoginOutcome
	Err     error
}

// provisionMember inserts an active, verified member so US2 tests do not depend on US3.
func provisionMember(t *testing.T, pool *pgxpool.Pool, id, email, displayName string, createdAt time.Time) string {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `INSERT INTO users
		(id,email,normalized_email,display_name,role,status,email_verified_at,created_at,updated_at)
		VALUES ($1,$2,lower($2),$3,'member','active',$4,$4,$4)`, id, email, displayName, createdAt); err != nil {
		t.Fatal(err)
	}
	return id
}

func memberSession(t *testing.T, secrets *auth.Secrets, sessionID, userID string, now time.Time) auth.Session {
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

// issueMemberCode issues a fresh newest-only challenge for code and returns its digest.
func issueMemberCode(t *testing.T, repository *auth.Repository, secrets *auth.Secrets, userID, code string, now time.Time) []byte {
	t.Helper()
	challengeID, deliveryID := newTestUUID(t), newTestUUID(t)
	digest := secrets.Digest(auth.PurposeMemberCode, code)
	if err := repository.IssueMemberChallenge(context.Background(), auth.IssueMemberChallengeParams{
		ChallengeID: challengeID, DeliveryID: deliveryID, UserID: userID, Email: "member@example.com",
		CodeDigest: digest, CreatedAt: now, ExpiresAt: now.Add(auth.MemberCodeTTL),
		OriginDigest: secrets.Digest(auth.PurposeOrigin, "203.0.113.0/24"),
	}); err != nil {
		t.Fatal(err)
	}
	return digest
}

var testUUIDCounter struct {
	sync.Mutex
	value int
}

func newTestUUID(t *testing.T) string {
	t.Helper()
	testUUIDCounter.Lock()
	defer testUUIDCounter.Unlock()
	testUUIDCounter.value++
	return fmt.Sprintf("50000000-0000-4000-8000-%012d", testUUIDCounter.value)
}

func TestThreeConsecutiveWrongMemberCodesBlockForFifteenDurableMinutes(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	_, secrets, _ := provisionOwner(t, pool)
	start := time.Date(2026, time.August, 30, 10, 0, 0, 0, time.UTC)
	member := provisionMember(t, pool, "10000000-0000-4000-8000-000000000201", "member@example.com", "Member One", start)
	repository := auth.NewRepository(pool)
	issueMemberCode(t, repository, secrets, member, "111111", start)

	wrong := secrets.Digest(auth.PurposeMemberCode, "999999")
	wantOutcomes := []auth.MemberLoginOutcome{auth.MemberLoginFailed, auth.MemberLoginFailed, auth.MemberLoginBlocked}
	var blockedAt time.Time
	for attempt, want := range wantOutcomes {
		now := start.Add(time.Duration(attempt+1) * time.Second)
		result, err := repository.VerifyMemberChallenge(context.Background(), auth.VerifyMemberChallengeParams{
			UserID: member, CodeDigest: wrong, Now: now,
			Session:      memberSession(t, secrets, newTestUUID(t), member, now),
			OriginDigest: secrets.Digest(auth.PurposeOrigin, "203.0.113.0/24"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if result.Outcome != want {
			t.Fatalf("attempt %d outcome = %q, want %q", attempt+1, result.Outcome, want)
		}
		blockedAt = now
	}

	// The block is durable and survives a fresh repository over the same database.
	state, err := auth.NewRepository(pool).MemberLoginStateFor(context.Background(), member)
	if err != nil {
		t.Fatal(err)
	}
	if state.BlockedUntil == nil || !state.BlockedUntil.Equal(blockedAt.Add(auth.MemberBlockDuration)) {
		t.Fatalf("blocked_until = %v, want %v", state.BlockedUntil, blockedAt.Add(auth.MemberBlockDuration))
	}
	if !state.BlockedAt(blockedAt.Add(auth.MemberBlockDuration-time.Nanosecond)) ||
		state.BlockedAt(blockedAt.Add(auth.MemberBlockDuration)) {
		t.Fatalf("block window = %#v", state)
	}

	// A correct code submitted inside the block is still refused, and the block does not extend.
	correct := secrets.Digest(auth.PurposeMemberCode, "111111")
	result, err := repository.VerifyMemberChallenge(context.Background(), auth.VerifyMemberChallengeParams{
		UserID: member, CodeDigest: correct, Now: blockedAt.Add(time.Minute),
		Session:      memberSession(t, secrets, newTestUUID(t), member, blockedAt.Add(time.Minute)),
		OriginDigest: secrets.Digest(auth.PurposeOrigin, "203.0.113.0/24"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != auth.MemberLoginBlocked {
		t.Fatalf("blocked-period outcome = %q, want blocked", result.Outcome)
	}
	var sessions, failures int
	if err := pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM sessions s JOIN users u ON u.id=s.user_id WHERE u.role='member'),
		(SELECT count(*) FROM login_failure_events)`).Scan(&sessions, &failures); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Fatalf("blocked member obtained %d sessions", sessions)
	}
	if failures != auth.MemberBlockThreshold {
		t.Fatalf("recorded failures = %d, want %d (blocked submissions must not count)", failures, auth.MemberBlockThreshold)
	}

	// Blocking revokes the outstanding challenge so a fresh code is required after expiry.
	var activeChallenges int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM member_login_challenges WHERE user_id=$1 AND state='active'`, member).Scan(&activeChallenges); err != nil {
		t.Fatal(err)
	}
	if activeChallenges != 0 {
		t.Fatalf("active challenges after block = %d, want 0", activeChallenges)
	}
}

func TestTenWrongMemberCodesInRollingDayLockAdministrativelyUntilOwnerUnlock(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	_, secrets, bootstrap := provisionOwner(t, pool)
	start := time.Date(2026, time.August, 30, 10, 0, 0, 0, time.UTC)
	member := provisionMember(t, pool, "10000000-0000-4000-8000-000000000202", "member@example.com", "Member One", start)
	repository := auth.NewRepository(pool)
	wrong := secrets.Digest(auth.PurposeMemberCode, "999999")
	origin := secrets.Digest(auth.PurposeOrigin, "203.0.113.0/24")

	now := start
	var outcomes []auth.MemberLoginOutcome
	for failure := 1; failure <= auth.MemberLockThreshold; failure++ {
		if (failure-1)%auth.MemberBlockThreshold == 0 {
			// Each block retires the code, so a distinct fresh one is issued after the block elapses.
			issueMemberCode(t, repository, secrets, member, fmt.Sprintf("11111%d", failure/auth.MemberBlockThreshold), now)
		}
		result, err := repository.VerifyMemberChallenge(context.Background(), auth.VerifyMemberChallengeParams{
			UserID: member, CodeDigest: wrong, Now: now,
			Session: memberSession(t, secrets, newTestUUID(t), member, now), OriginDigest: origin,
		})
		if err != nil {
			t.Fatal(err)
		}
		outcomes = append(outcomes, result.Outcome)
		if result.Outcome == auth.MemberLoginBlocked {
			now = now.Add(auth.MemberBlockDuration + time.Minute)
			continue
		}
		now = now.Add(time.Second)
	}

	if outcomes[len(outcomes)-1] != auth.MemberLoginLocked {
		t.Fatalf("outcomes = %v, want the tenth wrong code to lock", outcomes)
	}
	state, err := repository.MemberLoginStateFor(context.Background(), member)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Locked() || state.LockedReason != auth.MemberLockedReason {
		t.Fatalf("locked state = %#v", state)
	}

	// A correct code cannot clear an administrative lock, even after the block window elapses.
	correct := secrets.Digest(auth.PurposeMemberCode, "111113")
	locked := now.Add(auth.MemberBlockDuration + time.Hour)
	result, err := repository.VerifyMemberChallenge(context.Background(), auth.VerifyMemberChallengeParams{
		UserID: member, CodeDigest: correct, Now: locked,
		Session: memberSession(t, secrets, newTestUUID(t), member, locked), OriginDigest: origin,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != auth.MemberLoginLocked {
		t.Fatalf("locked-period outcome = %q, want locked", result.Outcome)
	}

	// Owner unlock clears the lock and the failure history without granting a session.
	if err := repository.UnlockMember(context.Background(), bootstrap.User.ID, member, locked.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	state, err = repository.MemberLoginStateFor(context.Background(), member)
	if err != nil {
		t.Fatal(err)
	}
	if state.Locked() || state.BlockedAt(locked.Add(time.Minute)) || state.ConsecutiveFailures != 0 {
		t.Fatalf("unlocked state = %#v", state)
	}
	var remainingFailures, activeChallenges, memberSessions, audits int
	if err := pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM login_failure_events WHERE user_id=$1),
		(SELECT count(*) FROM member_login_challenges WHERE user_id=$1 AND state='active'),
		(SELECT count(*) FROM sessions WHERE user_id=$1 AND revoked_at IS NULL),
		(SELECT count(*) FROM security_audit_events WHERE event_type='member.unlocked.v1' AND outcome='succeeded')`,
		member).Scan(&remainingFailures, &activeChallenges, &memberSessions, &audits); err != nil {
		t.Fatal(err)
	}
	if remainingFailures != 0 || activeChallenges != 0 || memberSessions != 0 || audits != 1 {
		t.Fatalf("after unlock failures=%d challenges=%d sessions=%d audits=%d",
			remainingFailures, activeChallenges, memberSessions, audits)
	}

	// A fresh code is required, and it now succeeds.
	unlocked := locked.Add(2 * time.Minute)
	issueMemberCode(t, repository, secrets, member, "222222", unlocked)
	success, err := repository.VerifyMemberChallenge(context.Background(), auth.VerifyMemberChallengeParams{
		UserID: member, CodeDigest: secrets.Digest(auth.PurposeMemberCode, "222222"), Now: unlocked,
		Session: memberSession(t, secrets, newTestUUID(t), member, unlocked), OriginDigest: origin,
	})
	if err != nil {
		t.Fatal(err)
	}
	if success.Outcome != auth.MemberLoginSucceeded || success.Account.ID != member || success.Account.Role != "member" {
		t.Fatalf("post-unlock login = %#v", success)
	}
}

func TestConcurrentMemberVerificationSerializesFailureAccounting(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	_, secrets, _ := provisionOwner(t, pool)
	start := time.Date(2026, time.August, 30, 10, 0, 0, 0, time.UTC)
	member := provisionMember(t, pool, "10000000-0000-4000-8000-000000000203", "member@example.com", "Member One", start)
	repository := auth.NewRepository(pool)
	issueMemberCode(t, repository, secrets, member, "111111", start)
	origin := secrets.Digest(auth.PurposeOrigin, "203.0.113.0/24")

	// Twelve devices submit the correct code at once; exactly one may consume it.
	const workers = 12
	results := make(chan memberVerifyRecord, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			now := start.Add(time.Second)
			result, err := repository.VerifyMemberChallenge(context.Background(), auth.VerifyMemberChallengeParams{
				UserID: member, CodeDigest: secrets.Digest(auth.PurposeMemberCode, "111111"), Now: now,
				Session: memberSession(t, secrets, newTestUUID(t), member, now), OriginDigest: origin,
			})
			results <- memberVerifyRecord{Outcome: result.Outcome, Err: err}
		}()
	}
	group.Wait()
	close(results)

	succeeded := 0
	for record := range results {
		if record.Err != nil {
			t.Fatal(record.Err)
		}
		if record.Outcome == auth.MemberLoginSucceeded {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("concurrent successes = %d, want exactly 1", succeeded)
	}
	var usedChallenges, activeChallenges, createdSessions int
	if err := pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM member_login_challenges WHERE user_id=$1 AND state='used'),
		(SELECT count(*) FROM member_login_challenges WHERE user_id=$1 AND state='active'),
		(SELECT count(*) FROM sessions WHERE user_id=$1)`, member).Scan(&usedChallenges, &activeChallenges, &createdSessions); err != nil {
		t.Fatal(err)
	}
	if usedChallenges != 1 || activeChallenges != 0 || createdSessions != 1 {
		t.Fatalf("used=%d active=%d sessions=%d, want exactly one single-use consumption",
			usedChallenges, activeChallenges, createdSessions)
	}
}

func TestNewMemberCodeSupersedesTheOlderDeliveredCode(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	_, secrets, _ := provisionOwner(t, pool)
	start := time.Date(2026, time.August, 30, 10, 0, 0, 0, time.UTC)
	member := provisionMember(t, pool, "10000000-0000-4000-8000-000000000204", "member@example.com", "Member One", start)
	repository := auth.NewRepository(pool)
	origin := secrets.Digest(auth.PurposeOrigin, "203.0.113.0/24")

	issueMemberCode(t, repository, secrets, member, "111111", start)
	issueMemberCode(t, repository, secrets, member, "222222", start.Add(time.Minute))

	// A delayed older email must not authenticate.
	older, err := repository.VerifyMemberChallenge(context.Background(), auth.VerifyMemberChallengeParams{
		UserID: member, CodeDigest: secrets.Digest(auth.PurposeMemberCode, "111111"), Now: start.Add(2 * time.Minute),
		Session: memberSession(t, secrets, newTestUUID(t), member, start.Add(2*time.Minute)), OriginDigest: origin,
	})
	if err != nil {
		t.Fatal(err)
	}
	if older.Outcome != auth.MemberLoginFailed {
		t.Fatalf("superseded code outcome = %q, want failed", older.Outcome)
	}

	// The newest code still works, and an expired code never does.
	newest, err := repository.VerifyMemberChallenge(context.Background(), auth.VerifyMemberChallengeParams{
		UserID: member, CodeDigest: secrets.Digest(auth.PurposeMemberCode, "222222"), Now: start.Add(3 * time.Minute),
		Session: memberSession(t, secrets, newTestUUID(t), member, start.Add(3*time.Minute)), OriginDigest: origin,
	})
	if err != nil {
		t.Fatal(err)
	}
	if newest.Outcome != auth.MemberLoginSucceeded {
		t.Fatalf("newest code outcome = %q, want succeeded", newest.Outcome)
	}

	issueMemberCode(t, repository, secrets, member, "333333", start.Add(10*time.Minute))
	expired, err := repository.VerifyMemberChallenge(context.Background(), auth.VerifyMemberChallengeParams{
		UserID: member, CodeDigest: secrets.Digest(auth.PurposeMemberCode, "333333"),
		Now:     start.Add(10 * time.Minute).Add(auth.MemberCodeTTL),
		Session: memberSession(t, secrets, newTestUUID(t), member, start.Add(25*time.Minute)), OriginDigest: origin,
	})
	if err != nil {
		t.Fatal(err)
	}
	if expired.Outcome != auth.MemberLoginFailed {
		t.Fatalf("expired code outcome = %q, want failed", expired.Outcome)
	}
}

func TestRetiredMemberCodeValueMayBeIssuedAgainLater(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	_, secrets, _ := provisionOwner(t, pool)
	start := time.Date(2026, time.August, 30, 10, 0, 0, 0, time.UTC)
	first := provisionMember(t, pool, "10000000-0000-4000-8000-000000000205", "first@example.com", "First Member", start)
	second := provisionMember(t, pool, "10000000-0000-4000-8000-000000000206", "second@example.com", "Second Member", start)
	repository := auth.NewRepository(pool)
	origin := secrets.Digest(auth.PurposeOrigin, "203.0.113.0/24")

	// Only one million codes exist, so the same value necessarily recurs over a host's lifetime.
	issueMemberCode(t, repository, secrets, first, "424242", start)
	consumed, err := repository.VerifyMemberChallenge(context.Background(), auth.VerifyMemberChallengeParams{
		UserID: first, CodeDigest: secrets.Digest(auth.PurposeMemberCode, "424242"), Now: start.Add(time.Minute),
		Session: memberSession(t, secrets, newTestUUID(t), first, start.Add(time.Minute)), OriginDigest: origin,
	})
	if err != nil {
		t.Fatal(err)
	}
	if consumed.Outcome != auth.MemberLoginSucceeded {
		t.Fatalf("first login outcome = %q, want succeeded", consumed.Outcome)
	}

	// A later member drawing the same random value must still receive a usable code.
	issueMemberCode(t, repository, secrets, second, "424242", start.Add(time.Hour))
	reissued, err := repository.VerifyMemberChallenge(context.Background(), auth.VerifyMemberChallengeParams{
		UserID: second, CodeDigest: secrets.Digest(auth.PurposeMemberCode, "424242"),
		Now:     start.Add(time.Hour + time.Minute),
		Session: memberSession(t, secrets, newTestUUID(t), second, start.Add(time.Hour+time.Minute)), OriginDigest: origin,
	})
	if err != nil {
		t.Fatal(err)
	}
	if reissued.Outcome != auth.MemberLoginSucceeded || reissued.Account.ID != second {
		t.Fatalf("reissued login = %#v, want the second member to sign in", reissued)
	}

	// Two members must never hold the same live code at the same time.
	issueMemberCode(t, repository, secrets, first, "515151", start.Add(2*time.Hour))
	if err := repository.IssueMemberChallenge(context.Background(), auth.IssueMemberChallengeParams{
		ChallengeID: newTestUUID(t), DeliveryID: newTestUUID(t), UserID: second, Email: "second@example.com",
		CodeDigest: secrets.Digest(auth.PurposeMemberCode, "515151"),
		CreatedAt:  start.Add(2 * time.Hour), ExpiresAt: start.Add(2*time.Hour + auth.MemberCodeTTL),
		OriginDigest: origin,
	}); err == nil {
		t.Fatal("two members were allowed to hold the same live code simultaneously")
	}
}
