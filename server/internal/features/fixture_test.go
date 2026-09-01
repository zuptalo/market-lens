package features_test

import (
	"context"
	"fmt"
	"testing"

	"market-lens/server/internal/db"
	"market-lens/server/internal/features"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The engine fixture is the universe every feature-engine suite computes and reads. It is
// built on the real Stockholm calendar that migration 0005 populates — open days, half days
// and closed days — because the claim under test is that windows count stored sessions and
// that a closed exchange day is not a gap. An invented calendar would let a test agree with
// a bug.
//
// Prices are generated from an integer formula of the bar's position (fixtureBar) so that a
// throwaway script outside the repository can reproduce the exact same series and the golden
// values in testdata/ are not the engine checking itself.
//
// Every identifier is fixed rather than random so that "sorted by instrument id" — the order
// the composite sums contributors in — is the same order in every run and in the script that
// produced the golden values.
const fixtureAsOf = features.SessionDate("2026-06-30")

const (
	fixtureExchangeMIC = "XSTO"
	fixtureUniverse    = "fixture-v1"

	fixtureASessions = 320
	fixtureBSessions = 20
	fixtureDSessions = 120
	fixtureESessions = 200
	// fixtureFillerSessions is the depth of the eight instruments that exist only so the
	// composite has at least ten contributors once D and E are listed, and exactly nine —
	// one short — before E is.
	fixtureFillerSessions = 320
	fixtureFillerCount    = 8

	// fixtureDGapStart is the offset, counted back from the most recent session, of the
	// first of three consecutive open sessions D has no bar for.
	fixtureDGapStart  = 40
	fixtureDGapLength = 3
	// fixtureESplitOffset is the offset back of E's 2-for-1 ex-date. Bars before it are
	// stored at twice the price, as the exchange quoted them at the time.
	fixtureESplitOffset = 30
	fixtureESplitRatio  = 2
	// fixtureZeroVolumeOffset is the one session at which every listed instrument stored a
	// bar with zero volume: an observation, not an absence.
	fixtureZeroVolumeOffset = 10
)

// Fixed identifiers; the composite sums contributors in this order.
var (
	fixtureA = features.UUID("ffffffff-0013-4000-8000-000000000001")
	fixtureB = features.UUID("ffffffff-0013-4000-8000-000000000002")
	fixtureC = features.UUID("ffffffff-0013-4000-8000-000000000003")
	fixtureD = features.UUID("ffffffff-0013-4000-8000-000000000004")
	fixtureE = features.UUID("ffffffff-0013-4000-8000-000000000005")
	// fixtureF is not created by newEngineFixture: it is the instrument a suite lists after
	// the others to show that a newcomer changes nothing before its first bar.
	fixtureF      = features.UUID("ffffffff-0013-4000-8000-000000000006")
	fixtureUnivID = features.UUID("ffffffff-0013-4000-8000-0000000000ff")
	fixtureRunID  = features.UUID("ffffffff-0013-4000-8000-0000000000aa")
)

func fixtureFiller(index int) features.UUID {
	return features.UUID(fmt.Sprintf("ffffffff-0013-4000-8000-0000000000%02d", 11+index))
}

// fixtureSeed decorrelates the instruments' series; it is part of the generator's input and
// therefore part of what the golden script reproduces. Fillers are seeded 11 to 18.
func fixtureSeed(instrument features.UUID) int {
	switch instrument {
	case fixtureA:
		return 1
	case fixtureB:
		return 2
	case fixtureD:
		return 4
	case fixtureE:
		return 5
	case fixtureF:
		return 6
	}
	for index := range fixtureFillerCount {
		if instrument == fixtureFiller(index) {
			return 11 + index
		}
	}
	panic("fixture instrument has no seed: " + instrument.String())
}

type fixtureBarValues struct {
	openCents, highCents, lowCents, closeCents, volume int64
}

// fixtureBar is the generator. position counts from the instrument's oldest session (0), and
// the arithmetic is integer only so that any language reproduces it exactly.
func fixtureBar(seed, position int) fixtureBarValues {
	i, s := int64(position), int64(seed)
	closeCents := 10000 + 500*s + 25*i + ((i*i*7919+i*104729+s*7)%401 - 200)
	highCents := closeCents + (i*31+s)%200 + 50
	lowCents := closeCents - (i*17+s)%200 - 50
	return fixtureBarValues{
		openCents:  (highCents + lowCents) / 2,
		highCents:  highCents,
		lowCents:   lowCents,
		closeCents: closeCents,
		volume:     1000 + (i*7919+s*101)%5000,
	}
}

type engineFixture struct {
	t    *testing.T
	pool *pgxpool.Pool
	ctx  context.Context

	exchange features.UUID
	// positions remembers how many positions each instrument's series has consumed, so
	// extendHistory continues the generator rather than restarting it.
	positions map[features.UUID]int
}

func newEngineFixture(t *testing.T) *engineFixture {
	t.Helper()
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	f := &engineFixture{t: t, pool: pool, ctx: ctx, positions: map[features.UUID]int{}}
	if err := pool.QueryRow(ctx, `SELECT id::text FROM exchanges WHERE mic = $1`, fixtureExchangeMIC).
		Scan((*string)(&f.exchange)); err != nil {
		t.Fatalf("find fixture exchange: %v", err)
	}
	f.exec(`INSERT INTO import_runs (id, kind, provider, status, started_at, finished_at, app_version)
		VALUES ($1, 'backfill', 'fixture', 'succeeded', now(), now(), 'test')`, fixtureRunID.String())

	f.addInstrument(fixtureA, "SE0000000101", "FXA", "Fixture Alpha AB", "EUR")
	f.addInstrument(fixtureB, "SE0000000102", "FXB", "Fixture Barely Listed AB", "SEK")
	f.addInstrument(fixtureC, "SE0000000103", "FXC", "Fixture No History AB", "SEK")
	f.addInstrument(fixtureD, "SE0000000104", "FXD", "Fixture Interrupted AB", "SEK")
	f.addInstrument(fixtureE, "SE0000000105", "FXE", "Fixture Split AB", "SEK")
	for index := range fixtureFillerCount {
		f.addInstrument(fixtureFiller(index), fmt.Sprintf("SE00000001%02d", 11+index), fmt.Sprintf("FXF%d", index+1),
			fmt.Sprintf("Fixture Filler %d AB", index+1), "SEK")
	}

	f.addBars(fixtureA, fixtureASessions, nil, 0)
	f.addBars(fixtureB, fixtureBSessions, nil, 0)
	f.addBars(fixtureD, fixtureDSessions, []int{fixtureDGapStart, fixtureDGapStart + 1, fixtureDGapStart + 2}, 0)
	f.addBars(fixtureE, fixtureESessions, nil, fixtureESplitOffset)
	for index := range fixtureFillerCount {
		f.addBars(fixtureFiller(index), fixtureFillerSessions, nil, 0)
	}

	splitDate := f.sessionAtOffset(fixtureESplitOffset)
	f.exec(`INSERT INTO corporate_actions
		(id, instrument_id, provider, provider_action_id, action_type, ex_date, ratio,
		 source_hash, import_run_id, first_observed_at, last_observed_at)
		VALUES ($1, $2, 'fixture', 'split-e', 'split', $3, $4, 'split-hash', $5, now(), now())`,
		"ffffffff-0013-4000-8000-0000000000e1", fixtureE.String(), splitDate.String(),
		fixtureESplitRatio, fixtureRunID.String())

	f.exec(`INSERT INTO research_universes (id, code, name, description)
		VALUES ($1, $2, 'Feature engine fixture', 'the engine test universe')`,
		fixtureUnivID.String(), fixtureUniverse)
	members := []features.UUID{fixtureA, fixtureB, fixtureC, fixtureD, fixtureE}
	for index := range fixtureFillerCount {
		members = append(members, fixtureFiller(index))
	}
	for _, member := range members {
		f.exec(`INSERT INTO universe_memberships
			(universe_id, instrument_id, included_from, curation_source)
			VALUES ($1, $2, '2016-01-01', 'fixture')`, fixtureUnivID.String(), member.String())
	}
	return f
}

func (f *engineFixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(f.ctx, sql, args...); err != nil {
		f.t.Fatalf("fixture: %v\n%s", err, sql)
	}
}

func (f *engineFixture) addInstrument(id features.UUID, isin, ticker, name, currency string) {
	f.t.Helper()
	f.exec(`INSERT INTO instruments
		(id, exchange_id, isin, ticker, name, currency, country, instrument_type, active, purchasability_status)
		VALUES ($1, $2, $3, $4, $5, $6, 'SE', 'common_stock', true, 'unverified')`,
		id.String(), f.exchange.String(), isin, ticker, name, currency)
}

// openSessions returns the exchange's open and half-day sessions in [from, through],
// ascending. A closed day is not a session and never appears.
func (f *engineFixture) openSessions(from, through features.SessionDate) []features.SessionDate {
	f.t.Helper()
	rows, err := f.pool.Query(f.ctx, `SELECT session_date::text FROM exchange_sessions
		WHERE exchange_id = $1 AND status IN ('open', 'half_day')
		  AND session_date >= $2 AND session_date <= $3
		ORDER BY session_date`, f.exchange.String(), from.String(), through.String())
	if err != nil {
		f.t.Fatalf("list open sessions: %v", err)
	}
	defer rows.Close()
	var sessions []features.SessionDate
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			f.t.Fatal(err)
		}
		sessions = append(sessions, features.SessionDate(value))
	}
	return sessions
}

