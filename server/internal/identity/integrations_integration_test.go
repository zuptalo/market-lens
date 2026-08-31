package identity

import (
	"context"
	"errors"
	"strings"
	"testing"

	"market-lens/server/internal/db"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

// integrationFixture is a bootstrapped installation whose credentials are already stored, so
// the tests below exercise changing a configuration rather than creating one.
type integrationFixture struct {
	pool     *pgxpool.Pool
	service  *Service
	owner    Actor
	verifier *recordingVerifier
}

type recordingVerifier struct {
	calls int
	last  SMTPSetupConfiguration
	err   error
}

func (v *recordingVerifier) VerifySMTP(_ context.Context, config SMTPSetupConfiguration) error {
	v.calls++
	v.last = config
	return v.err
}

// Mail fixtures are built here from named constants rather than written as a host, username
// and password sitting next to each other in each test.
//
// GitGuardian flagged one of these on the pull request that introduced them. Centralising the
// values behind this helper did NOT clear the finding, and it was never going to: the detector
// looks for a host, a username and a password appearing together, which is a shape any test of
// SMTP configuration must contain somewhere. It was resolved on the GitGuardian dashboard as a
// test credential instead.
//
// So if that check fails again on a fixture, resolve it there rather than reshaping the code -
// reshaping only moves the literals around. Keeping them centralised is still worth doing on
// its own merits, in a repository whose subject is storing real provider credentials.
const (
	fixtureMailHost = "smtp.example.test"
	fixtureMailFrom = "access@example.test"
	fixtureMailUser = "mailer"
)

// fixtureMailSecret keeps the value out of the literal that carries the host and username.
// It is a fixture, never a credential: nothing here reaches a real mail server.
func fixtureMailSecret(label string) string { return label + "-never-persist-plaintext" }

func mailFixture(mutate func(*SMTPUpdate)) *SMTPUpdate {
	update := &SMTPUpdate{Host: fixtureMailHost, Port: 587, From: fixtureMailFrom, Username: fixtureMailUser}
	if mutate != nil {
		mutate(update)
	}
	return update
}

func newIntegrationFixture(t *testing.T) *integrationFixture {
	t.Helper()
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	validator := &credentialValidatorStub{}
	service, _ := newBootstrapServiceWithValidator(t, NewRepository(pool), validator)
	verifier := &recordingVerifier{}
	service.smtpVerifier = verifier

	setup, err := service.IssueSetupCapability(ctx)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := service.BootstrapOwner(ctx, credentialBootstrapRequest(setup.Token))
	if err != nil {
		t.Fatal(err)
	}
	verifier.calls = 0
	validator.calls = 0
	return &integrationFixture{
		pool: pool, service: service, verifier: verifier,
		owner: Actor{UserID: owner.User.ID, Role: RoleOwner},
	}
}

func (f *integrationFixture) ciphertexts(t *testing.T) map[string]string {
	t.Helper()
	rows, err := f.pool.Query(context.Background(),
		`SELECT kind, encode(ciphertext,'hex') FROM external_service_credentials ORDER BY kind`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	stored := map[string]string{}
	for rows.Next() {
		var kind, ciphertext string
		if err := rows.Scan(&kind, &ciphertext); err != nil {
			t.Fatal(err)
		}
		stored[kind] = ciphertext
	}
	return stored
}

func TestIntegrationSettingsShowConfigurationWithoutSecrets(t *testing.T) {
	fixture := newIntegrationFixture(t)

	settings, err := fixture.service.IntegrationSettings(context.Background(), fixture.owner)
	if err != nil {
		t.Fatal(err)
	}
	if !settings.SMTPConfigured || settings.SMTP.Host != "smtp.example.test" ||
		settings.SMTP.Port != 587 || settings.SMTP.From != "access@example.test" ||
		settings.SMTP.Username != "mailer" {
		t.Fatalf("smtp settings = %#v", settings.SMTP)
	}
	// The password is reported as present, never returned.
	if !settings.SMTP.PasswordConfigured {
		t.Error("a stored SMTP password was reported as absent")
	}
	if !settings.EODHDConfigured || settings.EODHDValidatedAt == nil {
		t.Fatalf("eodhd settings configured=%t validated=%v", settings.EODHDConfigured, settings.EODHDValidatedAt)
	}
	rendered := settings.SMTP.Host + settings.SMTP.From + settings.SMTP.Username
	if settings.EODHDValidatedAt != nil {
		rendered += *settings.EODHDValidatedAt
	}
	for _, secret := range []string{testEODHDKey, testSMTPSecret} {
		if strings.Contains(rendered, secret) {
			t.Fatalf("settings disclosed a stored secret")
		}
	}
}

func TestVerifyIntegrationsChecksWithoutStoringAnything(t *testing.T) {
	fixture := newIntegrationFixture(t)
	before := fixture.ciphertexts(t)

	newHost := "smtp.moved.test"
	_, err := fixture.service.VerifyIntegrations(context.Background(), fixture.owner, IntegrationUpdate{
		SMTP: mailFixture(func(u *SMTPUpdate) { u.Host = newHost; u.Port = 2525 }),
	})
	if err != nil {
		t.Fatalf("verifying a good configuration failed: %v", err)
	}
	if fixture.verifier.calls != 1 || fixture.verifier.last.Host != newHost {
		t.Fatalf("verifier calls=%d host=%q", fixture.verifier.calls, fixture.verifier.last.Host)
	}
	// An omitted password must be filled in from storage, so the check runs against the
	// credentials that would actually be used.
	if fixture.verifier.last.Password != testSMTPSecret {
		t.Error("the stored SMTP password was not used for verification")
	}
	if after := fixture.ciphertexts(t); after["smtp"] != before["smtp"] || after["eodhd_api"] != before["eodhd_api"] {
		t.Fatal("checking a configuration changed what is stored")
	}
}

func TestUpdateIntegrationsStoresOnlyVerifiedConfiguration(t *testing.T) {
	t.Run("a rejected configuration changes nothing", func(t *testing.T) {
		fixture := newIntegrationFixture(t)
		fixture.verifier.err = ErrSMTPAuthRejected
		before := fixture.ciphertexts(t)

		password := fixtureMailSecret("replacement-mail")
		_, err := fixture.service.UpdateIntegrations(context.Background(), fixture.owner, IntegrationUpdate{
			SMTP: mailFixture(func(u *SMTPUpdate) {
				u.Host = "smtp.moved.test"
				u.Port = 2525
				u.Password = &password
			}),
		})
		var validation *SetupValidationError
		if !errors.As(err, &validation) {
			t.Fatalf("error = %v, want a field-level validation error", err)
		}
		if after := fixture.ciphertexts(t); after["smtp"] != before["smtp"] {
			t.Fatal("a configuration that failed verification was stored anyway")
		}
	})

	t.Run("a verified change is stored and audited", func(t *testing.T) {
		fixture := newIntegrationFixture(t)
		before := fixture.ciphertexts(t)
		ctx := context.Background()

		password := fixtureMailSecret("retained-mail")
		if _, err := fixture.service.UpdateIntegrations(ctx, fixture.owner, IntegrationUpdate{
			SMTP: mailFixture(func(u *SMTPUpdate) {
				u.Host = "smtp.moved.test"
				u.Port = 2525
				u.Username = "postmaster"
				u.Password = &password
			}),
		}); err != nil {
			t.Fatal(err)
		}
		if after := fixture.ciphertexts(t); after["smtp"] == before["smtp"] {
			t.Fatal("a verified change was not stored")
		}
		settings, err := fixture.service.IntegrationSettings(ctx, fixture.owner)
		if err != nil {
			t.Fatal(err)
		}
		if settings.SMTP.Host != "smtp.moved.test" || settings.SMTP.Port != 2525 ||
			settings.SMTP.Username != "postmaster" {
			t.Fatalf("stored settings = %#v", settings.SMTP)
		}
		// The provider credential must be left alone by an SMTP-only change.
		if after := fixture.ciphertexts(t); after["eodhd_api"] != before["eodhd_api"] {
			t.Fatal("an SMTP change rewrote the provider credential")
		}

		var audited int
		if err := fixture.pool.QueryRow(ctx, `SELECT count(*) FROM security_audit_events
			WHERE event_type='integration.updated.v1' AND actor_user_id=$1`, fixture.owner.UserID).Scan(&audited); err != nil {
			t.Fatal(err)
		}
		if audited != 1 {
			t.Fatalf("audit entries = %d, want 1", audited)
		}
		var published int
		if err := fixture.pool.QueryRow(ctx, `SELECT count(*) FROM client_events
			WHERE event_type='integration.updated.v1' AND scope='owner'`).Scan(&published); err != nil {
			t.Fatal(err)
		}
		if published != 1 {
			t.Fatalf("published events = %d, want 1", published)
		}
		assertNoStoredSecretText(t, fixture.pool, password)
	})

	t.Run("omitting the password keeps the stored one", func(t *testing.T) {
		fixture := newIntegrationFixture(t)
		ctx := context.Background()

		if _, err := fixture.service.UpdateIntegrations(ctx, fixture.owner, IntegrationUpdate{
			SMTP: mailFixture(func(u *SMTPUpdate) { u.Port = 2525 }),
		}); err != nil {
			t.Fatal(err)
		}
		settings, err := fixture.service.IntegrationSettings(ctx, fixture.owner)
		if err != nil {
			t.Fatal(err)
		}
		if settings.SMTP.Port != 2525 || !settings.SMTP.PasswordConfigured {
			t.Fatalf("settings after a port change = %#v", settings.SMTP)
		}
		// Proven by what the verifier was handed on the way through.
		if fixture.verifier.last.Password != testSMTPSecret {
			t.Fatal("the retained password was not the stored one")
		}
	})

	t.Run("an explicitly empty credential pair removes authentication", func(t *testing.T) {
		fixture := newIntegrationFixture(t)
		ctx := context.Background()

		empty := ""
		if _, err := fixture.service.UpdateIntegrations(ctx, fixture.owner, IntegrationUpdate{
			SMTP: mailFixture(func(u *SMTPUpdate) {
				u.Username = ""
				u.Password = &empty
			}),
		}); err != nil {
			t.Fatal(err)
		}
		settings, err := fixture.service.IntegrationSettings(ctx, fixture.owner)
		if err != nil {
			t.Fatal(err)
		}
		if settings.SMTP.Username != "" || settings.SMTP.PasswordConfigured {
			t.Fatalf("authentication survived removal: %#v", settings.SMTP)
		}
	})

	t.Run("a submission that changes nothing is refused", func(t *testing.T) {
		fixture := newIntegrationFixture(t)
		if _, err := fixture.service.UpdateIntegrations(context.Background(), fixture.owner,
			IntegrationUpdate{}); err == nil {
			t.Fatal("an empty submission was accepted")
		}
	})
}

func TestIntegrationAdministrationRequiresAnOwner(t *testing.T) {
	fixture := newIntegrationFixture(t)
	ctx := context.Background()
	member := Actor{UserID: "00000000-0000-4000-8000-0000000000aa", Role: RoleMember}

	if _, err := fixture.service.IntegrationSettings(ctx, member); !errors.Is(err, ErrOwnerRequired) {
		t.Errorf("member read error = %v, want %v", err, ErrOwnerRequired)
	}
	update := IntegrationUpdate{SMTP: mailFixture(func(u *SMTPUpdate) { u.Username = "" })}
	if _, err := fixture.service.VerifyIntegrations(ctx, member, update); !errors.Is(err, ErrOwnerRequired) {
		t.Errorf("member verify error = %v, want %v", err, ErrOwnerRequired)
	}
	if _, err := fixture.service.UpdateIntegrations(ctx, member, update); !errors.Is(err, ErrOwnerRequired) {
		t.Errorf("member update error = %v, want %v", err, ErrOwnerRequired)
	}
	if fixture.verifier.calls != 0 {
		t.Fatal("a member reached the mail server")
	}
}

func assertNoStoredSecretText(t *testing.T, pool *pgxpool.Pool, secrets ...string) {
	t.Helper()
	ctx := context.Background()
	for _, query := range []string{
		`SELECT coalesce(string_agg(metadata::text,' '),'') FROM security_audit_events`,
		`SELECT coalesce(string_agg(payload::text,' '),'') FROM client_events`,
	} {
		var text string
		if err := pool.QueryRow(ctx, query).Scan(&text); err != nil {
			t.Fatal(err)
		}
		for _, secret := range append(secrets, testEODHDKey, testSMTPSecret) {
			if secret != "" && strings.Contains(text, secret) {
				t.Fatalf("a secret reached persisted output: %s", text)
			}
		}
	}
}

// Each integration reports its own outcome. The case that matters most is the third: when a
// submitted value is the wrong shape no network call is made at all, so claiming the other
// integration was verified would assert something that never happened.
func TestIntegrationChecksReportEachIntegrationOutcome(t *testing.T) {
	tests := []struct {
		name         string
		update       func(*IntegrationUpdate)
		validatorErr error
		verifierErr  error
		wantEODHD    IntegrationOutcome
		wantSMTP     IntegrationOutcome
	}{
		{
			name:      "only mail submitted",
			update:    func(u *IntegrationUpdate) { u.EODHD = nil },
			wantEODHD: IntegrationNotChecked, wantSMTP: IntegrationVerified,
		},
		{
			name:      "only the provider key submitted",
			update:    func(u *IntegrationUpdate) { u.SMTP = nil },
			wantEODHD: IntegrationVerified, wantSMTP: IntegrationNotChecked,
		},
		{
			name:         "the key is rejected while mail works",
			validatorErr: errors.New("unauthorized"),
			wantEODHD:    IntegrationFailed, wantSMTP: IntegrationVerified,
		},
		{
			name:        "mail is rejected while the key works",
			verifierErr: ErrSMTPAuthRejected,
			wantEODHD:   IntegrationVerified, wantSMTP: IntegrationFailed,
		},
		{
			name:      "a bad port stops both checks",
			update:    func(u *IntegrationUpdate) { u.SMTP.Port = 0 },
			wantEODHD: IntegrationNotChecked, wantSMTP: IntegrationNotChecked,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newIntegrationFixture(t)
			fixture.verifier.err = tt.verifierErr
			fixture.service.eodhdValidator = validatorFunc(func(context.Context, string) error {
				return tt.validatorErr
			})

			password := fixtureMailSecret("submitted-mail")
			update := IntegrationUpdate{
				SMTP:  mailFixture(func(u *SMTPUpdate) { u.Password = &password }),
				EODHD: &EODHDUpdate{APIKey: "a-provider-key"},
			}
			if tt.update != nil {
				tt.update(&update)
			}

			outcomes, _ := fixture.service.VerifyIntegrations(context.Background(), fixture.owner, update)
			if outcomes.EODHD != tt.wantEODHD || outcomes.SMTP != tt.wantSMTP {
				t.Fatalf("outcomes = %+v, want eodhd %q smtp %q", outcomes, tt.wantEODHD, tt.wantSMTP)
			}
		})
	}
}
