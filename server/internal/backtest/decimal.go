package backtest

import (
	"errors"
	"math/big"
	"strings"
)

// places is the stored precision of every figure this package writes: numeric(24,12), the same
// precision the feature and strategy layers already use.
const places = 12

var (
	scale   = new(big.Int).Exp(big.NewInt(10), big.NewInt(places), nil)
	bigTwo  = big.NewInt(2)
	tenThou = mustDec("10000")
	decZero = dec{new(big.Int)}
	decOne  = mustDec("1")
)

// dec is an exact decimal held as an integer scaled by 10^12.
//
// Every operation rounds to that precision immediately, and the rounded value is what is carried
// forward — never a wider intermediate that is rounded once at the end. Feature 015 learned this
// the hard way: an explanation derived from unrounded contributions did not reconcile with the
// score it explained, and the fix was to snap first and derive after. The same discipline is what
// makes cash plus positions equal the stored equity exactly rather than nearly.
type dec struct{ v *big.Int }

func mustDec(value string) dec {
	parsed, err := parseDec(value)
	if err != nil {
		panic("backtest: " + err.Error())
	}
	return parsed
}

// parseDec reads a decimal string. Anything beyond the stored precision is rounded half to even,
// the same rule the engine's own renderer uses.
func parseDec(value string) (dec, error) {
	text := strings.TrimSpace(value)
	if text == "" {
		return dec{}, errors.New("empty decimal")
	}
	negative := strings.HasPrefix(text, "-")
	text = strings.TrimPrefix(strings.TrimPrefix(text, "-"), "+")
	integer, fraction, _ := strings.Cut(text, ".")
	if integer == "" && fraction == "" {
		return dec{}, errors.New("malformed decimal " + value)
	}
	digits := integer
	switch {
	case len(fraction) <= places:
		digits += fraction + strings.Repeat("0", places-len(fraction))
	default:
		digits += fraction[:places]
	}
	number, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return dec{}, errors.New("malformed decimal " + value)
	}
	if len(fraction) > places {
		tail := fraction[places:]
		up := tail[0] > '5'
		if tail[0] == '5' {
			up = strings.Trim(tail[1:], "0") != "" ||
				new(big.Int).Mod(number, bigTwo).Sign() == 1
		}
		if up {
			number.Add(number, big.NewInt(1))
		}
	}
	if negative {
		number.Neg(number)
	}
	return dec{number}, nil
}

func (d dec) zero() bool { return d.v == nil || d.v.Sign() == 0 }
func (d dec) sign() int {
	if d.v == nil {
		return 0
	}
	return d.v.Sign()
}
func (d dec) cmp(other dec) int { return d.int().Cmp(other.int()) }
func (d dec) int() *big.Int {
	if d.v == nil {
		return new(big.Int)
	}
	return d.v
}
func (d dec) add(other dec) dec { return dec{new(big.Int).Add(d.int(), other.int())} }
func (d dec) sub(other dec) dec { return dec{new(big.Int).Sub(d.int(), other.int())} }
func (d dec) neg() dec          { return dec{new(big.Int).Neg(d.int())} }

func (d dec) mul(other dec) dec {
	return dec{divideHalfEven(new(big.Int).Mul(d.int(), other.int()), scale)}
}

func (d dec) div(other dec) dec {
	return dec{divideHalfEven(new(big.Int).Mul(d.int(), scale), other.int())}
}

// max is used for the brokerage minimum, which is the reason a small trade can cost more than it
// makes and therefore the reason a simulation that ignored it would overtrade for free.
func (d dec) max(other dec) dec {
	if d.cmp(other) >= 0 {
		return d
	}
	return other
}

// floor is how many whole shares fit. Fractional shares would make every position exactly the
// target size and quietly remove the reason a small account cannot hold ten of anything.
func (d dec) floor() *big.Int {
	quotient, remainder := new(big.Int).QuoRem(d.int(), scale, new(big.Int))
	if remainder.Sign() < 0 {
		quotient.Sub(quotient, big.NewInt(1))
	}
	return quotient
}

func decFromInt(value *big.Int) dec { return dec{new(big.Int).Mul(value, scale)} }

// basisPoints turns a stated cost in basis points into the factor it multiplies by.
func basisPoints(value dec) dec { return value.div(tenThou) }

func (d dec) String() string {
	number := d.int()
	negative := number.Sign() < 0
	digits := new(big.Int).Abs(number).String()
	if len(digits) <= places {
		digits = strings.Repeat("0", places-len(digits)+1) + digits
	}
	split := len(digits) - places
	rendered := digits[:split] + "." + digits[split:]
	if negative {
		rendered = "-" + rendered
	}
	return rendered
}

func (d dec) float() float64 {
	value, _ := new(big.Rat).SetFrac(d.int(), scale).Float64()
	return value
}

// divideHalfEven rounds a quotient to the nearest integer, ties to even — the same rule as the
// engine's renderer, so a figure computed here and a feature value computed there round the same
// way and a reader comparing them is never off by one in the last place.
func divideHalfEven(numerator, denominator *big.Int) *big.Int {
	if denominator.Sign() == 0 {
		panic("backtest: division by zero")
	}
	negative := numerator.Sign()*denominator.Sign() < 0
	n := new(big.Int).Abs(numerator)
	d := new(big.Int).Abs(denominator)
	quotient, remainder := new(big.Int).QuoRem(n, d, new(big.Int))
	twice := new(big.Int).Mul(remainder, bigTwo)
	switch twice.Cmp(d) {
	case 1:
		quotient.Add(quotient, big.NewInt(1))
	case 0:
		if new(big.Int).Mod(quotient, bigTwo).Sign() == 1 {
			quotient.Add(quotient, big.NewInt(1))
		}
	}
	if negative {
		quotient.Neg(quotient)
	}
	return quotient
}
