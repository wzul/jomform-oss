// MX pre-check (issue #56): before a verification email is sent we resolve the
// recipient domain's MX records; a domain that accepts no mail is rejected at
// registration (400) BEFORE the user row is created. Lookups are cached in
// Valkey (positive 24h, negative 1h) to stay resilient to DNS flapping.
package email

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const (
	mxCachePrefix = "jf:mx:"
	mxPositiveTTL = 24 * time.Hour
	mxNegativeTTL = 1 * time.Hour
	mxNegVal      = "none"
)

// MXChecker resolves MX records for a domain and caches the result.
type MXChecker struct {
	rdb *goredis.Client
}

// NewMXChecker wires the checker over the same Valkey client as sessions.
// A nil client disables caching (lookups still run).
func NewMXChecker(rdb *goredis.Client) *MXChecker {
	return &MXChecker{rdb: rdb}
}

// DomainOf extracts the domain from an email address ("a@b.com" → "b.com").
func DomainOf(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(email[at+1:]))
}
func mxKey(domain string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(domain)))
	return mxCachePrefix + hex.EncodeToString(sum[:])
}

// lookupRaw is a package-level var so tests can intercept real DNS. Production
// uses net.LookupMX inside a timeout goroutine (net.LookupMX has no context
// variant); a lookup error is treated as "cannot prove accepts mail".
var lookupRaw = func(domain string) ([]*net.MX, error) {
	return net.LookupMX(domain)
}

// lookupRawCtx wraps the swappable lookupRaw with a caller-supplied timeout.
func lookupRawCtx(ctx context.Context, domain string) ([]*net.MX, error) {
	done := make(chan struct{})
	var recs []*net.MX
	var err error
	go func() {
		recs, err = lookupRaw(domain)
		close(done)
	}()
	select {
	case <-done:
		return recs, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Check reports whether a domain accepts mail. It consults the Valkey cache
// first, falling back to a live net.LookupMX. A domain is considered valid when
// it has at least one MX record. Errors (DNS failures, cache errors) are
// returned so the caller can decide whether to fail closed (safe) — rejecting
// a domain we cannot verify is the conservative reputation posture.
func (m *MXChecker) Check(ctx context.Context, domain string) (bool, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return false, fmt.Errorf("empty domain")
	}

	// Cache hit?
	if m.rdb != nil {
		cached, err := m.rdb.Get(ctx, mxKey(domain)).Result()
		if err == nil {
			return cached != mxNegVal, nil
		}
		if err != goredis.Nil {
			// cache read error — fall through to live lookup, cache write best-effort
		}
	}

	records, err := lookupRawCtx(ctx, domain)
	if err == nil && len(records) > 0 {
		m.cacheSet(ctx, domain, "1", mxPositiveTTL)
		return true, nil
	}
	// No MX records (net.NoSuchHost or empty) → negative cache.
	neg := err != nil // a lookup error = we can't prove it accepts mail
	if len(records) == 0 {
		neg = true
	}
	if neg {
		m.cacheSet(ctx, domain, mxNegVal, mxNegativeTTL)
	}
	if err == nil && len(records) == 0 {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("mx lookup %q: %w", domain, err)
	}
	return false, nil
}

// cacheSet writes a cache entry; failures are non-fatal.
func (m *MXChecker) cacheSet(ctx context.Context, domain, val string, ttl time.Duration) {
	if m.rdb == nil {
		return
	}
	_ = m.rdb.Set(ctx, mxKey(domain), val, ttl).Err()
}
