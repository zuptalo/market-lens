package notify

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Telling people that a new version is running.
//
// The claim is about the version, not about this process having booted. Pods roll and crash loops
// restart, so announcing per start would turn one deployment into a stream of identical messages —
// the fastest way to teach somebody to ignore them.
//
// Whichever process records the version first is the one that announces it, decided by the primary
// key rather than by coordination: `INSERT ... ON CONFLICT DO NOTHING` returns one row to exactly
// one caller, however many start at once.

// developmentVersion is what a binary built outside the release pipeline calls itself. It is not a
// deployment, and announcing one would make every `go run` on a laptop a release.
const developmentVersion = "dev"

// AnnounceVersion records this version and, if it had not been seen before, tells whoever asked.
//
// It returns whether it announced, so a caller can log the difference between "a new version
// started" and "the same version started again", which are different operational events.
func (s *Service) AnnounceVersion(ctx context.Context, version, summary string) (bool, error) {
	if s == nil || s.repository == nil {
		return false, errors.New("notification service is not configured")
	}
	version = strings.TrimSpace(version)
	if version == "" || version == developmentVersion {
		return false, nil
	}

	tx, err := s.repository.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `INSERT INTO deployed_versions (version, summary)
		VALUES ($1, $2) ON CONFLICT DO NOTHING`, version, strings.TrimSpace(summary))
	if err != nil {
		return false, fmt.Errorf("record the version: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Another process got there first, or this one has restarted. Either way it has been said.
		return false, nil
	}

	if _, err := RaiseIn(ctx, tx, Raise{
		Kind:       KindReleaseDeployed,
		SubjectKey: version,
		Count:      1,
		Detail:     map[string]string{"version": version, "summary": strings.TrimSpace(summary)},
	}, time.Now().UTC()); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit: %w", err)
	}
	return true, nil
}
