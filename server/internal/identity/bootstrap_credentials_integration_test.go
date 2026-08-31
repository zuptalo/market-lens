package identity

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"market-lens/server/internal/authtest"
	"market-lens/server/internal/db"
	"market-lens/server/internal/testdb"
)

const (
	testEODHDKey   = "eodhd-api-key-never-persist-plaintext"
	testSMTPSecret = "smtp-password-never-persist-plaintext"
)

type credentialValidatorStub struct {
	mu         sync.Mutex
	err        error
	calls      int
	key        string
	onValidate func()
}

func (stub *credentialValidatorStub) ValidateCredential(_ context.Context, key string) error {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.calls++
	stub.key = key
	if stub.onValidate != nil {
		stub.onValidate()
	}
	return stub.err
}

func TestBootstrapCredentialsRejectProviderFailureWithoutConsumingSetup(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
	}{
		{name: "invalid entitlement", err: ErrProviderCredentialInvalid},
		{name: "provider unavailable", err: ErrProviderUnavailable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pool := testdb.Open(t)
			if err := db.Migrate(context.Background(), pool); err != nil {
				t.Fatal(err)
			}
			validator := &credentialValidatorStub{err: tt.err}
			service, _ := newBootstrapServiceWithValidator(t, NewRepository(pool), validator)
			setup, err := service.IssueSetupCapability(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			_, err = service.BootstrapOwner(context.Background(), credentialBootstrapRequest(setup.Token))
			if !errors.Is(err, tt.err) {
				t.Fatalf("bootstrap error = %v, want %v", err, tt.err)
			}
			var owners, usableCapabilities int
			if err := pool.QueryRow(context.Background(), `SELECT
				(SELECT count(*) FROM users WHERE role='owner'),
				(SELECT count(*) FROM auth_capabilities WHERE kind='owner_setup' AND consumed_at IS NULL AND revoked_at IS NULL)
			`).Scan(&owners, &usableCapabilities); err != nil {
				t.Fatal(err)
			}
			if owners != 0 || usableCapabilities != 1 {
				t.Fatalf("failed validation persisted owners=%d usable_capabilities=%d", owners, usableCapabilities)
			}
			if validator.calls != 1 || validator.key != testEODHDKey {
				t.Fatalf("validator calls=%d key=%q", validator.calls, validator.key)
			}
		})
	}
}

func TestBootstrapCredentialsRechecksCapabilityAfterProviderValidation(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	validator := &credentialValidatorStub{}
	service, clock := newBootstrapServiceWithValidator(t, NewRepository(pool), validator)
	validator.onValidate = func() { clock.Advance(16 * time.Minute) }
	setup, err := service.IssueSetupCapability(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.BootstrapOwner(context.Background(), credentialBootstrapRequest(setup.Token))
	if !errors.Is(err, ErrCapabilityUnavailable) {
		t.Fatalf("expired-after-validation bootstrap error = %v", err)
	}
	var owners int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM users`).Scan(&owners); err != nil {
		t.Fatal(err)
	}
	if owners != 0 {
		t.Fatalf("expired-after-validation bootstrap created %d owners", owners)
	}
}

func TestBootstrapCredentialsCommitEncryptedProviderConfigurationAtomically(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	validator := &credentialValidatorStub{}
	service, _ := newBootstrapServiceWithValidator(t, NewRepository(pool), validator)
	setup, err := service.IssueSetupCapability(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.BootstrapOwner(context.Background(), credentialBootstrapRequest(setup.Token)); err != nil {
		t.Fatal(err)
	}

	var owners, credentials, sessions, audits, events int
	var persisted string
	if err := pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM users WHERE role='owner'),
		(SELECT count(*) FROM external_service_credentials WHERE kind IN ('eodhd_api','smtp')),
		(SELECT count(*) FROM sessions),
		(SELECT count(*) FROM security_audit_events WHERE event_type='owner.setup.v1'),
		(SELECT count(*) FROM client_events WHERE event_type IN ('account.changed.v1','setup.changed.v1')),
		coalesce((SELECT string_agg(encode(ciphertext, 'escape'), '') FROM external_service_credentials), '')
	`).Scan(&owners, &credentials, &sessions, &audits, &events, &persisted); err != nil {
		t.Fatal(err)
	}
	if owners != 1 || credentials != 2 || sessions != 1 || audits != 1 || events != 2 {
		t.Fatalf("atomic setup counts owner=%d credentials=%d sessions=%d audits=%d events=%d", owners, credentials, sessions, audits, events)
	}
	for _, secret := range []string{testEODHDKey, testSMTPSecret, "smtp.example.test", "access@example.test"} {
		if strings.Contains(persisted, secret) {
			t.Fatalf("persisted credential ciphertext disclosed %q", secret)
		}
	}
}

func newBootstrapServiceWithValidator(t *testing.T, repository *Repository, validator EODHDCredentialValidator) (*Service, *authtest.Clock) {
	t.Helper()
	service, _, clock := newBootstrapService(t, repository)
	serviceDepsValidator := validator
	service.eodhdValidator = serviceDepsValidator
	return service, clock
}

func credentialBootstrapRequest(capability string) BootstrapRequest {
	request := validBootstrapRequest(capability)
	request.EODHDAPIKey = testEODHDKey
	request.SMTP = SMTPSetupConfiguration{
		Host: "smtp.example.test", Port: 587, From: "access@example.test",
		Username: "mailer", Password: testSMTPSecret,
	}
	return request
}
