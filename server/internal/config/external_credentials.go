package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

type ExternalCredentialConfig struct {
	Key        []byte
	KeyVersion uint32
	Configured bool
}

func loadExternalCredentials(environment string) (ExternalCredentialConfig, error) {
	encodedKey := strings.TrimSpace(os.Getenv("EXTERNAL_CREDENTIAL_KEY"))
	versionText := strings.TrimSpace(os.Getenv("EXTERNAL_CREDENTIAL_KEY_VERSION"))
	production := environment == "production" || environment == "prod"
	if encodedKey == "" && versionText == "" {
		if production {
			return ExternalCredentialConfig{}, errors.New("EXTERNAL_CREDENTIAL_KEY is required in production")
		}
		return ExternalCredentialConfig{}, nil
	}
	if encodedKey == "" || versionText == "" {
		return ExternalCredentialConfig{}, errors.New("external credential key and version must be configured together")
	}
	key, err := base64.StdEncoding.Strict().DecodeString(encodedKey)
	if err != nil || len(key) != 32 {
		return ExternalCredentialConfig{}, errors.New("EXTERNAL_CREDENTIAL_KEY must encode exactly 32 bytes")
	}
	version, err := strconv.ParseUint(versionText, 10, 32)
	if err != nil || version == 0 || version > math.MaxUint32 {
		return ExternalCredentialConfig{}, fmt.Errorf("EXTERNAL_CREDENTIAL_KEY_VERSION must be a positive 32-bit integer")
	}
	return ExternalCredentialConfig{Key: key, KeyVersion: uint32(version), Configured: true}, nil
}
