package push

import (
	"bytes"
	"encoding/base64"
	"testing"
)

// The worked example from RFC 8291 section 5.
//
// Every value below is printed in the specification itself and is public by design: the server
// private key, the subscription's public key and auth secret, the salt, and the ciphertext they
// produce. None of them belongs to anything. They exist so an implementation can be checked against
// the specification, which is why `.gitguardian.yaml` excludes this one file by name.
//
// Checking against the specification's own vector rather than against this implementation's output
// is the difference between "it encrypts consistently" and "it encrypts correctly". A push that a
// browser cannot decrypt fails silently: the push service accepts it, and nothing ever appears.
const (
	rfcPlaintext        = "V2hlbiBJIGdyb3cgdXAsIEkgd2FudCB0byBiZSBhIHdhdGVybWVsb24"
	rfcSubscriptionPub  = "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"
	rfcSubscriptionAuth = "BTBZMqHH6r4Tts7J_aSIgg"
	rfcServerPrivate    = "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"
	rfcSalt             = "DGv6ra1nlYgDCS1FRnbzlw"
	rfcCiphertext       = "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPTpK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
)

func decode(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		t.Fatalf("decode %q: %v", value, err)
	}
	return decoded
}

// TestEncryptionMatchesTheSpecification pins the whole aes128gcm body — header, ephemeral key and
// ciphertext — against RFC 8291's example, with the ephemeral key and salt supplied rather than
// generated so the output is comparable at all.
func TestEncryptionMatchesTheSpecification(t *testing.T) {
	body, err := encryptWith(
		decode(t, rfcPlaintext),
		decode(t, rfcSubscriptionPub),
		decode(t, rfcSubscriptionAuth),
		decode(t, rfcServerPrivate),
		decode(t, rfcSalt),
	)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	want := decode(t, rfcCiphertext)
	if !bytes.Equal(body, want) {
		t.Errorf("the encrypted body does not match RFC 8291 section 5\n got %x\nwant %x", body, want)
	}
}

// The header a browser reads to find the salt, the record size and the key it must agree with.
func TestTheBodyCarriesTheHeaderTheBrowserNeeds(t *testing.T) {
	body, err := encryptWith(
		decode(t, rfcPlaintext), decode(t, rfcSubscriptionPub), decode(t, rfcSubscriptionAuth),
		decode(t, rfcServerPrivate), decode(t, rfcSalt),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) < 86 {
		t.Fatalf("the body is %d bytes, shorter than an aes128gcm header", len(body))
	}
	if !bytes.Equal(body[:16], decode(t, rfcSalt)) {
		t.Errorf("the first 16 bytes are not the salt")
	}
	if body[20] != 65 {
		t.Errorf("the key length byte reads %d, want 65", body[20])
	}
	// An uncompressed P-256 point starts with 0x04.
	if body[21] != 0x04 {
		t.Errorf("the ephemeral key is not an uncompressed point")
	}
}

// Every message gets its own salt and its own ephemeral key: reusing either across two messages to
// the same subscription would let somebody holding both recover the plaintext.
func TestEveryMessageIsEncryptedFreshly(t *testing.T) {
	subscription := Subscription{
		Endpoint: "https://example.invalid/x",
		P256DH:   decode(t, rfcSubscriptionPub),
		Auth:     decode(t, rfcSubscriptionAuth),
	}
	first, err := Encrypt([]byte("hello"), subscription)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Encrypt([]byte("hello"), subscription)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first[:16], second[:16]) {
		t.Errorf("two messages shared a salt")
	}
	if bytes.Equal(first[21:86], second[21:86]) {
		t.Errorf("two messages shared an ephemeral key")
	}
	if bytes.Equal(first, second) {
		t.Errorf("two encryptions of the same text produced the same bytes")
	}
}

// A payload that cannot fit one record is refused rather than silently truncated. Nothing this
// product sends comes close — a push carries a kind and a count — so the ceiling is a guard against
// a future template, not a limit anybody meets.
func TestAnOversizedPayloadIsRefused(t *testing.T) {
	subscription := Subscription{
		Endpoint: "https://example.invalid/x",
		P256DH:   decode(t, rfcSubscriptionPub),
		Auth:     decode(t, rfcSubscriptionAuth),
	}
	if _, err := Encrypt(bytes.Repeat([]byte("x"), 4000), subscription); err == nil {
		t.Errorf("a payload too large for one record was accepted")
	}
}

func TestAMalformedSubscriptionIsRefused(t *testing.T) {
	good := Subscription{
		Endpoint: "https://example.invalid/x",
		P256DH:   decode(t, rfcSubscriptionPub),
		Auth:     decode(t, rfcSubscriptionAuth),
	}
	for name, broken := range map[string]Subscription{
		"no endpoint":  {P256DH: good.P256DH, Auth: good.Auth},
		"short key":    {Endpoint: good.Endpoint, P256DH: []byte{4, 1, 2}, Auth: good.Auth},
		"short secret": {Endpoint: good.Endpoint, P256DH: good.P256DH, Auth: []byte{1}},
		"not a point":  {Endpoint: good.Endpoint, P256DH: bytes.Repeat([]byte{9}, 65), Auth: good.Auth},
	} {
		if _, err := Encrypt([]byte("hello"), broken); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
