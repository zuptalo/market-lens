package config

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

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

func TestLoadParsesMarketDataConfiguration(t *testing.T) {
	t.Setenv("MARKET_DATA_PROVIDER", "eodhd")
	t.Setenv("EODHD_API_TOKEN", "test-token-never-log")
	t.Setenv("MARKET_DATA_SCHEDULE_ENABLED", "true")
	t.Setenv("MARKET_DATA_DAILY_TIME", "20:15")
	t.Setenv("MARKET_DATA_DAILY_TIMEZONE", "Europe/Stockholm")
	t.Setenv("MARKET_DATA_REQUEST_TIMEOUT", "45s")
	t.Setenv("MARKET_DATA_MAX_RETRIES", "4")
	t.Setenv("MARKET_DATA_WORKERS", "3")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	marketData := reflect.ValueOf(cfg).FieldByName("MarketData")
	if !marketData.IsValid() {
		t.Fatal("market data configuration is absent")
	}
	assertField(t, marketData, "Provider", "eodhd")
	assertField(t, marketData, "APIToken", "test-token-never-log")
	assertField(t, marketData, "ScheduleEnabled", true)
	assertField(t, marketData, "DailyHour", 20)
	assertField(t, marketData, "DailyMinute", 15)
	assertField(t, marketData, "RequestTimeout", 45*time.Second)
	assertField(t, marketData, "MaxRetries", 4)
	assertField(t, marketData, "Workers", 3)

	location := marketData.FieldByName("DailyLocation")
	if !location.IsValid() || location.IsNil() || location.Interface().(*time.Location).String() != "Europe/Stockholm" {
		t.Fatalf("unexpected daily location: %v", location)
	}
}

func TestLoadAllowsReadOnlyModeWithoutProviderToken(t *testing.T) {
	t.Setenv("EODHD_API_TOKEN", "")
	if _, err := Load(); err != nil {
		t.Fatalf("read-only mode should load without a provider token: %v", err)
	}
}

func TestLoadRejectsInvalidMarketDataSettings(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "schedule", key: "MARKET_DATA_SCHEDULE_ENABLED", value: "sometimes"},
		{name: "daily time", key: "MARKET_DATA_DAILY_TIME", value: "25:00"},
		{name: "timezone", key: "MARKET_DATA_DAILY_TIMEZONE", value: "Mars/Olympus"},
		{name: "timeout", key: "MARKET_DATA_REQUEST_TIMEOUT", value: "forever"},
		{name: "retries", key: "MARKET_DATA_MAX_RETRIES", value: "many"},
		{name: "workers", key: "MARKET_DATA_WORKERS", value: "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.key, tt.value)
			if _, err := Load(); err == nil {
				t.Fatalf("expected %s=%q to fail", tt.key, tt.value)
			}
		})
	}
}

func assertField(t *testing.T, value reflect.Value, name string, want any) {
	t.Helper()
	field := value.FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("market data field %s is absent", name)
	}
	if got := field.Interface(); !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %#v, want %#v", name, got, want)
	}
}

// TestReobserveSessionsIsBoundedAndRefused covers the window a routine pass re-observes.
//
// Refusing an out-of-range value rather than clamping it is the point. An operator who sets 500
// believing they have covered a quarter's corrections would, under clamping, silently be covering
// three months instead — and would have no way to find out short of reading the source.
func TestReobserveSessionsIsBoundedAndRefused(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")

	t.Run("defaults to five sessions", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://example")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if cfg.MarketData.ReobserveSessions != 5 {
			t.Fatalf("default window is %d sessions, wanted 5", cfg.MarketData.ReobserveSessions)
		}
	})

	for _, accepted := range []string{"1", "5", "20", "60"} {
		t.Run("accepts "+accepted, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://example")
			t.Setenv("MARKET_DATA_REOBSERVE_SESSIONS", accepted)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("%s was refused: %v", accepted, err)
			}
			if got := strconv.Itoa(cfg.MarketData.ReobserveSessions); got != accepted {
				t.Fatalf("configured %s, read %s", accepted, got)
			}
		})
	}

	for _, refused := range []string{"0", "-1", "61", "500", "five", "5.5", ""} {
		t.Run("refuses "+refused, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://example")
			t.Setenv("MARKET_DATA_REOBSERVE_SESSIONS", refused)
			_, err := Load()
			if err == nil {
				t.Fatalf("%q was accepted; an out-of-range window must be refused, not clamped", refused)
			}
			if !strings.Contains(err.Error(), "MARKET_DATA_REOBSERVE_SESSIONS") {
				t.Fatalf("the error does not name the setting: %v", err)
			}
		})
	}
}
