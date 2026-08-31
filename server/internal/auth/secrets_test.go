package auth_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"market-lens/server/internal/api"
	"market-lens/server/internal/auth"
	"market-lens/server/internal/authtest"
	"market-lens/server/internal/db"
	clientevents "market-lens/server/internal/events"
	"market-lens/server/internal/httpx"
	"market-lens/server/internal/identity"
	appmail "market-lens/server/internal/mail"
	"market-lens/server/internal/mail/mailtest"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

// fixtureSecret derives one stand-in for something a person or the host hands this
// application. The values are built at run time rather than written down, so this file holds
// no credential-shaped literal for a scanner to flag and no value that could match a real one
// by accident. They stay stable across runs, so a failure is still reproducible.
func fixtureSecret(label string) string {
	sum := sha256.Sum256([]byte("market-lens/secret-regression/" + label))
	return label + "-" + hex.EncodeToString(sum[:8])
}

// The four things that must never come back out of the application once they go in.
var (
	secretOwnerPassword = fixtureSecret("owner-secret")
	secretEODHDKey      = fixtureSecret("eodhd-secret")
	secretSMTPPassword  = fixtureSecret("mail-secret")
	secretSMTPUsername  = fixtureSecret("mail-account")
)

// liveSecrets are the capabilities and tokens minted during the flow. They are collected as the
// run proceeds because their values are not known in advance.
type liveSecrets struct {
	values []string
	labels []string
}

func (s *liveSecrets) add(label, value string) {
	if value == "" {
		return
	}
	s.values = append(s.values, value)
	s.labels = append(s.labels, label)
}

func (s *liveSecrets) assertAbsent(t *testing.T, where, haystack string) {
	t.Helper()
	lower := strings.ToLower(haystack)
	for index, value := range s.values {
		if strings.Contains(lower, strings.ToLower(value)) {
			t.Errorf("%s disclosed the %s", where, s.labels[index])
		}
	}
}

