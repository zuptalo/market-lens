package marketdata

import (
	"math/big"
	"regexp"
	"strings"
)

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

type ValidationIssue struct {
	SessionDate SessionDate
	Rule        string
	Severity    FindingSeverity
	Disposition FindingDisposition
	Detail      string
}

type ValidationResult struct {
	Bars     []ProviderBar
	Actions  []ProviderAction
	Issues   []ValidationIssue
	Rejected int
}

type ValidationOptions struct {
	ExpectedSessions map[SessionDate]struct{}
}

func ValidateDailyPage(page DailyPage, options ValidationOptions) (ValidationResult, error) {
	result := ValidationResult{
		Bars:    make([]ProviderBar, 0, len(page.Bars)),
		Actions: make([]ProviderAction, 0, len(page.Actions)),
	}
	for _, bar := range page.Bars {
		if len(options.ExpectedSessions) > 0 {
			if _, expected := options.ExpectedSessions[bar.SessionDate]; !expected {
				result.reject(bar.SessionDate, "provider_gap", "Provider returned a bar outside the expected exchange sessions.")
				continue
			}
		}
		if rule, detail := basicBarError(bar); rule != "" {
			result.reject(bar.SessionDate, rule, detail)
			continue
		}
		result.Bars = append(result.Bars, bar)
	}
	for _, action := range page.Actions {
		if !validAction(action) {
			result.reject(action.ExDate, "possible_corporate_action_discontinuity", "Provider returned an incomplete corporate action.")
			continue
		}
		result.Actions = append(result.Actions, action)
	}
	return result, nil
}

func (r *ValidationResult) reject(session SessionDate, rule, detail string) {
	r.Rejected++
	r.Issues = append(r.Issues, ValidationIssue{
		SessionDate: session,
		Rule:        rule,
		Severity:    SeverityError,
		Disposition: DispositionRejected,
		Detail:      detail,
	})
}

func basicBarError(bar ProviderBar) (string, string) {
	if bar.SessionDate == "" || strings.TrimSpace(bar.SourceHash) == "" {
		return "provider_gap", "Provider returned a daily record without required identity."
	}
	prices := []Decimal{bar.Open, bar.High, bar.Low, bar.Close}
	if bar.AdjustedClose != nil {
		prices = append(prices, *bar.AdjustedClose)
	}
	for _, price := range prices {
		if decimalSign(price) <= 0 {
			return "non_positive_price", "Provider returned a non-positive daily price."
		}
	}
	if decimalCompare(bar.Low, bar.Open) > 0 || decimalCompare(bar.Open, bar.High) > 0 ||
		decimalCompare(bar.Low, bar.Close) > 0 || decimalCompare(bar.Close, bar.High) > 0 {
		return "invalid_ohlc", "Provider returned inconsistent daily OHLC values."
	}
	if bar.Volume < 0 {
		return "negative_volume", "Provider returned negative daily volume."
	}
	return "", ""
}

func validAction(action ProviderAction) bool {
	if action.ProviderActionID == "" || action.ExDate == "" || strings.TrimSpace(action.SourceHash) == "" {
		return false
	}
	switch action.Type {
	case ActionSplit, ActionReverseSplit:
		return action.Ratio != nil && decimalSign(*action.Ratio) > 0
	case ActionDividend:
		return action.Amount != nil && decimalSign(*action.Amount) >= 0 && currencyPattern.MatchString(action.Currency)
	case ActionSymbolChange:
		return strings.TrimSpace(action.OldSymbol) != "" && strings.TrimSpace(action.NewSymbol) != ""
	case ActionDelisting:
		return true
	default:
		return false
	}
}

func decimalSign(value Decimal) int {
	ratio, ok := new(big.Rat).SetString(value.String())
	if !ok {
		return -1
	}
	return ratio.Sign()
}

func decimalCompare(left, right Decimal) int {
	leftRatio, leftOK := new(big.Rat).SetString(left.String())
	rightRatio, rightOK := new(big.Rat).SetString(right.String())
	if !leftOK || !rightOK {
		return 1
	}
	return leftRatio.Cmp(rightRatio)
}
