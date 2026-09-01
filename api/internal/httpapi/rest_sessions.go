// Session→account resolution cache: a small in-process TTL cache that lets
// repeat authenticated requests skip the Accounts.ByOwner Postgres query in
// resolve(). Only account IDs are cached — never Account objects — so CHIP
// credential or account-detail changes are never served stale; a cache hit
// still re-fetches the account by primary key (Accounts.ByID), one cheap
// query. The cache is invalidated on logout (see AuthHandlers.handleLogout).
package httpapi

import (
	"sync"
	"time"
)

const (
	// sessionAcctCacheTTL is how long a session→account-ID mapping stays hot.
	sessionAcctCacheTTL = 60 * time.Second
	// sessionAcctCacheMaxEntries bounds memory use; beyond this, soonest-
	// expiring (oldest) entries are dropped first.
	sessionAcctCacheMaxEntries = 5000
)

// sessionAcctEntry is one cached token→account-ID-list mapping.
type sessionAcctEntry struct {
	accountIDs []string
	expires    time.Time
}

// sessionAcctCache is a mutex-guarded map with per-entry expiry. It lives at
// package level so both AuthHandlers (logout invalidation) and RESTHandlers
// (resolve) share one process-wide cache regardless of wiring.
type sessionAcctCache struct {
	mu      sync.Mutex
	entries map[string]sessionAcctEntry
	ttl     time.Duration
	maxSize int
}

// defaultSessionAcctCache is the process-wide cache used by resolve/logout.
var defaultSessionAcctCache = newSessionAcctCache()

// newSessionAcctCache builds a cache with the package defaults.
func newSessionAcctCache() *sessionAcctCache {
	return &sessionAcctCache{
		entries: make(map[string]sessionAcctEntry),
		ttl:     sessionAcctCacheTTL,
		maxSize: sessionAcctCacheMaxEntries,
	}
}

// get returns the cached account IDs for a token, if present and unexpired.
func (c *sessionAcctCache) get(token string) ([]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[token]
	if !ok {
		return nil, false
	}
	if time.Now().After(e.expires) {
		delete(c.entries, token)
		return nil, false
	}
	return e.accountIDs, true
}

// put stores the account IDs for a token, evicting when over capacity.
func (c *sessionAcctCache) put(token string, accountIDs []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictLocked()
	c.entries[token] = sessionAcctEntry{
		accountIDs: accountIDs,
		expires:    time.Now().Add(c.ttl),
	}
}

// delete drops a token's cached mapping (logout / stale-entry fallback).
func (c *sessionAcctCache) delete(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, token)
}

// evictLocked makes room for one more entry: expired entries go first, then
// the soonest-expiring (oldest) entries until under the size cap.
func (c *sessionAcctCache) evictLocked() {
	if len(c.entries) < c.maxSize {
		return
	}
	now := time.Now()
	for k, e := range c.entries {
		if now.After(e.expires) {
			delete(c.entries, k)
		}
	}
	for len(c.entries) >= c.maxSize {
		oldestKey := ""
		var oldest time.Time
		found := false
		for k, e := range c.entries {
			if !found || e.expires.Before(oldest) {
				oldestKey, oldest, found = k, e.expires, true
			}
		}
		if !found {
			return
		}
		delete(c.entries, oldestKey)
	}
}

// cachedSessionAcctIDs returns the cached account-ID list for a token.
func cachedSessionAcctIDs(token string) ([]string, bool) {
	return defaultSessionAcctCache.get(token)
}

// storeSessionAcctIDs caches the resolved account-ID list for a token.
func storeSessionAcctIDs(token string, accountIDs []string) {
	defaultSessionAcctCache.put(token, accountIDs)
}

// invalidateSessionAcctCache drops the cached mapping for a token; called on
// logout so a destroyed session never resolves from cache afterward.
func invalidateSessionAcctCache(token string) {
	defaultSessionAcctCache.delete(token)
}
