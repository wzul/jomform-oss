// RFC 7505 null-MX tests (card t_f5b4d28a).
//
// A null MX — `example.com. IN MX 0 .` — is a domain stating, in DNS, that it
// accepts NO mail. Go's resolver surfaces it as ONE record with Host "." and
// Pref 0, so the old `len(records) > 0` test scored it as "accepts mail": a
// registration for anything@example.com passed the MX pre-check, the user row
// was created, and the verification email was a guaranteed hard bounce against
// the identity every receipt and login link shares.
//
// Every assertion below swaps lookupRaw (the package seam) rather than touching
// the network, because a test that depends on live DNS proves what the resolver
// said that minute, not what the code does with the answer.
package email

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
)

// swapLookup installs a fake resolver for one test and restores the previous
// one. lookupRaw is a package-level var shared by every test in this package
// (TestMXCheckerCaches swaps it too), so a swap that is not restored leaks into
// whatever test the runner schedules next.
func swapLookup(t *testing.T, fn func(string) ([]*net.MX, error)) {
	t.Helper()
	prev := lookupRaw
	lookupRaw = fn
	t.Cleanup(func() { lookupRaw = prev })
}

// mxHosts renders an MX set readably ("\".\" (pref 0)") — printing []*net.MX
// dumps heap addresses, which says nothing about which record failed.
func mxHosts(records []*net.MX) string {
	out := "["
	for i, r := range records {
		if i > 0 {
			out += " "
		}
		if r == nil {
			out += "<nil>"
			continue
		}
		out += "\"" + r.Host + "\" (pref " + strconv.Itoa(int(r.Pref)) + ")"
	}
	return out + "]"
}

// TestMXCheckerNullMXAcceptsNoMail is the regression test: a null MX must be
// reported as (false, nil) — a definitive "accepts no mail", not an error (the
// caller treats an error the same way, but nil keeps the answer cacheable and
// distinguishable from "could not resolve") — and it must be cached as the
// existing negative entry so it is not re-resolved on every registration.
func TestMXCheckerNullMXAcceptsNoMail(t *testing.T) {
	rdb := breakerTestClient(t)

	for _, tc := range []struct {
		name    string
		slug    string
		records []*net.MX
	}{
		{
			name: "null MX as Go resolves it (Host \".\", Pref 0)",
			slug: "dot-host",
			records: []*net.MX{
				{Host: ".", Pref: 0},
			},
		},
		{
			name: "null MX with an empty exchange (some resolvers)",
			slug: "empty-host",
			records: []*net.MX{
				{Host: "", Pref: 0},
			},
		},
		{
			name: "no usable exchange at all (defensive: two null-ish records)",
			slug: "all-null",
			records: []*net.MX{
				{Host: ".", Pref: 0},
				{Host: "", Pref: 10},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := testMXChecker(t, rdb)
			ctx := context.Background()
			domain := "null-mx-" + tc.slug + ".example"

			var lookups atomic.Int32
			swapLookup(t, func(_ string) ([]*net.MX, error) {
				lookups.Add(1)
				return tc.records, nil
			})

			ok, err := m.Check(ctx, domain)
			if err != nil {
				t.Fatalf("Check(%q) error = %v, want nil: a null MX is a definitive 'accepts no mail', not a lookup failure", domain, err)
			}
			if ok {
				t.Fatalf("Check(%q) = accepts mail, want accepts no mail: %s is the RFC 7505 null MX", domain, mxHosts(tc.records))
			}
			if got := lookups.Load(); got != 1 {
				t.Fatalf("lookups = %d, want 1", got)
			}

			// Cached as the negative entry, like the no-MX case (1h TTL).
			key := m.mxKey(domain)
			val, err := rdb.Get(ctx, key).Result()
			if err != nil {
				t.Fatalf("negative cache entry missing for %q: %v", domain, err)
			}
			if val != mxNegVal {
				t.Fatalf("cached value = %q, want %q", val, mxNegVal)
			}
			if ttl := rdb.TTL(ctx, key).Val(); ttl <= 0 || ttl > mxNegativeTTL {
				t.Fatalf("cached TTL = %v, want (0, %v]", ttl, mxNegativeTTL)
			}

			// Served from the cache: a second Check answers "no mail" without
			// asking DNS again, even if the fake resolver would now say yes.
			swapLookup(t, func(_ string) ([]*net.MX, error) {
				lookups.Add(1)
				return []*net.MX{{Host: "mx.example.net.", Pref: 10}}, nil
			})
			ok, err = m.Check(ctx, domain)
			if err != nil || ok {
				t.Fatalf("cached Check(%q) = (%v, %v), want (false, nil)", domain, ok, err)
			}
			if got := lookups.Load(); got != 1 {
				t.Fatalf("cached Check re-resolved DNS: lookups = %d, want 1", got)
			}
		})
	}
}

