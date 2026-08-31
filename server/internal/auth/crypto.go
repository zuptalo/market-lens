// Package auth implements authentication security primitives and session behavior.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

type Argon2Params struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

func DefaultArgon2Params() Argon2Params {
	return Argon2Params{Memory: 19 * 1024, Iterations: 2, Parallelism: 1, SaltLength: 16, KeyLength: 32}
}

type PasswordHasher struct {
	mu     sync.Mutex
	random io.Reader
	params Argon2Params
}

func NewPasswordHasher(random io.Reader, params Argon2Params) (*PasswordHasher, error) {
	if random == nil {
		return nil, errors.New("password hasher requires a random source")
	}
	if err := validateArgon2Params(params); err != nil {
		return nil, err
	}
	return &PasswordHasher{random: random, params: params}, nil
}

func (h *PasswordHasher) Encode(password string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	salt := make([]byte, h.params.SaltLength)
	if _, err := io.ReadFull(h.random, salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, h.params.Iterations, h.params.Memory, h.params.Parallelism, h.params.KeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, h.params.Memory,
		h.params.Iterations, h.params.Parallelism, base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash)), nil
}

func (h *PasswordHasher) Verify(encoded, password string) (valid bool, needsRehash bool, err error) {
	params, salt, expected, err := parseArgon2id(encoded)
	if err != nil {
		return false, false, err
	}
	actual := argon2.IDKey([]byte(password), salt, params.Iterations, params.Memory, params.Parallelism, params.KeyLength)
	valid = subtle.ConstantTimeCompare(actual, expected) == 1
	return valid, valid && params != h.params, nil
}

func validateArgon2Params(params Argon2Params) error {
	if params.Memory < 8*1024 || params.Memory > 1024*1024 {
		return errors.New("Argon2 memory must be between 8 MiB and 1 GiB")
	}
	if params.Iterations == 0 || params.Iterations > 20 {
		return errors.New("Argon2 iterations must be between 1 and 20")
	}
	if params.Parallelism == 0 || params.Parallelism > 32 || params.Memory < 8*uint32(params.Parallelism) {
		return errors.New("Argon2 parallelism is invalid")
	}
	if params.SaltLength < 16 || params.SaltLength > 64 {
		return errors.New("Argon2 salt length must be between 16 and 64 bytes")
	}
	if params.KeyLength < 16 || params.KeyLength > 64 {
		return errors.New("Argon2 key length must be between 16 and 64 bytes")
	}
	return nil
}

func parseArgon2id(encoded string) (Argon2Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return Argon2Params{}, nil, nil, errors.New("invalid Argon2id encoding")
	}
	parameters := strings.Split(parts[3], ",")
	if len(parameters) != 3 {
		return Argon2Params{}, nil, nil, errors.New("invalid Argon2id parameters")
	}
	memory, err := parseUintParameter(parameters[0], "m=", 32)
	if err != nil {
		return Argon2Params{}, nil, nil, err
	}
	iterations, err := parseUintParameter(parameters[1], "t=", 32)
	if err != nil {
		return Argon2Params{}, nil, nil, err
	}
	parallelism, err := parseUintParameter(parameters[2], "p=", 8)
	if err != nil {
		return Argon2Params{}, nil, nil, err
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil {
		return Argon2Params{}, nil, nil, errors.New("invalid Argon2id salt")
	}
	hash, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil {
		return Argon2Params{}, nil, nil, errors.New("invalid Argon2id hash")
	}
	params := Argon2Params{Memory: uint32(memory), Iterations: uint32(iterations), Parallelism: uint8(parallelism), SaltLength: uint32(len(salt)), KeyLength: uint32(len(hash))}
	if err := validateArgon2Params(params); err != nil {
		return Argon2Params{}, nil, nil, errors.New("invalid Argon2id parameters")
	}
	return params, salt, hash, nil
}

func parseUintParameter(value, prefix string, bits int) (uint64, error) {
	if !strings.HasPrefix(value, prefix) {
		return 0, errors.New("invalid Argon2id parameters")
	}
	parsed, err := strconv.ParseUint(strings.TrimPrefix(value, prefix), 10, bits)
	if err != nil {
		return 0, errors.New("invalid Argon2id parameters")
	}
	return parsed, nil
}

type DigestPurpose string

const (
	PurposeSetup      DigestPurpose = "setup"
	PurposeInvitation DigestPurpose = "invitation"
	PurposeRecovery   DigestPurpose = "recovery"
	PurposeSession    DigestPurpose = "session"
	PurposeCSRF       DigestPurpose = "csrf"
	PurposeMemberCode DigestPurpose = "member_code"
	PurposeOrigin     DigestPurpose = "origin"
)

type Secrets struct {
	mu     sync.Mutex
	key    []byte
	random io.Reader
}

func NewSecrets(key []byte, random io.Reader) (*Secrets, error) {
	if len(key) < 32 {
		return nil, errors.New("authentication key must contain at least 32 bytes")
	}
	if random == nil {
		random = rand.Reader
	}
	return &Secrets{key: append([]byte(nil), key...), random: random}, nil
}

func (s *Secrets) Capability() (string, error) { return s.randomToken() }

func (s *Secrets) SessionToken() (string, error) { return s.randomToken() }

func (s *Secrets) CSRFToken() (string, error) { return s.randomToken() }

func (s *Secrets) randomToken() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value := make([]byte, 32)
	if _, err := io.ReadFull(s.random, value); err != nil {
		return "", fmt.Errorf("generate authentication token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func (s *Secrets) MemberCode() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, err := rand.Int(s.random, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("generate member code: %w", err)
	}
	return fmt.Sprintf("%06d", value.Int64()), nil
}

func (s *Secrets) Digest(purpose DigestPurpose, value string) []byte {
	digest := hmac.New(sha256.New, s.key)
	_, _ = digest.Write([]byte("market-lens/auth/"))
	_, _ = digest.Write([]byte(purpose))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(value))
	return digest.Sum(nil)
}

func (s *Secrets) VerifyDigest(purpose DigestPurpose, value string, expected []byte) bool {
	return hmac.Equal(s.Digest(purpose, value), expected)
}
