package features

import (
	"fmt"
	"sort"
	"strconv"
)

// Input is what one definition reads at one session: the window of bars on the definition's
// price basis, ascending and ending at the session, and — for definitions that read the
// composite — the composite at each session of the window after the first, ascending.
type Input struct {
	Bars       []Bar
	Composites []CompositeValue
}

// Result is one definition's outcome at one session: a value, a label, or a reason.
type Result struct {
	Value  *float64
	Label  string
	Reason AbsenceReason
}

func number(value float64) Result { return Result{Value: &value} }

func numberOr(value float64, reason AbsenceReason) Result {
	if reason != "" {
		return Result{Reason: reason}
	}
	return number(value)
}

type computeFunc func(definition Definition, in Input) Result

// spec is what the code knows about a named definition that the table does not: how to
// compute it, whether it is denominated in the instrument's currency, and whether it reads
// the composite series.
type spec struct {
	compute       computeFunc
	currency      bool
	usesComposite bool
}

// specs maps every computable definition name to its function. A name in the table with no
// entry here is a startup error; an entry here with no active row is never called.
var specs = map[string]spec{
	"return_1":     {compute: simpleReturn},
	"return_5":     {compute: simpleReturn},
	"return_20":    {compute: simpleReturn},
	"return_60":    {compute: simpleReturn},
	"return_90":    {compute: simpleReturn},
	"return_250":   {compute: simpleReturn},
	"log_return_1": {compute: func(_ Definition, in Input) Result { return numberOr(LogReturn(closes(in.Bars))) }},
	"sma_20":       {compute: simpleAverage, currency: true},
	"sma_50":       {compute: simpleAverage, currency: true},
	"sma_200":      {compute: simpleAverage, currency: true},
	"trend_50_200": {compute: func(d Definition, in Input) Result {
		fast, slow := intParameter(d, "fast_sessions", 50), intParameter(d, "slow_sessions", 200)
		return numberOr(Trend(SMA(closes(in.Bars[len(in.Bars)-fast:])), SMA(closes(in.Bars[len(in.Bars)-slow:]))))
	}},
	"momentum_20": {compute: func(_ Definition, in Input) Result {
		return numberOr(Momentum(in.Bars[len(in.Bars)-1].Close, SMA(closes(in.Bars))))
	}},
	"relative_strength_20": {compute: relativeStrength, usesComposite: true},
	"relative_strength_90": {compute: relativeStrength, usesComposite: true},
	"volatility_20":        {compute: func(_ Definition, in Input) Result { return number(Volatility(closes(in.Bars))) }},
	"atr_14":               {compute: func(_ Definition, in Input) Result { return number(ATR(in.Bars)) }, currency: true},
	"rsi_14": {compute: func(d Definition, in Input) Result {
		return number(RSI(closes(in.Bars), intParameter(d, "period", 14)))
	}},
	"macd_12_26": {compute: func(d Definition, in Input) Result {
		line, _, _ := macd(d, in)
		return number(line)
	}},
	"macd_signal_9": {compute: func(d Definition, in Input) Result {
		_, signal, _ := macd(d, in)
		return number(signal)
	}},
	"macd_histogram": {compute: func(d Definition, in Input) Result {
		_, _, histogram := macd(d, in)
		return number(histogram)
	}},
	"drawdown_250":  {compute: func(_ Definition, in Input) Result { return number(Drawdown(closes(in.Bars))) }},
	"volume_sma_20": {compute: func(_ Definition, in Input) Result { return number(VolumeSMA(in.Bars)) }},
	"volume_ratio_20": {compute: func(_ Definition, in Input) Result {
		return numberOr(VolumeRatio(in.Bars[len(in.Bars)-1].Volume, VolumeSMA(in.Bars)))
	}},
	"regime": {compute: regime},
}

func closes(bars []Bar) []float64 {
	out := make([]float64, len(bars))
	for i, bar := range bars {
		out[i] = bar.Close
	}
	return out
}

func simpleReturn(_ Definition, in Input) Result  { return numberOr(Return(closes(in.Bars))) }
func simpleAverage(_ Definition, in Input) Result { return number(SMA(closes(in.Bars))) }

func relativeStrength(_ Definition, in Input) Result {
	own, reason := Return(closes(in.Bars))
	if reason != "" {
		return Result{Reason: reason}
	}
	return numberOr(RelativeStrength(own, in.Composites))
}

func macd(d Definition, in Input) (float64, float64, float64) {
	return MACD(closes(in.Bars), intParameter(d, "fast", 12), intParameter(d, "slow", 26), intParameter(d, "signal", 9))
}

