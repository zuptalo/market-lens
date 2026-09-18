package notify_test

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"market-lens/server/internal/notify"
)

// TestSomebodyWhoAskedIsToldOnce. The ordinary path, and the two properties that make it safe:
// asked for it, and told once.
func TestSomebodyWhoAskedIsToldOnce(t *testing.T) {
	f := newFixture(t)
	f.ask(memberID, notify.KindDecisionWaiting, notify.ChannelEmail)

	if raised := f.raise(notify.Raise{
		Kind: notify.KindDecisionWaiting, SubjectKey: "findings", Count: 3,
	}); raised != 1 {
		t.Fatalf("%d notifications were raised for one consenting person", raised)
	}

	if delivered := f.deliver(); delivered != 1 {
		t.Fatalf("the pass delivered %d", delivered)
	}
	if f.mail.count() != 1 {
		t.Fatalf("%d emails were sent", f.mail.count())
	}

	// Running again sends nothing: the row's state is what makes that true, not the pass being
	// careful.
	if delivered := f.deliver(); delivered != 0 {
		t.Errorf("a second pass delivered %d already-sent notifications", delivered)
	}
	if f.mail.count() != 1 {
		t.Errorf("%d emails after two passes", f.mail.count())
	}

	message := f.mail.last()
	// Every message says the person asked for it and how to stop. A message that does not is one
	// somebody reports as spam rather than unsubscribes from.
	if !strings.Contains(message.Text, "you asked") {
		t.Errorf("the message does not say the person asked for it:\n%s", message.Text)
	}
	if !strings.Contains(message.Text, "/unsubscribe?token=") {
		t.Errorf("the message carries no way to stop:\n%s", message.Text)
	}

	// And it is readable afterwards, without the payload.
	history := f.history(memberID)
	if len(history) != 1 || history[0].State != notify.StateSent || history[0].SentAt == nil {
		t.Errorf("the history reads %+v", history)
	}
}

// TestTwoPassesAtOnceSendOnceBetweenThem. SC-003. At-most-once has to hold without assuming there
// is only ever one pass, because nothing inside the process can enforce that.
func TestTwoPassesAtOnceSendOnceBetweenThem(t *testing.T) {
	f := newFixture(t)
	f.ask(memberID, notify.KindDecisionWaiting, notify.ChannelEmail)
	for index := 0; index < 12; index++ {
		f.raise(notify.Raise{Kind: notify.KindDecisionWaiting,
			SubjectKey: "finding", Count: index + 1})
	}

	var wait sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, _ = f.service().DeliverDue(f.ctx)
		}()
	}
	wait.Wait()

	if f.mail.count() != 12 {
		t.Errorf("%d emails for 12 notifications across four simultaneous passes", f.mail.count())
	}
	if sent := f.count(`SELECT count(*) FROM notifications WHERE state = 'sent'`); sent != 12 {
		t.Errorf("%d notifications are marked sent", sent)
	}
}

// TestAQuietNotificationIsHeldAndThenDelivered. SC-002. The difference between a quiet hour and a
// lost alert is that the second one never arrives.
func TestAQuietNotificationIsHeldAndThenDelivered(t *testing.T) {
	f := newFixture(t)
	f.ask(memberID, notify.KindPaperFill, notify.ChannelEmail)

	// A window covering every hour of the day but one, so "now" is inside it whenever this runs.
	if _, err := f.service().SetQuietHours(f.ctx, memberID.String(), &notify.QuietHours{
		StartsAt: "00:00", EndsAt: "23:59", Timezone: "UTC",
	}); err != nil {
		t.Fatalf("set quiet hours: %v", err)
	}
	f.raise(notify.Raise{Kind: notify.KindPaperFill, SubjectKey: "order", Count: 1})

	if delivered := f.deliver(); delivered != 0 {
		t.Fatalf("a notification raised inside quiet hours was delivered immediately")
	}
	if f.mail.count() != 0 {
		t.Errorf("%d emails were sent during quiet hours", f.mail.count())
	}
	// Held, not dropped: the row is still there, waiting, with a moment it becomes due.
	var state string
	var availableAt time.Time
	if err := f.pool.QueryRow(f.ctx,
		`SELECT state, available_at FROM notifications WHERE user_id = $1`,
		memberID.String()).Scan(&state, &availableAt); err != nil {
		t.Fatal(err)
	}
	if state != string(notify.StatePending) {
		t.Errorf("a held notification reads %s", state)
	}
	if !availableAt.After(time.Now().UTC()) {
		t.Errorf("a held notification is already due at %s", availableAt)
	}

	// When the window ends it goes, exactly once.
	f.exec(`UPDATE notifications SET available_at = now() - interval '1 minute'`)
	if delivered := f.deliver(); delivered != 1 {
		t.Errorf("the held notification was not delivered once the window ended")
	}
	if f.mail.count() != 1 {
		t.Errorf("%d emails after the window ended", f.mail.count())
	}
}

