package strategies_test

import (
	"context"
	"errors"
	"math"
	"strconv"
	"testing"

	"market-lens/server/internal/features"
	"market-lens/server/internal/strategies"
)

// TestReadingOneInstrumentsSignalAsOfASession is the read the instrument screen is built on.
//
// It asserts the version travels with the signal, because the caveat lives on the version: a
// read that returned a score without the statement that it is not advice would put the burden of
// remembering on every caller, and one of them would eventually forget.
func TestReadingOneInstrumentsSignalAsOfASession(t *testing.T) {
	fixture := newStrategyFixture(t)
	fixture.compute(strategies.ComputeRequest{Kind: strategies.RunKindFull})

	repository := strategies.NewRepository(fixture.pool)
	ctx := context.Background()
	instrument := strategyRanked(0)
	session := fixture.latestScoredSession(instrument)

	view, err := repository.SignalAsOf(ctx, instrument, session, "", 0)
	if err != nil {
		t.Fatalf("read the signal: %v", err)
	}
	if view.Signal.InstrumentID != instrument || view.Signal.SessionDate != session {
		t.Fatalf("read %s at %s, wanted %s at %s",
			view.Signal.InstrumentID, view.Signal.SessionDate, instrument, session)
	}
	if view.Signal.Score == nil || view.Signal.Action == nil || view.Signal.Confidence == nil {
		t.Fatalf("the signal is not scored: %+v", view.Signal)
	}
	if view.Strategy.Name == "" || view.Strategy.Version == 0 {
		t.Fatalf("the read did not carry the strategy version: %+v", view.Strategy)
	}
	if view.Strategy.Caveat == "" {
		t.Fatalf("the read did not carry the caveat, so a caller could show a score without it")
	}
	if len(view.Signal.Contributions) != len(view.Strategy.Factors) {
		t.Fatalf("the signal records %d contributions for %d factors",
			len(view.Signal.Contributions), len(view.Strategy.Factors))
	}
	for index, contribution := range view.Signal.Contributions {
		if contribution.Factor != view.Strategy.Factors[index].Name {
			t.Fatalf("contribution %d is %q, the version's factor %d is %q",
				index, contribution.Factor, index, view.Strategy.Factors[index].Name)
		}
		if contribution.Feature == "" {
			t.Fatalf("contribution %q does not name the feature it read", contribution.Factor)
		}
	}

	// An instrument that exists but has no signal at that session, and one that does not exist
	// at all, must be distinguishable: the first is a stated gap, the second is a bad request.
	if _, err := repository.SignalAsOf(ctx, instrument, features.SessionDate("1999-01-04"), "", 0); !errors.Is(err, strategies.ErrNoSignal) {
		t.Fatalf("a session with no signal returned %v, wanted ErrNoSignal", err)
	}
	unknown := features.UUID("ffffffff-0015-4000-8000-0000000000dd")
	if _, err := repository.SignalAsOf(ctx, unknown, session, "", 0); !errors.Is(err, strategies.ErrNotFound) {
		t.Fatalf("an unknown instrument returned %v, wanted ErrNotFound", err)
	}
}

// latestScoredSession is the most recent session at which the instrument has an actual view,
// which is what the instrument screen shows by default.
func (f *strategyFixture) latestScoredSession(instrument features.UUID) features.SessionDate {
	f.t.Helper()
	var session *string
	if err := f.pool.QueryRow(f.ctx, `SELECT max(session_date)::text FROM signals
		WHERE instrument_id = $1 AND score IS NOT NULL`, instrument.String()).Scan(&session); err != nil {
		f.t.Fatalf("find a scored session: %v", err)
	}
	if session == nil {
		f.t.Fatalf("instrument %s has no scored session at all", instrument)
	}
	return features.SessionDate(*session)
}

