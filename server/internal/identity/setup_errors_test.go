package identity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/authtest"
	"market-lens/server/internal/marketdata"
)

const (
	shortPassword  = "eleven-char"
	goodPassword   = "correct horse battery staple"
	submittedEODHD = "submitted-eodhd-key"
	submittedSMTP  = "submitted-smtp" + "-never-persist-plaintext"
)

// validationOnlyService builds a service whose repository is never reached, because every case
// below is rejected before any persistence happens. That is itself part of the contract: a
// setup that cannot succeed must not touch the database or an external provider.
func validationOnlyService(t *testing.T, validator EODHDCredentialValidator, verifier SMTPVerifier) *Service {
	t.Helper()
	hasher, err := auth.NewPasswordHasher(authtest.NewRandomReader(0x19), auth.Argon2Params{
		Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	pattern := make([]byte, 251)
	for index := range pattern {
		pattern[index] = byte(index*13 + 7)
	}
	secrets, err := auth.NewSecrets(make([]byte, 32), authtest.NewRandomReader(pattern...))
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(ServiceDependencies{
		Repository: NewRepository(nil), Passwords: hasher, Secrets: secrets,
		Now:      authtest.NewClock(time.Date(2026, time.August, 31, 9, 0, 0, 0, time.UTC)).Now,
		SetupTTL: 15 * time.Minute, OwnerIdleTimeout: 8 * time.Hour,
		SessionAbsoluteTimeout: 30 * 24 * time.Hour,
		EODHDValidator:         validator, SMTPVerifier: verifier,
		CredentialCipher: nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func setupRequest(mutate func(*BootstrapRequest)) BootstrapRequest {
	request := BootstrapRequest{
		Capability: "capability-token", Email: "owner@example.com", Password: goodPassword,
		DisplayName: "Market Owner", DeviceLabel: "Test browser", Origin: "192.0.2.0/24",
		EODHDAPIKey: submittedEODHD,
		SMTP: SMTPSetupConfiguration{
			Host: fixtureMailHost, Port: 587, From: fixtureMailFrom,
			Username: fixtureMailUser, Password: submittedSMTP,
		},
	}
	if mutate != nil {
		mutate(&request)
	}
	return request
}

type passingValidator struct{ calls int }

func (v *passingValidator) ValidateCredential(context.Context, string) error { v.calls++; return nil }

type passingVerifier struct{ calls int }

func (v *passingVerifier) VerifySMTP(context.Context, SMTPSetupConfiguration) error {
	v.calls++
	return nil
}

// TestBootstrapReportsEveryBadFieldAtOnce is the behavior the setup form needs: one response
// that names each thing to fix, so the operator corrects them in a single pass.
func TestBootstrapReportsEveryBadFieldAtOnce(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*BootstrapRequest)
		wantFields map[string]string
	}{
		{
			name:       "password below the minimum",
			mutate:     func(r *BootstrapRequest) { r.Password = shortPassword },
			wantFields: map[string]string{"password": "12"},
		},
		{
			name:       "email without a domain",
			mutate:     func(r *BootstrapRequest) { r.Email = "owner@" },
			wantFields: map[string]string{"email": "email"},
		},
		{
			name:       "blank display name",
			mutate:     func(r *BootstrapRequest) { r.DisplayName = "   " },
			wantFields: map[string]string{"display_name": "name"},
		},
		{
			name:       "empty provider key",
			mutate:     func(r *BootstrapRequest) { r.EODHDAPIKey = "" },
			wantFields: map[string]string{"eodhd_api_key": "API key"},
		},
		{
			name:       "port outside the valid range",
			mutate:     func(r *BootstrapRequest) { r.SMTP.Port = 70000 },
			wantFields: map[string]string{"smtp_port": "65535"},
		},
		{
			name:       "blank mail host",
			mutate:     func(r *BootstrapRequest) { r.SMTP.Host = "" },
			wantFields: map[string]string{"smtp_host": "host"},
		},
		{
			name:       "sender that is not an address",
			mutate:     func(r *BootstrapRequest) { r.SMTP.From = "not-an-address" },
			wantFields: map[string]string{"smtp_from": "address"},
		},
		{
			name:       "a username with no password",
			mutate:     func(r *BootstrapRequest) { r.SMTP.Password = "" },
			wantFields: map[string]string{"smtp_password": "password"},
		},
		{
			name: "five mistakes at once are all reported",
			mutate: func(r *BootstrapRequest) {
				r.Password = shortPassword
				r.Email = "nope"
				r.DisplayName = ""
				r.SMTP.Port = 0
				r.SMTP.From = "bad"
			},
			wantFields: map[string]string{
				"password": "12", "email": "email", "display_name": "name",
				"smtp_port": "65535", "smtp_from": "address",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator, verifier := &passingValidator{}, &passingVerifier{}
			service := validationOnlyService(t, validator, verifier)

			_, err := service.BootstrapOwner(context.Background(), setupRequest(tt.mutate))
			var validation *SetupValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("error = %v, want a field-level setup validation error", err)
			}
			if validation.Unreachable {
				t.Error("a typing mistake was reported as an unreachable dependency")
			}
			assertFields(t, validation, tt.wantFields)

			// FR-003: a request that cannot succeed must not spend a provider call or open a
			// connection to somebody's mail server.
			if validator.calls != 0 || verifier.calls != 0 {
				t.Errorf("network checks ran despite local errors: provider=%d smtp=%d",
					validator.calls, verifier.calls)
			}
		})
	}
}

// A rejected provider key and an unreachable provider need opposite responses from the
// operator, so they must not look the same.
func TestBootstrapDistinguishesRejectedFromUnreachableDependencies(t *testing.T) {
	tests := []struct {
		name            string
		validatorErr    error
		verifierErr     error
		wantField       string
		wantCode        string
		wantUnreachable bool
		wantFragment    string
	}{
		{
			name: "provider rejects the key", validatorErr: errors.New("unauthorized"),
			wantField: "eodhd_api_key", wantCode: "rejected", wantFragment: "rejected",
		},
		{
			name: "provider cannot be reached", validatorErr: ErrProviderUnavailable,
			wantField: "eodhd_api_key", wantCode: "unreachable", wantUnreachable: true,
			wantFragment: "could not be reached",
		},
		{
			name:         "provider times out",
			validatorErr: &marketdata.ProviderError{Transient: true},
			wantField:    "eodhd_api_key", wantCode: "unreachable", wantUnreachable: true,
			wantFragment: "could not be reached",
		},
		{
			name: "mail server rejects the credentials", verifierErr: ErrSMTPAuthRejected,
			wantField: "smtp_password", wantCode: "auth_rejected", wantFragment: "rejected",
		},
		{
			name: "mail server cannot be reached", verifierErr: ErrSMTPUnreachable,
			wantField: "smtp_host", wantCode: "unreachable", wantUnreachable: true,
			wantFragment: "reach",
		},
		{
			name: "mail server refuses the sender", verifierErr: ErrSMTPSenderRejected,
			wantField: "smtp_from", wantCode: "sender_rejected", wantFragment: "sender",
		},
		{
			name: "mail server refuses TLS", verifierErr: ErrSMTPTLSFailed,
			wantField: "smtp_host", wantCode: "tls_failed", wantFragment: "encrypt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := validationOnlyService(t,
				validatorFunc(func(context.Context, string) error { return tt.validatorErr }),
				verifierFunc(func(context.Context, SMTPSetupConfiguration) error { return tt.verifierErr }))

			_, err := service.BootstrapOwner(context.Background(), setupRequest(nil))
			var validation *SetupValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("error = %v, want a field-level setup validation error", err)
			}
			if validation.Unreachable != tt.wantUnreachable {
				t.Errorf("unreachable = %t, want %t", validation.Unreachable, tt.wantUnreachable)
			}
			found := false
			for _, field := range validation.Fields {
				if field.Field != tt.wantField {
					continue
				}
				found = true
				if field.Code != tt.wantCode {
					t.Errorf("code for %q = %q, want %q", tt.wantField, field.Code, tt.wantCode)
				}
				if !strings.Contains(strings.ToLower(field.Message), tt.wantFragment) {
					t.Errorf("message for %q = %q, want it to mention %q",
						tt.wantField, field.Message, tt.wantFragment)
				}
			}
			if !found {
				t.Fatalf("no error reported for %q: %#v", tt.wantField, validation.Fields)
			}
			assertNoSubmittedSecrets(t, validation)
		})
	}
}

