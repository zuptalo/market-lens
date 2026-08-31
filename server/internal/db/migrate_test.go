package db

import "testing"

func TestLoadMigrations(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 11 || migrations[0].version != 1 || migrations[0].name != "0001_baseline.sql" ||
		migrations[1].version != 2 || migrations[1].name != "0002_instruments.sql" ||
		migrations[2].version != 3 || migrations[2].name != "0003_nordic_universe.sql" ||
		migrations[3].version != 4 || migrations[3].name != "0004_market_data.sql" ||
		migrations[4].version != 5 || migrations[4].name != "0005_nordic_calendars.sql" ||
		migrations[5].version != 6 || migrations[5].name != "0006_client_events.sql" ||
		migrations[6].version != 7 || migrations[6].name != "0007_identity_access.sql" ||
		migrations[7].version != 8 || migrations[7].name != "0008_client_event_authorization.sql" ||
		migrations[8].version != 9 || migrations[8].name != "0009_external_credentials_and_owner_reset.sql" ||
		migrations[9].version != 10 || migrations[9].name != "0010_member_code_digest_scope.sql" ||
		migrations[10].version != 11 || migrations[10].name != "0011_instance_signing_key.sql" {
		t.Fatalf("unexpected migrations: %#v", migrations)
	}
}
