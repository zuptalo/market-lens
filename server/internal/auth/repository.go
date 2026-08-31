package auth

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"hash/fnv"
	"io"
	"time"

	clientevents "market-lens/server/internal/events"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

type ownerCredentialRecord struct {
	Account      Account
	PasswordHash string
}

func (repository *Repository) OwnerCredential(ctx context.Context, normalizedEmail string) (ownerCredentialRecord, error) {
	var record ownerCredentialRecord
	err := repository.pool.QueryRow(ctx, `SELECT
		u.id::text,u.email,u.display_name,u.role,u.status,u.email_verified_at,c.password_hash
		FROM users u JOIN owner_credentials c ON c.user_id=u.id
		WHERE u.normalized_email=$1 AND u.role='owner' AND u.status='active' AND u.email_verified_at IS NOT NULL`,
		normalizedEmail).Scan(&record.Account.ID, &record.Account.Email, &record.Account.DisplayName,
		&record.Account.Role, &record.Account.Status, &record.Account.EmailVerifiedAt, &record.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return ownerCredentialRecord{}, ErrAuthenticationFailed
	}
	return record, err
}

func (repository *Repository) CreateOwnerSession(ctx context.Context, account Account, session Session, replacementHash string) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var active bool
	err = tx.QueryRow(ctx, `SELECT role='owner' AND status='active' AND email_verified_at IS NOT NULL
		FROM users WHERE id=$1 FOR UPDATE`, account.ID).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !active) {
		return ErrAuthenticationFailed
	}
	if err != nil {
		return err
	}
	if replacementHash != "" {
		if _, err := tx.Exec(ctx, `UPDATE owner_credentials SET password_hash=$1,changed_at=$2 WHERE user_id=$3`,
			replacementHash, session.CreatedAt, account.ID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO sessions
		(id,user_id,token_digest,csrf_digest,created_at,last_seen_at,idle_expires_at,absolute_expires_at,
		revoked_at,revoked_reason,device_label,origin_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULL,NULL,$9,$10)`,
		session.ID, session.UserID, session.TokenDigest, session.CSRFDigest, session.CreatedAt,
		session.LastSeenAt, session.IdleExpiresAt, session.AbsoluteExpiresAt, session.DeviceLabel,
		session.OriginDigest); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO security_audit_events
		(occurred_at,event_type,actor_user_id,subject_user_id,session_id,outcome,origin_digest,metadata)
		VALUES ($1,'owner.login.v1',$2,$2,$3,'succeeded',$4,'{}'::jsonb)`,
		session.CreatedAt, account.ID, session.ID, session.OriginDigest); err != nil {
		return err
	}
	if err := clientevents.Insert(ctx, tx, clientevents.Event{
		Type: "session.created.v1", Version: 1, Scope: "user", SubjectUserID: account.ID,
		EntityType: "session", EntityID: session.ID, OccurredAt: session.CreatedAt,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (repository *Repository) SessionByDigest(ctx context.Context, digest []byte) (Session, Account, error) {
	var session Session
	var account Account
	var revokedReason *string
	err := repository.pool.QueryRow(ctx, `SELECT
		s.id::text,s.user_id::text,s.token_digest,s.csrf_digest,s.created_at,s.last_seen_at,
		s.idle_expires_at,s.absolute_expires_at,s.revoked_at,s.revoked_reason,s.device_label,s.origin_digest,
		u.email,u.display_name,u.role,u.status,u.email_verified_at
		FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_digest=$1`, digest).Scan(
		&session.ID, &session.UserID, &session.TokenDigest, &session.CSRFDigest, &session.CreatedAt,
		&session.LastSeenAt, &session.IdleExpiresAt, &session.AbsoluteExpiresAt, &session.RevokedAt,
		&revokedReason, &session.DeviceLabel, &session.OriginDigest, &account.Email, &account.DisplayName,
		&account.Role, &account.Status, &account.EmailVerifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, Account{}, ErrAuthenticationRequired
	}
	if err != nil {
		return Session{}, Account{}, err
	}
	account.ID = session.UserID
	if revokedReason != nil {
		session.RevokedReason = RevokeReason(*revokedReason)
	}
	return session, account, nil
}

// SessionByID reads a session and its account without touching activity, for the periodic
// recheck behind an open stream. Streams must not keep an idle session alive by being watched.
func (repository *Repository) SessionByID(ctx context.Context, sessionID string) (Session, Account, error) {
	var session Session
	var account Account
	var revokedReason *string
	err := repository.pool.QueryRow(ctx, `SELECT
		s.id::text,s.user_id::text,s.token_digest,s.csrf_digest,s.created_at,s.last_seen_at,
		s.idle_expires_at,s.absolute_expires_at,s.revoked_at,s.revoked_reason,s.device_label,s.origin_digest,
		u.email,u.display_name,u.role,u.status,u.email_verified_at
		FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.id=$1`, sessionID).Scan(
		&session.ID, &session.UserID, &session.TokenDigest, &session.CSRFDigest, &session.CreatedAt,
		&session.LastSeenAt, &session.IdleExpiresAt, &session.AbsoluteExpiresAt, &session.RevokedAt,
		&revokedReason, &session.DeviceLabel, &session.OriginDigest, &account.Email, &account.DisplayName,
		&account.Role, &account.Status, &account.EmailVerifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, Account{}, ErrAuthenticationRequired
	}
	if err != nil {
		return Session{}, Account{}, err
	}
	account.ID = session.UserID
	if revokedReason != nil {
		session.RevokedReason = RevokeReason(*revokedReason)
	}
	return session, account, nil
}

func (repository *Repository) UpdateSessionActivity(ctx context.Context, session Session) error {
	result, err := repository.pool.Exec(ctx, `UPDATE sessions SET last_seen_at=$1,idle_expires_at=$2
		WHERE id=$3 AND revoked_at IS NULL`, session.LastSeenAt, session.IdleExpiresAt, session.ID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrAuthenticationRequired
	}
	return nil
}

func (repository *Repository) Account(ctx context.Context, userID string) (Account, error) {
	var account Account
	err := repository.pool.QueryRow(ctx, `SELECT id::text,email,display_name,role,status,email_verified_at
		FROM users WHERE id=$1 AND status='active' AND email_verified_at IS NOT NULL`, userID).
		Scan(&account.ID, &account.Email, &account.DisplayName, &account.Role, &account.Status, &account.EmailVerifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrAuthenticationRequired
	}
	return account, err
}

func (repository *Repository) ListSessions(ctx context.Context, userID string) ([]Session, error) {
	rows, err := repository.pool.Query(ctx, `SELECT id::text,user_id::text,token_digest,csrf_digest,created_at,last_seen_at,
		idle_expires_at,absolute_expires_at,revoked_at,revoked_reason,device_label,origin_digest
		FROM sessions WHERE user_id=$1 ORDER BY created_at DESC,id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []Session
	for rows.Next() {
		var session Session
		var revokedReason *string
		if err := rows.Scan(&session.ID, &session.UserID, &session.TokenDigest, &session.CSRFDigest,
			&session.CreatedAt, &session.LastSeenAt, &session.IdleExpiresAt, &session.AbsoluteExpiresAt,
			&session.RevokedAt, &revokedReason, &session.DeviceLabel, &session.OriginDigest); err != nil {
			return nil, err
		}
		if revokedReason != nil {
			session.RevokedReason = RevokeReason(*revokedReason)
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func (repository *Repository) RevokeSession(ctx context.Context, userID, sessionID string, now time.Time) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at=$1,revoked_reason=$2
		WHERE id=$3 AND user_id=$4 AND revoked_at IS NULL`, now, RevokeUserRequested, sessionID, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE id=$1 AND user_id=$2)`, sessionID, userID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrAuthenticationRequired
		}
		return tx.Commit(ctx)
	}
	if err := clientevents.Insert(ctx, tx, clientevents.Event{
		Type: "session.revoked.v1", Version: 1, Scope: "user", SubjectUserID: userID,
		EntityType: "session", EntityID: sessionID, OccurredAt: now,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (repository *Repository) RevokeAllSessions(ctx context.Context, userID string, now time.Time) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at=$1,revoked_reason=$2
		WHERE user_id=$3 AND revoked_at IS NULL`, now, RevokeAllDevices, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() > 0 {
		if err := clientevents.Insert(ctx, tx, clientevents.Event{
			Type: "sessions.revoked.v1", Version: 1, Scope: "user", SubjectUserID: userID,
			EntityType: "sessions", EntityID: userID, OccurredAt: now,
		}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (repository *Repository) ResetOwnerPassword(ctx context.Context, passwordHash string, now time.Time) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var ownerID string
	err = tx.QueryRow(ctx, `SELECT u.id::text
		FROM users u JOIN owner_credentials c ON c.user_id=u.id
		WHERE u.role='owner' AND u.status='active' AND u.email_verified_at IS NOT NULL
		FOR UPDATE OF u,c`).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAuthenticationFailed
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE owner_credentials SET password_hash=$1,changed_at=$2 WHERE user_id=$3`,
		passwordHash, now, ownerID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at=$1,revoked_reason=$2
		WHERE user_id=$3 AND revoked_at IS NULL`, now, RevokeOwnerPasswordReset, ownerID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO security_audit_events
		(occurred_at,event_type,actor_user_id,subject_user_id,outcome,metadata)
		VALUES ($1,'owner.password_reset.v1',$2,$2,'succeeded','{}'::jsonb)`, now, ownerID); err != nil {
		return err
	}
	if err := clientevents.Insert(ctx, tx, clientevents.Event{
		Type: "owner.password_reset.v1", Version: 1, Scope: "owner",
		EntityType: "credential", EntityID: ownerID, OccurredAt: now,
	}); err != nil {
		return err
	}
	if err := clientevents.Insert(ctx, tx, clientevents.Event{
		Type: "sessions.revoked.v1", Version: 1, Scope: "user", SubjectUserID: ownerID,
		EntityType: "sessions", EntityID: ownerID, OccurredAt: now,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (repository *Repository) RecoveryOwner(ctx context.Context, normalizedEmail string) (Account, error) {
	var account Account
	err := repository.pool.QueryRow(ctx, `SELECT id::text,email,display_name,role,status,email_verified_at
		FROM users WHERE normalized_email=$1 AND role='owner' AND status='active' AND email_verified_at IS NOT NULL`,
		normalizedEmail).Scan(&account.ID, &account.Email, &account.DisplayName, &account.Role,
		&account.Status, &account.EmailVerifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrAuthenticationFailed
	}
	return account, err
}

type CreateRecoveryParams struct {
	CapabilityID     string
	CapabilityDigest []byte
	DeliveryID       string
	Account          Account
	CreatedAt        time.Time
	ExpiresAt        time.Time
	OriginDigest     []byte
}

func (repository *Repository) CreateRecovery(ctx context.Context, params CreateRecoveryParams) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var active bool
	err = tx.QueryRow(ctx, `SELECT role='owner' AND status='active' AND email_verified_at IS NOT NULL
		FROM users WHERE id=$1 FOR UPDATE`, params.Account.ID).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !active) {
		return ErrAuthenticationFailed
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE auth_capabilities SET revoked_at=$1
		WHERE kind='owner_recovery' AND user_id=$2 AND consumed_at IS NULL AND revoked_at IS NULL`,
		params.CreatedAt, params.Account.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO auth_capabilities
		(id,kind,user_id,token_digest,expires_at,consumed_at,revoked_at,created_at)
		VALUES ($1,'owner_recovery',$2,$3,$4,NULL,NULL,$5)`,
		params.CapabilityID, params.Account.ID, params.CapabilityDigest, params.ExpiresAt, params.CreatedAt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO account_email_deliveries
		(id,kind,recipient_email,subject_user_id,state,attempt_count,created_at)
		VALUES ($1,'owner_recovery',$2,$3,'pending',0,$4)`,
		params.DeliveryID, params.Account.Email, params.Account.ID, params.CreatedAt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO security_audit_events
		(occurred_at,event_type,subject_user_id,outcome,origin_digest,metadata)
		VALUES ($1,'owner.recovery_requested.v1',$2,'succeeded',$3,'{}'::jsonb)`,
		params.CreatedAt, params.Account.ID, params.OriginDigest); err != nil {
		return err
	}
	if err := clientevents.Insert(ctx, tx, clientevents.Event{
		Type: "recovery.changed.v1", Version: 1, Scope: "owner",
		EntityType: "owner_recovery", EntityID: params.CapabilityID, OccurredAt: params.CreatedAt,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (repository *Repository) MarkRecoveryDelivery(ctx context.Context, capabilityID, deliveryID string, now time.Time, delivered bool) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if delivered {
		result, err := tx.Exec(ctx, `UPDATE account_email_deliveries
			SET state='sent',attempt_count=attempt_count+1,last_attempt_at=$1,sent_at=$1,error_code=NULL
			WHERE id=$2 AND state='pending'`, now, deliveryID)
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return errors.New("recovery delivery state changed")
		}
	} else {
		if _, err := tx.Exec(ctx, `UPDATE account_email_deliveries
			SET state='failed',attempt_count=attempt_count+1,last_attempt_at=$1,error_code='temporary_failure'
			WHERE id=$2 AND state='pending'`, now, deliveryID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE auth_capabilities SET revoked_at=$1
			WHERE id=$2 AND consumed_at IS NULL AND revoked_at IS NULL`, now, capabilityID); err != nil {
			return err
		}
	}
	if err := clientevents.Insert(ctx, tx, clientevents.Event{
		Type: "recovery.changed.v1", Version: 1, Scope: "owner",
		EntityType: "owner_recovery", EntityID: capabilityID, OccurredAt: now,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type CompleteRecoveryParams struct {
	CapabilityDigest []byte
	PasswordHash     string
	Now              time.Time
	OriginDigest     []byte
}

func (repository *Repository) CompleteRecovery(ctx context.Context, params CompleteRecoveryParams) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var capabilityID, userID string
	var expiresAt time.Time
	var consumedAt, revokedAt *time.Time
	var active bool
	err = tx.QueryRow(ctx, `SELECT c.id::text,c.user_id::text,c.expires_at,c.consumed_at,c.revoked_at,
		u.role='owner' AND u.status='active' AND u.email_verified_at IS NOT NULL
		FROM auth_capabilities c JOIN users u ON u.id=c.user_id
		WHERE c.kind='owner_recovery' AND c.token_digest=$1 FOR UPDATE`, params.CapabilityDigest).Scan(
		&capabilityID, &userID, &expiresAt, &consumedAt, &revokedAt, &active)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (!active || consumedAt != nil || revokedAt != nil || !params.Now.Before(expiresAt))) {
		return errors.New("retired recovery capability is unavailable")
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE owner_credentials SET password_hash=$1,changed_at=$2 WHERE user_id=$3`,
		params.PasswordHash, params.Now, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE auth_capabilities SET consumed_at=$1 WHERE id=$2`, params.Now, capabilityID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE auth_capabilities SET revoked_at=$1
		WHERE kind='owner_recovery' AND user_id=$2 AND id<>$3 AND consumed_at IS NULL AND revoked_at IS NULL`,
		params.Now, userID, capabilityID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at=$1,revoked_reason=$2
		WHERE user_id=$3 AND revoked_at IS NULL`, params.Now, RevokeOwnerRecovery, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO security_audit_events
		(occurred_at,event_type,subject_user_id,outcome,origin_digest,metadata)
		VALUES ($1,'owner.recovery_completed.v1',$2,'succeeded',$3,'{}'::jsonb)`,
		params.Now, userID, params.OriginDigest); err != nil {
		return err
	}
	if err := clientevents.Insert(ctx, tx, clientevents.Event{
		Type: "credential.changed.v1", Version: 1, Scope: "user", SubjectUserID: userID,
		EntityType: "account", EntityID: userID, OccurredAt: params.Now,
	}); err != nil {
		return err
	}
	if err := clientevents.Insert(ctx, tx, clientevents.Event{
		Type: "sessions.revoked.v1", Version: 1, Scope: "user", SubjectUserID: userID,
		EntityType: "sessions", EntityID: userID, OccurredAt: params.Now,
	}); err != nil {
		return err
	}
	if err := clientevents.Insert(ctx, tx, clientevents.Event{
		Type: "recovery.changed.v1", Version: 1, Scope: "owner",
		EntityType: "owner_recovery", EntityID: capabilityID, OccurredAt: params.Now,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// IssueMemberChallengeParams carries one newest-only member code issuance.
type IssueMemberChallengeParams struct {
	ChallengeID  string
	DeliveryID   string
	UserID       string
	Email        string
	CodeDigest   []byte
	CreatedAt    time.Time
	ExpiresAt    time.Time
	OriginDigest []byte
}

// VerifyMemberChallengeParams carries one serialized member code verification.
type VerifyMemberChallengeParams struct {
	UserID       string
	CodeDigest   []byte
	Session      Session
	Now          time.Time
	OriginDigest []byte
}

// VerifyMemberChallengeResult reports the durable outcome of a verification attempt.
type VerifyMemberChallengeResult struct {
	Outcome      MemberLoginOutcome
	Account      Account
	BlockedUntil *time.Time
	LockedAt     *time.Time
}

// MemberForLogin returns the active, verified member that may receive a login code.
func (repository *Repository) MemberForLogin(ctx context.Context, normalizedEmail string) (Account, error) {
	var account Account
	err := repository.pool.QueryRow(ctx, `SELECT id::text,email,display_name,role,status,email_verified_at
		FROM users WHERE normalized_email=$1 AND role='member' AND status='active' AND email_verified_at IS NOT NULL`,
		normalizedEmail).Scan(&account.ID, &account.Email, &account.DisplayName, &account.Role,
		&account.Status, &account.EmailVerifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrAuthenticationFailed
	}
	return account, err
}

// lockMemberLoginState creates the throttling row when absent and locks it for this transaction.
func lockMemberLoginState(ctx context.Context, tx pgx.Tx, userID string, now time.Time) (MemberLoginState, error) {
	if _, err := tx.Exec(ctx, `INSERT INTO member_login_state (user_id,consecutive_failures,updated_at)
		VALUES ($1,0,$2) ON CONFLICT (user_id) DO NOTHING`, userID, now); err != nil {
		return MemberLoginState{}, err
	}
	state := MemberLoginState{UserID: userID}
	var lockedReason *string
	err := tx.QueryRow(ctx, `SELECT consecutive_failures,blocked_until,administratively_locked_at,
		locked_reason,last_code_sent_at,updated_at FROM member_login_state WHERE user_id=$1 FOR UPDATE`, userID).
		Scan(&state.ConsecutiveFailures, &state.BlockedUntil, &state.AdministrativelyLockedAt,
			&lockedReason, &state.LastCodeSentAt, &state.UpdatedAt)
	if err != nil {
		return MemberLoginState{}, err
	}
	if lockedReason != nil {
		state.LockedReason = *lockedReason
	}
	return state, nil
}

// saveMemberLoginState persists the throttling row after a domain transition.
func saveMemberLoginState(ctx context.Context, tx pgx.Tx, state MemberLoginState) error {
	var lockedReason *string
	if state.LockedReason != "" {
		reason := state.LockedReason
		lockedReason = &reason
	}
	_, err := tx.Exec(ctx, `UPDATE member_login_state SET consecutive_failures=$2,blocked_until=$3,
		administratively_locked_at=$4,locked_reason=$5,updated_at=$6 WHERE user_id=$1`,
		state.UserID, state.ConsecutiveFailures, state.BlockedUntil, state.AdministrativelyLockedAt,
		lockedReason, state.UpdatedAt)
	return err
}

// IssueMemberChallenge supersedes any outstanding code and stores exactly one newest active
// challenge together with its pending transactional-email delivery row.
func (repository *Repository) IssueMemberChallenge(ctx context.Context, params IssueMemberChallengeParams) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var active bool
	err = tx.QueryRow(ctx, `SELECT role='member' AND status='active' AND email_verified_at IS NOT NULL
		FROM users WHERE id=$1 FOR UPDATE`, params.UserID).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !active) {
		return ErrAuthenticationFailed
	}
	if err != nil {
		return err
	}
	state, err := lockMemberLoginState(ctx, tx, params.UserID, params.CreatedAt)
	if err != nil {
		return err
	}
	if state.Locked() {
		return ErrMemberLocked
	}
	// A code issued during a temporary block could not be used before the block elapses, so
	// issuing one would only enable mail-bombing the member.
	if state.BlockedAt(params.CreatedAt) {
		return ErrMemberBlocked
	}
	if _, err := tx.Exec(ctx, `UPDATE member_login_challenges SET state='superseded',invalidated_at=$2
		WHERE user_id=$1 AND state='active'`, params.UserID, params.CreatedAt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO account_email_deliveries
		(id,kind,recipient_email,subject_user_id,challenge_id,state,attempt_count,created_at)
		VALUES ($1,'member_login_code',$2,$3,NULL,'pending',0,$4)`,
		params.DeliveryID, params.Email, params.UserID, params.CreatedAt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO member_login_challenges
		(id,user_id,code_digest,state,expires_at,used_at,invalidated_at,created_at,delivery_id)
		VALUES ($1,$2,$3,'active',$4,NULL,NULL,$5,$6)`,
		params.ChallengeID, params.UserID, params.CodeDigest, params.ExpiresAt,
		params.CreatedAt, params.DeliveryID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE account_email_deliveries SET challenge_id=$2 WHERE id=$1`,
		params.DeliveryID, params.ChallengeID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE member_login_state SET last_code_sent_at=$2,updated_at=$2 WHERE user_id=$1`,
		params.UserID, params.CreatedAt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO security_audit_events
		(occurred_at,event_type,subject_user_id,outcome,origin_digest,metadata)
		VALUES ($1,'member.code_requested.v1',$2,'succeeded',$3,'{}'::jsonb)`,
		params.CreatedAt, params.UserID, params.OriginDigest); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// VerifyMemberChallenge serializes one member verification attempt, applying durable block and
// administrative-lock thresholds before any code comparison.
func (repository *Repository) VerifyMemberChallenge(ctx context.Context, params VerifyMemberChallengeParams) (VerifyMemberChallengeResult, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return VerifyMemberChallengeResult{}, err
	}
	defer tx.Rollback(ctx)

	var account Account
	account.ID = params.UserID
	var active bool
	err = tx.QueryRow(ctx, `SELECT email,display_name,role,status,email_verified_at,
		role='member' AND status='active' AND email_verified_at IS NOT NULL
		FROM users WHERE id=$1 FOR UPDATE`, params.UserID).Scan(&account.Email, &account.DisplayName,
		&account.Role, &account.Status, &account.EmailVerifiedAt, &active)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !active) {
		return VerifyMemberChallengeResult{Outcome: MemberLoginFailed}, nil
	}
	if err != nil {
		return VerifyMemberChallengeResult{}, err
	}
	state, err := lockMemberLoginState(ctx, tx, params.UserID, params.Now)
	if err != nil {
		return VerifyMemberChallengeResult{}, err
	}

	// Refuse before comparing any code so blocked and locked periods cannot be probed.
	if state.Locked() {
		if err := recordMemberAudit(ctx, tx, params, "member.login.v1", string(MemberLoginLocked)); err != nil {
			return VerifyMemberChallengeResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return VerifyMemberChallengeResult{}, err
		}
		return VerifyMemberChallengeResult{Outcome: MemberLoginLocked, LockedAt: state.AdministrativelyLockedAt}, nil
	}
	if state.BlockedAt(params.Now) {
		if err := recordMemberAudit(ctx, tx, params, "member.login.v1", string(MemberLoginBlocked)); err != nil {
			return VerifyMemberChallengeResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return VerifyMemberChallengeResult{}, err
		}
		return VerifyMemberChallengeResult{Outcome: MemberLoginBlocked, BlockedUntil: state.BlockedUntil}, nil
	}

	// Retire any elapsed challenge before selecting the newest usable one.
	if _, err := tx.Exec(ctx, `UPDATE member_login_challenges SET state='expired'
		WHERE user_id=$1 AND state='active' AND expires_at<=$2`, params.UserID, params.Now); err != nil {
		return VerifyMemberChallengeResult{}, err
	}
	var activeChallengeID, latestChallengeID *string
	var storedDigest []byte
	if err := tx.QueryRow(ctx, `SELECT
		(SELECT id::text FROM member_login_challenges WHERE user_id=$1 AND state='active'),
		(SELECT id::text FROM member_login_challenges WHERE user_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1),
		(SELECT code_digest FROM member_login_challenges WHERE user_id=$1 AND state='active')`,
		params.UserID).Scan(&activeChallengeID, &latestChallengeID, &storedDigest); err != nil {
		return VerifyMemberChallengeResult{}, err
	}
	if latestChallengeID == nil {
		// Nothing was ever issued, so there is no challenge to brute force.
		if err := tx.Commit(ctx); err != nil {
			return VerifyMemberChallengeResult{}, err
		}
		return VerifyMemberChallengeResult{Outcome: MemberLoginFailed}, nil
	}

	if activeChallengeID != nil && hmac.Equal(storedDigest, params.CodeDigest) {
		if _, err := tx.Exec(ctx, `UPDATE member_login_challenges SET state='used',used_at=$2
			WHERE id=$1 AND state='active'`, *activeChallengeID, params.Now); err != nil {
			return VerifyMemberChallengeResult{}, err
		}
		if err := state.RecordSuccess(params.Now); err != nil {
			return VerifyMemberChallengeResult{}, err
		}
		if err := saveMemberLoginState(ctx, tx, state); err != nil {
			return VerifyMemberChallengeResult{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO sessions
			(id,user_id,token_digest,csrf_digest,created_at,last_seen_at,idle_expires_at,absolute_expires_at,
			revoked_at,revoked_reason,device_label,origin_digest)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULL,NULL,$9,$10)`,
			params.Session.ID, params.UserID, params.Session.TokenDigest, params.Session.CSRFDigest,
			params.Session.CreatedAt, params.Session.LastSeenAt, params.Session.IdleExpiresAt,
			params.Session.AbsoluteExpiresAt, params.Session.DeviceLabel, params.Session.OriginDigest); err != nil {
			return VerifyMemberChallengeResult{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO security_audit_events
			(occurred_at,event_type,actor_user_id,subject_user_id,session_id,outcome,origin_digest,metadata)
			VALUES ($1,'member.login.v1',$2,$2,$3,'succeeded',$4,'{}'::jsonb)`,
			params.Now, params.UserID, params.Session.ID, params.OriginDigest); err != nil {
			return VerifyMemberChallengeResult{}, err
		}
		if err := clientevents.Insert(ctx, tx, clientevents.Event{
			Type: "session.created.v1", Version: 1, Scope: "user", SubjectUserID: params.UserID,
			EntityType: "session", EntityID: params.Session.ID, OccurredAt: params.Now,
		}); err != nil {
			return VerifyMemberChallengeResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return VerifyMemberChallengeResult{}, err
		}
		return VerifyMemberChallengeResult{Outcome: MemberLoginSucceeded, Account: account}, nil
	}

	// A wrong, expired, superseded, or already-used code counts as one durable failure.
	if _, err := tx.Exec(ctx, `INSERT INTO login_failure_events (user_id,challenge_id,occurred_at,origin_digest)
		VALUES ($1,$2,$3,$4)`, params.UserID, *latestChallengeID, params.Now, params.OriginDigest); err != nil {
		return VerifyMemberChallengeResult{}, err
	}
	var rollingFailures int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM login_failure_events
		WHERE user_id=$1 AND occurred_at > $2`, params.UserID, params.Now.Add(-MemberLockWindow)).
		Scan(&rollingFailures); err != nil {
		return VerifyMemberChallengeResult{}, err
	}
	outcome, err := state.RecordFailure(params.Now, rollingFailures)
	if err != nil {
		return VerifyMemberChallengeResult{}, err
	}
	if err := saveMemberLoginState(ctx, tx, state); err != nil {
		return VerifyMemberChallengeResult{}, err
	}
	if outcome != MemberLoginFailed {
		// Crossing a threshold retires the outstanding code so a fresh one is always required.
		if _, err := tx.Exec(ctx, `UPDATE member_login_challenges SET state='revoked',invalidated_at=$2
			WHERE user_id=$1 AND state='active'`, params.UserID, params.Now); err != nil {
			return VerifyMemberChallengeResult{}, err
		}
		if err := clientevents.Insert(ctx, tx, clientevents.Event{
			Type: "member.changed.v1", Version: 1, Scope: "owner",
			EntityType: "member", EntityID: params.UserID, OccurredAt: params.Now,
		}); err != nil {
			return VerifyMemberChallengeResult{}, err
		}
	}
	if err := recordMemberAudit(ctx, tx, params, "member.login.v1", string(outcome)); err != nil {
		return VerifyMemberChallengeResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return VerifyMemberChallengeResult{}, err
	}
	return VerifyMemberChallengeResult{
		Outcome: outcome, BlockedUntil: state.BlockedUntil, LockedAt: state.AdministrativelyLockedAt,
	}, nil
}

// recordMemberAudit appends one secret-free member authentication audit row.
func recordMemberAudit(ctx context.Context, tx pgx.Tx, params VerifyMemberChallengeParams, eventType, outcome string) error {
	_, err := tx.Exec(ctx, `INSERT INTO security_audit_events
		(occurred_at,event_type,subject_user_id,outcome,origin_digest,metadata)
		VALUES ($1,$2,$3,$4,$5,'{}'::jsonb)`,
		params.Now, eventType, params.UserID, outcome, params.OriginDigest)
	return err
}

// MemberLoginStateFor returns the durable throttling record, or a zero state when absent.
func (repository *Repository) MemberLoginStateFor(ctx context.Context, userID string) (MemberLoginState, error) {
	state := MemberLoginState{UserID: userID}
	var lockedReason *string
	err := repository.pool.QueryRow(ctx, `SELECT consecutive_failures,blocked_until,administratively_locked_at,
		locked_reason,last_code_sent_at,updated_at FROM member_login_state WHERE user_id=$1`, userID).
		Scan(&state.ConsecutiveFailures, &state.BlockedUntil, &state.AdministrativelyLockedAt,
			&lockedReason, &state.LastCodeSentAt, &state.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return MemberLoginState{UserID: userID}, nil
	}
	if err != nil {
		return MemberLoginState{}, err
	}
	if lockedReason != nil {
		state.LockedReason = *lockedReason
	}
	return state, nil
}

// UnlockMember clears owner-recoverable throttling state without reactivating the account or
// granting any session, and retires outstanding codes so a fresh one is required.
func (repository *Repository) UnlockMember(ctx context.Context, ownerID, userID string, now time.Time) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var ownerActive bool
	err = tx.QueryRow(ctx, `SELECT role='owner' AND status='active' AND email_verified_at IS NOT NULL
		FROM users WHERE id=$1`, ownerID).Scan(&ownerActive)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !ownerActive) {
		return ErrOwnerRequired
	}
	if err != nil {
		return err
	}
	var isMember bool
	err = tx.QueryRow(ctx, `SELECT role='member' FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&isMember)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !isMember) {
		return ErrMemberNotFound
	}
	if err != nil {
		return err
	}
	state, err := lockMemberLoginState(ctx, tx, userID, now)
	if err != nil {
		return err
	}
	if err := state.Unlock(now); err != nil {
		return err
	}
	if err := saveMemberLoginState(ctx, tx, state); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM login_failure_events WHERE user_id=$1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE member_login_challenges SET state='revoked',invalidated_at=$2
		WHERE user_id=$1 AND state='active'`, userID, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO security_audit_events
		(occurred_at,event_type,actor_user_id,subject_user_id,outcome,metadata)
		VALUES ($1,'member.unlocked.v1',$2,$3,'succeeded','{}'::jsonb)`, now, ownerID, userID); err != nil {
		return err
	}
	if err := clientevents.Insert(ctx, tx, clientevents.Event{
		Type: "member.changed.v1", Version: 1, Scope: "owner",
		EntityType: "member", EntityID: userID, OccurredAt: now,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// MarkMemberCodeDelivery records the transactional-email outcome for an issued code and retires
// the challenge when the provider could not accept the message.
func (repository *Repository) MarkMemberCodeDelivery(ctx context.Context, challengeID, deliveryID string, now time.Time, delivered bool) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if delivered {
		if _, err := tx.Exec(ctx, `UPDATE account_email_deliveries
			SET state='sent',attempt_count=attempt_count+1,last_attempt_at=$1,sent_at=$1,error_code=NULL
			WHERE id=$2 AND state='pending'`, now, deliveryID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `UPDATE account_email_deliveries
		SET state='failed',attempt_count=attempt_count+1,last_attempt_at=$1,error_code='temporary_failure'
		WHERE id=$2 AND state='pending'`, now, deliveryID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE member_login_challenges SET state='revoked',invalidated_at=$2
		WHERE id=$1 AND state='active'`, challengeID, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// rateBucketLock derives a stable advisory-lock key so decisions for one bucket serialize
// without blocking unrelated accounts or origins.
func rateBucketLock(kind RateBucketKind, digest []byte) int64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(kind))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(digest)
	return int64(hash.Sum64())
}

// AllowRate applies every sliding window for one bucket and records the attempt only when it
// is permitted, so refused attempts never consume budget or extend the window.
func (repository *Repository) AllowRate(ctx context.Context, kind RateBucketKind, digest []byte, now time.Time, limits []RateLimit) (RateDecision, error) {
	if len(digest) != 32 {
		return RateDecision{}, errors.New("rate bucket requires a 32-byte digest")
	}
	if len(limits) == 0 {
		return RateDecision{}, errors.New("rate bucket requires at least one limit")
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return RateDecision{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, rateBucketLock(kind, digest)); err != nil {
		return RateDecision{}, err
	}
	for _, limit := range limits {
		if limit.Limit < 1 || limit.Window <= 0 {
			return RateDecision{}, errors.New("rate limit ceiling and window must be positive")
		}
		var count int
		var oldest *time.Time
		if err := tx.QueryRow(ctx, `SELECT count(*),min(occurred_at) FROM auth_rate_events
			WHERE bucket_kind=$1 AND bucket_digest=$2 AND occurred_at > $3`,
			string(kind), digest, now.Add(-limit.Window)).Scan(&count, &oldest); err != nil {
			return RateDecision{}, err
		}
		if count < limit.Limit {
			continue
		}
		retryAfter := limit.Window
		if oldest != nil {
			retryAfter = oldest.Add(limit.Window).Sub(now)
		}
		if err := tx.Commit(ctx); err != nil {
			return RateDecision{}, err
		}
		return RateDecision{Allowed: false, RetryAfter: CoarsenRetryAfter(retryAfter)}, nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO auth_rate_events (bucket_kind,bucket_digest,occurred_at)
		VALUES ($1,$2,$3)`, string(kind), digest, now); err != nil {
		return RateDecision{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RateDecision{}, err
	}
	return RateDecision{Allowed: true}, nil
}

// PruneRateEvents removes events that can no longer influence any sliding window.
func (repository *Repository) PruneRateEvents(ctx context.Context, now time.Time) (int64, error) {
	result, err := repository.pool.Exec(ctx, `DELETE FROM auth_rate_events WHERE occurred_at <= $1`,
		now.Add(-MaxRateWindow))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// ResolveSigningKey returns the key this instance must sign with, provisioning one when the
// installation has none.
//
// It is safe to call from any number of instances starting at once and takes no lock: the
// insert is ON CONFLICT DO NOTHING against the singleton unique index, so a loser of the race
// simply reads the winner's row and adopts it. That is why 100 simultaneous first starts
// converge on one key rather than 100.
func (repository *Repository) ResolveSigningKey(ctx context.Context, supplied string,
	random io.Reader, now time.Time) (SigningKeyResolution, error) {
	stored, err := repository.signingKeyRecord(ctx)
	if err != nil {
		return SigningKeyResolution{}, err
	}
	resolution, err := ResolveSigningKey(stored, supplied, random)
	if err != nil || resolution.Provision == nil {
		return resolution, err
	}

	inserted, err := repository.insertSigningKey(ctx, resolution.Provision, now)
	if err != nil {
		return SigningKeyResolution{}, err
	}
	if inserted {
		return resolution, nil
	}

	// Another instance provisioned first. Re-decide against what it stored, so the loser
	// adopts the winner's key instead of failing or minting a second one.
	stored, err = repository.signingKeyRecord(ctx)
	if err != nil {
		return SigningKeyResolution{}, err
	}
	if stored == nil {
		return SigningKeyResolution{}, errors.New("the instance signing key disappeared during provisioning")
	}
	return ResolveSigningKey(stored, supplied, random)
}

func (repository *Repository) signingKeyRecord(ctx context.Context) (*SigningKeyRecord, error) {
	var record SigningKeyRecord
	var source string
	err := repository.pool.QueryRow(ctx,
		`SELECT source,key_material,fingerprint,generation FROM instance_signing_key`).
		Scan(&source, &record.KeyMaterial, &record.Fingerprint, &record.Generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("read the instance signing key")
	}
	record.Source = SigningKeySource(source)
	return &record, nil
}

// insertSigningKey reports whether this caller was the one that created the key.
func (repository *Repository) insertSigningKey(ctx context.Context, record *SigningKeyRecord,
	now time.Time) (bool, error) {
	id, err := newAuthUUID()
	if err != nil {
		return false, err
	}
	result, err := repository.pool.Exec(ctx, `INSERT INTO instance_signing_key
		(id,source,key_material,fingerprint,generation,created_at)
		VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`,
		id, string(record.Source), record.KeyMaterial, record.Fingerprint, record.Generation, now.UTC())
	if err != nil {
		return false, errors.New("provision the instance signing key")
	}
	return result.RowsAffected() == 1, nil
}

// RotateSigningKey replaces the instance signing key and, in the same transaction, ends
// everything whose digest was computed under the old one.
func (repository *Repository) RotateSigningKey(ctx context.Context, newKey []byte,
	now time.Time) (SigningKeyRecord, error) {
	if len(newKey) < 32 {
		return SigningKeyRecord{}, errors.New("a replacement signing key must contain at least 32 bytes")
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return SigningKeyRecord{}, errors.New("begin instance signing key rotation")
	}
	defer tx.Rollback(ctx)

	var generation int
	err = tx.QueryRow(ctx,
		`SELECT generation FROM instance_signing_key FOR UPDATE`).Scan(&generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return SigningKeyRecord{}, errors.New("this installation has no signing key to rotate")
	}
	if err != nil {
		return SigningKeyRecord{}, errors.New("lock the instance signing key")
	}

	record := SigningKeyRecord{
		Source: SigningKeyProvisioned, KeyMaterial: newKey,
		Fingerprint: SigningKeyFingerprint(newKey), Generation: generation + 1,
	}
	if _, err := tx.Exec(ctx, `UPDATE instance_signing_key
		SET source='provisioned',key_material=$1,fingerprint=$2,generation=$3,rotated_at=$4`,
		record.KeyMaterial, record.Fingerprint, record.Generation, now.UTC()); err != nil {
		return SigningKeyRecord{}, errors.New("replace the instance signing key")
	}

	// Every row below carries a digest computed under the key just replaced, so none of them
	// can ever be verified again. Clearing them here is what makes the rotation leave the
	// installation on exactly one usable key rather than between two.
	if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at=$1,revoked_reason=$2
		WHERE revoked_at IS NULL`, now.UTC(), string(RevokeSigningKeyRotated)); err != nil {
		return SigningKeyRecord{}, errors.New("end sessions for the instance signing key rotation")
	}
	if _, err := tx.Exec(ctx, `UPDATE auth_capabilities SET revoked_at=$1
		WHERE consumed_at IS NULL AND revoked_at IS NULL`, now.UTC()); err != nil {
		return SigningKeyRecord{}, errors.New("revoke capabilities for the instance signing key rotation")
	}
	if _, err := tx.Exec(ctx, `UPDATE invitations SET state='revoked',revoked_at=$1,updated_at=$1
		WHERE state='pending'`, now.UTC()); err != nil {
		return SigningKeyRecord{}, errors.New("revoke invitations for the instance signing key rotation")
	}
	if _, err := tx.Exec(ctx, `UPDATE member_login_challenges SET state='revoked',invalidated_at=$1
		WHERE state='active'`, now.UTC()); err != nil {
		return SigningKeyRecord{}, errors.New("revoke login codes for the instance signing key rotation")
	}
	// Rate buckets are keyed by a digest under the old key, so they would neither match nor
	// expire. Removing them does not unlock anybody: member lockouts live in
	// member_login_state, which rotation deliberately leaves alone.
	if _, err := tx.Exec(ctx, `DELETE FROM auth_rate_events`); err != nil {
		return SigningKeyRecord{}, errors.New("clear rate buckets for the instance signing key rotation")
	}

	metadata, err := json.Marshal(map[string]any{"generation": record.Generation})
	if err != nil {
		return SigningKeyRecord{}, errors.New("describe the instance signing key rotation")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO security_audit_events
		(occurred_at,event_type,outcome,metadata)
		VALUES ($1,'signing_key.rotated.v1','succeeded',$2)`, now.UTC(), metadata); err != nil {
		return SigningKeyRecord{}, errors.New("audit the instance signing key rotation")
	}
	if err := clientevents.Insert(ctx, tx, clientevents.Event{
		Type: "signing_key.rotated.v1", Version: 1, Scope: "owner",
		EntityType: "instance", EntityID: "signing-key", Payload: metadata, OccurredAt: now.UTC(),
	}); err != nil {
		return SigningKeyRecord{}, errors.New("publish the instance signing key rotation")
	}
	// Every account whose sessions just ended learns why through the event it already handles.
	rows, err := tx.Query(ctx, `SELECT DISTINCT user_id::text FROM sessions
		WHERE revoked_at=$1 AND revoked_reason=$2`, now.UTC(), string(RevokeSigningKeyRotated))
	if err != nil {
		return SigningKeyRecord{}, errors.New("list accounts affected by the instance signing key rotation")
	}
	affected := make([]string, 0, 8)
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			rows.Close()
			return SigningKeyRecord{}, errors.New("scan accounts affected by the instance signing key rotation")
		}
		affected = append(affected, userID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return SigningKeyRecord{}, errors.New("list accounts affected by the instance signing key rotation")
	}
	for _, userID := range affected {
		if err := clientevents.Insert(ctx, tx, clientevents.Event{
			Type: "sessions.revoked.v1", Version: 1, Scope: "user", SubjectUserID: userID,
			EntityType: "sessions", EntityID: userID, OccurredAt: now.UTC(),
		}); err != nil {
			return SigningKeyRecord{}, errors.New("publish session revocation for the instance signing key rotation")
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return SigningKeyRecord{}, errors.New("commit the instance signing key rotation")
	}
	return record, nil
}
