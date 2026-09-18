package notify_test

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"

	"market-lens/server/internal/notify"
)

// TestNoMessageCarriesWhatSomebodyOwns. SC-005.
//
// A push payload rests on a third party's server until the browser collects it, and an email leaves
// the building entirely. The threat is not interception, it is accumulation — so what may travel is
// asserted across every template rather than reviewed per message.
func TestNoMessageCarriesWhatSomebodyOwns(t *testing.T) {
	f := newFixture(t)
	// The owner, because one kind is offered to nobody else, so this covers all four.
	for _, kind := range notify.Kinds {
		f.ask(ownerID, kind, notify.ChannelEmail)
		f.ask(ownerID, kind, notify.ChannelWebPush)
	}
	f.subscribe(ownerID, "phone")

	details := map[notify.Kind]map[string]string{
		notify.KindDecisionWaiting: {"area": "quality"},
		notify.KindPaperFill:       {"ticker": "ABB", "outcome": "filled"},
		notify.KindPipelineFailure: {"provider": "eodhd", "stage": "import"},
		notify.KindSignalChange:    {"ticker": "NOKIA", "from": "hold", "to": "buy", "strategy": "momentum_trend"},
	}
	for kind, detail := range details {
		f.raise(notify.Raise{Kind: kind, SubjectKey: string(kind), Count: 2, Detail: detail})
	}
	f.deliver()

	// Nothing that is somebody's money, in any email. Whole words, because "against" contains
	// "gain" and a substring check would be testing English rather than the message.
	forbidden := regexp.MustCompile(`(?i)\b(sek|eur|nok|dkk|usd|balance|unrealised|realised|` +
		`profit|shares|portfolio value)\b`)
	// And no figure at all: anything that reads as an amount is what this must never carry.
	figures := regexp.MustCompile(`\d[\d,. ]{2,}`)
	f.mail.mu.Lock()
	messages := append([]mailMessage(nil), toMailMessages(f.mail.sent)...)
	f.mail.mu.Unlock()
	if len(messages) == 0 {
		t.Fatal("nothing was sent, so nothing was checked")
	}
	for _, message := range messages {
		body := prose(message.subject + " " + message.text)
		if found := forbidden.FindString(body); found != "" {
			t.Errorf("an email carries %q:\n%s", found, body)
		}
		if found := figures.FindString(body); found != "" {
			t.Errorf("an email carries a figure %q:\n%s", found, body)
		}
	}

	// A push payload carries less still: a kind, a count and a path, and no instrument at all.
	f.pusher.mu.Lock()
	payloads := append([][]byte(nil), f.pusher.payloads...)
	f.pusher.mu.Unlock()
	if len(payloads) == 0 {
		t.Fatal("nothing was pushed, so nothing was checked")
	}
	for _, raw := range payloads {
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("a push payload is not readable: %v", err)
		}
		for key := range payload {
			switch key {
			case "kind", "title", "body", "path", "count":
			default:
				t.Errorf("a push payload carries %q, which is more than a kind, a count and a path", key)
			}
		}
		text := strings.ToLower(string(raw))
		for _, word := range []string{"abb", "nokia", "momentum", "sek", "eur"} {
			if strings.Contains(text, word) {
				t.Errorf("a push payload names %q: %s", word, raw)
			}
		}
	}
}

// TestADetailNobodyShouldSeeIsRefusedAtTheSource. The schema is the guard, not the template: a
// template that never gets handed a figure cannot render one by accident later.
func TestADetailNobodyShouldSeeIsRefusedAtTheSource(t *testing.T) {
	f := newFixture(t)
	f.ask(memberID, notify.KindPaperFill, notify.ChannelEmail)

	for _, forbidden := range []map[string]string{
		{"value": "272560"},
		{"quantity": "400"},
		{"price": "600.30"},
		{"balance": "727440"},
		{"profit": "32440"},
	} {
		tx, err := f.pool.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = notify.RaiseIn(f.ctx, tx, notify.Raise{
			Kind: notify.KindPaperFill, SubjectKey: "order", Detail: forbidden,
		}, nowForTest())
		_ = tx.Rollback(f.ctx)
		if err == nil {
			t.Errorf("a notification carrying %v was accepted", forbidden)
		}
	}

	// And a key that is simply not part of this kind is refused too, so a template cannot quietly
	// start depending on something nobody reviewed.
	tx, _ := f.pool.Begin(f.ctx)
	_, err := notify.RaiseIn(f.ctx, tx, notify.Raise{
		Kind: notify.KindPaperFill, SubjectKey: "order", Detail: map[string]string{"strategy": "x"},
	}, nowForTest())
	_ = tx.Rollback(f.ctx)
	if err == nil {
		t.Errorf("a paper fill carrying a strategy was accepted")
	}
}

