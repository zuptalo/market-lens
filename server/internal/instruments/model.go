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

type SearchFilter struct {
	Query    string
	MIC      string
	Country  string
	Currency string
	Active   *bool
	Cursor   string
	Limit    int
}

type SearchResult struct {
	Instrument
	Exchange Exchange
}

type SearchPage struct {
	Items      []SearchResult
	NextCursor string
}

type SessionDate string

func ParseSessionDate(value string) (SessionDate, error) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return "", errors.New("session date must be a valid YYYY-MM-DD date")
	}
	return SessionDate(value), nil
}

func (date SessionDate) String() string { return string(date) }

type Decimal string

func (decimal Decimal) String() string { return string(decimal) }

type DailyBar struct {
	SessionDate   SessionDate
	Open          Decimal
	High          Decimal
	Low           Decimal
	Close         Decimal
	AdjustedClose *string
	Volume        int64
	Currency      string
	Provider      string
	ObservedAt    time.Time
}

type HistoryCoverage struct {
	FirstSession SessionDate
	LastSession  SessionDate
	BarCount     int64
}

type QualitySummary struct {
	OpenWarnings int64
	OpenErrors   int64
}

type MarketDataSummary struct {
	LatestBar *DailyBar
	Freshness time.Time
	Coverage  HistoryCoverage
	Quality   QualitySummary
}

type Inspection struct {
	Identity SearchResult
	MarketDataSummary
}

type PriceFilter struct {
	From   SessionDate
	To     SessionDate
	Cursor string
	Limit  int
}

type PricePage struct {
	Items      []DailyBar
	NextCursor string
}

// --- Instrument exploration read model (feature 005) ---
//
// Nothing below is stored. Every value is derived by the listing and history queries from
// tables feature 002 already owns, so these types are the shape of an answer rather than the
// shape of a row.
//
// Absent statistics are pointers on purpose. A 20-session return that cannot be computed is
// *absent*, and rendering it as 0 would be a claim the data does not support (FR-007).

// FreshnessState distinguishes an instrument that is current from one whose history has
// fallen behind and from one that has no history at all. The last two are different facts
// and the specification requires them to be told apart.
type FreshnessState string

const (
	FreshnessCurrent   FreshnessState = "current"
	FreshnessStale     FreshnessState = "stale"
	FreshnessNoHistory FreshnessState = "no_history"
)

// Freshness reports how current an instrument's stored history is, measured in open
// exchange sessions rather than calendar days.
type Freshness struct {
	State FreshnessState
	// SessionsBehind counts the exchange's open sessions since the latest stored bar. It is
	// absent when there is no history to be behind.
	SessionsBehind *int
}

// ListingRow is one instrument as the universe list shows it.
type ListingRow struct {
	Instrument
	Exchange Exchange

	LatestSession  SessionDate
	LatestClose    *Decimal
	PreviousClose  *Decimal
	ChangeAbsolute *Decimal
	ChangePercent  *float64

	// The three adopted statistics are the engine's own decimals, carried as stored rather
	// than as float64: a number that has been through a binary float on its way to the screen
	// is no longer the number the engine computed (feature 013, US5-2).
	Return20   *Decimal
	Return90   *Decimal
	Volatility *Decimal

	// StoredSessions is the count of stored bars, so a reader can see *why* a statistic is
	// absent instead of guessing.
	StoredSessions int64
	Freshness      Freshness
}

// ListingSort names a column the whole result set can be ordered by. Sorting happens in the
// database over every matching row, never over the page already fetched.
type ListingSort string

const (
	SortName          ListingSort = "name"
	SortTicker        ListingSort = "ticker"
	SortExchange      ListingSort = "exchange"
	SortSector        ListingSort = "sector"
	SortCountry       ListingSort = "country"
	SortLatestClose   ListingSort = "latest_close"
	SortChangePercent ListingSort = "change_percent"
	SortReturn20      ListingSort = "return_20"
	SortReturn90      ListingSort = "return_90"
	SortVolatility    ListingSort = "volatility"
	SortFreshness     ListingSort = "freshness"
)

// ListingFilter selects and orders the universe.
type ListingFilter struct {
	// ID narrows the listing to one instrument, so the detail view derives its identity and
	// statistics from exactly the same projection as the list rather than from a second one
	// that would eventually disagree with it.
	ID         UUID
	Query      string
	MIC        string
	Country    string
	Sector     string
	Currency   string
	Status     string
	Sort       ListingSort
	Descending bool
	Cursor     string
	Limit      int
	// AsOf anchors freshness to a date rather than to the clock. The service defaults it to
	// today; tests set it so their answers do not change overnight.
	AsOf SessionDate
}

// ListingPage is one page of the universe under a filter and ordering.
type ListingPage struct {
	Items      []ListingRow
	NextCursor string
}

// SeriesBasis records whether the displayed closes are the provider's raw observations or
// its adjusted ones. Showing an adjusted series as though it were raw is exactly the kind of
// quiet distortion this feature exists to prevent (FR-014).
type SeriesBasis string

const (
	SeriesRaw              SeriesBasis = "raw"
	SeriesProviderAdjusted SeriesBasis = "provider_adjusted"
)

// ChartAction is a recorded corporate action anchored to the session it affects.
type ChartAction struct {
	ID        UUID
	Type      string
	ExDate    SessionDate
	Ratio     *Decimal
	Amount    *Decimal
	Currency  *string
	OldSymbol *string
	NewSymbol *string
}

// ChartFinding is a data-quality finding anchored to the session it concerns.
type ChartFinding struct {
	ID          UUID
	Rule        string
	Status      string
	Severity    string
	SessionDate *SessionDate
	Detail      *string
}

// HistoryWindow is everything the chart needs to draw one instrument's stored history
// honestly: the bars that exist, the sessions that are absent, and the context that explains
// a discontinuity.
type HistoryWindow struct {
	Instrument ListingRow
	Coverage   HistoryCoverage

	RequestedFrom SessionDate
	RequestedTo   SessionDate

	Bars []DailyBar
	// MissingSessions are dates the exchange was open and no bar is stored. A day the
	// exchange was closed never appears here — that distinction is the whole point.
	MissingSessions []SessionDate

	SeriesBasis SeriesBasis
	Provider    *string
	ObservedAt  *time.Time

	Actions  []ChartAction
	Findings []ChartFinding
}

// HistoryFilter bounds a history window. Sessions are counted in stored exchange sessions,
// never in calendar days, because a calendar window means a different number of observations
// on each exchange (research R7).
type HistoryFilter struct {
	Sessions int
	To       SessionDate
}
