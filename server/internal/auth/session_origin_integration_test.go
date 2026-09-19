package auth_test

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/db"
	"market-lens/server/internal/identity"
	"market-lens/server/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

// storedAddresses reads the two columns straight from the row, because what the screen will show
// has to be what the database holds rather than what a service happened to return.
func storedAddresses(t *testing.T, pool *pgxpool.Pool, sessionID string) (created, lastSeen *netip.Addr) {
	t.Helper()
	if err := pool.QueryRow(context.Background(),
		`SELECT created_ip, last_seen_ip FROM sessions WHERE id=$1`, sessionID).Scan(&created, &lastSeen); err != nil {
		t.Fatalf("read session addresses: %v", err)
	}
	return created, lastSeen
}

// The address follows the session (US3): the column says *last seen from*, and it must not quietly
// mean *signed in from* — a session used from somewhere new would otherwise be invisible.
func TestSessionRecordsTheAddressItWasLastSeenFrom(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	clock, secrets, _ := provisionOwner(t, pool)
	service, _ := newOwnerAuthService(t, pool, clock, secrets, 2, 8*time.Hour, 30*24*time.Hour)

	home := netip.MustParseAddr("203.0.113.12")
	result, err := service.LoginOwner(context.Background(), auth.OwnerLoginRequest{
		Email: "owner@example.com", Password: "correct horse battery staple",
		DeviceLabel: "Chrome on macOS", Origin: home.String(), ClientAddress: home,
	})
	if err != nil {
		t.Fatal(err)
	}
	created, lastSeen := storedAddresses(t, pool, result.Session.ID)
	if created == nil || created.String() != "203.0.113.12" {
		t.Fatalf("created_ip = %v, want the address the session was created from", created)
	}
	if lastSeen == nil || lastSeen.String() != "203.0.113.12" {
		t.Fatalf("last_seen_ip = %v, want the address the session was created from", lastSeen)
	}

	hotel := netip.MustParseAddr("198.51.100.4")
	clock.Advance(time.Hour)
	if _, err := service.AuthenticateSession(context.Background(), result.SessionToken, hotel); err != nil {
		t.Fatal(err)
	}
	created, lastSeen = storedAddresses(t, pool, result.Session.ID)
	if created == nil || created.String() != "203.0.113.12" {
		t.Fatalf("created_ip = %v; where a session was created from does not change", created)
	}
	if lastSeen == nil || lastSeen.String() != "198.51.100.4" {
		t.Fatalf("last_seen_ip = %v, want the address of the most recent request", lastSeen)
	}

	// An IPv6 client is stored in one canonical form, so the same device cannot appear as two.
	clock.Advance(time.Hour)
	if _, err := service.AuthenticateSession(context.Background(), result.SessionToken,
		netip.MustParseAddr("2001:0DB8:0000:0000:0000:0000:0000:0001")); err != nil {
		t.Fatal(err)
	}
	if _, lastSeen = storedAddresses(t, pool, result.Session.ID); lastSeen == nil || lastSeen.String() != "2001:db8::1" {
		t.Fatalf("last_seen_ip = %v, want the canonical form of the address", lastSeen)
	}

	// A request whose address could not be resolved records nothing rather than keeping the
	// previous one: an old address beside a fresh timestamp would be a quiet lie (FR-003).
	clock.Advance(time.Hour)
	if _, err := service.AuthenticateSession(context.Background(), result.SessionToken, netip.Addr{}); err != nil {
		t.Fatal(err)
	}
	if _, lastSeen = storedAddresses(t, pool, result.Session.ID); lastSeen != nil {
		t.Fatalf("last_seen_ip = %v, want nothing recorded when the address is unknown", lastSeen)
	}
}

