package testdb

import (
	"context"
	"regexp"
	"testing"
)

func TestSchemaNameIsSafeIdentifier(t *testing.T) {
	name := schemaName(t)
	if !regexp.MustCompile(`^market_lens_test_[0-9a-f]{16}$`).MatchString(name) {
		t.Fatalf("unsafe schema name %q", name)
	}
}

func TestOpenUsesIsolatedSchema(t *testing.T) {
	pool := Open(t)

	var schema string
	if err := pool.QueryRow(context.Background(), "SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^market_lens_test_[0-9a-f]{16}$`).MatchString(schema) {
		t.Fatalf("unexpected current schema %q", schema)
	}
}