// Both external checks run in one pass, so a setup with two wrong values is reported once.
func TestBootstrapReportsProviderAndMailProblemsTogether(t *testing.T) {
	service := validationOnlyService(t,
		validatorFunc(func(context.Context, string) error { return errors.New("unauthorized") }),
		verifierFunc(func(context.Context, SMTPSetupConfiguration) error { return ErrSMTPAuthRejected }))

	_, err := service.BootstrapOwner(context.Background(), setupRequest(nil))
	var validation *SetupValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %v, want a field-level setup validation error", err)
	}
	assertFields(t, validation, map[string]string{
		"eodhd_api_key": "rejected", "smtp_username": "rejected", "smtp_password": "rejected",
	})
	assertNoSubmittedSecrets(t, validation)
}

func assertFields(t *testing.T, validation *SetupValidationError, want map[string]string) {
	t.Helper()
	seen := map[string]string{}
	for _, field := range validation.Fields {
		if field.Code == "" || field.Message == "" {
			t.Errorf("field %q carries an empty code or message", field.Field)
		}
		seen[field.Field] = field.Message
	}
	for name, fragment := range want {
		message, ok := seen[name]
		if !ok {
			t.Errorf("no error reported for %q; reported %v", name, keysOf(seen))
			continue
		}
		if !strings.Contains(strings.ToLower(message), strings.ToLower(fragment)) {
			t.Errorf("message for %q = %q, want it to mention %q", name, message, fragment)
		}
	}
	for name := range seen {
		if _, ok := want[name]; !ok {
			t.Errorf("unexpected error reported for %q: %q", name, seen[name])
		}
	}
	assertNoSubmittedSecrets(t, validation)
}

func assertNoSubmittedSecrets(t *testing.T, validation *SetupValidationError) {
	t.Helper()
	for _, field := range validation.Fields {
		for _, secret := range []string{goodPassword, shortPassword, submittedEODHD, submittedSMTP} {
			if strings.Contains(field.Message, secret) {
				t.Errorf("message for %q echoed a submitted secret: %q", field.Field, field.Message)
			}
		}
	}
}

func keysOf(values map[string]string) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	return names
}

type validatorFunc func(context.Context, string) error

func (fn validatorFunc) ValidateCredential(ctx context.Context, key string) error {
	return fn(ctx, key)
}

type verifierFunc func(context.Context, SMTPSetupConfiguration) error

func (fn verifierFunc) VerifySMTP(ctx context.Context, config SMTPSetupConfiguration) error {
	return fn(ctx, config)
}
