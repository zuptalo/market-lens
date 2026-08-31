package auth

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"sync"
	"time"
)

// MemberCodeTTL bounds how long an emailed six-digit member code stays usable.
const MemberCodeTTL = 10 * time.Minute

// MemberCodeGenerator produces uniformly distributed six-digit login codes.
type MemberCodeGenerator struct {
	mu     sync.Mutex
	random io.Reader
}

// NewMemberCodeGenerator reads code entropy from random, defaulting to crypto/rand.
func NewMemberCodeGenerator(random io.Reader) *MemberCodeGenerator {
	if random == nil {
		random = rand.Reader
	}
	return &MemberCodeGenerator{random: random}
}

// Generate returns a zero-padded six-digit code.
func (generator *MemberCodeGenerator) Generate() (string, error) {
	generator.mu.Lock()
	defer generator.mu.Unlock()
	value, err := rand.Int(generator.random, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("generate member code: %w", err)
	}
	return fmt.Sprintf("%06d", value.Int64()), nil
}

type MemberChallengeState string

const (
	MemberChallengeActive     MemberChallengeState = "active"
	MemberChallengeUsed       MemberChallengeState = "used"
	MemberChallengeSuperseded MemberChallengeState = "superseded"
	MemberChallengeExpired    MemberChallengeState = "expired"
	MemberChallengeRevoked    MemberChallengeState = "revoked"
)

// MemberChallenge is a single-use emailed code retained only as a keyed digest.
type MemberChallenge struct {
	ID            string
	UserID        string
	CodeDigest    []byte
	State         MemberChallengeState
	CreatedAt     time.Time
	ExpiresAt     time.Time
	UsedAt        *time.Time
	InvalidatedAt *time.Time
}

