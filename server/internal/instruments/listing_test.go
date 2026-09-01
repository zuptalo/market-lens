package instruments_test

import (
	"testing"
	"time"

	"market-lens/server/internal/instruments"
)

// listingFor runs a listing query over the exploration fixture and returns the rows keyed by
// ticker, because every assertion below is about a named instrument rather than a position.
func listingFor(t *testing.T, fixture *explorationFixture, filter instruments.ListingFilter) map[string]instruments.ListingRow {
	t.Helper()
	if filter.Limit == 0 {
		// The seeded Nordic universe is around a hundred instruments, so a small page would
		// simply not contain the fixture's rows and the test would be asserting on absence.
		filter.Limit = 200
	}
	if filter.AsOf == "" {
		filter.AsOf = fixtureAsOf
	}
	if filter.Sort == "" {
		filter.Sort = instruments.SortName
	}
	page, err := instruments.NewRepository(fixture.pool).Listing(fixture.ctx, filter)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	rows := map[string]instruments.ListingRow{}
	for _, row := range page.Items {
		rows[row.Ticker] = row
	}
	return rows
}

func decimalOrNil(value *instruments.Decimal) string {
	if value == nil {
		return "<absent>"
	}
	return value.String()
}

// T013, the designated first red for this feature. It fails against the identity-only search
// because the rows carry no latest close and no change, and the requested ordering is
// ignored — an assertion about returned values, not about compilation.
func TestListingReturnsLatestCloseChangeAndFreshness(t *testing.T) {
	fixture := newExplorationFixture(t)
	rows := listingFor(t, fixture, instruments.ListingFilter{})

	long, ok := rows["LONG"]
	if !ok {
		t.Fatal("the listing did not return the instrument with the deepest history")
	}

	if long.LatestSession == "" {
		t.Error("the listing reported no latest session for an instrument with 300 stored bars")
	}
	if long.LatestClose == nil {
		t.Error("the listing reported no latest close for an instrument with 300 stored bars")
	}
	if long.ChangeAbsolute == nil || long.ChangePercent == nil {
		t.Errorf("the listing reported no prior-session change: absolute=%s percent=%v",
			decimalOrNil(long.ChangeAbsolute), long.ChangePercent)
	}
	if long.StoredSessions != fixtureLongSessions {
		t.Errorf("stored session count was %d, expected %d", long.StoredSessions, fixtureLongSessions)
	}

	// The fixture's closes rise by 0.25 each session, so the most recent close is exactly
	// 0.25 above the one before it. Asserting the value rather than merely its presence is
	// what makes this a test of the derivation instead of a test of plumbing.
	if long.ChangeAbsolute != nil && long.ChangeAbsolute.String() != "0.25000000" {
		t.Errorf("prior-session change was %s, expected 0.25000000", long.ChangeAbsolute.String())
	}
	if long.PreviousClose == nil {
		t.Error("the listing reported no previous close, so the change cannot be checked")
	}
}

func TestListingOrdersTheWholeResultSetByTheRequestedSort(t *testing.T) {
	fixture := newExplorationFixture(t)
	repository := instruments.NewRepository(fixture.pool)

	ascending, err := repository.Listing(fixture.ctx, instruments.ListingFilter{
		Sort: instruments.SortTicker, Limit: 200, AsOf: fixtureAsOf,
	})
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	descending, err := repository.Listing(fixture.ctx, instruments.ListingFilter{
		Sort: instruments.SortTicker, Descending: true, Limit: 200, AsOf: fixtureAsOf,
	})
	if err != nil {
		t.Fatalf("listing: %v", err)
	}

	// Both must hold the entire universe, or this compares a first page with a last page and
	// proves nothing about ordering.
	if len(ascending.Items) != len(descending.Items) || len(ascending.Items) == 0 {
		t.Fatalf("the two orderings returned %d and %d rows",
			len(ascending.Items), len(descending.Items))
	}
	for index := range ascending.Items {
		mirrored := descending.Items[len(descending.Items)-1-index]
		if ascending.Items[index].Ticker != mirrored.Ticker {
			t.Fatalf("descending order is not the reverse of ascending: position %d held %s and %s",
				index, ascending.Items[index].Ticker, mirrored.Ticker)
		}
	}

	// Sorting by a derived value is the case that cannot be satisfied by ordering identity
	// columns, so it is the one that proves the sort reaches the database.
	byClose, err := repository.Listing(fixture.ctx, instruments.ListingFilter{
		Sort: instruments.SortLatestClose, Descending: true, Limit: 200, AsOf: fixtureAsOf,
	})
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	var previous *instruments.Decimal
	for _, row := range byClose.Items {
		if row.LatestClose == nil {
			continue // absent closes sort together; their relative order is checked below
		}
		if previous != nil && row.LatestClose.String() > previous.String() {
			t.Errorf("rows sorted by latest close descending were out of order: %s after %s",
				row.LatestClose.String(), previous.String())
		}
		previous = row.LatestClose
	}
}

