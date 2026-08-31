package instruments_test

import (
	"testing"
	"time"

	"market-lens/server/internal/instruments"
)

func historyFor(t *testing.T, fixture *explorationFixture, id instruments.UUID,
	filter instruments.HistoryFilter) instruments.HistoryWindow {
	t.Helper()
	if filter.Sessions == 0 {
		filter.Sessions = 250
	}
	window, err := instruments.NewRepository(fixture.pool).History(fixture.ctx, id, filter, fixtureAsOf)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	return window
}

// The distinction this test defends is the one the whole feature rests on: a day the
// exchange was closed is not missing data, and an open session with no bar is.
func TestHistoryReportsOnlyOpenSessionsWithNoStoredBarAsMissing(t *testing.T) {
	fixture := newExplorationFixture(t)
	window := historyFor(t, fixture, fixture.gappy, instruments.HistoryFilter{Sessions: 5000})

	expected := fixture.missingSessions(t)
	if len(window.MissingSessions) != len(expected) {
		t.Fatalf("reported %d missing sessions, expected %d: %v vs %v",
			len(window.MissingSessions), len(expected), window.MissingSessions, expected)
	}
	for index, date := range expected {
		if window.MissingSessions[index] != date {
			t.Errorf("missing session %d was %s, expected %s", index, window.MissingSessions[index], date)
		}
	}

	// A Swedish public holiday inside the same range must not appear. Inferring gaps from
	// weekday arithmetic would report every one of them and train a reader to ignore the
	// warning entirely.
	closed := fixture.closedSessionInRange(t)
	for _, date := range window.MissingSessions {
		if date == closed {
			t.Errorf("the closed exchange day %s was reported as a missing session", closed)
		}
	}

	// An instrument with no interruptions must report none, so "missing" is not simply
	// always non-empty.
	unbroken := historyFor(t, fixture, fixture.long, instruments.HistoryFilter{Sessions: 5000})
	if len(unbroken.MissingSessions) != 0 {
		t.Errorf("an instrument stored without interruption reported %d missing sessions: %v",
			len(unbroken.MissingSessions), unbroken.MissingSessions)
	}
}

func TestHistoryReturnsStoredBarsAndNothingElse(t *testing.T) {
	fixture := newExplorationFixture(t)
	window := historyFor(t, fixture, fixture.gappy, instruments.HistoryFilter{Sessions: 5000})

	if len(window.Bars) != fixtureGappySessions-len(fixtureGapOffsets) {
		t.Errorf("returned %d bars, expected the %d stored ones",
			len(window.Bars), fixtureGappySessions-len(fixtureGapOffsets))
	}
	for index := 1; index < len(window.Bars); index++ {
		if window.Bars[index-1].SessionDate >= window.Bars[index].SessionDate {
			t.Fatalf("bars are not ascending by session: %s then %s",
				window.Bars[index-1].SessionDate, window.Bars[index].SessionDate)
		}
	}
	// Nothing may be invented to fill a gap. Every returned session must be one of the
	// missing ones' opposites: a session that is actually stored.
	missing := map[instruments.SessionDate]bool{}
	for _, date := range window.MissingSessions {
		missing[date] = true
	}
	for _, bar := range window.Bars {
		if missing[bar.SessionDate] {
			t.Errorf("session %s was reported both as stored and as missing", bar.SessionDate)
		}
	}

	if window.Coverage.BarCount != int64(fixtureGappySessions-len(fixtureGapOffsets)) {
		t.Errorf("coverage reported %d stored bars", window.Coverage.BarCount)
	}
	if window.SeriesBasis != instruments.SeriesProviderAdjusted {
		t.Errorf("series basis was %q; the fixture stores adjusted closes", window.SeriesBasis)
	}
	if window.Provider == nil || *window.Provider != "fixture" {
		t.Errorf("provider was %v, expected the provider of the most recent bar in range", window.Provider)
	}
	if window.ObservedAt == nil {
		t.Error("the window did not report when its most recent bar was observed")
	}
}

