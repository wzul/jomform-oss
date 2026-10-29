// Package currency: BNM (Bank Negara Malaysia) exchange-rate fetching,
// Valkey caching, and minor-unit conversion for per-workspace currencies (issue
// #185). Rates are fetched from https://api.bnm.gov.my/public/exchange-rate
// once daily and cached in Valkey with a 24h TTL.
//
// Conversion is ON DEMAND and display/reporting only: the single production
// consumer is the MCP convert_currency tool (api/internal/mcpsrv/currency.go).
// Nothing converts a saved amount — products and sales are stored in the
// workspace's currency and historical sales keep theirs, so a rate moving can
// never rewrite a completed order. The Settings page's rates table is reference
// data and says so (card t_467614db: before that tool existed this package
// carried a "used for display/conversion of new amounts" claim with no consumer
// behind it at all).
package currency

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/wzul/jomform/api/internal/textutil"
)

// BNMExchangeRateURL is the public BNM OpenAPI endpoint for daily interbank
// exchange rates (Ringgit per unit of foreign currency).
const BNMExchangeRateURL = "https://api.bnm.gov.my/public/exchange-rate"

// BNMRatesAccept is the Accept value BNM's Open API requires. The public API
// serves its JSON envelope ONLY for the versioned vendor media type: a plain
// "application/json", no Accept header at all, or an unsupported version (v2)
// is answered 404 with an empty {"message": ""} body. Sending the wrong one
// does not degrade — it guarantees every refresh fails, which is how the rates
// cache stayed empty while the endpoint kept answering 200 with [].
const BNMRatesAccept = "application/vnd.BNM.API.v1+json"

// cacheKey is the Valkey key holding the cached BNM rates JSON.
const cacheKey = "jf:currency:bnm_rates"

// cacheTTL is how long a fetched rate set is considered fresh (24h — the
// issue's "once daily" requirement).
const cacheTTL = 24 * time.Hour

// Rate is one currency's BNM rate, normalised to Ringgit per ONE unit of the
// foreign currency (e.g. USD 4.084 means 1 USD = 4.084 MYR).
//
// Unit is the block size BNM publishes the quote in: most currencies are quoted
// per 1 unit, but 15 of the 27 (JPY, THB, IDR, …) are quoted per 100. It is
// kept for provenance only — Rate is already divided by it, so consumers must
// not divide again (Rate*Unit is the value of one published block).
type Rate struct {
	CurrencyCode string  `json:"currency_code"`
	Unit         int     `json:"unit"`
	Rate         float64 `json:"rate"`
}

// bnmQuote is one entry of BNM's v1 envelope. Two shape facts the decoder has
// to honour and no BNM document spells out: `data` is an ARRAY (not an object
// keyed by a rate list), and the quote under `rate` is an OBJECT carrying
// buying/selling/middle — not a number. BNM leaves buying_rate AND selling_rate
// null for some currencies (SDR today) while middle_rate is populated for every
// quote, so middle is the only field that yields a rate for all 27.
type bnmQuote struct {
	CurrencyCode string `json:"currency_code"`
	Unit         int    `json:"unit"`
	Rate         struct {
		Date        string  `json:"date"`
		BuyingRate  float64 `json:"buying_rate"`
		SellingRate float64 `json:"selling_rate"`
		MiddleRate  float64 `json:"middle_rate"`
	} `json:"rate"`
}

// bnmResponse is the BNM v1 envelope: {"data":[ …quotes ]}.
type bnmResponse struct {
	Data []bnmQuote `json:"data"`
}

// Store fetches + caches BNM rates in Valkey and converts minor units.
type Store struct {
	http *http.Client
	rdb  *goredis.Client
	log  *slog.Logger
}

// New builds a currency Store. rdb may be nil (cache disabled — rates are
// fetched on demand and not persisted); log may be nil.
func New(rdb *goredis.Client, log *slog.Logger) *Store {
	if log == nil {
		log = slog.Default()
	}
	return &Store{
		http: &http.Client{Timeout: 20 * time.Second},
		rdb:  rdb,
		log:  log,
	}
}

// Client exposes the underlying Valkey client (nil when not wired).
func (s *Store) Client() *goredis.Client { return s.rdb }

// FetchRates calls the BNM API and returns the parsed rates. It does not
// touch the cache.
func (s *Store) FetchRates(ctx context.Context) ([]Rate, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, BNMExchangeRateURL, nil)
	if err != nil {
		return nil, fmt.Errorf("bnm request: %w", err)
	}
	req.Header.Set("Accept", BNMRatesAccept)
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bnm fetch: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("bnm read: %w", err)
	}
	if resp.StatusCode >= 400 {
		// textutil cuts on a rune boundary: a multi-byte BNM body must not
		// render as a broken character in the operator's message.
		return nil, fmt.Errorf("bnm http %d: %s", resp.StatusCode, textutil.Truncate(string(raw), 256))
	}
	var out bnmResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("bnm decode: %w", err)
	}
	rates := toRates(out.Data)
	if len(rates) == 0 {
		return nil, fmt.Errorf("bnm: no rates in response")
	}
	return rates, nil
}

