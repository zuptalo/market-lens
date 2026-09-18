// Package push speaks Web Push: it encrypts a message so only the browser that subscribed can read
// it, and signs the request so the push service knows which server sent it.
//
// Implemented against the specifications rather than imported, because everything needed is in the
// standard library at Go 1.26 and this path handles a private key. The encryption is checked
// against RFC 8291's own worked example, so it is verified against the specification rather than
// against itself — a push a browser cannot decrypt fails silently, with the service accepting it
// and nothing ever appearing.
//
//   - RFC 8291 — Message Encryption for Web Push
//   - RFC 8188 — Encrypted Content-Encoding (the aes128gcm content coding)
//   - RFC 8292 — VAPID, the assertion identifying this server
package push

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

// Subscription is exactly what a browser handed the page: where to send, its public point, and the
// shared secret that salts the key derivation.
type Subscription struct {
	Endpoint string
	P256DH   []byte
	Auth     []byte
}

const (
	saltLength = 16
	keyLength  = 65
	authLength = 16
	recordSize = 4096
	// The header is salt, record size, key length, key — then the ciphertext. A record must also
	// hold the padding delimiter and GCM's tag.
	headerLength = saltLength + 4 + 1 + keyLength
	overhead     = 16 + 1 // GCM tag, and the delimiter byte RFC 8188 appends
	maxPayload   = recordSize - headerLength - overhead
)

func (s Subscription) validate() error {
	if s.Endpoint == "" {
		return errors.New("a subscription needs an endpoint")
	}
	if len(s.P256DH) != keyLength {
		return fmt.Errorf("a subscription key is %d bytes, want %d", len(s.P256DH), keyLength)
	}
	if len(s.Auth) != authLength {
		return fmt.Errorf("a subscription secret is %d bytes, want %d", len(s.Auth), authLength)
	}
	return nil
}

// Encrypt seals a payload for one subscription, with a fresh salt and a fresh ephemeral key.
//
// Fresh both, every time: reusing either across two messages to the same subscription would let
// somebody holding both recover the plaintext.
func Encrypt(payload []byte, subscription Subscription) ([]byte, error) {
	if err := subscription.validate(); err != nil {
		return nil, err
	}
	ephemeral, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate an ephemeral key: %w", err)
	}
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate a salt: %w", err)
	}
	return encryptWith(payload, subscription.P256DH, subscription.Auth, ephemeral.Bytes(), salt)
}

// encryptWith is Encrypt with the ephemeral key and salt supplied, so the result can be compared
// against RFC 8291's worked example. Nothing but the test calls it with fixed values.
func encryptWith(payload, subscriptionKey, authSecret, ephemeralPrivate, salt []byte) ([]byte, error) {
	if len(payload) > maxPayload {
		return nil, fmt.Errorf("a payload of %d bytes does not fit one record of %d", len(payload), recordSize)
	}
	if len(salt) != saltLength {
		return nil, fmt.Errorf("a salt is %d bytes, want %d", len(salt), saltLength)
	}

	private, err := ecdh.P256().NewPrivateKey(ephemeralPrivate)
	if err != nil {
		return nil, fmt.Errorf("read the ephemeral key: %w", err)
	}
	recipient, err := ecdh.P256().NewPublicKey(subscriptionKey)
	if err != nil {
		return nil, fmt.Errorf("read the subscription key: %w", err)
	}
	shared, err := private.ECDH(recipient)
	if err != nil {
		return nil, fmt.Errorf("agree a key: %w", err)
	}
	serverPublic := private.PublicKey().Bytes()

	// RFC 8291 §3.4. The pseudo-random key is derived from the agreement using the subscription's
	// auth secret as salt, over an info string that binds both public keys — so a message encrypted
	// for one subscription cannot be replayed at another.
	keyInfo := make([]byte, 0, len("WebPush: info\x00")+keyLength*2)
	keyInfo = append(keyInfo, []byte("WebPush: info\x00")...)
	keyInfo = append(keyInfo, subscriptionKey...)
	keyInfo = append(keyInfo, serverPublic...)
	ikm, err := hkdf.Key(sha256.New, shared, authSecret, string(keyInfo), 32)
	if err != nil {
		return nil, fmt.Errorf("derive the input key: %w", err)
	}

	contentKey, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, fmt.Errorf("derive the content key: %w", err)
	}
	nonce, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, fmt.Errorf("derive the nonce: %w", err)
	}

	block, err := aes.NewCipher(contentKey)
	if err != nil {
		return nil, fmt.Errorf("prepare the cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("prepare the mode: %w", err)
	}

	// RFC 8188 §2: the record is the plaintext followed by a delimiter. 0x02 marks the last record,
	// and this implementation always sends exactly one.
	record := make([]byte, 0, len(payload)+1)
	record = append(record, payload...)
	record = append(record, 0x02)

	// The header a browser reads to find the salt, the record size and the key to agree with.
	body := make([]byte, 0, headerLength+len(record)+16)
	body = append(body, salt...)
	body = binary.BigEndian.AppendUint32(body, recordSize)
	body = append(body, byte(len(serverPublic)))
	body = append(body, serverPublic...)

	return gcm.Seal(body, nonce, record, nil), nil
}