// TestMXCheckerRealRecordsStillAcceptMail is the other half of the guard: the
// fix must not turn a domain that CAN receive mail into one that cannot.
func TestMXCheckerRealRecordsStillAcceptMail(t *testing.T) {
	rdb := breakerTestClient(t)

	for _, tc := range []struct {
		name    string
		slug    string
		records []*net.MX
	}{
		{
			name: "ordinary two-MX set",
			slug: "two-mx",
			records: []*net.MX{
				{Host: "mx1.example.net.", Pref: 10},
				{Host: "mx2.example.net.", Pref: 20},
			},
		},
		{
			name: "a null record alongside a usable exchange",
			slug: "null-plus-real",
			records: []*net.MX{
				{Host: ".", Pref: 0},
				{Host: "mx1.example.net.", Pref: 10},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := testMXChecker(t, rdb)
			ctx := context.Background()
			domain := "real-mx-" + tc.slug + ".example"

			swapLookup(t, func(_ string) ([]*net.MX, error) {
				return tc.records, nil
			})

			ok, err := m.Check(ctx, domain)
			if err != nil {
				t.Fatalf("Check(%q) error = %v, want nil", domain, err)
			}
			if !ok {
				t.Fatalf("Check(%q) = accepts no mail, want accepts mail: %s has a usable exchange", domain, mxHosts(tc.records))
			}
			if val, err := rdb.Get(ctx, m.mxKey(domain)).Result(); err != nil || val == mxNegVal {
				t.Fatalf("positive result not cached as positive: val=%q err=%v", val, err)
			}
		})
	}
}

// TestMXCheckerNullMXWithoutCache pins the same verdict when no cache is wired
// (NewMXChecker(nil)), so the fix does not live in the caching path.
func TestMXCheckerNullMXWithoutCache(t *testing.T) {
	swapLookup(t, func(_ string) ([]*net.MX, error) {
		return []*net.MX{{Host: ".", Pref: 0}}, nil
	})
	m := NewMXChecker(nil)
	ok, err := m.Check(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("Check error = %v, want nil", err)
	}
	if ok {
		t.Fatal("Check = accepts mail for a null MX, want accepts no mail")
	}
}

// TestMXCheckerLookupErrorStillFailsClosed: the fail-closed posture for a
// lookup error is the part of Check that must NOT change — an unprovable domain
// is an error (which registration rejects), never a silent "accepts mail".
func TestMXCheckerLookupErrorStillFailsClosed(t *testing.T) {
	swapLookup(t, func(_ string) ([]*net.MX, error) {
		return nil, errors.New("no such host")
	})
	m := NewMXChecker(nil)
	ok, err := m.Check(context.Background(), "unresolvable.example")
	if err == nil {
		t.Fatal("Check error = nil, want the lookup error passed through")
	}
	if ok {
		t.Fatal("Check = accepts mail on a lookup error, want fail closed")
	}
}

// TestMXCheckerLiveExampleComNullMX is the live arm: example.com publishes a
// null MX as a matter of record, so the real resolver should show (false, nil).
// It is NOT the only assertion (the swapped-lookup tests above are), and it
// skips — loudly — when this host cannot see the published record, so a network
// blip or a hijacking resolver does not turn into a red suite with a misleading
// message.
func TestMXCheckerLiveExampleComNullMX(t *testing.T) {
	records, err := net.LookupMX("example.com")
	if err != nil {
		t.Skipf("live arm skipped: cannot resolve example.com MX here: %v", err)
	}
	if len(records) != 1 || (records[0].Host != "." && records[0].Host != "") {
		t.Skipf("live arm skipped: this resolver does not return the published null MX for example.com: %s", mxHosts(records))
	}
	t.Logf("live example.com MX: %s", mxHosts(records))

	m := NewMXChecker(nil)
	ok, err := m.Check(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("Check(example.com) error = %v, want nil", err)
	}
	if ok {
		t.Fatal("Check(example.com) = accepts mail, want accepts no mail (null MX)")
	}
}