// toRates flattens BNM's nested quotes into the per-single-unit Rate that the
// REST endpoint, the MCP tool and the settings page all document ("Ringgit per
// unit of foreign currency"). BNM quotes 15 of its 27 currencies per 100 units,
// so the published figure is divided by the unit — without that, a JPY or THB
// amount converts 100x off. A quote with no code or no middle rate is dropped
// rather than cached as a zero rate, which would convert every amount in that
// currency to nothing.
func toRates(quotes []bnmQuote) []Rate {
	out := make([]Rate, 0, len(quotes))
	for _, q := range quotes {
		code := strings.ToUpper(strings.TrimSpace(q.CurrencyCode))
		if code == "" || q.Rate.MiddleRate <= 0 {
			continue
		}
		unit := q.Unit
		if unit <= 0 {
			unit = 1
		}
		out = append(out, Rate{
			CurrencyCode: code,
			Unit:         unit,
			Rate:         q.Rate.MiddleRate / float64(unit),
		})
	}
	return out
}

// Refresh fetches fresh BNM rates and stores them in Valkey (24h TTL). It is
// the daily job's entry point. Returns the number of rates cached.
func (s *Store) Refresh(ctx context.Context) (int, error) {
	rates, err := s.FetchRates(ctx)
	if err != nil {
		return 0, err
	}
	if s.rdb == nil {
		return len(rates), nil
	}
	buf, err := json.Marshal(rates)
	if err != nil {
		return 0, fmt.Errorf("marshal rates: %w", err)
	}
	if err := s.rdb.Set(ctx, cacheKey, buf, cacheTTL).Err(); err != nil {
		return 0, fmt.Errorf("cache rates: %w", err)
	}
	return len(rates), nil
}

// Rates returns the cached BNM rates, fetching + caching on a cache miss
// (lazy refresh). Returns the rates and whether they came from cache.
func (s *Store) Rates(ctx context.Context) ([]Rate, bool, error) {
	if s.rdb != nil {
		raw, err := s.rdb.Get(ctx, cacheKey).Bytes()
		if err == nil {
			var rates []Rate
			if json.Unmarshal(raw, &rates) == nil && len(rates) > 0 {
				return rates, true, nil
			}
		}
	}
	rates, err := s.FetchRates(ctx)
	if err != nil {
		return nil, false, err
	}
	if s.rdb != nil {
		if buf, merr := json.Marshal(rates); merr == nil {
			_ = s.rdb.Set(ctx, cacheKey, buf, cacheTTL).Err()
		}
	}
	return rates, false, nil
}

// rateFor returns the BNM rate for a currency code. MYR is the BNM base
// (rate 1.0) and is never listed in the foreign-currency rates, so it is
// handled explicitly. Returns (rate, true) when known.
func rateFor(rates []Rate, code string) (float64, bool) {
	code = strings.ToUpper(code)
	if code == "MYR" {
		return 1.0, true
	}
	for _, r := range rates {
		if strings.ToUpper(r.CurrencyCode) == code {
			return r.Rate, true
		}
	}
	return 0, false
}

// convertMinorWithRates is the conversion arithmetic without the cache, so the
// same code is what ConvertMinor runs and what the tests exercise. It is one
// function rather than a copy in the test file on purpose: a test holding its own
// copy passes while the production path drifts (that was the shape here until
// card t_467614db — the copy in currency_test.go was the only thing the math test
// ever called).
//
// Both amounts are integer minor units (hundredths, e.g. sen). Rate is already
// Ringgit per ONE unit (toRates divides the published per-100 quote), so this
// multiplies by Rate and must never divide by Unit again — doing so is the 100x
// error the per-unit decode fixed.
func convertMinorWithRates(rates []Rate, from, to string, amountMinor int64) (int64, bool) {
	from, to = strings.ToUpper(strings.TrimSpace(from)), strings.ToUpper(strings.TrimSpace(to))
	if from == "" || to == "" || from == to {
		return amountMinor, true
	}
	// BNM rates are Ringgit per unit of foreign currency. To convert
	// from->to: value_in_MYR = amount_from * rate_from; then
	// value_in_to = value_in_MYR / rate_to.
	rf, okFrom := rateFor(rates, from)
	rt, okTo := rateFor(rates, to)
	if !okFrom || !okTo || rf <= 0 || rt <= 0 {
		return amountMinor, false
	}
	// Work in float minor units; round to the nearest integer at the end.
	// math.Round rather than int64(x + 0.5): a signed amount (a refund, or an
	// exchange-rate difference quoted backwards) would otherwise round toward
	// zero — -2.6 would become -2.
	return int64(math.Round(float64(amountMinor) * rf / rt)), true
}

// ConvertMinor converts an amount in minor units from one currency to another
// using the cached BNM rates. Both amounts are integer minor units (e.g. sen).
// from/to are ISO currency codes; MYR is the BNM base. Returns the converted
// minor-unit amount rounded to the nearest integer. When either rate is
// unknown, ok=false (caller should fall back to the original amount).
//
// This is display/reporting conversion only, and its one production caller is the
// MCP convert_currency tool — no stored amount is ever re-converted.
func (s *Store) ConvertMinor(ctx context.Context, from, to string, amountMinor int64) (int64, bool) {
	from, to = strings.ToUpper(strings.TrimSpace(from)), strings.ToUpper(strings.TrimSpace(to))
	if from == "" || to == "" || from == to {
		// Same currency (or nothing to convert): no rate needed, so this must
		// not depend on the cache being warm.
		return amountMinor, true
	}
	rates, _, err := s.Rates(ctx)
	if err != nil {
		return amountMinor, false
	}
	return convertMinorWithRates(rates, from, to, amountMinor)
}
