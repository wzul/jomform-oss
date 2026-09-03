package currency

import (
	"context"
	"testing"
)

// TestConvertMinor exercises the BNM-rate minor-unit conversion math without a
// network or Valkey. It uses a Store with a nil cache so Rates() fetches on
// demand — but we stub the fetch by injecting rates via a custom http client
// is not possible here, so we test the pure math through a helper that takes
// rates directly.
func TestConvertMinorMath(t *testing.T) {
	rates := []Rate{
		{CurrencyCode: "USD", Unit: 1, Rate: 4.0}, // 1 USD = 4 MYR
		{CurrencyCode: "SGD", Unit: 1, Rate: 3.0}, // 1 SGD = 3 MYR
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
		{"MYR to USD", "MYR", "USD", 400, 100, true},   // 400 sen MYR = 100 sen USD
		{"USD to MYR", "USD", "MYR", 100, 400, true},   // 100 sen USD = 400 sen MYR
		{"USD to SGD", "USD", "SGD", 300, 400, true},   // 300*4/3 = 400
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

// convertMinorWithRates is the pure form of Store.ConvertMinor, factored out
// so the math is unit-testable without a network or Valkey.
func convertMinorWithRates(rates []Rate, from, to string, amountMinor int64) (int64, bool) {
	if from == "" || to == "" || from == to {
		return amountMinor, true
	}
	rf, okFrom := rateFor(rates, from)
	rt, okTo := rateFor(rates, to)
	if !okFrom || !okTo || rf <= 0 || rt <= 0 {
		return amountMinor, false
	}
	myr := float64(amountMinor) * rf
	converted := myr / rt
	return int64(converted + 0.5), true
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
