// Package currency: BNM (Bank Negara Malaysia) exchange-rate fetching,
// Valkey caching, and minor-unit conversion for per-workspace currencies (issue
// #185). Rates are fetched from https://api.bnm.gov.my/public/exchange-rate
// once daily and cached in Valkey with a 24h TTL. Conversion is used for
// display/new amounts; historical sales keep their original currency.
package currency

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// BNMExchangeRateURL is the public BNM OpenAPI endpoint for daily interbank
// exchange rates (Ringgit per unit of foreign currency).
const BNMExchangeRateURL = "https://api.bnm.gov.my/public/exchange-rate"

// cacheKey is the Valkey key holding the cached BNM rates JSON.
const cacheKey = "jf:currency:bnm_rates"

// cacheTTL is how long a fetched rate set is considered fresh (24h — the
// issue's "once daily" requirement).
const cacheTTL = 24 * time.Hour

// Rate is one currency's BNM rate: how many Ringgit buy one unit of the
// foreign currency (e.g. USD 4.03 means 1 USD = 4.03 MYR).
type Rate struct {
	CurrencyCode string  `json:"currency_code"`
	Unit         int     `json:"unit"`
	Rate         float64 `json:"rate"`
}

// bnmResponse is the BNM API envelope.
type bnmResponse struct {
	Data struct {
		Rates []Rate `json:"rates"`
	} `json:"data"`
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
	req.Header.Set("Accept", "application/json")
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
		return nil, fmt.Errorf("bnm http %d: %s", resp.StatusCode, truncate(raw, 256))
	}
	var out bnmResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("bnm decode: %w", err)
	}
	if len(out.Data.Rates) == 0 {
		return nil, fmt.Errorf("bnm: no rates in response")
	}
	return out.Data.Rates, nil
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

// ConvertMinor converts an amount in minor units from one currency to another
// using the cached BNM rates. Both amounts are integer minor units (e.g. sen).
// from/to are ISO currency codes; MYR is the BNM base. Returns the converted
// minor-unit amount rounded to the nearest integer. When either rate is
// unknown, ok=false (caller should fall back to the original amount).
func (s *Store) ConvertMinor(ctx context.Context, from, to string, amountMinor int64) (int64, bool) {
	if from == "" || to == "" || from == to {
		return amountMinor, true
	}
	rates, _, err := s.Rates(ctx)
	if err != nil {
		return amountMinor, false
	}
	// BNM rates are Ringgit per unit of foreign currency. To convert
	// from->to: value_in_MYR = amount_from * rate_from; then
	// value_in_to = value_in_MYR / rate_to.
	rf, okFrom := rateFor(rates, from)
	rt, okTo := rateFor(rates, to)
	if !okFrom || !okTo || rf <= 0 || rt <= 0 {
		return amountMinor, false
	}
	// Work in float minor units; round to nearest integer at the end.
	myr := float64(amountMinor) * rf
	converted := myr / rt
	return int64(converted + 0.5), true
}

func truncate(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n])
	}
	return string(b)
}
