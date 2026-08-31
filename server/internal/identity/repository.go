package identity

import (
	"context"
	"errors"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/authorization"
	"market-lens/server/internal/credentials"
	clientevents "market-lens/server/internal/events"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (repository *Repository) SetupRequired(ctx context.Context) (bool, error) {
	if repository == nil || repository.pool == nil {
		return false, errors.New("identity repository is unavailable")
	}
	var required bool
	if err := repository.pool.QueryRow(ctx,
		`SELECT closed_at IS NULL FROM bootstrap_state WHERE singleton`).Scan(&required); err != nil {
		return false, err
	}
	return required, nil
}

func (repository *Repository) IssueSetupCapability(ctx context.Context, capability Capability, now time.Time) error {
	if repository == nil || repository.pool == nil {
		return errors.New("identity repository is unavailable")
	}
	if err := capability.Validate(); err != nil {
		return err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var closedAt *time.Time
	if err := tx.QueryRow(ctx, `SELECT closed_at FROM bootstrap_state WHERE singleton FOR UPDATE`).Scan(&closedAt); err != nil {
		return err
	}
	if closedAt != nil {
		return ErrSetupClosed
	}
	if _, err := tx.Exec(ctx, `UPDATE auth_capabilities
		SET revoked_at=$1
		WHERE kind='owner_setup' AND consumed_at IS NULL AND revoked_at IS NULL`, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO auth_capabilities
		(id,kind,user_id,token_digest,expires_at,consumed_at,revoked_at,created_at)
		VALUES ($1,$2,NULL,$3,$4,NULL,NULL,$5)`,
		capability.ID, capability.Kind, capability.TokenDigest, capability.ExpiresAt, capability.CreatedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type CompleteBootstrapParams struct {
	CapabilityDigest    []byte
	User                User
	Credential          OwnerCredential
	Session             auth.Session
	Audit               SecurityAuditEvent
	ExternalCredentials []credentials.StoredCredential
	Now                 time.Time
}

func (repository *Repository) CompleteBootstrap(ctx context.Context, params CompleteBootstrapParams) error {
	if repository == nil || repository.pool == nil {
		return errors.New("identity repository is unavailable")
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var closedAt *time.Time
	if err := tx.QueryRow(ctx, `SELECT closed_at FROM bootstrap_state WHERE singleton FOR UPDATE`).Scan(&closedAt); err != nil {
		return err
	}
	if closedAt != nil {
		return ErrSetupClosed
	}

	var capabilityID string
	var expiresAt time.Time
	var consumedAt, revokedAt *time.Time
	err = tx.QueryRow(ctx, `SELECT id::text,expires_at,consumed_at,revoked_at
		FROM auth_capabilities
		WHERE kind='owner_setup' AND token_digest=$1
		FOR UPDATE`, params.CapabilityDigest).Scan(&capabilityID, &expiresAt, &consumedAt, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCapabilityUnavailable
	}
	if err != nil {
		return err
	}
	if consumedAt != nil || revokedAt != nil || !params.Now.Before(expiresAt) {
		return ErrCapabilityUnavailable
	}

	if _, err := tx.Exec(ctx, `INSERT INTO users
		(id,email,normalized_email,display_name,role,status,email_verified_at,created_at,updated_at,deactivated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NULL)`,
		params.User.ID, params.User.Email, params.User.NormalizedEmail, params.User.DisplayName,
		params.User.Role, params.User.Status, params.User.EmailVerifiedAt, params.User.CreatedAt, params.User.UpdatedAt); err != nil {
		return classifyBootstrapError(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO owner_credentials
		(user_id,password_hash,changed_at,created_at) VALUES ($1,$2,$3,$4)`,
		params.Credential.UserID, params.Credential.PasswordHash, params.Credential.ChangedAt, params.Credential.CreatedAt); err != nil {
		return err
	}
	if len(params.ExternalCredentials) != 2 {
		return errors.New("external credential set is incomplete")
	}
	for _, externalCredential := range params.ExternalCredentials {
		if err := credentials.Insert(ctx, tx, externalCredential); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO sessions
		(id,user_id,token_digest,csrf_digest,created_at,last_seen_at,idle_expires_at,absolute_expires_at,
		revoked_at,revoked_reason,device_label,origin_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULL,NULL,$9,$10)`,
		params.Session.ID, params.Session.UserID, params.Session.TokenDigest, params.Session.CSRFDigest,
		params.Session.CreatedAt, params.Session.LastSeenAt, params.Session.IdleExpiresAt,
		params.Session.AbsoluteExpiresAt, params.Session.DeviceLabel, params.Session.OriginDigest); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `UPDATE auth_capabilities SET consumed_at=$1
		WHERE id=$2 AND consumed_at IS NULL AND revoked_at IS NULL`, params.Now, capabilityID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrCapabilityUnavailable
	}
	if _, err := tx.Exec(ctx, `UPDATE bootstrap_state SET closed_at=$1,owner_user_id=$2
		WHERE singleton AND closed_at IS NULL`, params.Now, params.User.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO security_audit_events
		(occurred_at,event_type,actor_user_id,subject_user_id,session_id,outcome,origin_digest,metadata)
		VALUES ($1,$2,NULL,$3,$4,$5,$6,$7::jsonb)`,
		params.Audit.OccurredAt, params.Audit.EventType, nullableID(params.Audit.SubjectUserID),
		nullableID(params.Audit.SessionID), params.Audit.Outcome, nullableBytes(params.Audit.OriginDigest),
		string(params.Audit.Metadata)); err != nil {
		return err
	}
	if err := clientevents.Insert(ctx, tx, clientevents.Event{
		Type: "account.changed.v1", Version: 1, Scope: "user", SubjectUserID: params.User.ID,
		EntityType: "account", EntityID: params.User.ID, OccurredAt: params.Now,
	}); err != nil {
		return err
	}
	if err := clientevents.Insert(ctx, tx, clientevents.Event{
		Type: "setup.changed.v1", Version: 1, Scope: "owner",
		EntityType: "setup", EntityID: params.User.ID, OccurredAt: params.Now,
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return classifyBootstrapError(err)
	}
	return nil
}

// isUniqueViolation reports a uniqueness or serialization conflict, which for invitations and
// member creation always means another writer won the same race.
func isUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && (postgresError.Code == "23505" || postgresError.Code == "40001")
}

func classifyBootstrapError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && (postgresError.Code == "23505" || postgresError.Code == "40001") {
		return ErrSetupClosed
	}
	return err
}

func nullableID(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

// IsOwner re-reads the durable role so administration never trusts a client-supplied claim.
func (repository *Repository) IsOwner(ctx context.Context, userID string) (bool, error) {
	if !validUUID(userID) {
		return false, nil
	}
	var owner bool
	err := repository.pool.QueryRow(ctx, `SELECT role='owner' AND status='active' AND email_verified_at IS NOT NULL
		FROM users WHERE id=$1`, userID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return owner, err
}

// memberColumns selects account and security metadata only. Private financial activity is
// deliberately absent so owner administration cannot expose another user's research.
const memberColumns = `u.id::text,u.email,u.display_name,u.status,u.created_at,
	s.blocked_until,s.administratively_locked_at,
	(SELECT count(*) FROM sessions x WHERE x.user_id=u.id AND x.revoked_at IS NULL
		AND x.idle_expires_at>$1 AND x.absolute_expires_at>$1)`

// scanMember reads one member row and derives its owner-visible login presentation.
func scanMember(rows interface{ Scan(...any) error }, now time.Time) (Member, error) {
	var member Member
	var status string
	var activeSessions int
	if err := rows.Scan(&member.ID, &member.Email, &member.DisplayName, &status, &member.CreatedAt,
		&member.BlockedUntil, &member.LockedAt, &activeSessions); err != nil {
		return Member{}, err
	}
	member.Status = Status(status)
	member.ActiveSessionCount = activeSessions
	member.LoginState = MemberLoginStateFor(member.BlockedUntil, member.LockedAt, now)
	return member, nil
}

// ListMembers returns one cursor-ordered page of members, excluding the owner.
// requireOwnerScope is the persistence-layer half of owner administration. The service checks
// the durable owner record; this refuses the query outright if a caller ever reaches it without
// owner authority, so a forgotten service check cannot become a disclosure.
func requireOwnerScope(scope Actor) error {
	if err := authorization.Require(authorization.Principal{
		UserID: scope.UserID, Role: authorization.Role(scope.Role), Authenticated: scope.UserID != "",
	}, authorization.Resource{Scope: authorization.ScopeOwner}); err != nil {
		return ErrOwnerRequired
	}
	return nil
}

func (repository *Repository) ListMembers(ctx context.Context, scope Actor, cursor string, limit int, now time.Time) (MemberPage, error) {
	if err := requireOwnerScope(scope); err != nil {
		return MemberPage{}, err
	}
	if cursor != "" && !validUUID(cursor) {
		return MemberPage{}, errors.New("member cursor is invalid")
	}
	rows, err := repository.pool.Query(ctx, `SELECT `+memberColumns+`
		FROM users u LEFT JOIN member_login_state s ON s.user_id=u.id
		WHERE u.role='member' AND ($2='' OR u.id>$2::uuid)
		ORDER BY u.id LIMIT $3`, now, cursor, limit+1)
	if err != nil {
		return MemberPage{}, err
	}
	defer rows.Close()
	page := MemberPage{Members: []Member{}}
	for rows.Next() {
		member, err := scanMember(rows, now)
		if err != nil {
			return MemberPage{}, err
		}
		page.Members = append(page.Members, member)
	}
	if err := rows.Err(); err != nil {
		return MemberPage{}, err
	}
	if len(page.Members) > limit {
		page.Members = page.Members[:limit]
		page.NextCursor = page.Members[limit-1].ID
	}
	return page, nil
}

// Member returns one member's administration metadata.
func (repository *Repository) Member(ctx context.Context, scope Actor, memberID string, now time.Time) (Member, error) {
	if err := requireOwnerScope(scope); err != nil {
		return Member{}, err
	}
	if !validUUID(memberID) {
		return Member{}, ErrMemberNotFound
	}
	row := repository.pool.QueryRow(ctx, `SELECT `+memberColumns+`
		FROM users u LEFT JOIN member_login_state s ON s.user_id=u.id
		WHERE u.role='member' AND u.id=$2`, now, memberID)
	member, err := scanMember(row, now)
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrMemberNotFound
	}
	return member, err
}

// CreateInvitationParams carries one owner-issued invitation and its pending delivery.
type CreateInvitationParams struct {
	Invitation Invitation
	DeliveryID string
	Now        time.Time
}

// AcceptInvitationParams carries one passwordless invitation acceptance.
type AcceptInvitationParams struct {
	TokenDigest  []byte
	Email        string
	DisplayName  string
	UserID       string
	Session      auth.Session
	Now          time.Time
	OriginDigest []byte
}

// invitationColumns selects owner-visible invitation state joined to its latest delivery.
const invitationColumns = `i.id::text,i.email,i.normalized_email,i.token_digest,i.state,i.expires_at,
	coalesce(i.accepted_by_user_id::text,''),i.accepted_at,i.revoked_at,i.created_by_user_id::text,
	i.created_at,i.updated_at,i.resend_count,
	coalesce(d.state,'pending'),coalesce(d.error_code,'')`

const invitationFrom = `FROM invitations i LEFT JOIN LATERAL (
		SELECT state,error_code FROM account_email_deliveries
		WHERE invitation_id=i.id ORDER BY created_at DESC LIMIT 1
	) d ON true`

func scanInvitation(row interface{ Scan(...any) error }) (Invitation, error) {
	var invitation Invitation
	var state, deliveryState, deliveryError string
	if err := row.Scan(&invitation.ID, &invitation.Email, &invitation.NormalizedEmail, &invitation.TokenDigest,
		&state, &invitation.ExpiresAt, &invitation.AcceptedByUserID, &invitation.AcceptedAt, &invitation.RevokedAt,
		&invitation.CreatedByUserID, &invitation.CreatedAt, &invitation.UpdatedAt, &invitation.ResendCount,
		&deliveryState, &deliveryError); err != nil {
		return Invitation{}, err
	}
	invitation.State = InvitationState(state)
	invitation.DeliveryState = DeliveryState(deliveryState)
	invitation.DeliveryError = deliveryError
	return invitation, nil
}

// CreateInvitation stores one pending invitation with its pending delivery, refusing any
// address that already holds a pending invitation or an existing account.
func (repository *Repository) CreateInvitation(ctx context.Context, params CreateInvitationParams) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var taken bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE normalized_email=$1)`,
		params.Invitation.NormalizedEmail).Scan(&taken); err != nil {
		return err
	}
	if taken {
		return ErrInvitationConflict
	}
	if _, err := tx.Exec(ctx, `INSERT INTO invitations
		(id,email,normalized_email,token_digest,state,expires_at,accepted_by_user_id,accepted_at,revoked_at,
		created_by_user_id,created_at,updated_at,resend_count)
		VALUES ($1,$2,$3,$4,'pending',$5,NULL,NULL,NULL,$6,$7,$7,0)`,
		params.Invitation.ID, params.Invitation.Email, params.Invitation.NormalizedEmail,
		params.Invitation.TokenDigest, params.Invitation.ExpiresAt, params.Invitation.CreatedByUserID,
		params.Invitation.CreatedAt); err != nil {
		if isUniqueViolation(err) {
			return ErrInvitationConflict
		}
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO account_email_deliveries
		(id,kind,recipient_email,invitation_id,state,attempt_count,created_at)
		VALUES ($1,'invitation',$2,$3,'pending',0,$4)`,
		params.DeliveryID, params.Invitation.Email, params.Invitation.ID, params.Now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO security_audit_events
		(occurred_at,event_type,actor_user_id,outcome,metadata)
		VALUES ($1,'member.invited.v1',$2,'succeeded','{}'::jsonb)`,
		params.Now, params.Invitation.CreatedByUserID); err != nil {
		return err
	}
	if err := clientevents.Insert(ctx, tx, clientevents.Event{
		Type: "invitation.changed.v1", Version: 1, Scope: "owner",
		EntityType: "invitation", EntityID: params.Invitation.ID, OccurredAt: params.Now,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ListInvitations returns one cursor-ordered page of owner-visible invitation state.
func (repository *Repository) ListInvitations(ctx context.Context, scope Actor, cursor string, limit int, _ time.Time) (InvitationPage, error) {
	if err := requireOwnerScope(scope); err != nil {
		return InvitationPage{}, err
	}
	if cursor != "" && !validUUID(cursor) {
		return InvitationPage{}, errors.New("invitation cursor is invalid")
	}
	rows, err := repository.pool.Query(ctx, `SELECT `+invitationColumns+` `+invitationFrom+`
		WHERE ($1='' OR i.id>$1::uuid) ORDER BY i.id LIMIT $2`, cursor, limit+1)
	if err != nil {
		return InvitationPage{}, err
	}
	defer rows.Close()
	page := InvitationPage{Items: []Invitation{}}
	for rows.Next() {
		invitation, err := scanInvitation(rows)
		if err != nil {
			return InvitationPage{}, err
		}
		page.Items = append(page.Items, invitation)
	}
	if err := rows.Err(); err != nil {
		return InvitationPage{}, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.NextCursor = page.Items[limit-1].ID
	}
	return page, nil
}

// ResendInvitation replaces the capability under a row lock so every earlier capability for
// this invitation becomes unusable atomically.
func (repository *Repository) ResendInvitation(ctx context.Context, invitationID string, tokenDigest []byte,
	deliveryID string, now time.Time) (Invitation, error) {
	if !validUUID(invitationID) {
		return Invitation{}, ErrInvitationUnavailable
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return Invitation{}, err
	}
	defer tx.Rollback(ctx)

	invitation, err := scanInvitation(tx.QueryRow(ctx, `SELECT `+invitationColumns+` `+invitationFrom+`
		WHERE i.id=$1 FOR UPDATE OF i`, invitationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, ErrInvitationUnavailable
	}
	if err != nil {
		return Invitation{}, err
	}
	if err := invitation.Resend(tokenDigest, now); err != nil {
		return Invitation{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE invitations SET token_digest=$2,expires_at=$3,resend_count=$4,updated_at=$5
		WHERE id=$1`, invitationID, invitation.TokenDigest, invitation.ExpiresAt,
		invitation.ResendCount, invitation.UpdatedAt); err != nil {
		return Invitation{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO account_email_deliveries
		(id,kind,recipient_email,invitation_id,state,attempt_count,created_at)
		VALUES ($1,'invitation',$2,$3,'pending',0,$4)`,
		deliveryID, invitation.Email, invitationID, now); err != nil {
		return Invitation{}, err
	}
	if err := clientevents.Insert(ctx, tx, clientevents.Event{
		Type: "invitation.changed.v1", Version: 1, Scope: "owner",
		EntityType: "invitation", EntityID: invitationID, OccurredAt: now,
	}); err != nil {
		return Invitation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Invitation{}, err
	}
	return invitation, nil
}

// RevokeInvitation retires a pending invitation. It is idempotent for an already retired one.
func (repository *Repository) RevokeInvitation(ctx context.Context, invitationID string, now time.Time) error {
	if !validUUID(invitationID) {
		return ErrInvitationUnavailable
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var state string
	err = tx.QueryRow(ctx, `SELECT state FROM invitations WHERE id=$1 FOR UPDATE`, invitationID).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvitationUnavailable
	}
	if err != nil {
		return err
	}
	if state != string(InvitationPending) {
		// Already retired; revocation stays idempotent rather than resurrecting anything.
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `UPDATE invitations SET state='revoked',revoked_at=$2,updated_at=$2
		WHERE id=$1 AND state='pending'`, invitationID, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE account_email_deliveries SET state='abandoned',error_code='abandoned'
		WHERE invitation_id=$1 AND state IN ('pending','sending')`, invitationID); err != nil {
		return err
	}
	if err := clientevents.Insert(ctx, tx, clientevents.Event{
		Type: "invitation.changed.v1", Version: 1, Scope: "owner",
		EntityType: "invitation", EntityID: invitationID, OccurredAt: now,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// AcceptInvitation consumes one capability and creates the member atomically. The invitation
// row is locked first, so concurrent attempts serialize and exactly one can succeed.
func (repository *Repository) AcceptInvitation(ctx context.Context, params AcceptInvitationParams) (User, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(ctx)

	invitation, err := scanInvitation(tx.QueryRow(ctx, `SELECT `+invitationColumns+` `+invitationFrom+`
		WHERE i.token_digest=$1 FOR UPDATE OF i`, params.TokenDigest))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrInvitationUnavailable
	}
	if err != nil {
		return User{}, err
	}
	_, normalized, err := NormalizeEmail(params.Email)
	if err != nil {
		return User{}, ErrInvitationUnavailable
	}
	// The capability is bound to the invited address, so it cannot onboard anyone else.
	if normalized != invitation.NormalizedEmail {
		return User{}, ErrInvitationUnavailable
	}
	if err := invitation.Accept(params.UserID, params.Now); err != nil {
		return User{}, err
	}

	verifiedAt := params.Now.UTC()
	user := User{
		ID: params.UserID, Email: invitation.Email, NormalizedEmail: invitation.NormalizedEmail,
		DisplayName: params.DisplayName, Role: RoleMember, Status: StatusActive,
		EmailVerifiedAt: &verifiedAt, CreatedAt: params.Now.UTC(), UpdatedAt: params.Now.UTC(),
	}
	if err := user.Validate(); err != nil {
		return User{}, err
	}
	// Holding the capability proves control of the mailbox, so the address is verified and no
	// password credential is ever created for a member.
	if _, err := tx.Exec(ctx, `INSERT INTO users
		(id,email,normalized_email,display_name,role,status,email_verified_at,created_at,updated_at)
		VALUES ($1,$2,$3,$4,'member','active',$5,$6,$6)`,
		user.ID, user.Email, user.NormalizedEmail, user.DisplayName, verifiedAt, params.Now); err != nil {
		if isUniqueViolation(err) {
			return User{}, ErrInvitationConflict
		}
		return User{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE invitations SET state='accepted',accepted_by_user_id=$2,accepted_at=$3,
		updated_at=$3 WHERE id=$1 AND state='pending'`, invitation.ID, params.UserID, params.Now); err != nil {
		return User{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO sessions
		(id,user_id,token_digest,csrf_digest,created_at,last_seen_at,idle_expires_at,absolute_expires_at,
		revoked_at,revoked_reason,device_label,origin_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULL,NULL,$9,$10)`,
		params.Session.ID, params.UserID, params.Session.TokenDigest, params.Session.CSRFDigest,
		params.Session.CreatedAt, params.Session.LastSeenAt, params.Session.IdleExpiresAt,
		params.Session.AbsoluteExpiresAt, params.Session.DeviceLabel, params.Session.OriginDigest); err != nil {
		return User{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO security_audit_events
		(occurred_at,event_type,subject_user_id,session_id,outcome,origin_digest,metadata)
		VALUES ($1,'member.invitation_accepted.v1',$2,$3,'succeeded',$4,'{}'::jsonb)`,
		params.Now, params.UserID, params.Session.ID, params.OriginDigest); err != nil {
		return User{}, err
	}
	for _, event := range []clientevents.Event{
		{Type: "invitation.changed.v1", Version: 1, Scope: "owner", EntityType: "invitation",
			EntityID: invitation.ID, OccurredAt: params.Now},
		{Type: "member.changed.v1", Version: 1, Scope: "owner", EntityType: "member",
			EntityID: params.UserID, OccurredAt: params.Now},
		{Type: "session.created.v1", Version: 1, Scope: "user", SubjectUserID: params.UserID,
			EntityType: "session", EntityID: params.Session.ID, OccurredAt: params.Now},
	} {
		if err := clientevents.Insert(ctx, tx, event); err != nil {
			return User{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, err
	}
	return user, nil
}

// MarkInvitationDelivery records the transactional-email outcome for an invitation without
// ever storing the capability itself.
func (repository *Repository) MarkInvitationDelivery(ctx context.Context, invitationID, deliveryID string,
	now time.Time, delivered bool) error {
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
	} else if _, err := tx.Exec(ctx, `UPDATE account_email_deliveries
		SET state='failed',attempt_count=attempt_count+1,last_attempt_at=$1,error_code='temporary_failure'
		WHERE id=$2 AND state='pending'`, now, deliveryID); err != nil {
		return err
	}
	if err := clientevents.Insert(ctx, tx, clientevents.Event{
		Type: "invitation.changed.v1", Version: 1, Scope: "owner",
		EntityType: "invitation", EntityID: invitationID, OccurredAt: now,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SetMemberStatus activates or deactivates a member. Deactivation revokes every session and
// outstanding login code so access stops immediately.
func (repository *Repository) SetMemberStatus(ctx context.Context, ownerID, memberID string,
	status Status, now time.Time) (Member, error) {
	if !validUUID(memberID) {
		return Member{}, ErrMemberNotFound
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return Member{}, err
	}
	defer tx.Rollback(ctx)

	var role, current string
	err = tx.QueryRow(ctx, `SELECT role,status FROM users WHERE id=$1 FOR UPDATE`, memberID).Scan(&role, &current)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && role != string(RoleMember)) {
		return Member{}, ErrMemberNotFound
	}
	if err != nil {
		return Member{}, err
	}
	if current != string(status) {
		if status == StatusDeactivated {
			if _, err := tx.Exec(ctx, `UPDATE users SET status='deactivated',deactivated_at=$2,updated_at=$2
				WHERE id=$1`, memberID, now); err != nil {
				return Member{}, err
			}
			if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at=$2,revoked_reason=$3
				WHERE user_id=$1 AND revoked_at IS NULL`, memberID, now, auth.RevokeUserDeactivated); err != nil {
				return Member{}, err
			}
			if _, err := tx.Exec(ctx, `UPDATE member_login_challenges SET state='revoked',invalidated_at=$2
				WHERE user_id=$1 AND state='active'`, memberID, now); err != nil {
				return Member{}, err
			}
		} else {
			// Reactivation restores access without requiring another invitation, and never
			// resurrects the sessions that deactivation revoked.
			if _, err := tx.Exec(ctx, `UPDATE users SET status='active',deactivated_at=NULL,updated_at=$2
				WHERE id=$1`, memberID, now); err != nil {
				return Member{}, err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO security_audit_events
			(occurred_at,event_type,actor_user_id,subject_user_id,outcome,metadata)
			VALUES ($1,$2,$3,$4,'succeeded','{}'::jsonb)`, now,
			memberStatusAuditType(status), ownerID, memberID); err != nil {
			return Member{}, err
		}
		if err := clientevents.Insert(ctx, tx, clientevents.Event{
			Type: "member.changed.v1", Version: 1, Scope: "owner",
			EntityType: "member", EntityID: memberID, OccurredAt: now,
		}); err != nil {
			return Member{}, err
		}
		if status == StatusDeactivated {
			// The member's own devices must learn immediately that access ended.
			if err := clientevents.Insert(ctx, tx, clientevents.Event{
				Type: "sessions.revoked.v1", Version: 1, Scope: "user", SubjectUserID: memberID,
				EntityType: "sessions", EntityID: memberID, OccurredAt: now,
			}); err != nil {
				return Member{}, err
			}
		}
	}
	member, err := scanMember(tx.QueryRow(ctx, `SELECT `+memberColumns+`
		FROM users u LEFT JOIN member_login_state s ON s.user_id=u.id
		WHERE u.role='member' AND u.id=$2`, now, memberID), now)
	if err != nil {
		return Member{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Member{}, err
	}
	return member, nil
}

func memberStatusAuditType(status Status) string {
	if status == StatusDeactivated {
		return "member.deactivated.v1"
	}
	return "member.reactivated.v1"
}
