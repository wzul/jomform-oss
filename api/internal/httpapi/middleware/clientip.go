// Client address resolution: WHO the request came from, and why that answer can
// be trusted.
//
// A request carries two kinds of evidence about its origin:
//
//   - the address that opened the TCP connection to this process (the socket
//     peer, r.RemoteAddr), which the caller cannot choose; and
//   - the forwarding headers (X-Forwarded-For, X-Real-IP, CF-Connecting-IP),
//     which ANY caller can write, and which only mean something when a proxy we
//     control wrote them.
//
// So the headers are read ONLY when the socket peer is an address a request can
// arrive THROUGH (intermediaryRanges, or a Cloudflare edge range) — and the peer
// classes NOT in that set are exactly the ones a client on the public internet
// can present. A request from such a peer carries nothing but the caller's own
// text in those headers, so the socket peer is the only evidence and it wins.
// That is the hole t_ac16d273 measured: the origin (Traefik, port 443) was
// reachable without Cloudflare, and CF-Connecting-IP was read unconditionally,
// so an anonymous caller chose its own rate-limit bucket (16 concurrent requests
// with a distinct header each: 16x401, ZERO 429 — while the same burst with no
// header drained burst 10 and returned 10x401 + 6x429).
//
// Behind a proxy the chain is read RIGHT TO LEFT, because every proxy appends
// the address it accepted the connection from: the first hop that is not itself
// a proxy is the client, and anything the caller wrote sits to the LEFT of it.
// Measured against the deployed front (traefik:v3.3 with the entrypoint
// configuration from deploy/vps/traefik-docker-compose.yml, 2026-10-11): a
// caller-supplied X-Forwarded-For and X-Real-IP are DELETED and replaced with
// the address of the peer Traefik accepted — a request from 172.17.0.1 that
// asked for "X-Forwarded-For: 203.0.113.79, 198.51.100.1" arrived as
// "X-Forwarded-For: 172.17.0.1" — while CF-Connecting-IP is passed through
// UNTOUCHED. That last header is what t_ac16d273 exploited: it was read first
// and unconditionally, so a request to the origin that never passed through
// Cloudflare picked its own bucket key.
//
// The one PUBLIC address that may be trusted as a hop is the Cloudflare edge
// (TrustedProxy.cloudflareEdge): when every hop in the visible chain is a proxy
// and one of them is a Cloudflare address, the request came through the CDN, and
// Cloudflare sets CF-Connecting-IP to the visitor. A caller cannot supply that
// header through the edge — Cloudflare refuses the request (measured
// 2026-10-11: 403, "error code: 1000") — so it is edge-controlled exactly when
// the chain says we are behind the edge. Honouring it there is what keeps the
// limiter per-VISITOR behind the CDN instead of collapsing every visitor behind
// one edge address.
//
// What this file does NOT do: it does not decide whether the proxy in front
// sanitises the forwarding headers — trust in that hop rests on the deployment
// (see the Traefik measurement above). It does mean the API no longer has to be
// correct about a header no proxy ever sanitises.
package middleware

import (
	"context"
	"net"
	"net/http"
	"strings"
)

// clientIPKey carries the address TrustedProxy.RealIP resolved, so ClientIP
// answers the same value everywhere in one request without re-walking the chain.
type clientIPKey struct{}

// intermediaryRanges are addresses a hop can have and still be the hop a request
// arrived THROUGH rather than the client that sent it: this host, and the
// container network the reverse proxy sits on. None of them can be the source of
// a connection from the public internet, and the API is not reachable from there
// directly either (its container port is not published, so a connection to the
// host is filtered — card t_ac16d273), so every internet request arrives from a
// proxy whose own address is in one of these ranges — that is the boundary, and
// the proxies that speak to us (Traefik, and the Next /api/* proxy behind it)
// live inside it.
//
// The trade-off, stated: a request that reaches this process from inside this
// host or the network (a local process, a sibling container) still has its
// forwarding headers read. That is the same boundary the proxy itself lives in,
// which is why it is acceptable here — and it is why the rule below is "a peer
// the internet cannot have", not "a peer I like".
var intermediaryRanges = parseRanges([]string{
	"10.0.0.0/8",     // RFC1918
	"172.16.0.0/12",  // RFC1918 — where the docker bridge networks live
	"192.168.0.0/16", // RFC1918
	"100.64.0.0/10",  // RFC6598 carrier-grade NAT
	"169.254.0.0/16", // link-local
	"127.0.0.0/8",    // loopback: a process on this host
	"::1/128",        // loopback v6
	"fc00::/7",       // unique-local
	"fe80::/10",      // link-local v6
})

