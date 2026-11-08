package mcpsrv

// convert_currency — the rate store's conversion must have a PRODUCTION caller,
// and that caller must apply the rate the way the store publishes it.
//
// THE DEFECT THIS FILE GUARDS (card t_467614db). currency.Store.ConvertMinor was
// added for issue #185 and, measured on the shipped tree e87ce565, had ZERO
// production call sites — only its own tests and a second test helper that held a
// COPY of the arithmetic. Meanwhile the Settings hint, this package's
// get_currency_rates description and the docs all told the merchant that rates were
// "used for display/conversion of new amounts". Nothing multiplied an amount by a
// rate, so the claim was false on every surface that made it.
//
// Two properties are pinned here, because the first is worthless without the
// second:
//
//	1. The tool exists and is registered with convertCurrency as its handler, and
//	   that handler converts THROUGH Store.ConvertMinor rather than re-implementing
//	   the arithmetic next to the rates. A second copy at the call site is exactly
//	   how the conversion would drift out from under the tested function again.
//	2. The conversion it returns is the per-ONE-unit conversion: 100.00 JPY is
//	   RM 2.60 (260 sen), not RM 260.44. Rate already carries the division by the
//	   published block size (currency.toRates divides a per-100 quote), so a
//	   consumer that divides by Unit a second time is 100x out. The card that
//	   opened this work warned about precisely that trap.
//
// WHAT THIS DOES NOT COVER. It does not prove the BNM fetch/parse (that is
// internal/currency's own suite, which drives a fake BNM endpoint), and it does
// not prove the rendered /docs or Settings copy — those are the web unit suite
// and a browser read. The Valkey-backed test is skipped when
// JOMFORM_TEST_VALKEY_URL is unset, exactly like the other tests in this package
// that need a real Valkey; the registry and argument-shape tests never skip.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/wzul/jomform/api/internal/currency"
)

// convertRatesKey mirrors currency.cacheKey, which is unexported. The value is
// the []currency.Rate JSON the daily refresh writes.
const convertRatesKey = "jf:currency:bnm_rates"

// TestConvertCurrencyIsRegisteredWithTheConversionHandler is the "has a caller"
// half. It reads the registered registry (the same tools/list-backed parse the
// other guards use) and then the handler's own source, so "the tool exists" and
// "the tool really goes through Store.ConvertMinor" are one assertion rather than
// two that can disagree.
func TestConvertCurrencyIsRegisteredWithTheConversionHandler(t *testing.T) {
	pkg := parsePackage(t)

	var reg *toolReg
	for i := range pkg.tools {
		if pkg.tools[i].Name == "convert_currency" {
			reg = &pkg.tools[i]
			break
		}
	}
	if reg == nil {
		t.Fatal("convert_currency is not registered — the rate store's conversion has no production caller again " +
			"(that is the t_467614db defect: rates displayed everywhere, nothing converting an amount)")
	}
	if reg.Handler != "convertCurrency" {
		t.Fatalf("convert_currency is registered with handler %q, want convertCurrency", reg.Handler)
	}
	if strings.TrimSpace(reg.Description) == "" {
		t.Fatal("convert_currency has no description — an agent would be told nothing about amount_minor's unit")
	}

	src, err := os.ReadFile(filepath.Join(mcpPackageDir(), "currency.go"))
	if err != nil {
		t.Fatalf("read currency.go: %v", err)
	}
	// The store method, not a local formula: re-implementing the arithmetic at the
	// call site would leave Store.ConvertMinor's own tests green while the tool
	// drifted (and would re-open the 100x trap the store already closed).
	if !strings.Contains(string(src), "s.deps.Currency.ConvertMinor(") {
		t.Fatal("convertCurrency does not call currency.Store.ConvertMinor — the conversion the store publishes " +
			"tests must be the conversion the tool returns")
	}
}

