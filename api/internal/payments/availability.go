// The brand's LIVE listing of the two FPX products (card t_1d0198e3).
//
// WHY THIS EXISTS. FPX B2C (`fpx`) and FPX B2B1 (`fpx_b2b1`) are two products
// with two fee structures, so the picker offers them as two rows. Whether a row
// may be OFFERED is not a merchant setting: a brand has to have the product
// enabled at CHIP, and a merchant enabled for B2C only must not see a B2B1 row.
// The merchant's own whitelist cannot answer that — an unrestricted workspace
// (empty whitelist) enables everything this product can label — so the picker
// asks CHIP's `GET /payment_methods/`, which is the same call the purchase path
// already trusts for its resolved whitelist.
//
// AMOUNT. CHIP's API reference (docs.chip-in.asia, "List of payment methods")
// marks `amount` optional in the schema but says it is "strongly recommended in
// practice": most methods, FPX included, have a minimum amount, and omitting the
// parameter HIDES them from the response. A page render does not know the cart,
// so it probes with availabilityProbeAmountSen (RM10 — the value CHIP's own docs
// name as a safe default). The purchase then re-asks with the REAL total, which
// is the one place the answer is exact.
//
// COST AND FAILURE. One CHIP round trip per (brand, currency, amount) per
// methodAvailabilityTTL; a workspace with no CHIP credentials never calls at all.
// A failure is NOT cached and answers known=false, and the caller then keeps the
// whitelist's own rows: a CHIP outage must not empty the buyer's payment picker.
package payments

import (
	"context"
	"strings"
	"time"

	"github.com/wzul/jomform/api/internal/cryptoutil"
)

const (
	// availabilityProbeAmountSen is RM10 — the value CHIP's own reference
	// suggests for probing the method list when the cart amount is unknown.
	availabilityProbeAmountSen int64 = 1000
	// methodAvailabilityTTL bounds how stale a listing may be. Long enough that
	// a form's traffic does not pay a CHIP call per visitor, short enough that a
	// merchant who enables FPX B2B1 in the CHIP portal sees the row without a
	// restart.
	methodAvailabilityTTL = 5 * time.Minute
	// availabilityCacheMax bounds the map (see storeAvailability): the purchase
	// path keys it by the exact amount, so the key space is open-ended.
	availabilityCacheMax = 512
	// availabilityTimeout keeps a slow CHIP from holding a page render: past
	// this the listing is simply unknown and the whitelist's rows stand.
	availabilityTimeout = 3 * time.Second
)

// methodAvailabilityKey is one cached listing: the brand (its credentials), the
// purchase currency and the probe amount. The amount is part of the key because
// CHIP filters the list by it — a render-time probe must never answer a
// purchase-time question.
type methodAvailabilityKey struct {
	brandID  string
	currency string
	amount   int64
}

type methodAvailabilityEntry struct {
	ids []string
	at  time.Time
}

func (s *Service) cachedAvailability(k methodAvailabilityKey) ([]string, bool) {
	s.availMu.Lock()
	defer s.availMu.Unlock()
	e, ok := s.avail[k]
	if !ok || time.Since(e.at) > methodAvailabilityTTL {
		return nil, false
	}
	return append([]string{}, e.ids...), true
}

func (s *Service) storeAvailability(k methodAvailabilityKey, ids []string) {
	s.availMu.Lock()
	defer s.availMu.Unlock()
	if s.avail == nil {
		s.avail = make(map[methodAvailabilityKey]methodAvailabilityEntry)
	}
	// The purchase path asks with the sale's exact amount, so the key space is
	// open-ended (every amount a buyer pays). Prune expired entries before the
	// map grows with them; if that is not enough, drop the lot — a cache miss
	// only costs one CHIP round trip.
	if len(s.avail) >= availabilityCacheMax {
		now := time.Now()
		for key, e := range s.avail {
			if now.Sub(e.at) > methodAvailabilityTTL {
				delete(s.avail, key)
			}
		}
		if len(s.avail) >= availabilityCacheMax {
			s.avail = make(map[methodAvailabilityKey]methodAvailabilityEntry)
		}
	}
	s.avail[k] = methodAvailabilityEntry{ids: append([]string{}, ids...), at: time.Now()}
}