// ClientIP returns the address the request came from: the one RealIP resolved
// from the forwarding headers when the request reached us through a proxy we
// trust, otherwise the socket peer. It is the key every rate limiter, the
// login-failure tracker and the audit log use, and realip.go/this file are the
// only readers of those headers (the class guard in
// forwarded_headers_one_reader_test.go refuses a second reader anywhere in the
// api module) — see the file comment above for why the answer is trustworthy.
func ClientIP(r *http.Request) string {
	if ip, ok := r.Context().Value(clientIPKey{}).(string); ok && ip != "" {
		return ip
	}
	return socketPeer(r)
}

// socketPeer returns the address that opened the connection, normalised, or the
// raw value when it does not parse (a test request whose RemoteAddr was cleared
// keeps its old behaviour of returning whatever is there).
func socketPeer(r *http.Request) string {
	peer := hostOf(r.RemoteAddr)
	if peer == "" {
		return ""
	}
	if ip := validIP(peer); ip != "" {
		return ip
	}
	return peer
}

// resolveClientIP is the trust decision, in one place.
func (t *TrustedProxy) resolveClientIP(r *http.Request) string {
	peer := socketPeer(r)
	if peer == "" {
		return ""
	}
	if !t.proxyHop(peer) {
		// Nothing in front of us: every forwarding header is text the caller
		// typed, so the peer is the client. This is the whole point of
		// t_ac16d273 — before this check, ClientIP honoured CF-Connecting-IP
		// here, and a request straight to the origin chose its own bucket.
		return peer
	}

	hops := forwardedHops(r)
	for _, hop := range hops {
		if !t.proxyHop(hop) {
			// The nearest hop the proxy that accepted this connection did not
			// identify as a proxy is the client. Caller-written entries are to
			// its left and are never reached.
			return hop
		}
	}

	// Every visible hop is a proxy: the request came through an edge we can
	// name. If that edge is Cloudflare, CF-Connecting-IP is the visitor (see the
	// file comment), so the bucket stays per-visitor behind the CDN.
	if len(hops) > 0 {
		for _, hop := range hops {
			if t.cloudflareEdge(hop) {
				if ip := validIP(r.Header.Get("CF-Connecting-IP")); ip != "" {
					return ip
				}
				break
			}
		}
		return hops[0] // the hop closest to us: our proxy's own record of the peer
	}
	return peer
}

// forwardedHops returns the addresses the proxies in front of us left, ordered
// from the hop CLOSEST to us outwards, so a caller's own entries end up last.
//
// X-Forwarded-For is the chain (rightmost = closest: every proxy appends the
// address it accepted the connection from). X-Real-IP is the same closest hop
// recorded separately and is used ONLY when no X-Forwarded-For reached us: a
// proxy that trusts its own peer keeps an incoming X-Real-IP verbatim, so it is
// the weaker of the two signals and must not be consulted while a chain exists.
func forwardedHops(r *http.Request) []string {
	var out []string
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			if ip := validIP(parts[i]); ip != "" {
				out = append(out, ip)
			}
		}
		return out
	}
	if ip := validIP(r.Header.Get("X-Real-IP")); ip != "" {
		out = append(out, ip)
	}
	return out
}

// RealIP middleware: publish the client address this request demonstrably came
// from (see the file comment), so ClientIP and r.RemoteAddr agree for the rest
// of the chain.
func (t *TrustedProxy) RealIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := t.resolveClientIP(r)
		if ip != "" {
			r = r.WithContext(context.WithValue(r.Context(), clientIPKey{}, ip))
			r.RemoteAddr = ip
		}
		next.ServeHTTP(w, r)
	})
}

// proxyHop reports whether ip is an address the request can arrive THROUGH: the
// container network, or (when the ranges have been fetched) the Cloudflare edge.
func (t *TrustedProxy) proxyHop(ip string) bool {
	return inRanges(intermediaryRanges, ip) || t.cloudflareEdge(ip)
}

// hostOf strips the port from a "host:port" address; a bare address (or one that
// does not parse as host:port, e.g. a bare IPv6 literal) is returned unchanged.
func hostOf(addr string) string {
	addr = strings.TrimSpace(addr)
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// validIP returns s normalised ("::ffff:203.0.113.1" and "203.0.113.1" key the
// same bucket), or "" when s is not an address.
func validIP(s string) string {
	host := hostOf(s)
	if host == "" {
		return ""
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return ""
	}
	return ip.String()
}

// parseRanges turns CIDR strings into networks, panicking on a malformed entry:
// these are package constants, so a bad one is a programming error, not input.
func parseRanges(specs []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(specs))
	for _, s := range specs {
		_, cidr, err := net.ParseCIDR(s)
		if err != nil {
			panic("middleware: bad CIDR " + s + ": " + err.Error())
		}
		out = append(out, cidr)
	}
	return out
}

// inRanges reports whether ip falls inside any of the networks (false when ip is
// not an address).
func inRanges(ranges []*net.IPNet, ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, c := range ranges {
		if c.Contains(parsed) {
			return true
		}
	}
	return false
}
