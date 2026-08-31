package identity

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/authtest"
	"market-lens/server/internal/credentials"
	"market-lens/server/internal/db"
	"market-lens/server/internal/testdb"
)

func TestBootstrapServiceCreatesExactlyOneOwnerSessionAndAtomicSecurityEvidence(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	service, hasher, _ := newBootstrapService(t, NewRepository(pool))
	setupRequired, err := service.SetupRequired(context.Background())
	if err != nil || !setupRequired {
		t.Fatalf("fresh setup status required=%v err=%v", setupRequired, err)
	}

	setup, err := service.IssueSetupCapability(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.BootstrapOwner(context.Background(), BootstrapRequest{
		Capability: setup.Token, Email: "Owner@EXAMPLE.COM", Password: "correct horse battery staple",
		DisplayName: "Market Owner", DeviceLabel: "Chrome on macOS", Origin: "192.0.2.0/24",
		EODHDAPIKey: testEODHDKey,
		SMTP:        SMTPSetupConfiguration{Host: "smtp.example.test", Port: 587, From: "access@example.test", Username: "mail-account", Password: testSMTPSecret},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.User.Role != RoleOwner || result.User.Status != StatusActive || result.User.Email != "Owner@example.com" {
		t.Fatalf("owner result = %#v", result.User)
	}
	if result.SessionToken == "" || result.CSRFToken == "" || result.Session.ID == "" {
		t.Fatalf("authentication result omitted one-time session material: %#v", result)
	}

	var ownerCount, credentialCount, sessionCount, auditCount, consumedCount, userEventCount, ownerEventCount int
	var closed bool
	var passwordHash, persistedText string
	err = pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM users WHERE role='owner'),
		(SELECT count(*) FROM owner_credentials),
		(SELECT count(*) FROM sessions),
		(SELECT count(*) FROM security_audit_events WHERE event_type='owner.setup.v1' AND outcome='succeeded'),
		(SELECT count(*) FROM auth_capabilities WHERE kind='owner_setup' AND consumed_at IS NOT NULL),
		(SELECT count(*) FROM client_events WHERE event_type='account.changed.v1' AND scope='user' AND subject_user_id=(SELECT id FROM users WHERE role='owner')),
		(SELECT count(*) FROM client_events WHERE event_type='setup.changed.v1' AND scope='owner' AND subject_user_id IS NULL),
		(SELECT closed_at IS NOT NULL FROM bootstrap_state WHERE singleton),
		(SELECT password_hash FROM owner_credentials LIMIT 1),
		jsonb_build_object(
			'capabilities',(SELECT jsonb_agg(to_jsonb(c)) FROM auth_capabilities c),
			'sessions',(SELECT jsonb_agg(to_jsonb(s)) FROM sessions s),
			'audit',(SELECT jsonb_agg(to_jsonb(a)) FROM security_audit_events a)
		)::text`).Scan(&ownerCount, &credentialCount, &sessionCount, &auditCount, &consumedCount,
		&userEventCount, &ownerEventCount, &closed, &passwordHash, &persistedText)
	if err != nil {
		t.Fatal(err)
	}
	if ownerCount != 1 || credentialCount != 1 || sessionCount != 1 || auditCount != 1 || consumedCount != 1 ||
		userEventCount != 1 || ownerEventCount != 1 || !closed {
		t.Fatalf("atomic bootstrap counts owner=%d credential=%d session=%d audit=%d consumed=%d user_events=%d owner_events=%d closed=%v",
			ownerCount, credentialCount, sessionCount, auditCount, consumedCount, userEventCount, ownerEventCount, closed)
	}
	valid, _, err := hasher.Verify(passwordHash, "correct horse battery staple")
	if err != nil || !valid {
		t.Fatalf("persisted owner credential did not verify: valid=%v err=%v", valid, err)
	}
	for _, secret := range []string{setup.Token, result.SessionToken, result.CSRFToken, "correct horse battery staple"} {
		if strings.Contains(persistedText, secret) {
			t.Fatal("bootstrap persisted plaintext secret material")
		}
	}
	if _, err := service.IssueSetupCapability(context.Background()); !errors.Is(err, ErrSetupClosed) {
		t.Fatalf("post-bootstrap setup issuance error = %v", err)
	}
	setupRequired, err = service.SetupRequired(context.Background())
	if err != nil || setupRequired {
		t.Fatalf("closed setup status required=%v err=%v", setupRequired, err)
	}
}

func TestBootstrapServiceRejectsExpiredAndConcurrentReplay(t *testing.T) {
	t.Run("expired", func(t *testing.T) {
		pool := testdb.Open(t)
		if err := db.Migrate(context.Background(), pool); err != nil {
			t.Fatal(err)
		}
		service, _, clock := newBootstrapService(t, NewRepository(pool))
		setup, err := service.IssueSetupCapability(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		clock.Advance(16 * time.Minute)
		_, err = service.BootstrapOwner(context.Background(), validBootstrapRequest(setup.Token))
		if !errors.Is(err, ErrCapabilityUnavailable) {
			t.Fatalf("expired bootstrap error = %v", err)
		}
		var users int
		if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM users`).Scan(&users); err != nil {
			t.Fatal(err)
		}
		if users != 0 {
			t.Fatalf("expired bootstrap created %d users", users)
		}
	})

	t.Run("concurrent replay", func(t *testing.T) {
		pool := testdb.Open(t)
		if err := db.Migrate(context.Background(), pool); err != nil {
			t.Fatal(err)
		}
		service, _, _ := newBootstrapService(t, NewRepository(pool))
		setup, err := service.IssueSetupCapability(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		start := make(chan struct{})
		results := make(chan error, 2)
		var wait sync.WaitGroup
		for range 2 {
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				_, err := service.BootstrapOwner(context.Background(), validBootstrapRequest(setup.Token))
				results <- err
			}()
		}
		close(start)
		wait.Wait()
		close(results)
		succeeded, rejected := 0, 0
		for err := range results {
			switch {
			case err == nil:
				succeeded++
			case errors.Is(err, ErrSetupClosed), errors.Is(err, ErrCapabilityUnavailable):
				rejected++
			default:
				t.Fatalf("unsafe concurrent bootstrap error: %v", err)
			}
		}
		if succeeded != 1 || rejected != 1 {
			t.Fatalf("concurrent results succeeded=%d rejected=%d", succeeded, rejected)
		}
	})
}

