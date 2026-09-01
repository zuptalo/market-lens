package marketdata_test

import (
	"context"
	"testing"

	"market-lens/server/internal/marketdata"
)

// The audit's judgement of an instrument missing from the provider's catalog turns on whether
// it is *still* importing, so the universe it reads has to carry the newest session stored.
// Without it every absent instrument looks alike, and either a healthy one gets "corrected"
// into a broken one or a renamed one goes on looking fine because it holds a long history.
func TestUniverseEntriesCarryStoredHistoryAndItsNewestSession(t *testing.T) {
	pool := migratedPool(t)
	ctx := context.Background()
	repository := marketdata.NewRepository(pool)

	if _, err := pool.Exec(ctx, `INSERT INTO provider_instruments
		(provider,provider_symbol,instrument_id)
		SELECT 'fixture','FIXTURE-' || i.ticker,i.id
		FROM research_universes u
		JOIN universe_memberships m ON m.universe_id=u.id AND m.included_to IS NULL
		JOIN instruments i ON i.id=m.instrument_id AND i.active
		WHERE u.code='nordic-liquid-v1' AND u.active`); err != nil {
		t.Fatal(err)
	}

	targets, err := repository.TargetsForUniverse(ctx, "fixture", "nordic-liquid-v1")
	if err != nil {
		t.Fatal(err)
	}
	var runID string
	if err := pool.QueryRow(ctx, `INSERT INTO import_runs
		(id,kind,provider,status,started_at,finished_at,processed_count,accepted_count,app_version)
		VALUES (gen_random_uuid(),'backfill','fixture','succeeded',now(),now(),2,2,'test') RETURNING id::text`).
		Scan(&runID); err != nil {
		t.Fatal(err)
	}
	// One instrument gets two bars; every other one gets none.
	withBars := targets[0].ProviderSymbol
	for _, sessionDate := range []string{"2024-04-02", "2024-04-03"} {
		if _, err := pool.Exec(ctx, `INSERT INTO daily_price_bars
			(instrument_id,session_date,open,high,low,close,adjusted_close,volume,currency,provider,source_hash,import_run_id,first_observed_at,last_observed_at)
			VALUES ($1,$2,10,10,10,10,10,100,'SEK','fixture',$3,$4,now(),now())`,
			targets[0].InstrumentID, sessionDate, "hash-"+sessionDate, runID); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := repository.UniverseEntries(ctx, "fixture", "nordic-liquid-v1")
	if err != nil {
		t.Fatal(err)
	}
	var checked int
	for _, entry := range entries {
		if entry.ProviderSymbol == withBars {
			checked++
			if entry.StoredBars != 2 || entry.LastSession != "2024-04-03" {
				t.Errorf("%s stored bars = %d, last session = %q; want 2 and 2024-04-03",
					entry.Ticker, entry.StoredBars, entry.LastSession)
			}
			continue
		}
		if entry.StoredBars != 0 || entry.LastSession != "" {
			t.Errorf("%s stored bars = %d, last session = %q; want 0 and empty",
				entry.Ticker, entry.StoredBars, entry.LastSession)
		}
	}
	if checked != 1 {
		t.Fatalf("the instrument with bars was not among the %d entries returned", len(entries))
	}
}
