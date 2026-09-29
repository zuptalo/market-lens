package marketdata

import (
	"errors"
	"testing"
)

func TestTheMappingAuditAcceptsOnlyEvidence(t *testing.T) {
	stored := Decimal("45.38")
	close := func(value string) *Decimal { d := Decimal(value); return &d }
	base := FallbackMappingEvidence{Ticker: "TELIA", Currency: "SEK", Symbol: "TELIA.ST",
		Session: "2026-09-28", StoredClose: &stored, ObservedCurrency: "SEK", ObservedClose: close("45.38")}

	for name, tc := range map[string]struct {
		change func(*FallbackMappingEvidence)
		want   FallbackMappingState
	}{
		"exact":                  {func(*FallbackMappingEvidence) {}, MappingVerified},
		"within half a percent":  {func(e *FallbackMappingEvidence) { e.ObservedClose = close("45.6") }, MappingVerified},
		"beyond half a percent":  {func(e *FallbackMappingEvidence) { e.ObservedClose = close("45.7") }, MappingMismatched},
		"another currency":       {func(e *FallbackMappingEvidence) { e.ObservedCurrency = "EUR" }, MappingMismatched},
		"no symbol":              {func(e *FallbackMappingEvidence) { e.Symbol = "" }, MappingUnmapped},
		"no primary price":       {func(e *FallbackMappingEvidence) { e.StoredClose = nil }, MappingUnverified},
		"the source had nothing": {func(e *FallbackMappingEvidence) { e.ObservedClose = nil }, MappingUnverified},
		"the source failed": {func(e *FallbackMappingEvidence) {
			e.ObservedClose, e.ObservedErr = nil, errors.New("unavailable")
		}, MappingUnverified},
	} {
		evidence := base
		tc.change(&evidence)
		if got := ClassifyFallbackMapping(evidence); got != tc.want {
			t.Errorf("%s: %s, want %s", name, got, tc.want)
		}
	}
}
