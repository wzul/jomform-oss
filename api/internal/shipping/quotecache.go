// One buyer intent, one quotation.
//
// WHY THIS EXISTS. Neither provider offers an idempotency key: a Lalamove
// quotation is created fresh on every call and expires (5 minutes per its docs),
// and EasyParcel rate-checks are billed against a call budget. A buyer who
// double-clicks Submit, or a browser that retries a slow POST, therefore asks two
// questions and gets two different quotations for ONE intent — and, once a
// merchant starts placing orders, two bookings.
//
// The guard is deliberately NARROW: an identical quote request (same workspace,
// same provider, same two stops, same parcel) made while the previous quotation
// is still valid reuses that quotation instead of issuing another. Nothing about
// money, sales or payment changes — the fee the buyer is charged is the same
// figure, produced once.
//
// IT IS PER-PROCESS. A second instance would issue its own quotation; the
// quotation ids are recorded on the sale, so a duplicate is visible there rather
// than invisible. Making it cross-process is a cache (Valkey) concern, not a
// correctness one, and is deliberately NOT done here.
package shipping

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// QuoteCacheTTL caps how long a cached quotation is reused. A quotation's own
// expiry wins when the provider supplies one (Lalamove does; EasyParcel does not).
const QuoteCacheTTL = 5 * time.Minute

// quoteCacheMaxBounds the map so a busy form cannot grow it without limit.
const quoteCacheMax = 512

type cachedQuote struct {
	quote   ProviderQuote
	expires time.Time
}

// QuoteCache memoises provider quotations keyed by the full request fingerprint.
// A nil cache is usable (every method is a no-op), so a caller never has to
// branch on it.
type QuoteCache struct {
	mu      sync.Mutex
	entries map[string]cachedQuote
	now     func() time.Time
	max     int
}

// NewQuoteCache builds an empty cache.
func NewQuoteCache() *QuoteCache {
	return &QuoteCache{entries: map[string]cachedQuote{}, now: time.Now, max: quoteCacheMax}
}

// key fingerprints a quote request. Every field a provider's price depends on is
// in it, so two requests share a quotation only when they would get the same
// answer.
func (c *QuoteCache) key(req QuoteRequest) string {
	parts := []string{
		strings.TrimSpace(req.AccountID),
		Normalize(req.ServiceType),
		strings.TrimSpace(req.Country),
		addrKey(req.Origin), strings.TrimSpace(req.OriginLat), strings.TrimSpace(req.OriginLng),
		addrKey(req.Destination), strings.TrimSpace(req.DestinationLat), strings.TrimSpace(req.DestinationLng),
		strconv.FormatInt(req.WeightG, 10),
		strconv.FormatInt(req.ParcelValueSen, 10),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:16])
}

// addrKey renders an address as comparable text.
func addrKey(a Address) string {
	parts := []string{Normalize(a.State), Normalize(a.District), Normalize(a.Postcode)}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

// Get returns a still-valid cached quotation for the request.
func (c *QuoteCache) Get(req QuoteRequest) (ProviderQuote, bool) {
	if c == nil {
		return ProviderQuote{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[c.key(req)]
	if !ok {
		return ProviderQuote{}, false
	}
	if !c.now().Before(e.expires) {
		delete(c.entries, c.key(req))
		return ProviderQuote{}, false
	}
	return e.quote, true
}

// Put stores a quotation. The entry lives until the quotation's own expiry, or
// QuoteCacheTTL when the provider did not state one.
func (c *QuoteCache) Put(req QuoteRequest, q ProviderQuote) {
	if c == nil {
		return
	}
	now := c.now()
	expires := now.Add(QuoteCacheTTL)
	if !q.ExpiresAt.IsZero() && q.ExpiresAt.Before(expires) {
		expires = q.ExpiresAt
	}
	if !expires.After(now) {
		// Already expired: caching it would hand a dead quotation to the next
		// submission.
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.max > 0 && len(c.entries) >= c.max {
		// Drop the oldest entries first (map iteration order is random, so this
		// prunes anything expired before falling back to an arbitrary eviction).
		for k, e := range c.entries {
			if !now.Before(e.expires) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= c.max {
			for k := range c.entries {
				delete(c.entries, k)
				break
			}
		}
	}
	c.entries[c.key(req)] = cachedQuote{quote: q, expires: expires}
}

// Len reports how many entries the cache holds (tests/diagnostics).
func (c *QuoteCache) Len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}
