package push

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
	"time"
)

func testKeyPair(t *testing.T) KeyPair {
	t.Helper()
	pair, err := GenerateKeyPair(rand.Reader, "mailto:owner@example.com")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return pair
}

// A key pair this instance owns. It is generated once and kept, because rotating it invalidates
// every subscription in existence and the failure is silent.
func TestAGeneratedKeyPairIsAUsableP256Pair(t *testing.T) {
	pair := testKeyPair(t)
	if len(pair.Private) != 32 {
		t.Errorf("the private scalar is %d bytes, want 32", len(pair.Private))
	}
	if len(pair.Public) != 65 || pair.Public[0] != 0x04 {
		t.Errorf("the public key is not an uncompressed P-256 point: %d bytes", len(pair.Public))
	}
	// The halves belong together: a mismatched pair signs assertions nothing will accept.
	private, err := ecdh.P256().NewPrivateKey(pair.Private)
	if err != nil {
		t.Fatalf("read the private key: %v", err)
	}
	if string(private.PublicKey().Bytes()) != string(pair.Public) {
		t.Errorf("the public key is not the one this private key produces")
	}
	// Two generations differ. A deterministic key would be the same on every deployment.
	if other := testKeyPair(t); string(other.Private) == string(pair.Private) {
		t.Errorf("two generated keys were identical")
	}
}

// RFC 8292: the assertion names the push service it is for, when it stops being valid, and who to
// contact. A service will reject one that names a different origin, which is what stops an
// assertion captured from one service being replayed at another.
func TestTheAssertionNamesTheServiceAndExpires(t *testing.T) {
	pair := testKeyPair(t)
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

	header, err := pair.AuthorizationHeader("https://fcm.googleapis.com/fcm/send/abc", now)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if !strings.HasPrefix(header, "vapid t=") || !strings.Contains(header, ", k=") {
		t.Fatalf("the header does not carry a token and a key: %q", header)
	}

	token := strings.TrimPrefix(strings.Split(header, ", k=")[0], "vapid t=")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("the token has %d parts, want three", len(parts))
	}

	var head struct{ Typ, Alg string }
	decodePart(t, parts[0], &head)
	if head.Alg != "ES256" || head.Typ != "JWT" {
		t.Errorf("the header reads %+v", head)
	}

	var claims struct {
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
		Sub string `json:"sub"`
	}
	decodePart(t, parts[1], &claims)
	// The origin of the endpoint, not the whole endpoint: the path names a subscription, and
	// putting it in a claim would tell the service which one this assertion was for.
	if claims.Aud != "https://fcm.googleapis.com" {
		t.Errorf("the audience is %q, want the service's origin alone", claims.Aud)
	}
	if claims.Sub != "mailto:owner@example.com" {
		t.Errorf("the subject is %q", claims.Sub)
	}
	expiry := time.Unix(claims.Exp, 0).UTC()
	if !expiry.After(now) {
		t.Errorf("the assertion expired before it was made")
	}
	// RFC 8292 §2 caps this at 24 hours, and services enforce it.
	if expiry.Sub(now) > 24*time.Hour {
		t.Errorf("the assertion lasts %s, longer than a day", expiry.Sub(now))
	}

	// The key travels beside the token so the service can check the signature without knowing us.
	advertised := strings.Split(header, ", k=")[1]
	if decoded := decode(t, advertised); string(decoded) != string(pair.Public) {
		t.Errorf("the advertised key is not this instance's public key")
	}
}

// The signature is checkable with the public key alone, which is the whole point of the assertion.
func TestTheAssertionVerifiesAgainstThePublicKey(t *testing.T) {
	pair := testKeyPair(t)
	header, err := pair.AuthorizationHeader("https://example.invalid/push/abc", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimPrefix(strings.Split(header, ", k=")[0], "vapid t=")
	parts := strings.Split(token, ".")

	signed := parts[0] + "." + parts[1]
	signature := decode(t, parts[2])
	if len(signature) != 64 {
		t.Fatalf("an ES256 signature is 64 bytes, this is %d", len(signature))
	}

	x, y := elliptic.Unmarshal(elliptic.P256(), pair.Public)
	if x == nil {
		t.Fatal("the public key is not a point on P-256")
	}
	public := &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}
	digest := sha256Sum(signed)
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:])
	if !ecdsa.Verify(public, digest[:], r, s) {
		t.Errorf("the assertion does not verify against the key it advertises")
	}
}

func TestAnUnusableEndpointIsRefused(t *testing.T) {
	pair := testKeyPair(t)
	for _, endpoint := range []string{"", "not a url", "ftp://example.invalid/x", "/relative"} {
		if _, err := pair.AuthorizationHeader(endpoint, time.Now()); err == nil {
			t.Errorf("%q was accepted as a push endpoint", endpoint)
		}
	}
}

func decodePart(t *testing.T, part string, into any) {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(part)
	if err != nil {
		t.Fatalf("decode %q: %v", part, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("read %s: %v", raw, err)
	}
}
