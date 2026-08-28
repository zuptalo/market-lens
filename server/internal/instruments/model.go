// Package instruments owns exchange-qualified security identity and curated-universe
// membership. Persistence and provider behavior live behind this package's models.
package instruments

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

type UUID string

func ParseUUID(value string) (UUID, error) {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return "", errors.New("UUID must use canonical form")
	}
	decoded, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	if err != nil || len(decoded) != 16 {
		return "", errors.New("UUID contains invalid hexadecimal data")
	}
	version := decoded[6] >> 4
	if version < 1 || version > 5 || decoded[8]&0xc0 != 0x80 {
		return "", errors.New("UUID version or variant is invalid")
	}
	return UUID(strings.ToLower(value)), nil
}

func NewUUID() (UUID, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate UUID: %w", err)
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	encoded := hex.EncodeToString(value[:])
	return UUID(encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]), nil
}

func (id UUID) String() string { return string(id) }

func (id UUID) Valid() bool {
	parsed, err := ParseUUID(string(id))
	return err == nil && parsed == id
}

type Exchange struct {
	ID        UUID
	MIC       string
	Name      string
	Country   string
	Currency  string
	Timezone  string
	Active    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type InstrumentType string

const InstrumentTypeCommonStock InstrumentType = "common_stock"

type PurchasabilityStatus string

const (
	PurchasabilityUserConfirmed PurchasabilityStatus = "user_confirmed"
	PurchasabilityUnverified    PurchasabilityStatus = "unverified"
	PurchasabilityUnavailable   PurchasabilityStatus = "unavailable"
)

type Instrument struct {
	ID                   UUID
	ExchangeID           UUID
	ISIN                 string
	Ticker               string
	Name                 string
	Currency             string
	Country              string
	Type                 InstrumentType
	Sector               string
	Industry             string
	Active               bool
	PurchasabilityStatus PurchasabilityStatus
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type ProviderMapping struct {
	Provider       string
	ProviderSymbol string
	InstrumentID   UUID
	Active         bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type ResearchUniverse struct {
	ID          UUID
	Code        string
	Name        string
	Description string
	Active      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type UniverseMembership struct {
	UniverseID     UUID
	InstrumentID   UUID
	IncludedFrom   time.Time
	IncludedTo     *time.Time
	CurationSource string
	CurationNote   string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
