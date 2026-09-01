package features

import (
	"fmt"
	"math/big"
)

// RegimeThresholds are the version-1 boundaries (research R-007), as decimal strings so the
// comparison is exact against the twelve-place values the store holds.
type RegimeThresholds struct {
	VolatileAtLeast   string
	TrendingUpAbove   string
	DrawdownAbove     string
	TrendingDownBelow string
}

// Regime labels are names, never numbers (FR-013).
const (
	RegimeVolatile     = "volatile"
	RegimeTrendingUp   = "trending_up"
	RegimeTrendingDown = "trending_down"
	RegimeRangeBound   = "range_bound"
)

// Regime classifies the stored volatility, trend and drawdown in precedence order: volatile
// when volatility >= VolatileAtLeast; else trending_up when trend > TrendingUpAbove and
// drawdown > DrawdownAbove; else trending_down when trend < TrendingDownBelow; else
// range_bound. The inputs are the rounded decimal strings the store holds, so the label is a
// function of what a reader can see. Any undefined input leaves the regime undefined.
func Regime(volatility, trend, drawdown *string, thresholds RegimeThresholds) (string, AbsenceReason) {
	if volatility == nil || trend == nil || drawdown == nil {
		return "", AbsenceInsufficientHistory
	}
	v, t, d := decimal(*volatility), decimal(*trend), decimal(*drawdown)
	switch {
	case v.Cmp(decimal(thresholds.VolatileAtLeast)) >= 0:
		return RegimeVolatile, ""
	case t.Cmp(decimal(thresholds.TrendingUpAbove)) > 0 && d.Cmp(decimal(thresholds.DrawdownAbove)) > 0:
		return RegimeTrendingUp, ""
	case t.Cmp(decimal(thresholds.TrendingDownBelow)) < 0:
		return RegimeTrendingDown, ""
	}
	return RegimeRangeBound, ""
}

func decimal(text string) *big.Rat {
	value, ok := new(big.Rat).SetString(text)
	if !ok {
		panic(fmt.Sprintf("features: %q is not a decimal", text))
	}
	return value
}
