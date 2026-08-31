package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
)

// SigningKeySource records how an installation obtained its signing key.
type SigningKeySource string

const (
	// SigningKeyProvisioned means the application generated the key and stored it, so it
	// travels with the database and a restore preserves every session.
	SigningKeyProvisioned SigningKeySource = "provisioned"
	// SigningKeySupplied means the operator sets AUTH_SECRET. The value itself is never
	// stored; only a fingerprint of it is, so a changed or removed value can be reported.
	SigningKeySupplied SigningKeySource = "supplied"
)

// signingKeyBytes matches the strength the installer has always generated
// (`openssl rand -base64 48`), so provisioning does not weaken an installation.
const signingKeyBytes = 48

// signingKeyFingerprintLabel domain-separates the fingerprint from every other digest this
// package derives from the same key.
const signingKeyFingerprintLabel = "market-lens/instance-signing-key/fingerprint/v1"

// SigningKeyRecord is the stored singleton row from instance_signing_key.
type SigningKeyRecord struct {
	Source      SigningKeySource
	KeyMaterial []byte
	Fingerprint []byte
	Generation  int
}

// SigningKeyResolution is the outcome of deciding which key an instance must sign with.
// Provision is non-nil when the caller must persist that row before using the key.
type SigningKeyResolution struct {
	Key        []byte
	Source     SigningKeySource
	Generation int
	Provision  *SigningKeyRecord
}

// SigningKeyFingerprint is a one-way identifier for a signing key: an HMAC of a fixed public
// label under the key itself. Storing it lets a start detect that a supplied AUTH_SECRET has
// changed or gone missing without ever storing the secret. It is the same construction
// Secrets.Digest already writes to sessions.token_digest, so the database gains no capability
// it did not already have.
func SigningKeyFingerprint(key []byte) []byte {
	digest := hmac.New(sha256.New, key)
	_, _ = digest.Write([]byte(signingKeyFingerprintLabel))
	return digest.Sum(nil)
}

// GenerateSigningKey reads fresh key material from a cryptographically secure source.
func GenerateSigningKey(random io.Reader) ([]byte, error) {
	if random == nil {
		return nil, errors.New("signing key generation requires a random source")
	}
	key := make([]byte, signingKeyBytes)
	if _, err := io.ReadFull(random, key); err != nil {
		return nil, fmt.Errorf("generate instance signing key: %w", err)
	}
	return key, nil
}

// ResolveSigningKey decides which key an instance signs with, given the stored row (nil when
// none exists) and any explicitly supplied AUTH_SECRET. It is pure: it writes nothing, and
// returns the row the caller must persist when one is needed.
//
// Every refusal below exists for one reason: resolving a disagreement silently would sign
// every user out with no explanation, which is the exact failure this feature removes. A
// refusal names the value and the remedy and discloses no key material.
func ResolveSigningKey(stored *SigningKeyRecord, supplied string, random io.Reader) (SigningKeyResolution, error) {
	if stored == nil {
		if supplied == "" {
			key, err := GenerateSigningKey(random)
			if err != nil {
				return SigningKeyResolution{}, err
			}
			record := &SigningKeyRecord{
				Source: SigningKeyProvisioned, KeyMaterial: key,
				Fingerprint: SigningKeyFingerprint(key), Generation: 1,
			}
			return SigningKeyResolution{
				Key: key, Source: SigningKeyProvisioned, Generation: 1, Provision: record,
			}, nil
		}
		// A supplied value wins on a fresh database and nothing is provisioned, so an
		// existing deployment that has always set AUTH_SECRET is never re-keyed. Only the
		// fingerprint is recorded; the operator's secret stays outside the database.
		record := &SigningKeyRecord{
			Source: SigningKeySupplied, Fingerprint: SigningKeyFingerprint([]byte(supplied)), Generation: 1,
		}
		return SigningKeyResolution{
			Key: []byte(supplied), Source: SigningKeySupplied, Generation: 1, Provision: record,
		}, nil
	}

	switch stored.Source {
	case SigningKeySupplied:
		if supplied == "" {
			return SigningKeyResolution{}, errors.New(
				"AUTH_SECRET was supplied when this installation was first started and is now missing; " +
					"restore it, or run auth signing-key rotate to replace it and sign everybody out")
		}
		if subtle.ConstantTimeCompare(SigningKeyFingerprint([]byte(supplied)), stored.Fingerprint) != 1 {
			return SigningKeyResolution{}, errors.New(
				"the supplied AUTH_SECRET does not match the value this installation was started with; " +
					"restore the previous value, or run auth signing-key rotate to adopt a new one")
		}
		return SigningKeyResolution{
			Key: []byte(supplied), Source: SigningKeySupplied, Generation: stored.Generation,
		}, nil
	case SigningKeyProvisioned:
		if supplied != "" {
			return SigningKeyResolution{}, errors.New(
				"this installation provisioned its own signing key; remove AUTH_SECRET from the " +
					"environment, or run auth signing-key rotate to replace the stored key")
		}
		if len(stored.KeyMaterial) < 32 {
			return SigningKeyResolution{}, errors.New("the stored instance signing key is unusable")
		}
		return SigningKeyResolution{
			Key: stored.KeyMaterial, Source: SigningKeyProvisioned, Generation: stored.Generation,
		}, nil
	default:
		return SigningKeyResolution{}, errors.New("the stored instance signing key has an unknown source")
	}
}
