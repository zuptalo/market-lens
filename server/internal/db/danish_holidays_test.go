package db_test

import (
	"context"
	"testing"

	"market-lens/server/internal/db"
	"market-lens/server/internal/testdb"
)

// Copenhagen's calendar was missing two closures, and every instrument on the exchange was
// reported as missing data on each of them.
//
// That is the failure the product most needs not to make: its central claim is that a day the
// exchange was closed is never reported as a missing session. An incomplete calendar breaks
// that claim silently, because a holiday looks exactly like a hole in the data.
//
// Both were found by the same evidence rather than by memory: on each date every one of the 25
// Copenhagen instruments was flagged, and a provider does not lose 25 companies on one day.
func TestCopenhagenIsClosedOnStoreBededagAndTheDayAfterAscension(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	for _, closure := range []struct {
		date   string
		reason string
	}{
		// Store Bededag, the fourth Friday after Easter. Denmark abolished it from 2024.
		{"2017-05-12", "Store Bededag"},
		{"2019-05-17", "Store Bededag"},
		{"2021-04-30", "Store Bededag"},
		{"2023-05-05", "Store Bededag, the last year it was observed"},
		// The Friday after Ascension, every year.
		{"2017-05-26", "day after Ascension"},
		{"2020-05-22", "day after Ascension"},
		{"2024-05-10", "day after Ascension"},
		{"2026-05-15", "day after Ascension"},
	} {
		var status string
		if err := pool.QueryRow(ctx, `SELECT s.status FROM exchange_sessions s
			JOIN exchanges e ON e.id = s.exchange_id
			WHERE e.mic = 'XCSE' AND s.session_date = $1`, closure.date).Scan(&status); err != nil {
			t.Fatalf("%s (%s): %v", closure.date, closure.reason, err)
		}
		if status != "closed" {
			t.Errorf("%s (%s) is %q, expected closed", closure.date, closure.reason, status)
		}
	}
}

// Store Bededag was abolished, so from 2024 it is an ordinary trading day. Closing it anyway
// would hide real sessions, which is the same error in the opposite direction.
func TestStoreBededagIsATradingDayOnceDenmarkAbolishedIt(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	// Easter 2024 was 31 March, so the fourth Friday after it is 26 April.
	for _, date := range []string{"2024-04-26", "2025-05-16"} {
		var status string
		if err := pool.QueryRow(ctx, `SELECT s.status FROM exchange_sessions s
			JOIN exchanges e ON e.id = s.exchange_id
			WHERE e.mic = 'XCSE' AND s.session_date = $1`, date).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status == "closed" {
			t.Errorf("%s is closed, but Store Bededag has not been observed since 2023", date)
		}
	}
}

// The other exchanges keep their calendars: this correction is Copenhagen's alone.
func TestTheOtherNordicCalendarsAreUnchanged(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	for _, mic := range []string{"XSTO", "XHEL", "XOSL"} {
		var open int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM exchange_sessions s
			JOIN exchanges e ON e.id = s.exchange_id
			WHERE e.mic = $1 AND s.status = 'open'
			  AND s.session_date IN ('2017-05-26','2021-04-30','2024-05-10')`, mic).Scan(&open); err != nil {
			t.Fatal(err)
		}
		if open == 0 {
			t.Errorf("%s lost sessions to a Danish-only correction", mic)
		}
	}
}
