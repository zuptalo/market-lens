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

// TestMaxReachIsBoundedAndRefused covers how far back the scheduled pass may reach on account of
// a data quality finding. Refused rather than clamped, for the same reason as the re-observation
// window: an operator who sets a value believing it covers a decade must find out that it does not.
func TestMaxReachIsBoundedAndRefused(t *testing.T) {
	// The default has to be able to reach a finding raised against the oldest history the
	// product stores, because a finding drives the reach exactly once: the pass that examines it
	// is also the last pass that widens for it. A bound that cannot reach the real ones does not
	// save a wide night — it spends a narrower one every night for ever and never settles
	// anything, which is what a year-long default did in production for fifteen instruments.
	t.Run("reaches as far as the stored history goes", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://example")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if cfg.MarketData.MaxReachSessions < 2500 {
			t.Fatalf("default reach is %d sessions; a decade of stored history is about 2,500, "+
				"and a finding older than the reach can never be examined",
				cfg.MarketData.MaxReachSessions)
		}
	})
	for _, accepted := range []string{"1", "260", "2600"} {
		t.Run("accepts "+accepted, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://example")
			t.Setenv("MARKET_DATA_MAX_REACH_SESSIONS", accepted)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("%s was refused: %v", accepted, err)
			}
			if got := strconv.Itoa(cfg.MarketData.MaxReachSessions); got != accepted {
				t.Fatalf("configured %s, read %s", accepted, got)
			}
		})
	}
	for _, refused := range []string{"0", "-1", "2601", "many", ""} {
		t.Run("refuses "+refused, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://example")
			t.Setenv("MARKET_DATA_MAX_REACH_SESSIONS", refused)
			if _, err := Load(); err == nil {
				t.Fatalf("%q was accepted; an out-of-range reach must be refused, not clamped", refused)
			} else if !strings.Contains(err.Error(), "MARKET_DATA_MAX_REACH_SESSIONS") {
				t.Fatalf("the error does not name the setting: %v", err)
			}
		})
	}
}

// Behind the ingress this value is the difference between recording every device's own address and
// recording Traefik's, identically, for all of them (feature 028, D1).
func TestLoadParsesTrustedProxies(t *testing.T) {
	t.Setenv("PORT", "8081")
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("TRUSTED_PROXIES", " 10.42.0.0/16 , 10.43.0.7 ")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.TrustedProxies) != 2 ||
		cfg.TrustedProxies[0].String() != "10.42.0.0/16" || cfg.TrustedProxies[1].String() != "10.43.0.7/32" {
		t.Fatalf("unexpected trusted proxies: %v", cfg.TrustedProxies)
	}
}

func TestTrustedProxiesDefaultToTrustingNothing(t *testing.T) {
	t.Setenv("PORT", "8081")
	t.Setenv("DATABASE_URL", "postgres://example")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.TrustedProxies) != 0 {
		t.Fatalf("nothing should be trusted by default, got %v", cfg.TrustedProxies)
	}
}

// Refused rather than quietly ignored. A trust list that silently failed to parse produces a
// screen full of plausible internal addresses that nobody would ever question (FR-006).
func TestLoadRefusesAMalformedTrustedProxy(t *testing.T) {
	t.Setenv("PORT", "8081")
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("TRUSTED_PROXIES", "10.42.0.0/16,the-ingress")
	_, err := Load()
	if err == nil {
		t.Fatal("expected a malformed trusted proxy to stop startup")
	}
	if !strings.Contains(err.Error(), "TRUSTED_PROXIES") || !strings.Contains(err.Error(), "the-ingress") {
		t.Fatalf("error must name the variable and the entry that failed: %v", err)
	}
}
