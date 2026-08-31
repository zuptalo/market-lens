package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/config"
	"market-lens/server/internal/credentials"
	"market-lens/server/internal/db"
	"market-lens/server/internal/identity"

	"github.com/jackc/pgx/v5/pgxpool"

	"market-lens/server/internal/testdb"
)

func TestExecuteSetupLinkPrintsCapabilityOnceAndLogsOnlySafeMetadata(t *testing.T) {
	expiresAt := time.Date(2026, time.August, 30, 12, 15, 0, 0, time.UTC)
	capability := identity.SetupCapability{
		ID:        "40000000-0000-4000-8000-000000000001",
		Token:     "host-only-setup-capability",
		ExpiresAt: expiresAt,
	}
	issuer := setupCapabilityIssuerFunc(func(context.Context) (identity.SetupCapability, error) {
		return capability, nil
	})
	var output bytes.Buffer
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	if err := executeSetupLink(context.Background(), issuer, &output, logger); err != nil {
		t.Fatal(err)
	}

	if got, want := output.String(), "setup_url=/setup#"+capability.Token+"\n"; got != want {
		t.Fatalf("command output = %q, want %q", got, want)
	}
	if strings.Count(output.String(), capability.Token) != 1 {
		t.Fatalf("capability must be printed exactly once: %q", output.String())
	}
	logged := logs.String()
	for _, safe := range []string{capability.ID, expiresAt.Format(time.RFC3339)} {
		if !strings.Contains(logged, safe) {
			t.Fatalf("safe command log %q does not contain %q", logged, safe)
		}
	}
	if strings.Contains(logged, capability.Token) || strings.Contains(logged, output.String()) {
		t.Fatalf("setup capability reached structured logs: %q", logged)
	}
}

func TestExecuteSetupLinkFailsSafelyWhenBootstrapIsClosed(t *testing.T) {
	issuer := setupCapabilityIssuerFunc(func(context.Context) (identity.SetupCapability, error) {
		return identity.SetupCapability{}, identity.ErrSetupClosed
	})
	var output bytes.Buffer
	var logs bytes.Buffer

	err := executeSetupLink(context.Background(), issuer, &output, slog.New(slog.NewJSONHandler(&logs, nil)))
	if !errors.Is(err, identity.ErrSetupClosed) {
		t.Fatalf("closed setup error = %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("closed setup printed capability output: %q", output.String())
	}
	if strings.Contains(logs.String(), "capability") {
		t.Fatalf("closed setup log disclosed capability detail: %q", logs.String())
	}
}

func TestExecuteSetupLinkRespectsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	issuer := setupCapabilityIssuerFunc(func(context.Context) (identity.SetupCapability, error) {
		called = true
		return identity.SetupCapability{}, nil
	})

	err := executeSetupLink(ctx, issuer, &bytes.Buffer{}, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled setup-link error = %v", err)
	}
	if called {
		t.Fatal("cancelled setup-link issued a capability")
	}
}

func TestExecuteAuthCommandRoutesOnlyExactSetupLink(t *testing.T) {
	capability := identity.SetupCapability{
		ID:        "40000000-0000-4000-8000-000000000001",
		Token:     "host-only-setup-capability",
		ExpiresAt: time.Date(2026, time.August, 30, 12, 15, 0, 0, time.UTC),
	}
	issued := 0
	issuer := setupCapabilityIssuerFunc(func(context.Context) (identity.SetupCapability, error) {
		issued++
		return capability, nil
	})
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	if err := executeAuthCommand(context.Background(), []string{"auth", "setup-link"}, issuer,
		nil, nil, nil, config.ExternalCredentialConfig{}, nil, &output, logger); err != nil {
		t.Fatal(err)
	}
	if issued != 1 || !strings.Contains(output.String(), capability.Token) {
		t.Fatalf("valid setup command issued=%d output=%q", issued, output.String())
	}

	for _, args := range [][]string{
		nil,
		{"auth"},
		{"auth", "setup-link", "unexpected"},
		{"auth", "unknown"},
		{"marketdata", "setup-link"},
	} {
		if err := executeAuthCommand(context.Background(), args, issuer, nil, nil, nil,
			config.ExternalCredentialConfig{}, nil, &bytes.Buffer{}, logger); err == nil {
			t.Fatalf("invalid auth command was accepted: %v", args)
		}
	}
	if issued != 1 {
		t.Fatalf("invalid auth command issued a capability; total=%d", issued)
	}
}

