package notify

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// available says which kinds this person is offered.
//
// pipeline_failure is absent for anybody but the owner rather than present and refused: a switch
// that cannot be switched is a worse answer than no switch, and it invites the question of why.
func available(kinds []Kind, isOwner bool) []Kind {
	offered := make([]Kind, 0, len(kinds))
	for _, kind := range kinds {
		if OwnerOnlyKinds[kind] && !isOwner {
			continue
		}
		offered = append(offered, kind)
	}
	return offered
}

// Settings is everything a person has said about being told things.
func (s *Service) Settings(ctx context.Context, userID string) (Settings, error) {
	if err := s.ready(userID); err != nil {
		return Settings{}, err
	}
	isOwner, err := s.repository.isOwner(ctx, userID)
	if err != nil {
		return Settings{}, err
	}
	enabled, err := s.repository.enabledPreferences(ctx, userID)
	if err != nil {
		return Settings{}, err
	}
	hours, err := s.repository.quietHours(ctx, userID)
	if err != nil {
		return Settings{}, err
	}

	// Every switch this person is offered, present whether or not a row exists for it. A missing
	// row and a disabled one mean the same thing; showing only the rows that exist would make the
	// interface depend on whether somebody had ever touched a switch.
	settings := Settings{NothingIsOnByDefault: true, QuietHours: hours}
	for _, kind := range available(Kinds, isOwner) {
		for _, channel := range Channels {
			settings.Preferences = append(settings.Preferences, Preference{
				Kind: kind, Channel: channel, Enabled: enabled[preferenceKey{kind, channel}],
			})
		}
	}
	return settings, nil
}

// SetPreference turns one kind on or off for one channel.
func (s *Service) SetPreference(ctx context.Context, userID string, kind Kind, channel Channel,
	enabled bool) (Settings, error) {
	if err := s.ready(userID); err != nil {
		return Settings{}, err
	}
	if !knownKind(kind) {
		return Settings{}, Refusal{Code: RefusalUnknownKind,
			Message: "This product does not send that kind of notification."}
	}
	if channel != ChannelEmail && channel != ChannelWebPush {
		return Settings{}, Refusal{Code: RefusalUnknownChannel,
			Message: "Notifications go by email or by push, and nowhere else."}
	}
	isOwner, err := s.repository.isOwner(ctx, userID)
	if err != nil {
		return Settings{}, err
	}
	if OwnerOnlyKinds[kind] && !isOwner {
		return Settings{}, Refusal{Code: RefusalNotAvailable,
			Message: "That kind is only offered to the owner, because nobody else can act on it."}
	}
	if err := s.repository.setPreference(ctx, userID, kind, channel, enabled); err != nil {
		return Settings{}, err
	}
	return s.Settings(ctx, userID)
}

// SetQuietHours sets or clears the window during which nothing arrives.
func (s *Service) SetQuietHours(ctx context.Context, userID string, hours *QuietHours) (Settings, error) {
	if err := s.ready(userID); err != nil {
		return Settings{}, err
	}
	if hours != nil {
		if err := hours.Valid(); err != nil {
			code := RefusalInvalidQuiet
			if _, zoneErr := time.LoadLocation(hours.Timezone); zoneErr != nil {
				code = RefusalUnknownTimezone
			}
			return Settings{}, Refusal{Code: code, Message: capitalise(err.Error()) + "."}
		}
	}
	if err := s.repository.setQuietHours(ctx, userID, hours); err != nil {
		return Settings{}, err
	}
	return s.Settings(ctx, userID)
}

func knownKind(kind Kind) bool {
	for _, known := range Kinds {
		if known == kind {
			return true
		}
	}
	return false
}

func capitalise(value string) string {
	if value == "" {
		return value
	}
	return string(value[0]-32*boolToByte(value[0] >= 'a' && value[0] <= 'z')) + value[1:]
}

func boolToByte(value bool) byte {
	if value {
		return 1
	}
	return 0
}

type preferenceKey struct {
	kind    Kind
	channel Channel
}

func (r *Repository) isOwner(ctx context.Context, userID string) (bool, error) {
	if err := r.ready(); err != nil {
		return false, err
	}
	var role string
	err := r.pool.QueryRow(ctx, `SELECT role FROM users WHERE id = $1 AND status = 'active'`,
		userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, fmt.Errorf("read the account: %w", err)
	}
	return role == "owner", nil
}

func (r *Repository) enabledPreferences(ctx context.Context, userID string) (map[preferenceKey]bool, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT kind, channel, enabled
		FROM notification_preferences WHERE user_id = $1`, userID)
	if err != nil {
		return nil, fmt.Errorf("read the preferences: %w", err)
	}
	defer rows.Close()

	enabled := map[preferenceKey]bool{}
	for rows.Next() {
		var key preferenceKey
		var on bool
		if err := rows.Scan(&key.kind, &key.channel, &on); err != nil {
			return nil, fmt.Errorf("scan a preference: %w", err)
		}
		enabled[key] = on
	}
	return enabled, rows.Err()
}

func (r *Repository) setPreference(ctx context.Context, userID string, kind Kind, channel Channel,
	enabled bool) error {
	if err := r.ready(); err != nil {
		return err
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO notification_preferences (user_id, kind, channel, enabled)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, kind, channel) DO UPDATE
		SET enabled = EXCLUDED.enabled, updated_at = now()`,
		userID, string(kind), string(channel), enabled)
	if err != nil {
		return fmt.Errorf("store the preference: %w", err)
	}
	return nil
}

func (r *Repository) quietHours(ctx context.Context, userID string) (*QuietHours, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	var hours QuietHours
	err := r.pool.QueryRow(ctx, `SELECT to_char(starts_at, 'HH24:MI'), to_char(ends_at, 'HH24:MI'), timezone
		FROM notification_quiet_hours WHERE user_id = $1`, userID).
		Scan(&hours.StartsAt, &hours.EndsAt, &hours.Timezone)
	if errors.Is(err, pgx.ErrNoRows) {
		// Absent means no quiet hours, which is the default. A default window would be a decision
		// about somebody's sleep.
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the quiet hours: %w", err)
	}
	return &hours, nil
}

func (r *Repository) setQuietHours(ctx context.Context, userID string, hours *QuietHours) error {
	if err := r.ready(); err != nil {
		return err
	}
	if hours == nil {
		if _, err := r.pool.Exec(ctx,
			`DELETE FROM notification_quiet_hours WHERE user_id = $1`, userID); err != nil {
			return fmt.Errorf("clear the quiet hours: %w", err)
		}
		return nil
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO notification_quiet_hours
		(user_id, starts_at, ends_at, timezone)
		VALUES ($1, $2::time, $3::time, $4)
		ON CONFLICT (user_id) DO UPDATE SET starts_at = EXCLUDED.starts_at,
			ends_at = EXCLUDED.ends_at, timezone = EXCLUDED.timezone, updated_at = now()`,
		userID, hours.StartsAt, hours.EndsAt, hours.Timezone)
	if err != nil {
		return fmt.Errorf("store the quiet hours: %w", err)
	}
	return nil
}
