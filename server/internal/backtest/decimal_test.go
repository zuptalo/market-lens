package backtest

import "testing"

// The rounding rule is the one the feature engine already uses. It is tested here rather than
// assumed because every stored figure in this package passes through it, and a half-up rule would
// bias a decade of simulated trades in one direction — a bias invisible in any single trade.
func TestDecimalRoundsHalfToEven(t *testing.T) {
	for _, testCase := range []struct{ value, want string }{
		{"1", "1.000000000000"},
		{"-2.5", "-2.500000000000"},
		{"0.0000000000005", "0.000000000000"},
		{"0.0000000000015", "0.000000000002"},
		{"0.0000000000025", "0.000000000002"},
		{"0.00000000000251", "0.000000000003"},
		{"1234567.891", "1234567.891000000000"},
	} {
		got, err := parseDec(testCase.value)
		if err != nil {
			t.Fatalf("parse %s: %v", testCase.value, err)
		}
		if got.String() != testCase.want {
			t.Errorf("parseDec(%s) = %s, want %s", testCase.value, got, testCase.want)
		}
	}
}

func TestDecimalArithmeticStaysAtStoredPrecision(t *testing.T) {
	third := mustDec("1").div(mustDec("3"))
	if third.String() != "0.333333333333" {
		t.Errorf("1/3 = %s", third)
	}
	if back := third.mul(mustDec("3")); back.String() != "0.999999999999" {
		// Not 1. Saying so is the point: the stored figures reconcile with each other exactly
		// because each step is rounded, not because the arithmetic is exact.
		t.Errorf("(1/3)*3 = %s, want 0.999999999999", back)
	}
	if floor := mustDec("7.9").floor().String(); floor != "7" {
		t.Errorf("floor(7.9) = %s", floor)
	}
	if floor := mustDec("-0.5").floor().String(); floor != "-1" {
		t.Errorf("floor(-0.5) = %s", floor)
	}
}
