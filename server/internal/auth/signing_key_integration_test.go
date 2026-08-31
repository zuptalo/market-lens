package auth_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/authtest"
	"market-lens/server/internal/db"
	clientevents "market-lens/server/internal/events"
	"market-lens/server/internal/identity"
	appmail "market-lens/server/internal/mail"
	"market-lens/server/internal/mail/mailtest"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

var signingKeyClock = time.Date(2026, time.August, 31, 9, 0, 0, 0, time.UTC)

func newSigningKeyPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestResolveSigningKeyProvisionsOnceAndReusesIt(t *testing.T) {
	pool := newSigningKeyPool(t)
	ctx := context.Background()
	repository := auth.NewRepository(pool)

	first, err := repository.ResolveSigningKey(ctx, "", rand.Reader, signingKeyClock)
	if err != nil {
		t.Fatal(err)
	}
	if first.Source != auth.SigningKeyProvisioned || first.Generation != 1 || len(first.Key) != 48 {
		t.Fatalf("first resolution = source %q generation %d keylen %d",
			first.Source, first.Generation, len(first.Key))
	}
	assertSigningKeyRowCount(t, pool, 1)

	// A restart must reuse the stored key, or every session issued before it would break.
	second, err := repository.ResolveSigningKey(ctx, "", rand.Reader, signingKeyClock.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Key, second.Key) {
		t.Fatal("a restart minted a new signing key")
	}
	assertSigningKeyRowCount(t, pool, 1)

	var source string
	var keyMaterial, fingerprint []byte
	if err := pool.QueryRow(ctx,
		`SELECT source, key_material, fingerprint FROM instance_signing_key`,
	).Scan(&source, &keyMaterial, &fingerprint); err != nil {
		t.Fatal(err)
	}
	if source != "provisioned" || !bytes.Equal(keyMaterial, first.Key) {
		t.Fatalf("stored row = source %q", source)
	}
	if !bytes.Equal(fingerprint, auth.SigningKeyFingerprint(first.Key)) {
		t.Fatal("stored fingerprint does not match the stored key")
	}
}

// A deployment that already supplies AUTH_SECRET must keep working untouched. Its value takes
// precedence, nothing is provisioned, and the secret itself never reaches the database - only
// a fingerprint, so that removing or changing it later can be reported rather than resolved.
func TestResolveSigningKeyPrefersSuppliedSecretAndNeverStoresIt(t *testing.T) {
	pool := newSigningKeyPool(t)
	ctx := context.Background()
	repository := auth.NewRepository(pool)
	const supplied = "an-operator-supplied-auth-secret-value-32b"

	resolution, err := repository.ResolveSigningKey(ctx, supplied, rand.Reader, signingKeyClock)
	if err != nil {
		t.Fatal(err)
	}
	if string(resolution.Key) != supplied || resolution.Source != auth.SigningKeySupplied {
		t.Fatalf("resolution = source %q", resolution.Source)
	}

	var source string
	var keyMaterial []byte
	if err := pool.QueryRow(ctx,
		`SELECT source, key_material FROM instance_signing_key`).Scan(&source, &keyMaterial); err != nil {
		t.Fatal(err)
	}
	if source != "supplied" {
		t.Fatalf("stored source = %q, want supplied", source)
	}
	if keyMaterial != nil {
		t.Fatal("a supplied AUTH_SECRET was written into the database")
	}

	// Supplying the same value again is the ordinary restart of an existing deployment.
	again, err := repository.ResolveSigningKey(ctx, supplied, rand.Reader, signingKeyClock.Add(time.Hour))
	if err != nil {
		t.Fatalf("restarting an AUTH_SECRET deployment failed: %v", err)
	}
	if string(again.Key) != supplied {
		t.Fatal("a restart changed the supplied key")
	}

	// Removing it must be reported, never silently replaced with a provisioned key.
	if _, err := repository.ResolveSigningKey(ctx, "", rand.Reader, signingKeyClock); err == nil {
		t.Fatal("removing AUTH_SECRET silently re-keyed the installation")
	}
}

