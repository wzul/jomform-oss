// Package ssrfguard provides a shared Server-Side Request Forgery guard for
// outbound fetches of merchant/operator-supplied URLs (webhook delivery,
// custom-domain verify probe, MCP webhook validation). It resolves the host
// and rejects any destination that maps to loopback, link-local, private,
// unspecified, CGNAT, or cloud-metadata addresses, and produces an HTTP
// transport that pins the socket to a single pre-validated resolved IP so a
// host cannot be rebound to a private address between validation and connect
// (DNS-rebinding TOCTOU).
package ssrfguard

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrBlocked is returned when a URL resolves to (or is) a disallowed address.
var ErrBlocked = errors.New("request address is not allowed")
// IsAllowedAddr reports whether ip may be fetched outbound. It rejects
// loopback, unspecified, private (RFC1918), link-local unicast/multicast
// (169.254.0.0/16 incl. cloud metadata 169.254.169.254), and CGNAT
// (100.64.0.0/10) addresses, for both IPv4 and IPv6.
func IsAllowedAddr(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	// CGNAT / Shared Address Space 100.64.0.0/10 (RFC 6598): not flagged by
	// IsPrivate; only usable by a carrier, so disallow outbound fetches.
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
		return false
	}
	return true
}

// CheckHostname resolves host and returns an error if any resolved address is
// disallowed. It fails closed: resolution failure or a disallowed address both
// return a non-nil wrapped error (callers treat any error as "blocked").
func CheckHostname(ctx context.Context, host string) error {
	if host == "" {
		return fmt.Errorf("%w: empty host", ErrBlocked)
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("%w: %q did not resolve: %v", ErrBlocked, host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("%w: %q did not resolve", ErrBlocked, host)
	}
	for _, ia := range ips {
		if !IsAllowedAddr(ia.IP) {
			return fmt.Errorf("%w: %q resolves to forbidden address %v", ErrBlocked, host, ia.IP)
		}
	}
	return nil
}

// ValidateURL parses raw, checks it is an absolute http(s) URL with a
// non-empty host, and verifies the host does not resolve to a forbidden
// address. It returns the host it validated (for callers that need to pin it).
func ValidateURL(ctx context.Context, raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !u.IsAbs() {
		return "", errors.New("url tidak sah")
	}
	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return "", errors.New("url mesti http/https")
	}
	host := u.Hostname()
	if host == "" {
		return "", errors.New("url mesti ada host")
	}
	if err := CheckHostname(ctx, host); err != nil {
		return "", err
	}
	return host, nil
}

// PrepareURL parses raw, enforces http(s) and a host, and resolves it exactly
// once to a pinned, allowed address. This single-resolution path makes
// validation and dialing agree on the same IP, so a DNS-rebinding attack
// cannot present a public address at check time and a private one at connect
// time. It returns the validated host plus the pin to feed into an
// http.Client's DialContext.
func PrepareURL(resolver Resolver, raw string) (host string, pin *PinResult, err error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !u.IsAbs() {
		return "", nil, errors.New("url tidak sah")
	}
	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return "", nil, errors.New("url mesti http/https")
	}
	host = u.Hostname()
	if host == "" {
		return "", nil, errors.New("url mesti ada host")
	}
	pin, err = Pin(resolver, host)
	if err != nil {
		return "", nil, err
	}
	return host, pin, nil
}

// PinResult carries the pinned dial context for one outbound fetch.
type PinResult struct {
	// DialContext dials the pre-validated IP for a resolved host, preserving
	// the destination port passed by the http.Transport.
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)
	// IPs are the allowed addresses the connection may use.
	IPs []net.IP
	// transport is the lazily-created pinned http.Transport; owned by this pin
	// and closed via Close.
	transport *http.Transport
}

// Transport returns an http.Transport that routes every connection through the
// pin's DialContext (single validated IP), disabling the transport's own DNS
// re-resolution so a rebinding host can never reconnect to a private address.
func (p *PinResult) Transport() *http.Transport {
	if p.transport == nil {
		p.transport = &http.Transport{
			DialContext: p.DialContext,
		}
	}
	return p.transport
}

// Close releases the pinned transport's idle connections. It is safe to call
// after the outbound request completes; not calling it only leaks idle sockets
// until GC.
func (p *PinResult) Close() {
	if p.transport != nil {
		p.transport.CloseIdleConnections()
	}
}

// Resolver abstracts hostname → address lookups so Pin/CheckHostname can be
// unit-tested with a static resolver. *net.Resolver satisfies it.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// Pin resolves host once, requires every resolved address to be allowed, and
// returns a DialContext that pins every connection to a single allowed IP
// while preserving the destination port from addr. This defeats DNS rebinding
// / TOCTOU between validation and connect. A literal IP host is validated and
// pinned as well. It returns ErrBlocked (via errors.Is) ONLY when the address
// is genuinely disallowed; a transient DNS resolution failure returns a plain
// error so callers can treat it as a retryable outage rather than a permanent
// block.
func Pin(resolver Resolver, host string) (*PinResult, error) {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	ips, err := resolver.LookupIPAddr(context.Background(), host)
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("resolve %q: no addresses", host)
	}
	allowed := make([]net.IP, 0, len(ips))
	for _, ia := range ips {
		if !IsAllowedAddr(ia.IP) {
			return nil, fmt.Errorf("%w: %q resolves to forbidden address %v", ErrBlocked, host, ia.IP)
		}
		allowed = append(allowed, ia.IP)
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return &PinResult{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if network != "tcp" && network != "tcp4" && network != "tcp6" {
				return nil, fmt.Errorf("unsupported network %q", network)
			}
			_, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			var lastErr error
			for _, ip := range allowed {
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			return nil, lastErr
		},
		IPs: allowed,
	}, nil
}

// NewPinnedTransport returns an http.Transport that pins every connection to a
// pre-validated, resolved IP of host, so redirects and re-resolves can never
// land on a private/metadata address. tlsConfig may be nil (defaults used).
// Callers should CloseIdleConnections() when done with the transport.
func NewPinnedTransport(host string, tlsConfig *tls.Config) (*http.Transport, *PinResult, error) {
	pin, err := Pin(net.DefaultResolver, host)
	if err != nil {
		return nil, nil, err
	}
	tr := &http.Transport{
		DialContext: pin.DialContext,
	}
	if tlsConfig != nil {
		tr.TLSClientConfig = tlsConfig.Clone()
	}
	return tr, pin, nil
}
