// Package decimal is the exact decimal arithmetic every money figure in this product passes
// through.
//
// It lives on its own because two features now need it: a backtest valuing a simulated portfolio
// and a person's own holdings. A second implementation would eventually disagree with the first,
// and the disagreement would surface as the two reporting different numbers for the same trade.
package decimal

import (
	"errors"
	"math/big"
	"strings"
)

// Places is the stored precision of every figure this package writes: numeric(24,12), the same
// precision the feature and strategy layers already use.
const Places = 12

var (
	scale   = new(big.Int).Exp(big.NewInt(10), big.NewInt(Places), nil)
	bigTwo  = big.NewInt(2)
	tenThou = MustDec("10000")
	// Zero and One are the two constants every caller needs and nobody should re-derive.
	Zero = Dec{new(big.Int)}
	One  = MustDec("1")
)

// Dec is an exact decimal held as an integer scaled by 10^12.
//
// Every operation rounds to that precision immediately, and the rounded value is what is carried
// forward — never a wider intermediate that is rounded once at the end. Feature 015 learned this
// the hard way: an explanation derived from unrounded contributions did not reconcile with the
// score it explained, and the fix was to snap first and derive after. The same discipline is what
// makes cash plus positions equal the stored equity exactly rather than nearly.
type Dec struct{ v *big.Int }

func MustDec(value string) Dec {
	parsed, err := ParseDec(value)
	if err != nil {
		panic("backtest: " + err.Error())
	}
	return parsed
}

// parseDec reads a decimal string. Anything beyond the stored precision is rounded half to even,
// the same rule the engine's own renderer uses.
func ParseDec(value string) (Dec, error) {
	text := strings.TrimSpace(value)
	if text == "" {
		return Dec{}, errors.New("empty decimal")
	}
	negative := strings.HasPrefix(text, "-")
	text = strings.TrimPrefix(strings.TrimPrefix(text, "-"), "+")
	integer, fraction, _ := strings.Cut(text, ".")
	if integer == "" && fraction == "" {
		return Dec{}, errors.New("malformed decimal " + value)
	}
	digits := integer
	switch {
	case len(fraction) <= Places:
		digits += fraction + strings.Repeat("0", Places-len(fraction))
	default:
		digits += fraction[:Places]
	}
	number, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return Dec{}, errors.New("malformed decimal " + value)
	}
	if len(fraction) > Places {
		tail := fraction[Places:]
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
	return Dec{number}, nil
}

func (d Dec) Zero() bool { return d.v == nil || d.v.Sign() == 0 }
func (d Dec) Sign() int {
	if d.v == nil {
		return 0
	}
	return d.v.Sign()
}
func (d Dec) Cmp(other Dec) int { return d.int().Cmp(other.int()) }
func (d Dec) int() *big.Int {
	if d.v == nil {
		return new(big.Int)
	}
	return d.v
}
func (d Dec) Add(other Dec) Dec { return Dec{new(big.Int).Add(d.int(), other.int())} }
func (d Dec) Sub(other Dec) Dec { return Dec{new(big.Int).Sub(d.int(), other.int())} }
func (d Dec) Neg() Dec          { return Dec{new(big.Int).Neg(d.int())} }

func (d Dec) Mul(other Dec) Dec {
	return Dec{divideHalfEven(new(big.Int).Mul(d.int(), other.int()), scale)}
}

func (d Dec) Div(other Dec) Dec {
	return Dec{divideHalfEven(new(big.Int).Mul(d.int(), scale), other.int())}
}

// max is used for the brokerage minimum, which is the reason a small trade can cost more than it
// makes and therefore the reason a simulation that ignored it would overtrade for free.
func (d Dec) Max(other Dec) Dec {
	if d.Cmp(other) >= 0 {
		return d
	}
	return other
}

// floor is how many whole shares fit. Fractional shares would make every position exactly the
// target size and quietly remove the reason a small account cannot hold ten of anything.
func (d Dec) Floor() *big.Int {
	quotient, remainder := new(big.Int).QuoRem(d.int(), scale, new(big.Int))
	if remainder.Sign() < 0 {
		quotient.Sub(quotient, big.NewInt(1))
	}
	return quotient
}

func FromInt(value *big.Int) Dec { return Dec{new(big.Int).Mul(value, scale)} }

// basisPoints turns a stated cost in basis points into the factor it multiplies by.
func BasisPoints(value Dec) Dec { return value.Div(tenThou) }

func (d Dec) String() string {
	number := d.int()
	negative := number.Sign() < 0
	digits := new(big.Int).Abs(number).String()
	if len(digits) <= Places {
		digits = strings.Repeat("0", Places-len(digits)+1) + digits
	}
	split := len(digits) - Places
	rendered := digits[:split] + "." + digits[split:]
	if negative {
		rendered = "-" + rendered
	}
	return rendered
}

func (d Dec) Float() float64 {
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