// TestAProviderOutageLosesNothing. US4. A mail server that is down for an hour must cost nothing
// but an hour.
func TestAProviderOutageLosesNothing(t *testing.T) {
	f := newFixture(t)
	f.ask(memberID, notify.KindDecisionWaiting, notify.ChannelEmail)
	f.raise(notify.Raise{Kind: notify.KindDecisionWaiting, SubjectKey: "finding", Count: 1})

	f.mail.failures = 2
	f.mail.err = errors.New("connection refused")

	if delivered := f.deliver(); delivered != 0 {
		t.Fatalf("a failing mail server reported a delivery")
	}
	// Kept, with the reason, and due again later rather than immediately.
	var state, lastError string
	var attempts int
	if err := f.pool.QueryRow(f.ctx,
		`SELECT state, attempts, COALESCE(last_error, '') FROM notifications WHERE user_id = $1`,
		memberID.String()).Scan(&state, &attempts, &lastError); err != nil {
		t.Fatal(err)
	}
	if state != string(notify.StateFailed) || attempts != 1 {
		t.Errorf("after one failure the notification reads %s with %d attempts", state, attempts)
	}
	if !strings.Contains(lastError, "connection refused") {
		t.Errorf("the reason was not kept: %q", lastError)
	}

	// The backoff is real: it is not due again straight away.
	if delivered := f.deliver(); delivered != 0 {
		t.Errorf("a failed notification was retried immediately rather than after a gap")
	}

	// When the gap passes and the server comes back, it arrives.
	f.exec(`UPDATE notifications SET available_at = now() - interval '1 minute'`)
	f.mail.failures = 0
	if delivered := f.deliver(); delivered != 1 {
		t.Errorf("the notification was not delivered once the server came back")
	}
	if f.mail.count() != 1 {
		t.Errorf("%d emails in total", f.mail.count())
	}
}

