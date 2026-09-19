package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"market-lens/server/internal/instruments"

	"github.com/jackc/pgx/v5"
)

// RaiseIn writes one notification per consenting person per channel, on the caller's transaction.
//
// On the caller's transaction deliberately: if the change that caused this rolls back, so does the
// telling. Raising afterwards would be simpler and would tell people about things that did not
// happen.
//
// One row per person, never one shared row with a list of recipients. A shared row makes
// "delivered" ambiguous the moment one channel succeeds and another fails, and it puts one person's
// delivery state where another person's query can read it.
func RaiseIn(ctx context.Context, tx pgx.Tx, raise Raise, now time.Time) (int, error) {
	if !knownKind(raise.Kind) {
		return 0, fmt.Errorf("unknown notification kind %q", raise.Kind)
	}
	if raise.SubjectKey == "" {
		return 0, fmt.Errorf("a notification needs to be about something")
	}
	detail, err := permittedDetail(raise.Kind, raise.Detail)
	if err != nil {
		return 0, err
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		return 0, fmt.Errorf("encode the detail: %w", err)
	}
	count := raise.Count
	if count < 1 {
		count = 1
	}

	// Who asked for this, right now. Somebody who opts in afterwards is not told about things that
	// happened before they asked — a notification is a consequence of consent, not a backlog.
	//
	// A deactivated account is excluded here rather than at delivery, so nothing is even written
	// for somebody who cannot sign in to read it.
	rows, err := tx.Query(ctx, `SELECT p.user_id::text, p.channel,
			COALESCE(q.starts_at::text, ''), COALESCE(q.ends_at::text, ''), COALESCE(q.timezone, '')
		FROM notification_preferences p
		JOIN users u ON u.id = p.user_id AND u.status = 'active'
		LEFT JOIN notification_quiet_hours q ON q.user_id = p.user_id
		WHERE p.kind = $1 AND p.enabled
		  AND (u.role = 'owner' OR $1 <> 'pipeline_failure')
		  AND (cardinality($2::uuid[]) = 0 OR p.user_id = ANY($2::uuid[]))`,
		string(raise.Kind), audienceIDs(raise.Audience))
	if err != nil {
		return 0, fmt.Errorf("read who asked: %w", err)
	}
	type recipient struct {
		userID  string
		channel string
		quiet   *QuietHours
	}
	var recipients []recipient
	for rows.Next() {
		var found recipient
		var startsAt, endsAt, zone string
		if err := rows.Scan(&found.userID, &found.channel, &startsAt, &endsAt, &zone); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan a recipient: %w", err)
		}
		if zone != "" {
			found.quiet = &QuietHours{StartsAt: startsAt[:5], EndsAt: endsAt[:5], Timezone: zone}
		}
		recipients = append(recipients, found)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	raised := 0
	for _, person := range recipients {
		availableAt := now
		if person.quiet != nil {
			release, err := person.quiet.Release(now)
			if err == nil {
				availableAt = release
			}
			// A window that cannot be read is treated as no window. Holding a notification forever
			// because a stored timezone became unknown would be a worse failure than sending it.
		}
		id, err := instruments.NewUUID()
		if err != nil {
			return raised, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO notifications
			(id, user_id, kind, channel, subject_key, count, detail, available_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8)`,
			id.String(), person.userID, string(raise.Kind), person.channel,
			raise.SubjectKey, count, string(encoded), availableAt); err != nil {
			return raised, fmt.Errorf("raise the notification: %w", err)
		}
		raised++
	}
	return raised, nil
}

func audienceIDs(audience []UUID) []string {
	ids := make([]string, 0, len(audience))
	for _, id := range audience {
		ids = append(ids, id.String())
	}
	return ids
}

// permittedDetail keeps a template from being handed something it must not say.
//
// The rule is a schema, not a convention: a push payload rests on a third party's server until the
// browser collects it, and an email leaves the building entirely. So what may travel is listed per
// kind, and anything else is refused here rather than caught in review of a template later.
var permittedKeys = map[Kind]map[string]bool{
	KindDecisionWaiting: {"area": true},
	KindPaperFill:       {"ticker": true, "outcome": true},
	KindPipelineFailure: {"provider": true, "stage": true},
	KindSignalChange:    {"ticker": true, "from": true, "to": true, "strategy": true},
	KindReleaseDeployed: {"version": true, "summary": true},
}

// Never, on any kind. A holding, a quantity, a valuation, a return or a balance is what somebody
// owns, and it is not sent anywhere.
var forbiddenKeys = []string{
	"value", "amount", "quantity", "price", "cost", "cash", "balance",
	"return", "profit", "loss", "holding", "total", "score",
}

func permittedDetail(kind Kind, detail map[string]string) (map[string]string, error) {
	allowed := permittedKeys[kind]
	cleaned := make(map[string]string, len(detail))
	for key, value := range detail {
		for _, forbidden := range forbiddenKeys {
			if key == forbidden {
				return nil, fmt.Errorf("a notification may not carry %q: what somebody owns is not sent anywhere", key)
			}
		}
		if !allowed[key] {
			return nil, fmt.Errorf("a %s notification may not carry %q", kind, key)
		}
		cleaned[key] = value
	}
	return cleaned, nil
}

// RaiseImportFailure tells whoever asked that market data did not arrive.
//
// Its own transaction, unlike every other raise here: the import already failed and rolled back
// whatever it was doing, so there is no caller transaction to join — and the telling is exactly the
// thing that must survive the failure.
func (s *Service) RaiseImportFailure(ctx context.Context, provider string) error {
	if s == nil || s.repository == nil {
		return errors.New("notification service is not configured")
	}
	tx, err := s.repository.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := RaiseIn(ctx, tx, Raise{
		Kind:       KindPipelineFailure,
		SubjectKey: "import",
		Count:      1,
		Detail:     map[string]string{"provider": provider, "stage": "import"},
	}, time.Now().UTC()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Survey raises the two kinds that come from shared reference data changing.
//
// These two differ from the others, and the difference is worth stating. A paper fill and an import
// failure each have one moment and one transaction to be raised in. A finding waiting to be settled,
// and a strategy changing its view, are properties of stored data that the nightly pass rewrites
// wholesale — so the honest boundary is the pass itself, and this runs at the end of it.
//
// The consequence is that decisions collapse into one telling: "four decisions are waiting" rather
// than four separate messages, which is what somebody actually wants at seven in the morning.
func (s *Service) Survey(ctx context.Context) error {
	if s == nil || s.repository == nil {
		return errors.New("notification service is not configured")
	}
	tx, err := s.repository.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Decisions only a person can make: findings this product refuses to settle by itself.
	// Awaiting a decision means the product looked again and the answer did not change: it will
	// not settle this by itself, and nobody but a person can. Exactly the predicate the Overview
	// uses, so the two never disagree about how many are waiting.
	var waiting int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM data_quality_findings
		WHERE status = 'open' AND reexamined_at IS NOT NULL`).Scan(&waiting); err != nil {
		return fmt.Errorf("count what is waiting: %w", err)
	}
	if waiting > 0 {
		if _, err := RaiseIn(ctx, tx, Raise{
			Kind:       KindDecisionWaiting,
			SubjectKey: "quality_findings",
			Count:      waiting,
			Detail:     map[string]string{"area": "quality"},
		}, time.Now().UTC()); err != nil {
			return err
		}
	}

	// Instruments whose latest stored view differs from the one before it. Compared rather than
	// remembered, so a restart cannot make the product forget it already said something.
	rows, err := tx.Query(ctx, `
		WITH ranked AS (
			SELECT s.instrument_id, s.action, s.session_date, st.name AS strategy,
			       row_number() OVER (PARTITION BY s.instrument_id ORDER BY s.session_date DESC) AS rank
			FROM signals s
			JOIN strategies st ON st.id = s.strategy_id
			WHERE s.action IS NOT NULL
		)
		SELECT i.ticker, latest.action, previous.action, latest.strategy
		FROM ranked latest
		JOIN ranked previous
		  ON previous.instrument_id = latest.instrument_id AND previous.rank = 2
		JOIN instruments i ON i.id = latest.instrument_id
		WHERE latest.rank = 1 AND latest.action <> previous.action
		  AND latest.session_date = (SELECT max(session_date) FROM signals)`)
	if err != nil {
		return fmt.Errorf("read what changed: %w", err)
	}
	type change struct{ ticker, to, from, strategy string }
	var changes []change
	for rows.Next() {
		var found change
		if err := rows.Scan(&found.ticker, &found.to, &found.from, &found.strategy); err != nil {
			rows.Close()
			return fmt.Errorf("scan a change: %w", err)
		}
		changes = append(changes, found)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, changed := range changes {
		if _, err := RaiseIn(ctx, tx, Raise{
			Kind:       KindSignalChange,
			SubjectKey: changed.ticker,
			Count:      1,
			Detail: map[string]string{
				"ticker": changed.ticker, "from": changed.from,
				"to": changed.to, "strategy": changed.strategy,
			},
		}, time.Now().UTC()); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
