package config

import "testing"

func TestLoadRejectsExplicitEmptyPort(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("DATABASE_URL", "postgres://example")
	if _, err := Load(); err == nil {
		t.Fatal("expected an empty explicit PORT to fail")
	}
}

func TestLoadParsesOrigins(t *testing.T) {
	t.Setenv("PORT", "8081")
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("ALLOWED_ORIGINS", "http://localhost:5173, https://example.test ")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AllowedOrigins) != 2 || cfg.AllowedOrigins[1] != "https://example.test" {
		t.Fatalf("unexpected origins: %#v", cfg.AllowedOrigins)
	}
}

func TestProductionAliases(t *testing.T) {
	for _, environment := range []string{"production", "prod"} {
		if !(Config{Environment: environment}).IsProduction() {
			t.Fatalf("%q should be production", environment)
		}
	}
}