// FPXAvailability returns which FPX identifiers the brand actually lists for this
// currency and amount, restricted to the two FPX products. known=false means CHIP
// could not be asked (no credentials, a decrypt failure, an API error) and the
// caller must fall back to the whitelist rather than hide rows.
//
// An EMPTY ids slice with known=true is a real answer: the brand lists neither
// FPX product for that currency/amount, so neither row may be offered.
func (s *Service) FPXAvailability(ctx context.Context, accountID string, ov *ChipOverride, currency string, amountSen int64) (ids []string, known bool) {
	if s == nil || s.Chip == nil {
		return nil, false
	}
	brand, secretEnc := "", ""
	if ov != nil && ov.BrandID != "" && ov.SecretEnc != "" {
		brand, secretEnc = ov.BrandID, ov.SecretEnc
	} else if s.Accounts != nil && accountID != "" {
		acc, err := s.Accounts.ByID(ctx, accountID)
		if err != nil || acc == nil {
			return nil, false
		}
		brand, secretEnc = acc.ChipBrandID, acc.ChipSecretKeyEnc
	}
	if brand == "" || secretEnc == "" {
		return nil, false
	}
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		return nil, false
	}
	key := methodAvailabilityKey{brandID: brand, currency: currency, amount: amountSen}
	if cached, ok := s.cachedAvailability(key); ok {
		return cached, true
	}
	secret, err := cryptoutil.Decrypt(s.DecryptKey, secretEnc)
	if err != nil {
		return nil, false
	}
	callCtx, cancel := context.WithTimeout(ctx, availabilityTimeout)
	defer cancel()
	avail, err := s.Chip.PaymentMethods(callCtx, string(secret), currency, "", amountSen)
	if err != nil {
		// Not cached: a transient outage must not hide the rows for a TTL.
		return nil, false
	}
	out := make([]string, 0, 2)
	for _, id := range avail {
		switch strings.ToLower(strings.TrimSpace(id)) {
		case PayMethodFPX:
			out = append(out, PayMethodFPX)
		case PayMethodFPXB2B1:
			out = append(out, PayMethodFPXB2B1)
		}
	}
	s.storeAvailability(key, out)
	return out, true
}

// CheckoutPayOptions is the picker's own resolution: the merchant's effective
// whitelist (a form's CHIP override wins over the account's), the groups that
// enables, MINUS the FPX row(s) the brand does not actually list at CHIP.
//
// It is the function both buyer surfaces call, so a row can never be rendered by
// one of them and refused by the other.
func (s *Service) CheckoutPayOptions(ctx context.Context, accountID string, ov *ChipOverride) []PayOption {
	wl, currency := s.effectiveWhitelistAndCurrency(ctx, accountID, ov)
	return s.dropUnlistedFPX(ctx, accountID, ov, currency, availabilityProbeAmountSen, PayOptions(wl))
}

// effectiveWhitelistAndCurrency reads the account once for BOTH facts the picker
// needs: the whitelist that applies (unless a form override replaces it) and the
// currency CHIP's method list is filtered by. On a read failure it answers the
// unrestricted whitelist, which is what the picker rendered before the override
// existed and what an unreadable setting already resolved to.
func (s *Service) effectiveWhitelistAndCurrency(ctx context.Context, accountID string, ov *ChipOverride) ([]string, string) {
	currency := ""
	if s.Accounts != nil && accountID != "" {
		if a, err := s.Accounts.ByID(ctx, accountID); err == nil && a != nil {
			currency = a.Currency
			if ov != nil && ov.BrandID != "" && ov.SecretEnc != "" {
				return ov.MethodWhitelist, currency
			}
			return a.ChipMethodWhitelist, currency
		}
	}
	if ov != nil && ov.BrandID != "" && ov.SecretEnc != "" {
		return ov.MethodWhitelist, currency
	}
	return nil, currency
}

// dropUnlistedFPX removes the FPX row(s) CHIP does not list, leaving every other
// group untouched. An unknown listing changes nothing (see FPXAvailability).
func (s *Service) dropUnlistedFPX(ctx context.Context, accountID string, ov *ChipOverride, currency string, amountSen int64, opts []PayOption) []PayOption {
	hasFPX := false
	for _, o := range opts {
		if IsFPXPayMethod(o.Method) {
			hasFPX = true
			break
		}
	}
	if !hasFPX {
		return opts
	}
	avail, known := s.FPXAvailability(ctx, accountID, ov, currency, amountSen)
	if !known {
		return opts
	}
	listed := make(map[string]bool, len(avail))
	for _, id := range avail {
		listed[id] = true
	}
	out := make([]PayOption, 0, len(opts))
	for _, o := range opts {
		if IsFPXPayMethod(o.Method) && !listed[o.Method] {
			continue
		}
		out = append(out, o)
	}
	return out
}

// FPXListed reports whether this checkout may offer/accept one FPX product right
// now. known=false means "CHIP could not be asked", and the caller keeps the
// whitelist's answer.
func (s *Service) FPXListed(ctx context.Context, accountID string, ov *ChipOverride, currency string, amountSen int64, method string) (listed, known bool) {
	avail, known := s.FPXAvailability(ctx, accountID, ov, currency, amountSen)
	if !known {
		return false, false
	}
	for _, id := range avail {
		if id == method {
			return true, true
		}
	}
	return false, true
}