func TestExecuteAuthCommandRoutesOwnerPasswordResetToInteractiveFlow(t *testing.T) {
	err := executeAuthCommand(context.Background(), []string{"auth", "owner-password", "reset"},
		setupCapabilityIssuerFunc(func(context.Context) (identity.SetupCapability, error) {
			t.Fatal("owner password reset attempted setup capability issuance")
			return identity.SetupCapability{}, nil
		}), &ownerPasswordResetterStub{}, nil, nil, config.ExternalCredentialConfig{}, &passwordTerminalStub{terminal: false},
		&bytes.Buffer{}, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	if err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("non-interactive owner reset error = %v, want safe interactive-terminal rejection", err)
	}
}

func TestExecuteOwnerPasswordResetRequiresTTYAndMatchingNoEchoInput(t *testing.T) {
	for _, tt := range []struct {
		name     string
		terminal passwordTerminalStub
	}{
		{name: "non tty", terminal: passwordTerminalStub{terminal: false}},
		{name: "mismatch", terminal: passwordTerminalStub{terminal: true, passwords: [][]byte{[]byte("first strong password"), []byte("different strong password")}}},
		{name: "weak", terminal: passwordTerminalStub{terminal: true, passwords: [][]byte{[]byte("short"), []byte("short")}}},
		{name: "eof", terminal: passwordTerminalStub{terminal: true, err: io.EOF}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resetter := &ownerPasswordResetterStub{}
			var output, logs bytes.Buffer
			err := executeOwnerPasswordReset(context.Background(), resetter, &tt.terminal, &output,
				slog.New(slog.NewTextHandler(&logs, nil)))
			if err == nil {
				t.Fatal("invalid interactive reset unexpectedly succeeded")
			}
			if resetter.calls != 0 || output.Len() != 0 {
				t.Fatalf("invalid reset calls=%d output=%q", resetter.calls, output.String())
			}
		})
	}

	terminal := &passwordTerminalStub{terminal: true, passwords: [][]byte{
		[]byte("replacement owner password"), []byte("replacement owner password"),
	}}
	resetter := &ownerPasswordResetterStub{}
	var output, logs bytes.Buffer
	if err := executeOwnerPasswordReset(context.Background(), resetter, terminal, &output,
		slog.New(slog.NewTextHandler(&logs, nil))); err != nil {
		t.Fatal(err)
	}
	if resetter.calls != 1 || resetter.password != "replacement owner password" {
		t.Fatalf("reset calls=%d password=%q", resetter.calls, resetter.password)
	}
	for _, leaked := range []string{"replacement owner password", "owner@example.com"} {
		if strings.Contains(output.String(), leaked) || strings.Contains(logs.String(), leaked) {
			t.Fatalf("reset output disclosed %q", leaked)
		}
	}
}