// Coverage describes the instrument, not the request. A person looking at three months has
// to be able to see that ten years exist behind it.
func TestHistoryCoverageIgnoresTheRequestedWindow(t *testing.T) {
	fixture := newExplorationFixture(t)
	narrow := historyFor(t, fixture, fixture.long, instruments.HistoryFilter{Sessions: 10})

	if len(narrow.Bars) != 10 {
		t.Errorf("a ten-session window returned %d bars", len(narrow.Bars))
	}
	if narrow.Coverage.BarCount != fixtureLongSessions {
		t.Errorf("coverage reported %d bars for a ten-session window; it must describe the "+
			"instrument's whole stored history", narrow.Coverage.BarCount)
	}
	wide := historyFor(t, fixture, fixture.long, instruments.HistoryFilter{Sessions: 5000})
	if narrow.Coverage.FirstSession != wide.Coverage.FirstSession ||
		narrow.Coverage.LastSession != wide.Coverage.LastSession {
		t.Errorf("coverage changed with the requested window: %v vs %v",
			narrow.Coverage, wide.Coverage)
	}
}

// Ranges are counted in stored exchange sessions, never in calendar days (research R7).
func TestHistoryCountsTheWindowInSessionsNotCalendarDays(t *testing.T) {
	fixture := newExplorationFixture(t)
	window := historyFor(t, fixture, fixture.long, instruments.HistoryFilter{Sessions: 30})

	if len(window.Bars) != 30 {
		t.Fatalf("a thirty-session window returned %d bars", len(window.Bars))
	}
	// Thirty sessions span more than thirty calendar days because of weekends, so a window
	// that really counted days would come back short.
	first, last := window.Bars[0].SessionDate, window.Bars[len(window.Bars)-1].SessionDate
	if first >= last {
		t.Fatalf("window boundaries are inverted: %s to %s", first, last)
	}
	spanDays := daysBetween(t, string(first), string(last))
	if spanDays <= 30 {
		t.Errorf("thirty sessions spanned only %d calendar days, which suggests the window "+
			"was counted in days rather than sessions", spanDays)
	}
}

// A window that begins before the instrument's first stored session starts where the data
// starts and says so, rather than padding what does not exist.
func TestHistoryClampsAWindowLongerThanTheStoredHistory(t *testing.T) {
	fixture := newExplorationFixture(t)
	window := historyFor(t, fixture, fixture.short, instruments.HistoryFilter{Sessions: 500})

	if len(window.Bars) != fixtureShortSessions {
		t.Errorf("returned %d bars for an instrument with %d stored sessions",
			len(window.Bars), fixtureShortSessions)
	}
	if window.RequestedFrom == "" || window.RequestedTo == "" {
		t.Error("the window did not state the boundaries it resolved to")
	}
	if window.RequestedFrom != window.Coverage.FirstSession {
		t.Errorf("a window longer than the stored history began at %s rather than at the "+
			"first stored session %s", window.RequestedFrom, window.Coverage.FirstSession)
	}
}

func TestHistoryForAnInstrumentWithNoStoredBars(t *testing.T) {
	fixture := newExplorationFixture(t)
	window := historyFor(t, fixture, fixture.empty, instruments.HistoryFilter{})

	if len(window.Bars) != 0 {
		t.Errorf("an instrument with no stored bars returned %d of them", len(window.Bars))
	}
	if window.Coverage.BarCount != 0 {
		t.Errorf("coverage reported %d bars", window.Coverage.BarCount)
	}
	// With no stored session there is no range, so nothing can be missing from it. Reporting
	// every open session as missing would be technically true and useless.
	if len(window.MissingSessions) != 0 {
		t.Errorf("an instrument with no history reported %d missing sessions",
			len(window.MissingSessions))
	}
}

func TestHistoryRejectsAnUnknownInstrument(t *testing.T) {
	fixture := newExplorationFixture(t)
	unknown := mustUUID(t)
	_, err := instruments.NewRepository(fixture.pool).History(
		fixture.ctx, unknown, instruments.HistoryFilter{Sessions: 10}, fixtureAsOf)
	if err == nil {
		t.Fatal("an unknown instrument returned a history window")
	}
}

