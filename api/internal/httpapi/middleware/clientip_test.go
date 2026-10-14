// The client-address resolution rule (card t_ac16d273).
//
// The defect: ClientIP read CF-Connecting-IP FIRST and unconditionally, and the
// deployed origin (Traefik on 443) is reachable without Cloudflare, so an
// anonymous caller chose its own rate-limit bucket. Measured on prod before the
// fix, 16 concurrent POSTs to a RateLimit-outermost route: 10x401 + 6x429 with
// no header (the limiter working, one bucket) but 16x401 with a distinct
// CF-Connecting-IP per request (a fresh bucket each).
//
// These tests pin the resolution rule and the limiter behaviour it produces.
//
// The same defect was filed a second time from the X-Real-IP side (card
// t_ce806b13: custom-domain traffic never carries our CF header, so the header a
// spoofing caller would actually reach for is X-Real-IP). The three
// bucket-level tests below pin that half header by header — rotation of X-Real-IP
// ALONE from a caller's own address must not pick a bucket, and the same header
// from a trusted front must still separate the clients behind it — so the
// property is measured for every header the rule reads, not only for the one the
// original card was measured with.
package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

const (
	// cfEdge is an address inside a published Cloudflare range, standing in for
	// the edge without touching the network.
	cfEdge = "173.245.48.9"
	// front is the address the reverse proxy has on the container network.
	front = "172.20.0.3"
	// visitor is the address Cloudflare would put in CF-Connecting-IP.
	visitor = "198.51.100.7"
)

// testProxy pins the edge ranges so resolution never fetches them (the server
// loads the real list once at startup); the pinned range is Cloudflare's own
// 173.245.48.0/20.
func testProxy() *TrustedProxy {
	return &TrustedProxy{
		cidrs:   parseRanges([]string{"173.245.48.0/20", "104.16.0.0/13"}),
		fetched: time.Now(),
	}
}

func TestClientIPResolution(t *testing.T) {
	cases := []struct {
		name string
		peer string
		hdrs map[string]string
		want string
	}{
		{
			name: "direct: the socket peer is the client and every header is the caller's own text",
			peer: "203.0.113.9:443",
			hdrs: map[string]string{"CF-Connecting-IP": visitor, "X-Real-IP": "198.51.100.8", "X-Forwarded-For": "198.51.100.9"},
			want: "203.0.113.9",
		},
		{
			name: "direct: a Cloudflare-looking chain does not unlock the CDN header either",
			peer: "203.0.113.9:443",
			hdrs: map[string]string{"X-Forwarded-For": cfEdge, "CF-Connecting-IP": visitor},
			want: "203.0.113.9",
		},
		{
			name: "behind the front: the nearest hop is the client, not the CDN header",
			peer: front + ":443",
			hdrs: map[string]string{"X-Forwarded-For": "203.0.113.200", "CF-Connecting-IP": visitor},
			want: "203.0.113.200",
		},
		{
			name: "behind the front: an entry the caller wrote is to the LEFT of the hop the front wrote",
			peer: front + ":443",
			hdrs: map[string]string{"X-Forwarded-For": "198.51.100.9, 203.0.113.200"},
			want: "203.0.113.200",
		},
		{
			name: "behind the CDN: CF-Connecting-IP is edge-controlled and is the visitor",
			peer: front + ":443",
			hdrs: map[string]string{"X-Forwarded-For": cfEdge, "CF-Connecting-IP": visitor},
			want: visitor,
		},
		{
			name: "behind the CDN with an extra internal hop (the Next /api/* proxy)",
			peer: front + ":443",
			hdrs: map[string]string{"X-Forwarded-For": cfEdge + ", 172.20.0.9", "CF-Connecting-IP": visitor},
			want: visitor,
		},
		{
			name: "behind the front: an internal-only chain must NOT unlock the CDN header",
			peer: front + ":443",
			hdrs: map[string]string{"X-Forwarded-For": "10.1.2.3", "CF-Connecting-IP": visitor},
			want: "10.1.2.3",
		},
		{
			name: "behind the front with no chain at all: the front's address is all we have",
			peer: front + ":443",
			want: front,
		},
		{
			name: "behind the front: X-Real-IP is used when no chain reached us",
			peer: front + ":443",
			hdrs: map[string]string{"X-Real-IP": "203.0.113.200"},
			want: "203.0.113.200",
		},
		{
			name: "behind the CDN: X-Real-IP is the weaker signal and is not consulted while a chain exists",
			peer: front + ":443",
			hdrs: map[string]string{"X-Forwarded-For": cfEdge, "X-Real-IP": "203.0.113.200", "CF-Connecting-IP": visitor},
			want: visitor,
		},
		{
			name: "behind the CDN: a non-IP CF-Connecting-IP falls back to the edge hop",
			peer: front + ":443",
			hdrs: map[string]string{"X-Forwarded-For": cfEdge, "CF-Connecting-IP": "not-an-ip"},
			want: cfEdge,
		},
		{
			name: "malformed entries are skipped, not trusted",
			peer: front + ":443",
			hdrs: map[string]string{"X-Forwarded-For": ",  ,", "CF-Connecting-IP": "junk"},
			want: front,
		},
		{
			name: "an IPv4-mapped peer normalises to one key",
			peer: "[::ffff:10.0.0.7]:9",
			want: "10.0.0.7",
		},
		{
			name: "an entry that carries a port is still an address",
			peer: front + ":443",
			hdrs: map[string]string{"X-Forwarded-For": "203.0.113.200:65535"},
			want: "203.0.113.200",
		},
		{
			name: "loopback: a process on this host is inside the boundary",
			peer: "127.0.0.1:5",
			hdrs: map[string]string{"X-Real-IP": "203.0.113.200"},
			want: "203.0.113.200",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			r.RemoteAddr = tc.peer
			for k, v := range tc.hdrs {
				r.Header.Set(k, v)
			}
			// Resolve the way the server does: RealIP unwraps the chain and
			// ClientIP answers from there.
			var got string
			var remote string
			h := testProxy().RealIP(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got = ClientIP(r)
				remote = r.RemoteAddr
			}))
			h.ServeHTTP(httptest.NewRecorder(), r)

			if got != tc.want {
				t.Errorf("ClientIP = %q, want %q", got, tc.want)
			}
			if remote != tc.want {
				t.Errorf("r.RemoteAddr = %q, want %q (RealIP must publish what ClientIP answers)", remote, tc.want)
			}
		})
	}
}

