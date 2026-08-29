package db_test

import (
	"context"
	"testing"

	"market-lens/server/internal/db"
	"market-lens/server/internal/testdb"
)

func TestNordicCalendarsCoverHistoricalWindowAndNextCompleteYear(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	rows, err := pool.Query(ctx, `SELECT e.mic, count(*), min(s.session_date)::text, max(s.session_date)::text,
		bool_and(s.source_reference LIKE 'https://%'),
		bool_and(s.status='closed' OR ((s.opens_at AT TIME ZONE e.timezone)::date=s.session_date AND
			(s.closes_at AT TIME ZONE e.timezone)::date=s.session_date AND s.closes_at>s.opens_at))
		FROM exchange_sessions s JOIN exchanges e ON e.id=s.exchange_id
		GROUP BY e.mic ORDER BY e.mic`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := 0
	for rows.Next() {
		var mic, first, last string
		var count int
		var sourced, timezoneCorrect bool
		if err := rows.Scan(&mic, &count, &first, &last, &sourced, &timezoneCorrect); err != nil {
			t.Fatal(err)
		}
		found++
		if count < 2900 || first > "2016-08-29" || last < "2027-12-31" || !sourced || !timezoneCorrect {
			t.Errorf("%s calendar: count=%d range=%s..%s sourced=%t timezone=%t", mic, count, first, last, sourced, timezoneCorrect)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if found != 4 {
		t.Fatalf("calendar MICs = %d, want 4", found)
	}
}

func TestNordicCalendarsRecordVenueClosuresAndHalfDays(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	tests := []struct{ mic, date, status string }{
		{"XSTO", "2026-06-19", "closed"},
		{"XCSE", "2026-12-24", "closed"},
		{"XHEL", "2026-12-24", "closed"},
		{"XOSL", "2026-05-14", "closed"},
		{"XSTO", "2026-01-05", "half_day"},
	}
	for _, tt := range tests {
		var status string
		if err := pool.QueryRow(ctx, `SELECT s.status FROM exchange_sessions s JOIN exchanges e ON e.id=s.exchange_id
			WHERE e.mic=$1 AND s.session_date=$2`, tt.mic, tt.date).Scan(&status); err != nil {
			t.Errorf("%s %s: %v", tt.mic, tt.date, err)
			continue
		}
		if status != tt.status {
			t.Errorf("%s %s status=%q, want %q", tt.mic, tt.date, status, tt.status)
		}
	}
}
