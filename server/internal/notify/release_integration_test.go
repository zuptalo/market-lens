package notify_test

import (
	"strings"
	"testing"

	"market-lens/server/internal/notify"
)

// TestAVersionIsAnnouncedOnceHowaverOftenTheProcessStarts.
//
// Pods roll and crash loops restart; "tell me when a new version is deployed" is a claim about the
// version, not about this process having booted. Announcing per start would turn one deployment
// into a stream of identical messages, which is the fastest way to teach somebody to ignore them.
func TestAVersionIsAnnouncedOnceHoweverOftenTheProcessStarts(t *testing.T) {
	f := newFixture(t)
	f.ask(memberID, notify.KindReleaseDeployed, notify.ChannelEmail)

	announced, err := f.service().AnnounceVersion(f.ctx, "0.23.4", "fix(notify): deliver on its own clock")
	if err != nil {
		t.Fatalf("announce: %v", err)
	}
	if !announced {
		t.Fatal("the first start on a new version announced nothing")
	}
	// Three more starts on the same version.
	for restart := 0; restart < 3; restart++ {
		again, err := f.service().AnnounceVersion(f.ctx, "0.23.4", "fix(notify): deliver on its own clock")
		if err != nil {
			t.Fatal(err)
		}
		if again {
			t.Errorf("restart %d announced the version again", restart+1)
		}
	}
	if raised := f.count(`SELECT count(*) FROM notifications WHERE kind = 'release_deployed'`); raised != 1 {
		t.Errorf("%d announcements for one version", raised)
	}

	// A different version is a different deployment — including a rollback, which is worth being
	// told about for exactly the reason an upgrade is.
	if rolled, err := f.service().AnnounceVersion(f.ctx, "0.23.3", "rolled back"); err != nil || !rolled {
		t.Errorf("a rollback announced nothing: %v", err)
	}

	f.deliver()
	if f.mail.count() != 2 {
		t.Fatalf("%d emails for two deployments", f.mail.count())
	}
	message := f.mail.last()
	if !strings.Contains(message.Subject, "0.23.3") {
		t.Errorf("the subject does not name the version: %q", message.Subject)
	}
}

// The detail is the point: "a new version" on its own tells somebody nothing they can use.
func TestAnAnnouncementSaysWhatChanged(t *testing.T) {
	f := newFixture(t)
	f.ask(memberID, notify.KindReleaseDeployed, notify.ChannelEmail)
	if _, err := f.service().AnnounceVersion(f.ctx, "0.24.0",
		"feat(notify): tell people when a new version is deployed"); err != nil {
		t.Fatal(err)
	}
	f.deliver()

	message := f.mail.last()
	body := prose(message.Subject + " " + message.Text)
	if !strings.Contains(body, "0.24.0") {
		t.Errorf("the message does not say which version:\n%s", body)
	}
	if !strings.Contains(body, "tell people when a new version is deployed") {
		t.Errorf("the message does not say what changed:\n%s", body)
	}
	// It is a statement about the software, not a nudge to go and look at anything.
	for _, forbidden := range []string{"you should", "recommend", "check it out", "don't miss"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Errorf("the message says %q:\n%s", forbidden, body)
		}
	}
}

// A release with nothing recorded about it still deploys, and is still worth saying.
func TestAnAnnouncementWithNoSummaryStillSaysTheVersion(t *testing.T) {
	f := newFixture(t)
	f.ask(memberID, notify.KindReleaseDeployed, notify.ChannelEmail)
	if _, err := f.service().AnnounceVersion(f.ctx, "0.24.1", ""); err != nil {
		t.Fatal(err)
	}
	f.deliver()
	if !strings.Contains(f.mail.last().Text, "0.24.1") {
		t.Errorf("a release with no summary said nothing useful:\n%s", f.mail.last().Text)
	}
}

// A development build is not a deployment. Announcing one would mean every `go run` on a laptop
// counted as a release.
func TestADevelopmentBuildIsNotADeployment(t *testing.T) {
	f := newFixture(t)
	f.ask(memberID, notify.KindReleaseDeployed, notify.ChannelEmail)

	for _, version := range []string{"dev", "", "   "} {
		announced, err := f.service().AnnounceVersion(f.ctx, version, "whatever")
		if err != nil {
			t.Fatalf("%q: %v", version, err)
		}
		if announced {
			t.Errorf("%q was announced as a deployment", version)
		}
	}
	if raised := f.count(`SELECT count(*) FROM notifications`); raised != 0 {
		t.Errorf("%d announcements for builds that were never deployed", raised)
	}
}

// Nobody who did not ask hears about a deployment either. The rule holds for every kind.
func TestADeploymentTellsOnlyThePeopleWhoAsked(t *testing.T) {
	f := newFixture(t)
	f.ask(memberID, notify.KindReleaseDeployed, notify.ChannelEmail)
	// The owner asked for nothing.

	if _, err := f.service().AnnounceVersion(f.ctx, "0.24.2", "something"); err != nil {
		t.Fatal(err)
	}
	if told := f.count(`SELECT count(*) FROM notifications WHERE user_id = $1`,
		ownerID.String()); told != 0 {
		t.Errorf("the owner was told about a deployment without asking")
	}
	if told := f.count(`SELECT count(*) FROM notifications WHERE user_id = $1`,
		memberID.String()); told != 1 {
		t.Errorf("the person who asked was told %d times", told)
	}
}

// It is offered to everybody, not only the owner: a deployment changes the product under every
// person using it, unlike an import failure that only the owner can act on.
func TestADeploymentIsOfferedToEverybody(t *testing.T) {
	f := newFixture(t)
	for _, person := range []notify.UUID{ownerID, memberID} {
		var offered bool
		for _, preference := range f.settings(person).Preferences {
			if preference.Kind == notify.KindReleaseDeployed {
				offered = true
			}
		}
		if !offered {
			t.Errorf("%s is not offered to hear about deployments", person)
		}
	}
}
