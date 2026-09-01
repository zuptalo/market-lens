package features

import (
	"strconv"
	"strings"
)

// Places is the stored precision of every numeric feature value: numeric(24,12).
const Places = 12

// Round renders a computed float64 as the twelve-place decimal string the store holds,
// rounding half-to-even (research R-001). The rounding is applied to the shortest decimal
// that round-trips the float64 — the number the computation actually produced — never to its
// full binary expansion, so 1.5e-12 is a true tie and rounds to 0.000000000002.
//
// The shortest decimal is already a digit string, so the rounding is done on its digits: it
// is exact, and it runs at the speed of a copy rather than of rational arithmetic, which is
// what keeps an incremental pass inside its budget (SC-006).
func Round(value float64) string {
	shortest := strconv.FormatFloat(value, 'f', -1, 64)
	if strings.ContainsAny(shortest, "NI") {
		// Only NaN and the infinities reach here; the callers report them as absences
		// before storing anything, so a panic is the correct response to a caller that did not.
		panic("features: Round of a non-finite value " + shortest)
	}
	negative := strings.HasPrefix(shortest, "-")
	shortest = strings.TrimPrefix(shortest, "-")
	integer, fraction, _ := strings.Cut(shortest, ".")
	var digits []byte
	if len(fraction) <= Places {
		digits = make([]byte, 0, len(integer)+Places)
		digits = append(digits, integer...)
		digits = append(digits, fraction...)
		for range Places - len(fraction) {
			digits = append(digits, '0')
		}
	} else {
		digits = make([]byte, 0, len(integer)+Places+1)
		digits = append(digits, integer...)
		digits = append(digits, fraction[:Places]...)
		tail := fraction[Places:]
		up := tail[0] > '5'
		if tail[0] == '5' {
			// Exactly half only if nothing follows the 5; then the last kept digit decides.
			up = strings.Trim(tail[1:], "0") != "" || digits[len(digits)-1]%2 == 1
		}
		if up {
			i := len(digits) - 1
			for ; i >= 0 && digits[i] == '9'; i-- {
				digits[i] = '0'
			}
			if i < 0 {
				digits = append([]byte{'1'}, digits...)
			} else {
				digits[i]++
			}
		}
	}
	split := len(digits) - Places
	result := string(digits[:split]) + "." + string(digits[split:])
	if negative && strings.Trim(result, "0.") != "" {
		return "-" + result
	}
	return result
}