// regime reads its three inputs from its own 250-session window — the longest of the three,
// so every input's window is satisfied whenever its own is — as the twelve-place decimals
// the store would hold for them.
func regime(d Definition, in Input) Result {
	volatility := Round(Volatility(closes(in.Bars[len(in.Bars)-21:])))
	trend, reason := Trend(SMA(closes(in.Bars[len(in.Bars)-50:])), SMA(closes(in.Bars[len(in.Bars)-200:])))
	if reason != "" {
		return Result{Reason: reason}
	}
	trendText := Round(trend)
	drawdown := Round(Drawdown(closes(in.Bars)))
	label, reason := Regime(&volatility, &trendText, &drawdown, regimeThresholds(d))
	return Result{Label: label, Reason: reason}
}

func regimeThresholds(d Definition) RegimeThresholds {
	return RegimeThresholds{
		VolatileAtLeast:   decimalParameter(d, "volatile", "volatility_20_at_least", "0.40"),
		TrendingUpAbove:   decimalParameter(d, "trending_up", "trend_50_200_above", "0.05"),
		DrawdownAbove:     decimalParameter(d, "trending_up", "drawdown_250_above", "-0.10"),
		TrendingDownBelow: decimalParameter(d, "trending_down", "trend_50_200_below", "-0.05"),
	}
}

func intParameter(d Definition, key string, fallback int) int {
	if value, ok := d.Parameters[key].(float64); ok {
		return int(value)
	}
	return fallback
}

// decimalParameter renders a JSON number from the definition's parameters as the shortest
// decimal that round-trips it, which for a stated threshold like 0.40 is "0.4".
func decimalParameter(d Definition, group, key, fallback string) string {
	if values, ok := d.Parameters[group].(map[string]any); ok {
		if value, ok := values[key].(float64); ok {
			return strconv.FormatFloat(value, 'f', -1, 64)
		}
	}
	return fallback
}

// Registry is the set of published definitions joined to the code that computes them.
type Registry struct {
	active          []Definition
	composite       Definition
	hasComposite    bool
	minContributors int
	wMax            int
}

// NewRegistry validates that every active definition can be computed. A superseded row is
// kept readable and never computed, so it needs no function.
func NewRegistry(definitions []Definition) (*Registry, error) {
	r := &Registry{}
	for _, definition := range definitions {
		if definition.SupersededAt != nil {
			continue
		}
		if definition.Name == CompositeDefinitionName {
			r.composite, r.hasComposite = definition, true
			r.minContributors = intParameter(definition, "min_contributors", 10)
			continue
		}
		if _, ok := specs[definition.Name]; !ok {
			return nil, fmt.Errorf("features: definition %s v%d has no compute function", definition.Name, definition.Version)
		}
		if definition.WindowSessions == nil || *definition.WindowSessions <= 0 {
			return nil, fmt.Errorf("features: definition %s v%d has no window", definition.Name, definition.Version)
		}
		r.active = append(r.active, definition)
		r.wMax = max(r.wMax, *definition.WindowSessions)
	}
	sort.Slice(r.active, func(i, j int) bool { return r.active[i].Name < r.active[j].Name })
	return r, nil
}

// Active returns the computable instrument-level definitions in name order.
func (r *Registry) Active() []Definition { return r.active }

// Composite returns the active composite definition, if one is published.
func (r *Registry) Composite() (Definition, bool) { return r.composite, r.hasComposite }

// MinContributors is the composite's stated minimum (FR-008b).
func (r *Registry) MinContributors() int { return r.minContributors }

// Named returns the active definitions with a name: the per-instrument one, or the
// composite when the name is its.
func (r *Registry) Named(name string) []Definition {
	if r.hasComposite && name == r.composite.Name {
		return []Definition{r.composite}
	}
	var named []Definition
	for _, definition := range r.active {
		if definition.Name == name {
			named = append(named, definition)
		}
	}
	return named
}

// CompositeUsers returns the active definitions that read the composite series.
func (r *Registry) CompositeUsers() []Definition {
	var users []Definition
	for _, definition := range r.active {
		if specs[definition.Name].usesComposite {
			users = append(users, definition)
		}
	}
	return users
}

// WMax is the longest active window, in sessions: the recomputation reach (research R-004).
func (r *Registry) WMax() int { return r.wMax }

// Currency reports whether a definition is denominated in the instrument's currency.
func (r *Registry) Currency(name string) bool { return specs[name].currency }

// UsesComposite reports whether a definition reads the composite series.
// usesComposite reports whether a definition reads the universe composite, by name.
func usesComposite(name string) bool { return specs[name].usesComposite }

func (r *Registry) UsesComposite(name string) bool { return specs[name].usesComposite }

// Compute evaluates one definition over its satisfied window.
func (r *Registry) Compute(definition Definition, in Input) Result {
	return specs[definition.Name].compute(definition, in)
}
