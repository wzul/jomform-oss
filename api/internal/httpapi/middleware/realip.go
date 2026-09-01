// Real-IP resolution behind Cloudflare (and other trusted proxies).
// IP ranges are fetched ONCE at startup and refreshed every 12h; direct IPs
// from trusted ranges get replaced by CF-Connecting-IP.
package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// TrustedProxy holds CIDR ranges whose X-Forwarded-For we trust.
type TrustedProxy struct {
	mu      sync.RWMutex
	cidrs   []*net.IPNet
	fetched time.Time
	client  *http.Client
	// Sources are fetched in order until one succeeds.
	Sources []string
}

// NewTrustedProxy builds a proxy list that will fetch on first use.
func NewTrustedProxy() *TrustedProxy {
	return &TrustedProxy{
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

// cloudflareURL is the canonical IPv4+IPv6 ranges list.
const cloudflareURL = "https://www.cloudflare.com/ips-v4"

// FetchCloudflare loads Cloudflare's published IP ranges (v4 then v6).
func (t *TrustedProxy) FetchCloudflare(ctx context.Context) error {
	var all []*net.IPNet
	for _, u := range []string{
		"https://www.cloudflare.com/ips-v4",
		"https://www.cloudflare.com/ips-v6",
	} {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		resp, err := t.client.Do(req)
		if err != nil {
			return fmt.Errorf("fetch %s: %w", u, err)
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		for _, line := range strings.Split(string(body), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if _, cidr, err := net.ParseCIDR(line); err == nil {
				all = append(all, cidr)
			}
		}
	}
	if len(all) == 0 {
		return fmt.Errorf("no cidrs fetched")
	}
	t.mu.Lock()
	t.cidrs = all
	t.fetched = time.Now()
	t.mu.Unlock()
	return nil
}

// ensure periodically refreshes (12h TTL).
func (t *TrustedProxy) ensure() {
	t.mu.RLock()
	ok := len(t.cidrs) > 0 && time.Since(t.fetched) < 12*time.Hour
	t.mu.RUnlock()
	if !ok {
		_ = t.FetchCloudflare(context.Background())
	}
}

// trusted reports whether ip falls inside a known proxy range.
func (t *TrustedProxy) trusted(ip string) bool {
	t.ensure()
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, c := range t.cidrs {
		if c.Contains(parsed) {
			return true
		}
	}
	return false
}

// RealIP middleware: rewrite r.RemoteAddr to the true client IP when the
// immediate peer is a trusted proxy (Cloudflare).
func (t *TrustedProxy) RealIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer := r.RemoteAddr
		if i := strings.LastIndex(peer, ":"); i > 0 && !strings.Contains(peer, "]") {
			peer = peer[:i] // strip port (v4); v6 bracketed kept as-is in net.SplitHostPort below
		}
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			peer = host
		}
		if t.trusted(peer) {
			if cf := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); net.ParseIP(cf) != nil {
				r.RemoteAddr = cf
			} else if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				parts := strings.Split(xff, ",")
				for i := len(parts) - 1; i >= 0; i-- { // rightmost non-trusted
					cand := strings.TrimSpace(parts[i])
					if cand != "" && !t.trusted(cand) {
						r.RemoteAddr = cand
						break
					}
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// JSON debug: current trusted CIDR count (health tooling).
func (t *TrustedProxy) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.cidrs)
}

var _ = json.Marshal
