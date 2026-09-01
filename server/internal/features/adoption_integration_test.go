package features_test

import (
	"testing"

	"market-lens/server/internal/features"
	"market-lens/server/internal/instruments"
)

// SC-010: the number on the Markets list and the number the features endpoint serves are the
// same number, for every instrument, because they are the same stored row. This walks the
// listing page by page — the way a person scrolls it — and compares each row's three adopted
// statistics against what the engine holds for that instrument at its latest stored session.
func TestMarketsAgreesWithTheEngineForEveryInstrument(t *testing.T) {
	f := newEngineFixture(t)
	computeFixture(t, f, 4)

	listings := instruments.NewRepository(f.pool)
	engine := features.NewRepository(f.pool)
	adopted := map[string]func(instruments.ListingRow) *instruments.Decimal{
		"return_20":     func(row instruments.ListingRow) *instruments.Decimal { return row.Return20 },
		"return_90":     func(row instruments.ListingRow) *instruments.Decimal { return row.Return90 },
		"volatility_20": func(row instruments.ListingRow) *instruments.Decimal { return row.Volatility },
	}

	cursor, rows, pages := "", 0, 0
	for {
		page, err := listings.Listing(f.ctx, instruments.ListingFilter{
			Sort: instruments.SortName, Limit: 5, Cursor: cursor, AsOf: instruments.SessionDate(fixtureAsOf),
		})
		if err != nil {
			t.Fatalf("listing page %d: %v", pages, err)
		}
		pages++
		for _, row := range page.Items {
			rows++
			if row.LatestSession == "" {
				// No stored history at all: there is nothing for the engine to have computed.
				for name, read := range adopted {
					if read(row) != nil {
						t.Errorf("%s lists %s = %s but has no stored session", row.Ticker, name, read(row))
					}
				}
				continue
			}
			set, err := engine.ReadAsOf(f.ctx, features.UUID(row.ID), features.SessionDate(row.LatestSession))
			if err != nil {
				t.Fatalf("read %s as of %s: %v", row.Ticker, row.LatestSession, err)
			}
			stored := map[string]*string{}
			for _, value := range set.Features {
				stored[value.Name] = value.Value
			}
			for name, read := range adopted {
				listed, engineValue := read(row), stored[name]
				switch {
				case listed == nil && engineValue == nil:
				case listed == nil || engineValue == nil:
					t.Errorf("%s: the listing says %v and the engine says %v for %s",
						row.Ticker, listed, engineValue, name)
				case listed.String() != *engineValue:
					t.Errorf("%s: the listing says %s and the engine says %s for %s",
						row.Ticker, listed, *engineValue, name)
				}
			}
		}
		if cursor = page.NextCursor; cursor == "" {
			break
		}
	}
	if rows < fixtureMemberCount || pages < 2 {
		t.Fatalf("walked %d rows over %d pages; the fixture holds at least %d instruments",
			rows, pages, fixtureMemberCount)
	}
}