// sessionAtOffset returns the open session `offset` sessions before the as-of date (0 is
// the as-of date itself, which is an open session).
func (f *engineFixture) sessionAtOffset(offset int) features.SessionDate {
	f.t.Helper()
	var value string
	if err := f.pool.QueryRow(f.ctx, `SELECT session_date::text FROM exchange_sessions
		WHERE exchange_id = $1 AND status IN ('open', 'half_day') AND session_date <= $2
		ORDER BY session_date DESC OFFSET $3 LIMIT 1`,
		f.exchange.String(), fixtureAsOf.String(), offset).Scan(&value); err != nil {
		f.t.Fatalf("session at offset %d: %v", offset, err)
	}
	return features.SessionDate(value)
}

// addBars stores `count` sessions ending at the as-of date, skipping the offsets in
// `skipOffsets` (a skipped offset is an open session with no bar: a gap). Bars at offsets
// beyond `preSplitFrom` — before the ex-date, which is the bar at that offset — are stored at
// the pre-split price, as quoted at the time.
func (f *engineFixture) addBars(instrument features.UUID, count int, skipOffsets []int, preSplitFrom int) {
	f.t.Helper()
	sessions := f.openSessions(features.SessionDate("2016-01-01"), fixtureAsOf)
	if len(sessions) < count {
		f.t.Fatalf("the calendar holds %d sessions, fewer than the %d requested", len(sessions), count)
	}
	sessions = sessions[len(sessions)-count:]
	skipped := map[int]bool{}
	for _, offset := range skipOffsets {
		skipped[offset] = true
	}
	var dates []string
	var opens, highs, lows, closes, volumes []int64
	for position, session := range sessions {
		offset := count - 1 - position
		if skipped[offset] {
			continue
		}
		bar := fixtureBar(fixtureSeed(instrument), position)
		multiplier := int64(1)
		if preSplitFrom > 0 && offset > preSplitFrom {
			multiplier = fixtureESplitRatio
		}
		volume := bar.volume
		if offset == fixtureZeroVolumeOffset {
			volume = 0
		}
		dates = append(dates, session.String())
		opens = append(opens, bar.openCents*multiplier)
		highs = append(highs, bar.highCents*multiplier)
		lows = append(lows, bar.lowCents*multiplier)
		closes = append(closes, bar.closeCents*multiplier)
		volumes = append(volumes, volume)
	}
	f.positions[instrument] = count
	f.insertBars(instrument, fixtureRunID, dates, opens, highs, lows, closes, volumes)
}

