package auth

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSessionValidationAndActivityBoundaries(t *testing.T) {
	created := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	session := validSession(created)
	if err := session.Validate(); err != nil {
		t.Fatalf("valid session rejected: %v", err)
	}
	if !session.ActiveAt(created.Add(time.Hour), true) {
		t.Fatal("active session was rejected")
	}
	if session.ActiveAt(session.IdleExpiresAt, true) || session.ActiveAt(session.AbsoluteExpiresAt, true) {
		t.Fatal("expiry boundary remained active")
	}
	if session.ActiveAt(created.Add(time.Hour), false) {
		t.Fatal("inactive user session remained active")
	}

	invalid := session
	invalid.TokenDigest = []byte("plaintext-token")
	if err := invalid.Validate(); err == nil {
		t.Fatal("invalid token digest unexpectedly validated")
	}
}

func TestSessionTouchCapsIdleExpiryAtAbsoluteLifetime(t *testing.T) {
	created := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	session := validSession(created)
	session.AbsoluteExpiresAt = created.Add(10 * time.Hour)
	now := created.Add(7 * time.Hour)
	if err := session.Touch(now, 8*time.Hour); err != nil {
		t.Fatal(err)
	}
	if !session.LastSeenAt.Equal(now) || !session.IdleExpiresAt.Equal(session.AbsoluteExpiresAt) {
		t.Fatalf("touched session = %#v", session)
	}
	if err := session.Touch(session.AbsoluteExpiresAt, time.Hour); err == nil {
		t.Fatal("expired session was touched")
	}
}

func TestSessionRevocationIsIdempotentAndSummaryIsSecretFree(t *testing.T) {
	created := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	session := validSession(created)
	revokedAt := created.Add(time.Hour)
	if err := session.Revoke(RevokeLogout, revokedAt); err != nil {
		t.Fatal(err)
	}
	if err := session.Revoke(RevokeLogout, revokedAt.Add(time.Minute)); err != nil {
		t.Fatalf("idempotent revoke failed: %v", err)
	}
	if session.ActiveAt(revokedAt, true) {
		t.Fatal("revoked session remained active")
	}

	summary := session.Summary(session.ID)
	if !summary.Current || !summary.Revoked {
		t.Fatalf("session summary = %#v", summary)
	}
	value := reflect.ValueOf(summary)
	valueType := value.Type()
	for index := range value.NumField() {
		name := valueType.Field(index).Name
		for _, forbidden := range []string{"Token", "Digest", "CSRF", "Origin"} {
			if strings.Contains(name, forbidden) {
				t.Fatalf("safe summary exposes %s", name)
			}
		}
	}
}

func validSession(created time.Time) Session {
	return Session{
		ID: "50000000-0000-4000-8000-000000000001", UserID: "40000000-0000-4000-8000-000000000001",
		TokenDigest: bytes.Repeat([]byte{0x51}, 32), CSRFDigest: bytes.Repeat([]byte{0x52}, 32),
		CreatedAt: created, LastSeenAt: created, IdleExpiresAt: created.Add(8 * time.Hour),
		AbsoluteExpiresAt: created.Add(30 * 24 * time.Hour), DeviceLabel: "Chrome on macOS",
		OriginDigest: bytes.Repeat([]byte{0x53}, 32),
	}
}

// A rotation of the instance signing key ends every session, and the audit trail must record
// why. Reusing "administrative" would have hidden the cause from an operator reading it
// afterwards, so rotation has a reason of its own. The literal is used deliberately: it is
// the value the database CHECK constraint in migration 0011 permits.
func TestSessionAcceptsSigningKeyRotationAsRevokeReason(t *testing.T) {
	created := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	session := validSession(created)
	if err := session.Revoke(RevokeReason("signing_key_rotated"), created.Add(time.Hour)); err != nil {
		t.Fatalf("signing key rotation was rejected as a revocation reason: %v", err)
	}
	if session.RevokedReason != RevokeSigningKeyRotated {
		t.Fatalf("revoked reason = %q, want %q", session.RevokedReason, RevokeSigningKeyRotated)
	}
	if session.ActiveAt(created.Add(2*time.Hour), true) {
		t.Fatal("session remained active after a signing key rotation")
	}
}