// TestASignalMessageStatesTheChangeAndNeverWhatToDo. FR-021.
//
// The kind that needed a second look. Feature 021 measured this strategy over ten years and found
// it lost to two of its three benchmarks; a message about its view must not read as a reason to act.
func TestASignalMessageStatesTheChangeAndNeverWhatToDo(t *testing.T) {
	f := newFixture(t)
	f.ask(memberID, notify.KindSignalChange, notify.ChannelEmail)
	f.raise(notify.Raise{
		Kind: notify.KindSignalChange, SubjectKey: "NOKIA", Count: 1,
		Detail: map[string]string{"ticker": "NOKIA", "from": "hold", "to": "buy",
			"strategy": "momentum_trend"},
	})
	f.deliver()

	message := f.mail.last()
	text := strings.ToLower(prose(message.Text))
	// It says what changed: the instrument, the two views, and whose view it is.
	for _, expected := range []string{"nokia", "hold", "buy", "momentum_trend"} {
		if !strings.Contains(text, expected) {
			t.Errorf("the message does not state %q:\n%s", expected, message.Text)
		}
	}
	// And it says what it is not.
	if !strings.Contains(text, "not advice") {
		t.Errorf("the message does not say it is not advice:\n%s", message.Text)
	}
	// No imperative, and nothing that reads as a reason to act — including in a denial, because a
	// reader skimming sees the phrase and not the "does not" in front of it.
	for _, forbidden := range []string{
		"you should", "we recommend", "consider buying", "consider selling",
		"act now", "opportunity", "take a position", "time to",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("the message says %q:\n%s", forbidden, prose(message.Text))
		}
	}
}