// TestRankingOrdersScoredInstrumentsAndSeparatesTheRest is the ranked view's contract.
//
// The separation is the part that matters. An instrument the strategy could not score is not a
// weak instrument, and sorting it to the bottom of the same ordering would make it look like
// one — the reader would see it below a SELL and draw exactly the wrong conclusion.
func TestRankingOrdersScoredInstrumentsAndSeparatesTheRest(t *testing.T) {
	fixture := newStrategyFixture(t)
	fixture.compute(strategies.ComputeRequest{Kind: strategies.RunKindFull})

	repository := strategies.NewRepository(fixture.pool)
	page, err := repository.Ranking(context.Background(), strategies.RankingRequest{Limit: 50})
	if err != nil {
		t.Fatalf("read the ranking: %v", err)
	}
	if page.Strategy.Name == "" || page.Strategy.Caveat == "" {
		t.Fatalf("the ranking did not carry the strategy version and its caveat")
	}
	if page.SessionDate == "" {
		t.Fatalf("the ranking did not state which session it is of")
	}
	if page.Scored == 0 {
		t.Fatalf("no instrument was scored, so there is no ranking to check")
	}
	if page.Unscored == 0 {
		t.Fatalf("no instrument was unscored, so the separation is untested")
	}
	if page.Total == nil || *page.Total != page.Scored+page.Unscored {
		t.Fatalf("a cursor-less request reported total %v for %d scored and %d unscored",
			page.Total, page.Scored, page.Unscored)
	}

	// Scores are decimal strings, so the comparison has to be numeric: "-0.2" sorts above
	// "-0.09" as text and below it as a number, and the text answer is the wrong one.
	previous := math.Inf(1)
	seenUnscored := false
	for index, item := range page.Items {
		scored := item.Signal.Score != nil
		if scored && seenUnscored {
			t.Fatalf("item %d is scored but follows an unscored one", index)
		}
		if !scored {
			seenUnscored = true
			if item.Rank != nil {
				t.Fatalf("unscored instrument %s carries rank %d", item.Ticker, *item.Rank)
			}
			if item.Signal.AbsenceReason == nil {
				t.Fatalf("unscored instrument %s states no reason", item.Ticker)
			}
			continue
		}
		if item.Rank == nil || *item.Rank != int64(index+1) {
			t.Fatalf("scored item %d carries rank %v", index, item.Rank)
		}
		score, err := strconv.ParseFloat(*item.Signal.Score, 64)
		if err != nil {
			t.Fatalf("item %d stored an unreadable score %q", index, *item.Signal.Score)
		}
		if score > previous {
			t.Fatalf("item %d scores %v, above the %v before it", index, score, previous)
		}
		previous = score
		if item.Ticker == "" || item.Name == "" {
			t.Fatalf("scored item %d does not name its instrument", index)
		}
	}
	if len(page.Items) != int(page.Scored+page.Unscored) {
		t.Fatalf("the page holds %d items for %d instruments", len(page.Items), page.Scored+page.Unscored)
	}
}

// TestRankingPagesWithoutLosingOrRepeatingAnInstrument walks the ranking a page at a time and
// asserts the whole universe comes back exactly once, in the same order the single page gave.
func TestRankingPagesWithoutLosingOrRepeatingAnInstrument(t *testing.T) {
	fixture := newStrategyFixture(t)
	fixture.compute(strategies.ComputeRequest{Kind: strategies.RunKindFull})

	repository := strategies.NewRepository(fixture.pool)
	ctx := context.Background()
	whole, err := repository.Ranking(ctx, strategies.RankingRequest{Limit: 200})
	if err != nil {
		t.Fatalf("read the whole ranking: %v", err)
	}
	if len(whole.Items) < 6 {
		t.Fatalf("the ranking holds %d instruments, too few to page meaningfully", len(whole.Items))
	}

	var walked []strategies.RankedSignal
	cursor := ""
	for page := 0; ; page++ {
		if page > 20 {
			t.Fatalf("paging did not terminate")
		}
		next, err := repository.Ranking(ctx, strategies.RankingRequest{Limit: 3, Cursor: cursor})
		if err != nil {
			t.Fatalf("read page %d: %v", page, err)
		}
		if page > 0 && next.Total != nil {
			t.Fatalf("page %d counted the whole ranking again", page)
		}
		walked = append(walked, next.Items...)
		if next.NextCursor == "" {
			break
		}
		cursor = next.NextCursor
	}

	if len(walked) != len(whole.Items) {
		t.Fatalf("paging returned %d instruments, the single page %d", len(walked), len(whole.Items))
	}
	for index := range walked {
		if walked[index].Signal.InstrumentID != whole.Items[index].Signal.InstrumentID {
			t.Fatalf("position %d is %s when paged and %s when not",
				index, walked[index].Signal.InstrumentID, whole.Items[index].Signal.InstrumentID)
		}
		if walked[index].Rank != nil && *walked[index].Rank != int64(index+1) {
			t.Fatalf("position %d carries rank %d across pages", index, *walked[index].Rank)
		}
	}
}

