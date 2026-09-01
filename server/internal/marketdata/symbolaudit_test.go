package marketdata

import "testing"

// Index compositions change and companies rename. When they do, a stored provider symbol
// stops existing and the instrument silently imports nothing — which is how Metso Oyj and
// Sydbank sat at zero bars while ninety-eight others filled up.
//
// The audit answers the question that actually helps: not "did this fail" but "what should
// the symbol be", by matching on the ISIN, which does not change when a ticker does.
func TestAuditReportsASymbolThatNoLongerExistsAndNamesItsReplacement(t *testing.T) {
	universe := []UniverseEntry{
		{Ticker: "MOCORP", ISIN: "FI0009014575", Name: "Metso Oyj", MIC: "XHEL", ProviderSymbol: "MOCORP.HE"},
		{Ticker: "NOKIA", ISIN: "FI0009000681", Name: "Nokia Oyj", MIC: "XHEL", ProviderSymbol: "NOKIA.HE"},
	}
	catalog := map[string][]CatalogEntry{
		"XHEL": {
			{ProviderSymbol: "METSO.HE", ISIN: "FI0009014575", Ticker: "METSO", Name: "Metso Oyj"},
			{ProviderSymbol: "NOKIA.HE", ISIN: "FI0009000681", Ticker: "NOKIA", Name: "Nokia Oyj"},
		},
	}

	findings := AuditProviderSymbols(universe, catalog)
	byTicker := map[string]SymbolFinding{}
	for _, finding := range findings {
		byTicker[finding.Entry.Ticker] = finding
	}

	renamed := byTicker["MOCORP"]
	if renamed.State != SymbolRenamed {
		t.Errorf("a renamed instrument was reported as %q", renamed.State)
	}
	if renamed.Suggested != "METSO.HE" {
		t.Errorf("the replacement symbol was %q, expected METSO.HE", renamed.Suggested)
	}

	if byTicker["NOKIA"].State != SymbolOK {
		t.Errorf("a correct symbol was reported as %q", byTicker["NOKIA"].State)
	}
}

// An instrument the provider does not carry at all is a different fact from a renamed one,
// and must not be reported as though a replacement exists.
func TestAuditDistinguishesAnInstrumentTheProviderDoesNotCarry(t *testing.T) {
	universe := []UniverseEntry{
		{Ticker: "GONE", ISIN: "SE0000000999", Name: "Delisted AB", MIC: "XSTO", ProviderSymbol: "GONE.ST"},
	}
	catalog := map[string][]CatalogEntry{
		"XSTO": {{ProviderSymbol: "ERIC-B.ST", ISIN: "SE0000108656", Ticker: "ERIC-B", Name: "Ericsson"}},
	}

	findings := AuditProviderSymbols(universe, catalog)
	if len(findings) != 1 {
		t.Fatalf("expected one finding, got %d", len(findings))
	}
	if findings[0].State != SymbolAbsent {
		t.Errorf("an instrument absent from the catalog was reported as %q", findings[0].State)
	}
	if findings[0].Suggested != "" {
		t.Errorf("a replacement was suggested for an instrument with no ISIN match: %q", findings[0].Suggested)
	}
}

// A symbol that still exists but under a different ISIN is not a rename — it is a mismatch
// worth flagging, because importing it would attach one company's prices to another's record.
func TestAuditFlagsASymbolThatResolvesToADifferentCompany(t *testing.T) {
	universe := []UniverseEntry{
		{Ticker: "AAA", ISIN: "SE0000000001", Name: "Alpha AB", MIC: "XSTO", ProviderSymbol: "AAA.ST"},
	}
	catalog := map[string][]CatalogEntry{
		"XSTO": {{ProviderSymbol: "AAA.ST", ISIN: "SE0000000002", Ticker: "AAA", Name: "Beta AB"}},
	}

	findings := AuditProviderSymbols(universe, catalog)
	if findings[0].State != SymbolMismatched {
		t.Errorf("a symbol pointing at another company was reported as %q", findings[0].State)
	}
	// Deciding a mismatch means comparing the two identifiers, so the provider's own ISIN has
	// to be reported. Without it the finding says only "these disagree" and leaves the reader
	// to guess which side is wrong — which is the guess this whole tool exists to avoid.
	if findings[0].CatalogISIN != "SE0000000002" {
		t.Errorf("the provider's ISIN was %q, expected it to be reported for a mismatch",
			findings[0].CatalogISIN)
	}
}

func TestAuditReportsAnExchangeItCouldNotRead(t *testing.T) {
	universe := []UniverseEntry{
		{Ticker: "AAA", ISIN: "SE0000000001", Name: "Alpha AB", MIC: "XSTO", ProviderSymbol: "AAA.ST"},
	}
	// No catalog for XSTO: the audit must say so rather than declare every symbol missing.
	findings := AuditProviderSymbols(universe, map[string][]CatalogEntry{})
	if findings[0].State != SymbolUnchecked {
		t.Errorf("an unreadable exchange was reported as %q", findings[0].State)
	}
}