// TestAPersonSeesAndChangesOnlyTheirOwn. FR-018.
func TestAPersonSeesAndChangesOnlyTheirOwn(t *testing.T) {
	f := newFixture(t)
	f.ask(ownerID, notify.KindDecisionWaiting, notify.ChannelEmail)
	f.ask(memberID, notify.KindPaperFill, notify.ChannelEmail)
	ownersDevice := f.subscribe(ownerID, "owner phone")
	f.subscribe(memberID, "member phone")

	// A shared change reaches only the person who asked for that kind.
	f.raise(notify.Raise{Kind: notify.KindDecisionWaiting, SubjectKey: "findings", Count: 1})
	if rows := f.count(`SELECT count(*) FROM notifications WHERE user_id = $1`,
		memberID.String()); rows != 0 {
		t.Errorf("%d notifications were written for somebody who asked for a different kind", rows)
	}
	if rows := f.count(`SELECT count(*) FROM notifications WHERE user_id = $1`,
		ownerID.String()); rows != 1 {
		t.Errorf("%d notifications for the person who asked", rows)
	}

	// Devices are each person's own, and revoking somebody else's answers as not found.
	member, err := f.service().Subscriptions(f.ctx, memberID.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(member) != 1 || member[0].Label != "member phone" {
		t.Errorf("the member's devices are %+v", member)
	}
	if err := f.service().Revoke(f.ctx, memberID.String(), string(ownersDevice.ID)); !errors.Is(err, notify.ErrNotFound) {
		t.Errorf("revoking somebody else's device returned %v", err)
	}
	if devices := f.count(`SELECT count(*) FROM push_subscriptions WHERE user_id = $1`,
		ownerID.String()); devices != 1 {
		t.Errorf("the owner's device was revoked by somebody else")
	}

	// And history is each person's own.
	if records := f.history(memberID); len(records) != 0 {
		t.Errorf("the member reads %d notifications that are not theirs", len(records))
	}
	if _, err := f.service().Settings(f.ctx, ""); err == nil {
		t.Errorf("an unauthenticated caller read notification settings")
	}
}

// TestOneKindIsOfferedToTheOwnerAlone. FR-003: absent rather than present and refused, because a
// switch that cannot be switched invites the question of why.
func TestOneKindIsOfferedToTheOwnerAlone(t *testing.T) {
	f := newFixture(t)

	member := f.settings(memberID)
	for _, preference := range member.Preferences {
		if preference.Kind == notify.KindPipelineFailure {
			t.Errorf("a member is offered %s", preference.Kind)
		}
	}
	if len(member.Preferences) != 6 {
		t.Errorf("a member is offered %d switches, want three kinds on two channels", len(member.Preferences))
	}

	owner := f.settings(ownerID)
	if len(owner.Preferences) != 8 {
		t.Errorf("the owner is offered %d switches, want four kinds on two channels", len(owner.Preferences))
	}

	// And asking for it directly is refused rather than quietly stored.
	_, err := f.service().SetPreference(f.ctx, memberID.String(),
		notify.KindPipelineFailure, notify.ChannelEmail, true)
	var refusal notify.Refusal
	if !errors.As(err, &refusal) || refusal.Code != notify.RefusalNotAvailable {
		t.Errorf("a member asking for a pipeline failure got %v", err)
	}

	// Even if a row somehow existed, a raise does not reach them.
	f.exec(`INSERT INTO notification_preferences (user_id, kind, channel, enabled)
		VALUES ($1, 'pipeline_failure', 'email', true)`, memberID.String())
	f.raise(notify.Raise{Kind: notify.KindPipelineFailure, SubjectKey: "import", Count: 1})
	if rows := f.count(`SELECT count(*) FROM notifications WHERE user_id = $1`,
		memberID.String()); rows != 0 {
		t.Errorf("a member was told about a pipeline failure")
	}
}

// TestAnUnsubscribeLinkStopsExactlyOneThing. FR-005 and R-007.
func TestAnUnsubscribeLinkStopsExactlyOneThing(t *testing.T) {
	f := newFixture(t)
	f.ask(memberID, notify.KindDecisionWaiting, notify.ChannelEmail)
	f.ask(memberID, notify.KindDecisionWaiting, notify.ChannelWebPush)
	f.ask(memberID, notify.KindPaperFill, notify.ChannelEmail)

	f.raise(notify.Raise{Kind: notify.KindDecisionWaiting, SubjectKey: "findings", Count: 1})
	f.deliver()

	token := tokenFrom(t, f.mail.last().Text)
	kind, channel, err := f.service().Unsubscribe(f.ctx, token)
	if err != nil {
		t.Fatalf("follow the link: %v", err)
	}
	if kind != notify.KindDecisionWaiting || channel != notify.ChannelEmail {
		t.Errorf("the link stopped %s on %s", kind, channel)
	}

	// Exactly one thing: the other two are untouched.
	enabled := map[string]bool{}
	for _, preference := range f.settings(memberID).Preferences {
		enabled[string(preference.Kind)+"/"+string(preference.Channel)] = preference.Enabled
	}
	if enabled["decision_waiting/email"] {
		t.Errorf("the kind that sent the message is still on")
	}
	if !enabled["decision_waiting/web_push"] || !enabled["paper_fill/email"] {
		t.Errorf("the link changed more than one preference: %+v", enabled)
	}

	// A token that has been tampered with changes nothing.
	for _, broken := range []string{"", "nonsense", token + "x", strings.ToUpper(token)} {
		if _, _, err := f.service().Unsubscribe(f.ctx, broken); err == nil {
			t.Errorf("a broken token %q was accepted", broken)
		}
	}
}

func tokenFrom(t *testing.T, body string) string {
	t.Helper()
	const marker = "/unsubscribe?token="
	index := strings.Index(body, marker)
	if index < 0 {
		t.Fatalf("no unsubscribe link in:\n%s", body)
	}
	rest := body[index+len(marker):]
	if end := strings.IndexAny(rest, "\n "); end >= 0 {
		rest = rest[:end]
	}
	return strings.TrimSpace(rest)
}
