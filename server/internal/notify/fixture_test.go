package notify_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"market-lens/server/internal/db"
	"market-lens/server/internal/mail"
	"market-lens/server/internal/notify"
	"market-lens/server/internal/notify/push"
	"market-lens/server/internal/testdb"
)

// Two people, always: this is per-person state, and a fixture with one user cannot fail an
// isolation test. One is the owner, because a kind exists that only an owner is offered.

var (
	ownerID  = notify.UUID("70000000-0027-4000-8000-000000000001")
	memberID = notify.UUID("70000000-0027-4000-8000-000000000002")
)

type fixture struct {
	t      *testing.T
	pool   *pgxpool.Pool
	ctx    context.Context
	mail   *countingMailer
	pusher *countingPusher
}

// countingMailer records what it was asked to send, and can be told to fail — which is how a
// provider outage is exercised without one.
type countingMailer struct {
	mu       sync.Mutex
	sent     []mail.Message
	failures int
	err      error
}

func (m *countingMailer) Send(_ context.Context, message mail.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failures > 0 {
		m.failures--
		return m.err
	}
	m.sent = append(m.sent, message)
	return nil
}

func (m *countingMailer) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sent)
}

func (m *countingMailer) last() mail.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sent) == 0 {
		return mail.Message{}
	}
	return m.sent[len(m.sent)-1]
}

type countingPusher struct {
	mu       sync.Mutex
	payloads [][]byte
	err      error
}

func (p *countingPusher) Send(_ context.Context, _ push.Subscription, payload []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	p.payloads = append(p.payloads, payload)
	return nil
}

func (p *countingPusher) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.payloads)
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	f := &fixture{t: t, pool: pool, ctx: ctx,
		mail: &countingMailer{}, pusher: &countingPusher{}}

	for index, id := range []notify.UUID{ownerID, memberID} {
		email := fmt.Sprintf("notify%d@example.com", index+1)
		role := "owner"
		if index == 1 {
			role = "member"
		}
		f.exec(`INSERT INTO users
			(id, email, normalized_email, display_name, role, status, email_verified_at, created_at, updated_at)
			VALUES ($1, $2, $2, $2, $3, 'active', now(), now(), now())`, id.String(), email, role)
	}
	return f
}

func (f *fixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(f.ctx, sql, args...); err != nil {
		f.t.Fatalf("fixture: %v\n%s", err, sql)
	}
}

func (f *fixture) count(sql string, args ...any) int64 {
	f.t.Helper()
	var total int64
	if err := f.pool.QueryRow(f.ctx, sql, args...).Scan(&total); err != nil {
		f.t.Fatalf("count: %v\n%s", err, sql)
	}
	return total
}

// testSigner stands in for the instance signing key. The production one is auth.Secrets.Digest,
// narrowed to this one purpose so nothing here can sign anything else with it.
type testSigner struct{}

func (testSigner) Sign(value string) []byte {
	digest := hmac.New(sha256.New, []byte("market-lens/test/unsubscribe"))
	_, _ = digest.Write([]byte(value))
	return digest.Sum(nil)
}

func (f *fixture) service() *notify.Service {
	return notify.NewService(notify.NewRepository(f.pool).WithSigner(testSigner{}),
		f.mail, f.pusher, "https://market-lens.example.com", slog.Default())
}

// ask is the ordinary path to a preference, so a test states what somebody asked for.
func (f *fixture) ask(user notify.UUID, kind notify.Kind, channel notify.Channel) {
	f.t.Helper()
	if _, err := f.service().SetPreference(f.ctx, user.String(), kind, channel, true); err != nil {
		f.t.Fatalf("ask for %s on %s: %v", kind, channel, err)
	}
}

func (f *fixture) subscribe(user notify.UUID, label string) notify.Subscription {
	f.t.Helper()
	subscription, err := f.service().Subscribe(f.ctx, user.String(), notify.SubscribeRequest{
		Endpoint: "https://push.example.invalid/" + label,
		P256DH:   "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4",
		Auth:     "BTBZMqHH6r4Tts7J_aSIgg",
		Label:    label,
	})
	if err != nil {
		f.t.Fatalf("subscribe %s: %v", label, err)
	}
	return subscription
}

