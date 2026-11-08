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
	// keyPrefix namespaces the cache entries, so a caller can run a checker
	// whose cache no other checker writes or deletes. Defaults to
	// mxCachePrefix (the production value).
	keyPrefix string
}

// NewMXChecker wires the checker over the same Valkey client as sessions.
// A nil client disables caching (lookups still run).
func NewMXChecker(rdb *goredis.Client) *MXChecker {
	return NewMXCheckerWithPrefix(rdb, mxCachePrefix)
}

// NewMXCheckerWithPrefix is NewMXChecker with an explicit cache-key prefix.
//
// The cache entries are plain Redis keys with a fixed production name derived
// from the domain, so two processes sharing a Valkey database — a test run and a
// live API, or two concurrent test runs — read and write the same entries: a
// test asserting a domain is cacheable can be answered by another process's
// entry for that domain, and a delete meant to clear the test's own state also
// clears the live process's cache. A prefix gives the caller a cache of its own.
// An empty prefix falls back to mxCachePrefix.
func NewMXCheckerWithPrefix(rdb *goredis.Client, prefix string) *MXChecker {
	if prefix == "" {
		prefix = mxCachePrefix
	}
	return &MXChecker{rdb: rdb, keyPrefix: prefix}
}

// DomainOf extracts the domain from an email address ("a@b.com" → "b.com").
func DomainOf(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(email[at+1:]))
}
func (m *MXChecker) mxKey(domain string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(domain)))
	return m.keyPrefix + hex.EncodeToString(sum[:])
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
// it has at least one MX record that names a real exchange — a null MX (RFC
// 7505) is an explicit "accepts no mail", not a record like any other. Errors
// (DNS failures, cache errors) are returned so the caller can decide whether to
// fail closed (safe) — rejecting a domain we cannot verify is the conservative
// reputation posture.
func (m *MXChecker) Check(ctx context.Context, domain string) (bool, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return false, fmt.Errorf("empty domain")
	}

	// Cache hit?
	if m.rdb != nil {
		cached, err := m.rdb.Get(ctx, m.mxKey(domain)).Result()
		if err == nil {
			return cached != mxNegVal, nil
		}
		if err != goredis.Nil {
			// cache read error — fall through to live lookup, cache write best-effort
		}
	}

	records, err := lookupRawCtx(ctx, domain)
	if err != nil {
		// A lookup error = we can't prove it accepts mail: negative cache and
		// return the error, so the caller fails closed.
		m.cacheSet(ctx, domain, mxNegVal, mxNegativeTTL)
		return false, fmt.Errorf("mx lookup %q: %w", domain, err)
	}
	if !mxAcceptsMail(records) {
		// No MX records, OR the RFC 7505 null MX — either way the domain has
		// published that it accepts no mail, so cache the negative entry and
		// report it as a definitive answer (no error) so it is not re-resolved.
		m.cacheSet(ctx, domain, mxNegVal, mxNegativeTTL)
		return false, nil
	}
	m.cacheSet(ctx, domain, "1", mxPositiveTTL)
	return true, nil
}

// mxAcceptsMail reports whether a resolved MX set names at least one exchange
// that can receive mail.
//
// RFC 7505 defines the "null MX": a single record, preference 0, exchange "."
// (the root), which is the domain stating in DNS that it accepts NO mail.
// example.com/.net/.org publish exactly that. Go's resolver surfaces it as ONE
// record with Host "." (some resolvers give ""), so asking only whether the
// record list is non-empty scored a null MX as "accepts mail": a registration
// for anything@example.com passed the MX pre-check, the user row was created,
// and the verification email hard-bounced against the identity every receipt
// and login link shares.
//
// A record with the root/empty exchange is therefore skipped rather than
// counted; a set that keeps none is "accepts no mail". (RFC 7505 requires the
// null MX to be alone, so the mixed case is defensive: if any usable exchange
// is published, mail is routable.)
func mxAcceptsMail(records []*net.MX) bool {
	for _, r := range records {
		if r == nil {
			continue
		}
		host := strings.TrimSpace(r.Host)
		if host == "" || host == "." {
			continue
		}
		return true
	}
	return false
}

// cacheSet writes a cache entry; failures are non-fatal.
func (m *MXChecker) cacheSet(ctx context.Context, domain, val string, ttl time.Duration) {
	if m.rdb == nil {
		return
	}
	_ = m.rdb.Set(ctx, m.mxKey(domain), val, ttl).Err()
}