func (f *engineFixture) insertBars(instrument, runID features.UUID, dates []string,
	opens, highs, lows, closes, volumes []int64) {
	f.t.Helper()
	var currency string
	if err := f.pool.QueryRow(f.ctx, `SELECT currency FROM instruments WHERE id = $1`,
		instrument.String()).Scan(&currency); err != nil {
		f.t.Fatalf("instrument currency: %v", err)
	}
	f.exec(`INSERT INTO daily_price_bars
		(instrument_id, session_date, open, high, low, close, adjusted_close, volume,
		 currency, provider, source_hash, import_run_id, first_observed_at, last_observed_at)
		SELECT $1::uuid, d::date,
		       round(o::numeric / 100, 8), round(h::numeric / 100, 8),
		       round(l::numeric / 100, 8), round(c::numeric / 100, 8),
		       NULL, v, $8, 'fixture', 'hash-' || $1::text || '-' || d, $9::uuid,
		       d::date::timestamptz, d::date::timestamptz
		FROM unnest($2::text[], $3::bigint[], $4::bigint[], $5::bigint[], $6::bigint[], $7::bigint[])
		     AS bars(d, o, h, l, c, v)`,
		instrument.String(), dates, opens, highs, lows, closes, volumes, currency, runID.String())
}

// insertBar stores one bar for one session under the given import run, at the given close
// (in cents), with a plausible range around it. It is how a later suite writes "a bar for
// session S" and then asks what changed.
func (f *engineFixture) insertBar(instrument, runID features.UUID, session features.SessionDate, closeCents, volume int64) {
	f.t.Helper()
	f.insertBars(instrument, runID, []string{session.String()},
		[]int64{closeCents}, []int64{closeCents + 100}, []int64{closeCents - 100}, []int64{closeCents}, []int64{volume})
}

