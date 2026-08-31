package auth

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"market-lens/server/internal/authtest"
)

var fastArgon2Params = Argon2Params{
	Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
}

func TestPasswordHasherEncodesAndVerifiesArgon2idWithoutPlaintext(t *testing.T) {
	hasher, err := NewPasswordHasher(authtest.NewRandomReader(0x42), fastArgon2Params)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := hasher.Encode("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$") {
		t.Fatalf("encoded password is not Argon2id: %q", encoded)
	}
	authtest.AssertSecretAbsent(t, "correct horse battery staple", encoded)

	valid, needsRehash, err := hasher.Verify(encoded, "correct horse battery staple")
	if err != nil || !valid || needsRehash {
		t.Fatalf("valid password result = valid:%v rehash:%v err:%v", valid, needsRehash, err)
	}
	valid, _, err = hasher.Verify(encoded, "wrong password")
	if err != nil || valid {
		t.Fatalf("wrong password result = valid:%v err:%v", valid, err)
	}
}

func TestPasswordHasherDetectsRehashAndRejectsMalformedEncoding(t *testing.T) {
	oldHasher, err := NewPasswordHasher(authtest.NewRandomReader(0x24), fastArgon2Params)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := oldHasher.Encode("owner password")
	if err != nil {
		t.Fatal(err)
	}
	stronger := fastArgon2Params
	stronger.Iterations++
	newHasher, err := NewPasswordHasher(authtest.NewRandomReader(0x25), stronger)
	if err != nil {
		t.Fatal(err)
	}
	valid, needsRehash, err := newHasher.Verify(encoded, "owner password")
	if err != nil || !valid || !needsRehash {
		t.Fatalf("old encoding result = valid:%v rehash:%v err:%v", valid, needsRehash, err)
	}
	if _, _, err := newHasher.Verify("not-an-argon2-encoding", "owner password"); err == nil {
		t.Fatal("malformed encoding unexpectedly succeeded")
	}
}

func TestSecretsGenerateHighEntropyURLSafeTokensAndLeadingZeroCodes(t *testing.T) {
	pattern := make([]byte, 67)
	for index := range pattern {
		pattern[index] = byte(index)
	}
	secrets, err := NewSecrets(bytes.Repeat([]byte{0x9a}, 32), authtest.NewRandomReader(pattern...))
	if err != nil {
		t.Fatal(err)
	}

	capability, err := secrets.Capability()
	if err != nil {
		t.Fatal(err)
	}
	session, err := secrets.SessionToken()
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"capability": capability, "session": session} {
		decoded, err := base64.RawURLEncoding.DecodeString(value)
		if err != nil || len(decoded) != 32 {
			t.Fatalf("%s is not a 256-bit URL-safe token: value=%q err=%v", name, value, err)
		}
	}
	if capability == session {
		t.Fatal("capability and session token unexpectedly match")
	}

	zeroSecrets, err := NewSecrets(bytes.Repeat([]byte{0x8b}, 32), authtest.NewRandomReader(0x00))
	if err != nil {
		t.Fatal(err)
	}
	code, err := zeroSecrets.MemberCode()
	if err != nil {
		t.Fatal(err)
	}
	if code != "000000" {
		t.Fatalf("MemberCode() = %q, want leading-zero code 000000", code)
	}
}

func TestSecretsUsePurposeSeparatedDigestsAndConstantTimeVerification(t *testing.T) {
	secrets, err := NewSecrets(bytes.Repeat([]byte{0x51}, 32), authtest.NewRandomReader(0x01))
	if err != nil {
		t.Fatal(err)
	}
	value := "opaque-value"
	setupDigest := secrets.Digest(PurposeSetup, value)
	sessionDigest := secrets.Digest(PurposeSession, value)
	if len(setupDigest) != 32 || bytes.Equal(setupDigest, sessionDigest) {
		t.Fatalf("digests are not purpose-separated: setup=%x session=%x", setupDigest, sessionDigest)
	}
	if !secrets.VerifyDigest(PurposeSetup, value, setupDigest) {
		t.Fatal("matching digest was rejected")
	}
	if secrets.VerifyDigest(PurposeSession, value, setupDigest) || secrets.VerifyDigest(PurposeSetup, "wrong", setupDigest) {
		t.Fatal("wrong purpose or value was accepted")
	}
}

func TestSecretsRequireAtLeast256BitsOfServerKey(t *testing.T) {
	if _, err := NewSecrets([]byte("short"), authtest.NewRandomReader(0x01)); err == nil {
		t.Fatal("short server key unexpectedly succeeded")
	}
}

func TestSecretsSeparateSessionAndCSRFTokensAndDigests(t *testing.T) {
	secrets, err := NewSecrets(bytes.Repeat([]byte{0x61}, 32), authtest.NewRandomReader(0x10, 0x20, 0x30))
	if err != nil {
		t.Fatal(err)
	}
	sessionToken, err := secrets.SessionToken()
	if err != nil {
		t.Fatal(err)
	}
	csrfToken, err := secrets.CSRFToken()
	if err != nil {
		t.Fatal(err)
	}
	if sessionToken == csrfToken {
		t.Fatal("session and CSRF tokens unexpectedly match")
	}
	decodedCSRF, err := base64.RawURLEncoding.DecodeString(csrfToken)
	if err != nil || len(decodedCSRF) != 32 {
		t.Fatalf("CSRF token is not 256-bit URL-safe material: value=%q err=%v", csrfToken, err)
	}
	sessionDigest := secrets.Digest(PurposeSession, sessionToken)
	csrfDigest := secrets.Digest(PurposeCSRF, csrfToken)
	if bytes.Equal(sessionDigest, csrfDigest) || !secrets.VerifyDigest(PurposeCSRF, csrfToken, csrfDigest) {
		t.Fatal("session and CSRF digests are not independently purpose-separated")
	}
}
