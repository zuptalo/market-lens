package db

import "testing"

func TestLoadMigrations(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 1 || migrations[0].version != 1 || migrations[0].name != "0001_baseline.sql" {
		t.Fatalf("unexpected migrations: %#v", migrations)
	}
}