func daysBetween(t *testing.T, from, to string) int {
	t.Helper()
	start, err := parseDay(from)
	if err != nil {
		t.Fatal(err)
	}
	end, err := parseDay(to)
	if err != nil {
		t.Fatal(err)
	}
	return int(end.Sub(start).Hours() / 24)
}

func parseDay(value string) (time.Time, error) {
	return time.Parse("2006-01-02", value)
}

// Annotations are scoped to the window actually drawn. A split outside it explains nothing
// about what is on screen, and listing it would invite a reader to attribute a move to it.
func TestHistoryReturnsOnlyTheAnnotationsTouchingTheWindow(t *testing.T) {
	fixture := newExplorationFixture(t)

	wide := historyFor(t, fixture, fixture.gappy, instruments.HistoryFilter{Sessions: 5000})
	if len(wide.Actions) != 2 {
		t.Fatalf("the whole history returned %d corporate actions, expected the split and the "+
			"dividend: %#v", len(wide.Actions), wide.Actions)
	}
	var split *instruments.ChartAction
	for index := range wide.Actions {
		if wide.Actions[index].Type == "split" {
			split = &wide.Actions[index]
		}
	}
	if split == nil {
		t.Fatal("the recorded split was not returned")
	}
	if split.Ratio == nil || split.Ratio.String() != "2.00000000" {
		t.Errorf("the split's ratio was %v; without it a reader cannot tell a real move from "+
			"an unadjusted split", split.Ratio)
	}
	for _, action := range wide.Actions {
		if action.ExDate < wide.RequestedFrom || action.ExDate > wide.RequestedTo {
			t.Errorf("action %s at %s is outside the window %s..%s",
				action.Type, action.ExDate, wide.RequestedFrom, wide.RequestedTo)
		}
	}

	// Both the open and the resolved finding fall in the full history; a caller decides which
	// to show, so the query must not silently drop one.
	if len(wide.Findings) != 2 {
		t.Errorf("the whole history returned %d findings, expected the open and the resolved "+
			"one: %#v", len(wide.Findings), wide.Findings)
	}
	var open int
	for _, finding := range wide.Findings {
		if finding.Status == "open" {
			open++
		}
		if finding.Rule == "" || finding.Severity == "" {
			t.Errorf("finding %s carries no rule or severity: %#v", finding.ID, finding)
		}
	}
	if open != 1 {
		t.Errorf("expected exactly one open finding, found %d", open)
	}

	// A five-session window sits after every annotation in the fixture, so it must return none.
	narrow := historyFor(t, fixture, fixture.gappy, instruments.HistoryFilter{Sessions: 2})
	for _, action := range narrow.Actions {
		if action.ExDate < narrow.RequestedFrom {
			t.Errorf("a two-session window returned action %s from %s", action.Type, action.ExDate)
		}
	}
}

