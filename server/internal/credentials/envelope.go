// Package credentials protects external-service configuration persisted in PostgreSQL.
package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

const maxPlaintextBytes = 16 * 1024

type Kind string

const (
	KindEODHDAPI Kind = "eodhd_api"
	KindSMTP     Kind = "smtp"
)

type Metadata struct {
	ID             string
	Kind           Kind
	PayloadVersion uint16
	KeyVersion     uint32
}

type Record struct {
	Metadata   Metadata
	Ciphertext []byte
}

type Cipher struct {
	aead       cipher.AEAD
	keyVersion uint32
}

func (c *Cipher) KeyVersion() uint32 {
	if c == nil {
		return 0
	}
	return c.keyVersion
}

func NewCipher(key []byte, keyVersion uint32) (*Cipher, error) {
	if len(key) != 32 {
		return nil, errors.New("external credential key must contain exactly 32 bytes")
	}
	if keyVersion == 0 {
		return nil, errors.New("external credential key version must be positive")
	}
	block, err := aes.NewCipher(append([]byte(nil), key...))
	if err != nil {
		return nil, errors.New("initialize external credential cipher")
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, errors.New("initialize external credential envelope")
	}
	return &Cipher{aead: aead, keyVersion: keyVersion}, nil
}

func (c *Cipher) Seal(metadata Metadata, plaintext []byte) ([]byte, error) {
	aad, err := c.additionalData(metadata)
	if err != nil {
		return nil, err
	}
	if len(plaintext) == 0 || len(plaintext) > maxPlaintextBytes {
		return nil, errors.New("external credential payload size is invalid")
	}
	return c.aead.Seal(nil, nil, plaintext, aad), nil
}

func (c *Cipher) Open(metadata Metadata, ciphertext []byte) ([]byte, error) {
	aad, err := c.additionalData(metadata)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < c.aead.Overhead()+1 || len(ciphertext) > maxPlaintextBytes+c.aead.Overhead() {
		return nil, errors.New("external credential ciphertext size is invalid")
	}
	plaintext, err := c.aead.Open(nil, nil, ciphertext, aad)
	if err != nil {
		return nil, errors.New("external credential authentication failed")
	}
	return plaintext, nil
}

func (c *Cipher) additionalData(metadata Metadata) ([]byte, error) {
	if !validUUID(metadata.ID) || (metadata.Kind != KindEODHDAPI && metadata.Kind != KindSMTP) ||
		metadata.PayloadVersion == 0 || metadata.KeyVersion == 0 || metadata.KeyVersion != c.keyVersion {
		return nil, errors.New("external credential metadata is invalid")
	}
	identifier := []byte(metadata.ID)
	kind := []byte(metadata.Kind)
	aad := make([]byte, 0, 2+len(identifier)+2+len(kind)+2+4+16)
	aad = appendField(aad, []byte("market-lens/v1"))
	aad = appendField(aad, identifier)
	aad = appendField(aad, kind)
	aad = binary.BigEndian.AppendUint16(aad, metadata.PayloadVersion)
	aad = binary.BigEndian.AppendUint32(aad, metadata.KeyVersion)
	return aad, nil
}

func appendField(target, value []byte) []byte {
	target = binary.BigEndian.AppendUint16(target, uint16(len(value)))
	return append(target, value...)
}

func ReencryptBatch(records []Record, oldCipher, newCipher *Cipher) ([]Record, error) {
	if oldCipher == nil || newCipher == nil || oldCipher.keyVersion == newCipher.keyVersion || len(records) == 0 {
		return nil, errors.New("external credential rotation input is invalid")
	}
	rotated := make([]Record, 0, len(records))
	for _, record := range records {
		plaintext, err := oldCipher.Open(record.Metadata, record.Ciphertext)
		if err != nil {
			return nil, err
		}
		metadata := record.Metadata
		metadata.KeyVersion = newCipher.keyVersion
		ciphertext, sealErr := newCipher.Seal(metadata, plaintext)
		clear(plaintext)
		if sealErr != nil {
			return nil, sealErr
		}
		rotated = append(rotated, Record{Metadata: metadata, Ciphertext: ciphertext})
	}
	return rotated, nil
}

func validUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for index, character := range strings.ToLower(value) {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}

func (metadata Metadata) String() string {
	return fmt.Sprintf("%s/%s/v%d/key-v%d", metadata.ID, metadata.Kind, metadata.PayloadVersion, metadata.KeyVersion)
}