func TestListingReportsFreshnessAgainstTheExchangeCalendar(t *testing.T) {
	fixture := newExplorationFixture(t)
	rows := listingFor(t, fixture, instruments.ListingFilter{})

	current := rows["LONG"]
	if current.Freshness.State != instruments.FreshnessCurrent {
		t.Errorf("an instrument with a bar for the most recent open session was reported %q",
			current.Freshness.State)
	}

	stale := rows["STALE"]
	if stale.Freshness.State != instruments.FreshnessStale {
		t.Errorf("an instrument ten open sessions behind was reported %q", stale.Freshness.State)
	}
	if stale.Freshness.SessionsBehind == nil {
		t.Error("a stale instrument did not say how far behind it is")
	} else if *stale.Freshness.SessionsBehind != fixtureStaleBehind {
		// Counting calendar days here instead of open sessions would overshoot, because the
		// ten sessions span two weekends.
		t.Errorf("stale instrument was %d sessions behind, expected %d",
			*stale.Freshness.SessionsBehind, fixtureStaleBehind)
	}

	empty := rows["EMPTY"]
	if empty.Freshness.State != instruments.FreshnessNoHistory {
		t.Errorf("an instrument with no stored bars was reported %q rather than no_history",
			empty.Freshness.State)
	}
	if empty.Freshness.SessionsBehind != nil {
		t.Errorf("an instrument with no history claimed to be %d sessions behind, but there is "+
			"nothing for it to be behind", *empty.Freshness.SessionsBehind)
	}
	if empty.LatestClose != nil {
		t.Errorf("an instrument with no stored bars reported a close of %s", empty.LatestClose.String())
	}
}

func TestListingLeavesUncomputableStatisticsAbsentRatherThanZero(t *testing.T) {
	fixture := newExplorationFixture(t)
	rows := listingFor(t, fixture, instruments.ListingFilter{})

	short := rows["SHORT"]
	if short.StoredSessions != fixtureShortSessions {
		t.Fatalf("the fixture instrument stored %d sessions, expected %d",
			short.StoredSessions, fixtureShortSessions)
	}
	// Twenty stored sessions is one short of the twenty-one a 20-session return needs, and
	// this is the exact boundary FR-007 is about: the answer is "absent", never "zero".
	// Twenty stored sessions is one short of the twenty-one a 20-session return needs, so the
	// engine records an absence for it and nothing downstream may invent one.
	if short.Return20 != nil || short.Return90 != nil || short.Volatility != nil {
		t.Errorf("statistics appeared for an instrument with only %d sessions: r20=%s r90=%s vol=%s",
			short.StoredSessions, decimalOrNil(short.Return20), decimalOrNil(short.Return90),
			decimalOrNil(short.Volatility))
	}
	// It still has a price, so absence of a statistic must not be confused with absence of data.
	if short.LatestClose == nil {
		t.Error("an instrument with 20 stored bars reported no latest close")
	}

	// The presence side of the same boundary: an instrument the engine has computed shows
	// exactly what it computed. Since feature 013 the number comes from feature_values rather
	// than from arithmetic in the listing query, so this seeds it and reads it back.
	session := fixture.latestSession(t, fixture.long)
	fixture.seedEngineValue(t, fixture.long, session, "return_20", "0.049875311721")
	fixture.seedEngineValue(t, fixture.long, session, "return_90", "0.234567890123")
	fixture.seedEngineValue(t, fixture.long, session, "volatility_20", "0.181818181818")

	long := listingFor(t, fixture, instruments.ListingFilter{})["LONG"]
	if long.Return20 == nil || long.Return90 == nil || long.Volatility == nil {
		t.Fatalf("an instrument with computed features listed none: r20=%v r90=%v vol=%v",
			long.Return20, long.Return90, long.Volatility)
	}
	if long.Return20.String() != "0.049875311721" || long.Return90.String() != "0.234567890123" ||
		long.Volatility.String() != "0.181818181818" {
		t.Errorf("statistics were %s, %s, %s; expected the stored decimals exactly",
			long.Return20, long.Return90, long.Volatility)
	}
}

