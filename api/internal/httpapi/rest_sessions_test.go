// Tests for the session→account resolution cache (rest_sessions.go).
package httpapi

import (
	"testing"
	"time"
)

// newTestSessionAcctCache builds a small short-TTL cache for tests that are ABOUT
// expiry: the TTL has to be short enough to observe a miss after a sleep.
func newTestSessionAcctCache(maxSize int) *sessionAcctCache {
	return &sessionAcctCache{
		entries: make(map[string]sessionAcctEntry),
		ttl:     60 * time.Millisecond,
		maxSize: maxSize,
	}
}

// newOrderTestSessionAcctCache builds the same cache with a TTL no entry can reach
// inside a test (card t_42c49ef9).
//
// THE TESTS ABOUT ORDER ARE NOT ABOUT EXPIRY, and sharing the 60 ms TTL made their
// verdict depend on how long the host took to schedule them. Measured: under load a
// 60 ms window is not reliably longer than the test's own three 3 ms sleeps plus the
// scheduling around them, so an entry legitimately expired in the middle of the
// assertions and the test failed with `expected entry "b" to survive` — a failure
// about the host's clock resolution, not about the cache. The staggering the eviction
// order needs is a few MILLISECONDS (time.Now() has nanosecond resolution on Linux), so
// a long TTL removes the expiry channel entirely and leaves the ordering signal the test
// exists to check.
//
// The ORDER is still genuinely asserted: `evictLocked` picks the soonest-expiring entry,
// so with a long TTL the only thing separating the entries is when each was put.
func newOrderTestSessionAcctCache(maxSize int) *sessionAcctCache {
	return &sessionAcctCache{
		entries: make(map[string]sessionAcctEntry),
		ttl:     time.Hour,
		maxSize: maxSize,
	}
}

func TestSessionAcctCacheRoundTrip(t *testing.T) {
	c := newTestSessionAcctCache(10)
	if _, ok := c.get("tok"); ok {
		t.Fatal("expected miss on empty cache")
	}
	c.put("tok", []string{"acct-1", "acct-2"})
	ids, ok := c.get("tok")
	if !ok {
		t.Fatal("expected hit after put")
	}
	if len(ids) != 2 || ids[0] != "acct-1" || ids[1] != "acct-2" {
		t.Fatalf("unexpected ids: %v", ids)
	}
}

func TestSessionAcctCacheExpiry(t *testing.T) {
	c := newTestSessionAcctCache(10)
	c.put("tok", []string{"acct-1"})
	if _, ok := c.get("tok"); !ok {
		t.Fatal("expected hit before expiry")
	}
	time.Sleep(80 * time.Millisecond)
	if _, ok := c.get("tok"); ok {
		t.Fatal("expected miss after TTL expiry")
	}
}

func TestSessionAcctCacheDelete(t *testing.T) {
	c := newTestSessionAcctCache(10)
	c.put("tok", []string{"acct-1"})
	c.delete("tok")
	if _, ok := c.get("tok"); ok {
		t.Fatal("expected miss after delete")
	}
}

func TestSessionAcctCacheEviction(t *testing.T) {
	c := newOrderTestSessionAcctCache(3)
	c.put("a", []string{"1"})
	time.Sleep(3 * time.Millisecond) // ensure staggered expiry times
	c.put("b", []string{"2"})
	time.Sleep(3 * time.Millisecond)
	c.put("c", []string{"3"})
	// Inserting a 4th entry evicts the soonest-expiring entry ("a").
	c.put("d", []string{"4"})
	if _, ok := c.get("a"); ok {
		t.Fatal("expected oldest entry 'a' to be evicted")
	}
	for _, k := range []string{"b", "c", "d"} {
		if _, ok := c.get(k); !ok {
			t.Fatalf("expected entry %q to survive", k)
		}
	}
}

func TestSessionAcctCacheOverwriteRefreshesExpiry(t *testing.T) {
	c := newOrderTestSessionAcctCache(10)
	c.put("a", []string{"1"})
	time.Sleep(3 * time.Millisecond)
	c.put("b", []string{"2"})
	time.Sleep(3 * time.Millisecond)
	c.put("a", []string{"1"}) // refresh: "a" now expires after "b"
	// Fill to the 10-entry cap.
	for _, k := range []string{"c", "d", "e", "f", "g", "h", "i", "j"} {
		c.put(k, []string{k})
	}
	c.put("k", []string{"k"}) // forces one eviction of the soonest-expiring
	if _, ok := c.get("b"); ok {
		t.Fatal("expected 'b' (never refreshed) to be evicted")
	}
	if _, ok := c.get("a"); !ok {
		t.Fatal("expected refreshed entry 'a' to survive")
	}
	if len(c.entries) > 10 {
		t.Fatalf("cache exceeded max size: %d", len(c.entries))
	}
}

func TestInvalidateUnknownTokenIsNoop(t *testing.T) {
	defaultSessionAcctCache = newSessionAcctCache()
	invalidateSessionAcctCache("never-cached")
	if len(defaultSessionAcctCache.entries) != 0 {
		t.Fatalf("expected empty cache, got %d entries", len(defaultSessionAcctCache.entries))
	}
}
