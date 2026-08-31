package config

import (
	"encoding/base64"
	"reflect"
	"strings"
	"testing"
)

func TestLoadParsesExternalCredentialKeyAndVersion(t *testing.T) {
	setValidAuthEnvironment(t)
	key := []byte("0123456789abcdef0123456789abcdef")
	t.Setenv("EXTERNAL_CREDENTIAL_KEY", base64.StdEncoding.EncodeToString(key))
	t.Setenv("EXTERNAL_CREDENTIAL_KEY_VERSION", "7")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	external := requiredStructField(t, reflect.ValueOf(cfg), "ExternalCredentials")
	assertConfigField(t, external, "Key", key)
	assertConfigField(t, external, "KeyVersion", uint32(7))
	assertConfigField(t, external, "Configured", true)
}

func TestLoadProductionRequiresValidExternalCredentialKeyAndVersion(t *testing.T) {
	secretValue := "external-key-secret-never-log"
	tests := []struct {
		name    string
		key     string
		version string
	}{
		{name: "missing", version: "1"},
		{name: "not base64", key: secretValue, version: "1"},
		{name: "short decoded key", key: base64.StdEncoding.EncodeToString([]byte("short")), version: "1"},
		{name: "zero version", key: base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")), version: "0"},
		{name: "negative version", key: base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")), version: "-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setValidAuthEnvironment(t)
			t.Setenv("ENV", "production")
			t.Setenv("EXTERNAL_CREDENTIAL_KEY", tt.key)
			t.Setenv("EXTERNAL_CREDENTIAL_KEY_VERSION", tt.version)
			if _, err := Load(); err == nil {
				t.Fatal("invalid external credential configuration unexpectedly succeeded")
			} else if strings.Contains(err.Error(), secretValue) || strings.Contains(err.Error(), tt.key) && tt.key != "" {
				t.Fatalf("configuration error disclosed credential key: %v", err)
			}
		})
	}
}

func TestLoadDevelopmentAllowsUnconfiguredExternalCredentialKey(t *testing.T) {
	setValidAuthEnvironment(t)
	t.Setenv("EXTERNAL_CREDENTIAL_KEY", "")
	t.Setenv("EXTERNAL_CREDENTIAL_KEY_VERSION", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	external := requiredStructField(t, reflect.ValueOf(cfg), "ExternalCredentials")
	assertConfigField(t, external, "Configured", false)
}
