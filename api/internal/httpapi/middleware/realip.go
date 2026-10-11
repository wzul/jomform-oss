// The trusted-proxy registry: which addresses are the Cloudflare edge.
//
// The client-address trust decision itself lives in clientip.go — the
// forwarding headers are read in exactly one place (TrustedProxy.resolveClientIP)
// and only when the socket peer is a hop the request can arrive through.
// IP ranges are fetched ONCE at startup and refreshed every 12h.
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

// TrustedProxy holds the Cloudflare edge ranges, the one PUBLIC address class we
// treat as a hop rather than as the client.
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

// cloudflareEdge reports whether ip is inside Cloudflare's published ranges.
// Callers reach it only through proxyHop, which also accepts the container
// network — an edge address is the ONE public address a request may arrive
// through, so it is never returned to a caller as "the client".
func (t *TrustedProxy) cloudflareEdge(ip string) bool {
	t.ensure()
	t.mu.RLock()
	defer t.mu.RUnlock()
	return inRanges(t.cidrs, ip)
}

// JSON debug: current Cloudflare range count (health tooling).
func (t *TrustedProxy) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.cidrs)
}

var _ = json.Marshal