// raise puts something worth telling people about into the outbox, in its own transaction — the
// same way a caller does inside the transaction that caused it.
func (f *fixture) raise(raise notify.Raise) int {
	f.t.Helper()
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(f.ctx) }()
	raised, err := notify.RaiseIn(f.ctx, tx, raise, time.Now().UTC())
	if err != nil {
		f.t.Fatalf("raise %s: %v", raise.Kind, err)
	}
	if err := tx.Commit(f.ctx); err != nil {
		f.t.Fatal(err)
	}
	return raised
}

func (f *fixture) deliver() int {
	f.t.Helper()
	delivered, err := f.service().DeliverDue(f.ctx)
	if err != nil {
		f.t.Fatalf("deliver: %v", err)
	}
	return delivered
}

func (f *fixture) settings(user notify.UUID) notify.Settings {
	f.t.Helper()
	settings, err := f.service().Settings(f.ctx, user.String())
	if err != nil {
		f.t.Fatalf("read settings: %v", err)
	}
	return settings
}

func (f *fixture) history(user notify.UUID) []notify.Record {
	f.t.Helper()
	records, err := f.service().History(f.ctx, user.String())
	if err != nil {
		f.t.Fatalf("read history: %v", err)
	}
	return records
}

// pushGone is what a browser's push service says when the browser has forgotten a subscription.
func pushGone() error { return push.ErrSubscriptionGone }

// mailMessage is the part of a sent message these tests read, so the assertions do not depend on
// the mail package's shape.
type mailMessage struct {
	subject string
	text    string
}

func toMailMessages(sent []mail.Message) []mailMessage {
	messages := make([]mailMessage, 0, len(sent))
	for _, message := range sent {
		messages = append(messages, mailMessage{subject: message.Subject, text: message.Text})
	}
	return messages
}

func nowForTest() time.Time { return time.Now().UTC() }

// prose is what a person actually reads, with opaque machine text removed.
//
// An unsubscribe token is base64 of a signature: several hundred random-looking characters that
// will contain any short word often enough to matter. Scanning it for forbidden vocabulary tests
// the entropy of HMAC, not the wording of a message.
func prose(body string) string {
	words := strings.Fields(body)
	kept := make([]string, 0, len(words))
	for _, word := range words {
		if strings.Contains(word, "://") || strings.Contains(word, "token=") {
			continue
		}
		kept = append(kept, word)
	}
	return strings.Join(kept, " ")
}

// seedFindingsAwaitingDecision puts the kind of thing this product refuses to settle by itself into
// the database, so a survey has something to count.
func (f *fixture) seedFindingsAwaitingDecision(howMany int) {
	f.t.Helper()
	f.exec(`INSERT INTO exchanges (id, mic, name, country, currency, timezone)
		VALUES (gen_random_uuid(), 'XNOT', 'Notify Exchange', 'SE', 'SEK', 'Europe/Stockholm')`)
	f.exec(`INSERT INTO import_runs (id, kind, provider, status, started_at, finished_at, app_version)
		VALUES ('70000000-0027-4000-8000-0000000000aa', 'backfill', 'fixture', 'succeeded',
		        now(), now(), 'test')`)
	for index := 0; index < howMany; index++ {
		label := strconv.Itoa(index)
		f.exec(`INSERT INTO instruments
			(id, exchange_id, isin, ticker, name, currency, country, instrument_type, active,
			 purchasability_status)
			SELECT gen_random_uuid(), e.id, 'SE000000002' || $1, 'NTF' || $1,
			       'Notify Fixture', 'SEK', 'SE', 'common_stock', true, 'unverified'
			FROM exchanges e WHERE e.mic = 'XNOT'`, label)
		// Re-examined and still open: the product looked again and will not settle it by itself.
		f.exec(`INSERT INTO data_quality_findings
			(id, instrument_id, session_date, run_id, rule, severity, disposition, detail,
			 status, created_at, reexamined_at, reexamining_run_id)
			SELECT gen_random_uuid(), i.id, current_date - $2::int,
			       '70000000-0027-4000-8000-0000000000aa', 'provider_gap', 'warning', 'flagged',
			       'the source has no bar for this session', 'open', now(), now(),
			       '70000000-0027-4000-8000-0000000000aa'
			FROM instruments i WHERE i.ticker = 'NTF' || $1`, label, index)
	}
}
