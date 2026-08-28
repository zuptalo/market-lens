// Package marketdata owns provider-neutral daily observations, import accounting,
// corrections, corporate-action context, and data-quality findings.
package marketdata

import (
	"errors"
	"strings"
	"time"

	"market-lens/server/internal/instruments"
)

type Decimal string

func ParseDecimal(value string) (Decimal, error) {
	if value == "" || strings.TrimSpace(value) != value {
		return "", errors.New("decimal must not be empty or padded")
	}
	sign := ""
	if value[0] == '-' {
		sign, value = "-", value[1:]
	}
	if value == "" || strings.Count(value, ".") > 1 {
		return "", errors.New("decimal has invalid syntax")
	}
	parts := strings.SplitN(value, ".", 2)
	if parts[0] == "" || !decimalDigits(parts[0]) {
		return "", errors.New("decimal integer part is invalid")
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
		if fraction == "" || len(fraction) > 8 || !decimalDigits(fraction) {
			return "", errors.New("decimal supports at most eight fractional digits")
		}
	}
	integer := strings.TrimLeft(parts[0], "0")
	if integer == "" {
		integer = "0"
	}
	if len(integer) > 12 {
		return "", errors.New("decimal exceeds numeric(20,8)")
	}
	fraction = strings.TrimRight(fraction, "0")
	if integer == "0" && fraction == "" {
		sign = ""
	}
	canonical := sign + integer
	if fraction != "" {
		canonical += "." + fraction
	}
	return Decimal(canonical), nil
}

func decimalDigits(value string) bool {
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func (d Decimal) String() string { return string(d) }

type SessionDate string

func ParseSessionDate(value string) (SessionDate, error) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return "", errors.New("session date must be a valid YYYY-MM-DD date")
	}
	return SessionDate(value), nil
}

func (d SessionDate) String() string { return string(d) }

func (d SessionDate) Time(location *time.Location) time.Time {
	if location == nil {
		location = time.UTC
	}
	parsed, err := time.ParseInLocation("2006-01-02", string(d), location)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

type DailyBar struct {
	InstrumentID    instruments.UUID
	SessionDate     SessionDate
	Open            Decimal
	High            Decimal
	Low             Decimal
	Close           Decimal
	AdjustedClose   *Decimal
	Volume          int64
	Currency        string
	Provider        string
	SourceHash      string
	ImportRunID     instruments.UUID
	FirstObservedAt time.Time
	LastObservedAt  time.Time
}

type CorporateActionType string

const (
	ActionSplit        CorporateActionType = "split"
	ActionReverseSplit CorporateActionType = "reverse_split"
	ActionDividend     CorporateActionType = "dividend"
	ActionSymbolChange CorporateActionType = "symbol_change"
	ActionDelisting    CorporateActionType = "delisting"
)

type CorporateAction struct {
	ID               instruments.UUID
	InstrumentID     instruments.UUID
	Provider         string
	ProviderActionID string
	Type             CorporateActionType
	ExDate           SessionDate
	EffectiveDate    *SessionDate
	Ratio            *Decimal
	Amount           *Decimal
	Currency         string
	OldSymbol        string
	NewSymbol        string
	SourceHash       string
	ImportRunID      instruments.UUID
	FirstObservedAt  time.Time
	LastObservedAt   time.Time
}

type ImportKind string

const (
	ImportUniverseSync ImportKind = "universe_sync"
	ImportBackfill     ImportKind = "backfill"
	ImportDailyUpdate  ImportKind = "daily_update"
	ImportRetry        ImportKind = "retry"
)

type ImportStatus string

const (
	ImportQueued    ImportStatus = "queued"
	ImportRunning   ImportStatus = "running"
	ImportSucceeded ImportStatus = "succeeded"
	ImportPartial   ImportStatus = "partial"
	ImportFailed    ImportStatus = "failed"
	ImportCancelled ImportStatus = "cancelled"
)

func (s ImportStatus) Terminal() bool {
	switch s {
	case ImportSucceeded, ImportPartial, ImportFailed, ImportCancelled:
		return true
	default:
		return false
	}
}

type ImportCounts struct {
	Processed int64
	Accepted  int64
	Rejected  int64
	Flagged   int64
}

func (c ImportCounts) Valid() bool {
	if c.Processed < 0 || c.Accepted < 0 || c.Rejected < 0 || c.Flagged < 0 {
		return false
	}
	return c.Accepted+c.Rejected <= c.Processed && c.Flagged <= c.Processed
}

type SafeError struct {
	Code    string
	Summary string
}

func SanitizeError(raw string, _ ...string) SafeError {
	lower := strings.ToLower(raw)
	result := SafeError{Code: "provider_error", Summary: "Market-data provider request failed."}
	switch {
	case strings.Contains(lower, "context canceled") || strings.Contains(lower, "cancelled"):
		result = SafeError{Code: "cancelled", Summary: "Market-data request was cancelled."}
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline exceeded"):
		result = SafeError{Code: "provider_timeout", Summary: "Market-data provider request timed out."}
	case strings.Contains(lower, "rate limit") || strings.Contains(lower, "status 429"):
		result = SafeError{Code: "provider_rate_limited", Summary: "Market-data provider rate limit was reached."}
	case strings.Contains(lower, "token") || strings.Contains(lower, "unauthorized") || strings.Contains(lower, "status 401"):
		result = SafeError{Code: "provider_authentication", Summary: "Market-data provider authentication failed."}
	}
	return result
}

type ImportRun struct {
	ID            instruments.UUID
	Kind          ImportKind
	Provider      string
	RequestedFrom *SessionDate
	RequestedTo   *SessionDate
	Status        ImportStatus
	ParentRunID   *instruments.UUID
	StartedAt     time.Time
	FinishedAt    *time.Time
	Counts        ImportCounts
	Error         *SafeError
	AppVersion    string
}

type ImportItem struct {
	RunID         instruments.UUID
	InstrumentID  instruments.UUID
	RequestedFrom SessionDate
	RequestedTo   SessionDate
	Status        ImportStatus
	Counts        ImportCounts
	Attempts      int
	StartedAt     *time.Time
	FinishedAt    *time.Time
	Error         *SafeError
}

type PriceBarRevision struct {
	DailyBar
	Revision     int
	SupersededBy instruments.UUID
	SupersededAt time.Time
}

type FindingSeverity string
type FindingDisposition string
type FindingStatus string

const (
	SeverityWarning           FindingSeverity    = "warning"
	SeverityError             FindingSeverity    = "error"
	DispositionFlagged        FindingDisposition = "flagged"
	DispositionRejected       FindingDisposition = "rejected"
	FindingOpen               FindingStatus      = "open"
	FindingResolved           FindingStatus      = "resolved"
	FindingAcceptedLimitation FindingStatus      = "accepted_limitation"
)

type QualityFinding struct {
	ID             instruments.UUID
	InstrumentID   instruments.UUID
	SessionDate    *SessionDate
	RunID          instruments.UUID
	Rule           string
	Severity       FindingSeverity
	Disposition    FindingDisposition
	Detail         string
	Status         FindingStatus
	CreatedAt      time.Time
	ResolvedAt     *time.Time
	ResolvingRunID *instruments.UUID
}
