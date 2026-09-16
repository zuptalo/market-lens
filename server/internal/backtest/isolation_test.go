package backtest

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// TestABacktestNeverCallsTheProvider is the property SC-001 rests on, asserted where it can
// actually be enforced.
//
// A simulation that fetched anything would be reproducible only for as long as the provider kept
// answering the same way, and a stored result would stop being checkable the moment a symbol was
// restated. Rather than mock a provider and assert it was not called — which proves only that one
// code path did not call it today — this asserts the package cannot reach one at all: no provider
// client, no HTTP, no network. A future change that added the import fails here, with the reason.
func TestABacktestNeverCallsTheProvider(t *testing.T) {
	forbidden := []string{"net/http", "net/url", "market-lens/server/internal/marketdata"}

	set := token.NewFileSet()
	packages, err := parser.ParseDir(set, ".", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("read the package: %v", err)
	}
	for name, parsed := range packages {
		if strings.HasSuffix(name, "_test") {
			continue
		}
		for path, file := range parsed.Files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			for _, imported := range file.Imports {
				value := strings.Trim(imported.Path.Value, `"`)
				for _, banned := range forbidden {
					if value == banned || strings.HasPrefix(value, banned+"/") {
						t.Errorf("%s imports %s; a backtest reads stored data only", path, value)
					}
				}
			}
		}
	}
}