// TestClientIPWithoutRealIPIsTheSocketPeer: a caller that never ran RealIP (a
// handler mounted outside the server chain, as the package's own tests do) sees
// the peer and nothing else — which is why a test that wants "this request came
// from address X" must put X in RemoteAddr.
func TestClientIPWithoutRealIPIsTheSocketPeer(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.RemoteAddr = "10.9.9.1:4444"
	r.Header.Set("CF-Connecting-IP", visitor)
	if got := ClientIP(r); got != "10.9.9.1" {
		t.Fatalf("ClientIP = %q, want 10.9.9.1 (the socket peer)", got)
	}

	empty := httptest.NewRequest(http.MethodPost, "/", nil)
	empty.RemoteAddr = ""
	if got := ClientIP(empty); got != "" {
		t.Fatalf("ClientIP = %q, want \"\" when there is no peer at all", got)
	}
}

// burstCodes fires n requests at once through h, building each with mk, and
// returns the status codes. Concurrency matters: the limiter refills 1 token per
// second, so a sequential loop can never drain a burst of 10.
func burstCodes(h http.Handler, n int, mk func(i int) *http.Request) []int {
	codes := make([]int, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := httptest.NewRecorder()
			<-start
			h.ServeHTTP(rec, mk(i))
			codes[i] = rec.Code
		}(i)
	}
	close(start)
	wg.Wait()
	return codes
}

func countStatus(codes []int, want int) int {
	n := 0
	for _, c := range codes {
		if c == want {
			n++
		}
	}
	return n
}

// TestDirectRequestCannotChooseItsBucket is the defect itself: 16 concurrent
// requests from ONE public address, each with a distinct CF-Connecting-IP. The
// bucket must drain (burst 10) whatever the header says. Before the fix this
// returned 16x200 with ZERO 429s — one fresh bucket per request.
func TestDirectRequestCannotChooseItsBucket(t *testing.T) {
	h := testProxy().RealIP(RateLimit(okHandler()))

	codes := burstCodes(h, 16, func(i int) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = "203.0.113.9:4444" // the caller's real address, as the origin sees it
		// A DISTINCT edge address per request — the shape measured on prod —
		// plus a distinct chain, so the headers claim a different client both
		// ways. All 16 still arrive from the one address above.
		r.Header.Set("CF-Connecting-IP", "173.245.48."+strconv.Itoa(i+1))
		r.Header.Set("X-Forwarded-For", "198.51.100."+strconv.Itoa(10+i))
		r.Header.Set("X-Real-IP", "198.51.100."+strconv.Itoa(200+i))
		return r
	})

	if got := countStatus(codes, http.StatusTooManyRequests); got != 6 {
		t.Fatalf("429s = %d of %d, want 6 (burst %d drains): the headers chose the bucket.\ncodes=%v",
			got, len(codes), RateLimitBurst, codes)
	}
}

// TestBehindTheFrontCannotChooseItsBucket is the deployed shape: the caller
// reaches the origin, the front writes the caller's OWN address into the chain
// (measured: Traefik deletes a caller-supplied X-Forwarded-For and replaces it
// with the peer it accepted), and the caller still supplies a distinct
// CF-Connecting-IP per request. One bucket — the caller's.
func TestBehindTheFrontCannotChooseItsBucket(t *testing.T) {
	h := testProxy().RealIP(RateLimit(okHandler()))

	codes := burstCodes(h, 16, func(i int) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = front + ":4444"                 // the proxy in front of us
		r.Header.Set("X-Forwarded-For", "203.0.113.9") // what the front recorded
		r.Header.Set("CF-Connecting-IP", "198.51.100."+strconv.Itoa(10+i))
		return r
	})

	if got := countStatus(codes, http.StatusTooManyRequests); got != 6 {
		t.Fatalf("429s = %d of %d, want 6 (one bucket for the caller behind the front)\ncodes=%v",
			got, len(codes), codes)
	}
}