func TestListingPagesWithoutRepeatingOrSkippingARow(t *testing.T) {
	fixture := newExplorationFixture(t)
	repository := instruments.NewRepository(fixture.pool)

	// The universe here is the seeded Nordic listing plus the fixture's own instruments, so
	// most rows have no stored history and every derived statistic on them is null. That is
	// the case keyset pagination gets wrong when the cursor is not made total by the
	// instrument identifier: without a tiebreaker, equal values repeat and skip rows.
	whole, err := repository.Listing(fixture.ctx, instruments.ListingFilter{
		Sort: instruments.SortName, Limit: 200, AsOf: fixtureAsOf,
	})
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	total := len(whole.Items)
	if total < 20 {
		t.Fatalf("the universe holds only %d instruments, too few to prove paging works", total)
	}

	for _, sort := range []instruments.ListingSort{
		instruments.SortName, instruments.SortReturn20, instruments.SortLatestClose,
	} {
		seen := map[string]int{}
		order := make([]string, 0, total)
		cursor := ""
		for page := 0; page <= total; page++ {
			result, err := repository.Listing(fixture.ctx, instruments.ListingFilter{
				Sort: sort, Limit: 7, Cursor: cursor, AsOf: fixtureAsOf,
			})
			if err != nil {
				t.Fatalf("listing sorted by %s: %v", sort, err)
			}
			for _, row := range result.Items {
				seen[row.ID.String()]++
				order = append(order, row.ID.String())
			}
			cursor = result.NextCursor
			if cursor == "" {
				break
			}
		}
		if len(seen) != total {
			t.Errorf("paging sorted by %s reached %d distinct instruments, expected %d",
				sort, len(seen), total)
		}
		if len(order) != total {
			t.Errorf("paging sorted by %s returned %d rows in total, expected %d",
				sort, len(order), total)
		}
		for id, count := range seen {
			if count != 1 {
				t.Errorf("paging sorted by %s returned instrument %s %d times", sort, id, count)
			}
		}
	}
}

func TestListingFiltersBySectorExchangeAndSearch(t *testing.T) {
	fixture := newExplorationFixture(t)

	// Assert the property rather than a row count: the seeded Nordic universe shares these
	// sectors and exchanges, and a count would make this test a hostage to the seed.
	bySector := listingFor(t, fixture, instruments.ListingFilter{Sector: "Technology", Limit: 200})
	if _, ok := bySector["GAPPY"]; !ok {
		t.Error("filtering by sector did not return the fixture's Technology instrument")
	}
	for ticker, row := range bySector {
		if row.Sector != "Technology" {
			t.Errorf("filtering by sector returned %s, whose sector is %q", ticker, row.Sector)
		}
	}

	byExchange := listingFor(t, fixture, instruments.ListingFilter{MIC: "XCSE", Limit: 200})
	for _, want := range []string{"SHORT", "EMPTY"} {
		if _, ok := byExchange[want]; !ok {
			t.Errorf("filtering by exchange did not return the Copenhagen listing %s", want)
		}
	}
	for ticker, row := range byExchange {
		if row.Exchange.MIC != "XCSE" {
			t.Errorf("filtering by XCSE returned %s, listed on %s", ticker, row.Exchange.MIC)
		}
	}

	// SC-004: every instrument must be reachable by its own name, ticker, and ISIN.
	for _, probe := range []struct{ kind, query, want string }{
		{"ticker", "LONG", "LONG"},
		{"name", "Interrupted History", "GAPPY"},
		{"isin", "DK0000000300", "SHORT"},
	} {
		found := listingFor(t, fixture, instruments.ListingFilter{Query: probe.query, Limit: 200})
		if _, ok := found[probe.want]; !ok {
			t.Errorf("searching by %s for %q did not return %s", probe.kind, probe.query, probe.want)
		}
	}
}

// SC-002 bounds the first page of the universe under any supported sort.
//
// This measures query time, not the end-to-end "on a typical connection" the criterion
// describes: network and rendering are outside what a Go test can see. It is the part of the
// budget this layer is responsible for, and the part a regression here would consume first.
func TestTheFirstPageOfTheUniverseStaysWithinItsBudget(t *testing.T) {
	fixture := newExplorationFixture(t)
	repository := instruments.NewRepository(fixture.pool)

	for _, sort := range []instruments.ListingSort{
		instruments.SortName, instruments.SortTicker, instruments.SortExchange,
		instruments.SortSector, instruments.SortCountry, instruments.SortLatestClose,
		instruments.SortChangePercent, instruments.SortReturn20, instruments.SortReturn90,
		instruments.SortVolatility, instruments.SortFreshness,
	} {
		started := time.Now()
		page, err := repository.Listing(fixture.ctx, instruments.ListingFilter{
			Limit: 50, Sort: sort, AsOf: fixtureAsOf,
		})
		if err != nil {
			t.Fatalf("listing sorted by %s: %v", sort, err)
		}
		elapsed := time.Since(started)
		if len(page.Items) == 0 {
			t.Fatalf("listing sorted by %s returned nothing", sort)
		}
		if elapsed > 2*time.Second {
			t.Errorf("the first page sorted by %s took %s, over the two-second budget", sort, elapsed)
		}
	}
}

