package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"market-lens/server/internal/config"
	"market-lens/server/internal/credentials"
	"market-lens/server/internal/db"
	"market-lens/server/internal/identity"
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
		nil, nil, config.ExternalCredentialConfig{}, nil, &output, logger); err != nil {
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
		if err := executeAuthCommand(context.Background(), args, issuer, nil, nil,
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
		}), &ownerPasswordResetterStub{}, nil, config.ExternalCredentialConfig{}, &passwordTerminalStub{terminal: false},
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
		rotator, commandTestExternalCredentials(), terminal, &output,
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
		if err := executeAuthCommand(context.Background(), args, nil, nil, rotator,
			commandTestExternalCredentials(), terminal, &bytes.Buffer{},
			slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))); err == nil {
			t.Fatalf("unsafe credential-key command accepted: %v", args)
		}
	}
}

type passwordTerminalStub struct {
	terminal  bool
	passwords [][]byte
	err       error
	reads     int
}

func (stub *passwordTerminalStub) IsTerminal() bool { return stub.terminal }

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
	service, err := newIdentityService(authConfig, commandTestExternalCredentials(), commandEODHDValidator{}, pool,
		slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := executeAuthCommand(context.Background(), []string{"auth", "setup-link"}, service,
		nil, nil, config.ExternalCredentialConfig{}, nil, &output,
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
	identityService, err := newIdentityService(authConfig, commandTestExternalCredentials(), commandEODHDValidator{}, pool,
		slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
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
	authenticationService, err := newAuthenticationService(authConfig, commandTestExternalCredentials(), pool,
		slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
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

func commandTestExternalCredentials() config.ExternalCredentialConfig {
	return config.ExternalCredentialConfig{
		Key: bytes.Repeat([]byte{0x44}, 32), KeyVersion: 1, Configured: true,
	}
}

type setupCapabilityIssuerFunc func(context.Context) (identity.SetupCapability, error)

func (function setupCapabilityIssuerFunc) IssueSetupCapability(ctx context.Context) (identity.SetupCapability, error) {
	return function(ctx)
}
