package httpapi

import (
	"net"
	"net/url"
	"strings"
)

// sharedCookieDomain derives the shared cookie Domain from the app's public
// base URL (config) so the jf_session cookie is shared across the dashboard
// host (jomform.com) and its product subdomains (mcp.jomform.com), which is
// required for the OAuth authorize flow (issue #140).
//
// It returns the registrable domain prefixed with ".", e.g.:
//
//	https://jomform.com        -> .jomform.com
//	https://mcp.jomform.com    -> .jomform.com
//
// returns "" (no Domain attribute → host-only cookie) when the base URL is a
// localhost/IP/dev host, so development and non-registrable hosts are never
// unintentionally scoped to a shared domain.
func SharedCookieDomain(appBaseURL string) string {
	u, err := url.Parse(appBaseURL)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	if host == "" || host == "localhost" {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil {
		return ""
	}
	// Strip a leading subdomain label (if present) to reach the registrable
	// domain (e.g. mcp.jomform.com -> jomform.com). This heuristic assumes a
	// single second-level registrable label, which is correct for this app's
	// hosts; a bare host with no subdomain is already registrable.
	last := strings.LastIndexByte(host, '.')
	if last <= 0 {
		return ""
	}
	before := strings.LastIndexByte(host[:last], '.')
	var reg string
	if before == -1 {
		reg = host
	} else {
		reg = host[before+1:]
	}
	if reg == "" || !strings.Contains(reg, ".") {
		return ""
	}
	return "." + reg
}
