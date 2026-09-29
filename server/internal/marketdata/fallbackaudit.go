package marketdata

import (
	"context"
	"fmt"
	"math/big"
	"strings"
)

// FallbackMappingState is what the mapping audit concluded about one instrument (FR-011).
type FallbackMappingState string

const (
	// MappingVerified: the fallback reports the instrument's currency and its stored close.
	MappingVerified FallbackMappingState = "verified"
	// MappingUnmapped: no fallback symbol is stored, so the instrument is never covered.
	MappingUnmapped FallbackMappingState = "unmapped"
	// MappingMismatched: the fallback answered, and what it said is not this instrument.
	MappingMismatched FallbackMappingState = "mismatched"
	// MappingUnverified: nothing to compare — no primary price, or no answer from the fallback.
	MappingUnverified FallbackMappingState = "unverified"
)

// mappingTolerance is how far the fallback's close may sit from the stored one. The verification
// that seeded the mappings found every instrument exact; half a percent leaves room for a source
// that rounds differently and none for another company's price.
var mappingTolerance = big.NewRat(5, 1000)

// FallbackMappingEvidence is what the audit compares for one instrument: the stored primary close
// on its newest primary session, and what the fallback reports for that same session.
type FallbackMappingEvidence struct {
	Ticker, ISIN, MIC, Currency string
	Symbol                      string
	Session                     SessionDate
	StoredClose                 *Decimal
	ObservedCurrency            string
	ObservedClose               *Decimal
	ObservedErr                 error
}

// ClassifyFallbackMapping decides from evidence alone; a symbol that merely looks right is never
// enough, which is the lesson of the symbol-drift history.
func ClassifyFallbackMapping(evidence FallbackMappingEvidence) FallbackMappingState {
	if strings.TrimSpace(evidence.Symbol) == "" {
		return MappingUnmapped
	}
	if evidence.StoredClose == nil || evidence.ObservedErr != nil || evidence.ObservedClose == nil {
		return MappingUnverified
	}
	if !strings.EqualFold(evidence.ObservedCurrency, evidence.Currency) {
		return MappingMismatched
	}
	stored, okStored := new(big.Rat).SetString(evidence.StoredClose.String())
	observed, okObserved := new(big.Rat).SetString(evidence.ObservedClose.String())
	if !okStored || !okObserved || stored.Sign() <= 0 {
		return MappingUnverified
	}
	difference := new(big.Rat).Sub(observed, stored)
	difference.Abs(difference)
	if difference.Quo(difference, stored).Cmp(mappingTolerance) > 0 {
		return MappingMismatched
	}
	return MappingVerified
}

// FallbackAuditEntries is the stored half of the evidence for every active instrument in a
// universe: its fallback symbol, if any, and its newest primary-sourced close.
func (r *Repository) FallbackAuditEntries(ctx context.Context, universe string) ([]FallbackMappingEvidence, error) {
	rows, err := r.pool.Query(ctx, `SELECT i.ticker, i.isin, e.mic, i.currency, coalesce(p.provider_symbol, ''),
			latest.session_date::text, latest.close::text
		FROM research_universes u
		JOIN universe_memberships m ON m.universe_id = u.id AND m.included_to IS NULL
		JOIN instruments i ON i.id = m.instrument_id AND i.active
		JOIN exchanges e ON e.id = i.exchange_id
		LEFT JOIN provider_instruments p ON p.instrument_id = i.id AND p.provider = $2 AND p.active
		LEFT JOIN LATERAL (SELECT b.session_date, b.close FROM daily_price_bars b
			WHERE b.instrument_id = i.id AND b.provider <> $2
			ORDER BY b.session_date DESC LIMIT 1) latest ON true
		WHERE u.code = $1
		ORDER BY e.mic, i.ticker`, universe, FallbackProvider)
	if err != nil {
		return nil, fmt.Errorf("read the audit's stored evidence: %w", err)
	}
	defer rows.Close()
	var entries []FallbackMappingEvidence
	for rows.Next() {
		var entry FallbackMappingEvidence
		var session, closeValue *string
		if err := rows.Scan(&entry.Ticker, &entry.ISIN, &entry.MIC, &entry.Currency, &entry.Symbol,
			&session, &closeValue); err != nil {
			return nil, err
		}
		if session != nil && closeValue != nil {
			entry.Session = SessionDate(*session)
			stored, err := ParseDecimal(*closeValue)
			if err != nil {
				return nil, err
			}
			entry.StoredClose = &stored
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}
