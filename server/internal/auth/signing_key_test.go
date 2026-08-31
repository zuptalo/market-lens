package auth

import (
	"bytes"
	"crypto/rand"
	"strings"
	"testing"
)

const suppliedTestSecret = "supplied-auth-secret-that-is-long-enough-32"

func TestSigningKeyFingerprintIsStableOneWayAndDistinct(t *testing.T) {
	first := bytes.Repeat([]byte{0xa1}, 48)
	second := bytes.Repeat([]byte{0xa2}, 48)

	fingerprint := SigningKeyFingerprint(first)
	if len(fingerprint) != 32 {
		t.Fatalf("fingerprint length = %d, want 32", len(fingerprint))
	}
	if !bytes.Equal(fingerprint, SigningKeyFingerprint(first)) {
		t.Fatal("fingerprint is not stable for one key")
	}
	if bytes.Equal(fingerprint, SigningKeyFingerprint(second)) {
		t.Fatal("distinct keys produced the same fingerprint")
	}
	// The fingerprint is stored, so it must not carry the key it identifies.
	if bytes.Contains(fingerprint, first) {
		t.Fatal("fingerprint contains its own key material")
	}
}

func TestGenerateSigningKeyProducesDistinctStrongKeys(t *testing.T) {
	first, err := GenerateSigningKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 48 {
		t.Fatalf("generated key length = %d, want 48", len(first))
	}
	second, err := GenerateSigningKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("two generated keys were identical")
	}
	// The generated key must satisfy the constructor that consumes it.
	if _, err := NewSecrets(first, rand.Reader); err != nil {
		t.Fatalf("generated key rejected by NewSecrets: %v", err)
	}
}

// TestResolveSigningKeyCoversEveryOutcome walks the resolution table in
// specs/009-self-provisioned-keys/data-model.md. The refusals matter as much as the
// successes: every one of them exists so that an installation is never silently re-keyed,
// which would sign every user out with no explanation.
func TestResolveSigningKeyCoversEveryOutcome(t *testing.T) {
	provisioned := bytes.Repeat([]byte{0xb1}, 48)
	suppliedFingerprint := SigningKeyFingerprint([]byte(suppliedTestSecret))

	storedProvisioned := &SigningKeyRecord{
		Source: SigningKeyProvisioned, KeyMaterial: provisioned,
		Fingerprint: SigningKeyFingerprint(provisioned), Generation: 3,
	}
	storedSupplied := &SigningKeyRecord{
		Source: SigningKeySupplied, Fingerprint: suppliedFingerprint, Generation: 1,
	}

	t.Run("empty database provisions", func(t *testing.T) {
		resolution, err := ResolveSigningKey(nil, "", rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if resolution.Provision == nil {
			t.Fatal("no row was offered for persistence")
		}
		if resolution.Provision.Source != SigningKeyProvisioned || resolution.Provision.Generation != 1 {
			t.Fatalf("provisioned row = %#v", resolution.Provision)
		}
		if len(resolution.Key) != 48 || !bytes.Equal(resolution.Key, resolution.Provision.KeyMaterial) {
			t.Fatal("resolved key does not match the row offered for persistence")
		}
		if !bytes.Equal(resolution.Provision.Fingerprint, SigningKeyFingerprint(resolution.Key)) {
			t.Fatal("provisioned fingerprint does not match its key")
		}
	})

	t.Run("empty database with supplied secret stores no key material", func(t *testing.T) {
		resolution, err := ResolveSigningKey(nil, suppliedTestSecret, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if resolution.Provision == nil || resolution.Provision.Source != SigningKeySupplied {
			t.Fatalf("provisioned row = %#v, want a supplied row", resolution.Provision)
		}
		// The operator's own secret must never be written to the database.
		if resolution.Provision.KeyMaterial != nil {
			t.Fatal("a supplied AUTH_SECRET was persisted as key material")
		}
		if !bytes.Equal(resolution.Provision.Fingerprint, suppliedFingerprint) {
			t.Fatal("supplied row recorded the wrong fingerprint")
		}
		if string(resolution.Key) != suppliedTestSecret {
			t.Fatal("supplied secret did not take precedence on an empty database")
		}
	})

	t.Run("supplied row with matching secret reuses it", func(t *testing.T) {
		resolution, err := ResolveSigningKey(storedSupplied, suppliedTestSecret, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if resolution.Provision != nil {
			t.Fatal("an existing installation was rewritten")
		}
		if string(resolution.Key) != suppliedTestSecret || resolution.Source != SigningKeySupplied {
			t.Fatalf("resolution = %#v", resolution)
		}
	})

	t.Run("provisioned row reuses the stored key", func(t *testing.T) {
		resolution, err := ResolveSigningKey(storedProvisioned, "", rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if resolution.Provision != nil {
			t.Fatal("an existing installation was rewritten")
		}
		if !bytes.Equal(resolution.Key, provisioned) || resolution.Generation != 3 {
			t.Fatalf("resolution = %#v", resolution)
		}
	})

	refusals := []struct {
		name     string
		stored   *SigningKeyRecord
		supplied string
		mentions []string
	}{
		{
			name: "supplied secret was removed", stored: storedSupplied, supplied: "",
			mentions: []string{"AUTH_SECRET", "signing-key rotate"},
		},
		{
			name: "supplied secret changed", stored: storedSupplied, supplied: "a-different-supplied-secret-value-32b",
			mentions: []string{"AUTH_SECRET", "signing-key rotate"},
		},
		{
			name: "supplied secret conflicts with a provisioned key", stored: storedProvisioned, supplied: suppliedTestSecret,
			mentions: []string{"AUTH_SECRET", "signing-key rotate"},
		},
	}
	for _, refusal := range refusals {
		t.Run(refusal.name, func(t *testing.T) {
			resolution, err := ResolveSigningKey(refusal.stored, refusal.supplied, rand.Reader)
			if err == nil {
				t.Fatal("a disagreeing configuration was silently resolved")
			}
			if resolution.Key != nil || resolution.Provision != nil {
				t.Fatal("a refused resolution still produced a key or a row")
			}
			for _, mention := range refusal.mentions {
				if !strings.Contains(err.Error(), mention) {
					t.Errorf("refusal does not name %q: %v", mention, err)
				}
			}
			assertNoSigningKeyDisclosure(t, err.Error(), refusal.stored, refusal.supplied)
		})
	}
}

// assertNoSigningKeyDisclosure enforces FR-006 on operator-facing text: a refusal may name
// the variable and the remedy, but never key material, a fingerprint, or a key length.
func assertNoSigningKeyDisclosure(t *testing.T, text string, stored *SigningKeyRecord, supplied string) {
	t.Helper()
	if supplied != "" && strings.Contains(text, supplied) {
		t.Errorf("message disclosed the supplied secret: %s", text)
	}
	if stored == nil {
		return
	}
	if len(stored.KeyMaterial) > 0 && strings.Contains(text, string(stored.KeyMaterial)) {
		t.Errorf("message disclosed stored key material: %s", text)
	}
	if len(stored.Fingerprint) > 0 && strings.Contains(text, string(stored.Fingerprint)) {
		t.Errorf("message disclosed the stored fingerprint: %s", text)
	}
}