// TestConvertCurrencyRejectsMalformedCodes pins the argument shape: a code that is
// not three ASCII letters is refused by name, so a caller is not told "no rate"
// when what it sent was malformed. No store is wired, so reaching the store at all
// would also change the failure — this runs with a nil store on purpose.
func TestConvertCurrencyRejectsMalformedCodes(t *testing.T) {
	srv := &Server{deps: &Deps{}}

	cases := []struct {
		name string
		in   convertCurrencyIn
		want string
	}{
		{"empty from", convertCurrencyIn{From: "", To: "MYR", AmountMinor: 100}, "from must be"},
		{"short from", convertCurrencyIn{From: "JP", To: "MYR", AmountMinor: 100}, "from must be"},
		{"dotted from", convertCurrencyIn{From: "JPY!", To: "MYR", AmountMinor: 100}, "from must be"},
		{"empty to", convertCurrencyIn{From: "JPY", To: "", AmountMinor: 100}, "to must be"},
		{"numeric to", convertCurrencyIn{From: "JPY", To: "12", AmountMinor: 100}, "to must be"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := srv.convertCurrency(context.Background(), nil, tc.in)
			if err == nil {
				t.Fatalf("expected a refusal for %+v", tc.in)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

// TestConvertCurrencyWithoutARateStore pins the dev-mode shape: a server built
// without a rate store refuses with a reason, rather than converting to zero.
func TestConvertCurrencyWithoutARateStore(t *testing.T) {
	srv := &Server{deps: &Deps{}}
	_, _, err := srv.convertCurrency(context.Background(), nil,
		convertCurrencyIn{From: "JPY", To: "MYR", AmountMinor: 10000})
	if err == nil {
		t.Fatal("expected a refusal when no rate store is wired")
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Fatalf("error = %q, want it to say the rates are unavailable", err)
	}
}

// TestConvertCurrencyConvertsThroughTheCachedRates is the end-to-end half: a real
// Valkey holding a warm BNM cache, the real store, and the tool's own handler.
//
// The numbers are the published BNM figures the prod-verify card measured
// (t_b4c23121): USD 4.084 per 1, JPY 2.6044 per 100. A tool that divided by the
// published unit again would answer 3 sen for 100 JPY, and one that never divided
// in toRates would answer 26,044 — this asserts 260.
func TestConvertCurrencyConvertsThroughTheCachedRates(t *testing.T) {
	valkeyURL := strings.TrimSpace(os.Getenv("JOMFORM_TEST_VALKEY_URL"))
	if valkeyURL == "" {
		t.Skip("JOMFORM_TEST_VALKEY_URL not set; the rates live in Valkey, so without it this test " +
			"could not observe the conversion the tool returns")
	}
	// The env URL is written valkey:// (the compose service), which go-redis'
	// ParseURL does not know; its wire protocol is Redis'.
	opts, err := goredis.ParseURL(strings.Replace(valkeyURL, "valkey://", "redis://", 1))
	if err != nil {
		t.Fatalf("parse JOMFORM_TEST_VALKEY_URL: %v", err)
	}
	rdb := goredis.NewClient(opts)
	ctx := context.Background()
	// Registered first so it runs LAST (cleanups are LIFO): the restore below still
	// has a live client.
	t.Cleanup(func() { _ = rdb.Close() })

	// The key is shared with the daily refresh, so put back exactly what was there.
	// This test is the only writer of it in the suite, but a probe API on the same
	// Valkey must not be left reading a fixture.
	prev, prevErr := rdb.Get(ctx, convertRatesKey).Bytes()
	t.Cleanup(func() {
		if prevErr == nil {
			_ = rdb.Set(context.Background(), convertRatesKey, prev, time.Hour).Err()
			return
		}
		_ = rdb.Del(context.Background(), convertRatesKey).Err()
	})

	body, err := json.Marshal([]currency.Rate{
		{CurrencyCode: "USD", Unit: 1, Rate: 4.084},
		{CurrencyCode: "JPY", Unit: 100, Rate: 0.026044}, // published 2.6044 per 100
	})
	if err != nil {
		t.Fatalf("marshal rates: %v", err)
	}
	if err := rdb.Set(ctx, convertRatesKey, body, time.Hour).Err(); err != nil {
		t.Fatalf("seed %s: %v", convertRatesKey, err)
	}

	srv := &Server{deps: &Deps{Currency: currency.New(rdb, nil)}}

	cases := []struct {
		name string
		in   convertCurrencyIn
		want int64
	}{
		// The per-unit trap. 10,000 minor units = 100.00 JPY.
		{"per-100 quote", convertCurrencyIn{From: "JPY", To: "MYR", AmountMinor: 10000}, 260},
		// unit=1 stays exact in both directions.
		{"unit-1 quote", convertCurrencyIn{From: "USD", To: "MYR", AmountMinor: 10000}, 40840},
		{"round trip", convertCurrencyIn{From: "MYR", To: "USD", AmountMinor: 40840}, 10000},
		// Lower case in, canonical out.
		{"lower-case code", convertCurrencyIn{From: "jpy", To: "myr", AmountMinor: 10000}, 260},
		// Same currency needs no rate and must not be refused.
		{"same currency", convertCurrencyIn{From: "MYR", To: "MYR", AmountMinor: 500}, 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, out, err := srv.convertCurrency(ctx, nil, tc.in)
			if err != nil {
				t.Fatalf("convertCurrency(%+v): %v", tc.in, err)
			}
			got, ok := out["converted_minor"].(int64)
			if !ok {
				t.Fatalf("converted_minor is %T (%v), want int64", out["converted_minor"], out["converted_minor"])
			}
			if got != tc.want {
				t.Fatalf("%s → %s: converted_minor = %d, want %d (amount_minor=%d)",
					tc.in.From, tc.in.To, got, tc.want, tc.in.AmountMinor)
			}
			if out["from"] != strings.ToUpper(tc.in.From) || out["to"] != strings.ToUpper(tc.in.To) {
				t.Fatalf("echoed codes = %v → %v, want the canonical upper-case codes", out["from"], out["to"])
			}
		})
	}

	// A code the cache does not carry is refused with a pointer to the rate list,
	// not converted to zero.
	if _, _, err := srv.convertCurrency(ctx, nil,
		convertCurrencyIn{From: "AED", To: "MYR", AmountMinor: 100}); err == nil {
		t.Fatal("expected a refusal for a code that is not in the cached rate set")
	} else if !strings.Contains(err.Error(), "get_currency_rates") {
		t.Fatalf("error = %q, want it to point at get_currency_rates", err)
	}
}