func newBootstrapService(t *testing.T, repository *Repository) (*Service, *auth.PasswordHasher, *authtest.Clock) {
	t.Helper()
	clock := authtest.NewClock(time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC))
	hasher, err := auth.NewPasswordHasher(authtest.NewRandomReader(0x71), auth.Argon2Params{
		Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	pattern := make([]byte, 67)
	for index := range pattern {
		pattern[index] = byte(index + 1)
	}
	secrets, err := auth.NewSecrets(bytes.Repeat([]byte{0x72}, 32), authtest.NewRandomReader(pattern...))
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(ServiceDependencies{
		Repository: repository, Passwords: hasher, Secrets: secrets, Now: clock.Now,
		SetupTTL: 15 * time.Minute, OwnerIdleTimeout: 8 * time.Hour,
		SessionAbsoluteTimeout: 30 * 24 * time.Hour,
		EODHDValidator:         &credentialValidatorStub{},
		CredentialCipher:       mustCredentialCipher(t),
		Credentials:            credentials.NewRepository(repository.pool),
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, hasher, clock
}

func validBootstrapRequest(capability string) BootstrapRequest {
	return BootstrapRequest{
		Capability: capability, Email: "owner@example.com", Password: "correct horse battery staple",
		DisplayName: "Market Owner", DeviceLabel: "Test browser", Origin: "192.0.2.0/24",
		EODHDAPIKey: testEODHDKey,
		SMTP:        SMTPSetupConfiguration{Host: "smtp.example.test", Port: 587, From: "access@example.test", Username: "mail-account", Password: testSMTPSecret},
	}
}

func mustCredentialCipher(t *testing.T) *credentials.Cipher {
	t.Helper()
	cipher, err := credentials.NewCipher(bytes.Repeat([]byte{0x73}, 32), 1)
	if err != nil {
		t.Fatal(err)
	}
	return cipher
}

// A deployment that supplies only DATABASE_URL has no credential key yet, and must still
// start and serve. Owner setup stores provider credentials, so it is the one operation that
// cannot proceed without one: it must refuse with a message naming the value, and leave the
// installation exactly as it found it rather than half-created.
func TestBootstrapRefusesClearlyWhenNoCredentialKeyIsConfigured(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	repository := NewRepository(pool)
	clock := authtest.NewClock(time.Date(2026, time.August, 31, 9, 0, 0, 0, time.UTC))
	hasher, err := auth.NewPasswordHasher(authtest.NewRandomReader(0x19), auth.Argon2Params{
		Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	pattern := make([]byte, 251)
	for index := range pattern {
		pattern[index] = byte(index*7 + 3)
	}
	secrets, err := auth.NewSecrets(bytes.Repeat([]byte{0x72}, 32), authtest.NewRandomReader(pattern...))
	if err != nil {
		t.Fatal(err)
	}

	// Constructing the service must succeed: the process has to come up and serve sign-in.
	service, err := NewService(ServiceDependencies{
		Repository: repository, Passwords: hasher, Secrets: secrets, Now: clock.Now,
		SetupTTL: 15 * time.Minute, OwnerIdleTimeout: 8 * time.Hour,
		SessionAbsoluteTimeout: 30 * 24 * time.Hour,
		EODHDValidator:         &credentialValidatorStub{},
		CredentialCipher:       nil,
	})
	if err != nil {
		t.Fatalf("a deployment without EXTERNAL_CREDENTIAL_KEY could not build its identity service: %v", err)
	}

	required, err := service.SetupRequired(ctx)
	if err != nil || !required {
		t.Fatalf("setup status unavailable without a credential key: required=%t err=%v", required, err)
	}
	capability, err := service.IssueSetupCapability(ctx)
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.BootstrapOwner(ctx, validBootstrapRequest(capability.Token))
	if err == nil {
		t.Fatal("owner setup stored provider credentials without a credential key")
	}
	if !strings.Contains(err.Error(), "EXTERNAL_CREDENTIAL_KEY") {
		t.Errorf("refusal does not name the missing value: %v", err)
	}
	for _, secret := range []string{testEODHDKey, testSMTPSecret, "correct horse battery staple"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("refusal disclosed a submitted secret: %v", err)
		}
	}

	// Nothing may be left behind: no owner, no credentials, and setup still open.
	for _, check := range []struct {
		name  string
		query string
	}{
		{"owners", `SELECT count(*) FROM users WHERE role='owner'`},
		{"stored credentials", `SELECT count(*) FROM external_service_credentials`},
	} {
		var count int
		if err := pool.QueryRow(ctx, check.query).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("%s after a refused setup = %d, want 0", check.name, count)
		}
	}
	if required, err := service.SetupRequired(ctx); err != nil || !required {
		t.Fatalf("setup closed after a refused bootstrap: required=%t err=%v", required, err)
	}
}