// reviseBar replaces a stored bar's close the way an import revision does: the previous
// observation is preserved in price_bar_revisions under the superseding run, and the bar is
// re-stamped with that run.
func (f *engineFixture) reviseBar(instrument, supersedingRunID features.UUID, session features.SessionDate, closeCents int64) {
	f.t.Helper()
	f.exec(`INSERT INTO price_bar_revisions
		(id, instrument_id, session_date, revision, open, high, low, close, adjusted_close, volume,
		 currency, provider, source_hash, import_run_id, first_observed_at, last_observed_at,
		 superseding_run_id, superseded_at)
		SELECT gen_random_uuid(), instrument_id, session_date,
		       1 + (SELECT count(*) FROM price_bar_revisions r
		            WHERE r.instrument_id = b.instrument_id AND r.session_date = b.session_date),
		       open, high, low, close, adjusted_close, volume, currency, provider, source_hash,
		       import_run_id, first_observed_at, last_observed_at, $3, now()
		FROM daily_price_bars b WHERE instrument_id = $1 AND session_date = $2`,
		instrument.String(), session.String(), supersedingRunID.String())
	f.exec(`UPDATE daily_price_bars
		SET close = round($3::numeric / 100, 8),
		    high = greatest(high, round($3::numeric / 100, 8)),
		    low = least(low, round($3::numeric / 100, 8)),
		    import_run_id = $4, last_observed_at = now(),
		    source_hash = source_hash || '-revised'
		WHERE instrument_id = $1 AND session_date = $2`,
		instrument.String(), session.String(), closeCents, supersedingRunID.String())
}

// extendHistory stores bars for every open session after the instrument's last stored bar
// through the given session, continuing the generator where it left off, under the given
// import run. Later suites extend rather than rebuild, which is what makes a "no earlier
// value moved" assertion mean something.
func (f *engineFixture) extendHistory(instrument, runID features.UUID, through features.SessionDate) []features.SessionDate {
	f.t.Helper()
	var last string
	if err := f.pool.QueryRow(f.ctx, `SELECT max(session_date)::text FROM daily_price_bars
		WHERE instrument_id = $1`, instrument.String()).Scan(&last); err != nil {
		f.t.Fatalf("last stored session: %v", err)
	}
	all := f.openSessions(features.SessionDate(last), through)
	var sessions []features.SessionDate
	for _, session := range all {
		if session.String() > last {
			sessions = append(sessions, session)
		}
	}
	var dates []string
	var opens, highs, lows, closes, volumes []int64
	position := f.positions[instrument]
	for _, session := range sessions {
		bar := fixtureBar(fixtureSeed(instrument), position)
		position++
		dates = append(dates, session.String())
		opens = append(opens, bar.openCents)
		highs = append(highs, bar.highCents)
		lows = append(lows, bar.lowCents)
		closes = append(closes, bar.closeCents)
		volumes = append(volumes, bar.volume)
	}
	f.positions[instrument] = position
	if len(dates) > 0 {
		f.insertBars(instrument, runID, dates, opens, highs, lows, closes, volumes)
	}
	return sessions
}

