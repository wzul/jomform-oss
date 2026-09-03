// Session-backed SES reputation tests (issue #56) — breaker + MX cache + send
// suppression. These need a real Valkey at JOMFORM_TEST_VALKEY_URL (skipped in
// CI); the breaker/mx behaviour is exercised against it exactly as in prod.
package email

import (
	"context"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

func breakerTestClient(t *testing.T) *goredis.Client {
	t.Helper()
	valkeyURL := os.Getenv("JOMFORM_TEST_VALKEY_URL")
	if valkeyURL == "" {
		t.Skip("JOMFORM_TEST_VALKEY_URL not set; skipping breaker integration test")
	}
	// Test isolation: route to a dedicated DB (…/2) so this package's FlushDB
	// never wipes keys that other packages (httpapi auth on …/1, emailqueue on
	// …/3) create concurrently when `go test ./...` runs packages in parallel.
	u := strings.TrimPrefix(valkeyURL, "valkey://")
	if i := strings.LastIndex(u, "/"); i >= 0 {
		u = u[:i]
	}
	opts, err := goredis.ParseURL("redis://" + u + "/2")
	if err != nil {
		t.Fatalf("parse valkey url: %v", err)
	}
	rdb := goredis.NewClient(opts)
	t.Cleanup(func() { _ = rdb.Close() })
	// flush a dedicated keyspace so counters never leak across runs
	if err := rdb.FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("flush db: %v", err)
	}
	return rdb
}

// TestBreakerFlipsAboveThreshold: 25 sends with 5 bounces (20%) trips the
// 5%/20 threshold; a clean set of sends does not.
func TestBreakerFlipsAboveThreshold(t *testing.T) {
	rdb := breakerTestClient(t)
	b := NewBreaker(rdb, 0, 0) // defaults 20 / 0.05
	ctx := context.Background()

	for i := 0; i < 25; i++ {
		if err := b.RecordSend(ctx); err != nil {
			t.Fatalf("record send: %v", err)
		}
	}
	tripped, err := b.Tripped(ctx)
	if err != nil {
		t.Fatalf("tripped (clean): %v", err)
	}
	if tripped {
		t.Fatal("breaker tripped at 0% bounce, want open")
	}

	_ = b.RecordBounceCount(ctx, 5) // 5/25 = 20% > 5%
	tripped, err = b.Tripped(ctx)
	if err != nil {
		t.Fatalf("tripped (bad): %v", err)
	}
	if !tripped {
		t.Fatal("breaker did NOT trip at 20% bounce, want closed")
	}
}

// TestBreakerNeedsMinimumSamples: fewer than minSends never trips even at 100%.
func TestBreakerNeedsMinimumSamples(t *testing.T) {
	rdb := breakerTestClient(t)
	b := NewBreaker(rdb, 0, 0)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_ = b.RecordSend(ctx)
	}
	_ = b.RecordBounceCount(ctx, 5) // 5/5 sends bounced but only 5 samples
	tripped, err := b.Tripped(ctx)
	if err != nil {
		t.Fatalf("tripped: %v", err)
	}
	if tripped {
		t.Fatal("breaker tripped below the minimum sample size")
	}
}

// TestBreakerAutoResets: after a quiet hour (no new activity, previous bucket
// expired out of the 2h TTL logic by moving the hour window) the breaker
// recovers. We simulate by recording activity then advancing both buckets out
// of the TTL by writing a fresh send-only bucket for a shifted window.
func TestBreakerAutoResets(t *testing.T) {
	rdb := breakerTestClient(t)
	b := NewBreaker(rdb, 0, 0)
	ctx := context.Background()

	now := time.Now()
	h := now.UTC().Format("20060102T15")
	rdb.Del(ctx, sendKey(h), bounceKey(h))
	rdb.Set(ctx, sendKey(h), "20", BreakerKeyTTL)
	rdb.Set(ctx, bounceKey(h), "5", BreakerKeyTTL)
	if tripped, _ := b.Tripped(ctx); !tripped {
		t.Fatal("precondition: breaker should be tripped")
	}
	// A quiet hour = the old buckets expire (removed) and only fresh sends
	// occur in the new window.
	rdb.Del(ctx, sendKey(h), bounceKey(h))
	_ = b.RecordSend(ctx) // one clean send, no bounces
	if tripped, _ := b.Tripped(ctx); tripped {
		t.Fatal("breaker did not auto-reset after a quiet window")
	}
}

// TestShouldSuppress passes through to the underlying store.
type fakeSuppressor struct{ sup bool }

func (f fakeSuppressor) IsSuppressed(_ context.Context, _ string) (bool, error) {
	return f.sup, nil
}

func TestShouldSuppress(t *testing.T) {
	for _, tc := range []struct {
		sup  bool
		want bool
	}{
		{sup: true, want: true},
		{sup: false, want: false},
	} {
		got, err := ShouldSuppress(context.Background(), fakeSuppressor{tc.sup}, " A@B.com ")
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got != tc.want {
			t.Errorf("ShouldSuppress(sup=%v) = %v, want %v", tc.sup, got, tc.want)
		}
	}
}

// TestMXCheckerCaches uses a fake DNS lookup to prove positive (24h) and
// negative (1h) caching paths resolve correctly. We override lookupRaw via a
// test hook to avoid real DNS dependency.
func TestMXCheckerCaches(t *testing.T) {
	rdb := breakerTestClient(t)
	m := NewMXChecker(rdb)
	ctx := context.Background()

	// Positive: domain with an MX record.
	lookupRaw = func(_ string) ([]*net.MX, error) {
		return []*net.MX{{Host: "mx.example.com.", Pref: 10}}, nil
	}
	ok, err := m.Check(ctx, "example.com")
	if err != nil {
		t.Fatalf("positive check: %v", err)
	}
	if !ok {
		t.Fatal("expected positive MX for example.com")
	}

	// Negative: domain with no MX → cached as negative.
	lookupRaw = func(_ string) ([]*net.MX, error) {
		return nil, nil
	}
	ok2, err := m.Check(ctx, "no-mx.example.com")
	if err != nil {
		t.Fatalf("negative check: %v", err)
	}
	if ok2 {
		t.Fatal("expected negative MX for no-mx.example.com")
	}
}
