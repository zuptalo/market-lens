package notify_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"market-lens/server/internal/notify"
)

func (f *fixture) raiseFallback(entered bool, instruments int, since string) int {
	f.t.Helper()
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(f.ctx) }()
	raised, err := notify.RaiseFallback(f.ctx, tx, entered, instruments, since, time.Now().UTC())
	if err != nil {
		f.t.Fatalf("raise the fallback notice: %v", err)
	}
	if err := tx.Commit(f.ctx); err != nil {
		f.t.Fatal(err)
	}
	return raised
}

// Feature 030. Only the owner can renew a subscription, so only the owner is offered this — and a
// member is not told even if a preference row somehow existed.
func TestTheFallbackNoticeIsTheOwnersAlone(t *testing.T) {
	f := newFixture(t)
	if !notify.OwnerOnlyKinds[notify.KindMarketDataFallback] {
		t.Fatal("the fallback notice is offered to members")
	}
	for _, preference := range f.settings(memberID).Preferences {
		if preference.Kind == notify.KindMarketDataFallback {
			t.Errorf("a member is offered %s", preference.Kind)
		}
	}
	f.ask(ownerID, notify.KindMarketDataFallback, notify.ChannelEmail)
	f.exec(`INSERT INTO notification_preferences (user_id, kind, channel, enabled)
		VALUES ($1, 'market_data_fallback', 'email', true)`, memberID.String())

	if raised := f.raiseFallback(true, 100, "2026-09-29"); raised != 1 {
		t.Errorf("the notice was raised %d times, want once for the owner", raised)
	}
	if rows := f.count(`SELECT count(*) FROM notifications WHERE user_id = $1`, memberID.String()); rows != 0 {
		t.Error("a member was told about the fallback")
	}
}

// Nobody who did not ask is told. Consent defaults to silence, as for every kind.
func TestTheFallbackNoticeWaitsToBeAskedFor(t *testing.T) {
	f := newFixture(t)
	if raised := f.raiseFallback(true, 100, "2026-09-29"); raised != 0 {
		t.Errorf("the notice reached %d people who never asked", raised)
	}
}

// It says which way the state moved, that paper fills are paused while it lasts, and what the owner
// can do — and names no instrument and no figure, on either channel.
func TestTheFallbackNoticeSaysWhatHappened(t *testing.T) {
	f := newFixture(t)
	f.ask(ownerID, notify.KindMarketDataFallback, notify.ChannelEmail)
	f.ask(ownerID, notify.KindMarketDataFallback, notify.ChannelWebPush)
	f.subscribe(ownerID, "phone")

	f.raiseFallback(true, 100, "2026-09-29")
	f.raiseFallback(false, 0, "2026-10-02")
	f.deliver()

	f.mail.mu.Lock()
	messages := toMailMessages(f.mail.sent)
	f.mail.mu.Unlock()
	if len(messages) != 2 {
		t.Fatalf("%d emails, want one for each change", len(messages))
	}
	entered, ended := messages[0], messages[1]
	if !strings.Contains(strings.ToLower(entered.subject), "fallback") ||
		!strings.Contains(entered.text, "paper") || !strings.Contains(entered.text, "subscription") {
		t.Errorf("the entering notice reads:\n%s\n%s", entered.subject, entered.text)
	}
	if !strings.Contains(strings.ToLower(ended.subject), "back") {
		t.Errorf("the ending notice reads:\n%s\n%s", ended.subject, ended.text)
	}

	f.pusher.mu.Lock()
	payloads := append([][]byte(nil), f.pusher.payloads...)
	f.pusher.mu.Unlock()
	if len(payloads) != 2 {
		t.Fatalf("%d pushes, want two", len(payloads))
	}
	var first map[string]any
	if err := json.Unmarshal(payloads[0], &first); err != nil {
		t.Fatal(err)
	}
	if first["kind"] != string(notify.KindMarketDataFallback) || first["path"] == "" {
		t.Errorf("the push reads %s", payloads[0])
	}
	// A push is built without the detail (feature 027), so it cannot say which way the state
	// moved; it says the source changed and where to look, and the email says which way.
	if strings.Contains(strings.ToLower(string(payloads[0])), "entered") {
		t.Errorf("the push carries the detail: %s", payloads[0])
	}
}

// One telling per change: the same change raised twice says nothing the second time.
func TestAFallbackChangeIsToldOnce(t *testing.T) {
	f := newFixture(t)
	f.ask(ownerID, notify.KindMarketDataFallback, notify.ChannelEmail)
	f.raiseFallback(true, 100, "2026-09-29")
	if again := f.raiseFallback(true, 100, "2026-09-29"); again != 0 {
		t.Errorf("the same change was raised again for %d people", again)
	}
}