// TestNothingIsDrawnThatIsNotStored is the claim this whole feature rests on, checked over
// every instrument in the fixture universe rather than over one convenient example (SC-005).
func TestNothingIsDrawnThatIsNotStored(t *testing.T) {
	fixture := newExplorationFixture(t)
	repository := instruments.NewRepository(fixture.pool)

	page, err := repository.Listing(fixture.ctx, instruments.ListingFilter{
		Limit: 200, Sort: instruments.SortName, AsOf: fixtureAsOf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) < 5 {
		t.Fatalf("the universe holds %d instruments, too few to prove anything", len(page.Items))
	}

	checked := 0
	for _, row := range page.Items {
		window, err := repository.History(fixture.ctx, row.ID,
			instruments.HistoryFilter{Sessions: 5000}, fixtureAsOf)
		if err != nil {
			t.Fatalf("history for %s: %v", row.Ticker, err)
		}
		checked++

		// 1. Every session drawn exists in stored data.
		for _, bar := range window.Bars {
			var stored bool
			if err := fixture.pool.QueryRow(fixture.ctx, `SELECT exists(
				SELECT 1 FROM daily_price_bars WHERE instrument_id = $1 AND session_date = $2)`,
				row.ID.String(), bar.SessionDate.String()).Scan(&stored); err != nil {
				t.Fatal(err)
			}
			if !stored {
				t.Errorf("%s: session %s was returned but is not stored — it was invented",
					row.Ticker, bar.SessionDate)
			}
		}

		// 2. No session is reported both as stored and as missing.
		drawn := map[instruments.SessionDate]bool{}
		for _, bar := range window.Bars {
			drawn[bar.SessionDate] = true
		}
		for _, missing := range window.MissingSessions {
			if drawn[missing] {
				t.Errorf("%s: session %s is both drawn and reported missing", row.Ticker, missing)
			}
			// 3. A day the exchange was closed is never reported as missing.
			var status string
			if err := fixture.pool.QueryRow(fixture.ctx, `SELECT s.status FROM exchange_sessions s
				JOIN instruments i ON i.exchange_id = s.exchange_id
				WHERE i.id = $1 AND s.session_date = $2`,
				row.ID.String(), missing.String()).Scan(&status); err != nil {
				t.Errorf("%s: missing session %s is not in the exchange calendar at all: %v",
					row.Ticker, missing, err)
				continue
			}
			if status == "closed" {
				t.Errorf("%s: %s was closed but is reported as a missing session", row.Ticker, missing)
			}
		}

		// 4. A statistic with too few sessions is absent, never zero.
		if row.StoredSessions < 21 {
			if row.Return20 != nil || row.Volatility != nil {
				t.Errorf("%s: computed a statistic from %d stored sessions",
					row.Ticker, row.StoredSessions)
			}
		}

		// 5. Every price is stated in the instrument's own currency; nothing is converted.
		for _, bar := range window.Bars {
			if bar.Currency != row.Currency {
				t.Errorf("%s: a bar is denominated in %s but the listing currency is %s",
					row.Ticker, bar.Currency, row.Currency)
			}
		}
	}
	if checked < 5 {
		t.Fatalf("only %d instruments were checked", checked)
	}
}

// Every recorded action and open finding inside a displayed range is visible in that range,
// checked across the universe rather than on one instrument (SC-006).
func TestEveryRecordedActionAndFindingInRangeIsReturned(t *testing.T) {
	fixture := newExplorationFixture(t)
	repository := instruments.NewRepository(fixture.pool)

	page, err := repository.Listing(fixture.ctx, instruments.ListingFilter{
		Limit: 200, Sort: instruments.SortName, AsOf: fixtureAsOf,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, row := range page.Items {
		window, err := repository.History(fixture.ctx, row.ID,
			instruments.HistoryFilter{Sessions: 5000}, fixtureAsOf)
		if err != nil {
			t.Fatal(err)
		}
		if window.RequestedFrom == "" {
			continue // no stored history, so there is no range to be visible in
		}

		var expectedActions, expectedFindings int
		if err := fixture.pool.QueryRow(fixture.ctx, `SELECT
			(SELECT count(*) FROM corporate_actions
			 WHERE instrument_id = $1 AND ex_date BETWEEN $2 AND $3),
			(SELECT count(*) FROM data_quality_findings
			 WHERE instrument_id = $1 AND (session_date IS NULL OR session_date BETWEEN $2 AND $3))`,
			row.ID.String(), window.RequestedFrom.String(), window.RequestedTo.String()).
			Scan(&expectedActions, &expectedFindings); err != nil {
			t.Fatal(err)
		}
		if len(window.Actions) != expectedActions {
			t.Errorf("%s: %d recorded actions fall in the window but %d were returned",
				row.Ticker, expectedActions, len(window.Actions))
		}
		if len(window.Findings) != expectedFindings {
			t.Errorf("%s: %d findings touch the window but %d were returned",
				row.Ticker, expectedFindings, len(window.Findings))
		}
	}
}