// TestBehindTheCdnStaysPerVisitor is the behaviour the CDN path must keep: when
// the chain shows a Cloudflare edge, CF-Connecting-IP (edge-set; Cloudflare
// refuses a request that supplies its own — 403 error 1000 through the edge, so
// it is not caller-controlled there) is the visitor, and 16 different visitors
// each get their own bucket.
func TestBehindTheCdnStaysPerVisitor(t *testing.T) {
	h := testProxy().RealIP(RateLimit(okHandler()))

	codes := burstCodes(h, 16, func(i int) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = front + ":4444"
		r.Header.Set("X-Forwarded-For", cfEdge) // the edge the front recorded
		r.Header.Set("CF-Connecting-IP", "198.51.100."+strconv.Itoa(10+i))
		return r
	})

	if got := countStatus(codes, http.StatusOK); got != 16 {
		t.Fatalf("200s = %d of %d, want 16 (per-visitor buckets behind the CDN)\ncodes=%v",
			got, len(codes), codes)
	}
}

// TestDirectXRealIPRotationCannotChooseItsBucket is the header this defect was
// filed twice for (card t_ce806b13): 16 concurrent requests from ONE public
// address, each carrying nothing but a distinct X-Real-IP — no chain and no CDN
// header, so no other input can explain a fresh bucket. The socket peer is the
// only evidence a directly-connected caller leaves, so the burst must drain
// (burst 10 -> 6x429). Before the fix this header was read unconditionally and
// the same burst returned 16x200 with ZERO 429s.
//
// This is deliberately narrower than TestDirectRequestCannotChooseItsBucket
// (which rotates all three headers at once): it names the ONE header a
// custom-domain caller would reach for — custom-domain traffic never carries our
// CF-Connecting-IP, so X-Real-IP is the only lever such a caller has.
func TestDirectXRealIPRotationCannotChooseItsBucket(t *testing.T) {
	h := testProxy().RealIP(RateLimit(okHandler()))

	codes := burstCodes(h, 16, func(i int) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = "203.0.113.9:4444" // the caller's real address, as the origin sees it
		r.Header.Set("X-Real-IP", "198.51.100."+strconv.Itoa(100+i))
		return r
	})

	if got := countStatus(codes, http.StatusTooManyRequests); got != 6 {
		t.Fatalf("429s = %d of %d, want 6 (burst %d drains: one bucket for the one address that reached us)\ncodes=%v",
			got, len(codes), RateLimitBurst, codes)
	}
}

// TestDirectForwardedForRotationCannotChooseItsBucket is the same shape for the
// other header the rule reads: a distinct X-Forwarded-For per request from one
// public address must not buy a fresh bucket either. One bucket, burst 10, 6x429.
func TestDirectForwardedForRotationCannotChooseItsBucket(t *testing.T) {
	h := testProxy().RealIP(RateLimit(okHandler()))

	codes := burstCodes(h, 16, func(i int) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = "203.0.113.9:4444"
		r.Header.Set("X-Forwarded-For", "198.51.100."+strconv.Itoa(100+i))
		return r
	})

	if got := countStatus(codes, http.StatusTooManyRequests); got != 6 {
		t.Fatalf("429s = %d of %d, want 6 (burst %d drains: a caller-written chain is not a key)\ncodes=%v",
			got, len(codes), RateLimitBurst, codes)
	}
}

// TestBehindTheFrontXRealIPStillSeparatesClients is the other half the card
// demands: the rule must not over-correct. A request that reached us THROUGH a
// proxy may still be keyed on the address that proxy recorded, so 16 buyers
// behind the front, each recorded with its own X-Real-IP (no chain — the front
// recorded the hop separately), keep 16 buckets and none of them is throttled.
// Keying them all on the front's own address would be a worse defect than the
// one this card fixes: one bucket, and one login-failure lockout, for the whole
// site.
func TestBehindTheFrontXRealIPStillSeparatesClients(t *testing.T) {
	h := testProxy().RealIP(RateLimit(okHandler()))

	codes := burstCodes(h, 16, func(i int) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = front + ":4444"
		r.Header.Set("X-Real-IP", "198.51.100."+strconv.Itoa(100+i))
		return r
	})

	if got := countStatus(codes, http.StatusOK); got != 16 {
		t.Fatalf("200s = %d of %d, want 16 (each recorded hop is its own bucket)\ncodes=%v",
			got, len(codes), codes)
	}
}

// itoa is gone: strconv is imported.