func TestAuditIsOrderedAndCountsWhatItChecked(t *testing.T) {
	universe := []UniverseEntry{
		{Ticker: "ZZZ", ISIN: "SE0000000003", MIC: "XSTO", ProviderSymbol: "ZZZ.ST"},
		{Ticker: "AAA", ISIN: "SE0000000001", MIC: "XSTO", ProviderSymbol: "AAA.ST"},
	}
	catalog := map[string][]CatalogEntry{"XSTO": {
		{ProviderSymbol: "AAA.ST", ISIN: "SE0000000001", Ticker: "AAA"},
		{ProviderSymbol: "ZZZ.ST", ISIN: "SE0000000003", Ticker: "ZZZ"},
	}}
	findings := AuditProviderSymbols(universe, catalog)
	if len(findings) != 2 || findings[0].Entry.Ticker != "AAA" || findings[1].Entry.Ticker != "ZZZ" {
		t.Fatalf("findings are not ordered by ticker: %#v", findings)
	}
}

// "Absent" is two different situations and only one of them needs acting on.
//
// An instrument missing from the catalog that also stores nothing is broken. One missing from
// the catalog that imports its full history is a disagreement between the provider's own
// endpoints — the symbol list omits it while the price endpoint serves it — and changing our
// data would break the import that currently works.
func TestAuditSaysWhetherAnAbsentInstrumentIsActuallyImporting(t *testing.T) {
	universe := []UniverseEntry{
		{Ticker: "WORKS", ISIN: "FI4000312251", Name: "Kojamo Oyj", MIC: "XHEL",
			ProviderSymbol: "WORKS.HE", StoredBars: 2500},
		{Ticker: "BROKEN", ISIN: "FI0000000001", Name: "Nothing Oyj", MIC: "XHEL",
			ProviderSymbol: "BROKEN.HE", StoredBars: 0},
	}
	catalog := map[string][]CatalogEntry{"XHEL": {
		{ProviderSymbol: "NOKIA.HE", ISIN: "FI0009000681", Ticker: "NOKIA", Name: "Nokia Oyj"},
	}}

	byTicker := map[string]SymbolFinding{}
	for _, finding := range AuditProviderSymbols(universe, catalog) {
		byTicker[finding.Entry.Ticker] = finding
	}

	if byTicker["WORKS"].State != SymbolUncatalogued {
		t.Errorf("an absent instrument that imports was reported as %q", byTicker["WORKS"].State)
	}
	if byTicker["BROKEN"].State != SymbolAbsent {
		t.Errorf("an absent instrument that imports nothing was reported as %q", byTicker["BROKEN"].State)
	}
}

// When a symbol is absent, the catalog is still searched by name, because a company that has
// been renamed or re-tickered is usually still there under a name a reader will recognise.
func TestAuditOffersANameMatchWhenNeitherSymbolNorISINIsFound(t *testing.T) {
	universe := []UniverseEntry{
		{Ticker: "OLD", ISIN: "FI0000000009", Name: "Nokia Oyj", MIC: "XHEL", ProviderSymbol: "OLD.HE"},
	}
	catalog := map[string][]CatalogEntry{"XHEL": {
		{ProviderSymbol: "NOKIA.HE", ISIN: "FI0009000681", Ticker: "NOKIA", Name: "Nokia Oyj"},
	}}

	finding := AuditProviderSymbols(universe, catalog)[0]
	if finding.Suggested != "NOKIA.HE" {
		t.Errorf("no name match was offered: %#v", finding)
	}
	// A name match is weaker evidence than an ISIN match and must not be presented as though
	// it were the same thing.
	if finding.MatchedOn != MatchedOnName {
		t.Errorf("the match was reported as %q, expected it to say it came from the name", finding.MatchedOn)
	}
}

func TestAuditSaysAnISINMatchCameFromTheISIN(t *testing.T) {
	universe := []UniverseEntry{
		{Ticker: "MOCORP", ISIN: "FI0009014575", Name: "Metso Oyj", MIC: "XHEL", ProviderSymbol: "MOCORP.HE"},
	}
	catalog := map[string][]CatalogEntry{"XHEL": {
		{ProviderSymbol: "METSO.HE", ISIN: "FI0009014575", Ticker: "METSO", Name: "Metso Oyj"},
	}}
	finding := AuditProviderSymbols(universe, catalog)[0]
	if finding.State != SymbolRenamed || finding.MatchedOn != MatchedOnISIN {
		t.Errorf("finding = %#v", finding)
	}
}