// SC-003. The callers are not serialized: they all start at once and the database is the only
// thing preventing a second key from existing.
func TestOneHundredSimultaneousStartsProduceExactlyOneSigningKey(t *testing.T) {
	pool := newSigningKeyPool(t)
	ctx := context.Background()

	keys := make([][]byte, concurrentWorkers)
	results := runConcurrently(concurrentWorkers, func(index int) error {
		resolution, err := auth.NewRepository(pool).ResolveSigningKey(ctx, "", rand.Reader, signingKeyClock)
		if err != nil {
			return err
		}
		keys[index] = resolution.Key
		return nil
	})
	if succeeded := countSucceeded(results); succeeded != concurrentWorkers {
		t.Fatalf("%d of %d simultaneous starts resolved a key, want all of them; first error: %v",
			succeeded, concurrentWorkers, firstError(results))
	}
	assertSigningKeyRowCount(t, pool, 1)
	for index, key := range keys {
		if !bytes.Equal(key, keys[0]) {
			t.Fatalf("instance %d resolved a different key from instance 0", index)
		}
	}
}

// SC-002. Rebuilding every service against the same database is what a restart is, and what a
// restore onto another host is once the data has moved. The session must survive both,
// because the key travelled with the data.
func TestSessionsSurviveARestartThatRebuildsEveryService(t *testing.T) {
	pool := newSigningKeyPool(t)
	ctx := context.Background()
	repository := auth.NewRepository(pool)

	resolution, err := repository.ResolveSigningKey(ctx, "", rand.Reader, signingKeyClock)
	if err != nil {
		t.Fatal(err)
	}
	fixture := newAccountFixtureWithKey(t, pool, resolution.Key)
	owner := fixture.bootstrapOwner(t)

	principal, err := fixture.auth.AuthenticateSession(ctx, owner.SessionToken)
	if err != nil {
		t.Fatalf("session rejected before the restart: %v", err)
	}
	if principal.UserID != owner.User.ID {
		t.Fatal("session resolved to the wrong account before the restart")
	}

	// The restart: nothing is carried over in memory. The key is read back from the database.
	restarted, err := auth.NewRepository(pool).ResolveSigningKey(ctx, "", rand.Reader, signingKeyClock.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restarted.Key, resolution.Key) {
		t.Fatal("the restart resolved a different key")
	}
	secrets, err := auth.NewSecrets(restarted.Key, authtest.NewRandomReader(signingKeyRandomPattern()...))
	if err != nil {
		t.Fatal(err)
	}
	fixture.secrets = secrets
	fixture.rebuild(t, fixture.mailbox)

	principal, err = fixture.auth.AuthenticateSession(ctx, owner.SessionToken)
	if err != nil {
		t.Fatalf("session issued before the restart was refused after it: %v", err)
	}
	if principal.UserID != owner.User.ID {
		t.Fatal("session resolved to the wrong account after the restart")
	}
}