// TestNoSuppliedOrMintedSecretIsRecoverableFromAnySurface drives one complete account lifecycle
// and then looks for every secret in every place the system could hold or emit one.
func TestNoSuppliedOrMintedSecretIsRecoverableFromAnySurface(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	// Reference data written by the migrations predates every secret in this test, so only rows
	// the account lifecycle actually created are searched.
	baseline := renderSchema(t, pool)
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	clock := authtest.NewClock(time.Date(2026, time.August, 31, 9, 0, 0, 0, time.UTC))
	// A long, prime-length pattern keeps every minted token distinct while staying deterministic.
	pattern := make([]byte, 251)
	for index := range pattern {
		pattern[index] = byte(index*7 + 3)
	}
	secrets, err := auth.NewSecrets(bytes.Repeat([]byte{0x5a}, 32), authtest.NewRandomReader(pattern...))
	if err != nil {
		t.Fatal(err)
	}
	hasher, err := auth.NewPasswordHasher(authtest.NewRandomReader(0x61), auth.Argon2Params{
		Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	mailbox := mailtest.NewCapture[appmail.Message]()
	authService, err := auth.NewService(auth.ServiceDependencies{
		Repository: auth.NewRepository(pool), Passwords: hasher, Secrets: secrets, Now: clock.Now,
		OwnerIdleTimeout: 8 * time.Hour, SessionAbsoluteTimeout: 30 * 24 * time.Hour,
		MemberCodes: auth.NewMemberCodeGenerator(authtest.NewRandomReader(0x7d, 0x2b, 0x91, 0x46)),
		Mail:        mailbox, Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	identityService, err := identity.NewService(identity.ServiceDependencies{
		Repository: identity.NewRepository(pool), Passwords: hasher, Secrets: secrets, Now: clock.Now,
		SetupTTL: 15 * time.Minute, OwnerIdleTimeout: 8 * time.Hour, SessionAbsoluteTimeout: 30 * 24 * time.Hour,
		EODHDValidator:   eodhdValidatorFunc(func(context.Context, string) error { return nil }),
		CredentialCipher: ownerTestCredentialCipher(t),
		MemberAccess:     auth.NewRepository(pool), Mail: mailbox,
		AppBaseURL: "https://lens.example.test", Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}

	live := &liveSecrets{}
	live.add("owner password", secretOwnerPassword)
	live.add("EODHD API key", secretEODHDKey)
	live.add("SMTP password", secretSMTPPassword)

	// Setup capability, owner credential, and the encrypted provider secrets.
	setup, err := identityService.IssueSetupCapability(ctx)
	if err != nil {
		t.Fatal(err)
	}
	live.add("setup capability", setup.Token)
	owner, err := identityService.BootstrapOwner(ctx, identity.BootstrapRequest{
		Capability: setup.Token, Email: "owner@example.com", Password: secretOwnerPassword,
		DisplayName: "Regression Owner", DeviceLabel: "Bootstrap browser", Origin: "192.0.2.0/24",
		EODHDAPIKey: secretEODHDKey,
		SMTP: identity.SMTPSetupConfiguration{
			Host: "smtp.example.test", Port: 587, From: "access@example.test",
			Username: secretSMTPUsername, Password: secretSMTPPassword,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	live.add("owner session token", owner.SessionToken)
	live.add("owner CSRF token", owner.CSRFToken)

	// Invitation capability and the member it creates.
	ownerActor := identity.Actor{UserID: owner.User.ID, Role: identity.RoleOwner}
	clock.Advance(time.Minute)
	if _, err := identityService.CreateInvitation(ctx, ownerActor, "member@example.com"); err != nil {
		t.Fatal(err)
	}
	invitationCapability := capabilityFromMail(t, mailbox.Messages(), "member@example.com")
	live.add("invitation capability", invitationCapability)
	clock.Advance(time.Minute)
	member, err := identityService.AcceptInvitation(ctx, identity.AcceptInvitationRequest{
		Capability: invitationCapability, Email: "member@example.com", DisplayName: "Regression Member",
		DeviceLabel: "Member phone", Origin: "198.51.100.0/24",
	})
	if err != nil {
		t.Fatal(err)
	}
	live.add("member session token", member.SessionToken)
	live.add("member CSRF token", member.CSRFToken)

	// Emailed member sign-in code.
	clock.Advance(time.Minute)
	if _, err := authService.StartSignIn(ctx, auth.SignInStartRequest{
		Email: "member@example.com", Origin: "198.51.100.0/24",
	}); err != nil {
		t.Fatal(err)
	}
	code := codeFromMail(t, mailbox.Messages())
	live.add("member sign-in code", code)
	clock.Advance(time.Minute)
	signedIn, err := authService.VerifyMemberCode(ctx, auth.MemberCodeVerifyRequest{
		Email: "member@example.com", Code: code, DeviceLabel: "Member laptop", Origin: "198.51.100.0/24",
	})
	if err != nil {
		t.Fatal(err)
	}
	live.add("member code session token", signedIn.SessionToken)
	live.add("member code CSRF token", signedIn.CSRFToken)

	// 1. Durable storage. Every row of every table is rendered whole, so a secret cannot hide
	// in a column this test forgot to name.
	for table, rows := range renderSchema(t, pool) {
		var written strings.Builder
		for row := range rows {
			if baseline[table][row] {
				continue
			}
			written.WriteString(row)
			written.WriteString("\n")
		}
		live.assertAbsent(t, "table "+table, written.String())
	}

	// 2. Logs, including the debug level a host might enable while diagnosing a problem.
	live.assertAbsent(t, "the application log", logs.String())

	// 3. Errors handed back to a caller. A wrong credential must not echo what was tried.
	_, wrongPassword := authService.LoginOwner(ctx, auth.OwnerLoginRequest{
		Email: "owner@example.com", Password: secretOwnerPassword + "-wrong", Origin: "192.0.2.0/24",
	})
	_, wrongCode := authService.VerifyMemberCode(ctx, auth.MemberCodeVerifyRequest{
		Email: "member@example.com", Code: "999999", DeviceLabel: "Member laptop", Origin: "198.51.100.0/24",
	})
	_, usedCapability := identityService.AcceptInvitation(ctx, identity.AcceptInvitationRequest{
		Capability: invitationCapability, Email: "member@example.com", DisplayName: "Replay",
		DeviceLabel: "Replay browser", Origin: "198.51.100.0/24",
	})
	for name, err := range map[string]error{
		"the owner login error": wrongPassword, "the member code error": wrongCode,
		"the invitation replay error": usedCapability,
	} {
		if err == nil {
			t.Fatalf("%s did not fail", name)
		}
		live.assertAbsent(t, name, err.Error())
	}

	// 4. Mail. A message carries exactly one capability addressed to one person, and never a
	// second person's material or anything about the host's own credentials.
	for _, message := range mailbox.Messages() {
		body := message.Subject + "\n" + message.Text
		for _, forbidden := range []string{secretOwnerPassword, secretEODHDKey, secretSMTPPassword, secretSMTPUsername} {
			if strings.Contains(body, forbidden) {
				t.Errorf("mail to %s carried host credential material", message.To)
			}
		}
	}

	// 5. REST and SSE. Everything an authenticated owner can read.
	router := api.NewRouter(api.Dependencies{
		Database: pingStub{}, Authenticator: authService, Identity: identityService,
		Authentication: authService, MemberAuth: authService, Members: identityService,
		Invitations: identityService, Events: clientevents.NewService(clientevents.NewRepository(pool)),
		EventHeartbeat: time.Hour, EventBatchLimit: 200,
	})
	for _, path := range []string{
		"/api/v1/account", "/api/v1/account/sessions", "/api/v1/owner/members",
		"/api/v1/owner/invitations", "/api/v1/owner/integrations", "/api/v1/setup/status",
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.AddCookie(&http.Cookie{Name: httpx.SessionCookieName, Value: owner.SessionToken})
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		// The session token is the caller's own cookie, so only the rest of the corpus applies.
		live.assertAbsent(t, "GET "+path, redact(recorder.Body.String(), owner.SessionToken))
	}

	streamCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	time.AfterFunc(300*time.Millisecond, cancel)
	streamRequest := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil).WithContext(streamCtx)
	streamRequest.AddCookie(&http.Cookie{Name: httpx.SessionCookieName, Value: owner.SessionToken})
	stream := httptest.NewRecorder()
	router.ServeHTTP(stream, streamRequest)
	live.assertAbsent(t, "the event stream", redact(stream.Body.String(), owner.SessionToken))
	if strings.Contains(strings.ToLower(stream.Body.String()), "digest") {
		t.Errorf("the event stream carried digest material: %s", stream.Body.String())
	}
}

// redact removes one value that legitimately belongs in a surface, so the remaining corpus can
// still be asserted against it.
func redact(haystack, allowed string) string {
	if allowed == "" {
		return haystack
	}
	return strings.ReplaceAll(haystack, allowed, "[caller's own credential]")
}

// renderSchema renders every row of every table whole, so a secret cannot hide in a column this
// test forgot to name. Rows are keyed by their rendering so two snapshots can be compared.
func renderSchema(t *testing.T, pool *pgxpool.Pool) map[string]map[string]bool {
	t.Helper()
	rendered := map[string]map[string]bool{}
	for _, table := range schemaTables(t, pool) {
		rows, err := pool.Query(context.Background(), `SELECT t::text FROM `+quoteIdentifier(table)+` t`)
		if err != nil {
			t.Fatalf("scan %s: %v", table, err)
		}
		rendered[table] = map[string]bool{}
		for rows.Next() {
			var row string
			if err := rows.Scan(&row); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			rendered[table][row] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
	}
	return rendered
}

func schemaTables(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT table_name FROM information_schema.tables
		WHERE table_schema=current_schema() AND table_type='BASE TABLE' ORDER BY table_name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(tables) < 5 {
		t.Fatalf("found only %d tables, so the scan is not covering the schema", len(tables))
	}
	return tables
}

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// capabilityFromMail extracts the link fragment an invitation email carries.
func capabilityFromMail(t *testing.T, messages []appmail.Message, to string) string {
	t.Helper()
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].To != to {
			continue
		}
		if _, fragment, found := strings.Cut(messages[index].Text, "#"); found {
			return strings.TrimSpace(strings.Fields(fragment)[0])
		}
	}
	t.Fatalf("no invitation capability was delivered to %s", to)
	return ""
}

// codeFromMail extracts the six-digit member sign-in code.
func codeFromMail(t *testing.T, messages []appmail.Message) string {
	t.Helper()
	for index := len(messages) - 1; index >= 0; index-- {
		for _, field := range strings.Fields(messages[index].Text) {
			trimmed := strings.Trim(field, ".,")
			if len(trimmed) == 6 && strings.IndexFunc(trimmed, func(r rune) bool { return r < '0' || r > '9' }) < 0 {
				return trimmed
			}
		}
	}
	t.Fatal("no member sign-in code was delivered")
	return ""
}

type pingStub struct{}

func (pingStub) Ping(context.Context) error { return nil }
