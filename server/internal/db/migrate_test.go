package db

import (
	"testing"
)

func TestLoadMigrations(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 13 || migrations[0].version != 1 || migrations[0].name != "0001_baseline.sql" ||
		migrations[1].version != 2 || migrations[1].name != "0002_instruments.sql" ||
		migrations[2].version != 3 || migrations[2].name != "0003_nordic_universe.sql" ||
		migrations[3].version != 4 || migrations[3].name != "0004_market_data.sql" ||
		migrations[4].version != 5 || migrations[4].name != "0005_nordic_calendars.sql" ||
		migrations[5].version != 6 || migrations[5].name != "0006_client_events.sql" ||
		migrations[6].version != 7 || migrations[6].name != "0007_identity_access.sql" ||
		migrations[7].version != 8 || migrations[7].name != "0008_client_event_authorization.sql" ||
		migrations[8].version != 9 || migrations[8].name != "0009_external_credentials_and_owner_reset.sql" ||
		migrations[9].version != 10 || migrations[9].name != "0010_member_code_digest_scope.sql" ||
		migrations[10].version != 11 || migrations[10].name != "0011_instance_signing_key.sql" ||
		migrations[11].version != 12 || migrations[11].name != "0012_stale_provider_symbols.sql" ||
		migrations[12].version != 13 || migrations[12].name != "0013_one_open_finding_per_condition.sql" {
		t.Fatalf("unexpected migrations: %#v", migrations)
	}
}

// Two migrations sharing a version number is silent data loss: the runner records the version
// once, and whichever file it did not apply never runs — on a clean install or an upgrade,
// with nothing to show that anything was skipped.
//
// It happened here. A merge brought 0011_instance_signing_key.sql onto the branch while
// 0011_stale_provider_symbols.sql was being written, and the second simply did not run. The
// only symptom was a test failing for what looked like the wrong reason.
func TestDuplicateMigrationVersionsAreRefused(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]string{}
	for _, m := range migrations {
		if existing, clash := seen[m.version]; clash {
			t.Errorf("migrations %s and %s both claim version %d, so one of them never runs",
				existing, m.name, m.version)
		}
		seen[m.version] = m.name
	}
}