// What the person reads is what was stored, through the query that is scoped to them.
func TestSessionListCarriesTheAddress(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	clock, secrets, _ := provisionOwner(t, pool)
	service, _ := newOwnerAuthService(t, pool, clock, secrets, 2, 8*time.Hour, 30*24*time.Hour)

	address := netip.MustParseAddr("203.0.113.12")
	result, err := service.LoginOwner(context.Background(), auth.OwnerLoginRequest{
		Email: "owner@example.com", Password: "correct horse battery staple",
		DeviceLabel: "Chrome on macOS", Origin: address.String(), ClientAddress: address,
	})
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := service.ListSessions(context.Background(), result.Account.ID, result.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	var current auth.SessionSummary
	for _, session := range sessions {
		if session.Current {
			current = session
		}
	}
	if current.ID == "" {
		t.Fatal("the current session was not listed")
	}
	if !current.LastSeenFrom.IsValid() || current.LastSeenFrom.String() != "203.0.113.12" {
		t.Fatalf("LastSeenFrom = %v, want the address the session was last seen from", current.LastSeenFrom)
	}
	if !current.CreatedFrom.IsValid() || current.CreatedFrom.String() != "203.0.113.12" {
		t.Fatalf("CreatedFrom = %v", current.CreatedFrom)
	}

	// The bootstrap session predates nothing here, but a session created before this feature has a
	// zero address, and a zero address is the "not recorded" state rather than a bug.
	for _, session := range sessions {
		if session.Current {
			continue
		}
		if session.LastSeenFrom.IsValid() && session.LastSeenFrom.String() == "" {
			t.Fatalf("a valid address must have a form: %#v", session)
		}
	}
}

// Nobody else's business (US4, FR-009). Two accounts on one installation, each signing in from an
// address of their own: neither may see the other's, and the owner has no more reach than the
// member does. The boundary is in the query, so the proof is at the service.
func TestOneAccountCannotSeeAnotherAccountsAddress(t *testing.T) {
	fixture := newAccountFixture(t)
	ctx := context.Background()
	ownerPassword := fixtureSecret("origin-owner")
	ownerAddress := netip.MustParseAddr("203.0.113.12")
	memberAddress := netip.MustParseAddr("198.51.100.4")

	setup, err := fixture.identity.IssueSetupCapability(ctx)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := fixture.identity.BootstrapOwner(ctx, identity.BootstrapRequest{
		Capability: setup.Token, Email: "owner@example.com", Password: ownerPassword,
		DisplayName: "Origin Owner", DeviceLabel: "Owner laptop", Origin: ownerAddress.String(),
		ClientAddress: ownerAddress, EODHDAPIKey: fixtureSecret("origin-eodhd"),
		SMTP: identity.SMTPSetupConfiguration{Host: "smtp.example.test", Port: 587, From: "access@example.test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.clock.Advance(time.Minute)
	if _, err := fixture.identity.CreateInvitation(ctx,
		identity.Actor{UserID: owner.User.ID, Role: identity.RoleOwner}, "member@example.com"); err != nil {
		t.Fatal(err)
	}
	capability := capabilityFromMail(t, fixture.mailbox.Messages(), "member@example.com")
	fixture.clock.Advance(time.Minute)
	member, err := fixture.identity.AcceptInvitation(ctx, identity.AcceptInvitationRequest{
		Capability: capability, Email: "member@example.com", DisplayName: "Origin Member",
		DeviceLabel: "Member phone", Origin: memberAddress.String(), ClientAddress: memberAddress,
	})
	if err != nil {
		t.Fatal(err)
	}

	addresses := func(userID, sessionID string) []string {
		t.Helper()
		sessions, err := fixture.auth.ListSessions(ctx, userID, sessionID)
		if err != nil {
			t.Fatal(err)
		}
		var found []string
		for _, session := range sessions {
			if session.LastSeenFrom.IsValid() {
				found = append(found, session.LastSeenFrom.String())
			}
		}
		return found
	}

	ownerSees := addresses(owner.User.ID, owner.Session.ID)
	if len(ownerSees) != 1 || ownerSees[0] != ownerAddress.String() {
		t.Fatalf("the owner sees %v, want only their own address", ownerSees)
	}
	memberSees := addresses(member.User.ID, member.Session.ID)
	if len(memberSees) != 1 || memberSees[0] != memberAddress.String() {
		t.Fatalf("the member sees %v, want only their own address", memberSees)
	}

	// And the addresses stay out of the event stream, which is the other way a private fact
	// escapes: every client event carries a payload somebody else's browser may resume.
	var leaked int
	if err := fixture.pool.QueryRow(ctx, `SELECT count(*) FROM client_events
		WHERE payload::text LIKE '%' || $1 || '%' OR payload::text LIKE '%' || $2 || '%'`,
		ownerAddress.String(), memberAddress.String()).Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	if leaked != 0 {
		t.Fatalf("%d client events carry a network address in their payload", leaked)
	}
}
