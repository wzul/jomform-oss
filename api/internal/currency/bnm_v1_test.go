package currency

// The BNM public API negotiates content strictly, so these tests drive
// FetchRates through a fake BNM that reproduces the live rule instead of
// pinning a header string in isolation: the versioned vendor media type gets the
// v1 envelope, and every other Accept — "application/json", an unsupported
// version such as v2, or none at all — gets the 404 with the empty
// {"message": ""} body the real endpoint answers.
//
// A header regression therefore fails here the way it failed in production,
// instead of passing a literal comparison. The envelope fixture is the shape
// measured from the live endpoint: `data` is an ARRAY, the quote under `rate` is
// an OBJECT, 15 of the 27 currencies are quoted per 100 units, and SDR's
// buying/selling rates are null while its middle rate is populated.

import (
	"context"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
)

const (
	// bnmAcceptWanted is deliberately a literal rather than BNMRatesAccept: if
	// the constant is ever changed to something else — including the
	// "application/json" this bug shipped — the fake answers 404 and FetchRates
	// fails for the same reason the daily prod refresh did.
	bnmAcceptWanted = "application/vnd.BNM.API.v1+json"

	// bnmV1Body mirrors a real https://api.bnm.gov.my/public/exchange-rate body.
	bnmV1Body = `{"data":[` +
		`{"currency_code":"USD","unit":1,"rate":{"date":"2026-10-23","buying_rate":4.082,"selling_rate":4.086,"middle_rate":4.084}},` +
		`{"currency_code":"JPY","unit":100,"rate":{"date":"2026-10-23","buying_rate":2.6028,"selling_rate":2.606,"middle_rate":2.6044}},` +
		`{"currency_code":"SDR","unit":1,"rate":{"date":"2026-10-23","buying_rate":null,"selling_rate":null,"middle_rate":5.5378}}` +
		`]}`

	// bnmNotFoundBody is the live 404 body, whitespace included.
	bnmNotFoundBody = "{\n    \"message\": \"\"\n}\n"
)

// bnmFake answers like the live API. seenAccept, when non-nil, records the
// Accept value the request actually carried.
func bnmFake(body string, seenAccept *string) roundTripFunc {
	return func(r *http.Request) (*http.Response, error) {
		got := r.Header.Get("Accept")
		if seenAccept != nil {
			*seenAccept = got
		}
		status, respBody := http.StatusOK, body
		if got != bnmAcceptWanted {
			status, respBody = http.StatusNotFound, bnmNotFoundBody
		}
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": []string{"application/vnd.BNM.API.v1+json"}},
			Body:       io.NopCloser(strings.NewReader(respBody)),
		}, nil
	}
}

// TestFetchRatesSendsBNMVersionedMediaType is the regression guard for the
// refresh that never succeeded: BNM serves JSON only for its versioned vendor
// media type, so an Accept of "application/json" is a guaranteed 404.
func TestFetchRatesSendsBNMVersionedMediaType(t *testing.T) {
	if BNMRatesAccept != bnmAcceptWanted {
		t.Fatalf("BNMRatesAccept = %q, want %q — BNM answers 404 to anything else, so the rates cache can never be written",
			BNMRatesAccept, bnmAcceptWanted)
	}

	var seen string
	s := &Store{http: &http.Client{Transport: bnmFake(bnmV1Body, &seen)}}
	if _, err := s.FetchRates(context.Background()); err != nil {
		t.Fatalf("FetchRates returned %v with Accept %q — BNM serves this endpoint only for %q",
			err, seen, bnmAcceptWanted)
	}
	if seen != bnmAcceptWanted {
		t.Fatalf("FetchRates sent Accept %q, want %q", seen, bnmAcceptWanted)
	}
}

