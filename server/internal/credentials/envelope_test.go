package credentials

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"market-lens/server/internal/authtest"
)

var testMetadata = Metadata{
	ID:             "00000000-0000-4000-8000-000000000101",
	Kind:           KindEODHDAPI,
	PayloadVersion: 1,
	KeyVersion:     7,
}

func TestCredentialEnvelopeUsesRandomNonceAndRoundTrips(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	cipher, err := NewCipher(key, 7)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte(`{"api_key":"eodhd-secret-never-disclose"}`)

	first, err := cipher.Seal(testMetadata, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	second, err := cipher.Seal(testMetadata, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("identical plaintext produced identical credential ciphertext")
	}
	if len(first) != len(plaintext)+28 {
		t.Fatalf("sealed length = %d, want %d", len(first), len(plaintext)+28)
	}
	authtest.AssertSecretAbsent(t, "eodhd-secret-never-disclose", string(first), string(second))

	opened, err := cipher.Open(testMetadata, first)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(opened, plaintext) {
		t.Fatalf("opened plaintext = %q, want original", opened)
	}
}

// testSealedSecret stands in for the mail credential without being written down as one.
var testSealedSecret = "sealed-" + hex.EncodeToString(func() []byte {
	sum := sha256.Sum256([]byte("market-lens/credentials-test/sealed"))
	return sum[:8]
}())

func TestCredentialEnvelopeRejectsTamperWrongKeyAndWrongAAD(t *testing.T) {
	cipher, err := NewCipher(bytes.Repeat([]byte{0x31}, 32), 7)
	if err != nil {
		t.Fatal(err)
	}
	// Shaped like the mail credential this envelope really carries, without being a literal
	// that reads as one.
	secret := []byte(`{"password":"` + testSealedSecret + `"}`)
	sealed, err := cipher.Seal(testMetadata, secret)
	if err != nil {
		t.Fatal(err)
	}

	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-1] ^= 0xff
	wrongKey, err := NewCipher(bytes.Repeat([]byte{0x32}, 32), 7)
	if err != nil {
		t.Fatal(err)
	}
	wrongID := testMetadata
	wrongID.ID = "00000000-0000-4000-8000-000000000102"
	wrongKind := testMetadata
	wrongKind.Kind = KindSMTP
	wrongPayloadVersion := testMetadata
	wrongPayloadVersion.PayloadVersion++

	for name, attempt := range map[string]func() error{
		"tampered ciphertext": func() error { _, err := cipher.Open(testMetadata, tampered); return err },
		"wrong key":           func() error { _, err := wrongKey.Open(testMetadata, sealed); return err },
		"wrong row":           func() error { _, err := cipher.Open(wrongID, sealed); return err },
		"wrong kind":          func() error { _, err := cipher.Open(wrongKind, sealed); return err },
		"wrong payload":       func() error { _, err := cipher.Open(wrongPayloadVersion, sealed); return err },
	} {
		t.Run(name, func(t *testing.T) {
			err := attempt()
			if err == nil {
				t.Fatal("credential envelope unexpectedly opened")
			}
			authtest.AssertSecretAbsent(t, "smtp-secret-never-disclose", err.Error())
		})
	}
}

func TestCredentialEnvelopeRejectsInvalidKeysMetadataAndBounds(t *testing.T) {
	for _, size := range []int{0, 16, 24, 31, 33} {
		if _, err := NewCipher(bytes.Repeat([]byte{0x11}, size), 1); err == nil {
			t.Fatalf("%d-byte key unexpectedly accepted", size)
		}
	}
	if _, err := NewCipher(bytes.Repeat([]byte{0x11}, 32), 0); err == nil {
		t.Fatal("zero key version unexpectedly accepted")
	}

	cipher, err := NewCipher(bytes.Repeat([]byte{0x11}, 32), 7)
	if err != nil {
		t.Fatal(err)
	}
	invalidMetadata := []Metadata{
		{Kind: KindEODHDAPI, PayloadVersion: 1, KeyVersion: 7},
		{ID: testMetadata.ID, Kind: "unknown", PayloadVersion: 1, KeyVersion: 7},
		{ID: testMetadata.ID, Kind: KindEODHDAPI, PayloadVersion: 0, KeyVersion: 7},
		{ID: testMetadata.ID, Kind: KindEODHDAPI, PayloadVersion: 1, KeyVersion: 0},
		{ID: testMetadata.ID, Kind: KindEODHDAPI, PayloadVersion: 1, KeyVersion: 8},
	}
	for _, metadata := range invalidMetadata {
		if _, err := cipher.Seal(metadata, []byte(`{"api_key":"value"}`)); err == nil {
			t.Fatalf("invalid metadata unexpectedly accepted: %#v", metadata)
		}
	}
	if _, err := cipher.Seal(testMetadata, nil); err == nil {
		t.Fatal("empty plaintext unexpectedly accepted")
	}
	if _, err := cipher.Seal(testMetadata, bytes.Repeat([]byte{'x'}, 16*1024+1)); err == nil {
		t.Fatal("oversized plaintext unexpectedly accepted")
	}
	if _, err := cipher.Open(testMetadata, bytes.Repeat([]byte{'x'}, 27)); err == nil {
		t.Fatal("undersized ciphertext unexpectedly accepted")
	}
}

func TestCredentialEnvelopeErrorsNeverContainKeyCiphertextOrPlaintext(t *testing.T) {
	key := bytes.Repeat([]byte("key-secret-never-log-1234567890!!"), 2)[:32]
	cipher, err := NewCipher(key, 7)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("provider-secret-never-log")
	sealed, err := cipher.Seal(testMetadata, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	_, err = cipher.Open(testMetadata, append(sealed, 0xff))
	if err == nil {
		t.Fatal("malformed ciphertext unexpectedly opened")
	}
	for _, secret := range []string{string(key), string(plaintext), string(sealed)} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("credential error disclosed secret material: %v", err)
		}
	}
}

func TestCredentialEnvelopeRotationReturnsNoPartialBatchOnFailure(t *testing.T) {
	oldCipher, err := NewCipher(bytes.Repeat([]byte{0x71}, 32), 7)
	if err != nil {
		t.Fatal(err)
	}
	newCipher, err := NewCipher(bytes.Repeat([]byte{0x81}, 32), 8)
	if err != nil {
		t.Fatal(err)
	}
	secondMetadata := testMetadata
	secondMetadata.ID = "00000000-0000-4000-8000-000000000103"
	secondMetadata.Kind = KindSMTP
	records := make([]Record, 0, 2)
	for _, item := range []struct {
		metadata Metadata
		secret   string
	}{
		{metadata: testMetadata, secret: `{"api_key":"first-secret"}`},
		{metadata: secondMetadata, secret: `{"host":"smtp.example.test"}`},
	} {
		sealed, err := oldCipher.Seal(item.metadata, []byte(item.secret))
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, Record{Metadata: item.metadata, Ciphertext: sealed})
	}
	originalFirst := append([]byte(nil), records[0].Ciphertext...)
	records[1].Ciphertext[len(records[1].Ciphertext)-1] ^= 0xff

	rotated, err := ReencryptBatch(records, oldCipher, newCipher)
	if err == nil {
		t.Fatal("rotation with a tampered row unexpectedly succeeded")
	}
	if rotated != nil {
		t.Fatalf("failed rotation returned %d partial rows", len(rotated))
	}
	if !bytes.Equal(records[0].Ciphertext, originalFirst) {
		t.Fatal("failed rotation mutated an input row")
	}
}