func TestExecuteCredentialKeyRotationRequiresTTYMatchingBase64KeyAndHigherVersion(t *testing.T) {
	current := commandTestExternalCredentials()
	validEncoded := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x55}, 32))
	for _, tt := range []struct {
		name     string
		version  uint32
		terminal passwordTerminalStub
	}{
		{name: "non tty", version: 2, terminal: passwordTerminalStub{terminal: false}},
		{name: "same version", version: 1, terminal: passwordTerminalStub{terminal: true, passwords: [][]byte{[]byte(validEncoded), []byte(validEncoded)}}},
		{name: "mismatch", version: 2, terminal: passwordTerminalStub{terminal: true, passwords: [][]byte{[]byte(validEncoded), []byte(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x66}, 32)))}}},
		{name: "invalid base64", version: 2, terminal: passwordTerminalStub{terminal: true, passwords: [][]byte{[]byte("not-a-key"), []byte("not-a-key")}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rotator := &credentialKeyRotatorStub{}
			var output, logs bytes.Buffer
			err := executeCredentialKeyRotation(context.Background(), rotator, current, tt.version,
				&tt.terminal, &output, slog.New(slog.NewTextHandler(&logs, nil)), time.Now())
			if err == nil {
				t.Fatal("invalid credential-key rotation unexpectedly succeeded")
			}
			if rotator.calls != 0 || output.Len() != 0 {
				t.Fatalf("invalid rotation calls=%d output=%q", rotator.calls, output.String())
			}
		})
	}

	terminal := &passwordTerminalStub{terminal: true, passwords: [][]byte{[]byte(validEncoded), []byte(validEncoded)}}
	rotator := &credentialKeyRotatorStub{}
	var output, logs bytes.Buffer
	if err := executeCredentialKeyRotation(context.Background(), rotator, current, 2, terminal, &output,
		slog.New(slog.NewTextHandler(&logs, nil)), time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if rotator.calls != 1 || rotator.oldVersion != 1 || rotator.newVersion != 2 {
		t.Fatalf("rotation calls=%d old=%d new=%d", rotator.calls, rotator.oldVersion, rotator.newVersion)
	}
	for _, destination := range []string{output.String(), logs.String()} {
		if strings.Contains(destination, validEncoded) || strings.Contains(destination, base64.StdEncoding.EncodeToString(current.Key)) {
			t.Fatalf("credential key reached output: %q", destination)
		}
	}
}

func TestExecuteAuthCommandRoutesCredentialKeyRotationWithoutSecretArguments(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x77}, 32))
	terminal := &passwordTerminalStub{terminal: true, passwords: [][]byte{[]byte(encoded), []byte(encoded)}}
	rotator := &credentialKeyRotatorStub{}
	var output bytes.Buffer
	err := executeAuthCommand(context.Background(),
		[]string{"auth", "credential-key", "rotate", "--new-version", "2"}, nil, nil,
		rotator, nil, commandTestExternalCredentials(), terminal, &output,
		slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if rotator.calls != 1 || !strings.Contains(output.String(), "key_version=2") || strings.Contains(output.String(), encoded) {
		t.Fatalf("credential rotation calls=%d output=%q", rotator.calls, output.String())
	}
	for _, args := range [][]string{
		{"auth", "credential-key", "rotate", "--new-version", "0"},
		{"auth", "credential-key", "rotate", "--new-version", "4294967296"},
		{"auth", "credential-key", "rotate", "--new-version", "2", "--key", encoded},
	} {
		if err := executeAuthCommand(context.Background(), args, nil, nil, rotator, nil,
			commandTestExternalCredentials(), terminal, &bytes.Buffer{},
			slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))); err == nil {
			t.Fatalf("unsafe credential-key command accepted: %v", args)
		}
	}
}

type passwordTerminalStub struct {
	terminal  bool
	passwords [][]byte
	lines     []string
	err       error
	reads     int
	lineReads int
}

func (stub *passwordTerminalStub) IsTerminal() bool { return stub.terminal }

func (stub *passwordTerminalStub) ReadLine(string) (string, error) {
	if stub.err != nil {
		return "", stub.err
	}
	if stub.lineReads >= len(stub.lines) {
		return "", io.EOF
	}
	line := stub.lines[stub.lineReads]
	stub.lineReads++
	return line, nil
}

