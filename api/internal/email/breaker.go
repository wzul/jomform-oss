// Verification circuit breaker (issue #56): guards SES reputation from a
// targeted bounce attack. Verification sends and bounce events are counted in
// per-hour Valkey buckets; when the bounce ratio in the trailing hour exceeds a
// threshold (default 5%) past a minimum sample (default 20 sends), verification
// email is paused. Receipts are deliberately NOT gated by this breaker.
package email

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const (
	// BreakerKeyPrefix and BreakerKeyTTL bound each hour bucket.
	BreakerKeyPrefix    = "jf:bnc:"
	BreakerKeyTTL       = 2 * time.Hour
	BreakerDefaultMin   = 20                              // min sends before ratio counts
	BreakerDefaultRatio = 0.05                            // >5% bounces pauses verification
	BreakerOpenMessage  = "banyak emel tidak dihantar, sila cuba lagi nanti" // 503 body
)

// Breaker tracks verification sends + bounces in Valkey hourly buckets and
// reports whether verification mail should be paused.
type Breaker struct {
	rdb      *goredis.Client
	minSends int64
	ratio    float64
}

// NewBreaker wires the breaker over the same Valkey client that backs
// sessions. minSends<=0 falls back to BreakerDefaultMin; ratio<=0 falls back
// to BreakerDefaultRatio.
func NewBreaker(rdb *goredis.Client, minSends int64, ratio float64) *Breaker {
	if minSends <= 0 {
		minSends = BreakerDefaultMin
	}
	if ratio <= 0 {
		ratio = BreakerDefaultRatio
	}
	return &Breaker{rdb: rdb, minSends: minSends, ratio: ratio}
}

// hour returns the current UTC bucket label, e.g. "20260902T10".
func hour(t time.Time) string { return t.UTC().Format("20060102T15") }

// sendKey / bounceKey namespace the per-hour counters.
func sendKey(h string) string   { return BreakerKeyPrefix + "send:" + h }
func bounceKey(h string) string { return BreakerKeyPrefix + "bounce:" + h }

// RecordSend counts a verification email sent in the current hour.
func (b *Breaker) RecordSend(ctx context.Context) error {
	now := time.Now()
	pipe := b.rdb.Pipeline()
	pipe.Incr(ctx, sendKey(hour(now)))
	pipe.Expire(ctx, sendKey(hour(now)), BreakerKeyTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("breaker record send: %w", err)
	}
	return nil
}

// RecordBounceCount adds n bounces in the current hour.
func (b *Breaker) RecordBounceCount(ctx context.Context, n int64) error {
	now := time.Now()
	key := bounceKey(hour(now))
	pipe := b.rdb.Pipeline()
	pipe.IncrBy(ctx, key, n)
	pipe.Expire(ctx, key, BreakerKeyTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("breaker record bounce: %w", err)
	}
	return nil
}

// Tripped reports whether verification mail must be paused: the bounce ratio
// across the current + previous hour buckets exceeds the threshold given at
// least minSends total sends. Because each bucket expires after BreakerKeyTTL,
// a quiet hour (no new sends or bounces) naturally drops the ratio back to zero
// — the breaker auto-resets after 1 quiet hour without extra bookkeeping.
//
// On Valkey failure it fails OPEN (sends allowed) for availability, mirroring
// the login-limiter behaviour: a broken cache must never take down a healthy
// identity's verification flow permanently.
func (b *Breaker) Tripped(ctx context.Context) (bool, error) {
	now := time.Now()
	h0, h1 := hour(now), hour(now.Add(-1*time.Hour)) // current + previous bucket
	cmd := b.rdb.MGet(ctx, sendKey(h0), sendKey(h1), bounceKey(h0), bounceKey(h1))
	res, err := cmd.Result()
	if err != nil {
		return false, fmt.Errorf("breaker read: %w", err)
	}
	var sends, bounces int64
	for i, v := range res {
		if v == nil {
			continue
		}
		vv, ok := v.(string)
		if !ok {
			continue
		}
		var n int64
		// keys 0,1 are sends; keys 2,3 are bounces
		if i < 2 {
			_, _ = fmt.Sscanf(vv, "%d", &n)
			sends += n
		} else {
			_, _ = fmt.Sscanf(vv, "%d", &n)
			bounces += n
		}
	}
	if sends < b.minSends {
		return false, nil
	}
	return float64(bounces)/float64(sends) > b.ratio, nil
}
