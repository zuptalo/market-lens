package push

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/url"
	"strings"
	"time"
)

// KeyPair is the identity this instance proves to a push service (RFC 8292).
//
// One pair, generated once and kept in this product's own database. Rotating it invalidates every
// subscription in existence, and the failure is silent — the service accepts nothing and no browser
// is ever told why — which is why nothing here rotates it.
type KeyPair struct {
	Private []byte
	Public  []byte
	// Subject is the `sub` claim: how a push service reaches whoever runs this instance if its
	// traffic becomes a problem for them.
	Subject string
}

// AssertionLifetime is how long one assertion is good for. RFC 8292 §2 caps this at 24 hours and
// services enforce it; an hour is long enough for any single delivery pass and short enough that a
// captured assertion is worth little.
const AssertionLifetime = time.Hour

// GenerateKeyPair produces a P-256 pair for this instance.
func GenerateKeyPair(random io.Reader, subject string) (KeyPair, error) {
	if strings.TrimSpace(subject) == "" {
		return KeyPair{}, errors.New("a push key needs a subject a service can reach")
	}
	private, err := ecdh.P256().GenerateKey(random)
	if err != nil {
		return KeyPair{}, fmt.Errorf("generate a push key: %w", err)
	}
	return KeyPair{
		Private: private.Bytes(),
		Public:  private.PublicKey().Bytes(),
		Subject: subject,
	}, nil
}

// AuthorizationHeader signs an assertion for one endpoint's service.
//
// The audience is the endpoint's **origin**, not the whole endpoint: the path names a particular
// subscription, and putting it in a claim would tell the service which one the assertion was for.
func (k KeyPair) AuthorizationHeader(endpoint string, now time.Time) (string, error) {
	audience, err := originOf(endpoint)
	if err != nil {
		return "", err
	}
	token, err := k.sign(audience, now)
	if err != nil {
		return "", err
	}
	// The public key travels beside the token so the service can check the signature without
	// having been told anything about this instance in advance.
	return fmt.Sprintf("vapid t=%s, k=%s", token, base64.RawURLEncoding.EncodeToString(k.Public)), nil
}

func originOf(endpoint string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("read the endpoint: %w", err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", fmt.Errorf("a push endpoint is an http(s) URL, this is %q", endpoint)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("a push endpoint needs a host, this is %q", endpoint)
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

func (k KeyPair) sign(audience string, now time.Time) (string, error) {
	private, err := ecdh.P256().NewPrivateKey(k.Private)
	if err != nil {
		return "", fmt.Errorf("read the push key: %w", err)
	}
	x, y := elliptic.Unmarshal(elliptic.P256(), private.PublicKey().Bytes())
	if x == nil {
		return "", errors.New("the push key is not a point on P-256")
	}
	signing := &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y},
		D:         new(big.Int).SetBytes(k.Private),
	}

	header, err := json.Marshal(map[string]string{"typ": "JWT", "alg": "ES256"})
	if err != nil {
		return "", err
	}
	claims, err := json.Marshal(map[string]any{
		"aud": audience,
		"exp": now.Add(AssertionLifetime).Unix(),
		"sub": k.Subject,
	})
	if err != nil {
		return "", err
	}
	signed := base64.RawURLEncoding.EncodeToString(header) + "." +
		base64.RawURLEncoding.EncodeToString(claims)

	digest := sha256.Sum256([]byte(signed))
	r, s, err := ecdsa.Sign(rand.Reader, signing, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign the assertion: %w", err)
	}
	// JWS wants the two halves fixed-width and concatenated, not the ASN.1 encoding ecdsa.Sign
	// would give through SignASN1.
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	return signed + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func sha256Sum(value string) [32]byte { return sha256.Sum256([]byte(value)) }
