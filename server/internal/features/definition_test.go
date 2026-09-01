package features_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"market-lens/server/internal/features"
)

func definitionsNamed(names ...string) []features.Definition {
	out := make([]features.Definition, 0, len(names))
	for _, name := range names {
		out = append(out, features.Definition{ID: features.UUID("00000000-0013-4000-8000-0000000000" + name[:2]),
			Name: name, Version: 1, PriceBasis: features.PriceBasisAdjusted, Parameters: map[string]any{},
			PublishedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)})
	}
	return out
}

var v1Names = []string{
	"return_1", "return_5", "return_20", "return_60", "return_90", "return_250", "log_return_1",
	"sma_20", "sma_50", "sma_200", "trend_50_200", "momentum_20", "relative_strength_20",
	"relative_strength_90", "volatility_20", "atr_14", "rsi_14", "macd_12_26", "macd_signal_9",
	"macd_histogram", "drawdown_250", "volume_sma_20", "volume_ratio_20", "regime", "composite_return_1",
}

func v1Definitions(t *testing.T) []features.Definition {
	t.Helper()
	windows := map[string]int{
		"return_1": 2, "return_5": 6, "return_20": 21, "return_60": 61, "return_90": 91, "return_250": 251,
		"log_return_1": 2, "sma_20": 20, "sma_50": 50, "sma_200": 200, "trend_50_200": 200, "momentum_20": 20,
		"relative_strength_20": 21, "relative_strength_90": 91, "volatility_20": 21, "atr_14": 15, "rsi_14": 140,
		"macd_12_26": 130, "macd_signal_9": 130, "macd_histogram": 130, "drawdown_250": 250,
		"volume_sma_20": 20, "volume_ratio_20": 20, "regime": 250, "composite_return_1": 2,
	}
	definitions := definitionsNamed(v1Names...)
	for index := range definitions {
		window := windows[definitions[index].Name]
		definitions[index].WindowSessions = &window
		switch definitions[index].Name {
		case "rsi_14":
			definitions[index].Parameters = map[string]any{"period": 14.0}
		case "macd_12_26", "macd_signal_9", "macd_histogram":
			definitions[index].Parameters = map[string]any{"fast": 12.0, "slow": 26.0, "signal": 9.0}
		case "regime":
			definitions[index].Parameters = map[string]any{
				"volatile":      map[string]any{"volatility_20_at_least": 0.40},
				"trending_up":   map[string]any{"trend_50_200_above": 0.05, "drawdown_250_above": -0.10},
				"trending_down": map[string]any{"trend_50_200_below": -0.05},
			}
		case "composite_return_1":
			definitions[index].Parameters = map[string]any{"min_contributors": 10.0}
		}
	}
	return definitions
}

func TestARegistryRefusesAnActiveDefinitionItCannotCompute(t *testing.T) {
	_, err := features.NewRegistry(append(v1Definitions(t), definitionsNamed("skew_60")...))
	if err == nil || !strings.Contains(err.Error(), "skew_60") {
		t.Fatalf("expected an error naming skew_60, got %v", err)
	}
	// A superseded row only has to stay readable; nothing computes it.
	superseded := definitionsNamed("skew_60")
	at := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	superseded[0].SupersededAt = &at
	if _, err := features.NewRegistry(append(v1Definitions(t), superseded...)); err != nil {
		t.Fatalf("a superseded definition without a compute function must be tolerated: %v", err)
	}
}