// A server that never comes back stops being retried, and says why rather than disappearing.
func TestSomethingThatCannotBeDeliveredIsAbandonedNotLost(t *testing.T) {
	f := newFixture(t)
	f.ask(memberID, notify.KindDecisionWaiting, notify.ChannelEmail)
	f.raise(notify.Raise{Kind: notify.KindDecisionWaiting, SubjectKey: "finding", Count: 1})

	f.mail.failures = 99
	f.mail.err = errors.New("no such mailbox")
	for attempt := 0; attempt < notify.MaximumAttempts; attempt++ {
		f.exec(`UPDATE notifications SET available_at = now() - interval '1 minute'`)
		f.deliver()
	}

	var state string
	var attempts int
	if err := f.pool.QueryRow(f.ctx, `SELECT state, attempts FROM notifications WHERE user_id = $1`,
		memberID.String()).Scan(&state, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != string(notify.StateAbandoned) {
		t.Errorf("after %d attempts the notification reads %s", attempts, state)
	}
	// And a person can see it happened and why.
	history := f.history(memberID)
	if len(history) != 1 || history[0].LastError == nil ||
		!strings.Contains(*history[0].LastError, "no such mailbox") {
		t.Errorf("the history does not say why nothing arrived: %+v", history)
	}
}

// TestAPushGoesToEveryDeviceAndAForgottenOneRemovesItself.
func TestAPushGoesToEveryDeviceAndAForgottenOneRemovesItself(t *testing.T) {
	f := newFixture(t)
	f.ask(memberID, notify.KindPaperFill, notify.ChannelWebPush)
	f.subscribe(memberID, "phone")
	f.subscribe(memberID, "laptop")

	f.raise(notify.Raise{Kind: notify.KindPaperFill, SubjectKey: "order", Count: 1})
	if delivered := f.deliver(); delivered != 1 {
		t.Fatalf("the push notification was not delivered")
	}
	if f.pusher.count() != 2 {
		t.Errorf("%d devices were pushed to, want both", f.pusher.count())
	}

	// A browser that forgot is telling the truth: the row goes, without anybody doing anything.
	f.pusher.err = pushGone()
	f.raise(notify.Raise{Kind: notify.KindPaperFill, SubjectKey: "order-2", Count: 1})
	f.deliver()
	if devices := f.count(`SELECT count(*) FROM push_subscriptions WHERE user_id = $1`,
		memberID.String()); devices != 0 {
		t.Errorf("%d forgotten subscriptions were kept", devices)
	}
}

// TestAskingForPushWithNoDeviceIsNotAFailure. It stays true until they subscribe one, so retrying
// it forever would fill the history with a failure nobody can fix from the outside.
func TestAskingForPushWithNoDeviceIsNotAFailure(t *testing.T) {
	f := newFixture(t)
	f.ask(memberID, notify.KindPaperFill, notify.ChannelWebPush)
	f.raise(notify.Raise{Kind: notify.KindPaperFill, SubjectKey: "order", Count: 1})

	if delivered := f.deliver(); delivered != 1 {
		t.Errorf("asking for push with no device was treated as a failure")
	}
	if f.pusher.count() != 0 {
		t.Errorf("%d pushes were sent with no device subscribed", f.pusher.count())
	}
}

// TestASurveyCollapsesDecisionsAndNamesWhatChanged.
//
// These two kinds come from shared data the nightly pass rewrites wholesale, so they are raised by
// a survey rather than inside one transaction. The behaviour that matters is that decisions
// collapse — one telling saying how many, not one telling each.
func TestASurveyCollapsesDecisionsAndNamesWhatChanged(t *testing.T) {
	f := newFixture(t)
	f.ask(memberID, notify.KindDecisionWaiting, notify.ChannelEmail)
	f.seedFindingsAwaitingDecision(4)

	if err := f.service().Survey(f.ctx); err != nil {
		t.Fatalf("survey: %v", err)
	}

	var raised, count int
	if err := f.pool.QueryRow(f.ctx,
		`SELECT count(*), COALESCE(max(count), 0) FROM notifications
		 WHERE user_id = $1 AND kind = 'decision_waiting'`, memberID.String()).
		Scan(&raised, &count); err != nil {
		t.Fatal(err)
	}
	if raised != 1 {
		t.Errorf("%d tellings for four waiting decisions, want one", raised)
	}
	if count != 4 {
		t.Errorf("the telling says %d decisions are waiting, want 4", count)
	}

	f.deliver()
	if !strings.Contains(f.mail.last().Text, "4 decisions") {
		t.Errorf("the message does not say how many are waiting:\n%s", f.mail.last().Text)
	}
}

// Nothing waiting means nothing said. Silence is the ordinary day, and a message saying "zero
// decisions are waiting" would teach somebody to ignore the next one.
func TestASurveyWithNothingWaitingSaysNothing(t *testing.T) {
	f := newFixture(t)
	f.ask(memberID, notify.KindDecisionWaiting, notify.ChannelEmail)

	if err := f.service().Survey(f.ctx); err != nil {
		t.Fatalf("survey: %v", err)
	}
	if raised := f.count(`SELECT count(*) FROM notifications`); raised != 0 {
		t.Errorf("%d tellings were raised with nothing waiting", raised)
	}
}
