package marketdata

import (
	"sort"
	"strings"
)

// Auditing the provider symbols a universe is stored with.
//
// A curated universe drifts: index compositions change, companies rename, and a ticker that
// was correct when it was seeded stops existing. When that happens the instrument simply
// imports nothing, and the failure is easy to read as a provider problem rather than as a
// stale identifier of ours.
//
// The audit answers the question that actually helps — not "did this fail" but "what should
// the symbol be" — by matching on the ISIN, which does not change when a ticker does.

// SymbolState is what the audit concluded about one stored provider symbol.
type SymbolState string

const (
	// SymbolOK: the stored symbol exists and identifies the same instrument.
	SymbolOK SymbolState = "ok"
	// SymbolRenamed: the stored symbol is gone, but the ISIN identifies one that exists.
	SymbolRenamed SymbolState = "renamed"
	// SymbolAbsent: neither the symbol nor the ISIN appears in the provider's catalog.
	SymbolAbsent SymbolState = "absent"
	// SymbolMismatched: the symbol exists but carries a different ISIN, so importing it would
	// attach one company's prices to another company's record.
	SymbolMismatched SymbolState = "mismatched"
	// SymbolUnchecked: the provider's catalog for that exchange could not be read, so nothing
	// is claimed either way.
	SymbolUnchecked SymbolState = "unchecked"
)

// UniverseEntry is one instrument as this installation has it stored.
type UniverseEntry struct {
	Ticker         string
	ISIN           string
	Name           string
	MIC            string
	ProviderSymbol string
}

// CatalogEntry is one instrument as the provider lists it.
type CatalogEntry struct {
	ProviderSymbol string
	ISIN           string
	Ticker         string
	Name           string
	Currency       string
}

// SymbolFinding is the audit's conclusion about one stored instrument.
type SymbolFinding struct {
	Entry UniverseEntry
	State SymbolState
	// Suggested is the provider symbol the ISIN maps to, when one exists and differs from
	// what is stored. Empty otherwise — a suggestion is only ever made from an ISIN match.
	Suggested string
	// CatalogName is the name the provider has for the suggested symbol, so a reader can see
	// that the replacement really is the same company before acting on it.
	CatalogName string
}

// AuditProviderSymbols compares stored provider symbols against the provider's own catalog.
//
// It is a pure function over data someone else fetched, so it can be tested exhaustively
// without a network or a database, and so the fetching stays where the credentials are.
func AuditProviderSymbols(universe []UniverseEntry, catalog map[string][]CatalogEntry) []SymbolFinding {
	findings := make([]SymbolFinding, 0, len(universe))

	for _, entry := range universe {
		entries, known := catalog[strings.ToUpper(strings.TrimSpace(entry.MIC))]
		if !known {
			findings = append(findings, SymbolFinding{Entry: entry, State: SymbolUnchecked})
			continue
		}

		bySymbol := map[string]CatalogEntry{}
		byISIN := map[string]CatalogEntry{}
		for _, candidate := range entries {
			bySymbol[strings.ToUpper(candidate.ProviderSymbol)] = candidate
			if isin := strings.ToUpper(strings.TrimSpace(candidate.ISIN)); isin != "" {
				byISIN[isin] = candidate
			}
		}

		storedSymbol := strings.ToUpper(strings.TrimSpace(entry.ProviderSymbol))
		storedISIN := strings.ToUpper(strings.TrimSpace(entry.ISIN))
		matchedSymbol, symbolExists := bySymbol[storedSymbol]
		matchedISIN, isinExists := byISIN[storedISIN]

		switch {
		case symbolExists && strings.EqualFold(matchedSymbol.ISIN, entry.ISIN):
			findings = append(findings, SymbolFinding{Entry: entry, State: SymbolOK})
		case symbolExists:
			// The symbol resolves, but to something else. Naming the replacement here would
			// be unhelpful, because the interesting fact is the collision.
			findings = append(findings, SymbolFinding{
				Entry: entry, State: SymbolMismatched, CatalogName: matchedSymbol.Name,
			})
		case isinExists:
			findings = append(findings, SymbolFinding{
				Entry: entry, State: SymbolRenamed,
				Suggested: matchedISIN.ProviderSymbol, CatalogName: matchedISIN.Name,
			})
		default:
			findings = append(findings, SymbolFinding{Entry: entry, State: SymbolAbsent})
		}
	}

	sort.SliceStable(findings, func(i, j int) bool {
		return findings[i].Entry.Ticker < findings[j].Entry.Ticker
	})
	return findings
}