func TestARegistryKnowsItsActiveDefinitionsAndTheLongestWindow(t *testing.T) {
	registry, err := features.NewRegistry(v1Definitions(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := registry.WMax(); got != 251 { // return_250 reads 251 sessions
		t.Errorf("WMax = %d, expected 251", got)
	}
	active := registry.Active()
	if len(active) != 24 {
		t.Fatalf("%d active instrument definitions, expected 24 (the composite is not one)", len(active))
	}
	for index := 1; index < len(active); index++ {
		if active[index-1].Name >= active[index].Name {
			t.Fatalf("active definitions are not in name order: %s before %s", active[index-1].Name, active[index].Name)
		}
	}
	composite, ok := registry.Composite()
	if !ok || composite.Name != features.CompositeDefinitionName || registry.MinContributors() != 10 {
		t.Errorf("composite = %+v (%v), min contributors %d", composite, ok, registry.MinContributors())
	}
	for name, want := range map[string]bool{"sma_20": true, "atr_14": true, "return_1": false, "volume_sma_20": false} {
		if got := registry.Currency(name); got != want {
			t.Errorf("Currency(%s) = %v, expected %v", name, got, want)
		}
	}
	for name, want := range map[string]bool{"relative_strength_20": true, "relative_strength_90": true, "return_20": false} {
		if got := registry.UsesComposite(name); got != want {
			t.Errorf("UsesComposite(%s) = %v, expected %v", name, got, want)
		}
	}
}

func TestARegistryComputesADefinitionFromItsWindow(t *testing.T) {
	registry, err := features.NewRegistry(v1Definitions(t))
	if err != nil {
		t.Fatal(err)
	}
	golden := loadGoldenA(t)
	bars := seriesA()
	latest := goldenAt(t, golden, 319)
	byName := map[string]features.Definition{}
	for _, definition := range registry.Active() {
		byName[definition.Name] = definition
	}
	for _, name := range []string{"return_20", "rsi_14", "macd_signal_9", "atr_14", "volume_ratio_20"} {
		definition := byName[name]
		result := registry.Compute(definition, features.Input{Bars: window(bars, 319, *definition.WindowSessions)})
		if result.Reason != "" || result.Value == nil || features.Round(*result.Value) != *latest.Features[name].Value {
			t.Errorf("%s = %+v, expected %s", name, result, *latest.Features[name].Value)
		}
	}
	regime := registry.Compute(byName["regime"], features.Input{Bars: window(bars, 319, 250)})
	if regime.Reason != "" || regime.Label != *latest.Features["regime"].Label {
		t.Errorf("regime = %+v, expected %s", regime, *latest.Features["regime"].Label)
	}
	series := universeSeries()
	strength := registry.Compute(byName["relative_strength_20"], features.Input{
		Bars: window(bars, 319, 21), Composites: compositeSeries(series, 0, 20),
	})
	if strength.Reason != "" || strength.Value == nil || features.Round(*strength.Value) != *latest.Features["relative_strength_20"].Value {
		t.Errorf("relative_strength_20 = %+v, expected %s", strength, *latest.Features["relative_strength_20"].Value)
	}
	undefined := registry.Compute(byName["relative_strength_20"], features.Input{
		Bars: window(bars, 319, 21), Composites: append(compositeSeries(series, 1, 19), features.CompositeValue{}),
	})
	if undefined.Reason != features.AbsenceCompositeUndefined {
		t.Errorf("relative strength over an undefined composite session = %+v", undefined)
	}
}

// Every definition the migration publishes must be exercised somewhere in this package's
// tests by name. It is a guard against the one failure mode a registry of formulas invites:
// a definition added to the seed and to the compute table, shipped, and never checked at the
// boundaries of its own window. Naming the definition in a test is the cheap half of the
// guarantee; the golden and boundary tests are the other half.
func TestEveryPublishedDefinitionHasAUnitTestAtItsBoundaries(t *testing.T) {
	published := publishedDefinitionNames(t)
	if len(published) < 20 {
		t.Fatalf("read %d published definitions from the migration, expected the full table", len(published))
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	tested := map[string]string{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, "_test.go") || name == "definition_test.go" || strings.Contains(name, "fixture") {
			continue
		}
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, definition := range published {
			if strings.Contains(string(body), definition) && tested[definition] == "" {
				tested[definition] = name
			}
		}
	}
	for _, definition := range published {
		if tested[definition] == "" {
			t.Errorf("no test in this package names %q: a published definition with no test of its own", definition)
		}
	}
}

// publishedDefinitionNames reads the names out of the seed migration, which is what
// "published" means: a definition exists because a migration inserted it.
func publishedDefinitionNames(t *testing.T) []string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "db", "migrations", "0017_feature_definitions.sql"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	seen := map[string]bool{}
	for _, match := range regexp.MustCompile(`(?m)^\s*\('[0-9a-f-]{36}',\s*'([a-z0-9_]+)'`).FindAllStringSubmatch(string(body), -1) {
		if !seen[match[1]] {
			seen[match[1]] = true
			names = append(names, match[1])
		}
	}
	return names
}
