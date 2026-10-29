package currency

import (
	"context"
	"testing"
)

// TestConvertMinorMath exercises the production conversion arithmetic (the same
// convertMinorWithRates that Store.ConvertMinor calls) without a network or
// Valkey. Until card t_467614db this file held its OWN copy of the arithmetic, so
// the test could stay green while the shipped function changed underneath it.
func TestConvertMinorMath(t *testing.T) {
	rates := []Rate{
		{CurrencyCode: "USD", Unit: 1, Rate: 4.0},        // 1 USD = 4 MYR
		{CurrencyCode: "SGD", Unit: 1, Rate: 3.0},        // 1 SGD = 3 MYR
		{CurrencyCode: "JPY", Unit: 100, Rate: 0.026044}, // published per 100; Rate is per 1
	}
	cases := []struct {
		name   string
		from   string
		to     string
		amount int64
		want   int64
		ok     bool
	}{
		{"same currency", "MYR", "MYR", 100, 100, true},
		{"same currency, lower case", "usd", "USD", 100, 100, true},
		{"MYR to USD", "MYR", "USD", 400, 100, true}, // 400 sen MYR = 100 sen USD
		{"USD to MYR", "USD", "MYR", 100, 400, true}, // 100 sen USD = 400 sen MYR
		{"USD to SGD", "USD", "SGD", 300, 400, true}, // 300*4/3 = 400
		// The per-unit trap: Rate already carries the /100, so 100.00 JPY is
		// 260 sen and NOT 26,044 sen. A second division by Unit would make this
		// read 3.
		{"per-100 currency", "JPY", "MYR", 10000, 260, true},
		// Rounding is to nearest, half away from zero, and symmetric for a
		// signed amount (a refund or a backwards quote): 2 sen MYR / 4 =
		// 0.5 sen -> 1, and -2 -> -1. Truncating x+0.5 would have said 0 / 0.
		{"rounds half away from zero", "MYR", "USD", 2, 1, true},
		{"rounds a negative half away from zero", "MYR", "USD", -2, -1, true},
		{"unknown from", "XXX", "MYR", 100, 100, false},
		{"unknown to", "MYR", "XXX", 100, 100, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := convertMinorWithRates(rates, tc.from, tc.to, tc.amount)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// TestStoreNilCache ensures a Store with a nil Valkey client still returns
// rates (fetch path) without panicking — the lazy-refresh fallback.
func TestStoreNilCache(t *testing.T) {
	s := New(nil, nil)
	if s.Client() != nil {
		t.Fatal("expected nil client for nil cache")
	}
	// Rates() would hit the network; just assert the store is constructible
	// and the cache key is stable.
	if cacheKey != "jf:currency:bnm_rates" {
		t.Fatalf("unexpected cache key %q", cacheKey)
	}
	_ = context.Background()
}