func (stub *passwordTerminalStub) ReadPassword(string) ([]byte, error) {
	if stub.err != nil {
		return nil, stub.err
	}
	if stub.reads >= len(stub.passwords) {
		return nil, io.EOF
	}
	password := append([]byte(nil), stub.passwords[stub.reads]...)
	stub.reads++
	return password, nil
}

type ownerPasswordResetterStub struct {
	calls    int
	password string
}

type credentialKeyRotatorStub struct {
	calls      int
	oldVersion uint32
	newVersion uint32
}

func (stub *credentialKeyRotatorStub) Rotate(_ context.Context, oldCipher, newCipher *credentials.Cipher, _ time.Time) error {
	stub.calls++
	stub.oldVersion = oldCipher.KeyVersion()
	stub.newVersion = newCipher.KeyVersion()
	return nil
}

func (stub *ownerPasswordResetterStub) ResetOwnerPassword(_ context.Context, password string) error {
	stub.calls++
	stub.password = password
	return nil
}

func TestNewIdentityServiceWiresConfiguredSetupLifetimeAndKeyedPersistence(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	authConfig := config.AuthConfig{
		Secret:                 "0123456789abcdef0123456789abcdef",
		SetupTTL:               15 * time.Minute,
		OwnerIdleTimeout:       8 * time.Hour,
		SessionAbsoluteTimeout: 30 * 24 * time.Hour,
	}
	service, err := newIdentityService(authConfig, []byte(authConfig.Secret), commandTestExternalCredentials(),
		commandEODHDValidator{}, commandSMTPVerifier{}, pool, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := executeAuthCommand(context.Background(), []string{"auth", "setup-link"}, service,
		nil, nil, nil, config.ExternalCredentialConfig{}, nil, &output,
		slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))); err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSuffix(strings.TrimPrefix(output.String(), "setup_url=/setup#"), "\n")
	if token == "" || token == output.String() {
		t.Fatalf("setup output did not contain a fragment capability: %q", output.String())
	}

	var lifetimeSeconds float64
	var persistedDigest string
	if err := pool.QueryRow(context.Background(), `SELECT
		extract(epoch FROM expires_at-created_at), encode(token_digest, 'hex')
		FROM auth_capabilities WHERE kind='owner_setup'`).Scan(&lifetimeSeconds, &persistedDigest); err != nil {
		t.Fatal(err)
	}
	if lifetimeSeconds != (15 * time.Minute).Seconds() {
		t.Fatalf("persisted setup lifetime = %v seconds", lifetimeSeconds)
	}
	if strings.Contains(persistedDigest, token) {
		t.Fatal("setup capability was persisted in plaintext")
	}
}

