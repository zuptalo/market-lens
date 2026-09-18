package notify_test

import (
	"testing"

	"market-lens/server/internal/notify"
)

// TestNothingIsSentToSomebodyWhoDidNotAskForIt is the property this whole feature depends on being
// true, and the one that would be most expensive to get wrong.
//
// A product that mails somebody unasked has made a decision on their behalf. Everything else here —
// the channels, the quiet hours, the retries — is arrangement; this is the rule.
func TestNothingIsSentToSomebodyWhoDidNotAskForIt(t *testing.T) {
	f := newFixture(t)
	// Two people who have asked for nothing, and one of everything worth telling them about.
	for _, kind := range notify.Kinds {
		f.raise(notify.Raise{Kind: kind, SubjectKey: "something", Count: 1})
	}

	if delivered := f.deliver(); delivered != 0 {
		t.Errorf("the pass delivered %d notifications to people who asked for nothing", delivered)
	}
	if f.mail.count() != 0 {
		t.Errorf("%d emails were sent to somebody who did not ask", f.mail.count())
	}
	if f.pusher.count() != 0 {
		t.Errorf("%d pushes were sent to somebody who did not ask", f.pusher.count())
	}
	// And nothing was even written down: a row would eventually become a message.
	if rows := f.count(`SELECT count(*) FROM notifications`); rows != 0 {
		t.Errorf("%d notifications exist for people who asked for nothing", rows)
	}

	// The settings a new account reads: every switch present, every switch off.
	settings := f.settings(memberID)
	if !settings.NothingIsOnByDefault {
		t.Errorf("the settings do not say that nothing is on by default")
	}
	for _, preference := range settings.Preferences {
		if preference.Enabled {
			t.Errorf("%s on %s was on before anybody asked", preference.Kind, preference.Channel)
		}
	}
}