// TestFetchRatesParsesTheBNMV1Envelope pins the decode contract: the previous
// struct expected data.rates[] with a numeric rate, a shape BNM never sends, so
// even a correct Accept would have failed here.
func TestFetchRatesParsesTheBNMV1Envelope(t *testing.T) {
	s := &Store{http: &http.Client{Transport: bnmFake(bnmV1Body, nil)}}
	rates, err := s.FetchRates(context.Background())
	if err != nil {
		t.Fatalf("FetchRates: %v — the v1 envelope is data[] of quote objects, not data.rates[]", err)
	}
	if len(rates) != 3 {
		t.Fatalf("got %d rates, want 3", len(rates))
	}

	byCode := make(map[string]Rate, len(rates))
	for _, r := range rates {
		if _, dup := byCode[r.CurrencyCode]; dup {
			t.Fatalf("duplicate rate for %s", r.CurrencyCode)
		}
		byCode[r.CurrencyCode] = r
	}

	cases := []struct {
		code string
		unit int
		want float64 // Ringgit per ONE unit
	}{
		{"USD", 1, 4.084},
		{"JPY", 100, 2.6044 / 100}, // published per 100 units, stored per unit
		{"SDR", 1, 5.5378},         // buying/selling are null upstream
	}
	for _, tc := range cases {
		got, ok := byCode[tc.code]
		if !ok {
			t.Fatalf("%s missing from the parsed rates: %+v", tc.code, rates)
		}
		if got.Unit != tc.unit {
			t.Errorf("%s unit = %d, want %d (the published block size)", tc.code, got.Unit, tc.unit)
		}
		if math.Abs(got.Rate-tc.want) > 1e-12 {
			t.Errorf("%s rate = %v, want %v Ringgit per unit", tc.code, got.Rate, tc.want)
		}
	}
}

// TestFetchedRatesConvertPerUnit closes the loop onto the surface that uses the
// rates: 100 JPY must be ~260 sen, not ~26,044. Converting off the unpublished
// per-unit figure is the 100x error that a flat decode would have hidden.
func TestFetchedRatesConvertPerUnit(t *testing.T) {
	s := &Store{http: &http.Client{Transport: bnmFake(bnmV1Body, nil)}}
	ctx := context.Background()

	// 10,000 JPY minor units = 100 JPY at BNM's 2.6044 MYR per 100 JPY.
	got, ok := s.ConvertMinor(ctx, "JPY", "MYR", 10000)
	if !ok {
		t.Fatal("JPY->MYR reported no rate, want the fetched rate")
	}
	if got != 260 {
		t.Fatalf("100 JPY = %d sen, want 260 — the per-100 quote is applied without dividing by unit", got)
	}

	// The unit=1 path must stay exact in both directions: 4.084 MYR per USD.
	if got, ok := s.ConvertMinor(ctx, "USD", "MYR", 10000); !ok || got != 40840 {
		t.Fatalf("100 USD = %d sen (ok=%v), want 40840", got, ok)
	}
	if got, ok := s.ConvertMinor(ctx, "MYR", "USD", 40840); !ok || got != 10000 {
		t.Fatalf("408.40 MYR = %d sen USD (ok=%v), want 10000", got, ok)
	}
}

// TestFetchRatesRejectsAnEnvelopeWithNoRates keeps the "never cache an empty
// set" guard: an empty list must surface as an error, not as a successful
// refresh that writes rates: [] over the last good cache.
func TestFetchRatesRejectsAnEnvelopeWithNoRates(t *testing.T) {
	s := &Store{http: &http.Client{Transport: bnmFake(`{"data":[]}`, nil)}}
	_, err := s.FetchRates(context.Background())
	if err == nil {
		t.Fatal("expected an error for an empty rate set")
	}
	if !strings.Contains(err.Error(), "no rates") {
		t.Fatalf("error = %q, want it to mention the empty rate set", err)
	}
}

// TestToRatesDropsUnusableQuotes guards the "drop, never cache zero" rule: a
// zero rate would convert every amount in that currency to nothing.
func TestToRatesDropsUnusableQuotes(t *testing.T) {
	quotes := []bnmQuote{
		{CurrencyCode: "usd", Unit: 0}, // no rate → dropped; unit 0 would be a divide-by-zero
		{CurrencyCode: "", Unit: 1},    // no code → dropped
		{CurrencyCode: "  thb  ", Unit: 100},
	}
	quotes[2].Rate.MiddleRate = 14.02

	got := toRates(quotes)
	if len(got) != 1 {
		t.Fatalf("got %d rates, want 1: %+v", len(got), got)
	}
	if got[0].CurrencyCode != "THB" {
		t.Errorf("currency code = %q, want THB (normalised)", got[0].CurrencyCode)
	}
	if math.Abs(got[0].Rate-0.1402) > 1e-12 {
		t.Errorf("THB rate = %v, want 0.1402 per unit", got[0].Rate)
	}
}