// ValidMemberCode reports whether value is exactly six ASCII digits.
func ValidMemberCode(value string) bool {
	if len(value) != 6 {
		return false
	}
	for index := range len(value) {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}

// NewMemberChallenge builds an active challenge that stores only the keyed code digest.
func NewMemberChallenge(id, userID, code string, secrets *Secrets, now time.Time) (MemberChallenge, error) {
	if !validSessionUUID(id) || !validSessionUUID(userID) {
		return MemberChallenge{}, errors.New("member challenge identity is invalid")
	}
	if !ValidMemberCode(code) {
		return MemberChallenge{}, errors.New("member code must contain exactly six digits")
	}
	if secrets == nil || now.IsZero() {
		return MemberChallenge{}, errors.New("member challenge requires secrets and a clock")
	}
	created := now.UTC()
	return MemberChallenge{
		ID: id, UserID: userID, CodeDigest: secrets.Digest(PurposeMemberCode, code),
		State: MemberChallengeActive, CreatedAt: created, ExpiresAt: created.Add(MemberCodeTTL),
	}, nil
}

// ActiveAt reports whether the challenge may still be consumed at now.
func (challenge MemberChallenge) ActiveAt(now time.Time) bool {
	return challenge.State == MemberChallengeActive && len(challenge.CodeDigest) == 32 &&
		!now.Before(challenge.CreatedAt) && now.Before(challenge.ExpiresAt)
}

// Supersede retires an active challenge because a newer code was issued.
func (challenge *MemberChallenge) Supersede(now time.Time) error {
	if challenge.State != MemberChallengeActive {
		return errors.New("only an active member challenge can be superseded")
	}
	if now.Before(challenge.CreatedAt) {
		return errors.New("member challenge supersession precedes creation")
	}
	invalidatedAt := now.UTC()
	challenge.State = MemberChallengeSuperseded
	challenge.InvalidatedAt = &invalidatedAt
	return nil
}

// Consume verifies code in constant time and marks the challenge used exactly once.
func (challenge *MemberChallenge) Consume(code string, secrets *Secrets, now time.Time) error {
	if secrets == nil || !challenge.ActiveAt(now) {
		return ErrAuthenticationFailed
	}
	if !ValidMemberCode(code) || !secrets.VerifyDigest(PurposeMemberCode, code, challenge.CodeDigest) {
		return ErrAuthenticationFailed
	}
	usedAt := now.UTC()
	challenge.State = MemberChallengeUsed
	challenge.UsedAt = &usedAt
	return nil
}

// Brute-force thresholds fixed by specification 004 (SC-009).
const (
	MemberBlockThreshold = 3
	MemberBlockDuration  = 15 * time.Minute
	MemberLockThreshold  = 10
	MemberLockWindow     = 24 * time.Hour
)

// MemberLockedReason is the only administrative lock cause this feature records.
const MemberLockedReason = "wrong_code_limit"

// MemberLoginOutcome reports how a member verification attempt resolved.
type MemberLoginOutcome string

const (
	MemberLoginSucceeded MemberLoginOutcome = "succeeded"
	MemberLoginFailed    MemberLoginOutcome = "failed"
	MemberLoginBlocked   MemberLoginOutcome = "blocked"
	MemberLoginLocked    MemberLoginOutcome = "locked"
)

// MemberLoginState is the durable per-member throttling record.
type MemberLoginState struct {
	UserID                   string
	ConsecutiveFailures      int
	BlockedUntil             *time.Time
	AdministrativelyLockedAt *time.Time
	LockedReason             string
	LastCodeSentAt           *time.Time
	UpdatedAt                time.Time
}

// Locked reports whether only an owner can restore this member's sign-in.
func (state MemberLoginState) Locked() bool {
	return state.AdministrativelyLockedAt != nil
}

// BlockedAt reports whether a temporary block is still in force at now.
func (state MemberLoginState) BlockedAt(now time.Time) bool {
	return state.BlockedUntil != nil && now.Before(*state.BlockedUntil)
}

// RecordFailure applies one wrong-code submission, escalating to a temporary block on the
// third consecutive failure and to an owner-only lock on the tenth failure in a rolling day.
func (state *MemberLoginState) RecordFailure(now time.Time, rollingFailures int) (MemberLoginOutcome, error) {
	if now.IsZero() || rollingFailures < 1 {
		return "", errors.New("member failure accounting requires a clock and rolling count")
	}
	if state.Locked() {
		return MemberLoginLocked, nil
	}
	state.UpdatedAt = now.UTC()
	if rollingFailures >= MemberLockThreshold {
		lockedAt := now.UTC()
		state.AdministrativelyLockedAt = &lockedAt
		state.LockedReason = MemberLockedReason
		state.ConsecutiveFailures = 0
		state.BlockedUntil = nil
		return MemberLoginLocked, nil
	}
	if state.ConsecutiveFailures+1 >= MemberBlockThreshold {
		blockedUntil := now.UTC().Add(MemberBlockDuration)
		state.BlockedUntil = &blockedUntil
		state.ConsecutiveFailures = 0
		return MemberLoginBlocked, nil
	}
	state.ConsecutiveFailures++
	return MemberLoginFailed, nil
}

// RecordSuccess clears consecutive failure accounting after a consumed code.
func (state *MemberLoginState) RecordSuccess(now time.Time) error {
	if now.IsZero() {
		return errors.New("member success accounting requires a clock")
	}
	if state.Locked() {
		return errors.New("a locked member cannot record a successful sign-in")
	}
	state.ConsecutiveFailures = 0
	state.BlockedUntil = nil
	state.UpdatedAt = now.UTC()
	return nil
}

// Unlock clears owner-recoverable block and lock state without changing account status.
func (state *MemberLoginState) Unlock(now time.Time) error {
	if now.IsZero() {
		return errors.New("member unlock requires a clock")
	}
	state.AdministrativelyLockedAt = nil
	state.LockedReason = ""
	state.BlockedUntil = nil
	state.ConsecutiveFailures = 0
	state.UpdatedAt = now.UTC()
	return nil
}

// RateBucketKind names an independent sliding-window throttle.
type RateBucketKind string

const (
	RateMemberCodeDelivery RateBucketKind = "member_code_delivery"
	RateOriginCodeRequest  RateBucketKind = "origin_code_request"
	RateOriginCodeVerify   RateBucketKind = "origin_code_verify"
	RateOwnerLogin         RateBucketKind = "owner_login"
	RateOwnerRecovery      RateBucketKind = "owner_recovery"
)

// RateLimit is one sliding window ceiling.
type RateLimit struct {
	Limit  int
	Window time.Duration
}

// Independent account and origin ceilings. Account limits bound how often one person can be
// emailed; origin limits bound distributed guessing and spraying across many accounts.
var (
	MemberCodeDeliveryLimits = []RateLimit{{Limit: 1, Window: time.Minute}, {Limit: 5, Window: time.Hour}}
	OriginCodeRequestLimits  = []RateLimit{{Limit: 10, Window: time.Minute}, {Limit: 60, Window: time.Hour}}
	OriginCodeVerifyLimits   = []RateLimit{{Limit: 20, Window: time.Minute}, {Limit: 120, Window: time.Hour}}
)

// RateRetryGranularity coarsens every retry hint so timing cannot be used to probe buckets.
const RateRetryGranularity = time.Minute

// RateDecision reports whether an attempt is permitted and a coarse retry hint.
type RateDecision struct {
	Allowed    bool
	RetryAfter time.Duration
}

// CoarsenRetryAfter rounds a retry hint up to a fixed granularity.
func CoarsenRetryAfter(remaining time.Duration) time.Duration {
	if remaining <= 0 {
		return RateRetryGranularity
	}
	steps := (remaining + RateRetryGranularity - 1) / RateRetryGranularity
	return steps * RateRetryGranularity
}

// MaxRateWindow is the longest sliding window any bucket uses, and therefore the age beyond
// which a recorded rate event can no longer affect a decision.
var MaxRateWindow = maxRateWindow()

func maxRateWindow() time.Duration {
	longest := time.Duration(0)
	for _, limits := range [][]RateLimit{MemberCodeDeliveryLimits, OriginCodeRequestLimits, OriginCodeVerifyLimits} {
		for _, limit := range limits {
			if limit.Window > longest {
				longest = limit.Window
			}
		}
	}
	return longest
}