// TestRankingIsStableForIdenticalScores: when two instruments score the same, the order between
// them must still be the same on every read, or a reader refreshing a page would watch rows swap
// for no reason and reasonably conclude the ranking means nothing.
func TestRankingIsStableForIdenticalScores(t *testing.T) {
	fixture := newStrategyFixture(t)
	fixture.compute(strategies.ComputeRequest{Kind: strategies.RunKindFull})
	fixture.exec(`UPDATE signals SET score = 0.5 WHERE score IS NOT NULL`)

	repository := strategies.NewRepository(fixture.pool)
	ctx := context.Background()
	first, err := repository.Ranking(ctx, strategies.RankingRequest{Limit: 200})
	if err != nil {
		t.Fatalf("read the ranking: %v", err)
	}
	if len(first.Items) < 6 {
		t.Fatalf("the ranking holds %d instruments, too few for ties to matter", len(first.Items))
	}
	for attempt := range 3 {
		again, err := repository.Ranking(ctx, strategies.RankingRequest{Limit: 200})
		if err != nil {
			t.Fatalf("read the ranking again: %v", err)
		}
		for index := range first.Items {
			if again.Items[index].Signal.InstrumentID != first.Items[index].Signal.InstrumentID {
				t.Fatalf("attempt %d moved position %d from %s to %s", attempt, index,
					first.Items[index].Signal.InstrumentID, again.Items[index].Signal.InstrumentID)
			}
		}
	}
}

// TestSignalsChangedEventIsSharedScopeAndTransactional: the event exists because the signals
// were committed, in the same transaction, so a client that reconnects and replays cannot learn
// about a change that did not happen or miss one that did.
func TestSignalsChangedEventIsSharedScopeAndTransactional(t *testing.T) {
	fixture := newStrategyFixture(t)
	run := fixture.compute(strategies.ComputeRequest{Kind: strategies.RunKindFull})

	events := fixture.count(`SELECT count(*) FROM client_events WHERE event_type = $1`,
		strategies.EventSignalsChanged)
	if events == 0 {
		t.Fatalf("the run wrote %d signals and published no change event", run.SignalCount)
	}
	if misscoped := fixture.count(`SELECT count(*) FROM client_events
		WHERE event_type = $1 AND (scope <> 'shared' OR subject_user_id IS NOT NULL
		  OR entity_type <> 'instrument' OR version <> 1)`, strategies.EventSignalsChanged); misscoped != 0 {
		t.Fatalf("%d signal events are not shared, versioned, instrument-scoped reference data", misscoped)
	}

	// The payload names the change, never the signals. A score inside an event would be a
	// second copy of a stored value that nothing keeps in step with the first.
	if leaking := fixture.count(`SELECT count(*) FROM client_events
		WHERE event_type = $1 AND (payload ? 'score' OR payload ? 'action' OR payload ? 'contributions')`,
		strategies.EventSignalsChanged); leaking != 0 {
		t.Fatalf("%d signal events carry the signals themselves", leaking)
	}
	if incomplete := fixture.count(`SELECT count(*) FROM client_events
		WHERE event_type = $1 AND NOT (payload ? 'instrument_id' AND payload ? 'from_session'
		  AND payload ? 'to_session' AND payload ? 'run_id' AND payload ? 'strategy_id')`,
		strategies.EventSignalsChanged); incomplete != 0 {
		t.Fatalf("%d signal events do not say what changed", incomplete)
	}

	// Every instrument that had signals written has an event, and every event names an
	// instrument that did: the two cannot come apart, because one transaction wrote both.
	if orphaned := fixture.count(`SELECT count(*) FROM client_events e
		WHERE e.event_type = $1 AND NOT EXISTS (
			SELECT 1 FROM strategy_run_items i
			WHERE i.instrument_id::text = e.payload ->> 'instrument_id'
			  AND i.run_id::text = e.payload ->> 'run_id')`,
		strategies.EventSignalsChanged); orphaned != 0 {
		t.Fatalf("%d signal events describe an instrument the run did not record", orphaned)
	}
}

// TestListingStrategyRunsReturnsTheMostRecentFirstFromTheStore covers the read the operational
// screen makes, including the failed count it derives from the run's items.
func TestListingStrategyRunsReturnsTheMostRecentFirstFromTheStore(t *testing.T) {
	fixture := newStrategyFixture(t)
	first := fixture.compute(strategies.ComputeRequest{Kind: strategies.RunKindFull})
	second := fixture.compute(strategies.ComputeRequest{Kind: strategies.RunKindFull})

	runs, err := strategies.NewRepository(fixture.pool).ListRuns(context.Background(), 10)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("listed %d runs", len(runs))
	}
	if runs[0].ID != second.ID || runs[1].ID != first.ID {
		t.Fatalf("runs are %s then %s; wanted the most recent first", runs[0].ID, runs[1].ID)
	}
	if runs[0].SignalCount == 0 || runs[0].InstrumentCount == 0 || runs[0].AppVersion == "" {
		t.Fatalf("the most recent run reads back as %+v", runs[0])
	}
	if runs[0].FinishedAt == nil {
		t.Fatalf("a finished run has no finish time")
	}
	if _, err := strategies.NewRepository(fixture.pool).ListRuns(context.Background(), 0); err == nil {
		t.Fatalf("a zero limit was accepted")
	}
}