func TestNewAuthenticationServiceAcceptsSessionCreatedBySharedIdentityConfiguration(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	authConfig := config.AuthConfig{
		Secret:                 "0123456789abcdef0123456789abcdef",
		SetupTTL:               15 * time.Minute,
		OwnerIdleTimeout:       8 * time.Hour,
		SessionAbsoluteTimeout: 30 * 24 * time.Hour,
	}
	identityService, err := newIdentityService(authConfig, []byte(authConfig.Secret), commandTestExternalCredentials(),
		commandEODHDValidator{}, commandSMTPVerifier{}, pool, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	if err != nil {
		t.Fatal(err)
	}
	setup, err := identityService.IssueSetupCapability(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	created, err := identityService.BootstrapOwner(context.Background(), identity.BootstrapRequest{
		Capability: setup.Token, Email: "owner@example.com", Password: "correct horse battery staple",
		DisplayName: "Market Owner", DeviceLabel: "Command test", Origin: "192.0.2.0/24",
		EODHDAPIKey: "command-test-eodhd-key",
		SMTP:        identity.SMTPSetupConfiguration{Host: "smtp.example.test", Port: 587, From: "access@example.test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	authenticationService, err := newAuthenticationService(authConfig, []byte(authConfig.Secret),
		commandTestExternalCredentials(), pool, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	if err != nil {
		t.Fatal(err)
	}
	principal, err := authenticationService.AuthenticateSession(context.Background(), created.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if principal.UserID != created.User.ID || principal.Role != string(identity.RoleOwner) || principal.SessionID != created.Session.ID {
		t.Fatalf("authenticated principal = %#v", principal)
	}
}

type commandEODHDValidator struct{}

func (commandEODHDValidator) ValidateCredential(context.Context, string) error { return nil }

// commandSMTPVerifier stands in for a reachable mail server. Setup now refuses when SMTP
// cannot be verified, so a test that is not about mail must not depend on a real one.
type commandSMTPVerifier struct{}

func (commandSMTPVerifier) VerifySMTP(context.Context, identity.SMTPSetupConfiguration) error {
	return nil
}

func commandTestExternalCredentials() config.ExternalCredentialConfig {
	return config.ExternalCredentialConfig{
		Key: bytes.Repeat([]byte{0x44}, 32), KeyVersion: 1, Configured: true,
	}
}

type setupCapabilityIssuerFunc func(context.Context) (identity.SetupCapability, error)

func (function setupCapabilityIssuerFunc) IssueSetupCapability(ctx context.Context) (identity.SetupCapability, error) {
	return function(ctx)
}

// TestResolvedSigningKeyReachesBothServicesAcrossARestart proves the wiring feature 009
// introduces: the key is a property of the database, not of the process or its environment.
// A session created by services built from a freshly provisioned key must still authenticate
// against services built from the key a later start reads back.
func TestResolvedSigningKeyReachesBothServicesAcrossARestart(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	authConfig := config.AuthConfig{
		SetupTTL:               15 * time.Minute,
		OwnerIdleTimeout:       8 * time.Hour,
		SessionAbsoluteTimeout: 30 * 24 * time.Hour,
	}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	// A production start with no AUTH_SECRET at all.
	first, err := resolveInstanceSigningKey(ctx, authConfig, pool)
	if err != nil {
		t.Fatalf("a start with only a database connection failed: %v", err)
	}
	if first.Source != auth.SigningKeyProvisioned || len(first.Key) == 0 {
		t.Fatalf("resolution = source %q keylen %d", first.Source, len(first.Key))
	}

	identityService, err := newIdentityService(authConfig, first.Key, commandTestExternalCredentials(),
		commandEODHDValidator{}, commandSMTPVerifier{}, pool, logger)
	if err != nil {
		t.Fatal(err)
	}
	setup, err := identityService.IssueSetupCapability(ctx)
	if err != nil {
		t.Fatal(err)
	}
	created, err := identityService.BootstrapOwner(ctx, identity.BootstrapRequest{
		Capability: setup.Token, Email: "owner@example.com", Password: "correct horse battery staple",
		DisplayName: "Market Owner", DeviceLabel: "Command test", Origin: "192.0.2.0/24",
		EODHDAPIKey: "command-test-eodhd-key",
		SMTP:        identity.SMTPSetupConfiguration{Host: "smtp.example.test", Port: 587, From: "access@example.test"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// The restart: nothing carried over except the database.
	second, err := resolveInstanceSigningKey(ctx, authConfig, pool)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Key, second.Key) {
		t.Fatal("the restart resolved a different signing key")
	}
	authenticationService, err := newAuthenticationService(authConfig, second.Key,
		commandTestExternalCredentials(), pool, logger)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := authenticationService.AuthenticateSession(ctx, created.SessionToken)
	if err != nil {
		t.Fatalf("session issued before the restart was refused after it: %v", err)
	}
	if principal.UserID != created.User.ID {
		t.Fatalf("authenticated principal = %#v", principal)
	}
}

// The four start-up outcomes from contracts/cli.md. Requiring EXTERNAL_CREDENTIAL_KEY is a
// question about stored ciphertext, so a fresh production installation starts with only
// DATABASE_URL (SC-001) while one whose credentials would become unreadable refuses (SC-006).
// No refusal may describe what is stored.
func TestExternalCredentialConfigurationIsRequiredOnlyWhenCredentialsAreStored(t *testing.T) {
	configured := commandTestExternalCredentials()
	wrongKey := config.ExternalCredentialConfig{
		Key: bytes.Repeat([]byte{0x99}, 32), KeyVersion: 1, Configured: true,
	}

	tests := []struct {
		name        string
		seed        bool
		external    config.ExternalCredentialConfig
		wantErr     bool
		wantStored  bool
		wantMention []string
	}{
		{name: "fresh installation without a key starts", seed: false, external: config.ExternalCredentialConfig{}},
		{name: "fresh installation with a key starts", seed: false, external: configured},
		{name: "stored credentials with the right key start", seed: true, external: configured, wantStored: true},
		{
			name: "stored credentials without a key refuse", seed: true,
			external: config.ExternalCredentialConfig{}, wantErr: true, wantStored: true,
			wantMention: []string{"EXTERNAL_CREDENTIAL_KEY"},
		},
		{
			name: "stored credentials with the wrong key refuse", seed: true,
			external: wrongKey, wantErr: true, wantStored: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := testdb.Open(t)
			ctx := context.Background()
			if err := db.Migrate(ctx, pool); err != nil {
				t.Fatal(err)
			}
			if tt.seed {
				seedCommandCredentialSet(t, ctx, pool, configured)
			}

			stored, err := validateExternalCredentialConfiguration(ctx, tt.external, pool)
			if tt.wantErr && err == nil {
				t.Fatal("an installation that cannot read its stored credentials started anyway")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("start refused: %v", err)
			}
			if stored != tt.wantStored {
				t.Errorf("stored credentials reported = %t, want %t", stored, tt.wantStored)
			}
			if err == nil {
				return
			}
			for _, mention := range tt.wantMention {
				if !strings.Contains(err.Error(), mention) {
					t.Errorf("refusal does not name %q: %v", mention, err)
				}
			}
			// The refusal must not describe the ciphertext it cannot read.
			for _, disclosure := range []string{
				"eodhd_api", "smtp", string(configured.Key), base64.StdEncoding.EncodeToString(configured.Key),
			} {
				if strings.Contains(err.Error(), disclosure) {
					t.Errorf("refusal disclosed stored credential detail %q: %v", disclosure, err)
				}
			}
		})
	}
}

func seedCommandCredentialSet(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	external config.ExternalCredentialConfig) {
	t.Helper()
	cipher, err := credentials.NewCipher(external.Key, external.KeyVersion)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	validated := now
	for _, seed := range []struct {
		id        string
		kind      credentials.Kind
		plaintext string
		validated *time.Time
	}{
		{"00000000-0000-4000-8000-0000000009a1", credentials.KindEODHDAPI, `{"api_key":"seeded"}`, &validated},
		{"00000000-0000-4000-8000-0000000009a2", credentials.KindSMTP, `{"host":"smtp.example.test"}`, nil},
	} {
		metadata := credentials.Metadata{
			ID: seed.id, Kind: seed.kind, PayloadVersion: 1, KeyVersion: external.KeyVersion,
		}
		ciphertext, err := cipher.Seal(metadata, []byte(seed.plaintext))
		if err != nil {
			t.Fatal(err)
		}
		if err := credentials.Insert(ctx, pool, credentials.StoredCredential{
			Record:      credentials.Record{Metadata: metadata, Ciphertext: ciphertext},
			ValidatedAt: seed.validated, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
}

// Rotation ends every session, so it must be deliberate: an interactive terminal, an explicit
// typed confirmation, and output that reports the effect without naming the key.
func TestExecuteSigningKeyRotationRequiresTTYAndTypedConfirmation(t *testing.T) {
	tests := []struct {
		name     string
		terminal *passwordTerminalStub
		wantCall bool
		wantErr  string
	}{
		{
			name:     "refuses without a terminal",
			terminal: &passwordTerminalStub{terminal: false},
			wantErr:  "interactive terminal",
		},
		{
			name:     "refuses an unconfirmed rotation",
			terminal: &passwordTerminalStub{terminal: true, lines: []string{"no"}},
			wantErr:  "not confirmed",
		},
		{
			name:     "refuses a lowercase confirmation",
			terminal: &passwordTerminalStub{terminal: true, lines: []string{"rotate"}},
			wantErr:  "not confirmed",
		},
		{
			name:     "rotates when confirmed",
			terminal: &passwordTerminalStub{terminal: true, lines: []string{"ROTATE"}},
			wantCall: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rotator := &signingKeyRotatorStub{generation: 4}
			var output bytes.Buffer
			var logs bytes.Buffer
			err := executeSigningKeyRotation(context.Background(), rotator, tt.terminal, &output,
				slog.New(slog.NewTextHandler(&logs, nil)), time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC))

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want one mentioning %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if rotator.calls != boolToInt(tt.wantCall) {
				t.Fatalf("rotation calls = %d, want %d", rotator.calls, boolToInt(tt.wantCall))
			}
			if !tt.wantCall {
				if output.Len() != 0 {
					t.Fatalf("a refused rotation produced output: %q", output.String())
				}
				return
			}
			if !strings.Contains(output.String(), "signing_key_rotation=complete") ||
				!strings.Contains(output.String(), "generation=5") ||
				!strings.Contains(output.String(), "sessions_revoked=all") {
				t.Fatalf("rotation output = %q", output.String())
			}
			for _, encoding := range []string{
				string(rotator.received), base64.StdEncoding.EncodeToString(rotator.received),
				hex.EncodeToString(rotator.received),
			} {
				if strings.Contains(output.String(), encoding) || strings.Contains(logs.String(), encoding) {
					t.Fatal("rotation disclosed key material")
				}
			}
			if len(rotator.received) < 32 {
				t.Fatalf("replacement key length = %d, want at least 32", len(rotator.received))
			}
		})
	}
}

func TestExecuteAuthCommandRoutesSigningKeyRotation(t *testing.T) {
	rotator := &signingKeyRotatorStub{generation: 1}
	terminal := &passwordTerminalStub{terminal: true, lines: []string{"ROTATE"}}
	var output bytes.Buffer
	if err := executeAuthCommand(context.Background(), []string{"auth", "signing-key", "rotate"},
		nil, nil, nil, rotator, config.ExternalCredentialConfig{}, terminal, &output,
		slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))); err != nil {
		t.Fatal(err)
	}
	if rotator.calls != 1 {
		t.Fatalf("signing-key rotate calls = %d, want 1", rotator.calls)
	}
	for _, args := range [][]string{
		{"auth", "signing-key"},
		{"auth", "signing-key", "rotate", "unexpected"},
		{"auth", "signing-key", "replace"},
	} {
		if err := executeAuthCommand(context.Background(), args, nil, nil, nil, rotator,
			config.ExternalCredentialConfig{}, terminal, &bytes.Buffer{},
			slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))); err == nil {
			t.Fatalf("invalid signing-key command was accepted: %v", args)
		}
	}
}

type signingKeyRotatorStub struct {
	calls      int
	generation int
	received   []byte
}

func (stub *signingKeyRotatorStub) RotateSigningKey(_ context.Context, newKey []byte,
	_ time.Time) (auth.SigningKeyRecord, error) {
	stub.calls++
	stub.received = append([]byte(nil), newKey...)
	return auth.SigningKeyRecord{
		Source: auth.SigningKeyProvisioned, KeyMaterial: stub.received,
		Fingerprint: auth.SigningKeyFingerprint(stub.received), Generation: stub.generation + 1,
	}, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
