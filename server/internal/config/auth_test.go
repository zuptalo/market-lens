package config

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

const authTestSecret = "0123456789abcdef0123456789abcdef"

func TestLoadParsesAuthenticationConfiguration(t *testing.T) {
	setValidAuthEnvironment(t)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	auth := requiredStructField(t, reflect.ValueOf(cfg), "Auth")
	assertConfigField(t, auth, "Secret", authTestSecret)
	assertConfigField(t, auth, "SecureCookies", true)
	assertConfigField(t, auth, "OwnerIdleTimeout", 8*time.Hour)
	assertConfigField(t, auth, "SessionAbsoluteTimeout", 30*24*time.Hour)
	assertConfigField(t, auth, "SetupTTL", 15*time.Minute)
	assertConfigField(t, auth, "MemberCodeTTL", 10*time.Minute)
	assertConfigField(t, auth, "MemberTempBlock", 15*time.Minute)
}

func TestLoadResolvesTheApplicationBaseURLForAccountEmailLinks(t *testing.T) {
	setValidAuthEnvironment(t)

	// Invitation and recovery emails carry absolute links, so a base URL is required. When the
	// operator does not set one, the first allowed origin is the only safe inference.
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	auth := requiredStructField(t, reflect.ValueOf(cfg), "Auth")
	assertConfigField(t, auth, "AppBaseURL", "https://market-lens.example")

	t.Setenv("APP_BASE_URL", "https://lens.example.test/")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	// A trailing slash must not produce a double slash in the emailed link.
	assertConfigField(t, requiredStructField(t, reflect.ValueOf(cfg), "Auth"), "AppBaseURL", "https://lens.example.test")

	for _, invalid := range []string{"not-a-url", "ftp://lens.example.test", "https://", "javascript:alert(1)"} {
		t.Setenv("APP_BASE_URL", invalid)
		if _, err := Load(); err == nil {
			t.Fatalf("APP_BASE_URL %q was accepted", invalid)
		}
	}
}

func TestLoadProductionFailsClosedWithoutStrongSecretOrSecureCookies(t *testing.T) {
	tests := []struct {
		name          string
		secret        string
		secureCookies string
	}{
		{name: "missing secret", secret: "", secureCookies: "true"},
		{name: "short secret", secret: "too-short", secureCookies: "true"},
		{name: "insecure cookies", secret: authTestSecret, secureCookies: "false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setValidAuthEnvironment(t)
			t.Setenv("ENV", "production")
			t.Setenv("AUTH_SECRET", tt.secret)
			t.Setenv("AUTH_SECURE_COOKIES", tt.secureCookies)

			if _, err := Load(); err == nil {
				t.Fatal("production configuration unexpectedly succeeded")
			} else if strings.Contains(err.Error(), tt.secret) && tt.secret != "" {
				t.Fatalf("configuration error disclosed a secret: %v", err)
			}
		})
	}
}

func TestLoadRejectsInvalidAuthenticationSettingsWithoutDisclosingSecrets(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "secure cookie boolean", key: "AUTH_SECURE_COOKIES", value: "sometimes"},
		{name: "idle timeout", key: "AUTH_OWNER_IDLE_TIMEOUT", value: "0s"},
		{name: "absolute timeout", key: "AUTH_SESSION_ABSOLUTE_TIMEOUT", value: "7h"},
		{name: "setup ttl", key: "AUTH_SETUP_TTL", value: "invalid"},
		{name: "member code ttl", key: "AUTH_MEMBER_CODE_TTL", value: "0s"},
		{name: "temporary block", key: "AUTH_MEMBER_TEMP_BLOCK", value: "none"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setValidAuthEnvironment(t)
			t.Setenv(tt.key, tt.value)

			if _, err := Load(); err == nil {
				t.Fatalf("%s=%q unexpectedly succeeded", tt.key, tt.value)
			} else if strings.Contains(err.Error(), authTestSecret) {
				t.Fatalf("configuration error disclosed a secret: %v", err)
			}
		})
	}
}

func setValidAuthEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("ENV", "development")
	t.Setenv("AUTH_SECRET", authTestSecret)
	t.Setenv("AUTH_SECURE_COOKIES", "true")
	t.Setenv("AUTH_OWNER_IDLE_TIMEOUT", "8h")
	t.Setenv("AUTH_SESSION_ABSOLUTE_TIMEOUT", "720h")
	t.Setenv("AUTH_SETUP_TTL", "15m")
	t.Setenv("AUTH_MEMBER_CODE_TTL", "10m")
	t.Setenv("AUTH_MEMBER_TEMP_BLOCK", "15m")
	t.Setenv("ALLOWED_ORIGINS", "https://market-lens.example,https://second.example")
	t.Setenv("APP_BASE_URL", "")
}

func requiredStructField(t *testing.T, value reflect.Value, name string) reflect.Value {
	t.Helper()
	field := value.FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("configuration field %s is absent", name)
	}
	if field.Kind() != reflect.Struct {
		t.Fatalf("configuration field %s is %s, want struct", name, field.Kind())
	}
	return field
}

func assertConfigField(t *testing.T, value reflect.Value, name string, want any) {
	t.Helper()
	field := value.FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("configuration field %s is absent", name)
	}
	if got := field.Interface(); !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %#v, want %#v", name, got, want)
	}
}