// newImportRun records a succeeded import run a suite can stamp bars with.
func (f *engineFixture) newImportRun(id features.UUID) features.UUID {
	f.t.Helper()
	f.exec(`INSERT INTO import_runs (id, kind, provider, status, started_at, finished_at, app_version)
		VALUES ($1, 'daily_update', 'fixture', 'succeeded', now(), now(), 'test')`, id.String())
	return id
}

// truncateHistory removes an instrument's bars after a session and rewinds the generator so
// that extendHistory later regenerates exactly the bars removed. It is how a suite computes
// over history cut short at N and then extends it.
func (f *engineFixture) truncateHistory(instrument features.UUID, through features.SessionDate) {
	f.t.Helper()
	var kept int
	if err := f.pool.QueryRow(f.ctx, `WITH removed AS (
			DELETE FROM daily_price_bars WHERE instrument_id = $1 AND session_date > $2 RETURNING 1)
		SELECT count(*) FROM daily_price_bars WHERE instrument_id = $1 AND session_date <= $2`,
		instrument.String(), through.String()).Scan(&kept); err != nil {
		f.t.Fatalf("truncate history: %v", err)
	}
	// The generator counts positions over the stored sessions including skipped ones, so
	// rewind by the number of sessions removed, not the number of bars kept.
	removed := len(f.openSessions(through, fixtureAsOf)) - 1
	f.positions[instrument] -= removed
	if f.positions[instrument] < kept {
		f.t.Fatalf("truncate history: %d positions for %d bars", f.positions[instrument], kept)
	}
}

// snapshot copies the value and composite tables under a name so a later diff can compare.
func (f *engineFixture) snapshot(name string) {
	f.t.Helper()
	f.exec(fmt.Sprintf(`CREATE TABLE %s_values AS SELECT * FROM feature_values`, name))
	f.exec(fmt.Sprintf(`CREATE TABLE %s_composites AS SELECT * FROM universe_composites`, name))
}

// changedValues counts the values whose value, label, absence reason or currency differ from
// a snapshot, for rows present in both, matching an optional extra condition on v.
func (f *engineFixture) changedValues(name, where string, args ...any) int64 {
	f.t.Helper()
	if where == "" {
		where = "true"
	}
	var n int64
	if err := f.pool.QueryRow(f.ctx, fmt.Sprintf(`SELECT count(*) FROM feature_values v
		JOIN %s_values b USING (instrument_id, session_date, definition_id)
		JOIN feature_definitions d ON d.id = v.definition_id
		WHERE (%s) AND (v.value IS DISTINCT FROM b.value OR v.label IS DISTINCT FROM b.label
		   OR v.absence_reason IS DISTINCT FROM b.absence_reason OR v.currency IS DISTINCT FROM b.currency)`,
		name, where), args...).Scan(&n); err != nil {
		f.t.Fatalf("changed values: %v", err)
	}
	return n
}

// changedComposites counts composite sessions that differ from a snapshot in mean, count or
// absence, for rows present in both.
func (f *engineFixture) changedComposites(name, where string, args ...any) int64 {
	f.t.Helper()
	if where == "" {
		where = "true"
	}
	var n int64
	if err := f.pool.QueryRow(f.ctx, fmt.Sprintf(`SELECT count(*) FROM universe_composites c
		JOIN %s_composites b USING (universe_id, session_date, definition_id)
		WHERE (%s) AND (c.mean_return IS DISTINCT FROM b.mean_return
		   OR c.contributor_count IS DISTINCT FROM b.contributor_count
		   OR c.absence_reason IS DISTINCT FROM b.absence_reason)`, name, where), args...).Scan(&n); err != nil {
		f.t.Fatalf("changed composites: %v", err)
	}
	return n
}
