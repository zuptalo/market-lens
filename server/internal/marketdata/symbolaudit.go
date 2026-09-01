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
// the symbol be" — by matching on the ISIN, which does not change when a ticker does, and
// falling back to the company name when even that finds nothing.
//
// It also refuses to call an instrument broken merely because the provider's symbol list omits
// it. A symbol that is importing its full history works, whatever the catalog says about it,
// and "correcting" one of those turns a working instrument into a broken one.

// SymbolState is what the audit concluded about one stored provider symbol.
type SymbolState string

const (
	// SymbolOK: the stored symbol exists and identifies the same instrument.
	SymbolOK SymbolState = "ok"
	// SymbolRenamed: the stored symbol is gone, but the ISIN identifies one that exists.
	SymbolRenamed SymbolState = "renamed"
	// SymbolAbsent: neither the symbol nor the ISIN appears in the provider's catalog, and
	// nothing is stored for it either. This one is broken.
	SymbolAbsent SymbolState = "absent"
	// SymbolUncatalogued: absent from the catalog, yet importing its history anyway. That is a
	// disagreement between the provider's own endpoints — its symbol list omits what its price
	// endpoint serves — and not something to correct on our side, because the symbol works.
	SymbolUncatalogued SymbolState = "uncatalogued"
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
	// StoredBars separates an instrument that is broken from one that merely disagrees with
	// the provider's catalog while importing perfectly well.
	StoredBars int64
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
	// Suggested is the provider symbol the stored instrument appears to be, when the catalog
	// holds one. Empty when nothing plausible was found. MatchedOn says what it rests on.
	Suggested string
	// CatalogName is the name the provider has for the symbol it matched, so a reader can see
	// that a replacement really is the same company before acting on it.
	CatalogName string
	// CatalogISIN is the provider's identifier for the symbol it matched. It is what a
	// mismatch is actually about: without both identifiers side by side, the finding says only
	// that they disagree and leaves the reader to guess which is wrong.
	CatalogISIN string
	// MatchedOn records what the suggestion rests on. An ISIN match is near-certain; a name
	// match is a lead worth checking. Presenting them identically would invite acting on the
	// weaker one as though it were the stronger.
	MatchedOn MatchBasis
}

// MatchBasis is the evidence a suggested replacement rests on.
type MatchBasis string

const (
	MatchedOnISIN MatchBasis = "isin"
	MatchedOnName MatchBasis = "name"
)

// normalizedName makes company names comparable without pretending to be clever about it:
// case and surrounding space only. Anything more would start matching companies that merely
// look alike, which is worse than offering no lead at all.
func normalizedName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
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
		byName := map[string]CatalogEntry{}
		for _, candidate := range entries {
			bySymbol[strings.ToUpper(candidate.ProviderSymbol)] = candidate
			if isin := strings.ToUpper(strings.TrimSpace(candidate.ISIN)); isin != "" {
				byISIN[isin] = candidate
			}
			if name := normalizedName(candidate.Name); name != "" {
				byName[name] = candidate
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
				Entry: entry, State: SymbolMismatched,
				CatalogName: matchedSymbol.Name, CatalogISIN: matchedSymbol.ISIN,
			})
		case isinExists:
			findings = append(findings, SymbolFinding{
				Entry: entry, State: SymbolRenamed,
				Suggested: matchedISIN.ProviderSymbol, CatalogName: matchedISIN.Name,
				CatalogISIN: matchedISIN.ISIN, MatchedOn: MatchedOnISIN,
			})
		default:
			// Nothing matched by identifier. An instrument that still imports its history is
			// not broken — the provider's symbol list simply omits what its price endpoint
			// serves — so it is reported as uncatalogued rather than as a fault to correct.
			state := SymbolAbsent
			if entry.StoredBars > 0 {
				state = SymbolUncatalogued
			}
			finding := SymbolFinding{Entry: entry, State: state}
			// The name is a last resort and a weaker one, so the match says where it came from.
			if named, found := byName[normalizedName(entry.Name)]; found {
				finding.Suggested = named.ProviderSymbol
				finding.CatalogName = named.Name
				finding.CatalogISIN = named.ISIN
				finding.MatchedOn = MatchedOnName
			}
			findings = append(findings, finding)
		}
	}

	sort.SliceStable(findings, func(i, j int) bool {
		return findings[i].Entry.Ticker < findings[j].Entry.Ticker
	})
	return findings
}