func assertSigningKeyRowCount(t *testing.T, pool *pgxpool.Pool, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM instance_signing_key`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("instance_signing_key rows = %d, want %d", got, want)
	}
}

func firstError(results []error) error {
	for _, err := range results {
		if err != nil {
			return err
		}
	}
	return nil
}

// signingKeyRandomPattern is the deterministic token source the account fixture uses, so a
// rebuilt service issues the same sequence a restarted process would.
func signingKeyRandomPattern() []byte {
	pattern := make([]byte, 251)
	for index := range pattern {
		pattern[index] = byte(index*11 + 5)
	}
	return pattern
}

// newAccountFixtureWithKey builds the standard account fixture over an existing pool, signing
// with a key resolved from that database rather than a constant. That is the difference this
// feature introduces: the key is a property of the data, not of the process.
func newAccountFixtureWithKey(t *testing.T, pool *pgxpool.Pool, key []byte) *account {
	t.Helper()
	secrets, err := auth.NewSecrets(key, authtest.NewRandomReader(signingKeyRandomPattern()...))
	if err != nil {
		t.Fatal(err)
	}
	hasher, err := auth.NewPasswordHasher(authtest.NewRandomReader(0x19), auth.Argon2Params{
		Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture := &account{
		pool: pool, clock: authtest.NewClock(signingKeyClock),
		secrets: secrets, hasher: hasher,
		mailbox: mailtest.NewCapture[appmail.Message](), logs: &bytes.Buffer{},
	}
	fixture.rebuild(t, fixture.mailbox)
	return fixture
}

// Story 2 scenarios 1 and 2, and SC-005. Rotation is the recovery path for a key believed
// compromised, so it must actually end every session and be recorded without the key.
func TestRotatingTheSigningKeyEndsEverySessionAndIsAudited(t *testing.T) {
	pool := newSigningKeyPool(t)
	ctx := context.Background()
	repository := auth.NewRepository(pool)

	resolution, err := repository.ResolveSigningKey(ctx, "", rand.Reader, signingKeyClock)
	if err != nil {
		t.Fatal(err)
	}
	fixture := newAccountFixtureWithKey(t, pool, resolution.Key)
	owner := fixture.bootstrapOwner(t)
	if _, err := fixture.auth.AuthenticateSession(ctx, owner.SessionToken); err != nil {
		t.Fatalf("session rejected before rotation: %v", err)
	}

	replacement, err := auth.GenerateSigningKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := repository.RotateSigningKey(ctx, replacement, signingKeyClock.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Generation != 2 || rotated.Source != auth.SigningKeyProvisioned {
		t.Fatalf("rotated record = generation %d source %q", rotated.Generation, rotated.Source)
	}
	assertSigningKeyRowCount(t, pool, 1)

	var storedKey []byte
	var rotatedAt *time.Time
	if err := pool.QueryRow(ctx,
		`SELECT key_material, rotated_at FROM instance_signing_key`).Scan(&storedKey, &rotatedAt); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(storedKey, replacement) || bytes.Equal(storedKey, resolution.Key) {
		t.Fatal("the stored key was not replaced")
	}
	if rotatedAt == nil {
		t.Fatal("rotation was not recorded on the key row")
	}

	// The old session must be refused, and a rebuilt service on the new key must work.
	secrets, err := auth.NewSecrets(replacement, authtest.NewRandomReader(signingKeyRandomPattern()...))
	if err != nil {
		t.Fatal(err)
	}
	fixture.secrets = secrets
	fixture.rebuild(t, fixture.mailbox)
	if _, err := fixture.auth.AuthenticateSession(ctx, owner.SessionToken); err == nil {
		t.Fatal("a session issued under the old key was accepted after rotation")
	}

	var revoked int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM sessions WHERE revoked_reason='signing_key_rotated'`).Scan(&revoked); err != nil {
		t.Fatal(err)
	}
	if revoked == 0 {
		t.Fatal("rotation revoked no sessions")
	}
	var unrevoked int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM sessions WHERE revoked_at IS NULL`).Scan(&unrevoked); err != nil {
		t.Fatal(err)
	}
	if unrevoked != 0 {
		t.Fatalf("%d sessions survived a signing key rotation", unrevoked)
	}

	// Recorded, with the generation and without the key.
	var metadata string
	if err := pool.QueryRow(ctx, `SELECT metadata::text FROM security_audit_events
		WHERE event_type='signing_key.rotated.v1'`).Scan(&metadata); err != nil {
		t.Fatalf("rotation was not audited: %v", err)
	}
	// jsonb normalises whitespace, so compare on the decoded value rather than its text.
	var audited struct {
		Generation int `json:"generation"`
	}
	if err := json.Unmarshal([]byte(metadata), &audited); err != nil {
		t.Fatalf("audit metadata is not an object: %s", metadata)
	}
	if audited.Generation != 2 {
		t.Fatalf("audited generation = %d, want 2", audited.Generation)
	}
	assertNoKeyMaterialInDatabaseText(t, pool, replacement, resolution.Key)
}

// Every row whose digest was computed under the old key can never verify again, so rotation
// must invalidate all of them. Leaving them behind would leave rows that can only fail.
func TestRotatingTheSigningKeyInvalidatesEveryDigestBearingRow(t *testing.T) {
	pool := newSigningKeyPool(t)
	ctx := context.Background()
	repository := auth.NewRepository(pool)

	resolution, err := repository.ResolveSigningKey(ctx, "", rand.Reader, signingKeyClock)
	if err != nil {
		t.Fatal(err)
	}
	fixture := newAccountFixtureWithKey(t, pool, resolution.Key)
	owner := fixture.bootstrapOwner(t)
	fixture.clock.Advance(time.Minute)
	if _, err := fixture.identity.CreateInvitation(ctx,
		identity.Actor{UserID: owner.User.ID, Role: identity.RoleOwner}, "member@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO auth_rate_events (bucket_kind,bucket_digest,occurred_at)
		VALUES ('owner_login', decode(repeat('31',32),'hex'), $1)`, signingKeyClock); err != nil {
		t.Fatal(err)
	}
	// A locked-out member must stay locked out: rotation is not a way to clear a lockout.
	if _, err := pool.Exec(ctx, `INSERT INTO member_login_state
		(user_id,consecutive_failures,updated_at) VALUES ($1,3,$2)`, owner.User.ID, signingKeyClock); err != nil {
		t.Fatal(err)
	}

	replacement, err := auth.GenerateSigningKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.RotateSigningKey(ctx, replacement, signingKeyClock.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	for _, check := range []struct {
		name  string
		query string
	}{
		{"usable owner setup capabilities",
			`SELECT count(*) FROM auth_capabilities WHERE consumed_at IS NULL AND revoked_at IS NULL`},
		{"pending invitations", `SELECT count(*) FROM invitations WHERE state='pending'`},
		{"active login challenges", `SELECT count(*) FROM member_login_challenges WHERE state='active'`},
		{"rate buckets", `SELECT count(*) FROM auth_rate_events`},
	} {
		var remaining int
		if err := pool.QueryRow(ctx, check.query).Scan(&remaining); err != nil {
			t.Fatal(err)
		}
		if remaining != 0 {
			t.Errorf("%s surviving a rotation = %d, want 0", check.name, remaining)
		}
	}

	var failures int
	if err := pool.QueryRow(ctx,
		`SELECT consecutive_failures FROM member_login_state WHERE user_id=$1`, owner.User.ID).Scan(&failures); err != nil {
		t.Fatal(err)
	}
	if failures != 3 {
		t.Fatalf("member lockout counter after rotation = %d, want 3; rotation must not unlock anybody", failures)
	}

	// The audit trail is history, not verifiable state, and must survive.
	var audited int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM security_audit_events`).Scan(&audited); err != nil {
		t.Fatal(err)
	}
	if audited == 0 {
		t.Fatal("rotation destroyed the audit trail")
	}
}

// Story 2 scenario 3. A rotation that fails part way must leave the installation on exactly
// one usable key, never between two. The failure is injected through the database so that no
// production code is edited to make the test pass.
func TestFailedSigningKeyRotationLeavesThePreviousKeyInForce(t *testing.T) {
	pool := newSigningKeyPool(t)
	ctx := context.Background()
	repository := auth.NewRepository(pool)

	resolution, err := repository.ResolveSigningKey(ctx, "", rand.Reader, signingKeyClock)
	if err != nil {
		t.Fatal(err)
	}
	fixture := newAccountFixtureWithKey(t, pool, resolution.Key)
	owner := fixture.bootstrapOwner(t)

	if _, err := pool.Exec(ctx, `ALTER TABLE security_audit_events
		ADD CONSTRAINT rotation_failure_injection CHECK (event_type <> 'signing_key.rotated.v1')`); err != nil {
		t.Fatal(err)
	}
	replacement, err := auth.GenerateSigningKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.RotateSigningKey(ctx, replacement, signingKeyClock.Add(time.Hour)); err == nil {
		t.Fatal("a rotation that could not be audited reported success")
	}

	assertSigningKeyRowCount(t, pool, 1)
	var storedKey []byte
	var generation int
	if err := pool.QueryRow(ctx,
		`SELECT key_material, generation FROM instance_signing_key`).Scan(&storedKey, &generation); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(storedKey, resolution.Key) || generation != 1 {
		t.Fatal("a failed rotation left the installation on a different key")
	}
	if _, err := fixture.auth.AuthenticateSession(ctx, owner.SessionToken); err != nil {
		t.Fatalf("a failed rotation ended a session: %v", err)
	}

	// Retrying after the cause is removed must succeed and leave exactly one usable key.
	if _, err := pool.Exec(ctx,
		`ALTER TABLE security_audit_events DROP CONSTRAINT rotation_failure_injection`); err != nil {
		t.Fatal(err)
	}
	rotated, err := repository.RotateSigningKey(ctx, replacement, signingKeyClock.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("retrying the rotation failed: %v", err)
	}
	if rotated.Generation != 2 {
		t.Fatalf("generation after a retried rotation = %d, want 2", rotated.Generation)
	}
	assertSigningKeyRowCount(t, pool, 1)
}

// assertNoKeyMaterialInDatabaseText proves no key reached any row a person or a client reads.
func assertNoKeyMaterialInDatabaseText(t *testing.T, pool *pgxpool.Pool, keys ...[]byte) {
	t.Helper()
	ctx := context.Background()
	for _, query := range []string{
		`SELECT coalesce(string_agg(metadata::text, ' '), '') FROM security_audit_events`,
		`SELECT coalesce(string_agg(payload::text, ' '), '') FROM client_events`,
	} {
		var text string
		if err := pool.QueryRow(ctx, query).Scan(&text); err != nil {
			t.Fatal(err)
		}
		for _, key := range keys {
			for _, encoded := range []string{
				string(key), base64.StdEncoding.EncodeToString(key), hex.EncodeToString(key),
			} {
				if strings.Contains(text, encoded) {
					t.Fatalf("key material reached persisted output: %s", text)
				}
			}
		}
	}
}

// A client disconnected across a rotation must still learn what happened when it reconnects
// with its last acknowledged event ID. Rotation adds a new event type but no new contract:
// publication is coupled to the same transaction, so a committed rotation cannot be missed.
func TestRotationEventsReplayFromTheLastAcknowledgedEventID(t *testing.T) {
	pool := newSigningKeyPool(t)
	ctx := context.Background()
	repository := auth.NewRepository(pool)

	resolution, err := repository.ResolveSigningKey(ctx, "", rand.Reader, signingKeyClock)
	if err != nil {
		t.Fatal(err)
	}
	fixture := newAccountFixtureWithKey(t, pool, resolution.Key)
	owner := fixture.bootstrapOwner(t)

	events := clientevents.NewRepository(pool)
	audience := clientevents.Audience{UserID: owner.User.ID, Role: "owner"}
	before, err := events.ListAuthorized(ctx, audience, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	// The cursor a connected client would have acknowledged just before the rotation.
	var cursor int64
	for _, event := range before {
		if event.ID > cursor {
			cursor = event.ID
		}
	}

	replacement, err := auth.GenerateSigningKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.RotateSigningKey(ctx, replacement, signingKeyClock.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	replayed, err := events.ListAuthorized(ctx, audience, cursor, 100)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, event := range replayed {
		seen[event.Type]++
		if event.ID <= cursor {
			t.Fatalf("replay returned an event the client had already acknowledged: %d", event.ID)
		}
	}
	if seen["signing_key.rotated.v1"] != 1 {
		t.Fatalf("signing_key.rotated.v1 events after the cursor = %d, want 1", seen["signing_key.rotated.v1"])
	}
	if seen["sessions.revoked.v1"] != 1 {
		t.Fatalf("sessions.revoked.v1 events after the cursor = %d, want 1", seen["sessions.revoked.v1"])
	}

	// Replaying the same cursor twice must return the same events, so a duplicate delivery
	// after a dropped connection is safe.
	again, err := events.ListAuthorized(ctx, audience, cursor, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != len(replayed) {
		t.Fatalf("second replay returned %d events, first returned %d", len(again), len(replayed))
	}
	for index := range again {
		if again[index].ID != replayed[index].ID || again[index].Type != replayed[index].Type {
			t.Fatal("replay is not stable for one cursor")
		}
	}
	assertNoKeyMaterialInDatabaseText(t, pool, replacement, resolution.Key)
}

// SC-004. One complete lifecycle - provision, sign in, invite, rotate - with every text-like
// column in the schema swept for the key in three encodings, plus the captured log output.
// The sweep is schema-driven rather than a list of tables, so a future column cannot quietly
// become a place the key leaks into.
func TestSigningKeyNeverAppearsAnywhereAcrossACompleteLifecycle(t *testing.T) {
	pool := newSigningKeyPool(t)
	ctx := context.Background()
	repository := auth.NewRepository(pool)

	resolution, err := repository.ResolveSigningKey(ctx, "", rand.Reader, signingKeyClock)
	if err != nil {
		t.Fatal(err)
	}
	fixture := newAccountFixtureWithKey(t, pool, resolution.Key)
	owner := fixture.bootstrapOwner(t)
	if _, err := fixture.auth.AuthenticateSession(ctx, owner.SessionToken); err != nil {
		t.Fatal(err)
	}
	fixture.clock.Advance(time.Minute)
	if _, err := fixture.identity.CreateInvitation(ctx,
		identity.Actor{UserID: owner.User.ID, Role: identity.RoleOwner}, "member@example.com"); err != nil {
		t.Fatal(err)
	}
	replacement, err := auth.GenerateSigningKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.RotateSigningKey(ctx, replacement, signingKeyClock.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	// Raw key bytes are not valid UTF-8, so they can only ever appear in Go-side output; a
	// text column physically cannot hold them. The encoded forms are what could realistically
	// be written somewhere, so those are what the schema sweep looks for.
	needles := make([]string, 0, 6)
	textNeedles := make([]string, 0, 4)
	for _, key := range [][]byte{resolution.Key, replacement} {
		encoded := []string{base64.StdEncoding.EncodeToString(key), hex.EncodeToString(key)}
		textNeedles = append(textNeedles, encoded...)
		needles = append(needles, append([]string{string(key)}, encoded...)...)
	}

	// Everything the process wrote to its log across the whole lifecycle.
	for _, needle := range needles {
		if strings.Contains(fixture.logs.String(), needle) {
			t.Fatalf("key material reached the log output")
		}
	}
	// Every delivered message, which is the other thing that leaves the process.
	for _, message := range fixture.mailbox.Messages() {
		rendered := message.To + " " + message.Subject + " " + message.Text
		for _, needle := range needles {
			if strings.Contains(rendered, needle) {
				t.Fatal("key material reached an outgoing message")
			}
		}
	}

	columns, err := pool.Query(ctx, `
		SELECT table_name, column_name
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND data_type IN ('text', 'jsonb', 'json', 'character varying', 'character')
		ORDER BY table_name, column_name`)
	if err != nil {
		t.Fatal(err)
	}
	type target struct{ table, column string }
	targets := make([]target, 0, 64)
	for columns.Next() {
		var item target
		if err := columns.Scan(&item.table, &item.column); err != nil {
			columns.Close()
			t.Fatal(err)
		}
		targets = append(targets, item)
	}
	columns.Close()
	if err := columns.Err(); err != nil {
		t.Fatal(err)
	}
	if len(targets) < 20 {
		t.Fatalf("schema sweep found only %d text columns; the sweep is not covering the schema", len(targets))
	}

	for _, item := range targets {
		for _, needle := range textNeedles {
			var hits int
			query := `SELECT count(*) FROM ` + quoteIdentifier(item.table) +
				` WHERE ` + quoteIdentifier(item.column) + `::text LIKE $1`
			if err := pool.QueryRow(ctx, query, "%"+needle+"%").Scan(&hits); err != nil {
				t.Fatalf("sweep %s.%s: %v", item.table, item.column, err)
			}
			if hits != 0 {
				t.Errorf("key material found in %s.%s", item.table, item.column)
			}
		}
	}

	// The one place key material is allowed to be, so the sweep above is meaningful rather
	// than passing because the key is nowhere at all.
	var stored []byte
	if err := pool.QueryRow(ctx, `SELECT key_material FROM instance_signing_key`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, replacement) {
		t.Fatal("the sweep passed because the key was not stored where it belongs")
	}
}
