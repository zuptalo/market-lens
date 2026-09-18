package notify

import (
	"fmt"
	"time"
)

// When a notification raised now may actually be sent.
//
// Local wall-clock times plus an IANA zone rather than a UTC offset, because a person means "while
// I am asleep" and daylight saving moves that against UTC twice a year. Loading the zone at each
// comparison is what makes the window follow the clock on their wall.

const clockLayout = "15:04"

// parseWindow reads a stored window, or says why it cannot be used.
func (q QuietHours) parse() (start, end time.Time, zone *time.Location, err error) {
	zone, err = time.LoadLocation(q.Timezone)
	if err != nil {
		return time.Time{}, time.Time{}, nil, fmt.Errorf("unknown timezone %q", q.Timezone)
	}
	start, err = time.Parse(clockLayout, q.StartsAt)
	if err != nil {
		return time.Time{}, time.Time{}, nil, fmt.Errorf("unreadable start %q", q.StartsAt)
	}
	end, err = time.Parse(clockLayout, q.EndsAt)
	if err != nil {
		return time.Time{}, time.Time{}, nil, fmt.Errorf("unreadable end %q", q.EndsAt)
	}
	return start, end, zone, nil
}

// Valid says whether a window can be stored: readable times, a real zone, and some length.
func (q QuietHours) Valid() error {
	start, end, _, err := q.parse()
	if err != nil {
		return err
	}
	if start.Equal(end) {
		// A window of zero length would silence everything forever while looking like a setting.
		return fmt.Errorf("a quiet window that starts and ends at the same moment never ends")
	}
	return nil
}

// Release is when a notification raised at `now` may be sent.
//
// Outside the window, that is now. Inside it, that is the moment the window ends — held, never sent
// early, and never dropped. The difference between a quiet hour and a lost alert is exactly this.
func (q QuietHours) Release(now time.Time) (time.Time, error) {
	start, end, zone, err := q.parse()
	if err != nil {
		return time.Time{}, err
	}
	local := now.In(zone)
	today := func(clock time.Time) time.Time {
		return time.Date(local.Year(), local.Month(), local.Day(),
			clock.Hour(), clock.Minute(), 0, 0, zone)
	}
	startsToday, endsToday := today(start), today(end)

	if startsToday.Before(endsToday) {
		// An ordinary daytime window, 09:00 to 17:00.
		if !local.Before(startsToday) && local.Before(endsToday) {
			return endsToday.UTC(), nil
		}
		return now, nil
	}

	// The window crosses midnight — 22:00 to 07:00, which is the ordinary case and the one naive
	// comparisons get wrong. Two halves: this evening, and this morning before the window ended.
	if !local.Before(startsToday) {
		return endsToday.AddDate(0, 0, 1).UTC(), nil
	}
	if local.Before(endsToday) {
		return endsToday.UTC(), nil
	}
	return now, nil
}