// US5-1: the three statistics on the Markets list are the engine's, not this query's. An
// instrument with plenty of history but no computed values must therefore show them absent —
// the state a person sees between an import and the pass that follows it. Deriving a number
// here instead would be a second definition of the same statistic, which is the disagreement
// feature 013 exists to end.
func TestListingStatisticsComeFromTheEngineNotTheQuery(t *testing.T) {
	fixture := newExplorationFixture(t)
	rows := listingFor(t, fixture, instruments.ListingFilter{})

	gappy := rows["GAPPY"]
	if gappy.StoredSessions != fixtureGappySessions-int64(len(fixtureGapOffsets)) {
		t.Fatalf("GAPPY stored %d sessions", gappy.StoredSessions)
	}
	if gappy.Return20 != nil || gappy.Return90 != nil || gappy.Volatility != nil {
		t.Errorf("an instrument with no computed features listed r20=%v r90=%v vol=%v; each must be absent until the engine has run",
			decimalOrNil(gappy.Return20), decimalOrNil(gappy.Return90), decimalOrNil(gappy.Volatility))
	}
	// Everything the listing derives from bars itself is unaffected.
	if gappy.LatestClose == nil || gappy.ChangePercent == nil {
		t.Errorf("GAPPY lost its price or its change: close=%v change=%v", gappy.LatestClose, gappy.ChangePercent)
	}
}

// US5-2: what the listing shows is exactly what the engine stored — the same decimal, not a
// number that has been through a float64 on the way. An absence the engine recorded stays an
// absence here.
func TestListingStatisticsEqualTheEnginesStoredValues(t *testing.T) {
	fixture := newExplorationFixture(t)
	session := fixture.latestSession(t, fixture.long)
	fixture.seedEngineValue(t, fixture.long, session, "return_20", "0.123456789012")
	fixture.seedEngineValue(t, fixture.long, session, "return_90", "-0.098765432109")
	fixture.seedEngineValue(t, fixture.long, session, "volatility_20", "")

	long := listingFor(t, fixture, instruments.ListingFilter{})["LONG"]
	if got := decimalOrNil(long.Return20); got != "0.123456789012" {
		t.Errorf("return_20 = %s, expected the stored 0.123456789012", got)
	}
	if got := decimalOrNil(long.Return90); got != "-0.098765432109" {
		t.Errorf("return_90 = %s, expected the stored -0.098765432109", got)
	}
	if long.Volatility != nil {
		t.Errorf("volatility = %s, but the engine recorded an absence for it", decimalOrNil(long.Volatility))
	}
}

// US5-2: sorting orders by the engine's column, over the whole result set, with absences last
// and pagination still total.
func TestSortingByAnAdoptedStatisticUsesTheEngineColumn(t *testing.T) {
	fixture := newExplorationFixture(t)
	// Three instruments with values, deliberately out of alphabetical order, and everything
	// else in the universe with none.
	for instrument, value := range map[instruments.UUID]string{
		fixture.long: "0.050000000000", fixture.gappy: "-0.020000000000", fixture.stale: "0.110000000000",
	} {
		fixture.seedEngineValue(t, instrument, fixture.latestSession(t, instrument), "return_20", value)
	}

	repository := instruments.NewRepository(fixture.pool)
	page, err := repository.Listing(fixture.ctx, instruments.ListingFilter{
		Sort: instruments.SortReturn20, Descending: true, Limit: 200, AsOf: fixtureAsOf,
	})
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	var ranked []string
	for _, row := range page.Items {
		if row.Return20 != nil {
			ranked = append(ranked, row.Ticker)
		} else if len(ranked) < 3 {
			t.Fatalf("an absent return sorted ahead of a computed one: %s", row.Ticker)
		}
	}
	if len(ranked) != 3 || ranked[0] != "STALE" || ranked[1] != "LONG" || ranked[2] != "GAPPY" {
		t.Errorf("descending by return_20 gave %v, expected STALE, LONG, GAPPY", ranked)
	}

	// The same keyset guarantee as every other sort: no row repeats and none is skipped.
	seen := map[string]int{}
	cursor := ""
	for page := 0; page < 100; page++ {
		result, err := repository.Listing(fixture.ctx, instruments.ListingFilter{
			Sort: instruments.SortReturn20, Limit: 7, Cursor: cursor, AsOf: fixtureAsOf,
		})
		if err != nil {
			t.Fatalf("paging by return_20: %v", err)
		}
		for _, row := range result.Items {
			seen[row.ID.String()]++
		}
		if cursor = result.NextCursor; cursor == "" {
			break
		}
	}
	if len(seen) != len(page.Items) {
		t.Errorf("paging by return_20 reached %d instruments, the unpaged listing has %d", len(seen), len(page.Items))
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("paging by return_20 returned %s %d times", id, count)
		}
	}
}
