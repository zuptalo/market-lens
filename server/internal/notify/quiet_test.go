package notify

import (
	"testing"
	"time"
)

func at(t *testing.T, zone string, value string) time.Time {
	t.Helper()
	location, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatal(err)
	}
	moment, err := time.ParseInLocation("2006-01-02 15:04", value, location)
	if err != nil {
		t.Fatal(err)
	}
	return moment.UTC()
}

// 22:00 to 07:00 is what most people mean, and it is the case a naive start<=now<end gets wrong.
func TestAWindowThatCrossesMidnightHoldsBothHalvesOfTheNight(t *testing.T) {
	window := QuietHours{StartsAt: "22:00", EndsAt: "07:00", Timezone: "Europe/Stockholm"}

	for _, moment := range []struct {
		name     string
		now      string
		released string
	}{
		{"just after it starts", "2026-09-18 22:30", "2026-09-19 07:00"},
		{"the small hours", "2026-09-19 03:00", "2026-09-19 07:00"},
		{"a minute before it ends", "2026-09-19 06:59", "2026-09-19 07:00"},
	} {
		release, err := window.Release(at(t, "Europe/Stockholm", moment.now))
		if err != nil {
			t.Fatalf("%s: %v", moment.name, err)
		}
		if want := at(t, "Europe/Stockholm", moment.released); !release.Equal(want) {
			t.Errorf("%s: released at %s, want %s", moment.name, release, want)
		}
	}

	// Outside it, nothing is held at all.
	for _, awake := range []string{"2026-09-19 07:00", "2026-09-19 12:00", "2026-09-18 21:59"} {
		now := at(t, "Europe/Stockholm", awake)
		release, err := window.Release(now)
		if err != nil {
			t.Fatal(err)
		}
		if !release.Equal(now) {
			t.Errorf("at %s a notification was held until %s", awake, release)
		}
	}
}

func TestAnOrdinaryDaytimeWindowHoldsUntilItEnds(t *testing.T) {
	window := QuietHours{StartsAt: "09:00", EndsAt: "17:00", Timezone: "Europe/Stockholm"}
	release, err := window.Release(at(t, "Europe/Stockholm", "2026-09-18 11:00"))
	if err != nil {
		t.Fatal(err)
	}
	if want := at(t, "Europe/Stockholm", "2026-09-18 17:00"); !release.Equal(want) {
		t.Errorf("released at %s, want %s", release, want)
	}
}

// The reason the zone is stored rather than an offset: the window follows the clock on the wall,
// and the same 07:00 is a different moment in UTC depending on the time of year.
func TestTheWindowFollowsTheWallClockAcrossDaylightSaving(t *testing.T) {
	window := QuietHours{StartsAt: "22:00", EndsAt: "07:00", Timezone: "Europe/Stockholm"}
	zone := mustZone(t)

	// A night entirely in summer time, and one entirely in winter. Both end at 07:00 local, and
	// that is two different hours in UTC — which is what an offset stored once would get wrong for
	// half the year.
	summer, err := window.Release(at(t, "Europe/Stockholm", "2026-08-24 23:00"))
	if err != nil {
		t.Fatal(err)
	}
	winter, err := window.Release(at(t, "Europe/Stockholm", "2026-11-24 23:00"))
	if err != nil {
		t.Fatal(err)
	}
	if summer.UTC().Hour() == winter.UTC().Hour() {
		t.Errorf("07:00 local was the same UTC hour in August and November: %s and %s",
			summer.UTC(), winter.UTC())
	}
	for name, release := range map[string]time.Time{"summer": summer, "winter": winter} {
		if local := release.In(zone); local.Hour() != 7 || local.Minute() != 0 {
			t.Errorf("the %s window ended at %s local, not 07:00", name, local)
		}
	}

	// And the night the clocks actually change. Going to sleep at 23:00 on 24 October is CEST;
	// waking at 07:00 on the 25th is CET, because the change happened at 03:00 in between. The
	// window is nine hours by the clock and ten by the sun, and the clock is what was meant.
	crossing, err := window.Release(at(t, "Europe/Stockholm", "2026-10-24 23:00"))
	if err != nil {
		t.Fatal(err)
	}
	if local := crossing.In(zone); local.Hour() != 7 {
		t.Errorf("the window across the change ended at %s local, not 07:00", local)
	}
}

func TestAnUnusableWindowIsRefused(t *testing.T) {
	for name, window := range map[string]QuietHours{
		"a zero-length window": {StartsAt: "22:00", EndsAt: "22:00", Timezone: "Europe/Stockholm"},
		"an unknown zone":      {StartsAt: "22:00", EndsAt: "07:00", Timezone: "Mars/Olympus"},
		"an unreadable start":  {StartsAt: "bedtime", EndsAt: "07:00", Timezone: "Europe/Stockholm"},
		"an unreadable end":    {StartsAt: "22:00", EndsAt: "25:99", Timezone: "Europe/Stockholm"},
	} {
		if err := window.Valid(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	good := QuietHours{StartsAt: "22:00", EndsAt: "07:00", Timezone: "Europe/Stockholm"}
	if err := good.Valid(); err != nil {
		t.Errorf("an ordinary window was refused: %v", err)
	}
}

func mustZone(t *testing.T) *time.Location {
	t.Helper()
	zone, err := time.LoadLocation("Europe/Stockholm")
	if err != nil {
		t.Fatal(err)
	}
	return zone
}
